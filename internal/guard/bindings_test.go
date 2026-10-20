package guard

import (
	"go/ast"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// 护栏 3：前端绑定必须与 Go 侧真实一致
//
// v2 与 v3 的失效形态不同，护栏的判据也必须跟着换：
//
//	v2：绑定产物是 frontend/wailsjs/go/main/App.d.ts，按【方法名】寻址。
//	    护栏只要断言方法名出现在 .d.ts 里。
//	v3：绑定产物是 frontend/bindings/<module>/app.ts，按【数字 ID】寻址——
//	      export function Confirm(...) { return $Call.ByID(3228319335, ...) }
//	    名字失效（改方法名/改包路径）时，页面上的调用会打到不存在的 ID，
//	    【但这是运行期才炸的，编译期完全看不出来】——这正是 v3 比 v2 更危险的地方。
//
// 好在 ID 是【可复算的】：wails 用
//     id = fnv32a("<pkgPath>.<TypeName>.<MethodName>")
// （见 wails/v3 pkg/application/bindings.go 的 `methodID := hash.Fnv(fqn)`）。
// 故本护栏不但查名字，还把 ID 复算出来逐条对账——「名字对但 ID 过期」也拦得住。
//
// 铁律：解析到 0 个目标必须报错（护栏感知自己瞎了）。
// ---------------------------------------------------------------------------

// frameworkExcludedAppMethods 是 wails 框架从绑定里【排除】的方法名。
//
// 镜像自 wails/v3 pkg/application/bindings.go 的 internalServiceMethods：
//
//	{"ServiceName", "ServiceStartup", "ServiceShutdown", "ServeHTTP"}
//
// 必须镜像而不是"忽略掉几个名字"：v3 的 App 是 Service，ServiceStartup /
// ServiceShutdown 是它的生命周期钩子——它们是【导出方法】，但框架不把它们交给前端。
// 护栏若不知道这条，会把它们当成"缺失的绑定"而误报。若框架日后扩大这个集合，
// 本护栏会因"生成了额外的绑定"而报错，提示同步——比静默错放好。
var frameworkExcludedAppMethods = map[string]bool{
	"ServiceName":     true,
	"ServiceStartup":  true,
	"ServiceShutdown": true,
	"ServeHTTP":       true,
}

// fnv32a 复算 wails 的方法 ID：FNV-1a 32 位。
// 与 wails/v3 internal/hash/fnv.go 的 Fnv() 等价。
func fnv32a(s string) uint32 {
	var h uint32 = 2166136261
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}

// TestBindingMethodsHaveGeneratedBindings 断言 app.go 的每个导出绑定方法都在生成的
// 绑定产物里有【同名且 ID 一致】的函数，且产物里没有多余的残留。
func TestBindingMethodsHaveGeneratedBindings(t *testing.T) {
	root := projectRoot()

	files, err := parseDir(root)
	if err != nil {
		t.Fatalf("解析项目根目录失败: %v", err)
	}
	appFile, ok := files[bindingsFile]
	if !ok {
		t.Fatalf("护栏找不到 %s——写法可能已变更，请同步更新护栏解析规则", bindingsFile)
	}

	pkgPath := packagePathFor(t, root, appFile)
	exported := exportedAppMethods(appFile)
	if len(exported) == 0 {
		// 合法空态：尚无绑定方法
		return
	}

	bindingsPath := filepath.Join(root, "frontend", "bindings", modulePath(t, root), "app.ts")
	data, err := os.ReadFile(bindingsPath)
	if err != nil {
		t.Fatalf("app.go 有 %d 个导出绑定方法，但找不到生成的绑定产物 %s\n"+
			"（请运行：make dev 或 wails3 generate bindings -clean=true -ts -i）",
			len(exported), bindingsPath)
	}
	generated := parseGeneratedBindings(t, string(data))

	// 正向：每个导出方法都要有绑定，且 ID 必须与复算值一致
	for _, m := range exported {
		got, ok := generated[m]
		if !ok {
			t.Errorf("绑定方法 %s 未在 %s 中找到——绑定过期，请重新生成", m, bindingsPath)
			continue
		}
		want := fnv32a(pkgPath + ".App." + m)
		if got != want {
			t.Errorf("%s 的绑定 ID 过期：产物里是 %d，按当前方法名/包路径复算应为 %d。\n"+
				"  说明方法与生成产物对不上了（改了方法名或移动了 App？），"+
				"运行期会打到不存在的 ID——【编译期不会报错】。请重新生成绑定。",
				m, got, want)
		}
	}

	// 反向：产物里多出的条目（Go 侧已删/改名）也要报——否则产物会退化成第二处真相
	for m := range generated {
		if !contains(exported, m) {
			t.Errorf("绑定产物里有 %s，但 app.go 已无此导出方法——产物过期，请重新生成", m)
		}
	}
}

// packagePathFor 返回绑定 FQN 里的包路径段。
//
// wails 取的是 reflect.Type.PkgPath()：
//   - package main → 字面量 "main"（已对着本项目产物复算 11/11 命中）
//   - 其余包 → 模块路径 + 相对目录
//
// App 若哪天真被挪进子包，这里能跟着算对，而不用改护栏逻辑。
func packagePathFor(t *testing.T, root string, f *ast.File) string {
	t.Helper()
	if f.Name.Name == "main" {
		return "main"
	}
	return modulePath(t, root)
}

// modulePath 从 go.mod 读模块路径（绑定目录名与子包 FQN 都由它派生）。
func modulePath(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("读取 go.mod 失败: %v", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			m := strings.TrimSpace(strings.TrimPrefix(line, "module "))
			if m == "" {
				t.Fatal("go.mod 的 module 行为空——绑定目录名无从派生，请同步修复")
			}
			return m
		}
	}
	t.Fatal("go.mod 里找不到 module 行——写法可能已变更，请同步更新护栏解析规则")
	return ""
}

