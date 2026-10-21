# 007 — 演示模式快捷键与设置入口

**Blocked by**: 003

**状态**: 代码完成，待手工验证快捷键（2026-10-21）

## 目标

用户可配置全局快捷键切换演示模式；注册失败不阻断启动，并在设置页如实显示。

## 涉及层

- [ ] 数据层：
  - `internal/config.Config` 增加 `ShortcutPresentation`（json `shortcut_presentation`）；`Load` / `Save`。
  - 增加只读注册状态字段（如 `PresentationShortcutRegistered`），供设置页回显。
- [ ] 逻辑层：
  - `app.go` 复用现有全局快捷键注册/注销模式；启动时注册，设置保存时重注册；失败只记日志、不阻断。
  - 新增 `GetPresentationShortcut` / `SetPresentationShortcut` 绑定；返回注册失败信息。
- [ ] UI 层：
  - `Settings.vue` 在「不被打断」区域增加快捷键输入与状态提示（已启用 / 未启用 / 注册失败）。
- [ ] 测试：
  - 设置往返；清空即禁用；冲突失败路径（可注入 fake 注册器或手工）。
  - 手工：在别的应用前台按快捷键，演示模式切换。
  - `make test`、`make lint`。

## 验收标准

- 快捷键可在任意前台应用切换演示模式；清空后禁用。
- 注册失败不回滚启动、不影响其他功能；设置页显示失败原因。
- 不影响现有「唤起主窗」快捷键。
- `make test`、`make lint` 通过；覆盖 A-14（快捷键部分）、A-15。

## 实现记录（2026-10-21）

- `config.Config` 新增 `shortcut_presentation`；`config.PresentationShortcut` DTO 分开回传「配置值」与「当前是否已注册」。
- 把快捷键注册抽成 `registerShortcut` 唯一实现，`registerToggleShortcut` 与 `registerPresentationShortcut` 共用；启动时注册，失败只记日志、不阻断。
- 新增 `SetPresentationShortcut` / `GetPresentationShortcut` 绑定；设置页「不被打断」区域增加演示快捷键输入、保存与「已生效/已配置但未生效」状态。
- 新增测试：快捷键配置落盘往返；`GetPresentationShortcut` 在注册失败时仍回显配置值。
- 验证：`make test`、`make lint` 通过。
- 待办：真实系统下验证快捷键切换、与唤起主窗快捷键冲突时的失败提示。
