package guard

import (
	"go/ast"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// 护栏 1：分层越界
//   - app.go（绑定方法层）不得 import gorm / internal/database，必须经 service。
//   - model 层是叶子：不得 import internal/service / internal/database。
// ---------------------------------------------------------------------------

// 绑定方法层所在文件：main 包的 app.go。
const bindingsFile = "app.go"

func TestBindingsDoNotTouchDB(t *testing.T) {
	files, err := parseDir(projectRoot())
	if err != nil {
		t.Fatalf("解析项目根目录失败: %v", err)
	}
	f, ok := files[bindingsFile]
	if !ok {
		t.Fatalf("护栏找不到 %s——写法可能已变更，请同步更新护栏解析规则", bindingsFile)
	}
	for _, imp := range collectImports(f) {
		if imp == "gorm.io/gorm" || strings.HasPrefix(imp, "gorm.io/") ||
			strings.Contains(imp, "internal/database") {
			t.Errorf("%s 越界：绑定方法层不得直接 import %s，应经 service 层", bindingsFile, imp)
		}
	}
}

func TestModelIsLeaf(t *testing.T) {
	files, err := parseDir(projectRoot() + "/internal/model")
	if err != nil {
		t.Fatalf("解析 internal/model 失败: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("护栏解析到 0 个 model 文件——写法可能已变更，请同步更新护栏解析规则")
	}
	for name, f := range files {
		for _, imp := range collectImports(f) {
			if strings.Contains(imp, "internal/service") || strings.Contains(imp, "internal/database") {
				t.Errorf("model/%s 越界：模型层是叶子，不得 import %s", name, imp)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// 护栏 2：模型注册双向校验
//   - 每个内嵌 BaseModel 的结构体都必须登记进 AllModels()。
//   - AllModels() 里每个条目都必须真实存在于 model 包。
// ---------------------------------------------------------------------------

func TestModelRegistryBidirectional(t *testing.T) {
	files, err := parseDir(projectRoot() + "/internal/model")
	if err != nil {
		t.Fatalf("解析 internal/model 失败: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("护栏解析到 0 个 model 文件——写法可能已变更，请同步更新护栏解析规则")
	}

	// 集合 A：model 包里所有内嵌 BaseModel 的结构体名
	structsWithBase := map[string]bool{}
	for _, f := range files {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok.String() != "type" {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					continue
				}
				for _, field := range st.Fields.List {
					// 内嵌字段（无名字）且类型为 BaseModel
					if len(field.Names) == 0 {
						if ident, ok := field.Type.(*ast.Ident); ok && ident.Name == "BaseModel" {
							structsWithBase[ts.Name.Name] = true
						}
					}
				}
			}
		}
	}
	// BaseModel 自身不应被登记
	delete(structsWithBase, "BaseModel")

	// 集合 B：AllModels() 里登记的结构体名
	registered := map[string]bool{}
	var modelFile *ast.File
	for name, f := range files {
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if ok && fd.Name.Name == "AllModels" {
				modelFile = f
				_ = name
				collectRegisteredStructs(fd, registered)
			}
		}
	}
	if modelFile == nil {
		t.Fatal("护栏找不到 AllModels() 函数——写法可能已变更，请同步更新护栏解析规则")
	}

	// 正向：每个带 BaseModel 的结构体都必须登记
	for s := range structsWithBase {
		if !registered[s] {
			t.Errorf("模型 %s 内嵌 BaseModel 但未登记进 AllModels()", s)
		}
	}
	// 反向：每个登记项都必须真实存在
	for s := range registered {
		if !structsWithBase[s] {
			t.Errorf("AllModels() 登记了 %s，但 model 包中无对应结构体（僵尸条目/拼写错误）", s)
		}
	}
}

// collectRegisteredStructs 从 AllModels() 函数体里收集登记的结构体名。
// 匹配 `&Asset{}` 或 `&Asset{...}` 这类复合字面量。
func collectRegisteredStructs(fd *ast.FuncDecl, out map[string]bool) {
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		cl, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		// 形如 &Asset{...} 或 Asset{...}；类型若是指针/标识符则取其名
		switch t := cl.Type.(type) {
		case *ast.StarExpr:
			if ident, ok := t.X.(*ast.Ident); ok {
				out[ident.Name] = true
			}
		case *ast.Ident:
			out[t.Name] = true
		}
		return true
	})
}
