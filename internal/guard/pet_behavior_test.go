package guard

// 护栏 27–28：宠物待机行为的「接线」与「门控完备」（openspec/changes/pet-autonomy）。
//
// 【为什么需要这两条】本变更新增的东西全都是"不报错、不崩溃、只是默默失效"的类型：
//
//   - 常量定义了、放到 lib/pet.ts 里，但 Pet.vue 忘了引用 → 微动作永远不触发。
//     不会有任何报错，宠物的表现只是"和以前一样安静"——和"这次没做好"无法区分。
//     这类问题在本仓已经有过前科：图集定义了 9 行动画，其中 5 行从未被状态机触达，
//     代码读起来完全正常。
//   - 门控漏掉一条压制条件 → 用户开会/暂停时宠物还在自顾自表演。
//     同样不会报错，而且只在特定场景下重复出现，靠人眼回归极难覆盖。
//
// 两条护栏都遵循 AGENTS.md 铁律 5：解析不到东西就 Fatal，不当成"通过"。

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"morrow/internal/sit"
)

// petVueRelativePath 是宠物渲染层的唯一实现文件。
const petVueRelativePath = "frontend/src/views/Pet.vue"

// petLibRelativePath 是宠物渲染常量的单一真相。
const petLibRelativePath = "frontend/src/lib/pet.ts"

// petGateSuppressors 是门控必须覆盖的压制条件（标识符 → 用户可读的名字）。
//
// 每一条都对应一种"此刻不该表演"的场景：
//   - pendingCount：有待处理提醒（提醒优先于一切表演）
//   - paused：智能免打扰/演示模式生效
//   - suggestionVisible：正在建议进入演示模式（气泡在场）
//   - dockSide：宠物已贴边收起（无观众）
//   - prefersReducedMotion：系统要求减弱动态效果
//   - dozing / waking：打盹或惊醒反馈期间
//   - menuVisible：右键菜单打开（菜单期间不演戏）
//
// 新增压制条件时请同时加进这张表——否则下一个人删掉它时护栏不会响。
var petGateSuppressors = []struct {
	ident string
	label string
}{
	{ident: "pendingCount", label: "有待处理提醒"},
	{ident: "paused", label: "暂停/演示中"},
	{ident: "suggestionVisible", label: "演示建议气泡在场"},
	{ident: "dockSide", label: "已贴边收起"},
	{ident: "prefersReducedMotion", label: "系统要求减弱动态"},
	{ident: "dozing", label: "打盹中"},
	{ident: "waking", label: "惊醒反馈中"},
	{ident: "menuVisible", label: "右键菜单打开"},
}

// petBehaviorConstantPattern 匹配 lib/pet.ts 里与宠物行为/交互相关、必须被消费的导出常量。
//
// 【为什么用前缀匹配而不是逐个列举】逐个列举会在下次新增常量时忘记加进护栏，
// 于是新常量又可以"定义了没人用"。前缀匹配让它自动纳入。
//
// MENU 这一类是后续补进来的：菜单的宽度/内边距/缩放档位最初没有任何护栏覆盖，
// 删掉用法不会有症状（宽度回落到默认、缩放菜单点了没反应）。
//
// MIRROR 是 pet-activity-mirror 补进来的：镜像的确认次数/最小停留/姿态表同样是
// "定义了但没人查表"就静默失效的那一类。
var petBehaviorConstantPattern = regexp.MustCompile(`(?m)^export const (PET_(?:MICRO|DOZE|SLEEP|WAKE|MENU|MIRROR)_[A-Z_]+)`)

func TestPetIdleGateCoversSuppressors(t *testing.T) {
	assertGateCovers(t, "function canPlayMicro(", petGateSuppressors)
}

