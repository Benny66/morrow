# scaffold-init Specification

## Purpose
TBD - created by archiving change scaffold-init. Update Purpose after archive.
## Requirements
### Requirement: 母版占位符化
脚手架母版 MUST 将模块名/品牌名统一为占位符 `my-notice`，不含任何具体项目名残留。

#### Scenario: 无具体名残留
- **WHEN** 在母版中 grep 模块名
- **THEN** 所有原本写 `wails-bailer-starter` 的位置均为 `my-notice`，无具体项目名硬编码

### Requirement: 一键实例化
`scripts/init.sh <name>` MUST 从母版生成一个可编译的新项目，替换零遗漏、不含母版私货。

#### Scenario: 生成可编译项目
- **WHEN** 执行 `scripts/init.sh myapp`
- **THEN** 生成 `myapp` 项目，`make build` 通过，模块名/import/wails.json/index.html title 全部为 `myapp`

#### Scenario: 无母版私货
- **WHEN** 检查 init 生成的项目
- **THEN** 不含母版的 `.git` 历史、`.claude/settings.local.json`、归档历史 `openspec/changes/archive`

#### Scenario: 非法名拒绝
- **WHEN** 执行 `scripts/init.sh` 传入空值或含非法字符的名称
- **THEN** 脚本报错退出，不生成任何项目

### Requirement: 保留治理基线
init 生成的项目 MUST 保留 OpenSpec 治理结构（specs 能力基线 + AGENTS/CLAUDE 宪法 + deps.yaml + Makefile），仅清空母版开发历史。

#### Scenario: 治理基线在
- **WHEN** 检查 init 生成的项目
- **THEN** 存在 `openspec/specs/`（能力基线）、`AGENTS.md`、`CLAUDE.md`、`deps.yaml`、`Makefile`、`_example/`

### Requirement: 占位符替换不得被脚本自替换干扰
实例化脚本 MUST 以「不可能被自身替换命中」的形式书写自身的占位符检测串，
使残留断言不依赖 shell 的脚本读取时机。

#### Scenario: 断言在实例化副本中仍有效
- **WHEN** 实例化脚本被复制进新项目后再次阅读其断言
- **THEN** 断言检查的是真正的占位符字面量，而非被替换后的项目名

#### Scenario: 脚本变大仍不失效
- **WHEN** 实例化脚本长度超过 shell 的单次读取块
- **THEN** 替换与断言行为不变（不依赖「脚本已被整体读入内存」这一侥幸）

### Requirement: 占位符残留全部纳入断言
实例化 MUST 断言所有占位符均已替换干净，且对关键字段做回读校验。

#### Scenario: 残留即失败
- **WHEN** 任一占位符在生成的目录中仍有残留
- **THEN** 实例化脚本报错退出并列出残留文件

#### Scenario: 关键字段回读校验
- **WHEN** 实例化完成
- **THEN** 回读产物配置（如 macOS bundle id）确认替换真的生效，
  而非仅断言「没有残留」——替换没生效与替换错是两类不同的问题

