package guard

import (
	"go/ast"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// 护栏 8：框架接线不得静默缺失
//
// 有些「约定」漏了不会有任何症状——不报错、不崩溃，只是某条链路默默断掉，
// 直到出问题要查日志时才发现日志里什么都没有。这类缺失只能靠检查兜住。
//
// 当前覆盖：
//   - application.Options.Logger：不接 Wails logger，框架系统日志只写 stdout，
//     打包后无人可见（详见 internal/logging/logging.go）。
//   - application.Options.MarshalError：不接则错误丢掉 code，前端按 code 分流的
//     逻辑全部退化成 internal，且不报错。
//   - pkg/services/log 服务：v3 里【前端日志的唯一通道】。不注册则前端日志只剩
//     console，而打包后的应用没有 DevTools（详见 main.go 的注册处注释）。
//
// 【v3 的额外理由】production 构建下框架的 DefaultLogger 是
// `slog.New(slog.NewTextHandler(io.Discard, nil))`（pkg/application/logger_prod.go）
// ——它把日志【直接丢掉】。即：凡是「不配就退回框架默认 logger」的地方，
// 生产产物里都是彻底静默。这让下面每一条断言都比 v2 时代更要紧。
//
// 铁律：解析到 0 个目标必须报错（护栏感知自己瞎了）。
// ---------------------------------------------------------------------------

// requiredWailsOptions 是必须出现在 application.Options 字面量里的字段及其缺失后果。
var requiredWailsOptions = []struct {
	field  string
	reason string
}{
	{
		field:  "Logger",
		reason: "不设置它，Wails 系统日志（框架内部错误、资源加载失败）只写 stdout（打包后无人可见），日志会静默丢失",
	},
	{
		field:  "MarshalError",
		reason: "不设置它，Go 侧错误丢掉 code——前端 AppError.code 恒退化为 internal，按 code 分流的逻辑静默失效",
	},
}

// TestWailsOptionsAreWired 断言 main.go 里创建应用的 application.Options 字面量设置了必需字段。
func TestWailsOptionsAreWired(t *testing.T) {
	files, err := parseDir(projectRoot())
	if err != nil {
		t.Fatalf("解析项目根目录失败: %v", err)
	}
	f, ok := files["main.go"]
	if !ok {
		t.Fatalf("护栏找不到 main.go——写法可能已变更，请同步更新护栏解析规则")
	}

	lit := findAppOptionsLiteral(f)
	if lit == nil {
		t.Fatal("护栏在 main.go 中未找到 application.Options{...} 字面量——写法可能已变更，请同步更新护栏解析规则")
	}

	present := map[string]bool{}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if ident, ok := kv.Key.(*ast.Ident); ok {
			present[ident.Name] = true
		}
	}
	if len(present) == 0 {
		t.Fatal("护栏解析到 application.Options 字面量但没有任何具名字段——写法可能已变更，请同步更新护栏解析规则")
	}

	for _, req := range requiredWailsOptions {
		if !present[req.field] {
			t.Errorf("main.go 的 application.Options 未设置 %s：%s", req.field, req.reason)
		}
	}
}

// ---------------------------------------------------------------------------
// 护栏 8b：前端日志通道必须显式注册（v3 新增的失效面）
//
// v3 把 v2 的「前端 LogXxx → WailsInvoke → Options.Logger」这条通道【整个拿掉了】：
// Options.Logger 的文档明确写「用于 Wails 系统消息，不是应用消息」，前端运行时
// (@wailsio/runtime) 里也不再有 LogDebug/LogInfo。前端日志要落进 app.log，
// 唯一的路是注册官方的 pkg/services/log 服务。
//
// 不注册的症状：应用照常跑，只是前端日志——绑定调用失败、启动时间线、全局异常上报
// ——全部只剩 console，而打包后的应用没有 DevTools，等于没记。与 Logger 字段同属
// 「没有任何症状的失效」，故放在同一个护栏文件里。
// ---------------------------------------------------------------------------

// TestFrontendLogServiceIsWired 断言 main.go 注册了官方 log 服务，且【接上了项目的 logger】。
func TestFrontendLogServiceIsWired(t *testing.T) {
	files, err := parseDir(projectRoot())
	if err != nil {
		t.Fatalf("解析项目根目录失败: %v", err)
	}
	f, ok := files["main.go"]
	if !ok {
		t.Fatal("护栏找不到 main.go——写法可能已变更，请同步更新护栏解析规则")
	}

	// 先确认 Services 字面量在（否则下面「没找到 log 服务」的结论建立在空气上）
	if findServicesLiteral(f) == nil {
		t.Fatal("护栏在 main.go 中未找到 application.Options 的 Services 字段字面量——" +
			"写法可能已变更，请同步更新护栏解析规则")
	}

	// 在 Services 里找 log 服务的构造调用。
	// 【必须限定在 Services 块内】：logservice.New() 出现在别的上下文里不算接线。
	var registered string
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
			if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "logservice" {
				registered = sel.Sel.Name
			}
		}
		return true
	})
	if registered == "" {
		t.Fatal("main.go 未注册 Wails 官方 log 服务（logservice.New*）：" +
			"v3 已删除 v2 的 runtime.LogInfo 那条前端日志通道，不注册则前端日志只剩 console，" +
			"而打包后的应用没有 DevTools——日志等于没记，且不会有任何报错。" +
			"修法：在 application.Options 的 Services 里加 " +
			"application.NewService(logservice.NewWithConfig(&logservice.Config{Logger: logging.Frontend(), LogLevel: logLevel}))")
	}

	// 【必须是 NewWithConfig，不能是 New()】：log.New() 会让服务退回框架默认 logger
	// ——production 构建下那是 io.Discard（pkg/application/logger_prod.go），
	// 前端日志被直接丢掉；dev 构建下它写 stderr，绕开项目的轮转文件与 source 标记。
	// 两种情况下「日志没进 app.log」都不会有任何报错，正是本护栏要拦的形态。
	if registered != "NewWithConfig" {
		t.Errorf("main.go 用 logservice.%s 构造日志服务，应为 NewWithConfig 并显式传入 Logger："+
			"log.New() 会退回框架默认 logger——production 构建下是 io.Discard（日志直接丢弃），"+
			"dev 构建下写 stderr（绕开 app.log 与 source=frontend 标记），两种情况都无声无息", registered)
	}

	// Logger 字段必须真的传了（光用 NewWithConfig 但 Config 里不给 Logger 同样退回默认）
	cfgLit := findCompositeLiteralIn(f, "logservice", "Config")
	if cfgLit == nil {
		t.Error("main.go 的 logservice.NewWithConfig 里找不到 &logservice.Config{...}——" +
			"不传 Config 或不设 Logger 都会退回框架默认 logger（production 下即 io.Discard）")
		return
	}
	hasLogger := false
	for _, elt := range cfgLit.Elts {
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			if ident, ok := kv.Key.(*ast.Ident); ok && ident.Name == "Logger" {
				hasLogger = true
			}
		}
	}
	if !hasLogger {
		t.Error("logservice.Config 未设置 Logger：服务会退回框架默认 logger，" +
			"production 构建下即 io.Discard——前端日志被静默丢弃")
	}
}

// findServicesLiteral 在 main.go 里定位 application.Options 的 Services 字段取值字面量。
func findServicesLiteral(f *ast.File) *ast.CompositeLit {
	opts := findAppOptionsLiteral(f)
	if opts == nil {
		return nil
	}
	for _, elt := range opts.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if ident, ok := kv.Key.(*ast.Ident); ok && ident.Name == "Services" {
			if cl, ok := kv.Value.(*ast.CompositeLit); ok {
				return cl
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// 护栏 9：构建期注入目标必须真实存在
//
// 链接器的 `-X pkg.Symbol=value` 对【不存在的符号是静默忽略】的（已实测：
// 写错符号名时构建成功、无任何警告，变量保持零值，版本悄悄退回 "dev"）。
// 故打包脚本里的注入目标必须由护栏核对，否则改了变量名不会有任何症状，
// 只是发布的版本号从此永远是 "dev"。
// ---------------------------------------------------------------------------

// injectionTargetRe 匹配脚本里的 -X 注入：`-X <module>/internal/appinfo.<Symbol>=`
var injectionTargetRe = regexp.MustCompile(`-X\s+\S*internal/appinfo\.([A-Za-z0-9_]+)=`)

// TestVersionInjectionTargetsExist 断言构建文件注入的符号在 appinfo 包真实存在。
//
// 注入点的位置在 v3 变了，护栏必须跟着走：
//   - v2：`wails build -ldflags ...`，-X 字面量写在 scripts/build.sh 里。
//   - v3：`wails3 build` 【不转发 -ldflags】（已实测），版本只能经 Taskfile 变量传：
//     scripts/build.sh → `wails3 build VERSION=x.y.z COMMIT=abc`
//     → 根 Taskfile.yml 的 VERSION_LDFLAGS 拼出 -X 字面量
//     → 各平台 Taskfile 把它拼进 BUILD_FLAGS
//     故 -X 字面量现在在 Taskfile.yml。若哪天链路又变，本护栏会因为
//     「一处也没解析到」而 Fatal——不会静默放行。
//
// 版本号单一真相（build/config.yml 的 info.version）另由护栏 10/10b 覆盖。
func TestVersionInjectionTargetsExist(t *testing.T) {
	scripts := []string{"Taskfile.yml", "scripts/build.sh"}

	// 收集 internal/appinfo 里的包级变量名（注入目标必须是可寻址的字符串变量）
	dir := filepath.Join(projectRoot(), "internal", "appinfo")
	files, err := parseDir(dir)
	if err != nil {
		t.Fatalf("解析 internal/appinfo 失败: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("护栏在 %s 未解析到任何 .go 文件——写法可能已变更，请同步更新护栏解析规则", dir)
	}
	vars := map[string]bool{}
	for _, f := range files {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok.String() != "var" {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, n := range vs.Names {
					vars[n.Name] = true
				}
			}
		}
	}
	if len(vars) == 0 {
		t.Fatal("护栏在 internal/appinfo 未解析到任何包级变量——写法可能已变更，请同步更新护栏解析规则")
	}

	checked := 0
	for _, rel := range scripts {
		data, err := os.ReadFile(filepath.Join(projectRoot(), rel))
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", rel, err)
		}
		for _, m := range injectionTargetRe.FindAllStringSubmatch(string(data), -1) {
			checked++
			if !vars[m[1]] {
				t.Errorf("%s 注入 %s，但 internal/appinfo 中无此包级变量——"+
					"链接器会静默忽略，版本将永远退回 dev", rel, m[1])
			}
		}
	}
	if checked == 0 {
		t.Fatalf("护栏未在任何打包脚本中解析到 -X internal/appinfo.* 注入——"+
			"注入被移除或写法已变更，请同步更新护栏解析规则（%v）", scripts)
	}
}

// ---------------------------------------------------------------------------
// 护栏 10：版本号与 bundle id 的单一真相不得失守
//
// v3 把这两个字段的载具从 wails.json（JSON）换成了 build/config.yml（YAML），
// 而且 v3 **不会自动渲染**——必须显式跑
//   wails3 task common:update:build-assets
// 才把 info 段写进 build/darwin/Info.plist 与 build/windows/info.json。
//
// 于是失效方式比 v2 多了一种：
//   - config.yml 缺 info.version → 渲染出空值/旧值，系统「显示简介」与产物文件名对不上；
//   - 改了 config.yml 却忘了跑 update:build-assets → Info.plist 停在旧值。
//     【这是 v3 新增的坑】，且不报任何错——正是本仓最不能接受的那类静默漂移。
//     故下面除了查 config.yml，还回读 Info.plist 与它逐字段对账（见护栏 10b）。
//   - productIdentifier 沿用模板默认值 → 宣称 Wails 是厂商，且与其他项目撞 bundle id。
// ---------------------------------------------------------------------------

// buildConfigInfo 是 build/config.yml 的 info 段（只需护栏关心的三个字段）。
type buildConfigInfo struct {
	version           string
	productName       string
	productIdentifier string
}

// parseBuildConfigInfo 解析 build/config.yml 的 info 段。
//
// 手写行级解析而非引 YAML 库：格式由本仓自己的 config.yml 决定，且脆弱性由
// 「解析不到即 Fatal」兜住（与 deps.yaml 的护栏同一套路）。只认 info: 段内的键，
// 避免撞上 dev_mode 段里的同名键。
func parseBuildConfigInfo(t *testing.T, path string) buildConfigInfo {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 build/config.yml 失败: %v", err)
	}

	var info buildConfigInfo
	inInfo := false
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// 顶格（无缩进）的 `key:` 是段边界；info 段在其后、dev_mode 段之前
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			inInfo = strings.HasPrefix(trimmed, "info:")
			continue
		}
		if !inInfo {
			continue
		}
		switch {
		case strings.HasPrefix(trimmed, "version:"):
			info.version = yamlValue(trimmed, "version:")
		case strings.HasPrefix(trimmed, "productName:"):
			info.productName = yamlValue(trimmed, "productName:")
		case strings.HasPrefix(trimmed, "productIdentifier:"):
			info.productIdentifier = yamlValue(trimmed, "productIdentifier:")
		}
	}
	if info.version == "" && info.productName == "" && info.productIdentifier == "" {
		t.Fatalf("护栏在 %s 的 info 段解析到 0 个字段——格式可能已变更，请同步更新护栏解析规则", path)
	}
	return info
}

