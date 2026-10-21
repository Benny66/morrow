# 004 — 全屏检测 + 屏幕录制权限

**Blocked by**: 001

## 目标

当 macOS 前台窗口铺满当前屏幕（按窗口几何判定）时，应用进入「全屏暂停」状态；Windows 同理。macOS 上若必须申请屏幕录制权限才能读取窗口几何，首次启用时向用户申请，说明「仅用于判断全屏，不采集屏幕内容」；用户拒绝后降级为空闲 + 会议识别，不反复弹窗。

## 涉及层

- [ ] 数据层：`internal/config` 新增 `PauseFullscreenEnabled` 与 `ScreenRecordingPermissionRequested` 字段
- [ ] 逻辑层：`internal/pause/fullscreen_darwin.go`（`CGWindowListCopyWindowInfo` 读取前台窗口几何，比较屏幕 bounds；若失败检查权限状态）与 `fullscreen_windows.go`（`GetForegroundWindow` + `GetWindowRect` + `MonitorFromWindow`）；`internal/dialog/permission.go`（新）封装权限申请与状态查询
- [ ] UI 层：`frontend/src/views/Settings.vue` 新增全屏检测开关与权限状态提示；复用 001 的宠物与菜单栏状态展示，原因文案切换为「全屏」
- [ ] 测试：`internal/pause/fullscreen_test.go` 验证几何比较逻辑；手动验收权限申请与降级

## 验收标准

- macOS 前台窗口铺满屏幕时暂停投递；退出全屏后恢复
- Windows 同理
- macOS 首次启用时弹出权限申请说明；拒绝后降级为空闲 + 会议识别，不反复弹窗
- 宠物与菜单栏显示「全屏」原因
- 不截图、不落盘、不上传窗口内容
- `make test` 与 `make lint` 通过
- 覆盖 US-4、US-8
