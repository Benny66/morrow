# install-experience Specification

## Purpose
TBD - created by archiving change install-experience. Update Purpose after archive.
## Requirements
### Requirement: mac dmg 拖拽安装
macOS 的 dmg MUST 包含应用与指向 `/Applications` 的软链，用户拖拽即完成安装。

#### Scenario: dmg 含 Applications 软链
- **WHEN** 用户双击 `make package` 产出的 dmg
- **THEN** 挂载的卷内含 `MyApp.app` 与 `Applications` 软链，用户可将 app 拖入 Applications 完成安装

### Requirement: mac dmg 美化引导
dmg MUST 通过 Finder 窗口布局提供拖拽引导（应用在左、Applications 在右、背景图/箭头）。
图标落点与背景图箭头 MUST 由同一组常量推导（箭头锚点 == 两图标落点中点），
不得各写各的魔数，也不得依赖「窗口宽/2」（窗口 bounds 宽 ≠ content 视口宽）。

#### Scenario: 打开 dmg 有可视化引导
- **WHEN** 用户打开 dmg 窗口
- **THEN** 呈现背景图与图标定位（应用在左、Applications 在右），箭头落在两图标之间，视觉上引导拖拽

#### Scenario: 箭头与图标对齐（跨机稳定）
- **WHEN** 打包产物在真机 GUI 下打开（任意 Finder 侧栏宽度）
- **THEN** 箭头中心与两图标落点的中点一致，不随侧栏宽度漂移

#### Scenario: 无背景图时仍保留布局
- **WHEN** 背景图缺失
- **THEN** 图标定位与窗口尺寸仍生效（仅跳过背景图），而非整段布局被跳过

#### Scenario: 美化失败不阻断
- **WHEN** Finder 布局设置因环境原因失败
- **THEN** dmg 仍生成（含软链的最小可用形态），仅缺失视觉美化

### Requirement: win 安装器安装范围
Windows 打包 MUST 支持安装范围参数（user/machine），默认 user。

#### Scenario: 默认 user 范围
- **WHEN** 执行 `make package os=windows`（无 scope 参数）
- **THEN** NSIS 安装器默认装到 `%LOCALAPPDATA%\Programs`，免管理员

#### Scenario: 指定 machine 范围
- **WHEN** 执行 `make package os=windows scope=machine`
- **THEN** NSIS 安装器装到 `Program Files`（需管理员）

#### Scenario: 无 NSIS 编译器明确提示
- **WHEN** 打包 windows 但未安装 makensis
- **THEN** 明确提示安装 NSIS 的方式，而非静默产出非安装器产物