// yamlValue 取 `key: value` 的 value，去掉 YAML 引号与行尾注释。
func yamlValue(line, key string) string {
	rest := strings.TrimSpace(strings.TrimPrefix(line, key))
	// 去掉行尾注释（`value # comment`）。值本身不含 #，故直接切第一个 #。
	if i := strings.Index(rest, " #"); i >= 0 {
		rest = rest[:i]
	}
	return strings.TrimSpace(strings.Trim(rest, `"'`))
}

// numericVersionRe 匹配数字点分版本（如 0.1.0 / 1.2.3.4）。
var numericVersionRe = regexp.MustCompile(`^[0-9]+(\.[0-9]+){2,3}$`)

// TestBuildConfigHasVersionSourceOfTruth 断言 build/config.yml 声明了可供产物渲染的版本与产品名。
func TestBuildConfigHasVersionSourceOfTruth(t *testing.T) {
	info := parseBuildConfigInfo(t, filepath.Join(projectRoot(), "build", "config.yml"))

	if info.version == "" {
		t.Error("build/config.yml 未声明 info.version：v3 的版本号单一真相处缺失，" +
			"update:build-assets 会渲染出空版本，产物里的版本从此与代码无关")
	} else if !numericVersionRe.MatchString(info.version) {
		t.Errorf("build/config.yml 的 info.version = %q 不是数字点分格式："+
			"NSIS 的 VIProductVersion 要求数字版本，Windows 打包会在最后一步失败",
			info.version)
	}
	if info.productName == "" {
		t.Error("build/config.yml 未声明 info.productName：渲染出的 Info.plist 里 CFBundleName 会为空")
	}
}

// ---------------------------------------------------------------------------
// 护栏 10b：渲染产物必须与 config.yml 一致（v3 新增）
//
// v3 的 build/darwin/Info.plist 是【生成物】——它不是真相，是 config.yml 的渲染结果，
// 且只在显式跑 update:build-assets 时才刷新。
//
// 这让「改了 config.yml 忘了渲染」成为一类新的静默漂移：应用内版本（-ldflags 注入）
// 是新的，系统「显示简介」是旧的，两边都"有值"、都不报错。
// 本护栏把两者逐字段对账，把这个坑变成会红的检查。
// ---------------------------------------------------------------------------

// plistStringValue 取 Info.plist 中某个 key 对应的 <string> 取值。
func plistStringValue(t *testing.T, content, key string) string {
	t.Helper()
	idx := strings.Index(content, "<key>"+key+"</key>")
	if idx < 0 {
		t.Fatalf("Info.plist 中找不到 <key>%s</key>——写法可能已变更，请同步更新护栏解析规则", key)
	}
	rest := content[idx:]
	start := strings.Index(rest, "<string>")
	if start < 0 {
		t.Fatalf("Info.plist 的 %s 之后没有 <string> 取值——写法可能已变更，请同步更新护栏解析规则", key)
	}
	rest = rest[start+len("<string>"):]
	end := strings.Index(rest, "</string>")
	if end < 0 {
		t.Fatalf("Info.plist 的 %s 取值缺 </string>——写法可能已变更，请同步更新护栏解析规则", key)
	}
	return strings.TrimSpace(rest[:end])
}

// TestBuildAssetsMatchConfig 断言 build/darwin/Info.plist 与 build/config.yml 逐字段一致。
func TestBuildAssetsMatchConfig(t *testing.T) {
	info := parseBuildConfigInfo(t, filepath.Join(projectRoot(), "build", "config.yml"))
	plistPath := filepath.Join(projectRoot(), "build", "darwin", "Info.plist")
	data, err := os.ReadFile(plistPath)
	if err != nil {
		t.Fatalf("读取 build/darwin/Info.plist 失败: %v", err)
	}
	content := string(data)

	pairs := []struct {
		plistKey string
		want     string
		source   string
	}{
		{"CFBundleShortVersionString", info.version, "info.version"},
		{"CFBundleVersion", info.version, "info.version"},
		{"CFBundleIdentifier", info.productIdentifier, "info.productIdentifier"},
		// 名字类键也一并核对：CFBundleName 直接来自 info.productName；
		// CFBundleExecutable 来自 update:build-assets 的 -binaryname（即 Taskfile 的
		// APP_NAME），而护栏 13 已要求 APP_NAME == info.productName，故这里可同源比对。
		// 加上这两条后，「访达里的 App 名 / 关于界面里的名字 / 可执行文件路径」
		// 三者不一致时会在这里变红，而不是等用户看到怪名字。
		{"CFBundleName", info.productName, "info.productName"},
		{"CFBundleExecutable", info.productName, "info.productName（经 Taskfile 的 APP_NAME）"},
	}
	for _, p := range pairs {
		if p.want == "" {
			continue // 缺失已在 10 号护栏报过，这里不重复
		}
		got := plistStringValue(t, content, p.plistKey)
		if got != p.want {
			t.Errorf("Info.plist 的 %s = %q，但 build/config.yml 的 %s = %q —— 构建资源未同步。"+
				"v3 不会自动渲染，改完 config.yml 必须跑：wails3 task common:update:build-assets"+
				"（漏掉的症状：应用内显示新版本、系统「显示简介」显示旧版本）",
				p.plistKey, got, p.source, p.want)
		}
	}
}

// TestBundleIDIsProjectControlled 断言 bundle id 不沿用 Wails 模板的默认值。
func TestBundleIDIsProjectControlled(t *testing.T) {
	info := parseBuildConfigInfo(t, filepath.Join(projectRoot(), "build", "config.yml"))
	id := info.productIdentifier
	if id == "" {
		t.Fatal("build/config.yml 的 info.productIdentifier 为空——macOS bundle id 会退化成空值，" +
			"登录项/权限授予/文件关联都会记在一个无名标识上")
	}

	// v2 时代模板靠 Info.plist 里的 {{safeBundleID .Name}} 派生，v3 改成 config.yml 里的字面量。
	// 于是「沿用模板默认值」的判据也跟着换：不再是找模板标记，而是禁掉模板的出厂值。
	if strings.Contains(id, "com.wails.") {
		t.Errorf("info.productIdentifier = %q 仍是 Wails 的 com.wails.* 前缀——"+
			"等于把 Wails 当厂商，且与其他 Wails 项目共享 bundle id 命名空间", id)
	}
	switch id {
	case "com.mycompany.myproduct":
		t.Errorf("info.productIdentifier = %q 是 v3 模板的出厂默认值——"+
			"多个产品会撞同一个 bundle id。应改成你的公司域名（如 com.example.<app>，"+
			"由 scripts/init.sh 的 bundle-prefix 参数生成）", id)
	}
}

// ---------------------------------------------------------------------------
// 护栏 11：可观测性接线不得静默缺失
//
// 这类失效的共同点：删掉不会报错、不会崩溃，只是**失去可见性**——
// 慢查询回到无人可见的 stdout、启动优化重新变成盲调。症状要等到真出问题时才显形，
// 而那时正是最需要日志的时候。故只能靠检查兜住。
// ---------------------------------------------------------------------------

// TestGormLoggerIsWired 断言数据库连接配置了日志实现。
func TestGormLoggerIsWired(t *testing.T) {
	files, err := parseDir(filepath.Join(projectRoot(), "internal", "database"))
	if err != nil {
		t.Fatalf("解析 internal/database 失败: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("护栏在 internal/database 未解析到任何 .go 文件——写法可能已变更，请同步更新护栏解析规则")
	}

	lit := findCompositeLiteral(files, "gorm", "Config")
	if lit == nil {
		t.Fatal("护栏未找到 gorm.Config{...} 字面量——写法可能已变更，请同步更新护栏解析规则")
	}

	for _, elt := range lit.Elts {
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			if ident, ok := kv.Key.(*ast.Ident); ok && ident.Name == "Logger" {
				return // 已接线
			}
		}
	}
	t.Error("gorm.Config 未设置 Logger：gorm 默认 logger 写 stdout，" +
		"打包后的 GUI 应用无人可见，慢查询与 SQL 错误会静默消失")
}

