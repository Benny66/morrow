# 005 — 捕获排除接入

**Blocked by**: 003

**状态**: 代码完成，待真实共享屏幕验证（2026-10-21）

> 002 已完成：Wails v3 内置 `ContentProtectionEnabled` / `SetContentProtection`，无需自建 `windowguard` 或平台 cgo 包。

## 目标

演示模式（以及日常运行）下，桌宠、宠物气泡与强提醒窗口不出现在屏幕共享/录屏画面里；平台不支持时明确降级。

## 涉及层

- [ ] 数据层：
  - `pet_window.go` 的 `petWindowOptions` 与 `alert_window.go` 的 `alertWindowOptions` 设置 `ContentProtectionEnabled: true`；主窗不设。
  - `app.go` 按平台判断捕获排除支持（macOS / Windows 支持，其余 false），`GetPauseState` 填充 `capture_exclusion_supported`。
- [ ] 逻辑层：
  - 只排除宠物与强提醒窗口，不排除主窗。
  - 不支持时：演示模式仍暂停/隐藏；托盘状态显示「演示中（捕获排除不可用）」或在设置页显示；不静默失败。
- [ ] UI 层：
  - `Settings.vue` 只读状态行「捕获排除：已支持 / 当前系统不支持」。
  - 进入演示模式时若不支持，给出一次可见提示（菜单栏状态或设置页；不发系统通知，避免讽刺）。
- [ ] 测试：
  - 编译验证 + 在支持平台上用真实共享屏幕/录屏确认观看看不到宠物/气泡/遮罩；主窗仍可见。
  - 不支持平台：演示仍可用；设置/菜单明确显示不支持。
  - `make test`、`make lint`。

## 验收标准

- 支持平台上，共享屏幕/录屏看不到宠物、气泡、强提醒；用户自己仍能看到。
- 主窗不被排除。
- 不支持平台：演示仍可用；设置/菜单明确显示不支持；无静默失败。
- 不读取屏幕内容；不新增权限（全屏判定已有的屏幕录制权限不因本切片扩大用途）。
- `make test`、`make lint` 通过；覆盖 A-5、A-12（捕获维度）。

## 实现记录（2026-10-21）

- 复用 Wails 内置能力：`pet_window.go` 的 `petWindowOptions` 与 `alert_window.go` 的 `alertWindowOptions` 设置 `ContentProtectionEnabled: true`；主窗不设。
- `capture_protection.go` 新增 `captureProtectionSupported()`（macOS/Windows true，其余 false）；`GetPauseState.CaptureExclusionSupported` 回传。
- 设置页「不被打断」区域增加只读的「捕获排除」状态；托盘在演示中且平台不支持时显示「（捕获排除不可用）」。
- 新增测试：宠物/遮罩建窗参数均启用内容保护；`GetPauseState` 的支持状态与平台一致。
- 验证：`make test`、`make lint` 通过。
- 待办：在 macOS / Windows 上真实共享屏幕，确认观看看不到桌宠/气泡/遮罩，用户自己仍可见；Linux 降级文案需在对应平台确认。
