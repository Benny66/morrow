# 006 — 会议/全屏建议与自动退出

**Blocked by**: 003

**状态**: 代码完成，待手工视觉验证（2026-10-21）

## 目标

会议应用或全屏持续 5 秒后，宠物气泡建议进入演示模式；用户接受后进入（source=suggested），触发条件消失 30 秒后自动退出；忽略后本次会话不再建议。

## 涉及层

- [ ] 数据层：
  - `internal/presentation/controller.go` 增加 suggestion 状态、触发原因、建议已展示/已忽略、autoExitAt。
  - 使用 001 的 `presentation_suggest_enabled` 设置。
- [ ] 逻辑层：
  - App 每秒把底层 pause state 喂给 controller（复用现有刷新循环）；建议只在 meeting / fullscreen 持续 ≥5 秒、无 pending、无休息模式时出现。
  - `SetPresentationMode(active=true, source=suggested)`；新增 `DismissPresentationSuggestion()`；条件消失 30 秒后自动退出，恢复则取消。
  - 手动来源不自动退出。
- [ ] UI 层：
  - `Pet.vue` 增加建议气泡（「在开会/共享屏幕？进入演示模式」+「进入」「不用了」）；优先级：休息 > 待处理 > 建议；宠物隐藏时不建议；不发系统通知。
  - `Settings.vue` 增加「演示模式建议」开关。
- [ ] 测试：
  - controller 状态机：建议一次、忽略、接受、自动退出、取消退出、手动不退出。
  - 手工：真实会议/全屏；无 pending / 有 pending 两种；设置关闭。
  - `make test`、`make lint`。

## 验收标准

- 会议/全屏 5 秒后出现建议（条件满足时）；忽略后本次会话不再出现；设置关闭后不出现。
- 接受后进入演示；条件消失 30 秒后自动退出；30 秒内恢复则继续。
- 手动进入不自动退出。
- 建议不发系统通知；不自动进入。
- `make test`、`make lint` 通过；覆盖 A-6、A-7、A-8。

## 实现记录（2026-10-21）

- `presentation.Controller` 增加 `Trigger`、`Observe`、`DismissSuggestion`、`SuggestionVisible`：触发持续 5 秒后建议；同一会话忽略/手动进出后不再建议；建议进入的演示在触发消失 30 秒后自动退出，触发恢复则取消；手动进入不自动退出。
- `app.go` 每秒用 Meeting/Fullscreen 探测器（尊重用户开关）观察触发；`GetPauseState.SuggestionVisible` 回传；新增 `DismissPresentationSuggestion` 绑定；自动退出复用暂停/宠物/托盘收尾。
- `Pet.vue` 增加建议气泡（进入 / 不用了），有待处理时让位；`Settings.vue` 增加「演示模式建议」开关与保存按钮。
- 新增测试：建议延迟/忽略/设置关闭/自动退出/取消退出/手动不退出/会话抑制；App 的建议回传、忽略入口与自动退出。
- 验证：`make test`、`make lint` 通过。
- 待办：真实会议/全屏下验证建议出现时机、接受/忽略、30 秒自动退出、无系统通知。