// TestStartupTimelineStagesExist 断言组合根的启动阶段打点覆盖关键阶段。
//
// 删掉打点不会有任何症状，只是启动优化重新变成盲调（此前只有首尾两点，
// 实测 359–733ms 的中间部分是黑盒）。
func TestStartupTimelineStagesExist(t *testing.T) {
	files, err := parseDir(projectRoot())
	if err != nil {
		t.Fatalf("解析项目根目录失败: %v", err)
	}
	f, ok := files["main.go"]
	if !ok {
		t.Fatal("护栏找不到 main.go——写法可能已变更，请同步更新护栏解析规则")
	}

	// 期望覆盖的阶段（改名即失败，提示同步护栏）
	want := []string{"日志初始化", "数据库初始化", "配置加载与组装"}
	found := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "stage" {
			return true
		}
		if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
			if name, uerr := strconv.Unquote(lit.Value); uerr == nil {
				found[name] = true
			}
		}
		return true
	})

	if len(found) == 0 {
		t.Fatal("护栏在 main.go 未解析到任何启动阶段打点——写法可能已变更，请同步更新护栏解析规则")
	}
	for _, w := range want {
		if !found[w] {
			t.Errorf("启动时间线缺少阶段 %q：删掉打点不会有任何症状，"+
				"只会让启动优化重新变成盲调（现有阶段: %v）", w, found)
		}
	}
}

// findCompositeLiteral 在给定文件集合中定位 `pkg.Type{...}` 形式的复合字面量。
func findCompositeLiteral(files map[string]*ast.File, pkg, typeName string) *ast.CompositeLit {
	var found *ast.CompositeLit
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			if found != nil {
				return false
			}
			cl, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			sel, ok := cl.Type.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			ident, ok := sel.X.(*ast.Ident)
			if !ok || ident.Name != pkg || sel.Sel.Name != typeName {
				return true
			}
			found = cl
			return false
		})
	}
	return found
}

// findAppOptionsLiteral 在 main.go 中定位 `application.Options{...}`（application.New 的参数）。
//
// v2 时这里找的是 `&options.App{...}`。写法变了，护栏锚点必须跟着变——
// 否则本护栏会以「找不到字面量」的 Fatal 报出来（而不会静默放行）。
func findAppOptionsLiteral(f *ast.File) *ast.CompositeLit {
	var found *ast.CompositeLit
	ast.Inspect(f, func(n ast.Node) bool {
		if found != nil {
			return false
		}
		cl, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		// 形如 application.Options{...}
		sel, ok := cl.Type.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		if pkg.Name == "application" && sel.Sel.Name == "Options" {
			found = cl
			return false
		}
		return true
	})
	return found
}

// findCompositeLiteralIn 在单个文件里定位 `pkg.Type{...}` 形式的复合字面量。
// 与 findCompositeLiteral 的区别：后者跨文件集合，本函数限定在一个文件内——
// 用于「这个字面量必须出现在 main.go 的这条调用里」这类断言。
func findCompositeLiteralIn(f *ast.File, pkg, typeName string) *ast.CompositeLit {
	var found *ast.CompositeLit
	ast.Inspect(f, func(n ast.Node) bool {
		if found != nil {
			return false
		}
		cl, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		sel, ok := cl.Type.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok || ident.Name != pkg || sel.Sel.Name != typeName {
			return true
		}
		found = cl
		return false
	})
	return found
}

// ---------------------------------------------------------------------------
// 护栏 12：mcp 标签不得进入默认构建（v3 新增，且是本仓最要紧的一条）
//
// `-tags mcp` 打开的是 Wails 运行时的 MCP 服务：它在本机监听一个 HTTP 端口，
// 允许外部进程 eval 页面里的 JS、调用任意绑定方法。这是本仓要的「AI 基座」能力，
// 但它【绝不能默认开着】。
//
// 破坏这条的症状几乎为零：应用照常启动、功能照常可用，只是多开了一个能执行 JS、
// 调 Go 方法的本机端口，界面上没有任何提示。属于「没有症状的失效」，只能靠护栏拦。
//
// 唯一受认可的启用方式是环境变量 WAILS_MCP=1（见 wails/v3 的 internal/commands/envTags）。
// 注意这条通道的【边界】（v3.0.0 实测）：只有 `wails3 build`（task_wrapper.go 的
// Build()）与 `wails3 dev`（dev.go）读它；`wails3 package` 不读——Package() 只是
// wrapTask("package", …)，不调用 envTags()。故 `WAILS_MCP=1 make package`/`make build`
// 在 macOS（交付路径走 package）上会【静默】产出不含 MCP 的产物。见 design.md P13。
//
// 故本护栏的判据是：
//   - 任何构建文件里都不得直接把 mcp 写进构建标签；
//   - 且 WAILS_MCP 这条显式通道必须真实存在——否则说明「怎么开 MCP」已经无据可依，
//     护栏本身也就瞎了一半（找不到通道 ≠ 通道安全）。
//
// 【三层覆盖，分辨率不同，都要】本条是源码层（没人主动加）；
// 护栏 17 是机制层（默认标签下 application 包不含 mcp_enabled.go）；
// scripts/smoke.sh 第 6 步是产物层（真二进制里没有 MCP 常量）。
// ---------------------------------------------------------------------------

// mcpLeakRe 匹配构建文件里把 mcp 当标签用的地方：`-tags a,mcp` 与 `EXTRA_TAGS: mcp` 两种形态。
var mcpLeakRe = regexp.MustCompile(`(?i)(-tags|extra_tags)[^\n]*\bmcp\b`)

// TestMCPTagNotInDefaultBuild 断言 mcp 标签没有渗进任何构建路径。
func TestMCPTagNotInDefaultBuild(t *testing.T) {
	root := projectRoot()
	// 清单刻意写全：这些是「能让 mcp 标签跑到产物里」的所有入口。
	// 文件被移动/改名会 Fatal，不会静默少扫一个。
	files := []string{
		"Makefile",
		"Taskfile.yml",
		"build/Taskfile.yml",
		"build/darwin/Taskfile.yml",
		"build/windows/Taskfile.yml",
		"build/linux/Taskfile.yml",
		"scripts/build.sh",
		"scripts/package.sh",
		"scripts/release.sh",
		"scripts/smoke.sh",
		".github/workflows/build.yml",
		".github/workflows/release.yml",
	}

	mentionsChannel := false
	for _, rel := range files {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("读取 %s 失败: %v —— 文件被移动或改名？本护栏的文件清单需要同步", rel, err)
		}
		content := string(data)
		for i, line := range strings.Split(content, "\n") {
			if mcpLeakRe.MatchString(line) {
				t.Errorf("%s:%d 把 mcp 写进了构建标签：%s\n"+
					"  mcp 标签会打开本机的 MCP 服务（可 eval 页面 JS、调任意绑定方法），"+
					"绝不能进默认构建或任何发布路径。唯一受认可的启用方式是 WAILS_MCP=1。",
					rel, i+1, strings.TrimSpace(line))
			}
		}
		if strings.Contains(content, "WAILS_MCP") {
			mentionsChannel = true
		}
	}
	if !mentionsChannel {
		t.Error("没有任何构建文件提到 WAILS_MCP：它是 mcp 标签唯一受认可的启用通道，" +
			"不见了就说明「怎么开 MCP」已无据可依（或本护栏的文件清单漏了地方）")
	}
}

// ---------------------------------------------------------------------------
// 护栏 13：Taskfile 变量必须与真相文件一致（v3 新增）
//
// v3 把版本注入的载体搬进了 Taskfile.yml：根 Taskfile 的 VERSION_LDFLAGS 拼出
// `-X {{.MODULE}}/internal/appinfo.*`，各平台 BUILD_FLAGS 再把它拼进 -ldflags。
// 于是 Taskfile 里多了两个【必须与别处一致】的字面量，而两处漂移都【没有任何症状】：
//
//	MODULE 漂移（与 go.mod 的 module 不一致）
//	  → -X 指向一个不存在的导入路径。链接器对不存在的 -X 符号是静默忽略的：
//	    构建照常成功、无任何告警，只是 InjectedVersion 永远为空串，
//	    应用内版本退回 "dev"。症状是「发布的版本号与用户报障时看到的版本号无关」。
//
//	APP_NAME 漂移（与 build/config.yml 的 info.productName 不一致）
//	  → 产物叫 build/bin/<APP_NAME>.*，而 Info.plist 的 CFBundleName 是 productName。
//	    macOS 上「访达里看到的 App 名」与「关于界面里的名字」会不一样；
//	    scripts/build.sh 回读产物时还会指到一个不存在的路径。
//
// 之所以只能靠护栏：Taskfile 读不了 build/config.yml（它不把 YAML 当数据源），
// 所以「一个字面量两处写法」是结构性的，一致性没有别的保证方式。
// ---------------------------------------------------------------------------

// taskfileVars 解析 Taskfile.yml 顶层 vars 段，返回变量名→字面量值。
//
// 只认【恰好两个空格】缩进的键：更深缩进的是嵌套结构（deps 列表等）而非顶层变量，
// 误认会让断言建立在误解上。含 {{...}} 模板表达式的值被跳过——它们不是字面量，
// 无法在此比对（且断言模板表达式本身没有意义，链接器看的是展开后的结果）。
func taskfileVars(t *testing.T, path string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", path, err)
	}
	vars := map[string]string{}
	inVars := false
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		// 空行与整行注释先跳过：它们【不是】段边界。
		// （踩过的坑：把空行当成顶格行会让 inVars 中途被重置，于是 vars 段里
		// 被空行/注释块分隔开的变量全都读不到——且「读到了 2 个」不会触发
		// 下面的 0 检查，属于本护栏内部最隐蔽的一种瞎法。）
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// 顶格行是段边界：vars 段到下一个顶格键为止
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			inVars = trimmed == "vars:"
			continue
		}
		if !inVars || !strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "   ") {
			continue
		}
		rest := strings.TrimSpace(line[2:])
		idx := strings.Index(rest, ":")
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(rest[:idx])
		val := yamlValue(rest, key+":")
		if val == "" || strings.Contains(val, "{{") {
			continue
		}
		vars[key] = val
	}
	if len(vars) == 0 {
		t.Fatalf("护栏在 %s 的 vars 段解析到 0 个字面量变量——写法可能已变更，请同步更新护栏解析规则", path)
	}
	return vars
}

