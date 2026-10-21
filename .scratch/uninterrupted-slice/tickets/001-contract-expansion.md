# 001 — 契约扩展：演示模式、安静投递与顺延设置

**Blocked by**: 无

**状态**: 已完成（2026-10-21）

## 目标

为后续切片一次性补齐共享契约与设置字段，保持行为不变。完成后 `make test` / `make lint` 全绿、绑定重新生成；后续切片不再反复改共享类型。

> 这是预重构（expand），不是用户可见切片。

## 涉及层

- [ ] 数据层：
  - `internal/pause/provider.go` 增加 `ReasonPresentation`（取值 `presentation`）。
  - `internal/pause/composite.go` 的 `reasonPriority` 给 presentation 最高优先级。
  - `internal/service/pause.go` 的 `PauseState` 增加 `presentation_active`、`presentation_source`、`presentation_exited_at`、`suggestion_visible`、`capture_exclusion_supported` 字段（零值，暂不填充）。
  - `internal/reminder/contract.go` 的 `FiredPayload` 增加 `quiet_delivery`。
  - `internal/config/config.go` 的 `ReminderSettings` 与 `Config` 增加 `presentation_suggest_enabled`（默认 true）、`natural_pause_enabled`（默认 true）、`natural_pause_max_seconds`（默认 60，合法 0/30/60）；`Default()`、`LoadReminderSettings`、`SaveReminderSettings` 同步。
- [ ] 逻辑层：
  - `app.go` 的 `pauseReasonZh` 增加「演示中」。
  - 引擎与绑定层暂不消费新字段；`GetPauseState` 返回零值，不改变现有行为。
- [ ] UI 层：
  - `frontend/src/lib/pause.ts` 增加 `Presentation: 'presentation'`，并更新优先级注释。
  - `frontend/src/lib/reminder.ts` 的 `ReminderFiredPayload` 增加 `quiet_delivery: boolean`。
  - 设置页不新增控件（后续切片各自加入）。
- [ ] 测试：
  - `internal/guard/parity_test.go` 通过；必要时补充新常量的镜像断言。
  - 新增设置往返测试：默认值、0/30/60、非法值回落 60。
  - `make gen` / `make lint` 重新生成并校验绑定。

## 验收标准

- `PauseReason` 新增 `presentation`，前端镜像一致，parity 测试通过。
- `FiredPayload` 新增 `quiet_delivery`，TS 镜像一致。
- `ReminderSettings` 三个新字段默认值正确；保存/读取往返；非法 `natural_pause_max_seconds` 回落 60。
- `GetPauseState` 返回的零值新字段不改变现有行为；启动、投递、暂停、通知、休息模式全部无回归。
- `make test`、`make lint` 通过。
- 覆盖 S-1（部分）。

## 实现记录（2026-10-21）

- 契约与设置字段已落地；`Settings.vue` 先把三个新字段读入并在保存时回传，避免保存其他提醒设置时把它们清零（实际控件留给 006 / 008）。
- 新增测试：`internal/pause` 的枚举与 presentation 最高优先级；`internal/config` 的默认值、往返、0/30/60 夹取；`internal/service` 与 `internal/reminder` 的 JSON 形状。
- 验证：目标包 `go test`、`make test`、`make lint` 均通过；绑定点已重新生成。
