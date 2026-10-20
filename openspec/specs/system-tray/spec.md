# system-tray Specification

## Purpose
TBD - created by archiving change runtime. Update Purpose after archive.
## Requirements
### Requirement: 托盘常驻
应用 MUST 在 Windows/Linux/macOS 三平台提供系统托盘图标，关闭窗口时驻留托盘（隐藏而非销毁）。
仅当托盘【未能启动】时，关闭窗口才真正退出应用，且此时 MUST NOT 残留无窗口进程。

#### Scenario: 关闭到托盘（三平台）
- **WHEN** 用户点击窗口关闭按钮（macOS 原生交通灯或 Windows/Linux 自绘按钮）且托盘已启动
- **THEN** 窗口隐藏、页面状态保留、应用驻留托盘、进程不退出

#### Scenario: 关闭后可恢复
- **WHEN** 窗口已隐藏到托盘
- **THEN** 经下列任一方式可恢复显示并置前：托盘菜单「显示」、双击托盘图标、macOS 点 Dock 图标、再次启动应用（单实例唤起）

#### Scenario: 托盘缺失时的关闭行为
- **WHEN** 系统托盘未能启动（启动失败或平台不支持）且用户点击关闭按钮
- **THEN** 窗口真正关闭并退出应用，MUST NOT 残留无窗口进程

### Requirement: 托盘交互
托盘 MUST 提供右键菜单与双击/单击恢复窗口（Windows/Linux/macOS）。

#### Scenario: 双击恢复
- **WHEN** 用户双击托盘图标
- **THEN** 窗口恢复显示并置前

#### Scenario: 右键菜单退出
- **WHEN** 用户右键托盘图标并选择退出
- **THEN** 应用优雅退出，托盘图标消失
