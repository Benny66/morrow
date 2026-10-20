package service

// 桌面宠物的素材包读取与导入。
//
// 【为什么走文件系统而不是数据库】宠物素材是「用户从外部装进来的资源包」：
//   - 它跟着 pet.json 与 spritesheet.webp 走，本身就有目录结构，
//     复制进数据目录后就是一个自洽的包；
//   - 它不参与任何业务查询（没有按条件筛选、没有跨表关联），
//     落成一张表只会多一层迁移与同步负担；
//   - 复用 ~/.codex/pets 的前提是【只读同一个目录】——把它搬进 SQLite 之后，
//     用户在 Codex 客户端里换宠物，本应用还得重新扫一遍，反而更复杂。
//
// 故本文件里没有一行 gorm：宠物的真相就是磁盘上的目录。
//
// 三种素材来源统一成一个 PetPack 视图（见 PetPack 注释）：
//   - builtin：程序化绘制（前端 SVG/CSS），没有精灵图；
//   - codex：直接复用 ~/.codex/pets/<id>/pet.json（只读，不修改该目录）；
//   - imported：用户手动导入，复制到 <数据目录>/pets/<id>/ 后由本服务接管。

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"morrow/internal/appdir"
	"morrow/internal/apperr"
	"morrow/internal/config"
)

// 宠物素材来源（单一真相）。前端只做展示，不据此分支。
const (
	// PetSourceBuiltin 内置的程序化绘制形象。
	PetSourceBuiltin = "builtin"
	// PetSourceCodex 复用 Codex 客户端的宠物包（只读）。
	PetSourceCodex = "codex"
	// PetSourceImported 用户手动导入、已复制进数据目录的宠物包。
	PetSourceImported = "imported"
)

// 宠物包的约定文件名与限制。
const (
	// petManifestFile 宠物包清单文件名（codexpet 生态的约定）。
	petManifestFile = "pet.json"
	// petSheetFile 默认精灵图文件名，也是导入后的统一落名（扩展名沿用源文件）。
	petSheetFile = "spritesheet.webp"
	// petPacksDirName 应用数据目录下存放导入宠物包的子目录名。
	petPacksDirName = "pets"
	// maxPetSpriteBytes 精灵图大小上限（8MB）。
	//
	// 为什么要卡：精灵图经绑定方法以 base64 字符串（约 1.33×）过 JSON 送到前端，
	// 一个几十 MB 的图集会让 IPC 直接卡死窗口，而且 base64 串还会进日志/错误详情。
	// 正常 v1 图集（1536×1872 webp）只有几百 KB，8MB 已是宽松上限。
	maxPetSpriteBytes = 8 << 20
	// maxPetIDLength 宠物包 ID 长度上限：ID 直接当目录名用，过长会撞系统路径限制。
	maxPetIDLength = 64
)

// codexPetsRelativePath 是 Codex 客户端宠物包相对用户主目录的位置（只读复用）。
var codexPetsRelativePath = filepath.Join(".codex", "pets")

// PetPack 是一个可用的宠物素材包。
//
// 三种来源（builtin / codex / imported）在界面上是同一个下拉框里的选项，
// 故统一成本结构；前端据 HasSprite 决定用 canvas 画图集还是用内置 SVG。
type PetPack struct {
	// ID 素材包标识，也是目录名（导入后不可变）。
	ID string `json:"id"`
	// Name 展示名（pet.json 的 displayName，缺失时回落为 ID）。
	Name string `json:"name"`
	// Description 一句话说明，可为空。
	Description string `json:"description"`
	// Source 来源，取值见 PetSource* 常量。
	Source string `json:"source"`
	// SpritePath 精灵图在素材包目录内的相对路径；空表示 builtin（无需精灵图）。
	SpritePath string `json:"sprite_path"`
	// HasSprite 是否真的有可用精灵图（前端据此决定渲染方式）。
	HasSprite bool `json:"has_sprite"`
}

