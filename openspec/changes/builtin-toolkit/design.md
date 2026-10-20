# design — 内置功能第一批

## 上游 API 核实（beta.23 模块缓存逐个读过，非凭记忆）

| 能力 | API | 核实结果 |
|------|-----|---------|
| 剪贴板 | `app.Clipboard.SetText(string) bool` / `Text() (string, bool)` | clipboard_manager.go |
| 全局快捷键 | `app.GlobalShortcut.Register(acc, cb) error` / `Unregister(acc) error` | global_shortcut_manager.go，回调在应用事件循环里派发 |
| 自启 | `app.Autostart.EnableWithOptions(AutostartOptions) error` / `Disable() error` / `IsEnabled() (bool, error)` | autostart_manager.go；macOS 走 SMAppService（要求打包后的 .app，dev 态会报错——按错误如实返回） |
| 通知 | `pkg/services/notifications`，`New()` 构造、`RequestNotificationAuthorization()`、`SendNotification(NotificationOptions)` | notifications.go；作为 Service 注册进 Options.Services |
| 应用菜单 | `app.Menu.New()` + `AddRole(application.AppMenu/EditMenu)` + `SetApplicationMenu(menu)` | roles.go / menu_manager.go；EditMenu role 展开为 Undo/Redo/Cut/Copy/Paste/SelectAll |
| MCP 鉴权 | `-tags mcp` 构建下 `WAILS_MCP_TOKEN` 环境变量，设置后所有请求要 Bearer | mcp_enabled.go（validateMCPBind + authToken 中间件） |

## Decisions

### D1 主题三档：Go 侧零改动，前端补「跟随系统」

Go 的 `SetTheme` 本就接受 `""`（语义=跟随系统），配置层已就绪。变更集中在前端：
`stores/app.ts` 把「config 存的值（模式）」与「实际生效值（解析后的 dark/light）」分开——
`themeMode` 存用户选择，`theme` 存解析结果（写 `data-theme`）。模式为 `''` 时挂
`matchMedia('(prefers-color-scheme: dark)')` 监听跟随系统；切到显式档位时摘掉监听。

### D2 剪贴板：绑定方法薄封装，不做 internal 模块

两行转发（`app.Clipboard.SetText/Text`）不值得开 internal 包；直接放 app.go 绑定方法区。
设置页示范挂在「数据目录」行：「复制路径」按钮。

### D3 通知：Service 注册 + App 持有实例

`notifications.New()` 在 main.go 组合根创建，注册进 `Options.Services`（框架负责
ServiceStartup/Shutdown），同一实例赋给 `App.notify`（同包 main，直接赋值导出字段）。
绑定方法：`RequestNotificationPermission()`（显式请求授权）与 `SendNotification(title, body)`。
macOS 打包态可用；dev 态/无 bundle 时授权会报错——如实返回错误，不吞。

### D4 全局快捷键：单个「唤起/隐藏主窗口」快捷键，配置驱动

- 配置项 `shortcut_toggle_window`（默认空串=不注册）。
- `SetToggleWindowShortcut(acc)`：先注销旧注册→注册新的→成功才落盘；注册失败保持原状并返回错误。
- 回调：主窗口可见 → 隐藏到托盘（托盘不在则不动，避免重演 R3 无窗口进程）；不可见 → `restoreWindow()`（四路径共用的单一实现）。
- 快捷键串格式是 v3 的 accelerator 语法（如 `CmdOrCtrl+Shift+Space`），合法性由 `Register` 的返回错误兜底，不自写解析器。

### D5 自启：薄封装 + 平台差异如实呈现

`GetAutostartEnabled` / `SetAutostart(enabled)` 转发 `app.Autostart`，`AutostartOptions`
用默认派生（macOS 用 bundle id）。平台差异（macOS 需打包态、SMAppService 系统偏好里可关）
写进设置页提示文案，由方法错误如实兜底。

### D6 数据库恢复：与导出成对，失败可回滚

`service.ImportDatabase(appName, source)` 流程（export.go 的镜像操作）：

1. 源文件非空、存在；拒绝位于数据目录内（与导出同规则，防把库文件当源复制到自己身上）。
2. 关闭当前连接（`Service.Close`）。
3. 现库改名 `*.pre-import`（同目录原子操作），并清掉 `-wal/-shm` 残留。
4. `io.Copy` 把源复制到位。
5. `database.Init` 重新打开并迁移；**失败则回滚**：删除复制进来的文件、把 `*.pre-import` 改名回来、重开旧库，返回原始错误。
6. 成功：替换 `s.db`，保留 `*.pre-import` 一份作为兜底备份（下次导入覆盖）。

已知限制（写进 spec 与设置页文案）：恢复后前端已加载的状态是旧的，建议重启应用。

### D7 MCP Token：启动期透传，默认空=不加鉴权

- 配置项 `mcp_token`（默认空）。main.go 在 `application.New` **之前**：
  `WAILS_MCP=1 且配置非空 → os.Setenv("WAILS_MCP_TOKEN", cfg.MCPToken)`。
  必须在 New 之前：MCP server 在框架启动流程里读取环境变量（mcp_enabled.go），跑起来后再设无效。
- 绑定方法：`GetMCPToken()` / `RotateMCPToken()`（crypto/rand 生成 64 位 hex 并落盘）。
- 默认空串不加鉴权：MCP 本就只在 `-tags mcp` 构建 + `make dev-mcp` 下才存在（护栏 17 把关生产构建），loopback 绑定是默认面；token 是「想暴露给本机之外的 Agent 客户端」时的显式开关。

### D8 macOS Edit 菜单：role 挂接，只 darwin

darwin 下 `Menu.New()` + `AddRole(AppMenu)` + `AddRole(EditMenu)` + `SetApplicationMenu`。
只 darwin：Windows/Linux 本仓是无边框自绘标题栏，不需要也不该出现原生菜单条。
修复的实际问题是 webview 里 `<input>` 的 Cmd+C/V/X/A 全军覆没（无菜单承接快捷键）。
**标红验证项**：role 展开行为需真跑确认（自动化只能确认菜单已挂、应用不崩；Cmd+C 是否生效需人工点一次）。

## 护栏（延续编号，当前 23）

- **护栏 24** `TestMCPTokenPassthroughIsWired`：main.go 必须含 `os.Setenv` 且第一个实参是
  字面量 `"WAILS_MCP_TOKEN"`——删掉透传无任何症状（token 静默失效），符合铁律 5。
- **护栏 25** `TestMacEditMenuIsWired`：main.go 必须出现 `SetApplicationMenu` 调用且引用
  `application.EditMenu` role。
- **护栏 26** `TestSetThemeAcceptsFollowSystem`：app.go 的 `SetTheme` 校验分支必须接受空串
  （三档语义的 Go 侧锚点，被误删成只收 dark/light 时前端「跟随系统」会静默 4xx）。

## Risks / Trade-offs

- 通知与自启在 dev 态（无打包 bundle）会报平台错误——设计上如实返回，不 mock。
- ImportDatabase 替换运行中应用的数据库属「尽力而为的一致」：恢复后建议重启（文案+spec 注明）。
- Edit 菜单 role 展开细节（是否含「重做」「粘贴并匹配样式」）以上游为准，不在本仓复刻。