// goModulePath 从 go.mod 取 module 路径。
func goModulePath(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("读取 go.mod 失败: %v", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	t.Fatal("go.mod 中找不到 module 行——写法可能已变更，请同步更新护栏解析规则")
	return ""
}

// TestTaskfileModuleMatchesGoMod 断言 Taskfile.yml 的 MODULE 与 go.mod 的 module 一致。
func TestTaskfileModuleMatchesGoMod(t *testing.T) {
	root := projectRoot()
	mod, ok := taskfileVars(t, filepath.Join(root, "Taskfile.yml"))["MODULE"]
	if !ok {
		t.Fatal("Taskfile.yml 的 vars 段没有 MODULE：版本注入的 -X <MODULE>/internal/appinfo.* " +
			"拼不出正确路径，链接器会静默忽略它，应用内版本永远退回 \"dev\"")
	}
	want := goModulePath(t, root)
	if mod != want {
		t.Errorf("Taskfile.yml 的 MODULE = %q，但 go.mod 的 module = %q —— 版本注入会打到不存在的路径。\n"+
			"  链接器对不存在的 -X 符号【静默忽略】：构建成功、无任何告警，只是应用内版本恒为 \"dev\"。"+
			"scripts/build.sh 会在产物里回读版本号（能拦住发布路径），本护栏拦得更早。", mod, want)
	}
}

// TestTaskfileAppNameMatchesConfig 断言 Taskfile.yml 的 APP_NAME 与 build/config.yml 的 info.productName 一致。
func TestTaskfileAppNameMatchesConfig(t *testing.T) {
	root := projectRoot()
	name, ok := taskfileVars(t, filepath.Join(root, "Taskfile.yml"))["APP_NAME"]
	if !ok {
		t.Fatal("Taskfile.yml 的 vars 段没有 APP_NAME：产物名与 .app 目录名都靠它，" +
			"缺失会产出以空名/模板默认名命名的产物")
	}
	info := parseBuildConfigInfo(t, filepath.Join(root, "build", "config.yml"))
	if info.productName == "" {
		t.Fatal("build/config.yml 的 info.productName 为空，无法比对（护栏 10 应已报过）")
	}
	if name != info.productName {
		t.Errorf("Taskfile.yml 的 APP_NAME = %q，但 build/config.yml 的 info.productName = %q —— 两者必须一致。\n"+
			"  漂移后果：产物叫 build/bin/%s.*，而渲染出的 Info.plist 里 CFBundleName 是 %s，"+
			"macOS 上「访达里的 App 名」与「关于界面里的名字」对不上；"+
			"scripts/build.sh 回读产物时还会指到不存在的路径。\n"+
			"  Taskfile 读不了 config.yml，故这条一致性只能靠本护栏保证。",
			name, info.productName, name, info.productName)
	}
}

// ---------------------------------------------------------------------------
// 护栏 14：build 任务的产物路径必须由 OUTPUT 决定（v3 新增）
//
// v3 官方模板里，每个平台的 build 任务都用 `-o {{.OUTPUT}}`，而 OUTPUT 自己是
// `{{.OUTPUT | default .DEFAULT_OUTPUT}}`——即「调用方传了就用调用方的，没传就用默认」。
// 这不是风格问题，是 build:universal 能工作的前提：
//
//	build:universal 给两次 build 传不同的 OUTPUT（-amd64 / -arm64），
//	产出的两个中间产物再交给 lipo 合并。
//
// 【本仓真的断过】v2 → v3 迁移时照抄平台 Taskfile，把 -o 写死成
// `{{.BIN_DIR}}/{{.APP_NAME}}`、顺手丢了 OUTPUT 变量。后果：
//   - darwin：两次编译覆盖同一个文件，lipo 报 "can't open input file"。
//     这条还算走运——它是【会出声】的失败。
//   - linux / windows：压根没有 build:universal 调用者，写死 OUTPUT 的后果
//     是「任何传 OUTPUT 的调用方静默拿到写死路径的产物」。没有报错、没有崩溃，
//     只是产物落在错误的地方。属于本仓最不能接受的那类静默失效。
//
// 故这里对三个平台一律断言「编译命令写 .OUTPUT」，而不是只盯 darwin——
// 否则「哪个平台的 build 认 OUTPUT」会变成一个要靠记忆的例外。
// ---------------------------------------------------------------------------

// buildOutputFlagRe 匹配编译命令里的 `-o {{.OUTPUT}}`（引号可有可无）。
var buildOutputFlagRe = regexp.MustCompile(`-o\s+"?\{\{\s*\.OUTPUT\s*\}\}"?`)

// buildOutputVarRe 匹配 OUTPUT 的「可覆盖 + 默认兜底」定义。
var buildOutputVarRe = regexp.MustCompile(`(?m)^\s+OUTPUT:\s*'?\{\{\s*\.OUTPUT\s*\|\s*default\s+\.DEFAULT_OUTPUT\s*\}\}`)

// buildDefaultOutputVarRe 匹配 DEFAULT_OUTPUT 的定义（值不重要，有即可）。
var buildDefaultOutputVarRe = regexp.MustCompile(`(?m)^\s+DEFAULT_OUTPUT:\s*\S`)

// taskBlock 取出平台 Taskfile 中某个任务的原文片段（从 `  <name>:` 到下一个同级键）。
//
// 只认【恰好两个空格】缩进的键：本仓 Taskfile 的顶层任务就是 2 空格，
// 任务内部（deps/cmds/env/vars）一律 4 空格起。列表项（trim 后以 - 开头）不算任务头，
// 否则 `  - task: build` 会被误判成块边界，解析结果会半途截断——
// 而截断后「块里没有 .OUTPUT」的结论是假阳性，比没护栏更糟。
func taskBlock(t *testing.T, path, name string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", path, err)
	}
	lines := strings.Split(string(data), "\n")

	start := -1
	for i, line := range lines {
		if line == "  "+name+":" {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("%s 中找不到任务 %q —— 任务被改名/删除，或缩进不再是 2 空格。"+
			"护栏已瞎，请同步更新解析规则", path, name)
	}

	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		l := lines[i]
		if l == "" {
			continue
		}
		if !strings.HasPrefix(l, "  ") || strings.HasPrefix(l, "   ") {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(l), "-") {
			continue // 列表项，不是任务头
		}
		end = i
		break
	}
	return strings.Join(lines[start:end], "\n")
}

// TestBuildTaskHonorsOutputVar 断言三个平台的 build 任务都把产物路径交给 OUTPUT。
func TestBuildTaskHonorsOutputVar(t *testing.T) {
	root := projectRoot()

	// 清单写全三个平台：漏一个就是「某平台的 build 不认 OUTPUT」这种隐秘例外
	platforms := []struct {
		rel string
	}{
		{"build/darwin/Taskfile.yml"},
		{"build/linux/Taskfile.yml"},
		{"build/windows/Taskfile.yml"},
	}

	for _, p := range platforms {
		path := filepath.Join(root, filepath.FromSlash(p.rel))
		block := taskBlock(t, path, "build")

		if !buildOutputFlagRe.MatchString(block) {
			t.Errorf("%s 的 build 任务没有用 -o \"{{.OUTPUT}}\" 指定产物路径。\n"+
				"  写死 -o 的后果：build:universal 给两次 build 传的 -amd64/-arm64 "+
				"会被忽略，两次编译覆盖同一个文件——darwin 上 lipo 随即报 "+
				"\"can't open input file\"；没有 universal 调用者的平台上则是"+
				"「传了 OUTPUT 却静默落在别处」。", p.rel)
		}
		if !buildOutputVarRe.MatchString(block) {
			t.Errorf("%s 的 build 任务缺少 OUTPUT: '{{.OUTPUT | default .DEFAULT_OUTPUT}}'。\n"+
				"  必须是「调用方给了就用、没给就兜底」的形式：只写 DEFAULT_OUTPUT "+
				"无法被 build:universal 覆盖，只写 OUTPUT 则普通调用会拿到空路径。", p.rel)
		}
		if !buildDefaultOutputVarRe.MatchString(block) {
			t.Errorf("%s 的 build 任务缺少 DEFAULT_OUTPUT：普通调用（package / run / "+
				"scripts/build.sh）不传 OUTPUT，没有默认值就会写出空路径的产物", p.rel)
		}
	}

	// darwin 额外一条：build:universal 的 lipo 输入必须正是各架构的 OUTPUT，
	// 且合并结果必须落回 DEFAULT_OUTPUT 指向的路径——否则随后的 create:app:bundle
	// 拷的是一个不存在的文件（还是那句：同一套路径约定互相咬合，不能各写各的）。
	universal := taskBlock(t, filepath.Join(root, "build", "darwin", "Taskfile.yml"), "build:universal")
	for _, arch := range []string{"amd64", "arm64"} {
		want := "OUTPUT: \"{{.BIN_DIR}}/{{.APP_NAME}}-" + arch + "\""
		if !strings.Contains(universal, want) {
			t.Errorf("build/darwin/Taskfile.yml 的 build:universal 未给 %s 架构传 %s ——\n"+
				"  两次 build 会产出同一个文件名，lipo 找不到输入。", arch, want)
		}
		if !strings.Contains(universal, "{{.BIN_DIR}}/{{.APP_NAME}}-"+arch) {
			t.Errorf("build:universal 的 lipo 命令未引用 %s 架构的中间产物路径", arch)
		}
	}
	if !strings.Contains(universal, `lipo -create`) {
		t.Fatal("build/darwin/Taskfile.yml 的 build:universal 里找不到 lipo -create —— " +
			"护栏已瞎，请同步更新解析规则")
	}
}

// ---------------------------------------------------------------------------
// 护栏 15：前端 dev server 必须监听 IPv4 回环（v3 新增）
//
// `make dev` 的页面内容不是从二进制里的 frontend/dist 来的，而是走 wails3 的
// dev 代理转发到 Vite dev server（FRONTEND_DEVSERVER_URL）。那次转发有两个端点：
//
//	Go 默认 http.Client（preRun 的健康检查）—— 按 localhost 逐个地址试，::1 通就成功
//	代理自带的 transport             —— internal/assetserver/build_dev.go 硬编码
//	                                   dialer.DialContext(ctx, "tcp4", addr)
//
// 因此只要 Vite 只监听 ::1，代理就必然 connection refused、回给 webview 一个 502，
// 页面纯空白；而健康检查照常打印 "Connected to frontend dev server!"（它是真的通了），
// `wails3 dev` 也照常退出码 0。
//
// 【这就是本条护栏存在的理由】症状（空白页）与原因（协议族不匹配）相隔极远，
// 且链路两端都在报「成功」。本仓真的这么空转过一整个开发态。
//
// Vite 的 server.host 默认是 'localhost'，在 macOS 上解析成 ::1 —— 也就是说
// 这个默认值本身就是错的。故这里断言它不是默认值，而是钉死的 IPv4 回环。
//
// 【为什么只做字面量断言，不解析 server 块结构】要判定的只有「host 是 IPv4 回环」
// 一件事；按大括号配对解析会被注释里的大括号带偏（截断后的结论是假阳性，
// 比没有护栏更糟）。故退而求其次：锚点用 server: {，命中 0 处即 Fatal（感知自己瞎了）。
// ---------------------------------------------------------------------------

// viteServerBlockRe 匹配 Vite 配置里 server 块的起始行。
var viteServerBlockRe = regexp.MustCompile(`(?m)^\s*server:\s*\{`)

