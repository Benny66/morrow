# architecture-guardrails

Go AST 护栏 + ESLint 自定义规则，把分层与注册约束编译成会红的检查。

## MODIFIED Requirements

### Requirement: 可观测性接线不得静默缺失

「删掉后不会报错、只是失去可见性」的观测接线 MUST 由护栏核对。护栏的解析目标 MUST 随框架版本同步，且解析到 0 个目标 MUST 报错。

框架升级会改变接线的**书写形式**（如 Wails v2 的 `options.App.Logger` 在 v3 是 `application.Options.Logger`）。形式变了而护栏没跟，护栏不会静默通过——它会因为解析不到目标而 Fatal。但**字段断言本身必须保留**：删掉适配器不等于删掉护栏。

#### Scenario: 数据库日志接线被核对
- **WHEN** 数据库连接未配置日志实现
- **THEN** 护栏失败并说明后果（慢查询回到无人可见的输出）

#### Scenario: 启动打点被核对
- **WHEN** 组合根的启动阶段打点被移除或数量异常减少
- **THEN** 护栏失败，说明后果（启动优化重新变成盲调）

#### Scenario: 框架日志接线跨版本仍被核对
- **WHEN** Wails 升级后 `Options.Logger` 的接线被移除
- **THEN** 护栏仍然失败——解析目标随框架更新，但断言未删除

#### Scenario: MCP 标签不得进默认构建
- **WHEN** 构建脚本或 CI 中被加入裸的 `mcp` 构建标签
- **THEN** 护栏失败并说明后果（默认产物多开一个能执行 JS 的本机端口）

#### Scenario: 解析不到即报错
- **WHEN** 护栏在目标文件中解析到 0 个打点或 0 个目标字段
- **THEN** 报错并提示同步更新护栏解析规则，而非静默通过

### Requirement: 绑定方法名一致

`app.go` 里服务结构体的每个导出方法 MUST 有对应的绑定生成文件，防手改绑定导致 Go↔TS 漂移。

绑定产物路径随框架版本变化（v2 为 `frontend/wailsjs/go/main/App.d.ts`，v3 为 `frontend/bindings/**`），护栏 MUST 跟随，且解析不到绑定目录 MUST 报错。

#### Scenario: 生成绑定与 Go 方法一致
- **WHEN** 服务结构体的导出方法在绑定产物中不存在
- **THEN** 护栏失败并提示重新生成绑定

#### Scenario: 路径变更即报错
- **WHEN** 框架升级后绑定产物路径变化而护栏未更新
- **THEN** 护栏报错并提示同步更新解析规则，而非静默通过
