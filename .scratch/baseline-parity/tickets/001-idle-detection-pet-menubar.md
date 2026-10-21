# 001 — 空闲检测 + 宠物/菜单栏状态

**Blocked by**: 无

## 目标

当系统连续 5 分钟无键鼠输入时，应用进入「空闲暂停」状态：期间到点的提醒不投递、不生成待处理；宠物气泡显示「我先不打扰」；菜单栏图标与菜单项同步显示暂停原因。恢复输入后调度自动恢复。

## 涉及层

- [ ] 数据层：`internal/config` 新增 `PauseIdleEnabled` 与 `PauseIdleMinutes` 字段；`internal/pause/provider.go`（新）定义 `PauseReason` 枚举与 `Provider` 接口
- [ ] 逻辑层：`internal/pause/idle_darwin.go`（macOS 用 `CGEventSourceSecondsSinceLastEventType`）与 `idle_windows.go`（Windows 用 `GetLastInputInfo`）；`internal/reminder/engine.go` 在调度循环前查询 `Provider.State()`，为 `Idle` 时跳过投递
- [ ] UI 层：`frontend/src/views/Pet.vue` 新增 `paused` 状态与「我先不打扰」气泡；`internal/tray/tray.go` 动态更新菜单项显示暂停原因
- [ ] 测试：`internal/pause/idle_test.go` 模拟空闲时间；`internal/reminder/engine_pause_test.go` 验证暂停期间不投递；`frontend` 手动验收宠物与菜单栏状态

## 验收标准

- 连续 5 分钟无输入后，到点的提醒不投递、不进待处理
- 宠物气泡显示「我先不打扰」+ 原因（空闲）
- 菜单栏菜单项显示当前暂停原因
- 恢复输入后，调度立即恢复，下一条提醒按原间隔重新计时
- `make test` 与 `make lint` 通过
- 覆盖 US-1、US-5（部分）、US-15、US-16、US-17（部分）、US-26（部分）
