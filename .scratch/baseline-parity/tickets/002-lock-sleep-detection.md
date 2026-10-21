# 002 — 锁屏 / 休眠检测

**Blocked by**: 001

## 目标

当系统锁屏或休眠时，应用立即进入暂停状态；唤醒/解锁后恢复调度。与 001 共用 `PauseReason` 枚举与 `Provider` 接口，新增 `Lock` 信号。

## 涉及层

- [ ] 数据层：`internal/config` 新增 `PauseLockEnabled` 字段
- [ ] 逻辑层：`internal/pause/lock_darwin.go`（监听 `NSWorkspace.willSleepNotification` 与 `NSWorkspace.screensDidSleepNotification`，以及 `CGSessionCopyCurrentDictionary` 的 `CGSSessionScreenIsLocked`）与 `lock_windows.go`（监听 `WM_WTSSESSION_CHANGE`）；注册到 `Provider`
- [ ] UI 层：复用 001 的宠物与菜单栏状态展示，原因文案切换为「锁屏」
- [ ] 测试：`internal/pause/lock_test.go` 模拟锁屏/唤醒事件；手动验收

## 验收标准

- 锁屏后立即暂停投递；唤醒后恢复
- 休眠后立即暂停投递；唤醒后恢复
- 宠物与菜单栏显示「锁屏」原因
- `make test` 与 `make lint` 通过
- 覆盖 US-2
