# 001 — 预警存活判据换成"目标触发是否落定"

**Blocked by**: 无

**状态**: 已完成（2026-10-22；单测 + 护栏 + 注入证据；GUI 走查转 005）

> 规格：`docs/specs/2026-10-22-rest-warning-survival.md` 的 WS-1…WS-8、§2.3 时序契约、§5.2 的 WS-T1/T3/T4/T5/T6/T7/T8。
> 术语：`CONTEXT.md` 的「休息预警（Rest Warning）」。取舍：`docs/adr/0012-rest-warning-survival.md`。
> 本票只做「预警该撤的时候撤得掉」；暂停期的撤销留给 002，措辞留给 003。

## 目标

用户看到的那句「1 分钟后该起身拉伸了」不再能跨越它预告的那次投递——目标触发点一旦被占用成任何终态（安静投递、静默时段、真实休息后跳过），或那条提醒项已不再是当前场景里启用中的 rest 提醒，预警就在下一轮 tick 撤销；顺延仍在排队时继续保留。

## 涉及层

- [x] 数据层：无表结构变更。"落定"这一事实继续由 `reminder_fires` 上的 `(reminder_id, fire_at)` 唯一索引 + 引擎内存备忘 `Engine.claimed` 承载（决策 D-7，不新增 `Store` 方法）。
  - 已核实四类"没进倒计时"的分支都在 `ClaimFireWithStatus` 之后调过 `e.markClaimed`：
    安静投递（`engine.go:517-527` → `:630` 跳过 Enter）、暂停期周期点（`:505-512`）、
    静默时段（`dispatch` 于 `:601-609` 早退）、休息后静默跳过（`:546-554`）。
  - 顺延分支（`deferralMode` 返回 `deliveryDeferMore`，`:485-490`）在 claim 循环**之前** return，
    所以"未占用"天然为真——这正是 WS-5 要保留预警的机制，不额外加判断。
- [x] 逻辑层：`internal/reminder/engine.go` 的 `tickWarnings`
  - 删掉「目标时刻已过就先保留预警」这条无上限分支（`:327-339` 里的 `cur.PlannedFireAt <= nowUnix → return nil`）。
  - 换成两条判据，任一不成立即 `CancelWarning()`：
    1. 预警的 `ReminderID` 仍出现在本轮 `ActiveReminders(scenario.ID)` 的 rest 候选集合里（未删除、未停用、场景未切走/停用）；
    2. `e.isClaimed(ReminderID, PlannedFireAt)` 为 false。
  - 键的选择：用 `RestRequest.ReminderID` + `PlannedFireAt`，不要用 `FireID`——
    Warning 阶段 `FireID` 存的就是 reminderID（`machine.go:56-57` 那条"兜底"注释），
    本票顺手把该注释改成「Warning 阶段 FireID 未使用，落定判据读 ReminderID + PlannedFireAt」，
    避免下一个读者以为两个字段语义相同。
  - 保留现有去重分支（`:340-345`），它防止每个 tick 重置 start。
  - 补日志：进入预警与撤销各一条 `slog.Info`，带 `reminder_id`、`planned_fire_at`、撤销原因（落定/不在场景）。
    今天这条链路全程零日志，是它排查了 23 分钟才定位到根因的直接原因。
- [x] UI 层：不改。撤销经 `GetRestModeSnapshot` 生效——宠物 4 秒轮询（`Pet.vue:762`）、
  菜单栏每秒（`app.go:1122`）。本票只需确认 `state` 变 `idle` 后气泡链落到下一支（`Pet.vue:1264` 之后）。
- [x] 测试（全部打在 `Engine.Tick()` + `Machine.State()/Current()` 现有接缝，参考 `engine_warning_test.go`）
  - WS-T1 安静投递占用 → 下一轮 `Tick()` 后 `idle`
  - WS-T3 静默时段到点 → `idle`
  - WS-T4 `shouldSkipStandMove` 为真 → `idle`
  - WS-T5 提醒项停用且目标时刻**已过** → `idle`（今天的第二种撤不掉形态；
    注意现有 `TestTickWarningsCancelWhenNoCandidates` 用的是 `PlannedFireAt: 9999999999`（未来），
    它盖不住这条，不要以为已经测过了）
  - WS-T6 顺延中（目标点未占用）→ 保持 `warning`
  - WS-T7 假时钟恰好等于计划时刻的一轮 `Tick()` → 终态是 `countdown`（正常路径的会话不丢）
    【原稿把这条写成"锁住 tickWarnings 先于 tickGrid 的顺序契约"，实现时的注入实验推翻了这个说法，
    见下方实现记录与规格 §2.3——顺序不是判据的前提】
  - WS-T8 撤销后下一条 rest 提醒进入 60 秒窗口 → 能重新 `warning` 且 `RemainingSec > 0`
  - WS-1 回归护栏：正常到点进倒计时、`>60 秒不预警`、`water 类不预警` 三条既有用例必须继续绿。