// petManifest 是 pet.json 里本服务关心的字段。
//
// 【字段缺失一律给兜底，不因缺字段失败】实测不同的宠物包写法并不统一
// （有的没有 spriteVersionNumber、有的用 display_name/spritesheet_path 下划线写法），
// 把它们当作错误只会让用户「导入了却看不到」。故解析走 map + 别名兜底，
// 见 parsePetManifest。
type petManifest struct {
	// ID 素材包 ID。
	ID string `json:"id"`
	// DisplayName 展示名。
	DisplayName string `json:"displayName"`
	// Description 说明。
	Description string `json:"description"`
	// SpriteVersionNumber 图集版本号。本服务不校验版本（只支持 v1 的 8×9 布局），
	// 但会原样写回导入后的清单，保留来源信息。
	SpriteVersionNumber int `json:"spriteVersionNumber"`
	// SpritesheetPath 图集相对路径，缺省时按约定文件名探测。
	SpritesheetPath string `json:"spritesheetPath"`
}

// petRoot 是一个宠物包根目录及其来源标记。
type petRoot struct {
	dir    string
	source string
}

// ---------------------------------------------------------------------------
// 列出素材包
// ---------------------------------------------------------------------------

// ListPetPacks 返回全部可用宠物包：内置项 + 数据目录里的导入项 + Codex 目录里的项。
//
// 同名 ID 以后者为准——顺序是「先 codex、后 imported」，故导入的包会覆盖同 ID 的
// codex 包（用户手动导入的意图更明确，应当生效）。
// 目录不存在不算错误：绝大多数用户没有装过宠物，那是正常状态而不是故障。
func (s *Service) ListPetPacks(appName string) ([]PetPack, error) {
	roots, err := s.petRoots(appName)
	if err != nil {
		return nil, err
	}

	// key 为 ID：后扫到的同名包覆盖先扫到的（见函数注释里的优先级）。
	byID := map[string]PetPack{}
	for _, root := range roots {
		for _, pack := range listPetPacksIn(root) {
			byID[pack.ID] = pack
		}
	}

	out := make([]PetPack, 0, len(byID)+1)
	out = append(out, builtinPetPack())
	// 分组输出：内置 → 导入的 → Codex 复用的，各自按名称排序。
	// 固定顺序是为了让下拉框在多次打开之间不跳动（map 遍历顺序是随机的）。
	for _, source := range []string{PetSourceImported, PetSourceCodex} {
		group := make([]PetPack, 0, len(byID))
		for _, pack := range byID {
			if pack.Source == source {
				group = append(group, pack)
			}
		}
		sort.Slice(group, func(i, j int) bool {
			if group[i].Name == group[j].Name {
				return group[i].ID < group[j].ID
			}
			return group[i].Name < group[j].Name
		})
		out = append(out, group...)
	}
	return out, nil
}

// builtinPetPack 返回内置宠物包（程序化绘制，无素材文件）。
func builtinPetPack() PetPack {
	return PetPack{
		ID:          config.DefaultPetPackID,
		Name:        "内置小宠",
		Description: "程序化绘制的占位形象，无需任何素材文件",
		Source:      PetSourceBuiltin,
		SpritePath:  "",
		HasSprite:   false,
	}
}

// petRoots 返回宠物包根目录列表（顺序即优先级：codex 在前，导入的在后并覆盖之）。
func (s *Service) petRoots(appName string) ([]petRoot, error) {
	dataDir, err := appdir.Dir(appName)
	if err != nil {
		return nil, apperr.Wrap(err)
	}
	roots := make([]petRoot, 0, 2)
	// Codex 目录排在前：它可能不存在，且优先级低于导入的包。
	if home, homeErr := os.UserHomeDir(); homeErr == nil && strings.TrimSpace(home) != "" {
		roots = append(roots, petRoot{dir: filepath.Join(home, codexPetsRelativePath), source: PetSourceCodex})
	}
	roots = append(roots, petRoot{dir: filepath.Join(dataDir, petPacksDirName), source: PetSourceImported})
	return roots, nil
}