// generatedBindingRe 从绑定产物里抓「当前函数名」与「它用的 ID」。
var (
	generatedFuncRe = regexp.MustCompile(`^export function ([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	generatedIDRe   = regexp.MustCompile(`\$Call\.ByID\((\d+)`)
)

// parseGeneratedBindings 解析绑定产物，返回 方法名 → 调用 ID。
//
// 行级两遍扫描：先认 `export function Name(`，再在其后几行里找 `$Call.ByID(<id>)`。
// 不用 TS 解析器是为了不引第三方依赖；脆弱性由「解析不到即 Fatal」兜住。
func parseGeneratedBindings(t *testing.T, content string) map[string]uint32 {
	t.Helper()

	out := map[string]uint32{}
	current := ""
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if m := generatedFuncRe.FindStringSubmatch(trimmed); m != nil {
			current = m[1]
			continue
		}
		if current == "" {
			continue
		}
		if m := generatedIDRe.FindStringSubmatch(trimmed); m != nil {
			id, err := strconv.ParseUint(m[1], 10, 32)
			if err != nil {
				t.Fatalf("绑定产物里的 ID %q 不是合法的 32 位整数: %v", m[1], err)
			}
			out[current] = uint32(id)
			current = ""
		}
	}

	if len(out) == 0 {
		t.Fatal("绑定产物里解析到 0 个 `export function ... $Call.ByID(...)`——" +
			"生成器的输出格式可能已变更，请同步更新护栏解析规则")
	}
	return out
}

// contains 判断字符串切片里是否含目标。
func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// exportedAppMethods 收集 App 结构体的导出绑定方法名。
//
// 排除两类：
//   - 非导出方法（首字母小写）——v3 只绑导出方法；
//   - 框架排除的生命周期方法（见 frameworkExcludedAppMethods）。
func exportedAppMethods(f *ast.File) []string {
	var out []string
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Recv == nil || len(fd.Recv.List) == 0 {
			continue
		}
		// 接收者类型是否为 *App
		recv := fd.Recv.List[0].Type
		var recvName string
		switch t := recv.(type) {
		case *ast.StarExpr:
			if ident, ok := t.X.(*ast.Ident); ok {
				recvName = ident.Name
			}
		case *ast.Ident:
			recvName = t.Name
		}
		if recvName != "App" {
			continue
		}
		// 导出方法 = 首字母大写
		name := fd.Name.Name
		if name[0] < 'A' || name[0] > 'Z' {
			continue
		}
		// 框架自己从绑定里排除的生命周期方法不算绑定
		if frameworkExcludedAppMethods[name] {
			continue
		}
		out = append(out, name)
	}
	return out
}