- [x] 文档：`engine.go:254-257` 与 `:275-282` 的注释从"空窗保留"改为"落定即撤销"；`ADR 0012` 路径引用校对。

## 验收标准

- `make test` / `make lint` 全绿，含上述 7 条新用例。
- WS-T1 与 WS-T5 必须"先把判据改错（例如仍用时间宽限）能让用例变红"再改回来——
  只跑一次全绿不算证据（这两条是本票的靶心）。
- 日志里能看到一次完整的「进入预警 → 撤销预警（原因=落定）」，不用靠猜。
- 现网那条僵尸预警（重启即清）不在本票范围内验证，实机走查留给 005。

## 边界

- 不撤销时机的暂停分支（002）、措辞（003）、护栏（004）。
- 不改 `WarningLeadTime`、不改顺延上限、不改安静投递是否进休息模式。
- 不新增 `Store` 方法、不查 DB 判落定（跨日尾部代价见规格 §7，写测试时按"可晚一个 tick"要求）。

## 实现记录（2026-10-22）

- 逻辑：`internal/reminder/engine.go` 的 `tickWarnings` 里那条"目标时刻已过就保留预警"的无上限分支
  换成 `warningStillPending(reminders, cur)` —— 两条同时成立才保留：那条提醒项仍在当前启用场景里
  且 kind 未变（`ActiveReminders` 只返回 `enabled = true`，停用/删除/切场景自然落在这条之外），
  以及该 `(ReminderID, PlannedFireAt)` 尚未被 `e.isClaimed` 占用。撤销仍走 `CancelWarning()`，
  `Machine` 与 `Store` 都没改。
- 日志：`tickWarnings` 现在有「保留 / 撤销」两条 `slog.Info`，带 `reminder_id` 与 `planned_fire_at`；
  之前这条链路全程零日志，是事故排查耗时 23 分钟的直接原因。
- 测试（`internal/reminder/engine_warning_test.go`，全部打在 `Engine.Tick()` + `Machine.State()/Snapshot()`）：
  `TestWarningClearedAfterQuietDelivery`、`TestWarningClearedWhenReminderDisabled`、
  `TestWarningSurvivesWhileDeferring`、`TestWarningAtFireTickEntersCountdown`、
  `TestWarningCanBeRaisedAgainAfterCancel`（断言重新预警的 `RemainingSec == 40`）、
  `TestWarningClearedDuringQuietHours`、`TestWarningClearedAfterRealBreak`。
  装配辅助 `standWarningStore`（生效时段起点即第一个触发点，避免积压点污染断言）。
- **注入证据**：
  - 注入 A（时间宽限 40 秒）→ 停用 / 静默时段 / 休息后跳过 / 既有的 `TestTickWarningsCancelWhenNoCandidates` 变红；
  - 注入 A2（时间宽限 80 秒，够跨过顺延窗口）→ 安静投递 / 停用 / 重新预警 / 静默时段 / 休息后跳过**五条全红**。
    这就是 ADR 0012 否决"时间宽限"的量化依据：宽限短了盖不住顺延，长了就盖不住落定，两头不能同时满足；
  - 注入 B2（无候选一律撤销，即抹掉保活）→ 只有 `TestWarningSurvivesWhileDeferring` 红。
- **对票文与规格的一处更正**：票里把 WS-T7 写成"锁住 `tickWarnings` 先于 `tickGrid` 的顺序契约"，
  注入 B2 证明这个说法不成立——那一轮即便先撤销，`tickGrid` 随后 `Enter` 也接受 `idle`，终态仍是
  `countdown`，测试区分不出顺序。测试已改名 `TestWarningAtFireTickEntersCountdown` 并在注释里写清它
  只断言"到点那轮会话不丢"；保活的真实作用域是跨多轮 tick 的顺延窗口。规格 §2.3 与 WS-6/WS-T7 同步更正。
- 验证：`make test`、`make lint` 全绿（17 个 Go 包 ok，0 FAIL）。