// viteLoopbackHostRe 匹配 server.host 被设成 IPv4 回环（单双引号均可）。
var viteLoopbackHostRe = regexp.MustCompile(`(?m)^\s*host:\s*['"]127\.0\.0\.1['"]`)

// TestViteDevServerBindsIPv4Loopback 断言前端 dev server 绑在 IPv4 回环上。
func TestViteDevServerBindsIPv4Loopback(t *testing.T) {
	root := projectRoot()
	path := filepath.Join(root, "frontend", "vite.config.ts")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v —— 前端配置被改名/移动，护栏已瞎，请同步更新路径", path, err)
	}
	cfg := string(data)

	// 先确认锚点还在。缺了它就没法判断下面那条是不是「真的在 server 块里」。
	if !viteServerBlockRe.MatchString(cfg) {
		t.Fatal("frontend/vite.config.ts 里找不到 `server: {` —— 配置写法可能已变更，" +
			"请同步更新护栏 15 的解析规则（本护栏不能在解析不到时静默放行）")
	}
	if !viteLoopbackHostRe.MatchString(cfg) {
		t.Errorf("frontend/vite.config.ts 的 server.host 没有显式设成 '127.0.0.1'。\n" +
			"  默认值 'localhost' 在 macOS 上只绑 [::1]（IPv6 回环），而 wails3 的 dev 代理" +
			"硬编码强制 IPv4（internal/assetserver/build_dev.go 的 dialer.DialContext(ctx, \"tcp4\", addr)）。\n" +
			"  后果：`make dev` 的窗口一片空白（代理拿到 connection refused → 502），" +
			"但 `wails3 dev` 退出码 0、终端还打印 \"Connected to frontend dev server!\"。\n" +
			"  修法就是写回 host: '127.0.0.1'。")
	}
}

// ---------------------------------------------------------------------------
// 护栏 16：production 构建标签必须真的到达构建命令（v3 新增）
//
// 【为什么这条在 v3 才成立】v2 的 `wails build` 自己会加 production 构建标签。v3 的
// `wails3 build` 只是 Taskfile 的薄封装，标签改由【本仓自己的】BUILD_FLAGS 在
// DEV!=true 分支里加。于是「这是生产构建」从框架的保证变成了本仓的约定。
//
// 【失效完全没有症状】标签若丢失，buildmode_production.go 不被编译、buildmode_dev.go
// 顶上，于是 debugBuild==true：
//   - DevToolsEnabled 仍为 true —— 发布版给用户留着一个调试入口（可改 DOM、执行 JS）；
//   - 日志级别仍是 Debug —— 噪音会快速吃掉 5MB×3 的轮转窗口，把真正的错误冲走。
// 两项都不报错、不崩溃，产物照常能跑。正是铁律 5 要覆盖的那类失效。
//
// 【单一真相】标签名不在这里写第二遍：从 buildmode_production.go 的构建约束读出来，
// 再要求每一份 BUILD_FLAGS 用的是同一个名字。这样「改了标签名却漏改一处」必红，
// 而不是退化成一条只有注释提醒的约定。
//
// 【为什么不去重而是逐份校验】BUILD_FLAGS 在上游模板里被定义了 4 份（common 一份 +
// 三个平台各一份，平台任务内的定义会 shadow common 的）。这份重复是上游自带的，
// 刻意保留——与上游模板一致才能让 wails3 升级时的 diff 可读。故本护栏的立场是
// 「每一份都必须对」，而不是「只允许存在一份」。
// ---------------------------------------------------------------------------

// buildFlagsDefRe 匹配 BUILD_FLAGS 的定义行。
//
// 刻意不锚定缩进：common 的 BUILD_FLAGS 在顶层 vars 段（2 空格），平台的在 build
// 任务内的 vars 段（更深）。两者都要抓到，否则平台那份漏检——而平台那份恰恰是
// shadow 掉 common 的那份。
var buildFlagsDefRe = regexp.MustCompile(`(?m)^[ \t]*BUILD_FLAGS:[ \t]*(.+)$`)

// buildConstraintRe 匹配 //go:build 约束行。
var buildConstraintRe = regexp.MustCompile(`(?m)^//go:build[ \t]+(.+)$`)

// debugBuildDeclRe 匹配 debugBuild 的声明行。
var debugBuildDeclRe = regexp.MustCompile(`(?m)^const debugBuild = (true|false)$`)

// buildTagsRe 从 BUILD_FLAGS 片段里抓出 -tags 的值（逗号分隔的标签列表）。
//
// 【为什么必须抓出来比对，不能用 strings.Contains】后者有前缀假阳性：
// "-tags production" 里包含子串 "-tags prod"，于是「把标签改名成 prod」这种
// 最危险的漂移反而会被判为通过。标签是要逐项相等的集合，就得按集合比。
var buildTagsRe = regexp.MustCompile(`-tags[ \t]+([A-Za-z0-9_.,]+)`)

// collectBuildTags 返回片段里 -tags 声明的全部标签。
func collectBuildTags(s string) []string {
	var out []string
	for _, m := range buildTagsRe.FindAllStringSubmatch(s, -1) {
		for _, tag := range strings.Split(m[1], ",") {
			if tag = strings.TrimSpace(tag); tag != "" {
				out = append(out, tag)
			}
		}
	}
	return out
}

// hasBuildTag 判断标签集合里是否有 want。
func hasBuildTag(tags []string, want string) bool {
	for _, t := range tags {
		if t == want {
			return true
		}
	}
	return false
}

// TestProductionTagReachesBuildFlags 断言生产构建标签出现在每一份 BUILD_FLAGS 的生产分支里。
func TestProductionTagReachesBuildFlags(t *testing.T) {
	root := projectRoot()

	// ---- 1. 标签名的唯一真相：buildmode_production.go 的构建约束 ----
	prodPath := filepath.Join(root, "buildmode_production.go")
	prodData, err := os.ReadFile(prodPath)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v —— 文件被改名/移动，护栏已瞎，请同步更新路径", prodPath, err)
	}
	prod := string(prodData)

	m := buildConstraintRe.FindStringSubmatch(prod)
	if m == nil {
		t.Fatal("buildmode_production.go 里找不到 `//go:build` 约束 —— 写法可能已变更，" +
			"请同步更新护栏 16 的解析规则（本护栏不能在解析不到时静默放行）")
	}
	tag := strings.TrimSpace(m[1])
	// 只接受单标签。复合约束（`a && b`）需要拆分才能判断「标签是否真的被要求」，
	// 与其猜，不如报错让人来更新解析规则——猜错会得出假阳性结论，比没有护栏更糟。
	if tag == "" || strings.ContainsAny(tag, " \t&|!()") {
		t.Fatalf("buildmode_production.go 的构建约束是 %q，本护栏只认单个标签。"+
			"约束写法已变更，请同步更新护栏 16 的解析规则", tag)
	}

	// 该文件还必须真的把 debugBuild 设成 false：约束对了但值反了同样致命，
	// 且同样没有症状。
	if dm := debugBuildDeclRe.FindStringSubmatch(prod); dm == nil || dm[1] != "false" {
		t.Errorf("buildmode_production.go 未把 debugBuild 声明为 false（生产标签下编译的就是它）。\n" +
			"  debugBuild=true 会让发布版保留 DevTools 与 Debug 级日志，且不报任何错。")
	}

	// ---- 2. 每一份 BUILD_FLAGS 的生产分支都必须加上该标签 ----
	// 清单刻意写全：这些是「能让构建标签走到产物里」的所有入口。文件被移动/改名会 Fatal。
	files := []string{
		"Taskfile.yml",
		"build/Taskfile.yml",
		"build/darwin/Taskfile.yml",
		"build/windows/Taskfile.yml",
		"build/linux/Taskfile.yml",
	}
	defs := 0
	for _, rel := range files {
		path := filepath.Join(root, rel)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v —— 文件被移动或改名？本护栏的文件清单需要同步", rel, err)
		}
		for i, sub := range buildFlagsDefRe.FindAllStringSubmatch(string(data), -1) {
			defs++
			tpl := strings.TrimSpace(sub[1])

			// 生产分支 = {{else}} 之后。这里刻意不做大括号配对解析：只要拿不到
			// 恰好两个分支，就说明写法变了，报错而非猜。
			parts := strings.Split(tpl, "{{else}}")
			if len(parts) != 2 {
				t.Fatalf("%s 第 %d 份 BUILD_FLAGS 里 {{else}} 出现 %d 次（期望 1 次）——\n"+
					"  模板写法已变更，请同步更新护栏 16 的解析规则", rel, i+1, len(parts)-1)
			}
			devBranch, prodBranch := parts[0], parts[1]

			if !hasBuildTag(collectBuildTags(prodBranch), tag) {
				t.Errorf("%s 第 %d 份 BUILD_FLAGS 的生产分支没有 `-tags %s`（实际抓到：%v）。\n"+
					"  后果：该平台的生产构建不带 %s 标签 → buildmode_production.go 不参与编译、\n"+
					"  buildmode_dev.go 顶上 → debugBuild==true → DevToolsEnabled 仍为 true\n"+
					"  且日志级别仍是 Debug。完全不报错，产物照常能跑。",
					rel, i+1, tag, collectBuildTags(prodBranch), tag)
			}
			if hasBuildTag(collectBuildTags(devBranch), tag) {
				t.Errorf("%s 第 %d 份 BUILD_FLAGS 的开发分支也带上了 `-tags %s`。\n"+
					"  dev 态会被当成生产态：DevTools 关闭、日志被压到 Info 级以上，"+
					"开发时看不到调试信息。", rel, i+1, tag)
			}
		}
	}
	if defs == 0 {
		t.Fatalf("在 %d 个构建入口里解析到 0 份 BUILD_FLAGS —— 写法可能已变更，"+
			"请同步更新护栏 16 的解析规则（解析不到时静默放行等于没有护栏）", len(files))
	}
}

// ---------------------------------------------------------------------------
// 护栏 17：MCP 能力不得被编译进默认构建（产物层，v3 新增）
//
// 护栏 12（TestMCPTagNotInDefaultBuild）管的是【源码层】：没有任何构建文件把 mcp
// 写进标签。但它只能证明「我们没主动加」。本护栏补上【机制层】：用真实的构建标签
// 去问 Go「wails 的 application 包里到底有哪些编译单元」，确认 mcp 的实现文件不在其中。
//
// 为什么值得两层：MCP 打开的是一个能 eval 页面 JS、调用任意绑定方法的本机端口，
// 而它被打开时【界面上没有任何提示】——应用照常启动、功能照常可用（护栏 12 的注释
// 已说明这点）。这类「没有症状」的失效正是铁律 5 的适用面，多一层分辨率不浪费。
//
// 【为什么查编译单元而不是符号表】语义上二者等价（mcp_enabled.go 不参与编译，
// 其中的符号自然不进二进制），但代价差一个量级：查编译单元是 `go list`（实测 1.2s，
// 不编译），查符号表要先完整构建两次产物。本仓把「真产物的断言」放在 `make smoke`
// 那条已经有构建可用的路径上（scripts/smoke.sh 用 go tool nm 查产物），
// `make test` 里只跑这条廉价的。
//
// 【正控是必需的，不是可选的】只看「默认构建里没有 mcp_enabled.go」是证不了的——
// 如果上游把文件改了名，这句会永远为真，护栏就变成了一个安静的假承诺。故必须同时断言
// 「加上 mcp 标签后能看到真实现文件」，把断言变成可证伪的。
// ---------------------------------------------------------------------------