// petMirrorGateSuppressors 是镜像门控必须覆盖的压制条件。
//
// 【为什么比微动作少一条 prefersReducedMotion】减弱动态约束的是"动"，不是"样子"：
// 镜像的姿态是静态倾斜（手臂摆动的动画部分由 CSS 媒体查询停掉），保留它才是对用户
// 有用的信息；而微动作整个就是"动一下"，只能整条挡掉。
//
// 这条差异是刻意写下来的：若哪天有人"顺手对齐"两张表，把 prefersReducedMotion
// 加进镜像门控，症状是减弱动态的用户永远看不到宠物在跟着他——不报错、不崩溃。
var petMirrorGateSuppressors = []struct {
	ident string
	label string
}{
	{ident: "pendingCount", label: "有待处理提醒"},
	{ident: "paused", label: "暂停/演示中"},
	{ident: "suggestionVisible", label: "演示建议气泡在场"},
	{ident: "dockSide", label: "已贴边收起"},
	{ident: "dozing", label: "打盹中"},
	{ident: "waking", label: "惊醒反馈中"},
	{ident: "menuVisible", label: "右键菜单打开"},
}

func TestPetMirrorGateCoversSuppressors(t *testing.T) {
	assertGateCovers(t, "function canApplyMirror(", petMirrorGateSuppressors)
}

// assertGateCovers 断言某个门控函数的函数体里命中了全部压制标识。
//
// 抽成一处是因为门控已经有两个（微动作 / 镜像），下一轮还会有第三个：
// 每多一个门控就复制一遍这段逻辑，漏掉的那个函数恰恰不会有任何症状。
func assertGateCovers(t *testing.T, signature string, suppressors []struct {
	ident string
	label string
}) {
	t.Helper()
	source := readPetVueStripped(t)

	body, ok := extractTSFunctionBody(source, signature)
	if !ok {
		t.Fatalf("在 %s 里找不到 %s 的函数体——门控可能被改名、拆走或改写成了箭头函数；"+
			"护栏已经看不清代码，写法若确实变了请同步更新本护栏的解析规则", petVueRelativePath, signature)
	}

	var missing []string
	for _, item := range suppressors {
		if !strings.Contains(body, item.ident) {
			missing = append(missing, item.label+"（"+item.ident+"）")
		}
	}
	if len(missing) > 0 {
		t.Errorf("%s 的门控漏掉了 %d 条压制条件：\n  %s\n"+
			"这些条件必须集中在门控函数一处判断，散到调用点迟早漏路径",
			signature, len(missing), strings.Join(missing, "\n  "))
	}
}

