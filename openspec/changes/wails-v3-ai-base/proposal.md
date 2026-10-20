# wails-v3-ai-base — Wails v3 迁移与「AI 基座」

## Why

基座目前是 Wails v2。v2 已经把「人可以上手」这件事做到了：护栏、契约、单一真相、开箱即用。但**能上手的是人，不是 Agent**——Agent 想验证一个改动，只能靠人肉点界面、贴截图、念日志。

Wails v3（beta.23）带来了一个改变定位的能力：**内置 MCP 服务**。它让 LLM 直接驱动正在运行的应用——点按钮、读 DOM、调 Go 方法、等事件，全程无需人写一行测试代码。这把基座的能力边界从「给人用的脚手架」推到「给人 + Agent 用的脚手架」。

同时 v3 顺带解决了 v2 时代两个被迫的妥协：

- **macOS 没有托盘**。v2 的 NSApplication delegate 与所有 systray 库冲突，项目为此选了 energye fork，又在 macOS 上整体 no-op。v3 内置 SystemTray 原生支持 macOS，这个妥协可以撤销。
- **日志适配器**。v2 的 `logger.Logger` 是自定义接口，项目写了 `WailsAdapter` 手工桥接到 slog。v3 的 `Options.Logger` 直接就是 `*slog.Logger`，适配器可以删掉。

所以这不是一次「版本号 +1」的升级，而是一次**能力换挡**：借 v3 的架构，把基座从「桌面应用脚手架」升级为「Agent 可驱动、可验收的 AI 基座」。

## What Changes

### 迁移（v2 → v3）

- **应用初始化三拆**：`wails.Run(&options.App{...})` 一调到底 → `application.New` → `app.Window.NewWithOptions` → `app.Run()` 三步。
- **绑定改服务**：`Bind: []interface{}{app}` → `Services: []application.Service{application.NewService(app)}`；`startup(ctx)` → `ServiceStartup(ctx, opts) error`；`shutdown(ctx)` → `ServiceShutdown() error`。
- **运行时改对象方法**：`runtime.*(ctx, ...)` 全部改为对象上的方法（`window.Show()`、`app.Quit()`、`app.Event.Emit(...)`）。**App 不再需要持有 `ctx`**。
- **错误协议签名变更**：`ErrorFormatter func(error) any`（返回 JSON 字符串）→ `MarshalError func(error) []byte`（返回 JSON 字节）。`apperr.Format` 随之改造。
- **日志适配器删除**：`internal/logging/wails.go` 的 `WailsAdapter` 不再需要，`Options.Logger` 直接吃 `*slog.Logger`。
- **托盘改用内置实现**：删除 `energye/systray` 依赖与 macOS 的 no-op 分支，改用 `app.SystemTray`。**macOS 从此有托盘**。
- **对话框注入 App**：`internal/dialog` 从「传 ctx 调 runtime」改为「持有 `*application.App` 调 `app.Dialog`」。
- **构建链换 CLI**：`wails` → `wails3`，配置从 `wails.json` 迁到 `Taskfile.yml` + `build/config.yml`。
- **前端绑定换形态**：`frontend/wailsjs/go/main/App` → `frontend/bindings/<module>/...`；事件与运行时从 `wailsjs/runtime` 换成 `@wailsio/runtime` 的 `Events` API。

### AI 基座（新增能力）

- **运行时 MCP 服务**：`mcp` 构建标签下编译进应用，`WAILS_MCP=1` 时由 CLI 自动加标签。启动后 Agent 可 `call_bound_method` / `dom_query` / `mouse_click` / `keyboard_type` / `wait_for_event`。**默认构建不含该服务**——无标签即无代码、无端口、无攻击面。
- **CLI MCP 服务**：`wails3 mcp` 暴露项目生命周期工具（`build` / `dev` / `task_run` / `generate_bindings` / `wails_job_status`），root 限定在项目目录，不暴露任意 shell。
- **两套服务职责切分**：CLI 管「静态空间」（源码、构建、任务），运行时管「活动空间」（窗口、DOM、绑定）。前者让 Agent 能改代码，后者让 Agent 能验收改动。