// wailsAppPkg 是 MCP 开关所在的包：wails v3 把 mcp_enabled.go / mcp_disabled.go
// 都放在 pkg/application 下，用构建约束二选一。
const wailsAppPkg = "github.com/wailsapp/wails/v3/pkg/application"

// mcpEnabledFileRe 匹配「MCP 能力被编译进来」的那些文件
// （mcp_enabled.go、mcp_eval_enabled.go、mcp_protocol_enabled.go、mcp_tools_enabled.go）。
var mcpEnabledFileRe = regexp.MustCompile(`^mcp_.*enabled\.go$`)

// productionBuildTags 从真实构建配置里取生产构建用的标签，避免在护栏里写第二遍。
//
// 取 build/darwin/Taskfile.yml：平台 Taskfile 的 BUILD_FLAGS 会 shadow common 的，
// 而 darwin 是本仓实际交付与验证的平台。四个平台的一致性由护栏 16 单独保证。
func productionBuildTags(t *testing.T) []string {
	t.Helper()
	path := filepath.Join(projectRoot(), "build", "darwin", "Taskfile.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v —— 文件被改名/移动，护栏已瞎，请同步更新路径", path, err)
	}
	m := buildFlagsDefRe.FindStringSubmatch(string(data))
	if m == nil {
		t.Fatalf("在 %s 里找不到 BUILD_FLAGS —— 写法可能已变更，"+
			"请同步更新护栏 17 的解析规则", path)
	}
	parts := strings.Split(strings.TrimSpace(m[1]), "{{else}}")
	if len(parts) != 2 {
		t.Fatalf("%s 的 BUILD_FLAGS 里 {{else}} 出现 %d 次（期望 1 次）—— "+
			"模板写法已变更，请同步更新护栏 17 的解析规则", path, len(parts)-1)
	}
	tags := collectBuildTags(parts[1])
	if len(tags) == 0 {
		t.Fatalf("%s 的 BUILD_FLAGS 生产分支解析到 0 个标签 —— "+
			"请同步更新护栏 17 的解析规则（解析不到时静默放行等于没有护栏）", path)
	}
	return tags
}

// wailsApplicationGoFiles 返回 wails 的 application 包在给定构建标签下的编译单元文件名。
func wailsApplicationGoFiles(t *testing.T, tags []string) []string {
	t.Helper()
	args := []string{"list", "-f", `{{join .GoFiles " "}}`}
	if len(tags) > 0 {
		args = append(args, "-tags", strings.Join(tags, ","))
	}
	args = append(args, wailsAppPkg)

	cmd := exec.Command("go", args...)
	cmd.Dir = projectRoot()
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("`go %s` 失败: %v —— 无法确定构建标签对应的编译单元，"+
			"护栏已瞎（不能在查不到时静默放行）", strings.Join(args, " "), err)
	}
	files := strings.Fields(string(out))
	if len(files) == 0 {
		t.Fatalf("`go %s` 返回 0 个编译单元 —— 包路径或 go list 模板已变更，"+
			"请同步更新护栏 17 的解析规则", strings.Join(args, " "))
	}
	return files
}

// TestMCPNotCompiledIntoDefaultBuild 断言默认构建不会把 MCP 能力编进来。
func TestMCPNotCompiledIntoDefaultBuild(t *testing.T) {
	tags := productionBuildTags(t)

	// ---- 1. 默认（= 生产构建用的标签）：MCP 实现文件一个都不该出现 ----
	defFiles := wailsApplicationGoFiles(t, tags)
	var leaked []string
	for _, f := range defFiles {
		if mcpEnabledFileRe.MatchString(f) {
			leaked = append(leaked, f)
		}
	}
	if len(leaked) > 0 {
		t.Errorf("默认构建标签（%v）下，wails 的 application 包仍编译了 MCP 实现：%v\n"+
			"  这意味着产物里带着一个能 eval 页面 JS、调用任意绑定方法的本机端口，"+
			"而界面上没有任何提示。\n"+
			"  唯一受认可的启用方式是 WAILS_MCP=1（只用于 make dev-mcp）。", tags, leaked)
	}

	// 关标签时上游必须提供 no-op 实现。缺了它不是「MCP 被剥掉」而是包压根编译不过，
	// 所以这里只当作锚点存在性检查，不作安全断言。
	if !contains(defFiles, "mcp_disabled.go") {
		t.Fatalf("默认标签下 application 包里找不到 mcp_disabled.go —— " +
			"上游的开关文件名可能已变更，请同步更新护栏 17 的解析规则")
	}

	// ---- 2. 正控：带上 mcp 标签必须能看到真实现 ----
	// 没有这一步，上面那条「没看到 mcp_*_enabled.go」可能只是因为我们查的名字不对，
	// 即一个永远为真的假承诺。
	onFiles := wailsApplicationGoFiles(t, append(append([]string{}, tags...), "mcp"))
	if !contains(onFiles, "mcp_enabled.go") {
		t.Fatalf("加上 mcp 标签后仍看不到 mcp_enabled.go —— 说明本护栏的判据已经与上游脱节" +
			"（包路径或文件名变了），上面那条「默认构建不含 MCP」因此不可信。\n" +
			"  请同步更新护栏 17 的解析规则。")
	}
}

// ---------------------------------------------------------------------------
// 护栏 18：macOS 最低系统版本必须两处一致（v3 新增）
//
// 【为什么需要】v3 要求 macOS 12.0（v2 是 10.13），而这个下限同时写在两个地方：
//
//	build/darwin/Taskfile.yml 的 CGO_CFLAGS / CGO_LDFLAGS（-mmacosx-version-min）
//	                          与 MACOSX_DEPLOYMENT_TARGET   ← 决定二进制真正能跑在哪
//	build/darwin/Info.plist 的 LSMinimumSystemVersion      ← 决定系统【允许】在哪启动
//
// 两者漂移的后果不对称，其中坏的那个完全没有症状：
//   - Info 低于二进制：macOS 按 Info 放行 → 在旧系统上启动后 dyld 报错/崩溃，
//     而这只发生在【用户机器】上，构建机与 CI 全绿；
//   - Info 高于二进制：系统提前拒绝启动，属于保守方向，但同样是错的。
//
// 【为什么不能只改 Info.plist】它是 common:update:build-assets 的渲染产物
// （由上游的 Info.plist.tmpl 生成，build.sh 每次构建都会重跑这一步），手改会被覆盖。
// 也就是说：真要把下限调低，必须动 Taskfile 的编译标志；只改 plist 是自欺。
//
// 【为什么这条值得护栏预算】上游自己就用测试把 12.0.0 钉住了
// （wails/v3 的 internal/commands/darwin_version_test.go，还专门断言不许出现 10.15），
// 说明这个值会随版本变动。一旦上游改了模板而我们没跟着改 Taskfile（或反过来），
// 差异就只会在用户机器上显形。
//
// 【不做的事】不断言 README 的措辞。系统要求在 README 里是散文，断言措辞会因改写而误红，
// 而 README 另有改写计划（tasks.md 7.4）；此处只覆盖两处机器可读的真相。
// ---------------------------------------------------------------------------

// macosVersionMinRe 匹配 Taskfile 里的 -mmacosx-version-min=X。
var macosVersionMinRe = regexp.MustCompile(`-mmacosx-version-min=([0-9]+(?:\.[0-9]+)*)`)

// macosDeploymentTargetRe 匹配 Taskfile 里的 MACOSX_DEPLOYMENT_TARGET: "X"。
var macosDeploymentTargetRe = regexp.MustCompile(`MACOSX_DEPLOYMENT_TARGET:\s*"([0-9]+(?:\.[0-9]+)*)"`)

// plistMinSystemVersionRe 匹配 plist 里 LSMinimumSystemVersion 的 string 值。
var plistMinSystemVersionRe = regexp.MustCompile(`(?s)<key>LSMinimumSystemVersion</key>\s*<string>([^<]+)</string>`)

// normMacOSVersion 把 12.0 / 12.0.0 归一成 12.0，便于跨写法比较。
//
// 只比 major.minor：patch 位在最低系统版本上没有意义，上游模板本身写作 12.0.0
// 而 Taskfile 里写作 12.0 —— 这是写法差异，不是漂移。
func normMacOSVersion(t *testing.T, v string) string {
	t.Helper()
	parts := strings.Split(strings.TrimSpace(v), ".")
	if len(parts) < 2 {
		t.Fatalf("版本号 %q 不是 major.minor 形态 —— 写法可能已变更，"+
			"请同步更新护栏 18 的解析规则", v)
	}
	return parts[0] + "." + parts[1]
}

