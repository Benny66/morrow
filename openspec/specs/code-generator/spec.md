# code-generator Specification

## Purpose
TBD - created by archiving change example-module. Update Purpose after archive.
## Requirements
### Requirement: 黄金范例
`_example/` MUST 是最小、干净、带 `// TODO` 锚点的模板，且无任何业务残留。

#### Scenario: 范例最小干净
- **WHEN** 检查 `_example/` 模板
- **THEN** 模板仅含 model/service/绑定方法/前端页面四段骨架，无资产特有字段或业务逻辑

### Requirement: 生成器锚点注入
`make gen name=<tool>` MUST 通过锚点注入新模块，且 fail-fast 前置校验。

#### Scenario: 生成新模块
- **WHEN** 执行 `make gen name=asset`
- **THEN** 生成对应 model/service/绑定方法/前端页面，并在 `AllModels()` 等锚点处注入登记

#### Scenario: 拒绝覆盖
- **WHEN** 目标模块文件已存在
- **THEN** 生成器报错退出，绝不覆盖已有业务代码

#### Scenario: 锚点缺失即失败
- **WHEN** 目标文件中的生成锚点被删除
- **THEN** 生成器在动任何文件前报错，不留下不一致状态

### Requirement: TODO 锚点
生成的文件 MUST 带 `// TODO` 锚点，供后续只填业务逻辑处。

#### Scenario: 生成含 TODO
- **WHEN** 生成器产出新模块文件
- **THEN** 文件含明确的 TODO 锚点标记业务逻辑待填位置

### Requirement: 开箱无业务残留
脚手架开箱 MUST 不含任何示例业务模块（模型/服务/绑定/页面），只交付模板与生成器。

#### Scenario: 零业务代码
- **WHEN** 检查脚手架正式代码
- **THEN** `internal/model` 仅含 BaseModel 注册表、`internal/service` 仅含聚合骨架、`app.go` 无示例绑定方法、前端无示例业务页面

#### Scenario: 模板验证不残留
- **WHEN** 用 `make gen` 生成临时模块验证模板可用后
- **THEN** 生成的临时模块被删除，正式代码恢复干净

### Requirement: 生成物格式立即合规
生成器 MUST 保证产出的代码直接通过项目静态检查，不得让用户生成完就去手工格式化。

#### Scenario: 生成后 lint 不红
- **WHEN** 执行 `make gen name=<module>`
- **THEN** 生成物立即通过 `make lint` 的 gofmt 检查（导入顺序等由生成器负责排好）
- **AND** 端到端验证脚本对此有断言，防止回归

### Requirement: 生成的页面不复制分页样板
生成的前端列表页 MUST 复用基座的分页加载辅助，而非在每份页面里重抄分页状态与加载流程。

#### Scenario: 生成页使用基座辅助
- **WHEN** 查看 `make gen` 产出的列表页
- **THEN** 它用 `usePagedList` 承载 loading/error/页码/总数，页面代码只保留业务表达

