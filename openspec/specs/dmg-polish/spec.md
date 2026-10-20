# dmg-polish Specification

## Purpose
TBD - created by archiving change mac-dmg-polish. Update Purpose after archive.
## Requirements
### Requirement: 背景图构建期合成
打包 macOS dmg 时，背景图 MUST 优先由模板合成（印上当前应用名），
使下游实例化后的 dmg 背景与自己的应用名相关，而非通用占位图。
应用名取 `wails.json` 的 `outputfilename`。

#### Scenario: 模板存在时合成带应用名的背景图
- **WHEN** `build/darwin/dmg-background.tpl.png` 存在，执行 `make package os=macos`
- **THEN** 打包生成 `dmg-background.png`，图中含 `outputfilename` 应用名，并作为 dmg 背景

#### Scenario: 合成工具不可用时降级
- **WHEN** 模板存在但合成工具（swift/qlmanage）不可用
- **THEN** 降级为直接使用模板/占位图（无应用名文字），dmg 仍正常生成，不阻断

#### Scenario: 模板缺失时用中性占位图
- **WHEN** 无 `build/darwin/dmg-background.tpl.png`
- **THEN** 使用内置中性占位背景图（或跳过背景图），dmg 仍可用

### Requirement: 箭头锚点与图标同源
背景图箭头的绘制坐标 MUST 由驱动 Finder 图标落点的**同一组常量**推导并传入合成脚本，
使箭头始终落在两图标中心之间；不得依赖「窗口宽/2」或「背景图宽/2」，
因为窗口 bounds 宽 ≠ content 视口宽（受 Finder 侧栏影响而浮动）。

#### Scenario: 箭头与图标一致
- **WHEN** 以默认常量合成背景图并应用 Finder 布局
- **THEN** 箭头中心落在两图标落点的中点（`(ICON_LEFT_X+ICON_RIGHT_X)/2, ICON_Y`），
  与 Finder 侧栏宽度无关

#### Scenario: 改图标落点不破坏对齐
- **WHEN** 修改 `ICON_LEFT_X` / `ICON_RIGHT_X`
- **THEN** 箭头坐标随之推导（同源），不会静态漂移

### Requirement: 零新增外部依赖
背景合成 MUST 只使用 macOS 系统自带工具，不得引入 create-dmg 等外部依赖。

#### Scenario: 无外部依赖
- **WHEN** 合成背景图
- **THEN** 仅调用 `sips`/`qlmanage`/`swift` 等系统自带能力，不新增 npm/go/brew 依赖

### Requirement: mac dmg 挂载点契约
Finder 布局 MUST 在挂载于 `/Volumes/<name>` 的卷上设置；使用 `mktemp -d` 等非
`/Volumes` 路径作挂载点会令 Finder 无法解析卷、布局静默失效。
卷名与 `tell disk` 引用的名称 MUST 一致；重名时回退改名并同步更新二者。

#### Scenario: 挂载点可被 Finder 解析
- **WHEN** 执行 `make package os=macos`
- **THEN** 中间 dmg 挂载于 `/Volumes/<name>`，`tell disk "<name>"` 成功（不报 -1728）

#### Scenario: 卷名重名回退
- **WHEN** `/Volumes/<name>` 已被既有卷占用
- **THEN** 改用带后缀的新卷名，并**同步**用于 `-volname` 与 `tell disk`，二者始终一致

