# builtin-toolkit 能力规格

## ADDED Requirements

### Requirement: 主题三档

应用 SHALL 提供「跟随系统 / 亮色 / 暗色」三档主题，配置持久化为 `theme` 字段（`""`=跟随系统）。

#### Scenario: 跟随系统档随系统偏好联动

- **WHEN** 用户选择「跟随系统」且系统在亮暗间切换
- **THEN** 界面主题实时跟随，无需重启；配置文件保持 `theme: ""`

#### Scenario: 非法主题值被拒绝

- **WHEN** `SetTheme` 收到 `""`、`"dark"`、`"light"` 之外的值
- **THEN** 返回校验错误，配置不落盘

### Requirement: 剪贴板

应用 SHALL 提供系统剪贴板的读写绑定方法。

#### Scenario: 写入后可读回

- **WHEN** `SetClipboardText(text)` 成功
- **THEN** `GetClipboardText()` 返回同一文本

### Requirement: 系统通知

应用 SHALL 注册官方通知服务，并提供授权请求与发送通知的绑定方法。

#### Scenario: 未授权时发送前先请求授权

- **WHEN** 前端调用发送通知而授权未决
- **THEN** 后端返回的错误如实描述平台原因（dev 态无 bundle 时亦然，不静默成功）

### Requirement: 全局快捷键（唤起主窗口）

应用 SHALL 支持配置一个全局快捷键，在窗口隐藏时唤起、可见时隐藏到托盘。

#### Scenario: 配置非法快捷键不落盘

- **WHEN** `SetToggleWindowShortcut` 传入注册失败的快捷键串
- **THEN** 返回错误，原快捷键保持注册且配置不变

#### Scenario: 空串表示关闭

- **WHEN** 传入空串
- **THEN** 注销现有快捷键并落盘空串

### Requirement: 开机自启

应用 SHALL 提供开机自启的查询与开关绑定方法，平台差异（macOS 需打包态）由错误如实呈现。

#### Scenario: 状态查询反映真实系统状态

- **WHEN** 调用 `GetAutostartEnabled`
- **THEN** 返回值为系统注册表/SMAppService 的真实状态，而非内存标记

### Requirement: 数据库恢复

应用 SHALL 支持从导出的数据库文件恢复数据，恢复失败时自动回滚到恢复前状态。

#### Scenario: 恢复失败自动回滚

- **WHEN** 导入的文件不是可用数据库导致重开失败
- **THEN** 原数据库被原样恢复并重新打开，返回原始错误，应用继续可用

#### Scenario: 源文件位于数据目录内被拒绝

- **WHEN** 源路径位于应用数据目录内
- **THEN** 返回校验错误，不触碰当前数据库

### Requirement: MCP Token 鉴权

应用 SHALL 支持为运行时 MCP 配置 Bearer token，并经 `WAILS_MCP_TOKEN` 在启动期生效。

#### Scenario: token 在服务启动前生效

- **WHEN** 配置了 `mcp_token` 且以 `WAILS_MCP=1` 启动
- **THEN** MCP 服务所有请求需 Bearer token（含 loopback），未带 token 的请求被 401 拒绝

#### Scenario: 默认不加鉴权

- **WHEN** `mcp_token` 为空
- **THEN** MCP 行为与上游默认一致（loopback 免鉴权）

### Requirement: macOS 应用菜单

应用 SHALL 在 macOS 挂载含 AppMenu 与 EditMenu role 的应用菜单，使 webview 输入框的 Cmd+C/V/X/A/Z 快捷键生效；其他平台不挂载。

#### Scenario: 非 darwin 平台无原生菜单条

- **WHEN** 应用在 Windows/Linux 运行
- **THEN** 不调用 SetApplicationMenu，自绘标题栏行为不变
