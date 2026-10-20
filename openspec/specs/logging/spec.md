# logging Specification

## Purpose
TBD - created by archiving change runtime. Update Purpose after archive.
## Requirements
### Requirement: 双写日志
应用 MUST 同时输出控制台与文件日志，并支持分级。

#### Scenario: 分级输出
- **WHEN** 应用在开发态运行
- **THEN** Debug 级日志输出到控制台；生产态输出 Info 级

#### Scenario: 文件落盘
- **WHEN** 应用运行
- **THEN** 日志写入用户配置目录下的日志文件

### Requirement: 日志轮转
日志文件 MUST 按大小自动轮转，避免无限增长。

#### Scenario: 达到大小即轮转
- **WHEN** 日志文件超过设定大小
- **THEN** 自动轮转为新文件，保留设定份数，旧文件归档

### Requirement: 框架与前端日志接入同一出口
Wails 的日志接口 MUST 接入应用日志，使前端与框架内部产生的日志落到同一个文件。

#### Scenario: 前端日志落盘
- **WHEN** 前端调用 Wails 的日志方法（如 `LogError`）
- **THEN** 该条日志写进应用的 `app.log`，并带有可区分来源的标记

#### Scenario: 框架内部日志落盘
- **WHEN** Wails 自身产生内部日志（资源加载失败、绑定错误等）
- **THEN** 同样写进 `app.log`，而不是只出现在无人可见的 stdout

#### Scenario: 接线缺失会红
- **WHEN** 窗口选项未设置日志接口
- **THEN** 护栏失败并说明后果（前端日志会静默丢失）

#### Scenario: 致命级别不重复退出
- **WHEN** 框架调用日志适配器的致命级别方法
- **THEN** 只记录日志、不调用进程退出（框架随后会自行退出，
  提前退出会让日志 flush 与资源释放来不及执行）

### Requirement: 构建模式决定日志级别
日志级别 MUST 由构建模式决定，且生产构建的判定必须真实有效。

#### Scenario: 开发构建输出调试日志
- **WHEN** 以开发模式运行（`wails dev` / 裸 `go build`）
- **THEN** 输出 Debug 及以上级别

#### Scenario: 生产构建不输出调试日志
- **WHEN** 以生产模式构建（`wails build`）并运行
- **THEN** 只输出 Info 及以上级别，避免调试噪音快速吃掉轮转窗口、
  把真正的错误冲走

### Requirement: 启动首行记录版本与路径
每次启动 MUST 在日志首行记录版本与日志文件自身路径。

#### Scenario: 日志自带版本上下文
- **WHEN** 查看任一 `app.log`
- **THEN** 首行含应用版本与平台，无需再问用户「你用的是哪个版本」

#### Scenario: 从日志定位日志
- **WHEN** 用户只提供了日志内容的一段
- **THEN** 可从中读到日志文件的绝对路径，便于取得完整日志

### Requirement: 启动阶段时间线
启动过程 MUST 按阶段记录耗时，使「启动慢在哪一段」可回答，而非只有首尾两个时间点。

#### Scenario: 组合根各阶段可见
- **WHEN** 应用启动
- **THEN** 日志中出现各阶段（日志初始化 / 数据库 / 配置等）各自的耗时与累计耗时

#### Scenario: 生成器与范例不受影响
- **WHEN** 生成新模块后构建运行
- **THEN** 启动阶段记录照常输出（打点在组合根，不在业务代码）

### Requirement: 慢查询进入同一日志出口
数据库的慢查询与错误 MUST 进入应用日志文件，MUST NOT 只写到标准输出。

#### Scenario: 慢查询可见
- **WHEN** 某次查询耗时超过配置的慢查询阈值
- **THEN** 该查询（含耗时与语句）写入 `app.log`

#### Scenario: 阈值与本地环境相称
- **WHEN** 检查慢查询阈值配置
- **THEN** 它是一个与本地嵌入式数据库相称的量级，而非远程数据库的默认值

#### Scenario: 记录不存在不算错误
- **WHEN** 查询按业务预期未命中记录
- **THEN** 不记错误日志（那是正常路径，记下来会淹没真正的问题）

#### Scenario: 接线缺失会红
- **WHEN** 数据库连接未配置日志实现
- **THEN** 护栏失败并说明后果（慢查询回到无人可见的标准输出）

