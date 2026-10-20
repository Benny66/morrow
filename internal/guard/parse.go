// Package guard 是架构护栏的实现。
//
// 护栏以 go test 的形式运行（见 *_test.go），用 go/parser + go/ast 解析源码做
// 结构化断言。核心铁律：
//
//	护栏必须"感知自己瞎了"——解析到 0 个结果时（写法变更导致匹配失效）必须
//	报错，而非当作"通过"静默放行。见 requireNonEmpty 系列函数。
//
// 本文件是护栏共用的解析辅助，非测试文件，供各 *_test.go 复用。
package guard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
)

// projectRoot 返回项目根目录绝对路径。
// guard 包位于 internal/guard/，项目根即其上一级目录。
func projectRoot() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "."
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

// parseDir 解析目录下所有 .go 文件（排除 _test.go），返回文件名→AST 的映射。
// 目录不存在时返回 nil（由调用方决定是否 Fatal）。
func parseDir(dir string) (map[string]*ast.File, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	files := make(map[string]*ast.File)
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || filepath.Ext(name) != ".go" || isTestFile(name) {
			continue
		}
		path := filepath.Join(dir, name)
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		files[name] = f
	}
	return files, nil
}

// isTestFile 判断是否为测试文件。
func isTestFile(name string) bool {
	return len(name) > len("_test.go") && name[len(name)-len("_test.go"):] == "_test.go"
}

// collectImports 收集单个文件的全部 import 路径。
func collectImports(f *ast.File) []string {
	var out []string
	for _, imp := range f.Imports {
		// import 路径去掉引号
		p := imp.Path.Value
		if len(p) >= 2 {
			p = p[1 : len(p)-1]
		}
		out = append(out, p)
	}
	return out
}
