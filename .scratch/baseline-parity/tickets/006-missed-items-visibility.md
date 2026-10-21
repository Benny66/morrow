# 006 — 错过提醒可见

**Blocked by**: 001, 002, 003, 004

## 目标

暂停期间错过的每日单点或一次性提醒，在首页保留「已错过」可见条目，不补弹。复用现有 `Missed One-off` 语义。

## 涉及层

- [ ] 数据层：复用 `internal/model/reminder_fire.go` 的 `OneOffOutcome` 逻辑；`internal/service/checkin.go` 新增 `ListMissedDuringPause` 查询
- [ ] 逻辑层：`internal/reminder/engine.go` 在暂停期间记录错过的 fire（不落库，仅内存标记）；恢复后查询并标记为「已错过」
- [ ] UI 层：`frontend/src/views/Home.vue` 在待处理卡片下方新增「已错过」分组，显示错过的提醒标题与时间
- [ ] 测试：`internal/service/checkin_missed_test.go` 验证查询逻辑；手动验收

## 验收标准

- 暂停期间错过的每日单点/一次性提醒在首页显示「已错过」
- 不补弹、不投递
- 「已错过」条目包含提醒标题与计划时间
- `make test` 与 `make lint` 通过
- 覆盖 US-6
