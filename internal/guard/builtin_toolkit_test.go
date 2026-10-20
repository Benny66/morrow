package guard

import (
	"go/ast"
	"testing"
)

// ---------------------------------------------------------------------------
// 护栏 24–26：内置功能第一批（openspec/changes/builtin-toolkit）的接线护栏。
//
// 三条共同点是「删掉完全无症状」：
//   - MCP token 透传删了，鉴权只是静默不生效（MCP 仅 dev-mcp 构建存在，日常无感）；
//   - macOS 应用菜单删了，输入框 Cmd+C/V 只是"本来就用不了"；
//   - SetTheme 的空串分支被收紧，前端「跟随系统」档只是收到一个 4xx。
// 均符合铁律 5 的适用面（没有任何症状的失效），故用源码级断言兜住。
// ---------------------------------------------------------------------------

// TestMCPTokenPassthroughIsWired 断言 main.go 把配置里的 MCP token 透传给运行时 MCP。
//
// 断言到【实参是字面量 "WAILS_MCP_TOKEN"】这一层：环境变量名是上游 mcp_enabled.go
// 约定的协议，改名即断链；而透传的值必须是 cfg.MCPToken（用 AST 确认第二个实参
// 含 MCPToken 选择器，防止被改成写死的空串/占位值）。
func TestMCPTokenPassthroughIsWired(t *testing.T) {
	files, err := parseDir(projectRoot())
	if err != nil {
		t.Fatalf("解析项目根目录失败: %v", err)
	}
	f, ok := files["main.go"]
	if !ok {
		t.Fatal("护栏找不到 main.go——写法可能已变更，请同步更新护栏解析规则")
	}

	found := false
	valueFromConfig := false
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if selectorPath(call.Fun) != "os.Setenv" {
			return true
		}
		if len(call.Args) != 2 {
			return true
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok || lit.Value != `"WAILS_MCP_TOKEN"` {
			return true
		}
		found = true
		if containsSelector(call.Args[1], "MCPToken") {
			valueFromConfig = true
		}
		return true
	})

	if !found {
		t.Error("main.go 未找到 os.Setenv(\"WAILS_MCP_TOKEN\", …)——MCP 鉴权透传被删除，" +
			"配置的 mcp_token 将静默失效（必须在 application.New 之前透传）")
	}
	if found && !valueFromConfig {
		t.Error("os.Setenv 的值不来自配置（cfg.MCPToken）——透传被改成了固定值")
	}
}

// TestMacEditMenuIsWired 断言 main.go 挂载了 macOS 应用菜单（AppMenu + EditMenu role）。
//
// 为什么值得一条护栏：不挂菜单，webview 输入框的 Cmd+C/V/X/A/Z 全军覆没，
// 且应用照常启动、无任何报错——正是「无症状失效」。
func TestMacEditMenuIsWired(t *testing.T) {
	files, err := parseDir(projectRoot())
	if err != nil {
		t.Fatalf("解析项目根目录失败: %v", err)
	}
	f, ok := files["main.go"]
	if !ok {
		t.Fatal("护栏找不到 main.go——写法可能已变更，请同步更新护栏解析规则")
	}

	var setMenu, editRole, appRole bool
	ast.Inspect(f, func(n ast.Node) bool {
		switch e := n.(type) {
		case *ast.CallExpr:
			switch selectorPath(e.Fun) {
			case "wailsApp.Menu.SetApplicationMenu":
				setMenu = true
			case "appMenu.AddRole":
				if len(e.Args) == 1 {
					switch selectorPath(e.Args[0]) {
					case "application.EditMenu":
						editRole = true
					case "application.AppMenu":
						appRole = true
					}
				}
			}
		}
		return true
	})

	if !setMenu {
		t.Error("main.go 未找到 wailsApp.Menu.SetApplicationMenu(...)——macOS 应用菜单被移除，" +
			"输入框的 Cmd+C/V/X/A 快捷键将全部失效")
	}
	if !editRole {
		t.Error("应用菜单未包含 application.EditMenu role——编辑快捷键没有承接项")
	}
	if !appRole {
		t.Error("应用菜单未包含 application.AppMenu role——macOS 菜单栏缺少标准应用项")
	}
}

// TestSetThemeAcceptsFollowSystem 断言 app.go 的 SetTheme 校验分支接受空串
// （主题三档的 Go 侧锚点：空串 = 跟随系统）。
//
// 前端「跟随系统」档存的就是空串；这里被收紧成只收 dark/light 时，
// 用户每次切到跟随系统都会拿到一个校验错误——配置层看起来"更严谨"了，
// 实际是砍掉了一个合法档位。
func TestSetThemeAcceptsFollowSystem(t *testing.T) {
	files, err := parseDir(projectRoot())
	if err != nil {
		t.Fatalf("解析项目根目录失败: %v", err)
	}
	f, ok := files["app.go"]
	if !ok {
		t.Fatal("护栏找不到 app.go——写法可能已变更，请同步更新护栏解析规则")
	}

	decl := findMethodDecl(f, "SetTheme")
	if decl == nil {
		t.Fatal("护栏在 app.go 中未找到 SetTheme 方法——写法可能已变更，请同步更新护栏解析规则")
	}

	acceptsEmpty := false
	acceptsDarkLight := 0
	ast.Inspect(decl.Body, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind.String() != "STRING" {
			return true
		}
		switch lit.Value {
		case `""`:
			acceptsEmpty = true
		case `"dark"`:
			acceptsDarkLight++
		case `"light"`:
			acceptsDarkLight++
		}
		return true
	})

	if !acceptsEmpty {
		t.Error("SetTheme 校验分支不再接受空串——「跟随系统」档（theme=\"\"）会被静默拒绝")
	}
	if acceptsDarkLight < 2 {
		t.Errorf("SetTheme 校验分支对 dark/light 的覆盖不完整（找到 %d 处）——三档语义被破坏", acceptsDarkLight)
	}
}

// containsSelector 报告 n 内是否引用了以 name 结尾的选择器/标识符（如 "MCPToken"）。
func containsSelector(n ast.Node, name string) bool {
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		switch e := x.(type) {
		case *ast.SelectorExpr:
			if e.Sel.Name == name {
				found = true
			}
		case *ast.Ident:
			if e.Name == name {
				found = true
			}
		}
		return !found
	})
	return found
}
