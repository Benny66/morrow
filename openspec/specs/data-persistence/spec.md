# data-persistence Specification

## Purpose
TBD - created by archiving change foundation. Update Purpose after archive.
## Requirements
### Requirement: SQLite 连接
后端 MUST 通过 gorm 建立 SQLite 连接，且跨平台交叉编译无 CGO 依赖。

#### Scenario: 连接成功
- **WHEN** 应用启动
- **THEN** gorm 成功连接本地 SQLite 数据库文件，连接失败时应用显式报错并退出

#### Scenario: 交叉编译友好
- **WHEN** 对 Windows / macOS 目标执行交叉编译
- **THEN** 构建不因 SQLite 驱动的 CGO 依赖而失败

### Requirement: 迁移框架
所有模型 MUST 通过显式注册表统一迁移，模型注册只此一处。

#### Scenario: 注册表唯一
- **WHEN** 需要新增一个 gorm 模型
- **THEN** 只需在 `internal/model/` 的注册切片（如 `AllModels()`）中添加该模型，启动时 AutoMigrate 自动建表

#### Scenario: 迁移生效
- **WHEN** 向注册表添加一个新模型并启动应用
- **THEN** 对应数据表被自动创建，无需手写建表 SQL

### Requirement: 数据库可导出
应用 MUST 能把数据库导出为一份完整可用的副本，供用户备份或迁移。

#### Scenario: 导出产物可直接打开
- **WHEN** 调用导出绑定方法并传入目标路径
- **THEN** 产出的文件能被独立连接打开，且数据完整（不是半截快照）

#### Scenario: 覆盖已存在的目标
- **WHEN** 目标文件已存在（用户在系统保存对话框中已确认覆盖）
- **THEN** 导出成功并替换该文件，而不是报「文件已存在」拒绝

#### Scenario: 拒绝导出到数据目录内
- **WHEN** 目标路径位于应用数据目录内
- **THEN** 返回校验错误并说明需另选位置（避免库自我复制、避免与库/日志混杂）

#### Scenario: 失败不留垃圾
- **WHEN** 导出过程失败
- **THEN** 中转文件被清理，目标文件不被破坏

