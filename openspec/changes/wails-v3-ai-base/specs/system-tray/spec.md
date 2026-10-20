# system-tray

系统托盘：常驻图标、交互菜单、关闭行为。

## MODIFIED Requirements

### Requirement: 托盘常驻

应用 MUST 在 Windows、Linux、macOS 三平台均提供系统托盘图标，三平台行为一致，由框架内置托盘实现提供。

v2 时代 macOS 不支持托盘是框架架构冲突导致的被迫妥协（Wails 自身占据 NSApplication delegate，与所有 systray 库互斥），项目为此引入 `energye/systray` fork 并在 macOS 上整体 no-op。v3 内置 SystemTray 在框架自己的 delegate 上实现，冲突从根上消失，该妥协 MUST 撤销。

#### Scenario: 三平台均有托盘
- **WHEN** 应用在 Windows、Linux 或 macOS 任一平台运行
- **THEN** 系统托盘区出现应用图标

#### Scenario: 托盘实现统一
- **WHEN** 检查托盘的平台实现
- **THEN** 只有一份实现（框架内置），不存在按平台分叉的 no-op 分支

#### Scenario: 关闭驻留托盘
- **WHEN** 用户点击窗口关闭按钮
- **THEN** 窗口隐藏，应用驻留托盘，进程不退出

### Requirement: 托盘交互

托盘 MUST 提供右键菜单（显示 / 退出）与单击恢复窗口，三平台一致。

#### Scenario: 单击恢复
- **WHEN** 用户单击托盘图标
- **THEN** 窗口恢复显示并置前

#### Scenario: 右键菜单退出
- **WHEN** 用户右键托盘图标并选择退出
- **THEN** 应用优雅退出（先停托盘、落盘配置、关数据库），托盘图标消失

#### Scenario: 无图标时的兜底
- **WHEN** 托盘图标资源缺失
- **THEN** 使用占位图而非启动失败
