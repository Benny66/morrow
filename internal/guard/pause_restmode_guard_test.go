package guard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestPauseReasonsHavePriority 是 spec §测试决策·护栏 里那条
// 「暂停信号提供者必须实现 PauseReason 接口」的落地。
//
// 具体断言：
//   - internal/pause 里每个导出的 Reason* 常量（ReasonNone 除外）都必须出现在
//     composite.go 的 reasonPriority map 中；
//   - 若新增 ReasonFocus / ReasonCalendar 这类信号时忘了在 CompositeProvider
//     的优先级里注册，多个信号同时触发时 Composite 会拿 0 作为默认优先级，
//     导致「新信号永远最优先」这类没有症状的误判。
//
// 【为什么不用运行时 reflect】PauseReason 是 string 类型，靠运行时只能拿到当前
// 包内被引用的值；用 AST 解析 const 声明能覆盖「已定义但从未被引用的新枚举」。
//
// 铁律：解析到 0 个 Reason* 常量必须报错（护栏感知自己瞎了）。
func TestPauseReasonsHavePriority(t *testing.T) {
	root := projectRoot()
	pauseDir := filepath.Join(root, "internal", "pause")

	// 1. 从 internal/pause 的所有非测试 go 文件里收集 Reason* 常量。
	files, err := parseDir(pauseDir)
	if err != nil {
		t.Fatalf("解析 %s 失败: %v", pauseDir, err)
	}
	reasons := map[string]bool{}
	for name, f := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || len(vs.Names) != len(vs.Values) {
					continue
				}
				for _, id := range vs.Names {
					if strings.HasPrefix(id.Name, "Reason") && id.Name != "ReasonNone" {
						reasons[id.Name] = true
					}
				}
			}
		}
	}
	if len(reasons) == 0 {
		t.Fatal("护栏在 internal/pause 未解析到任何 Reason* 常量——写法可能已变更，请同步更新护栏解析规则")
	}

	// 2. 从 composite.go 的 reasonPriority map 字面量里收集已注册的 Reason 名。
	prioPath := filepath.Join(pauseDir, "composite.go")
	prioFile, err := parser.ParseFile(token.NewFileSet(), prioPath, nil, 0)
	if err != nil {
		t.Fatalf("解析 composite.go 失败: %v", err)
	}
	registered := map[string]bool{}
	ast.Inspect(prioFile, func(n ast.Node) bool {
		cl, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		for _, elt := range cl.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			if id, ok := kv.Key.(*ast.Ident); ok && strings.HasPrefix(id.Name, "Reason") {
				registered[id.Name] = true
			}
		}
		return true
	})
	if len(registered) == 0 {
		t.Fatalf("护栏在 %s 未解析到任何 reasonPriority 字面量——map 写法可能已变更，请同步更新护栏解析规则", prioPath)
	}

	// 3. 正向：每个 Reason*（除 None）都必须注册优先级。
	for r := range reasons {
		if !registered[r] {
			t.Errorf("PauseReason %s 未在 composite.go 的 reasonPriority 中注册——"+
				"新增信号时漏加优先级会让它在多信号同时触发时拿到默认值 0", r)
		}
	}
}

// TestRestModeStatesCoverFiveSpecStates 是 spec「休息模式状态机必须覆盖
// 未开始 / 预警 / 倒计时 / 完成 / 跳过」的落地。
//
// spec 里列了 5 个；实现中额外加了 early_completed（见 CONTEXT.md 的说明），
// 所以本护栏的判据是「spec 5 态必须全部存在」，允许实现多出别的（比如
// early_completed），但不允许少了 spec 要求的那几个。
func TestRestModeStatesCoverFiveSpecStates(t *testing.T) {
	root := projectRoot()
	dir := filepath.Join(root, "internal", "restmode")
	files, err := parseDir(dir)
	if err != nil {
		t.Fatalf("解析 %s 失败: %v", dir, err)
	}
	// Go 常量名 → 字面值（如 StateIdle → "idle"）。
	have := map[string]bool{}
	for name, f := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || len(vs.Names) != len(vs.Values) {
					continue
				}
				for i, id := range vs.Names {
					if !strings.HasPrefix(id.Name, "State") {
						continue
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					v, err := strconv.Unquote(lit.Value)
					if err != nil {
						continue
					}
					have[v] = true
				}
			}
		}
	}
	if len(have) < 5 {
		t.Fatalf("护栏在 internal/restmode 只解析到 %d 个 State 常量——写法可能已变更，请同步更新护栏解析规则", len(have))
	}

	// spec §测试决策·护栏 明写的 5 态。特别注意 spec 用 "skip" 而实现用 "skipped"：
	// 语义一致，字面量按实现取。若将来有人把 skipped 改名回 skip，这条测试会报红。
	required := []string{"idle", "warning", "countdown", "done", "skipped"}
	for _, want := range required {
		if !have[want] {
			t.Errorf("休息模式缺少 spec 要求的状态 %q；实际状态: %v", want, keysOf(have))
		}
	}
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
