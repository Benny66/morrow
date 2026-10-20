# ci-pipeline Specification

## Purpose
TBD - created by archiving change ci-release. Update Purpose after archive.
## Requirements
### Requirement: 提交即检查
CI MUST 在提交后自动运行静态检查（Go 测试 + 护栏 + lint）。

#### Scenario: 静态检查失败即红
- **WHEN** 提交引入了护栏或 lint 违规
- **THEN** CI 失败，问题在合并前暴露

### Requirement: 生成器端到端验证纳入 CI
CI MUST 执行生成器端到端验证，使该检查不再只依赖开发者记得在本地运行。

#### Scenario: 生成器回归在 CI 变红
- **WHEN** 生成器产出的代码无法编译、不符格式或被护栏拦住
- **THEN** CI 失败，而不是等人工在本地跑验证脚本才发现

#### Scenario: 单腿执行即可
- **WHEN** 该检查与平台无关，且在部分平台上依赖不可用
- **THEN** 只在一条具备全部前置条件的腿（有构建工具链与所需系统命令）上执行一次，
  不重复三遍

### Requirement: 检查 job 自备编译前置条件
检查 job MUST 在执行编译类检查前满足其前置条件，不得假定检出目录中已存在构建产物。

#### Scenario: 嵌入产物的前置条件被满足
- **WHEN** 根包以嵌入方式依赖某个构建产物目录，而该目录不入库
- **THEN** 检查 job 在编译前先构建它，而不是直接失败

#### Scenario: 检查 job 真的能跑通
- **WHEN** 在干净检出上运行检查 job 的全部步骤
- **THEN** 每一步都通过，不出现「检查本身跑不起来」的长期红灯

### Requirement: 编译矩阵
CI MUST 编译并上传产物，且矩阵 MUST 只包含能稳定通过的平台。
编译结果取决于 runner 镜像装了什么系统包这类环境耦合，MUST NOT 作为矩阵成员长期维护。

#### Scenario: 矩阵内平台产物
- **WHEN** CI 运行编译 job
- **THEN** 产出 app（macOS）与 exe（Windows），并上传为 artifact

#### Scenario: 环境耦合的平台不进矩阵
- **WHEN** 某平台的编译结果取决于 runner 镜像装了什么系统包
- **THEN** 该平台不在编译矩阵内，其构建说明标注为「无 CI 验证」，
  而非以经常失败的形态留在矩阵中

#### Scenario: 不软化失败
- **WHEN** 一条腿无法稳定通过
- **THEN** 从矩阵移除，而不是用「允许失败」的方式让它红着不拦——
  后者会让失败长期无人处理

#### Scenario: Go 层覆盖不随之丢失
- **WHEN** 某平台因产物编译不稳定而退出矩阵
- **THEN** 它的 Go 层行为仍由静态检查腿覆盖（测试 / 护栏 / lint / 生成器端到端），
  只是不再承诺其产物可编译

### Requirement: 发版前先过静态检查
发布工作流 MUST 在构建安装包之前执行静态检查，MUST NOT 从检查不通过的提交发版。

#### Scenario: 红提交不发版
- **WHEN** tag 指向一个未通过测试或 lint 的提交
- **THEN** 发布流程在打包前失败，不产出也不创建 Release

#### Scenario: 检查通过才继续
- **WHEN** 静态检查通过
- **THEN** 进入各平台打包阶段

### Requirement: 发布产物在多平台间正确汇合
跨平台构建的产物 MUST 汇聚到同一次发布，且 MUST NOT 因并行而产生重复发布。

#### Scenario: 并行构建不重复创建
- **WHEN** 多个平台的构建并行完成
- **THEN** 发布只被创建一次，全部产物附加到同一个发布

#### Scenario: 打包失败不留空发布
- **WHEN** 某一平台的打包失败
- **THEN** 不创建（或不同步）缺少产物的发布

