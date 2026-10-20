# project-scaffold Specification

## Purpose
TBD - created by archiving change foundation. Update Purpose after archive.
## Requirements
### Requirement: 最小可跑骨架
脚手架 MUST 提供一个 Wails v2 (Go + Vue3) 骨架，`make dev` 能启动开发态，`make package` 能产出跨平台安装包。

#### Scenario: 开发态启动
- **WHEN** 用户在项目根目录执行 `make dev`
- **THEN** Wails 开发服务启动，Vue3 前端热更新生效，主窗口正常显示

#### Scenario: 打包
- **WHEN** 用户执行 `make package`
- **THEN** 系统产出目标平台安装包（当前平台），且构建过程无致命错误

### Requirement: 统一命令入口
所有高频操作 MUST 通过 `Makefile` 的 target 暴露，AI 与人不记零散脚本路径。

#### Scenario: 命令入口存在
- **WHEN** 用户执行 `make help` 或查看 `Makefile`
- **THEN** 系统列出 `dev / build / test / lint / smoke / package / gen` 等 target 及其用途

#### Scenario: 未实现能力显式失败
- **WHEN** 用户执行某个尚未实现的 target（如 `make smoke`）
- **THEN** 系统打印"未实现，待 X change"并返回非零退出码，而非静默成功

### Requirement: 干净性
`.gitignore` MUST 挡住所有运行时产物，禁止其进入版本控制。

#### Scenario: 产物不入库
- **WHEN** 项目产生 `*.db`、`node_modules/`、`dist/`、`build/`、`*.exe`、日志文件等
- **THEN** 这些文件被 `.gitignore` 规则排除，`git status` 不显示它们

### Requirement: 前置条件缺失时给出可操作错误
命令入口在缺少必要前置条件时 MUST 以可读消息失败并指明修复命令，
MUST NOT 把底层工具的原始报错直接抛给用户。

#### Scenario: 缺少构建产物时提示修复命令
- **WHEN** 用户在未构建前端的新检出上运行测试或静态检查
- **THEN** 输出说明「为什么需要它」+「怎么修」，并以非零退出
- **AND** 不出现只有内部符号名的原始编译错误

#### Scenario: 相关命令不受噪音污染
- **WHEN** 前置条件缺失时运行与编译无关的目标（如列出命令）
- **THEN** 该目标正常输出，不被「前置条件缺失」的报错刷屏

#### Scenario: 不用静默吞错换取安静
- **WHEN** 收集软件包列表的命令因非前置条件的原因失败
- **THEN** 失败仍然可见（不通过丢弃 stderr 让问题消失），
  避免退化成「跳过检查却显示通过」