// listPetPacksIn 扫描一个根目录下的全部宠物包。目录不存在/不可读时返回空切片。
func listPetPacksIn(root petRoot) []PetPack {
	entries, err := os.ReadDir(root.dir)
	if err != nil {
		// 目录不存在是常态（用户没装过宠物），不打扰用户、也不必报错。
		return nil
	}
	out := make([]PetPack, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pack, ok := readPetPack(filepath.Join(root.dir, entry.Name()), root.source)
		if !ok {
			continue
		}
		out = append(out, pack)
	}
	return out
}

// readPetPack 读取单个宠物包目录；清单缺失或非法时返回 false（跳过该目录）。
//
// 跳过而不报错：一个坏目录不该让整个下拉框变空——用户还有别的包可用。
// 但坏目录必须留日志，否则「我明明放进去了却不显示」无从排查。
func readPetPack(dir, source string) (PetPack, bool) {
	manifestPath := filepath.Join(dir, petManifestFile)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return PetPack{}, false
	}
	manifest, ok := parsePetManifest(data)
	if !ok {
		slog.Warn("宠物包清单解析失败，已跳过", "path", manifestPath)
		return PetPack{}, false
	}

	id := sanitizePetID(manifest.ID)
	if id == "" {
		// 清单没写 id 时用目录名兜底：目录名天然唯一，且用户看得懂。
		id = sanitizePetID(filepath.Base(dir))
	}
	// ID 与内置包同名会让下拉框出现两个「builtin」且 SetPetSettings 无法区分，
	// 故直接跳过（内置包永远赢）。
	if id == "" || id == config.DefaultPetPackID {
		return PetPack{}, false
	}

	name := strings.TrimSpace(manifest.DisplayName)
	if name == "" {
		name = id
	}
	sheet, hasSheet := findPetSheet(dir, manifest.SpritesheetPath)
	return PetPack{
		ID:          id,
		Name:        name,
		Description: strings.TrimSpace(manifest.Description),
		Source:      source,
		SpritePath:  sheet,
		HasSprite:   hasSheet,
	}, true
}

// ---------------------------------------------------------------------------
// 读取精灵图
// ---------------------------------------------------------------------------

// GetPetSprite 返回可直接放进 <img>/canvas 的精灵图 data URI
// （形如 "data:image/webp;base64,<...>"）。
//
// 为什么要返回 data URI 而不是文件路径：渲染层没有读取本地文件的能力
// （前端护栏禁止渲染层碰 node/文件系统），而 wails 的 asset server 只服务
// 打包进二进制的 frontend/dist。把图集编成 data URI 是最短的一条路。
func (s *Service) GetPetSprite(appName, packID string) (string, error) {
	id := sanitizePetID(packID)
	if id == "" {
		return "", apperr.Validation("宠物包 ID 非法")
	}
	if id == config.DefaultPetPackID {
		return "", apperr.Validation("内置宠物为程序化绘制，没有精灵图")
	}

	roots, err := s.petRoots(appName)
	if err != nil {
		return "", err
	}
	// 导入的包优先（与 ListPetPacks 的优先级一致）：倒序查找。
	for i := len(roots) - 1; i >= 0; i-- {
		dir := filepath.Join(roots[i].dir, id)
		// 兜底防线：即使 sanitizePetID 漏掉了某种写法，也不许解析出根目录之外。
		if !isInside(dir, roots[i].dir) {
			continue
		}
		sheet, ok := petSheetPath(dir)
		if !ok {
			continue
		}
		dataURI, readErr := readSpriteDataURI(sheet)
		if readErr != nil {
			return "", readErr
		}
		return dataURI, nil
	}
	return "", apperr.NotFound("找不到宠物包：" + id + "（或它没有可用的精灵图）")
}

