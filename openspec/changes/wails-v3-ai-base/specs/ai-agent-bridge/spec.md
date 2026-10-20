# ai-agent-bridge

Agent 驱动基座的能力契约：运行时 MCP（控运行中的应用）与 CLI MCP（控项目生命周期）的启用方式、工具边界与安全约束。

## ADDED Requirements

### Requirement: MCP 默认关闭

`mcp` 构建标签 MUST NOT 出现在默认构建与发布产物中。默认 `wails3 build` 必须产出不含 MCP 服务代码的二进制。

理由：MCP 服务能给任意连上它的本机进程执行 JS、调用 Go 方法、模拟输入。破坏这条不会产生任何症状——应用照常运行，只是多开了一个高权限本机端口。

#### Scenario: 默认构建不含 MCP
- **WHEN** 执行 `make build` 后检查产物
- **THEN** 9099 端口无监听，且二进制中不含 MCP 服务符号

#### Scenario: 只有显式设置才启用
- **WHEN** 未设置 `WAILS_MCP=1` 而直接运行 `wails3 build`
- **THEN** 构建命令不会附加 `mcp` 标签

#### Scenario: 护栏阻止标签泄漏
- **WHEN** `scripts/build.sh` 或 CI 工作流中被加入裸的 `-tags mcp`
- **THEN** `internal/guard/wiring_test.go` 失败并说明后果

### Requirement: MCP 开发入口独立于默认开发入口

启用 MCP 的开发命令 MUST 是独立 target（如 `make dev-mcp`），MUST NOT 改变 `make dev` 的行为。

#### Scenario: 两个入口互不影响
- **WHEN** 执行 `make dev`
- **THEN** 应用启动但不监听 MCP 端口

#### Scenario: MCP 入口可用
- **WHEN** 执行 `make dev-mcp`
- **THEN** 应用启动并在日志中输出 MCP 端点（默认 `http://127.0.0.1:9099/mcp`）

### Requirement: 运行时 MCP 绑定 loopback

运行时 MCP 服务 MUST 默认只绑定 `127.0.0.1`。绑定非 loopback 地址时 MUST 要求高熵 bearer token，缺失则启动失败。

#### Scenario: 默认 loopback
- **WHEN** 以 MCP 标签启动应用且未设置 `WAILS_MCP_HOST`
- **THEN** 服务只监听 `127.0.0.1:9099`

#### Scenario: 非 loopback 必须有 token
- **WHEN** 设置 `WAILS_MCP_HOST=0.0.0.0` 但未设置 `WAILS_MCP_TOKEN`
- **THEN** 启动失败并报错，而非静默放开访问

### Requirement: CLI MCP 限定项目根

`wails3 mcp` 服务 MUST 把可访问路径限定在项目根目录内。MUST NOT 暴露任意 shell 执行能力。

#### Scenario: 越界路径被拒
- **WHEN** Agent 请求访问项目根之外的路径
- **THEN** 请求被拒绝

#### Scenario: 无任意命令执行工具
- **WHEN** 查看 CLI MCP 暴露的工具清单
- **THEN** 只有项目检查/初始化、诊断、任务列表与执行、构建、开发启动、绑定生成、任务状态查询，没有通用 shell 工具

### Requirement: Agent 可驱动运行中的应用

在 MCP 标签构建下，Agent MUST 能对运行中的应用执行：列出与控制窗口、查询 DOM、执行 JS、模拟鼠标与键盘输入、调用绑定的 Go 方法、发送与等待应用事件。

前提：目标窗口 MUST 已加载正常的前端页面（含 Wails 运行时），否则需要页面回传结果的操作（`js_eval`、`call_bound_method`、`dom_query`、输入模拟）会超时。窗口控制类操作（`app_info`、`windows_list`、`window_control`）与 `emit_event` 不经页面，不受此限。

#### Scenario: 调用绑定方法
- **WHEN** Agent 调用 `call_bound_method` 指定某个已绑定的 Go 服务方法
- **THEN** 方法被执行且返回值回传给 Agent

#### Scenario: 页面未就绪时的可诊断失败
- **WHEN** Agent 在页面未加载完成时调用 `js_eval`
- **THEN** 返回超时错误并提示窗口可能正在重载或忙碌，而非静默无响应

#### Scenario: 驱动界面
- **WHEN** Agent 用 `dom_query` 定位一个按钮并用 `mouse_click` 点击
- **THEN** 按钮的点击行为真实发生

#### Scenario: 等待事件
- **WHEN** Agent 在触发某个操作后 `wait_for_event` 等待该操作应产生的事件
- **THEN** 事件到达并把它携带的数据返回给 Agent

### Requirement: 基座交付 MCP 用法文档

`docs/map.md` 与 README MUST 说明如何启用 MCP、如何把 MCP 客户端接到运行中的应用，以及「默认关闭」的原因。

#### Scenario: 文档可照做
- **WHEN** 一个不了解本项目的开发者按文档操作
- **THEN** 他能启动带 MCP 的应用并成功接入一个 MCP 客户端
