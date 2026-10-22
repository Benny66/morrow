# 004 — 与定时提醒结合：休息跳过与目标抵扣

**Blocked by**: 003

**状态**: 已完成（2026-10-22；真实锁屏/Windows 实机待 011）

> 规格：`docs/specs/2026-10-22-sit-awareness-and-sassy-pet.md` 的 B-6、B-7、C-5–C-9、F-2、E-4。
> 本票把 003 的「只清零」升级为真正与定时提醒做加减法。

## 目标

用户刚真正休息过，10 分钟内到点的站立/走动提醒不再弹；用户点「起来走走」或满足条件的锁屏会自动抵扣当前场景最近到点的 stand/move 一次，首页目标进度立即 +1。喝水/护眼/如厕/一次性提醒完全不受影响。

## 涉及层

- [ ] 数据层：`internal/service/sit.go`
  - `CreditNearestStandMove(scenarioID uint, now time.Time, source string) (CreditResult, error)`：
    - 若有当前场景内待处理的 stand/move，优先完成最早的一条（复用 `act` 事务，source 传 `active_break` / `lock_break`）。
    - 否则在启用且今天仍有剩余触发点的 stand/move 中选 `NextFireAt` 最早的一条；写 `CheckIn{ReminderID, ScenarioID, ActionDone, ScheduledAt:0, Source:...}`。
    - 一个 `SitSession` 只允许一次抵扣：在事务里检查并写入 `credited_reminder_id` / `credited_check_in_id`；并发（主动休息 + 解锁记账）只成功一次。
    - 没有可用 stand/move 时不写 CheckIn，返回 `credited=false`。
  - `CheckIn.Source` 使用 002 的常量；统计查询仍按 `action=done`，不改完成率/连续天数语义。
  - 抵扣成功后由绑定层/运行时广播 `reminder:changed`（reason `check_in`）。

- [ ] 逻辑层：`internal/reminder`
  - `Options` 增加 `BreakProvider`；接口：
    `ShouldSkipStandMove(reminderID uint, fireAt int64, now time.Time) bool`。
  - `tickGridReminder` 在占用最新到点触发点之前询问；仅 stand/move、非一次性、非用户推迟的补发适用。
  - 返回 true 时把该触发点 claim 为 `FireSilent`（不投递、不进待处理、不算错过），不写 CheckIn；日志记录提醒 ID 与触发时刻。
  - 喝水/护眼/如厕/一次性/推迟补发不调用或直接返回 false；现有暂停/顺延/静默语义不变。
  - 引擎测试注入 fake provider，覆盖 skip / 不 skip / 其他 kind / 暂停优先。

- [ ] 逻辑层：`sit_runtime.go`
  - 把 `sit.Tracker` 作为 `BreakProvider` 注入提醒引擎；启动顺序：pause bundle → sit tracker → reminder engine → sit sampler。
  - `TakeActiveBreak` 从「只清零」升级为「清零 + 记录休息 + CreditNearestStandMove(source=active_break)」；返回 `SitActionResult`（message / credited / credited_title）。
  - 锁屏恢复时：锁屏时长 ≥5 分钟且锁屏开始时本次久坐 ≥30 分钟 → `CreditNearestStandMove(source=lock_break)`；同一会话与主动休息合计最多一次；锁屏低于门槛只清零。
  - 普通空闲 ≥15 分钟只清零、记录休息，不抵扣。
  - 抵扣成功广播 `reminder:changed`；失败记 warn，不阻塞休息/欢迎回来。
  - 按 001 结论处理 Windows Win+L：不可检测时只覆盖休眠/离开模式，文案明确降级。

- [ ] UI 层
  - 不新增页面；首页完成数/进度由既有 `reminder:changed` 刷新。
  - 宠物欢迎回来可带一句「这次记在『起身活动』上」；不显示应用名，不占主动发言预算。
  - 若没有可抵扣的 stand/move，欢迎文案只说休息已记下，不假装完成。

- [ ] 测试
  - service：待处理抵扣、无待处理抵扣、无 stand/move 不写、一个会话只一次、并发只一次、source 正确、跨午夜 DateKey、CheckIn 统计不变。
  - engine：stand/move 触发点 silent skip；喝水/一次性/推迟不 skip；skip 后无通知/无 pending/无 CheckIn；暂停/顺延优先。
  - 锁屏：29 分钟 + 锁 10 分钟不抵扣；31 分钟 + 锁 4 分钟不抵扣；31 分钟 + 锁 6 分钟抵扣；主动休息后再锁不重复。
  - Home 刷新与现有统计回归；`make test` / `make lint`。
  - 手工 demo：点起来走走 → 首页 +1；休息后 3 分钟到点 stand 不弹；锁屏 6 分钟 → +1。

## 验收标准

- 真实休息后 10 分钟内到点的 stand/move 静默跳过；喝水/护眼/如厕/一次性照常。
- 「起来走走」抵扣当前场景最近到点的 stand/move 一次；首页进度立即更新；同一会话不重复。
- 锁屏满足 5 分钟锁屏 + 30 分钟久坐才抵扣；普通空闲不抵扣；Windows 不可检测时如实降级。
- 不重排任何计划时间；不改变一次性/推迟/休息模式/强提醒语义。
- `SitActionResult` 能告诉用户这次是否抵扣、抵扣了哪条；没有可用项时不写 CheckIn。
- `make test` / `make lint` 通过；真实 GUI/锁屏走查记录。

## 实现记录（2026-10-22）

- 引擎：`reminder.BreakProvider` + `Options.BreakProvider`/`SetBreakProvider`；`tickGridReminder` 在最新 stand/move 触发点前询问，true 时落 `FireSilent`，不投递/不 pending/不写 CheckIn；喝水/一次性/推迟补发不询问（`break_skip_test.go`）。
- Tracker：`lastBreakAt` + `ShouldSkipStandMove`；主动休息在点击时、锁屏在解锁时、空闲休息在回来时开始 10 分钟跳过窗口（`tracker_test.go`）。
- Service：`CreditNearestStandMove(scenarioID, sitSessionID, now, source)`；待处理 stand/move 优先，否则选今天 `NextFireAt` 最早的一条；`actWithSource`/事务写 `CheckIn` 与 `sit_sessions.credited_*`；`sitCreditMu` 保证 active break 与锁屏只成功一次；测试覆盖 pending/no-pending/非 stand-move/最早到点/重复。
- 运行时：`startSitRuntime` 把 tracker 注入引擎；`TakeActiveBreak` 捕获会话 ID 后抵扣并广播 `reminder:changed`，返回 `credited/credited_title/message`；锁屏 `BreakEnded` 用 `ShouldCreditLock`（锁屏 ≥5m + 久坐 ≥30m）抵扣；`SitActionResult` 可见回执。
- GUI（2026-10-22）：临时 app + 预占历史触发点 + 3 分钟 stand；调用 `TakeActiveBreak` 返回 `credited=true`、`credited_title=起身活动`；`sit_sessions.end_reason=active_break`、`credited_reminder_id/check_in_id` 写入、`check_ins.source=active_break`、今日完成 1；下一次 3 分钟触发点落 `silent`、无 pending。验收后清理临时环境并重新打开 `/Applications/Morrow.app`。
- 未覆盖：真实锁屏转换（macOS 手工锁屏 1/6 分钟、Windows Win+L）与 Windows 实机；`rest mode` 完成已有自己的 CheckIn，不再额外抵扣（避免双计）；B-9 崩溃收尾留后续。