// petSheetPath 返回某个宠物包目录里精灵图的绝对路径；没有可用精灵图时返回 false。
func petSheetPath(dir string) (string, bool) {
	declared := ""
	if data, err := os.ReadFile(filepath.Join(dir, petManifestFile)); err == nil {
		if manifest, ok := parsePetManifest(data); ok {
			declared = manifest.SpritesheetPath
		}
	}
	rel, ok := findPetSheet(dir, declared)
	if !ok {
		return "", false
	}
	path, ok := resolveInside(dir, rel)
	if !ok {
		return "", false
	}
	return path, true
}

// findPetSheet 在 dir 下定位精灵图，返回相对路径与是否找到。
//
// 顺序：清单声明的路径优先，其次按约定文件名探测（webp → png → jpg → gif）。
// 只返回【相对路径】：绝对路径或越界路径一律跳过（见 resolveInside）。
func findPetSheet(dir, declared string) (string, bool) {
	candidates := make([]string, 0, 6)
	if path := strings.TrimSpace(declared); path != "" {
		candidates = append(candidates, path)
	}
	candidates = append(candidates,
		petSheetFile,
		"spritesheet.png",
		"spritesheet.jpg",
		"spritesheet.jpeg",
		"spritesheet.gif",
	)

	for _, candidate := range candidates {
		path, ok := resolveInside(dir, candidate)
		if !ok {
			continue
		}
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		if info.Size() > maxPetSpriteBytes || info.Size() == 0 {
			continue
		}
		return candidate, true
	}
	return "", false
}

// resolveInside 把相对路径解析到 dir 之内；越界（../、绝对路径）返回 false。
//
// 这是防目录穿越的唯一入口：pet.json 的 spritesheetPath 是用户可写的字段，
// 一句 "../../../../etc/passwd" 就能把任意文件读成 base64 交给渲染层。
func resolveInside(dir, rel string) (string, bool) {
	text := strings.TrimSpace(rel)
	if text == "" {
		return "", false
	}
	// 清单可能来自 Windows（反斜杠）或手写，统一成正斜杠再判。
	text = strings.ReplaceAll(text, "\\", "/")
	if strings.HasPrefix(text, "/") || filepath.IsAbs(text) {
		return "", false
	}
	path := filepath.Join(dir, filepath.FromSlash(text))
	if !isInside(path, dir) {
		return "", false
	}
	return path, true
}

// readSpriteDataURI 把精灵图读成 data URI（含大小上限与 MIME 判定）。
func readSpriteDataURI(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", apperr.NotFound("精灵图不存在：" + filepath.Base(path))
	}
	if info.IsDir() {
		return "", apperr.Validation("精灵图路径指向目录")
	}
	if info.Size() > maxPetSpriteBytes {
		return "", apperr.Validation(fmt.Sprintf("精灵图过大（上限 %dMB）", maxPetSpriteBytes>>20))
	}

	file, err := os.Open(path)
	if err != nil {
		return "", apperr.Wrap(err)
	}
	defer func() {
		// 只读文件，关闭失败没有可补救的动作；仍留日志便于排查句柄泄漏。
		if closeErr := file.Close(); closeErr != nil {
			slog.Warn("关闭精灵图文件失败", "path", path, "err", closeErr)
		}
	}()

	// 再按上限 +1 读：Stat 与 Open 之间文件可能被替换成更大的，这道限制才真正生效。
	data, err := io.ReadAll(io.LimitReader(file, maxPetSpriteBytes+1))
	if err != nil {
		return "", apperr.Wrap(fmt.Errorf("读取精灵图失败: %w", err))
	}
	if len(data) == 0 {
		return "", apperr.Validation("精灵图是空文件")
	}
	if len(data) > maxPetSpriteBytes {
		return "", apperr.Validation(fmt.Sprintf("精灵图过大（上限 %dMB）", maxPetSpriteBytes>>20))
	}
	return "data:" + spriteMIME(path) + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

