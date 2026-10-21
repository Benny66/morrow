# 010 — P1：系统专注联动（macOS）

**Blocked by**: 001

## 目标

macOS 上通过公开的 Focus 状态接口（`INFocusStatusCenter`）读取系统专注是否开启；开启时自动暂停提醒，关闭后恢复。Windows 上若存在等价公开能力则实现；若不存在，P1 不降级也不报错。

## 涉及层

- [ ] 数据层：`internal/config` 新增 `PauseFocusEnabled` 字段（默认 false）
- [ ] 逻辑层：`internal/pause/focus_darwin.go`（通过 cgo 调用 `INFocusStatusCenter.default.focusStatus.isFocusEnabled`，需用户授权）与 `focus_windows.go`（若 `SHQueryUserNotificationState` 可用则调用，否则返回 `unknown`）；注册到 `Provider`，新增 `PauseReason.Focus`
- [ ] UI 层：`frontend/src/views/Settings.vue` 新增「系统专注联动」开关与授权状态提示；复用 001 的宠物与菜单栏状态展示，原因文案切换为「专注模式」
- [ ] 测试：`internal/pause/focus_test.go` 验证授权流程与状态读取；手动验收

## 验收标准

- macOS 系统专注开启时暂停投递；关闭后恢复
- 首次开启时弹出授权说明，明确「只读忙闲，不读内容」
- 用户拒绝授权后，静默降级，不弹二次请求
- Windows 上若 `SHQueryUserNotificationState` 不可用，不报错，保持 P0 行为
- 宠物与菜单栏显示「专注模式」原因
- `make test` 与 `make lint` 通过
- 覆盖 US-20、US-21
