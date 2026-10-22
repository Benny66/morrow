package guard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// 护栏：预警措辞只允许有一个出处
//
// 背景：「1 分钟后该起身拉伸了」这类预警文案曾经同时存在两份——
// internal/restmode 一份给宠物气泡，app.go 的菜单栏再写死一份「1 分钟后休息」。
// 两份都会各自演进，结果是同一时刻宠物说「马上」、菜单栏说「1 分钟后」，
// 而代码里没有任何一处会因此报错（AGENTS.md 铁律 1 的单一真相被绕过）。
//
// 判据：
//   - 正向：措辞字面量在【生产代码】里只允许出现在 internal/restmode；
//   - 反向（铁律：护栏必须感知自己瞎了）：internal/restmode 里解析到的措辞
//     少于下限即 Fatal——那说明写法变了（改成模板拼接、挪去常量表、删掉分档都会
//     让命中数掉下去），此时本护栏其实已经看不清代码，必须报错而不是静默通过。
//
// 范围取舍：
//   - 跳过 _test.go：测试里必须能写出期望的字面量当独立真相源（见
//     internal/restmode/warning_test.go 的三档表）；
//   - Go 侧只看字符串字面量（AST），注释不参与——注释里引用旧文案是文档问题，
//     不该把护栏变成 grep 注释；
//   - 前端没有 AST，先剥掉注释再按引号内容扫，避免 `// 「1 分钟后」` 这类说明文字误报。
// ---------------------------------------------------------------------------

// warningWordingPattern 是预警措辞的字面特征。
//
// 【为什么锚在句首而不是搜「分钟后」】久坐软提醒里有一句合法的「好，10 分钟后再提醒你」
// （sit_runtime.go 的动作确认），搜"分钟后"会把它误判成越界出处。预警措辞的形态是三档
// 句首——「1 分钟后…」「半分钟后…」「马上…」——锚住句首就只抓得到它。
// 代价：将来若给预警文案加前缀装饰（emoji、场景名），这条正则需要同步放宽，
// 而那属于"写法变更"，正该由下面的下限检查一起提醒。
var warningWordingPattern = regexp.MustCompile(`^(1 分钟后|半分钟后|马上)`)

// restmodeWordingFloor 是 internal/restmode 生产代码里应当存在的措辞字面量下限。
//
// 【为什么是 12】预警措辞是三档 × 四个分支（stand/move/rest + 兜底），
// 恰好 12 句。低于这个数就说明分档表被改写了形态，护栏的解析规则需要跟着同步。
const restmodeWordingFloor = 12

// 不参与扫描的目录：构建产物、依赖与文档。
var wordingSkipDirs = map[string]bool{
	".git": true, ".scratch": true, "docs": true, "build": true,
	"node_modules": true, "dist": true, "vendor": true,
}

func TestWarningWordingHasSingleSource(t *testing.T) {
	root := projectRoot()
	restmodeDir := filepath.Join(root, "internal", "restmode")

	if info, err := os.Stat(restmodeDir); err != nil || !info.IsDir() {
		t.Fatalf("解析起点 internal/restmode 不存在或不可读（%v）——护栏已瞎，写法可能已变更，请同步更新本护栏的解析规则", err)
	}

	var offenders []string
	homeHits := 0

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// 【按目录名跳过，不看深度】frontend/node_modules 与 frontend/dist 都是
			// 嵌套在第一层目录之下，早先用 rel 精确匹配顶层名字的做法会整片扫进依赖里
			// ——既慢（几万个文件）又不稳定：任何第三方包里的中文串都可能被判成越界出处。
			base := filepath.Base(path)
			if wordingSkipDirs[base] || strings.HasPrefix(base, ".") {
				return fs.SkipDir
			}
			return nil
		}

		name := d.Name()
		if isTestFile(name) {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}

		switch filepath.Ext(name) {
		case ".go":
			inHome := strings.HasPrefix(filepath.ToSlash(rel), "internal/restmode/")
			for _, lit := range goStringLiterals(t, path, data) {
				if !warningWordingPattern.MatchString(lit) {
					continue
				}
				if inHome {
					homeHits++
					continue
				}
				offenders = append(offenders, rel+"（"+trimLiteral(lit)+"）")
			}
		case ".ts", ".vue":
			for _, lit := range quotedSnippets(stripComments(string(data))) {
				if !warningWordingPattern.MatchString(lit) {
					continue
				}
				offenders = append(offenders, rel+"（"+trimLiteral(lit)+"）")
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("遍历项目源码失败: %v", err)
	}

	if homeHits < restmodeWordingFloor {
		t.Fatalf("internal/restmode 里只解析到 %d 条预警措辞，低于下限 %d 条——"+
			"三档 × 四个分支的措辞表可能已被改写或移走，护栏已经看不清代码；"+
			"写法若真的变了，请同步更新本护栏的解析规则，不要靠调低下限蒙过去",
			homeHits, restmodeWordingFloor)
	}
	if len(offenders) > 0 {
		t.Errorf("预警措辞只能在 internal/restmode 定义（单一真相，AGENTS.md 铁律 1），发现越界出处：\n  %s\n"+
			"要显示预警请取 restmode.Snapshot().CueText，别再抄一份文案",
			strings.Join(offenders, "\n  "))
	}
}

// goStringLiterals 解析源码并返回所有字符串字面量的值。
func goStringLiterals(t *testing.T, path string, src []byte) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, string(src), parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("解析 %s 失败: %v", path, err)
	}
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		value, err := strconv.Unquote(lit.Value)
		if err != nil {
			// 反引号里的原始串：去掉首尾反引号即可参与匹配。
			value = strings.Trim(lit.Value, "`")
		}
		out = append(out, value)
		return true
	})
	return out
}

// stripComments 剥掉 // 行注释与 /* */ 块注释（含 HTML 的 <!-- -->），
// 让前端扫描只看代码而不是说明文字。
func stripComments(s string) string {
	s = regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(s, "")
	s = regexp.MustCompile(`(?s)<!--.*?-->`).ReplaceAllString(s, "")
	var keep []string
	for _, line := range strings.Split(s, "\n") {
		if idx := strings.Index(line, "//"); idx >= 0 {
			line = line[:idx]
		}
		keep = append(keep, line)
	}
	return strings.Join(keep, "\n")
}

// quotedSnippets 返回成对引号（' " `）之间的内容。
var quotedPattern = regexp.MustCompile("`[^`]*`|\"[^\"]*\"|'[^']*'")

func quotedSnippets(s string) []string {
	var out []string
	for _, m := range quotedPattern.FindAllString(s, -1) {
		out = append(out, m[1:len(m)-1])
	}
	return out
}

// trimLiteral 只为把报错文案压短，不参与判据。
func trimLiteral(s string) string {
	s = strings.TrimSpace(s)
	if len([]rune(s)) <= 24 {
		return s
	}
	return string([]rune(s)[:24]) + "…"
}