func TestPetBehaviorConstantsAreConsumed(t *testing.T) {
	libSource := readFileOrFail(t, petLibRelativePath)
	vueSource := readPetVueStripped(t)

	// 只扫 lib/pet.ts 的【代码】（注释里提到常量名不算定义）。
	constants := petBehaviorConstantPattern.FindAllStringSubmatch(stripComments(libSource), -1)
	if len(constants) == 0 {
		t.Fatalf("在 %s 里解析到 0 个待机行为常量——命名前缀可能已变更（当前匹配 PET_(MICRO|DOZE|SLEEP|WAKE)_）；"+
			"护栏已经看不清代码，请同步更新本护栏的解析规则", petLibRelativePath)
	}

	// 【为什么也要扫 lib/pet.ts 自己】镜像的时序状态机（advanceMirror）住在 lib/pet.ts 里，
	// 它才是 PET_MIRROR_CONFIRM_TICKS / PET_MIRROR_MIN_HOLD_MS 的真实消费者。
	// 只看 Pet.vue 会把这类"在库内被消费"的常量误判成没人用。
	//
	// 但必须剥掉常量的**定义行**：不剥的话，每个常量都能被自己的名字蹭到一次，
	// 这条护栏就退化成永远为真（本仓第一版护栏被自己骗过一次，见文件头）。
	libBody := stripConstantDefinitionLines(stripComments(libSource))

	var unused []string
	for _, match := range constants {
		name := match[1]
		// 用词边界匹配，避免 PET_DOZE_AFTER_MS 被 PET_DOZE_AFTER_MS_OLD 之类的名字蹭到。
		pattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`)
		used := pattern.MatchString(vueSource) || pattern.MatchString(libBody)
		if !used {
			unused = append(unused, name)
		}
	}
	if len(unused) > 0 {
		t.Errorf("这些常量定义在 %s 却没有被 %s 或该文件的模块代码引用：\n  %s\n"+
			"定义了但没人用不会有任何报错——功能只是静默地不存在。要么接上，要么删掉",
			petLibRelativePath, petVueRelativePath, strings.Join(unused, "\n  "))
	}
}

// petConstantDefinitionPattern 匹配 lib/pet.ts 里的常量定义行（含带类型注解的写法）。
var petConstantDefinitionPattern = regexp.MustCompile(`(?m)^export const PET_[A-Z_]+.*$`)

// stripConstantDefinitionLines 去掉常量定义行，只留"用到它"的地方。
//
// 多行定义（如 `export const PET_MIRROR_ROW: Record<…> = {`）只有首行带名字，
// 首行被删掉后块体残留是合法的——护栏只做字符串搜索，不编译。
func stripConstantDefinitionLines(source string) string {
	return petConstantDefinitionPattern.ReplaceAllString(source, "")
}

// ---------------------------------------------------------------------------
// 护栏 29：镜像的类别覆盖与姿态可达（openspec/changes/pet-activity-mirror）
//
// 三类"不报错、只是功能静默不存在"的漂移：
//   1. 映射表漏了一个类别 → 那类活动下宠物毫无反应；
//   2. 映射表出现 Go 侧没有的类别 → 镜像已成第二处真相（Go 改了分类，前端还在按旧的演）；
//   3. 定义了一个姿态但没有任何类别会产出它 → 这个姿态永远不可能出现在屏幕上。
//
// 三条都遵循铁律 5：解析不到东西就 Fatal，不当成"通过"。
// ---------------------------------------------------------------------------

var (
	// petMirrorCategoryBlockStart 匹配映射表的块起始（允许带类型注解）。
	petMirrorCategoryBlockStart = regexp.MustCompile(`(?m)^export const PET_MIRROR_POSE_BY_CATEGORY[^{]*=\s*\{`)
	// petMirrorCategoryEntry 匹配 `[SitCategory.Coding]: 'working',`。
	//
	// 这也是 lib/pet.ts 里要求"值写字符串字面量、不写 PetMirrorPose.Working"的原因：
	// 成员访问会让这条正则解析到 0 条，于是护栏以 Fatal 而不是以"通过"收场。
	petMirrorCategoryEntry = regexp.MustCompile(`(?m)^\[SitCategory\.([A-Za-z]+)\]\s*:\s*'([^']*)'\s*,?\s*$`)
	// petMirrorPoseBlockStart / Entry 解析姿态枚举本身。
	petMirrorPoseBlockStart = regexp.MustCompile(`(?m)^export const PetMirrorPose\s*=\s*\{`)
	petMirrorPoseEntry      = regexp.MustCompile(`(?m)^([A-Za-z]+)\s*:\s*'([^']*)'\s*,?\s*$`)
)

func TestPetMirrorCoversAllCategories(t *testing.T) {
	source := stripComments(readFileOrFail(t, petLibRelativePath))
	byCategory := parseKeyedBlock(t, source, petMirrorCategoryBlockStart, petMirrorCategoryEntry,
		"PET_MIRROR_POSE_BY_CATEGORY")
	poses := parseKeyedBlock(t, source, petMirrorPoseBlockStart, petMirrorPoseEntry, "PetMirrorPose")

	legalPoses := map[string]bool{}
	for _, pose := range poses {
		legalPoses[pose] = true
	}

	for _, category := range sit.AllCategories() {
		key := strings.ToLower(string(category))
		pose, ok := byCategory[key]
		if !ok {
			t.Errorf("类别 %q 在 PET_MIRROR_POSE_BY_CATEGORY 里没有映射——"+
				"该类别下宠物不会有任何反应，且不会有任何报错", key)
			continue
		}
		if pose == "" {
			continue
		}
		if !legalPoses[pose] {
			t.Errorf("类别 %q 映射到姿态 %q，但它不在 PetMirrorPose 里——"+
				"这个姿态永远不会被渲染，也不会报错", key, pose)
		}
	}

	for key := range byCategory {
		found := false
		for _, category := range sit.AllCategories() {
			if strings.ToLower(string(category)) == key {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("映射表里的类别 %q 在 Go 侧 sit.AllCategories() 中不存在——"+
				"镜像已成第二处真相，请删除或改名", key)
		}
	}

	produced := map[string]bool{}
	for _, pose := range byCategory {
		if pose != "" {
			produced[pose] = true
		}
	}
	for name, pose := range poses {
		if !produced[pose] {
			t.Errorf("姿态 PetMirrorPose.%s = %q 没有任何类别会产出它——"+
				"它在 PET_MIRROR_ROW / 样式里是一段永远不会被执行的代码", name, pose)
		}
	}
}

// parseKeyedBlock 解析 `export const X = { Key: 'literal', ... }` 形式的块。
//
// 只认整行条目（与 parity_test.go 的 parseTSObject 同一思路）：不引 TS 解析器，
// 脆弱性由"解析不到即 Fatal"兜住。
func parseKeyedBlock(t *testing.T, source string, start, entry *regexp.Regexp, name string) map[string]string {
	t.Helper()
	loc := start.FindStringIndex(source)
	if loc == nil {
		t.Fatalf("在 %s 里找不到 `%s` 的块起始——写法可能已变更，请同步更新护栏解析规则",
			petLibRelativePath, name)
	}
	out := map[string]string{}
	for _, line := range strings.Split(source[loc[1]:], "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "}") {
			break
		}
		if trimmed == "" {
			continue
		}
		if m := entry.FindStringSubmatch(trimmed); m != nil {
			out[strings.ToLower(m[1])] = m[2]
		}
	}
	if len(out) == 0 {
		t.Fatalf("在 %s 的 `%s` 块里解析到 0 个条目——写法可能已变更，请同步更新护栏解析规则",
			petLibRelativePath, name)
	}
	return out
}

// readPetVueStripped 读取 Pet.vue 并剥掉注释与 import 块。
//
// 必须剥注释：门控里的标识符经常在说明文字里被提到（"这里判 paused"），
// 不剥的话护栏会被自己的注释骗过，永远绿。
//
// 必须剥 import：只 import 不使用的常量同样"没接线"，但它会在 import 块里出现一次。
// 这是本护栏第一版被自己骗过的原因——删掉实际用法后护栏仍然是绿的。
func readPetVueStripped(t *testing.T) string {
	t.Helper()
	return stripImportBlocks(stripComments(readFileOrFail(t, petVueRelativePath)))
}

// importBlockPattern 匹配 import 语句（含跨行的花括号列表，无分号也能匹配）。
var importBlockPattern = regexp.MustCompile("(?s)import\\s+[^;]*?from\\s+['\"][^'\"]+['\"];?")

// stripImportBlocks 去掉所有 import 语句，只留实际代码。
func stripImportBlocks(source string) string {
	return importBlockPattern.ReplaceAllString(source, "")
}

// readFileOrFail 读取项目内相对路径的文件；读不到直接 Fatal（护栏不能靠跳过装绿）。
func readFileOrFail(t *testing.T, relative string) string {
	t.Helper()
	path := filepath.Join(projectRoot(), relative)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v——文件被移动或改名，护栏已经看不见被测代码", relative, err)
	}
	return string(data)
}

// extractTSFunctionBody 从源码里截取某个函数声明的花括号体。
//
// 【局限】按字符做花括号配对，若函数体里出现含 `{` 的模板串或正则字面量，配对会偏早。
// 目前的被解析函数（canPlayMicro）里没有这类写法；真出现时下面的"找不到函数体"分支
// 会以 Fatal 提醒，而不是静默给出一个截断的函数体。
func extractTSFunctionBody(source, signature string) (string, bool) {
	index := strings.Index(source, signature)
	if index < 0 {
		return "", false
	}
	rest := source[index:]
	start := strings.Index(rest, "{")
	if start < 0 {
		return "", false
	}
	depth := 0
	for i := start; i < len(rest); i++ {
		switch rest[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return rest[start : i+1], true
			}
		}
	}
	return "", false
}
