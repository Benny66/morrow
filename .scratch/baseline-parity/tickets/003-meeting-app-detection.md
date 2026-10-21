# 003 — 会议应用前台检测

**Blocked by**: 001

## 目标

当 Zoom / Teams / Webex / 腾讯会议 / 飞书 / 钉钉 / FaceTime 任一应用处于前台时，应用进入「会议暂停」状态。不读取会议内容，只识别前台应用 bundle ID。

## 涉及层

- [ ] 数据层：`internal/config` 新增 `PauseMeetingEnabled` 字段；`internal/pause/meeting.go` 维护内置会议应用 bundle ID 名单（单一真相）
- [ ] 逻辑层：`internal/pause/meeting_darwin.go`（`NSWorkspace.shared.frontmostApplication.bundleIdentifier`）与 `meeting_windows.go`（`GetForegroundWindow` + `GetWindowThreadProcessId` + `ProcessName`）；定期轮询（1 秒间隔），注册到 `Provider`
- [ ] UI 层：复用 001 的宠物与菜单栏状态展示，原因文案切换为「会议中」
- [ ] 测试：`internal/pause/meeting_test.go` 验证名单匹配与轮询逻辑；手动验收

## 验收标准

- 会议应用前台时暂停投递；切出后恢复
- 宠物与菜单栏显示「会议中」原因
- 不读取会议内容（音频/视频/聊天）
- 内置名单覆盖 7 个主流会议应用
- `make test` 与 `make lint` 通过
- 覆盖 US-3
