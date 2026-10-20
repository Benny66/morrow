# constitution Specification

## Purpose
TBD - created by archiving change foundation. Update Purpose after archive.
## Requirements
### Requirement: 宪法分层结构
项目 MUST 维护两层宪法文档：根 `AGENTS.md` 管跨端通用铁律，根 `CLAUDE.md` 管 Go 域规范、`frontend/CLAUDE.md` 管 Vue 域规范，AI 在哪个目录干活读哪份。

#### Scenario: 文档齐全
- **WHEN** 检查项目根目录与 `frontend/` 目录
- **THEN** 存在 `AGENTS.md`、`CLAUDE.md`、`frontend/CLAUDE.md`、`docs/map.md` 四个文件

#### Scenario: 分工清晰
- **WHEN** 阅读根 `AGENTS.md`
- **THEN** 它只含跨端铁律（目录命名、依赖登记、干净性、验证入口），不含 Vue 或 Go 的域内实现细节

### Requirement: 机器强制规则标记
能被机器强制执行的规则 MUST 标 `⚙️` 并指向对应的检查（护栏/规则文件），与软约束区分。

#### Scenario: 硬约束可识别
- **WHEN** 阅读任一份宪法文档
- **THEN** 每条硬约束带 `⚙️` 标记，并说明由哪个护栏或 lint 规则强制，读者一眼分辨软硬约束

### Requirement: 代码导航地图
`docs/map.md` MUST 说明"哪类代码在哪个文件"，供 AI 定向读取而非全局盲扫。

#### Scenario: 地图可用
- **WHEN** AI 需要定位某类代码（如 model、service、路由）
- **THEN** 它能从 `docs/map.md` 找到对应文件路径，而无需 `find` 全库搜索

