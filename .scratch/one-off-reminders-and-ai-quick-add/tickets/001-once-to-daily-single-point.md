# 旧 Once 收编为每日单点（含模型预重构）

**Blocked by**: 无

> 规格：US-18、US-19、US-20、US-34、US-38、US-39

## 目标

用户不再看到「仅一次」；`interval_min = 0` 成为「每日单点」；历史 `once=true` 数据迁移后仍每天响；模型新增 `once_at` 并把 `scenario_id` 变为可空，为后续一次性提醒腾位。

## 涉及层

- [ ] 数据层
  - `model.Reminder` 新增 `OnceAt int64`（`index;not null;default:0`，0 表示周期项），`ScenarioID` 改为可空（`*uint`），移除 `Once`。
  - `internal/database` 增加一次性迁移：SQLite 需要重建 `reminders` 表；把 `once=1` 转为 `interval_min=0`，保留其余字段与关联数据，删除 `once` 列。
  - 用旧 schema fixture 验证迁移；导出/导入旧库时 `database.Init` 走同一迁移。
- [ ] 逻辑层
  - `service.ReminderInput` 移除 `Once`；本切片不开放 `once_at > 0` 的写入。
  - 删除 `normalizeOnceWindow` / `onceLatestStartMinute` 及「完成后停用」分支；`interval_min = 0` 合法且为每日单点。
  - `normalizeReminderInput`、`UpdateReminder`、`SetReminderEnabled` 适配可空 `ScenarioID`；周期项仍必须有场景。
  - `ai.ReminderDraft` 移除 `Once`；Plan 提示词、`ParsePlanText`、`GeneratePlan`、`GeneratePlanFromInput` 只生成周期项。
- [ ] UI 层
  - Plan 页移除「重复方式：每天 / 仅一次」单选、`once` 表单字段与「仅一次」标签。
  - `interval_min = 0` 的列表元信息显示「每天 HH:MM 一次」；表单提示改为「0 = 每天同一时刻一次」。
  - Plan 页 AI 面板移除一次性标签与 `draftToInput` 里的 once 收敛。
- [ ] 测试
  - 迁移：旧库 `once=1` / `once=0` 两类数据，迁移后分别成为 `interval=0` / 原间隔；无数据丢失；新库可写入 `scenario_id = NULL`。
  - 服务：完成一条 `interval=0` 的周期项后不停用，次日仍可调度。
  - AI：Plan 解析 fixture 不包含 `once`；提示词不出现一次性规则。
  - `make test`、`make lint` 绿；生成的绑定不再含 `once`。

## 验收标准

- Plan 页不存在「仅一次」选项或标签；`interval=0` 显示为「每天 HH:MM 一次」。
- 新建/编辑一条 `interval=0` 的周期提醒，完成一次后次日仍会触发；不会被永久停用。
- 旧库启动不报错；`once=true` 迁移为每日单点，不是新的一次性提醒。
- `reminders` 表存在 `once_at`（默认 0）、`scenario_id` 可空；旧 `once` 列不再被模型/接口读写。
- Plan 页 AI 只生成周期项，不会产出一次性项。
- `make test` 与 `make lint` 通过。
