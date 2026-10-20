# dependency-registry Specification

## Purpose
TBD - created by archiving change guardrails. Update Purpose after archive.
## Requirements
### Requirement: 依赖必须登记
新增直接依赖 MUST 在 `deps.yaml` 登记并附理由，护栏做双向校验。

#### Scenario: 漏登记
- **WHEN** `go.mod` 或 `package.json` 的直接依赖（非 indirect / 非 dev）未在 `deps.yaml` 登记
- **THEN** 护栏测试失败

#### Scenario: 僵尸条目
- **WHEN** `deps.yaml` 登记的条目在 `go.mod` 与 `package.json` 中均不存在
- **THEN** 护栏测试失败

#### Scenario: 登记含理由
- **WHEN** 查看 `deps.yaml` 的登记条目
- **THEN** 每条都附有引入理由说明