// TestMacOSMinimumVersionIsConsistent 断言编译目标与 Info.plist 的最低系统版本一致。
func TestMacOSMinimumVersionIsConsistent(t *testing.T) {
	root := projectRoot()

	// ---- 1. Taskfile 的编译标志 ----
	tfPath := filepath.Join(root, "build", "darwin", "Taskfile.yml")
	tfData, err := os.ReadFile(tfPath)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v —— 文件被改名/移动，护栏已瞎，请同步更新路径", tfPath, err)
	}
	tf := string(tfData)

	minFlags := macosVersionMinRe.FindAllStringSubmatch(tf, -1)
	if len(minFlags) == 0 {
		t.Fatal("build/darwin/Taskfile.yml 里找不到 -mmacosx-version-min —— 写法可能已变更，" +
			"请同步更新护栏 18 的解析规则（本护栏不能在解析不到时静默放行）")
	}
	// CGO_CFLAGS 与 CGO_LDFLAGS 两处都要有：只设一个的话编译或链接会退回 SDK 默认版本。
	if len(minFlags) < 2 {
		t.Errorf("build/darwin/Taskfile.yml 里 -mmacosx-version-min 只出现 %d 次"+
			"（期望 2 次：CGO_CFLAGS 与 CGO_LDFLAGS）——\n"+
			"  只设一个会让编译或链接其中一侧退回 SDK 默认值。", len(minFlags))
	}

	targetMatch := macosDeploymentTargetRe.FindStringSubmatch(tf)
	if targetMatch == nil {
		t.Fatal("build/darwin/Taskfile.yml 里找不到 MACOSX_DEPLOYMENT_TARGET —— " +
			"写法可能已变更，请同步更新护栏 18 的解析规则")
	}
	tfVersion := normMacOSVersion(t, targetMatch[1])

	for _, m := range minFlags {
		if got := normMacOSVersion(t, m[1]); got != tfVersion {
			t.Errorf("build/darwin/Taskfile.yml 内部就不一致：-mmacosx-version-min=%s "+
				"与 MACOSX_DEPLOYMENT_TARGET=%s", got, tfVersion)
		}
	}

	// ---- 2. Info.plist（渲染产物，代表系统看到的下限） ----
	for _, rel := range []string{
		filepath.Join("build", "darwin", "Info.plist"),
		filepath.Join("build", "darwin", "Info.dev.plist"),
	} {
		path := filepath.Join(root, rel)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v —— 文件被改名/移动，护栏已瞎，请同步更新路径", rel, err)
		}
		m := plistMinSystemVersionRe.FindStringSubmatch(string(data))
		if m == nil {
			t.Fatalf("%s 里找不到 LSMinimumSystemVersion 的字符串值 —— 写法可能已变更，"+
				"请同步更新护栏 18 的解析规则", rel)
		}
		plistVersion := normMacOSVersion(t, m[1])
		if plistVersion != tfVersion {
			t.Errorf("%s 的 LSMinimumSystemVersion = %s，但 build/darwin/Taskfile.yml "+
				"的编译目标是 %s。\n"+
				"  两者漂移时：Info 低于二进制 → macOS 按 Info 放行、在旧系统上启动后崩溃，"+
				"而构建机与 CI 全绿（只在用户机器上显形）；\n"+
				"  Info 高于二进制 → 系统提前拒绝启动（保守方向，但同样是错的）。\n"+
				"  注意 Info.plist 是 common:update:build-assets 的渲染产物，改它会被覆盖——"+
				"真要调下限得动 Taskfile 的 CGO 标志。", rel, plistVersion, tfVersion)
		}
	}
}

// ---------------------------------------------------------------------------
// 护栏 19：关窗必须隐藏到托盘（三平台统一，且不得再靠平台常量）
//
// 【为什么需要】本次报障「关闭后没办法再次显示界面」的直接成因是
// hide_window_darwin.go 的 hideWindowOnClose() 在 darwin 上返回 false：
// main.go 于是【不注册】WindowClosing 钩子，关闭按钮走默认动作——销毁窗口。
// 而该文件的立论（「macOS 无托盘常驻」）在 v3 已失效（internal/tray/tray.go 文件头
// 写明 v3 起 macOS 有真托盘，app.go 也是三平台一律 tray.Start）。分叉删掉后，
// 关窗决策改为【运行期】询问「托盘是否真的起来了」。
//
// 本护栏拦两类回归：
//   - 关窗接线被移除或被包进平台分支（darwin 上重新变成「关窗不隐藏」）；
//   - 有人把 hideWindowOnClose 之类的「编译期回答要不要隐藏」分叉又加回来。
//
// 铁律：解析到 0 个目标必须 Fatal（护栏感知自己瞎了）。
// ---------------------------------------------------------------------------

// selectorPath 返回 `a.b.c` 形式的点分选择器路径；非选择器链（含调用等）返回 ""。
func selectorPath(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		p := selectorPath(v.X)
		if p == "" {
			return ""
		}
		return p + "." + v.Sel.Name
	}
	return ""
}

// containsCallNamed 判断子树里是否出现名为 name 的函数/方法调用（按选择器尾名或标识符匹配）。
func containsCallNamed(n ast.Node, name string) bool {
	return countCallsNamed(n, name) > 0
}

// countCallsNamed 统计子树里名为 name 的函数/方法调用数量。
func countCallsNamed(n ast.Node, name string) int {
	if n == nil {
		return 0
	}
	count := 0
	ast.Inspect(n, func(x ast.Node) bool {
		call, ok := x.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fun := call.Fun.(type) {
		case *ast.Ident:
			if fun.Name == name {
				count++
			}
		case *ast.SelectorExpr:
			if fun.Sel.Name == name {
				count++
			}
		}
		return true
	})
	return count
}

// mainFuncDecl 返回 main.go 的 main() 函数声明（无接收者）。
func mainFuncDecl(f *ast.File) *ast.FuncDecl {
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == "main" && fd.Recv == nil {
			return fd
		}
	}
	return nil
}

// findWindowClosingHook 定位注册在 events.Common.WindowClosing 上的 RegisterHook 调用
// 及其回调函数字面量。找不到返回 (nil, nil)。
func findWindowClosingHook(f *ast.File) (*ast.CallExpr, *ast.FuncLit) {
	var found *ast.CallExpr
	ast.Inspect(f, func(n ast.Node) bool {
		if found != nil {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); !ok || sel.Sel.Name != "RegisterHook" {
			return true
		}
		if len(call.Args) < 2 || selectorPath(call.Args[0]) != "events.Common.WindowClosing" {
			return true
		}
		if _, ok := call.Args[1].(*ast.FuncLit); !ok {
			return true
		}
		found = call
		return false
	})
	if found == nil {
		return nil, nil
	}
	fl, _ := found.Args[1].(*ast.FuncLit)
	return found, fl
}

// TestWindowCloseHidesToTray 断言关窗接线无条件存在，且不再有平台分叉的隐藏决策。
func TestWindowCloseHidesToTray(t *testing.T) {
	files, err := parseDir(projectRoot())
	if err != nil {
		t.Fatalf("解析项目根目录失败: %v", err)
	}

	// ---- 1. 编译期的「要不要隐藏」平台分叉必须已删除 ----
	for name, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			fd, ok := n.(*ast.FuncDecl)
			if ok && fd.Name.Name == "hideWindowOnClose" {
				t.Errorf("%s 定义了 hideWindowOnClose —— 关窗决策【不得】用编译期平台常量。\n"+
					"  darwin 上它曾返回 false，于是 main.go 不注册 WindowClosing 钩子，"+
					"关闭按钮走默认动作销毁窗口——这正是「关窗后回不去」的直接成因。\n"+
					"  应改为运行期询问托盘是否真的起来了（main.go 的 WindowClosing 钩子 + app.go 的 trayRunning）。", name)
			}
			return true
		})
	}

	f, ok := files["main.go"]
	if !ok {
		t.Fatal("护栏找不到 main.go——写法可能已变更，请同步更新护栏解析规则")
	}

	// ---- 2. 必须在 events.Common.WindowClosing 上注册钩子，回调里 Hide()+Cancel() ----
	call, fl := findWindowClosingHook(f)
	if call == nil || fl == nil {
		t.Fatal("main.go 未在 events.Common.WindowClosing 上注册 RegisterHook（回调须为函数字面量）——\n" +
			"  接线被移除或写法变更，护栏已瞎，请同步更新解析规则。\n" +
			"  少了它，关闭按钮会走默认动作销毁窗口（本次报障的成因）。")
	}
	if !containsCallNamed(fl.Body, "Hide") {
		t.Error("WindowClosing 钩子回调里没有 window.Hide()：关窗不会隐藏窗口")
	}
	if !containsCallNamed(fl.Body, "Cancel") {
		t.Error("WindowClosing 钩子回调里没有 e.Cancel()：不 Cancel 则默认的销毁 listener 照常执行（" +
			"框架在 Common.WindowClosing 上自挂了一个销毁 listener，见 webview_window.go），窗口仍会被销毁")
	}

	// ---- 3. 该注册必须是 main() 的【顶层】语句，不得裹在条件/平台分支里 ----
	md := mainFuncDecl(f)
	if md == nil {
		t.Fatal("main.go 找不到 main() —— 写法可能已变更，护栏已瞎，请同步更新解析规则")
	}
	topLevel := false
	for _, stmt := range md.Body.List {
		if es, ok := stmt.(*ast.ExprStmt); ok && es.X == ast.Expr(call) {
			topLevel = true
			break
		}
	}
	if !topLevel {
		t.Error("RegisterHook(events.Common.WindowClosing, …) 不是 main() 的顶层语句——\n" +
			"  说明关窗接线又被包进了条件分支（例如旧的 if hideWindowOnClose()）。\n" +
			"  darwin 上这会重新变成「关窗不隐藏、窗口被销毁、进程还在、回不去」。")
	}
}

// ---------------------------------------------------------------------------
// 护栏 19b：休息模式自动完成必须同时广播两个事件
//
// 【本次报障的根因】tickRestMode 自动把 fire 写成 done，却只广播了
// restmode:completed，没有广播 reminder:changed。宠物窗口只在 fired/changed 时
// 刷新待处理，于是一直挂着一条早已完成的提醒；用户点击「完成」得到「该提醒已处理」，
// 前端只写日志、UI 不动——看起来就是按钮死了。
//
// 这类缺失没有任何症状：状态机照常 Reset，日志里也只有一条正常完成记录。
// 故必须把「自动完成 → 两个广播」编译成会红的检查。
//
// 铁律：解析到 0 个目标必须报错（护栏感知自己瞎了）。
// ---------------------------------------------------------------------------

// TestRestAutoCompleteBroadcastsBothEvents 断言 tickRestMode 同时广播
// reminder:changed 与 restmode:completed。
func TestRestAutoCompleteBroadcastsBothEvents(t *testing.T) {
	files, err := parseDir(projectRoot())
	if err != nil {
		t.Fatalf("解析项目根目录失败: %v", err)
	}
	app, ok := files["app.go"]
	if !ok {
		t.Fatal("护栏找不到 app.go——写法可能已变更，请同步更新护栏解析规则")
	}

	fd := findMethodDecl(app, "tickRestMode")
	if fd == nil {
		t.Fatal("app.go 中找不到 tickRestMode——方法被改名/移动，护栏已瞎，" +
			"请同步更新护栏解析规则（解析不到时静默放行等于没有护栏）")
	}

	if !containsCallNamed(fd.Body, "emitReminderChanged") {
		t.Error("tickRestMode 没有广播 reminder:changed：\n" +
			"  自动完成后宠物/首页/Alert 都不知道待处理已变化，宠物会一直挂着一条\n" +
			"  已被写成 done 的提醒；用户再点完成只会得到「该提醒已处理」，且 UI 不动。\n" +
			"  必须在 Reset 的同一路径里调用 emitReminderChanged。")
	}
	if !containsEmitWithLiteral(fd.Body, "restmode:completed") {
		t.Error("tickRestMode 没有广播 restmode:completed：\n" +
			"  Alert.vue 的「做完了」瞬时反馈依赖这条事件；缺了它不会报错，" +
			"只会让休息结束变成一个无声的消失。")
	}
}

