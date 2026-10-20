# app-shell Specification

## Purpose
TBD - created by archiving change design-system. Update Purpose after archive.
## Requirements
### Requirement: 平台感知无边框
应用 MUST 按平台采用不同的无边框方案：Windows 自绘标题栏 + 三按钮，macOS 原生交通灯 + 内容顶到顶。

#### Scenario: Windows 自绘标题栏
- **WHEN** 应用运行在 Windows
- **THEN** 窗口无系统边框，显示自绘标题栏，含最小化/最大化/关闭三按钮，位于右侧

#### Scenario: macOS 原生交通灯
- **WHEN** 应用运行在 macOS
- **THEN** 窗口保留原生交通灯按钮（左上），内容区顶到顶部，不自绘三按钮

#### Scenario: macOS 交通灯不被 Frameless 吞掉
- **WHEN** 应用运行在 macOS 且配置了 `TitleBarHiddenInset`
- **THEN** `Frameless` 必须为 `false`（否则 Wails 跳过 `NSWindowStyleMaskTitled`，交通灯消失），窗口既有交通灯又有透明贴边的无边框观感

### Requirement: 拖拽区
标题栏中央区域 MUST 可拖拽移动窗口，交互控件区域 MUST 排除在拖拽区外。

#### Scenario: 标题栏可拖拽
- **WHEN** 用户按住标题栏空白区域拖动
- **THEN** 窗口随之移动

#### Scenario: 控件不可拖拽
- **WHEN** 用户点击标题栏上的按钮/输入框
- **THEN** 点击正常触发，不触发窗口拖拽

### Requirement: 侧栏导航壳
应用 MUST 提供可折叠的侧栏 + 内容主区域布局。

#### Scenario: 侧栏折叠
- **WHEN** 用户点击侧栏折叠按钮
- **THEN** 侧栏以过渡动画折叠/展开，内容区宽度随之调整