// spriteMIME 按扩展名给出图集 MIME。未知扩展名按 webp 处理（本仓的约定格式）。
func spriteMIME(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	default:
		return "image/webp"
	}
}

// ---------------------------------------------------------------------------
// 导入素材包
// ---------------------------------------------------------------------------

// ImportPetPack 把 sourcePath 指向的宠物包复制进应用数据目录，返回导入后的包信息。
//
// sourcePath 由调用方（app.go 的绑定方法）经系统「选择文件」对话框取得，
// 因此可能是 pet.json，也可能用户直接选了精灵图。两种入口都接受：
//   - 选了 pet.json：以它为准，精灵图按清单声明/约定名在【同一目录】找；
//   - 选了精灵图：从同一目录找 pet.json（缺清单时用文件名当 ID 与名称）。
//
// 只读源目录，不做任何修改（Codex 目录尤其不能被写）。
func (s *Service) ImportPetPack(appName, sourcePath string) (PetPack, error) {
	path := strings.TrimSpace(sourcePath)
	if path == "" {
		return PetPack{}, apperr.Validation("未选择宠物包文件")
	}
	path = filepath.Clean(path)
	info, err := os.Stat(path)
	if err != nil {
		return PetPack{}, apperr.NotFound("所选文件不存在：" + filepath.Base(path))
	}
	if info.IsDir() {
		return PetPack{}, apperr.Validation("请选择宠物包里的 pet.json 或精灵图文件")
	}

	sourceDir := filepath.Dir(path)
	baseName := filepath.Base(path)

	manifest, err := resolveImportManifest(path, baseName)
	if err != nil {
		return PetPack{}, err
	}

	sheetRel, ok := findPetSheet(sourceDir, manifest.SpritesheetPath)
	if !ok {
		return PetPack{}, apperr.Validation("所选宠物包里找不到可用的精灵图（spritesheet.webp）")
	}

	id := sanitizePetID(manifest.ID)
	if id == "" {
		// 清单没写 ID：用精灵图文件名（去掉扩展名）兜底。
		id = sanitizePetID(strings.TrimSuffix(filepath.Base(sheetRel), filepath.Ext(sheetRel)))
	}
	if id == "" {
		return PetPack{}, apperr.Validation("宠物包 ID 非法（只允许字母、数字、点、横线、下划线）")
	}
	if id == config.DefaultPetPackID {
		// 内置 ID 是保留字：导入的包若与它同名，改动配置时无法区分两者。
		id += "-imported"
	}

	name := strings.TrimSpace(manifest.DisplayName)
	if name == "" {
		name = id
	}

	dataDir, err := appdir.Dir(appName)
	if err != nil {
		return PetPack{}, apperr.Wrap(err)
	}
	packsDir := filepath.Join(dataDir, petPacksDirName)
	targetDir := filepath.Join(packsDir, id)
	if !isInside(targetDir, packsDir) {
		return PetPack{}, apperr.Validation("宠物包 ID 非法：" + id)
	}
	if mkErr := os.MkdirAll(targetDir, 0o755); mkErr != nil {
		return PetPack{}, apperr.Wrap(fmt.Errorf("创建宠物包目录失败: %w", mkErr))
	}

	// 精灵图沿用源扩展名落盘：MIME 由扩展名推导（见 spriteMIME），
	// 强行改名成 .webp 会让 data URI 的 MIME 与内容不符。
	sheetName := "spritesheet" + strings.ToLower(filepath.Ext(sheetRel))
	sourceSheet := filepath.Join(sourceDir, filepath.FromSlash(sheetRel))
	if copyErr := copyFileLimited(sourceSheet, filepath.Join(targetDir, sheetName)); copyErr != nil {
		return PetPack{}, copyErr
	}

	// 写一份归一化清单：路径统一成刚落盘的文件名，
	// 这样即使源清单里写的是 "assets/sheet.webp" 之类的相对路径，导入后也不会失效。
	imported := petManifest{
		ID:                  id,
		DisplayName:         name,
		Description:         strings.TrimSpace(manifest.Description),
		SpriteVersionNumber: manifest.SpriteVersionNumber,
		SpritesheetPath:     sheetName,
	}
	if writeErr := writePetManifest(filepath.Join(targetDir, petManifestFile), imported); writeErr != nil {
		return PetPack{}, writeErr
	}

	slog.Info("宠物包已导入", "id", id, "source", sourceDir, "sheet", sheetName)
	return PetPack{
		ID:          id,
		Name:        name,
		Description: imported.Description,
		Source:      PetSourceImported,
		SpritePath:  sheetName,
		HasSprite:   true,
	}, nil
}

