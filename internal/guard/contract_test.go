package guard

import (
	"go/ast"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// 护栏 6：管道契约（pipeline-contract）
//   - 分页结构体字段齐全（少字段会破坏前端分页渲染）。
//   - 页大小上下界常量存在（单一真相）。
//   - 绑定层（app.go）不得用 errors.New / fmt.Errorf 直接造业务错误，
//     必须经 apperr 构造（否则前端拿不到结构化 code）。
//
// 铁律：解析到 0 个目标必须报错（护栏感知自己瞎了）。
// ---------------------------------------------------------------------------

// TestPageContractShape 断言分页契约的结构形状。
func TestPageContractShape(t *testing.T) {
	files, err := parseDir(projectRoot() + "/internal/page")
	if err != nil {
		t.Fatalf("解析 internal/page 失败: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("护栏解析到 0 个 page 文件——写法可能已变更，请同步更新护栏解析规则")
	}

	// Result[T] 的必备字段（JSON tag → 结构体字段名）
	wantResultFields := map[string]string{
		"list":      "List",
		"total":     "Total",
		"page":      "Page",
		"page_size": "PageSize",
	}
	// Request 的必备字段
	wantRequestFields := map[string]string{
		"page":      "Page",
		"page_size": "PageSize",
	}

	gotResult := map[string]bool{}
	gotRequest := map[string]bool{}

	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSpec)
			if !ok {
				return true
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				return true
			}
			// 收集该结构体的 json tag → 字段名
			tags := map[string]string{}
			for _, field := range st.Fields.List {
				if field.Tag == nil {
					continue
				}
				tag := strings.Trim(field.Tag.Value, "`")
				if jsonKey := jsonTagName(tag); jsonKey != "" && len(field.Names) == 1 {
					tags[jsonKey] = field.Names[0].Name
				}
			}
			switch ts.Name.Name {
			case "Result":
				gotResult = mapContains(tags, wantResultFields)
			case "Request":
				gotRequest = mapContains(tags, wantRequestFields)
			}
			return true
		})
	}

	if len(gotResult) == 0 {
		t.Errorf("分页契约 Result 结构体缺失或字段不全——期望含 %v", wantResultFields)
	} else {
		reportMissing(t, "Result", wantResultFields, gotResult)
	}
	if len(gotRequest) == 0 {
		t.Errorf("分页契约 Request 结构体缺失或字段不全——期望含 %v", wantRequestFields)
	} else {
		reportMissing(t, "Request", wantRequestFields, gotRequest)
	}
}

// TestPageSizeConstantsExist 断言页大小上下界常量存在（单一真相）。
func TestPageSizeConstantsExist(t *testing.T) {
	files, err := parseDir(projectRoot() + "/internal/page")
	if err != nil {
		t.Fatalf("解析 internal/page 失败: %v", err)
	}

	want := []string{"DefaultPageSize", "MaxPageSize"}
	found := map[string]bool{}
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			vs, ok := n.(*ast.ValueSpec)
			if !ok {
				return true
			}
			for _, name := range vs.Names {
				for _, w := range want {
					if name.Name == w {
						found[w] = true
					}
				}
			}
			return true
		})
	}

	if len(found) == 0 {
		t.Fatal("护栏在 internal/page 未解析到任何页大小常量——写法可能已变更，请同步更新护栏解析规则")
	}
	for _, w := range want {
		if !found[w] {
			t.Errorf("页大小常量 %s 缺失——契约要求上下界为单一真相", w)
		}
	}
}

// TestBindingsUseAppErr 断言绑定层不用 errors.New / fmt.Errorf 直接造业务错误。
// 业务错误必须经 apperr 构造，否则前端拿不到结构化 code。
func TestBindingsUseAppErr(t *testing.T) {
	files, err := parseDir(projectRoot())
	if err != nil {
		t.Fatalf("解析项目根目录失败: %v", err)
	}
	f, ok := files[bindingsFile]
	if !ok {
		t.Fatalf("护栏找不到 %s——写法可能已变更，请同步更新护栏解析规则", bindingsFile)
	}

	// 确认确实解析到了 App 方法（避免"瞎了"却静默通过）
	methodCount := 0
	for _, decl := range f.Decls {
		if fd, ok := decl.(*ast.FuncDecl); ok && fd.Recv != nil {
			methodCount++
		}
	}
	if methodCount == 0 {
		t.Fatalf("护栏在 %s 未解析到任何方法——写法可能已变更，请同步更新护栏解析规则", bindingsFile)
	}

	banned := map[string]string{
		"errors.New": "apperr.New/NotFound/Validation/Conflict",
		"fmt.Errorf": "apperr.Wrap",
	}
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		name := callName(call)
		if hint, hit := banned[name]; hit {
			t.Errorf("%s 中直接调用 %s 造错误——业务错误必须经 %s，否则前端拿不到结构化 code",
				bindingsFile, name, hint)
		}
		return true
	})
}

// --- 辅助 ---

// callName 返回调用表达式的 "pkg.Func" 形式名称（仅处理包限定调用）。
func callName(call *ast.CallExpr) string {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	pkgIdent, ok := sel.X.(*ast.Ident)
	if !ok {
		return ""
	}
	return pkgIdent.Name + "." + sel.Sel.Name
}

// jsonTagName 从结构体 tag 中提取 json 名（忽略 omitempty 等选项）。
func jsonTagName(tag string) string {
	const key = `json:"`
	i := strings.Index(tag, key)
	if i < 0 {
		return ""
	}
	rest := tag[i+len(key):]
	if j := strings.Index(rest, `"`); j >= 0 {
		rest = rest[:j]
	}
	if c := strings.Index(rest, ","); c >= 0 {
		rest = rest[:c]
	}
	return rest
}

// mapContains 检查 tags 是否涵盖 want 的全部键，返回「命中」的 JSON 键集合。
func mapContains(tags map[string]string, want map[string]string) map[string]bool {
	got := map[string]bool{}
	for jsonKey, fieldName := range want {
		if tags[jsonKey] == fieldName {
			got[jsonKey] = true
		}
	}
	return got
}

// reportMissing 报告缺失字段。
func reportMissing(t *testing.T, structName string, want map[string]string, got map[string]bool) {
	for jsonKey, fieldName := range want {
		if !got[jsonKey] {
			t.Errorf("分页契约 %s 缺少字段 %s（json:%q）——删字段会破坏前端分页渲染",
				structName, fieldName, jsonKey)
		}
	}
}