## Capabilities

### New Capabilities

- `ai-agent-bridge`: Agent 驱动基座的能力契约——运行时 MCP（控应用）与 CLI MCP（控生命周期）的启用方式、工具边界、安全约束与默认关闭原则。

### Modified Capabilities

- `system-tray`: 从「Windows/Linux 有、macOS no-op」改为「三平台统一，由 v3 内置 SystemTray 提供」。
- `logging`: 删除 Wails 日志适配器；Wails 系统日志直接走 `Options.Logger`（`*slog.Logger`）。
- `native-dialogs`: 对话框从 `context.Context` 注入改为 `*application.App` 注入。
- `pipeline-contract`: 错误协议的 Wails 侧签名由 `func(error) any` 改为 `MarshalError func(error) []byte`（前端 `JSON.parse` 还原逻辑不变）。
- `app-lifecycle`: 生命周期钩子从 `startup(ctx)`/`shutdown(ctx)` 改为 `ServiceStartup`/`ServiceShutdown`；组合根拆为「创建 → 建窗 → Run」。
- `architecture-guardrails`: 护栏解析目标随框架变更——`wiring` 改认 `application.Options` 字面量，`bindings` 改认 v3 绑定产物路径。
- `dependency-registry`: `deps.yaml` 中 `wails/v2` → `wails/v3`，删除 `energye/systray`。
- `ci-pipeline` / `package-release`: CI 与打包脚本从 `wails` CLI 切到 `wails3`，版本号单一真相的读取路径随配置迁移调整。
- `delivery-docs`: `docs/map.md`、`AGENTS.md`、`CLAUDE.md`、README 随新架构更新，并补 MCP 用法。

## Impact

- **Go 改动面（已核实）**：只有 3 个文件直接 import v2——`main.go`、`app.go`、`internal/dialog/dialog.go`；另有 `internal/logging/wails.go`（删）、`internal/apperr/apperr.go`（签名）、`internal/tray/*`（换实现）。
- **非 Go 改动面（机械但铺得广）**：`go.mod`、`wails.json`、新增 `Taskfile.yml`、`Makefile`、`scripts/*.sh`（6 个）、`.github/workflows/*`（2 个）、前端 `lib/*.ts`（4 个）与绑定目录、`internal/guard/*`（3 个护栏）。
- **依赖**：新增 `wails/v3` 与 `@wailsio/runtime`；删除 `energye/systray`。
- **破坏性**：绑定产物路径变了，下游若直接 import `wailsjs/go/main/App` 会断（本项目前端经 `lib/invoke.ts` 收口，影响可控）。
- **风险**：v3 是 beta，API 仍在动。**原先 6 项「待实跑确认」已全部实跑钉死**（结论见 `design.md`），并额外挖出 5 个会炸的坑，其中最需要注意的是三条：
  1. **版本号载体换成了 `build/config.yml`**，且 Info.plist 渲染从「自动」变成「必须显式跑 `wails3 task common:update:build-assets`」——漏了就会重现本项目当初要消灭的双源漂移。
  2. **`MessageDialog` 在 v3 没有同步返回**，`Confirm` 必须用 channel 桥接才能保住现有签名。
  3. **macOS 托盘传空图标会 panic**（不是返回 error）——项目原有的 `placeholderIcon()` 兜底必须保留。
- **已验证成立的关键能力**：MCP 的 16 个工具在真实前端下全部可用。外部 MCP 客户端成功调用运行中应用的 Go 方法（`Hello Wails!`）、读取真实 DOM 与元素坐标、发送事件并被 Go 侧收到。「Agent 改完代码后自动验收」这个定位**有实跑支撑，不是纸面推断**。
- **验证前提**：本机已装 `wails3 v3.0.0-beta.23`（`go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.23`），`frontend/dist` 已存在，可本地实跑。
- **下游影响**：v3 的最低系统要求高于 v2（模板 Info.plist 声明 macOS 12.0+），需在发布说明中告知。
