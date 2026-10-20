# tasks — 内置功能第一批

> 进度：阶段 1–4 完成（`make build` / `make test` / `make lint` 全绿），5.2 运行时验证完成。护栏共 26 项。
> 5.3 的人工确认项待用户在真机界面复核（无法自动化）。

## 1. 配置与 Go 能力层

- [x] 1.1 `internal/config`：新增 `shortcut_toggle_window`、`mcp_token` 两个字段（带 json tag，默认空串）
- [x] 1.2 `internal/service/import.go`：`ImportDatabase`（校验 → 关连接 → pre-import 备份 → 复制 → 重开，失败回滚）
- [x] 1.3 `app.go` 新绑定方法：剪贴板读写、通知授权/发送、自启查询/开关、快捷键查询/设置、MCP token 查看/轮换、数据库恢复

## 2. 组合根接线（main.go）

- [x] 2.1 MCP token 透传：配置加载后、`application.New` 前 `os.Setenv("WAILS_MCP_TOKEN", …)`
- [x] 2.2 注册 `notifications.New()` 服务并赋给 `App.notify`
- [x] 2.3 darwin 下挂应用菜单（AppMenu + EditMenu role）

## 3. 前端

- [x] 3.1 `stores/app.ts`：themeMode 与 theme 分离，跟随系统档 + matchMedia 监听
- [x] 3.2 `Settings.vue`：主题三档；数据目录「复制路径」；测试通知；自启开关；快捷键设置；MCP token 生成；恢复数据库
- [x] 3.3 重新生成绑定（`wails3 generate bindings -clean=true -ts -i`），`vue-tsc` 通过

## 4. 护栏

- [x] 4.1 护栏 24：`TestMCPTokenPassthroughIsWired`（AST 断言 main.go 的 os.Setenv("WAILS_MCP_TOKEN", …)，且值来自 cfg.MCPToken）
- [x] 4.2 护栏 25：`TestMacEditMenuIsWired`
- [x] 4.3 护栏 26：`TestSetThemeAcceptsFollowSystem`
- [x] 4.4 三条护栏红/绿破坏性验证：绿态全过；红态（改环境变量名 / SetApplicationMenu 改名 / 砍掉空串分支）各自 FAIL 且报错指向准确

## 5. 验证

- [x] 5.1 `make build` + `make test` + `make lint` 全绿
- [x] 5.2a 恢复数据库：单测覆盖（`TestImportDatabaseRestoresData` / `TestImportDatabaseRollsBackOnBadFile` / 拒绝数据目录内源 / 拒绝空路径）
- [x] 5.2b 实跑验证（dev-mcp，2026-09-20）：见「验证记录 · 阶段 5」
- [ ] 5.3 人工确认（标红项）：macOS 菜单栏出现应用菜单且输入框 Cmd+C/V 生效；通知授权弹窗

## 验证记录 · 阶段 4

| 验证 | 结果 |
|------|------|
| 护栏 24 红态 | `WAILS_MCP_TOKEN` 实参改名 → FAIL，报错指向透传缺失 |
| 护栏 25 红态 | `SetApplicationMenu` 改名 → FAIL，报错指向菜单移除 |
| 护栏 26 红态 | SetTheme 砍掉空串分支 → FAIL，报错指向「跟随系统」档 |
| 单测 | `internal/service` 4 个导入用例 + 既有导出用例全绿 |

## 验证记录 · 阶段 5（运行时实跑，dev-mcp）

| 能力 | 结果 |
|------|------|
| 主题三档 | SetTheme("dark"/"light") 落盘 config.json；SetTheme("") 回到跟随系统，GetTheme 返回空串 |
| 剪贴板 | SetClipboardText("builtin-toolkit-verification") → GetClipboardText 往返一致 |
| 全局快捷键 | SetToggleWindowShortcut("CmdOrCtrl+Shift+Y") 成功；非法 accelerator 被 422 拒绝（错误链 `invalid global shortcut … not a valid key`）；置空注销成功 |
| 自启 | SetAutostart(true) → GetAutostartEnabled=true → false，往返一致 |
| 通知 | SendNotification 成功（授权请求通过既有授权状态） |
| 数据库恢复 | ExportDatabase → ImportDatabase 往返；`.pre-import` 备份策略生效（旧库 0 字节、备份 4096 字节接管为现库） |
| MCP 鉴权 | 启动日志 `已启用 MCP Bearer 鉴权（WAILS_MCP_TOKEN）`；无 Bearer → HTTP 401；带 RotateMCPToken 轮换出的 Bearer → initialize/tools/list/tools/call 及 `main.App.GetTheme`/`main.App.GetMCPToken` 绑定调用全通（GetMCPToken 返回值与 token 精确一致） |
| macOS 菜单 | 启动日志 `已挂载 macOS 应用菜单（AppMenu + EditMenu）`（菜单交互项归 5.3 人工确认） |

## 实施中新发现

- 绑定生成必须用 `wails3 generate bindings -clean=true -ts -i`（裸跑不带 `-ts -i` 只产 .js，
  vue-tsc 报 TS7016——build/Taskfile.yml 的 generate:bindings 任务里有完整参数）。
- `app.go` 新增绑定方法被既有护栏 `TestBindingsUseAppErr` 拦下一次：直接 `errors.New`/
  `fmt.Errorf` 造错误违反错误协议——改用 `apperr.New(apperr.CodeInternal, …)` 或
  `apperr.Wrap(err)` 后改写 Message（保留 Detail）。护栏生效的好例子。
- MCP `call_bound_method` 需全限定名（`main.App.GetTheme`），短名报 `unknown bound method name`。
- 观察项（非本批范围）：窗口经托盘关闭（visible=false）后，MCP window_control 的
  show/focus/restore 均无法唤起——依赖可见窗口的 call_bound_method/js_eval 调用会失败。
  若后续要支持「无头驱动」，需在窗口隐藏态下保持 webview 激活或提供唤起绑定。
