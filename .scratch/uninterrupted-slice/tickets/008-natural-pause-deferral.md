# 008 — 自然停顿顺延

**Blocked by**: 001

**状态**: 代码完成，待手工输入场景验证（2026-10-21）

## 目标

用户在打字/拖拽时，非一次性提醒不再立刻弹全屏；最多顺延 60 秒，停手后投递；到上限改为安静投递（通知 + 宠物 + 待处理，不弹全屏、不进休息模式）。

## 涉及层

- [ ] 数据层：
  - 使用 001 的 `natural_pause_enabled` / `natural_pause_max_seconds`；`internal/reminder.Settings` 增加同名字段；`app.currentReminderSettings` 映射。
  - 前端绑定自动生成。
- [ ] 逻辑层：
  - `internal/reminder/engine.go`：新增 `ActivityProvider`（`IdleDuration() time.Duration`），注入 `pause.GetIdleDuration`；纯函数 `ShouldDefer(idle, enabled, max)`。
  - 顺延表（内存，key = reminderID+fireAt 或 fireID）；`tickGrid` / `tickSnoozes` 在 claim/dispatch 前判定；到上限带 `quietDelivery=true`；暂停时清空顺延表（覆盖演示模式）。
  - `dispatch` 增加独立参数（不要复用现有 quiet-hours 的 `quiet`）；安静投递跳过休息模式，仍发通知、仍 emit fired。
  - 不写数据库、不新增迁移；重启后按正常投递。
- [ ] UI 层：
  - `Pet.vue` 增加「等你停下」气泡（通过 `GetDeferralState` 或 `reminder:changed`，不新增事件域）；优先级低于休息/待处理。
  - `Settings.vue` 增加「自然停顿顺延」下拉：关闭 / 30 秒 / 60 秒。
  - `Alert.vue` 对 `quiet_delivery=true` 的触发不显示强提醒；宠物/首页仍显示待处理。
- [ ] 测试：
  - 引擎 fake clock / activity / store：T-07、T-08、T-09、T-10、T-11、T-14。
  - 一次性不顺延；每日单点/推迟可顺延；不重复、不积压；打卡语义不变。
  - 平台不支持时设置页显示不可用、提醒正常投递。
  - 手工打字验证。
  - `make test`、`make lint`。

## 验收标准

- 打字中到点不弹全屏、不发通知；停手后下一 tick 投递。
- 连续输入 60 秒：安静投递（通知 + 宠物 + 待处理），无全屏、无休息模式。
- 一次性提醒不顺延；每日单点/推迟可顺延。
- 顺延不造成重复/积压；打卡语义不变；暂停优先；重启不保留顺延。
- 设置 0 秒关闭；30/60 秒生效；非法值回落。
- `make test`、`make lint` 通过；覆盖 B-1..B-13。

> 安静投递的强提醒抑制通过 service 内存标记 + `PendingFire.quiet_delivery` 实现；不新增数据库迁移。应用重启后内存标记丢失，旧安静投递会按普通待处理展示（已知取舍，若实测不可接受再开票评估持久化）。

## 实现记录（2026-10-21）

- `internal/reminder` 新增 `ActivityProvider`、顺延状态（按提醒项 / 按 fireID，内存）、`DeferralState`；`tickGrid` / `tickSnoozes` 在 claim/dispatch 前裁决正常 / 继续顺延 / 安静投递；上限存进顺延记录，中途改设置不影响已开始的一轮；暂停时清空顺延。
- `dispatch` 增加独立的 `quietDelivery` 参数：安静投递仍发通知、仍写待处理，但跳过休息模式；`FiredPayload.QuietDelivery` 回传。
- 安静投递的强提醒抑制改为更可靠的方案：`Store.MarkQuietFire` + `service.PendingFire.quiet_delivery`（内存标记，不迁移），Alert 窗口即使错过 fired 事件也不会误弹；Go 侧 `alertShowHook` 同时跳过 quiet。
- App 注入 `systemActivity`（`pause.IdleSupported()` 区分平台支持），`GetDeferralState` 回传 active/since/supported；设置页增加关闭/30/60 选择与平台降级提示；Pet 增加「等你停下」气泡。
- 新增引擎测试：输入中顺延、停手投递、上限安静投递、正常投递进休息模式、一次性例外、关闭/平台不支持、暂停优先、推迟补发、提醒移除后清理。
- 验证：`make test`、`make lint` 通过；绑定已重新生成。
- 待办：真实打字场景验证顺延与停手投递、60 秒安静投递不弹全屏、一次性提醒不受影响、推迟补发同样顺延。
