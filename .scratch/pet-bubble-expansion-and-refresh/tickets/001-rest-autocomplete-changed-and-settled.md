# 休息自动完成补发 changed 与已结算

**Blocked by**: 无

> 规格：B-1、C-1、C-2、C-3、C-4、D-3、D-4

## 目标

休息倒计时自动完成后，宠物与首页立刻同步；在别处提前完成时状态机不再卡在 `Done`。

## 涉及层

- [ ] 逻辑层（Go）
  - `app.go` 的 `tickRestMode`：自动完成的结果分三类——成功、已结算、真失败。
  - 新增未导出纯函数 `restCompletionSettled(err error) bool`：`apperr.Wrap(err).Code` 为 `validation` / `not_found` 时返回 true；`internal` 与其他返回 false；nil 不参与判断。
  - 成功或已结算：`Reset` → 广播 `restmode:completed` 与 `emitReminderChanged(reminder.ReasonCheckIn)`；已结算记 Info（`休息模式自动完成：该提醒已处理，视为已结算`），不再记 warn。
  - 真失败：保持 `Done` 不 `Reset`，保留现有 warn 与下一轮重试。
  - 两条广播调用保持在 `tickRestMode` 函数体内，便于护栏按函数体解析。
- [ ] 事件层
  - 不新增事件；复用 `reminder:changed` / `restmode:completed`。
- [ ] UI 层
  - 无新增 UI；`Pet.vue` / `Home.vue` / store / `Alert.vue` 的既有订阅自动生效。
- [ ] 测试
  - main 包单测 `app_test.go`：四种错误码分类（validation / not_found / internal / 其他）+ `apperr.Wrap` 包装后的形态。
  - 扩展 `internal/guard/wiring_test.go`：定位 `tickRestMode` 函数体，断言同时包含 `emitReminderChanged` 与 `restmode:completed`；解析到 0 个函数或 0 处广播即失败，并提示「写法可能已变更，请同步更新护栏解析规则」。

## 验收标准

- 0.5 档触发 stand 休息提醒，等倒计时自然结束：宠物气泡消失并播放庆祝；日志为 Info，不出现「该提醒已处理」循环。
- 倒计时期间从系统通知或首页提前完成：倒计时结束状态回 `Idle`；下一条 stand 仍能进入预警与倒计时。
- 模拟内部错误：状态保持 `Done`，错误消失后的下一轮 tick 完成结算，CheckIn 不丢。
- 删除 `emitReminderChanged` 或 `restmode:completed` 任一广播，`make test` 必须变红。
- `make test` 通过；无新增依赖、无新事件常量。
