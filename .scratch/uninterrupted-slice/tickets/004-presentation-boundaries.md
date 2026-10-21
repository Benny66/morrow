# 004 — 演示模式边界

**Blocked by**: 003

**状态**: 代码完成，待手工视觉验证（2026-10-21）

## 目标

补齐演示模式的边界行为：进入时休息倒计时按 skip 结束、退出后不补弹旧待处理、一次性提醒记 missed、旧强提醒不重放。完成后可独立验证。

## 涉及层

- [ ] 数据层：
  - `internal/presentation/controller.go` 记录 `exitedAt`；`GetPauseState` 返回 `presentation_exited_at`。
- [ ] 逻辑层：
  - 进入演示时：休息模式 warning → `CancelWarning`；countdown → 走现有 `SkipRestMode` 路径并记录 CheckIn(skip)；不弹确认框。
  - 退出演示时：更新 `exitedAt`；不触发任何补发。
  - 一次性/每日单点在暂停期间记 missed（已有引擎语义，补测试锁定）。
- [ ] UI 层：
  - `frontend/src/views/Alert.vue` 显示规则：`!presentationActive && pending.fire_at > presentation_exited_at`（旧待处理不重放；新触发照常）。
  - 宠物/首页的待处理与已错过列表不变。
- [ ] 测试：
  - T-02 / T-05 / T-06；退出后新触发仍弹；旧待处理不弹。
  - `restmode` skip 记录测试；引擎 paused 一次性 missed 测试。
  - 手工：倒计时中进入演示、退出后有待处理、新提醒到点。
  - `make test`、`make lint`。

## 验收标准

- 演示中休息倒计时按 skip 结束，无确认框、无覆盖层；预警取消不记录。
- 退出演示后，演示前/期间产生的待处理不自动弹强提醒；新到点提醒正常弹。
- 一次性提醒在演示期间到点记 missed，首页可见，不补弹。
- 演示中旧强提醒不重放；`Alert.vue` 规则有手工验证。
- `make test`、`make lint` 通过；覆盖 A-3 的退出路径、A-9、A-10。

## 实现记录（2026-10-21）

- `app.go` 新增 `handleRestModeOnPresentationEnter`：预警 `CancelWarning`；倒计时走现有 `SkipRestMode`（写 CheckIn(skip)）并归零；不弹确认框。
- `Alert.vue` 增加 `presentationExitedAt` 并在 `refreshPending` 过滤 `fire_at <= presentation_exited_at` 的旧待处理；退出演示后只有新到点提醒会触发强提醒，旧条目仍在首页。
- 一次性/每日单点在暂停期间记 missed 的语义由现有引擎测试锁定，未改引擎。
- 新增 App 测试：进入演示跳过倒计时、取消预警。
- 验证：`make test`、`make lint` 通过。
- 待办：真实 GUI 下验证进入演示时休息覆盖层收起、退出后旧待处理不弹、新提醒正常弹。
