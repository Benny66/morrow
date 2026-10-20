# observability Specification

## Purpose
TBD - created by archiving change runtime-pipeline. Update Purpose after archive.
## Requirements
### Requirement: 前端日志有正式出口
前端 MUST 提供统一日志出口，把日志送到应用日志文件，而非停留在控制台。

#### Scenario: 日志进入文件
- **WHEN** 前端调用日志出口记录一条信息
- **THEN** 该条写进 `app.log`（生产态无 DevTools，控制台是无人可见的输出）

#### Scenario: 非框架环境安全退化
- **WHEN** 前端运行在没有注入运行时的环境（纯 vite dev）
- **THEN** 日志退化为控制台输出且不抛错，调用方无需判断环境

#### Scenario: 多行内容不破坏行式日志
- **WHEN** 日志内容含换行或超长对象
- **THEN** 压成单行并截断，不破坏逐行解析

### Requirement: 未捕获异常必须留痕
前端 MUST 安装全局错误兜底，覆盖同步错误、未处理的 Promise 拒绝与组件树内异常。

#### Scenario: 未处理的 Promise 拒绝被记录
- **WHEN** 某处绑定调用漏了 catch 导致 Promise 拒绝
- **THEN** 该异常被归一化后写进日志，而不是静默消失

#### Scenario: 同步错误与资源加载失败被记录
- **WHEN** 发生 window 级错误（含资源加载失败这类没有 error 对象的）
- **THEN** 同样被记录

#### Scenario: 组件树内异常被记录
- **WHEN** Vue 组件在渲染或生命周期中抛错（window 事件覆盖不到）
- **THEN** 经框架的错误钩子被记录

#### Scenario: 兜底在挂载前安装
- **WHEN** 应用启动过程中发生异常
- **THEN** 已能被捕获（安装早于应用挂载）

#### Scenario: 呈现方式可被下游接管
- **WHEN** 下游要把异常呈现给用户（toast/弹窗）
- **THEN** 可通过兜底的 hook 参数接管，基座不预置 UI

### Requirement: 应用信息可查
前端 MUST 能查询应用的版本、平台与关键路径。

#### Scenario: 查询应用信息
- **WHEN** 前端调用应用信息绑定方法
- **THEN** 返回版本、提交、平台、数据目录与日志文件路径，
  使排障时用户念一次返回值即可

#### Scenario: 版本可追溯
- **WHEN** 查看发布产物的版本
- **THEN** 版本来自构建期注入（打包脚本注入 `git describe` 的结果），
  而非固定字符串

### Requirement: 启动分段可测
前端 MUST 记录启动各分段的耗时，且 MUST 覆盖「脚本尚未开始执行」的那一段。

#### Scenario: 分段可见
- **WHEN** 应用启动完成
- **THEN** 日志中出现到脚本就绪、主题往返、挂载各自耗时与总计

#### Scenario: 覆盖脚本执行之前的耗时
- **WHEN** 大量依赖在应用入口执行之前被解析执行
- **THEN** 这部分耗时由浏览环境的导航计时覆盖，不因计时起点选在入口文件而丢失

#### Scenario: 阻塞首帧的往返可被量化
- **WHEN** 应用在挂载前等待一次后端往返（如读取主题）
- **THEN** 该往返的耗时作为独立分段出现在日志中，供判断这一取舍是否值得

### Requirement: 绑定调用耗时采样
前端 MUST 对绑定调用计时，并在耗时异常时留下可定位的日志。

#### Scenario: 慢调用告警
- **WHEN** 某次绑定调用超过阈值
- **THEN** 记录一条告警，含耗时与调用点标签

#### Scenario: 调用点标签由调用方提供
- **WHEN** 调用方未提供标签
- **THEN** 如实记为未标注，而非猜测一个可能误导的名字

#### Scenario: 启动期聚合不随调用量增长
- **WHEN** 应用启动完成
- **THEN** 启动日志中包含调用次数、最大耗时与最慢调用标签的聚合，
  且不因此逐条记录每次调用

