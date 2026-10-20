# pipeline-contract Specification

## Purpose
TBD - created by archiving change frontend-pipeline-parity. Update Purpose after archive.
## Requirements
### Requirement: 错误协议结构化
绑定方法的业务错误 MUST 携带机器可读的错误码与面向用户的中文消息，
使前端能按码分流而不解析文案。

#### Scenario: 前端拿到结构化错误
- **WHEN** 绑定方法返回业务错误
- **THEN** 前端拿到 `{code, message}` 形状的错误对象，而非裸字符串或 `[object Object]`

#### Scenario: 绑定层不得自造错误
- **WHEN** `app.go` 的绑定方法直接使用 `errors.New` / `fmt.Errorf` 构造业务错误
- **THEN** 护栏失败，要求改用 `apperr` 构造

### Requirement: 分页协议结构化
列表类绑定方法 MUST 返回分页结果（含 list/total/page/page_size），
且查询前 MUST 归一化分页参数。

#### Scenario: 列表返回分页结果
- **WHEN** 调用列表类绑定方法
- **THEN** 返回结构含 `list` / `total` / `page` / `page_size`，
  且 `page`/`page_size` 为归一化后的回显值

#### Scenario: 页大小上下界为单一真相
- **WHEN** 请求的页大小超出上限或为 0
- **THEN** 被夹取/取默认值，且上下界常量只有一个出处

### Requirement: 事件协议为约定式
后端事件名 MUST 遵循 `<domain>:<action>`；进度类 payload MUST 含 `done`/`total`，
结束类 MUST 含 `ok`。

#### Scenario: 事件名合规
- **WHEN** 后端向前端推送事件
- **THEN** 事件名形如 `asset:progress`，domain 与业务模块名一致

### Requirement: 契约前端侧必须存在
三类管道契约 MUST 同时提供前端侧消费入口，使「遵守契约」比「绕过契约」更省事：
错误用 `src/lib/invoke.ts`，事件用 `src/lib/event.ts`，分页用 `src/lib/page.ts`
（加载辅助在 `src/composables/`）。

#### Scenario: 事件订阅不需要手拼事件名
- **WHEN** 下游要在组件里接收后端进度事件
- **THEN** 用 `eventName(domain, action)` 拼名、`useEvent` 订阅，
  无需手写 `'asset:progress'` 字符串字面量，也无需自己记着解绑

#### Scenario: 列表页不需要重写分页样板
- **WHEN** 下游要写一个分页列表页
- **THEN** 用 `usePagedList(fetcher)` 拿到 `list/total/page/pageSize/loading/error/load`，
  不重复手写这套状态与加载流程

#### Scenario: 归一化逻辑不在前端重复实现
- **WHEN** 前端需要归一化分页参数（页码下界、页大小上下界）
- **THEN** 它不自行实现，而是把原始值交给 Go，并以 Go 回填的 `page`/`page_size` 为准
  （一件规则只允许有一个实现）

### Requirement: 事件订阅必须可解绑
前端订阅后端事件时 MUST 能解除订阅，且在组件中使用时 MUST 自动解绑。

#### Scenario: 组件卸载自动解绑
- **WHEN** 组件在 setup 中用 `useEvent` 订阅了事件，随后被卸载
- **THEN** 该订阅被解除，组件重新挂载不会导致同一事件触发多次

#### Scenario: 非 wails 环境下订阅可退化
- **WHEN** 前端运行在非 wails 环境（如纯 vite dev）
- **THEN** 订阅返回一个空取消函数并在控制台留下原因，不抛错阻断页面渲染

### Requirement: 错误码取值前端可分流
前端 MUST 能以具名常量（而非字符串字面量）判断错误码，且常量取值与 Go 侧一致。

#### Scenario: 按错误码分流
- **WHEN** 绑定调用失败并被归一化为 `AppError`
- **THEN** 前端可用 `ErrorCode.NotFound` 等具名常量与 `err.code` 比较，
  无需硬编码 `'not_found'`

#### Scenario: 取值漂移被护栏拦住
- **WHEN** Go 侧错误码取值变更而前端镜像未同步
- **THEN** `make test` 失败

