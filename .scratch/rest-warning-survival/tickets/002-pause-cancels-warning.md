# 002 — 进入暂停立即撤销预警，解除后按真实窗口重扫

**Blocked by**: 001

**状态**: 已完成（2026-10-22；单测 + 护栏 + 注入证据；GUI 走查转 005）

> 规格：`docs/specs/2026-10-22-rest-warning-survival.md` 的 WS-9…WS-12、§5.2 的 WS-T2。
> 取舍：`docs/adr/0012-rest-warning-survival.md`（备选方案里"给时间宽限加上限"就是被这条场景否决的——
> 暂停期间 `tickWarnings` 整轮不跑，任何基于时间的上限都没机会执行）。

## 目标

用户锁屏 / 进会议 / 挂机离开时，宠物头顶那句预警立刻消失，换成「屏幕已锁定，我先不打扰」；
解锁回来不会看到一条挂了半小时的假预警。这正是 2026-10-22 实机事故（17:15 锁屏压在预警窗口内，
气泡挂到 17:38 之后）的复现路径。

## 涉及层

- [x] 逻辑层：`internal/reminder/engine.go` 的 `Tick`
  - 现状：`if !paused && e.restMode != nil { tickWarnings }`（`:256-261`）——暂停时既不新建也不撤销。
  - 改成：暂停时也推进休息模式状态机一步，只做撤销不做扫描。
    优先形态是把 `paused` 传进 `tickWarnings`（或在 `if paused { e.clearDeferrals() }`（`:246-249`）里补一句撤销），
    两种都能满足 WS-9；**选定后在本票的注释里写清"暂停期 tickWarnings 是否仍会看到候选"**，
    别留成一个读代码要猜的岔口。
  - 撤销必须走 `restmode.Machine.CancelWarning()`，不新增状态、不改 `Machine`。
  - WS-11 的重扫是 001 判据的自然结果（暂停解除后下一轮按当时真实 60 秒窗口重算），
    本票只需把它测出来，不需要额外代码。
- [x] UI 层：`Pet.vue` 优先级链**不动**（陪做 > 预警 > 顺延 > 演示建议 > 暂停 > 待处理 > 久坐软提醒，
  即 `:1259/:1264/:1269/:1276/:1301/:1310` 这条 v-if 链）。
  暂停时预警已在服务端变 `idle`，暂停气泡自然上位——这是 WS-12 的机制，
  所以**禁止**用"把预警的 UI 优先级降到暂停之后"来绕（那是 ADR 0012 明确否决的遮丑做法）。
- [x] 测试（接缝同 001）
  - WS-T2 暂停中周期点被占用成 `silent`（`engine.go:505-512`）→ 该轮 `Tick()` 后 `idle`
  - WS-9 预警存活 → provider 翻成 paused → 该轮 `Tick()` 后 `idle`，且**不看目标点是否落定**
  - WS-10 暂停期间不产生新预警（paused provider + 窗口内有候选 → 保持 `idle`）
  - WS-11 解除暂停后，若窗口内仍有待投递触发 → 能重新 `warning`；若没有 → 保持 `idle`
  - 多信号覆盖：锁屏 / 会议 / 全屏 / 空闲 / 专注 / 日历 任一为 paused 都走同一路径
    （用 `pause.Provider` 的假实现注入，不必真锁屏）。
  - 若本票选择"把 `paused` 传进 `tickWarnings`"，补一条断言：`slog` 的撤销原因在暂停路径上是 `paused`
    而不是 `落定`——否则事故复盘时日志会指错方向。
- [x] 文档：`CONTEXT.md` 的「休息预警」已写明"进入智能免打扰时同样立即撤销"，本票落地后校对措辞；
  `app.go:1213-1220`（`trayStatusText` 三优先级注释）里"休息模式 Warning"那条补充"暂停时不会存在预警"。

## 验收标准

- `make test` / `make lint` 全绿，含上述 4+ 条用例。
- WS-T2 / WS-9 各做一次"注入错误值"验证：把撤销条件写成仅在 `best == nil` 时触发，用例必须变红。
- 真实场景走查不在本票（临时 HOME + MCP 环境成本高，集中到 005）；
  本票交的是"逻辑上暂停一来预警就没了"，判据是测试与日志。

## 边界

- 不改智能免打扰的判定信号、优先级与"暂停期顺延不补发"的既有语义（ADR 0004 / 0005）。
- 不改 `RestModeState` 枚举，不加"预警已过期"这类新状态。
- 不动宠物气泡优先级链。

## 实现记录（2026-10-22）

- 逻辑：`internal/reminder/engine.go` 的 `Tick` 里原来那句
  `if !paused && e.restMode != nil { tickWarnings }` 改成两分支——
  暂停时对状态机**只撤销不扫描**（`CancelWarning()` 成功才记一行日志，避免每秒刷屏），
  非暂停时照旧 `tickWarnings`。选这个形态而不是"把 paused 传进 tickWarnings"：
  暂停期间的语义是"整轮不判断"，传进去反而要在函数里再造一个早退分支，读的人要多绕一层。
  日志不重复记原因——同一轮上面已有「智能免打扰生效 reason=…」那行。
- 测试（`engine_warning_test.go`，用 `mockPauseProvider` 注入，不依赖真实锁屏）：
  `TestWarningClearedWhenPaused`（暂停发生在到点**之前**：那次触发永远不会被占用，
  所以这条只能靠"暂停即撤销"通过——正是 2026-10-22 实机挂住 23 分钟的形态）、
  `TestNoWarningRaisedWhilePaused`、`TestWarningRescannedAfterPauseResumes`
  （解锁后窗口内仍有待投递触发 → 重新预警且 `RemainingSec == 20`）。
- 注入证据：`TestWarningClearedWhenPaused` 在 001 完成后仍是红的
  （`--- FAIL: 进入暂停后预警应立即撤销，实际 warning（快照 … RemainingSec:20）`），
  加上本票的暂停分支才转绿——这条不是同义反复。
- UI 层未改：`Pet.vue` 优先级链保持原样，暂停气泡上位是服务端状态变 `idle` 的自然结果。
- 验证：`make test` / `make lint` 全绿。