// resolveImportManifest 依据用户选中的文件类型得出待导入包的清单。
func resolveImportManifest(path, baseName string) (petManifest, error) {
	if strings.EqualFold(baseName, petManifestFile) {
		data, err := os.ReadFile(path)
		if err != nil {
			return petManifest{}, apperr.Wrap(fmt.Errorf("读取 pet.json 失败: %w", err))
		}
		manifest, ok := parsePetManifest(data)
		if !ok {
			return petManifest{}, apperr.Validation("pet.json 不是合法的 JSON 对象")
		}
		return manifest, nil
	}

	if !isSpriteFileName(baseName) {
		return petManifest{}, apperr.Validation("请选择宠物包里的 pet.json 或精灵图（webp/png/jpg/gif）")
	}

	// 用户直接选了精灵图：从同一目录找清单，缺清单就用文件名兜底。
	manifest := petManifest{}
	if data, err := os.ReadFile(filepath.Join(filepath.Dir(path), petManifestFile)); err == nil {
		if parsed, ok := parsePetManifest(data); ok {
			manifest = parsed
		} else {
			slog.Warn("同目录的 pet.json 解析失败，按文件名推断宠物信息", "dir", filepath.Dir(path))
		}
	}
	manifest.SpritesheetPath = baseName
	if strings.TrimSpace(manifest.ID) == "" {
		manifest.ID = strings.TrimSuffix(baseName, filepath.Ext(baseName))
	}
	return manifest, nil
}

// isSpriteFileName 判断文件名是否是受支持的精灵图。
func isSpriteFileName(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".webp", ".png", ".jpg", ".jpeg", ".gif":
		return true
	default:
		return false
	}
}

// copyFileLimited 复制文件，并施加与读取侧一致的大小上限。
func copyFileLimited(source, target string) error {
	info, err := os.Stat(source)
	if err != nil {
		return apperr.NotFound("源文件不存在：" + filepath.Base(source))
	}
	if info.IsDir() {
		return apperr.Validation("源路径是目录")
	}
	if info.Size() > maxPetSpriteBytes {
		return apperr.Validation(fmt.Sprintf("精灵图过大（上限 %dMB）", maxPetSpriteBytes>>20))
	}

	in, err := os.Open(source)
	if err != nil {
		return apperr.Wrap(fmt.Errorf("打开源文件失败: %w", err))
	}
	defer func() {
		if closeErr := in.Close(); closeErr != nil {
			slog.Warn("关闭宠物素材源文件失败", "path", source, "err", closeErr)
		}
	}()

	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return apperr.Wrap(fmt.Errorf("写入宠物素材失败: %w", err))
	}
	if _, copyErr := io.Copy(out, io.LimitReader(in, maxPetSpriteBytes)); copyErr != nil {
		// 失败时删掉半截文件：数据目录里留一个残缺的图集，
		// 会让这个包在下次列出时「有清单、图集却是坏的」，比没有更难查。
		// 删除本身失败无副作用（下次导入会以 O_TRUNC 覆盖）。
		_ = os.Remove(target)
		if closeErr := out.Close(); closeErr != nil {
			slog.Warn("关闭宠物素材目标文件失败", "path", target, "err", closeErr)
		}
		return apperr.Wrap(fmt.Errorf("复制宠物素材失败: %w", copyErr))
	}
	if closeErr := out.Close(); closeErr != nil {
		return apperr.Wrap(fmt.Errorf("关闭宠物素材目标文件失败: %w", closeErr))
	}
	return nil
}

