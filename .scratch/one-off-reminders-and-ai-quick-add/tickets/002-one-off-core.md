# 一次性提醒核心链路

**Blocked by**: 001

> 规格：US-01 ~ US-17、US-34 ~ US-36

## 目标

用户可在没有任何场景时手动创建一次性提醒，在首页「即将到来」看到它；到点（±60 秒）触发并进入待处理，错过则保留「已错过」；完成/跳过/推迟可用；场景启停不影响它。

## 涉及层

- [ ] 数据层
  - `service.ReminderInput` 新增 `OnceAt int64`；一次性项以 `scenario_id = 0` 表达，落库为 `NULL`。
  - `Service` 注入可注入时钟（默认 `time.Now`），测试用固定时钟。
  - 一次性写入校验：标题/正文上限、未来时间、场景为空、`interval_min`/`daily_target` 强制 0、`enabled` 恒 true；禁止改调度类型或挂场景。
  - `ListOneOffs` 返回未触发与已错过两类；`DeleteReminder` 清理其 fire。
  - `act` 对一次性项跳过 `CheckIn` 写入；保留完成/跳过/推迟的 fire 状态机。
  - `ListPendingFires` 改 LEFT JOIN 场景 + `COALESCE`，新增 `is_one_off`；`PendingFire.ScenarioID` 对一次性为 0。
  - `reminder.Store` 新增 `DueOneOffs(now)`；错过判定的 fire 初始状态即 `missed`（或与 claim 同事务）。
  - `DueFire` 增加 `IsOneOff`；推迟补发查询填充它。
- [ ] 逻辑层
  - `reminder.OneOffOutcome(onceAt, now, grace)` 纯函数；grace = 60 秒。
  - 引擎新增 `tickOneOffs`：到点且在容差内 → claim + `dispatch`（quiet 强制 false）；超过容差 → claim + `missed`，不广播/不通知；不依赖 `ActiveScenario`。
  - 重复 tick / 重启不重复触发；推迟队列对一次性照常补发，且补发同样豁免静默时段。
  - `FiredPayload` 增加 `IsOneOff`（前端镜像同步）。
  - `app.go` 增加 `ListOneOffs` 绑定；`CreateReminder` / `UpdateReminder` 走同一写入路径。
- [ ] UI 层
  - 首页快速添加（本切片只有手动表单）：标题、正文、日期、时间；保存后清空并刷新「即将到来」。
  - 首页「即将到来」区：未触发可按时间编辑/删除；已错过标记「已错过」、只可删除；无场景时仍展示。
  - 待处理卡片：一次性项显示「一次性」标识，不渲染空场景分隔符；日期+时刻完整展示；完成/跳过/推迟可用。
  - 强提醒窗口：一次性项不显示空场景行，三个动作可用。
  - Plan 页不展示一次性项（它们不属于场景）。
- [ ] 测试
  - `OneOffOutcome` 表驱动：未到点、正好到点、容差内、正好 60 秒、超过、时钟回拨。
  - 引擎假 Store + 假时钟：触发/错过/静默豁免/无场景/场景停用/重复 tick/重启/推迟补发/错过初始状态。
  - 服务临时库：创建、编辑、删除、过去时间拒绝、不变量、`ListOneOffs`、待处理 LEFT JOIN、`act` 不写 CheckIn。
  - UI E2E：建 1–2 分钟后的一次性提醒 → 可见 → 到点出现卡片/通知 → 完成；另测跳过、推迟、错过、场景停用。
  - `make test`、`make lint` 绿。

## 验收标准

- 无场景、旧库、全新库都能手动创建一次性提醒；UI 不要求选择场景。
- 到点 ±60 秒进入待处理；超过 60 秒或应用未运行 → 标记「已错过」，不发通知、不补发。
- 待处理卡片可完成/跳过/推迟；推迟到点后仍补发，重启也不丢；同一时刻不重复触发。
- 一次性触发不受静默时段影响；通知总开关关闭时仍有待处理卡片。
- 一次性项不出现在 Plan 页/今日进度；完成/跳过不写 CheckIn。
- 删除未触发或已错过项后，列表与数据库一致，fire 一并清理。
- `make test`、`make lint` 通过。

## 不在本切片

- 首页「下一条」倒计时与统计隔离的显式断言（003）。
- AI 生成、附件、粘贴/拖拽（004–006）。
