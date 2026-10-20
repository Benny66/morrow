# smoke-test Specification

## Purpose
TBD - created by archiving change ci-release. Update Purpose after archive.
## Requirements
### Requirement: 冒烟可跑
`make smoke` MUST 构建后启动应用，经 bindings 做一次握手，断言成功，并清理进程。

#### Scenario: 冒烟通过
- **WHEN** 执行 `make smoke`
- **THEN** 应用构建、启动、绑定握手返回成功、退出，无残留进程

#### Scenario: 失败即报错
- **WHEN** 启动或握手失败
- **THEN** 冒烟返回非零退出码，且清理已启动的进程