// writePetManifest 把清单写回磁盘（缩进 2 空格，便于用户手工查看/修改）。
func writePetManifest(path string, manifest petManifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return apperr.Wrap(fmt.Errorf("序列化宠物清单失败: %w", err))
	}
	if writeErr := os.WriteFile(path, data, 0o644); writeErr != nil {
		return apperr.Wrap(fmt.Errorf("写入宠物清单失败: %w", writeErr))
	}
	return nil
}

// ---------------------------------------------------------------------------
// 解析与归一化
// ---------------------------------------------------------------------------

// parsePetManifest 解析 pet.json。
//
// 【为什么不用 struct 直解】字段名在不同来源的包里写法不一致
// （displayName / display_name、spritesheetPath / spritesheet_path），
// 且 spriteVersionNumber 有时是字符串 "1"。先解成 map 再按键名归一，
// 才能做到「缺字段给兜底、写法不同也认得」——本函数只在 JSON 本身非法时失败。
func parsePetManifest(data []byte) (petManifest, bool) {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return petManifest{}, false
	}
	// 键名统一小写后查找：JSON 的键是大小写敏感的，而归一化才是我们想要的语义。
	fields := make(map[string]any, len(raw))
	for key, value := range raw {
		fields[strings.ToLower(key)] = value
	}

	manifest := petManifest{
		ID:              firstString(fields, "id", "petid", "pet_id"),
		DisplayName:     firstString(fields, "displayname", "display_name", "name", "title"),
		Description:     firstString(fields, "description", "desc", "summary"),
		SpritesheetPath: firstString(fields, "spritesheetpath", "spritesheet_path", "sprite", "sheet", "sheetpath"),
	}
	// 版本号单独赋值：它在不同包里可能是数字 1，也可能是字符串 "1"，
	// 故统一走 firstString → toInt 这条宽松路径。缺失即 0，不当作错误。
	manifest.SpriteVersionNumber = toInt(firstString(fields,
		"spriteversionnumber", "sprite_version_number", "version"))
	return manifest, true
}

// firstString 按候选键顺序取第一个非空字符串值。
func firstString(fields map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := fields[key]
		if !ok {
			continue
		}
		switch v := value.(type) {
		case string:
			if text := strings.TrimSpace(v); text != "" {
				return text
			}
		case float64:
			// 少数包的 id 被写成数字，转成字符串同样可用。
			return strconv.FormatFloat(v, 'f', -1, 64)
		}
	}
	return ""
}

// toInt 把宽松取值转成整数（无法解析时返回 0）。
func toInt(text string) int {
	if text == "" {
		return 0
	}
	value, err := strconv.Atoi(text)
	if err != nil {
		return 0
	}
	return value
}

// sanitizePetID 把任意字符串归一为安全的宠物包 ID。
//
// 只保留 [A-Za-z0-9._-]，其余字符替换成 '-'；并拒绝 "." / ".."（目录穿越的两种写法）。
// ID 会直接作为目录名使用，故这一步是路径安全的第一道闸。
func sanitizePetID(raw string) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(text))
	for _, r := range text {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	// 去掉首尾的点与横线：'.hidden' 与 '..' 都不可接受。
	id := strings.Trim(b.String(), ".-")
	if id == "" || id == "." || id == ".." {
		return ""
	}
	if len(id) > maxPetIDLength {
		// 归一化后全是 ASCII，按字节截断不会切坏字符。
		id = id[:maxPetIDLength]
	}
	return strings.Trim(id, ".-")
}
