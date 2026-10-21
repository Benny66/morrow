# 011 — P1：日历忙闲感知（macOS）

**Blocked by**: 001

## 目标

macOS 上通过系统日历权限（`EventKit`）读取「当前是否忙」的布尔状态；忙碌时暂停，空闲后恢复。不读取事件标题、参与人、备注、地点；不进入 AI 提示词；不落盘。Windows 上若存在等价公开能力则实现；若不存在，P1 不降级也不报错。

## 涉及层

- [ ] 数据层：`internal/config` 新增 `PauseCalendarEnabled` 字段（默认 false）
- [ ] 逻辑层：`internal/pause/calendar_darwin.go`（通过 cgo 调用 `EKEventStore.requestAccessToEvents`，查询当前日历事件的 `busyStatus`）与 `calendar_windows.go`（若 `Windows.ApplicationModel.Appointments` 可用则调用，否则返回 `unknown`）；注册到 `Provider`，新增 `PauseReason.Calendar`
- [ ] UI 层：`frontend/src/views/Settings.vue` 新增「日历忙闲感知」开关与授权状态提示；复用 001 的宠物与菜单栏状态展示，原因文案切换为「日历忙碌」
- [ ] 测试：`internal/pause/calendar_test.go` 验证授权流程与状态读取；手动验收

## 验收标准

- macOS 日历忙碌时暂停投递；空闲后恢复
- 不读取事件标题、参与人、备注、地点
- 首次开启时弹出授权说明，明确「只读忙闲，不读内容」
- 用户拒绝授权后，静默降级，不弹二次请求
- Windows 上若 `Windows.ApplicationModel.Appointments` 不可用，不报错，保持 P0 行为
- 宠物与菜单栏显示「日历忙碌」原因
- `make test` 与 `make lint` 通过
- 覆盖 US-22、US-23