// containsEmitWithLiteral 判断子树里是否出现 `xxx.Emit("want", ...)`。
func containsEmitWithLiteral(n ast.Node, want string) bool {
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		if found {
			return false
		}
		call, ok := x.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Emit" || len(call.Args) == 0 {
			return true
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		s, uerr := strconv.Unquote(lit.Value)
		if uerr == nil && s == want {
			found = true
			return false
		}
		return true
	})
	return found
}

// ---------------------------------------------------------------------------
// 护栏 20：托盘缺失时，关闭必须真正退出（兜底链路）
//
// 【为什么需要】D13 指出：托盘在 ServiceStartup 里起，可能失败（失败只记日志、不阻断
// 启动）。若此时关窗仍「隐藏」，应用就变成一个看不见也点不到的后台进程——比「关窗即退」
// 糟得多。故隐藏【必须】以「托盘真的起来了」为条件，且托盘缺失时进程必须退出，
// 不留无窗口进程（openspec/specs/system-tray/spec.md）。
//
// 本护栏拦：把 Hide() 改成无条件（不看托盘状态）；托盘状态被写成编译期常量；
// 托盘缺失分支不再真正退出；以及 macOS 选项 ApplicationShouldTerminateAfterLastWindowClosed
// 被误设成 true（它会与 Hide() 相冲、在隐藏最后一个窗口时反过来杀进程，破坏 R1）。
//
// 铁律：解析到 0 个目标必须 Fatal。
// ---------------------------------------------------------------------------

// compositeBoolField 在复合字面量里找字段 field 的布尔字面量取值。
// 返回 (found, value)：field 不存在时 found=false。
func compositeBoolField(lit *ast.CompositeLit, field string) (bool, bool) {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		ident, ok := kv.Key.(*ast.Ident)
		if !ok || ident.Name != field {
			continue
		}
		if v, ok := kv.Value.(*ast.Ident); ok {
			return true, v.Name == "true"
		}
		return true, false
	}
	return false, false
}

// nestedLiteral 在复合字面量里找字段 field 的复合字面量取值。
func nestedLiteral(lit *ast.CompositeLit, field string) *ast.CompositeLit {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if ident, ok := kv.Key.(*ast.Ident); ok && ident.Name == field {
			if cl, ok := kv.Value.(*ast.CompositeLit); ok {
				return cl
			}
		}
	}
	return nil
}

// findMethodDecl 返回文件里名为 name 的方法声明（带接收者）。找不到返回 nil。
func findMethodDecl(f *ast.File, name string) *ast.FuncDecl {
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv != nil && fd.Name.Name == name {
			return fd
		}
	}
	return nil
}

// hookHidesOnlyWhenTrayRunning 判断关窗钩子里所有 Hide() 调用是否都落在
// 「if <trayRunning>() { … }」分支内（即：没有任何无条件隐藏）。
func hookHidesOnlyWhenTrayRunning(fl *ast.FuncLit) bool {
	total := countCallsNamed(fl.Body, "Hide")
	if total == 0 {
		return false
	}
	inside := 0
	for _, stmt := range fl.Body.List {
		ifs, ok := stmt.(*ast.IfStmt)
		if !ok {
			continue
		}
		if !containsCallNamed(ifs.Cond, "trayRunning") {
			continue
		}
		inside += countCallsNamed(ifs.Body, "Hide")
	}
	return inside > 0 && inside == total
}

// TestCloseWithoutTrayExitsCleanly 断言「托盘缺失 → 关闭即退出」这条兜底链路成立。
func TestCloseWithoutTrayExitsCleanly(t *testing.T) {
	files, err := parseDir(projectRoot())
	if err != nil {
		t.Fatalf("解析项目根目录失败: %v", err)
	}
	f, ok := files["main.go"]
	if !ok {
		t.Fatal("护栏找不到 main.go——写法可能已变更，请同步更新护栏解析规则")
	}

	// ---- 1. macOS 选项：ApplicationShouldTerminateAfterLastWindowClosed 必须为 false ----
	// 【为什么是 false，而不是直觉上的 true】true 会在「隐藏最后一个窗口」时反过来触发进程
	// 退出（A/B 实测 2026-09-20：true 时关窗钩子隐藏成功、进程随即退出；false 时进程驻留），
	// 破坏 R1「托盘在 → 进程驻留」。上游 issue #4389 记录了这个行为，官方范例 hide-window
	// 也设 false。托盘缺失时的退出改由关窗钩子显式 Quit() 保证（见第 3 条断言）。
	opts := findAppOptionsLiteral(f)
	if opts == nil {
		t.Fatal("护栏在 main.go 中未找到 application.Options{...} 字面量——写入法可能已变更，请同步更新解析规则")
	}
	mac := nestedLiteral(opts, "Mac")
	if mac == nil {
		t.Fatal("application.Options 未设置 Mac（application.MacOptions{...}）——\n" +
			"  该选项须显式钉住 ApplicationShouldTerminateAfterLastWindowClosed=false（见下）")
	}
	found, isTrue := compositeBoolField(mac, "ApplicationShouldTerminateAfterLastWindowClosed")
	if !found {
		t.Fatal("Mac.Options 未设置 ApplicationShouldTerminateAfterLastWindowClosed——" +
			"该字段曾在「隐藏最后一个窗口」时反过来触发退出（issue #4389），必须显式钉住")
	}
	if isTrue {
		t.Error("Mac.ApplicationShouldTerminateAfterLastWindowClosed 被设成了 true：\n" +
			"  实测（并见上游 issue #4389）它会与 Hide() 相冲——隐藏最后一个窗口即触发进程退出，\n" +
			"  破坏「托盘在 → 窗口隐藏、进程驻留」。必须为 false；托盘缺失时的退出去关窗钩子显式 Quit()。")
	}

	// ---- 2. 关窗钩子里 Hide() 必须以「托盘在运行」为条件 ----
	_, fl := findWindowClosingHook(f)
	if fl == nil {
		t.Fatal("main.go 未在 events.Common.WindowClosing 上注册 RegisterHook —— 护栏已瞎，请同步更新解析规则")
	}
	if !hookHidesOnlyWhenTrayRunning(fl) {
		t.Error("关窗钩子里的 window.Hide() 不是被「托盘在运行」分支包住的：\n" +
			"  托盘没起来时仍隐藏，会让应用变成看不见也点不到的后台进程（design.md D13）。\n" +
			"  钩子应为：if appSvc.trayRunning() { window.Hide(); e.Cancel(); return }，其后再走退出。")
	}

	// ---- 3. 托盘缺失分支必须显式退出（Quit），不能只「不隐藏」----
	// flag=false 时，销毁最后一个窗口不会让 macOS 进程退出；只「不隐藏」会留下无窗口进程。
	if !containsCallNamed(fl.Body, "Quit") {
		t.Error("关窗钩子的托盘缺失分支没有退出应用（找不到 Quit 调用）：\n" +
			"  Mac.ApplicationShouldTerminateAfterLastWindowClosed=false 时，销毁窗口不会让进程退出，\n" +
			"  只放行销毁会留下无窗口进程。托盘缺失分支必须显式 Quit() 优雅退出。")
	}

	// ---- 4. trayRunning 必须是未导出的运行期谓词（读 atomic），不能是编译期常量 ----
	app, ok := files["app.go"]
	if !ok {
		t.Fatal("护栏找不到 app.go——写法可能已变更，请同步更新护栏解析规则")
	}
	tr := findMethodDecl(app, "trayRunning")
	if tr == nil {
		t.Fatal("app.go 未定义 trayRunning 方法——关窗钩子依赖它做运行期判断，护栏已瞎，请同步更新解析规则")
	}
	if ast.IsExported("trayRunning") {
		t.Error("trayRunning 是导出方法——App 是注册的 Service，导出方法会被绑定给前端")
	}
	// 返回类型必须是 bool
	if tr.Type.Results == nil || len(tr.Type.Results.List) != 1 {
		t.Error("trayRunning 的返回值不是单个 bool——关窗钩子的条件判断将失去意义")
	} else if id, ok := tr.Type.Results.List[0].Type.(*ast.Ident); !ok || id.Name != "bool" {
		t.Errorf("trayRunning 返回类型不是 bool —— 关窗钩子的条件判断将失去意义")
	}
	// 谓词体必须读 atomic（.Load()），否则可能是写死的常量
	if !containsCallNamed(tr.Body, "Load") {
		t.Error("trayRunning 的体里没有原子读取（.Load()）——它应读取 ServiceStartup 写入的运行期状态，" +
			"而不是返回编译期常量（那又回到了 hide_window_*.go 的老路）")
	}

	// ---- 5. 托盘启动成功处必须真的把状态置 true ----
	if countCallsNamed(app, "Store") == 0 {
		t.Error("app.go 里没有任何 .Store() —— trayUp 从未被写入，" +
			"trayRunning 将恒为 false、永远走「放行退出」分支（永不隐藏）。")
	}
}

// ---------------------------------------------------------------------------
// 护栏 21：更新检查必须真的被启动/停止，绑定方法不得缺失
//
// 自动检查更新没被 ServiceStartup 启动时没有任何症状：应用照常运行、日志干净，
// 只是永远收不到新版本提示。按铁律 5，把这条接线编译成会红的检查。
// ---------------------------------------------------------------------------

func TestUpdateCheckerWired(t *testing.T) {
	files, err := parseDir(projectRoot())
	if err != nil {
		t.Fatalf("解析项目根目录失败: %v", err)
	}
	app, ok := files["app.go"]
	if !ok {
		t.Fatal("护栏找不到 app.go——写法可能已变更，请同步更新护栏解析规则")
	}

	startup := findMethodDecl(app, "ServiceStartup")
	if startup == nil {
		t.Fatal("app.go 中找不到 ServiceStartup——方法被改名/移动，护栏已瞎，请同步更新解析规则")
	}
	if !containsCallNamed(startup.Body, "startUpdateChecker") {
		t.Error("ServiceStartup 没有调用 startUpdateChecker：\n" +
			"  自动检查更新永远不会启动，用户停在旧版本也收不到提示，且没有任何报错。")
	}

	shutdown := findMethodDecl(app, "ServiceShutdown")
	if shutdown == nil {
		t.Fatal("app.go 中找不到 ServiceShutdown——方法被改名/移动，护栏已瞎，请同步更新解析规则")
	}
	if !containsCallNamed(shutdown.Body, "stopUpdateChecker") {
		t.Error("ServiceShutdown 没有调用 stopUpdateChecker：检查 goroutine 会在关闭过程中继续跑。")
	}

	for _, name := range []string{
		"GetUpdateState",
		"CheckForUpdates",
		"SetUpdateCheckEnabled",
		"SkipUpdateVersion",
		"OpenUpdatePage",
	} {
		if findMethodDecl(app, name) == nil {
			t.Errorf("app.go 缺少绑定方法 %s——设置页/宠物气泡的更新链路会静默断掉", name)
		}
	}
}
