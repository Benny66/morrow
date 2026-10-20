# package-release Specification

## Purpose
TBD - created by archiving change package-release. Update Purpose after archive.
## Requirements
### Requirement: 参数化打包
`make package os=<OS>` MUST 支持参数化：无参打包当前平台，`windows` 参数在 mac/linux 上交叉编译出 .exe。

#### Scenario: 无参打包当前平台
- **WHEN** 执行 `make package`（无参数）
- **THEN** 打包当前平台产物（mac 产 .dmg，windows 产 .exe 安装器）

#### Scenario: 交叉编译 windows
- **WHEN** 在 mac/linux 上执行 `make package os=windows`
- **THEN** 交叉编译出 Windows .exe（无 makensis 时降级为裸 exe 并提示）

#### Scenario: 非本机平台明确报错
- **WHEN** 在 mac 上执行 `make package os=linux`（或反过来）
- **THEN** 报错提示"请在对应平台执行"，不产出无效产物

### Requirement: mac dmg 安装包
macOS 打包 MUST 在 `.app` 之后封装为 `.dmg` 安装包，并在产出后**回读校验**美化结果，
使「已美化」与「降级产物」可区分。

#### Scenario: 产出 dmg
- **WHEN** 在 macOS 执行 `make package`
- **THEN** 产出 `build/bin/<name>.dmg`（含 .app 的压缩映像）

#### Scenario: 回读校验美化结果
- **WHEN** dmg 产出完成
- **THEN** 二次挂载成品只读卷，检查 `.DS_Store` 与背景图引用：
  命中则打印 `✓ 美化已生效`，缺失则打印 `⚠ 降级产物（无美化）`（默认不阻断，退出码 0）

#### Scenario: 校验本身失败不误报
- **WHEN** 回读校验因无法挂载等原因失败
- **THEN** 报「无法校验」而非误报「无美化」，并提示校准方法可能已随 macOS 版本变化

### Requirement: 母版防呆
母版（模板）MUST 拒绝被打包成可分发产物，且其发布流程 MUST NOT 因此失败。

#### Scenario: 母版跳过打包但不阻断发布
- **WHEN** 在含占位符的母版仓库上推送 tag
- **THEN** 跳过安装包构建，Release 仍照常创建（只含平台自动附加的源码归档）

#### Scenario: 占位符检测不被自身替换破坏
- **WHEN** 工作流文件中的占位符检测串经过实例化处理
- **THEN** 检测结果仍然正确（母版命中、实例化项目不命中）

### Requirement: 版本号单一真相
版本号 MUST 只有一个出处（`wails.json` 的 `info.productVersion`），
所有产物与应用内取值 MUST 由它派生，且四处一致。

#### Scenario: 系统可见版本与配置一致
- **WHEN** 构建 macOS 产物
- **THEN** 产物 `Info.plist` 的 `CFBundleShortVersionString` 等于配置中的版本，
  而非平台或框架的默认值

#### Scenario: Windows 版本资源同源
- **WHEN** 交叉编译 Windows 产物
- **THEN** exe 版本资源中的版本等于同一出处

#### Scenario: 应用内版本同源
- **WHEN** 应用启动
- **THEN** 日志首行与 `GetAppInfo()` 返回的版本等于同一出处

#### Scenario: 版本注入点唯一
- **WHEN** 检查各构建脚本
- **THEN** 只有一处进行版本注入，其余构建入口都经它调用，
  不存在「各脚本各算一份版本」

#### Scenario: 构建后回读校验
- **WHEN** macOS 构建完成
- **THEN** 构建脚本回读产物 `Info.plist` 并断言版本一致，
  不一致时构建失败（而非等用户看「显示简介」才发现）

#### Scenario: 交叉编译不误读残留产物
- **WHEN** 在 macOS 上交叉编译其他平台的产物
- **THEN** 不做 macOS 产物回读校验（避免读到上一次构建残留的 `.app`）

### Requirement: 版本格式前置校验
版本号 MUST 在构建开始前校验为数字点分格式，非法值 MUST 立即失败。

#### Scenario: 非法版本早失败
- **WHEN** 版本号含非数字成分（如 `v1.2.3` 的前缀、git 短哈希）
- **THEN** 构建与发布脚本在动手前报错退出，并说明 Windows 打包会失败的原因

#### Scenario: 版本号缺失即失败
- **WHEN** 配置中缺少版本号
- **THEN** 构建脚本报错退出，不使用框架默认值静默继续

### Requirement: 提交号与版本号分开注入
提交标识 MUST 与版本号分开注入，使版本号保持数字格式的同时保留追溯能力。

#### Scenario: 构建期注入提交号
- **WHEN** 在 git 仓库内构建
- **THEN** 提交短哈希被注入并被 `GetAppInfo()` 返回，供排查定位

### Requirement: bundle id 由项目决定
应用的 bundle id MUST 由项目自定义，MUST NOT 沿用框架模板的默认厂商前缀。

#### Scenario: bundle id 使用项目前缀
- **WHEN** 构建 macOS 产物
- **THEN** `CFBundleIdentifier` 形如 `<项目前缀>.<应用名>`，而非框架默认前缀

#### Scenario: 前缀可配置且默认明显为占位
- **WHEN** 实例化新项目且未指定前缀
- **THEN** 使用 `com.example` 这类明显是占位符的前缀，并提示发布前替换

### Requirement: 打 tag 自动发布
推送版本 tag MUST 自动产出 GitHub Release，含安装包与自动生成的变更说明。

#### Scenario: 发布自动完成
- **WHEN** 推送形如 `v0.1.0` 的 tag
- **THEN** 自动构建各平台安装包，并创建 Release（说明由平台按提交自动生成）

#### Scenario: 产物可识别
- **WHEN** 查看 Release 的资产
- **THEN** 资产名含应用名、版本与平台，用户无需打开即可分辨

#### Scenario: 单平台上无法产出的产物由流水线补齐
- **WHEN** 某平台的安装包只能在对应操作系统上构建
- **THEN** 由该平台的构建环境产出并汇入同一次发布，不要求操作者具备所有平台

### Requirement: 版本号写入必须单一实现
把版本号写进配置的代码 MUST 只有一处实现，本地发布与 CI 发布 MUST 共用它。

#### Scenario: 两处共用
- **WHEN** 检查本地发布脚本与 CI 发布工作流
- **THEN** 二者都调用同一个写入脚本，各自不重复实现校验与写入

#### Scenario: 非法版本早失败
- **WHEN** 传入非数字点分格式的版本（如带前缀的 tag）
- **THEN** 在写入前即失败并说明原因，而非等到某平台打包的最后一步才炸

