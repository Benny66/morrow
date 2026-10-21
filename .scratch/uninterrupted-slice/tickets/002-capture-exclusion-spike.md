# 002 — 捕获排除能力 spike

**Blocked by**: 无

**状态**: 已完成（2026-10-21）

## 目标

在投入 UI 前验证：Wails v3 的透明置顶宠物窗与全屏遮罩，能否在 macOS 与 Windows 上排除出屏幕捕获。产出实现路径、平台支持矩阵与降级结论。

## 结论

Wails v3 beta.23 已内置窗口内容保护，不需要自建 `windowguard`、cgo 或原生句柄包：

- `WebviewWindowOptions.ContentProtectionEnabled` 与 `WebviewWindow.SetContentProtection(bool)` 是公开 API。
- macOS：`setSharingType:NSWindowSharingNone`（`webview_window_darwin.go`）。
- Windows：`SetWindowDisplayAffinity(WDA_EXCLUDEFROMCAPTURE)`；失败时由 Wails 回退（`webview_window_windows.go`）。
- Linux / Android / iOS / server：no-op，必须如实降级。

对 005 的影响：

- 宠物与强提醒窗口在创建时设置 `ContentProtectionEnabled: true` 即可；主窗不设。
- 平台支持状态按平台判断（macOS / Windows 支持，其余降级），在 `GetPauseState` 暴露。
- 真实共享屏幕的可见性验证放到 005 与 009；本片不产生行为变更。

## 涉及层

- [x] 逻辑层：查阅 Wails v3 beta.23 源码与公开 API；不新建包。
- [x] UI 层：无。
- [x] 测试：源码核对 + 平台支持矩阵；真实共享屏幕验证留给 005 / 009。
- [x] 文档：`docs/adr/0006-presentation-mode-capture-exclusion.md` 已追加 spike 结论；005 范围已更新。

## 验收标准

- 明确 macOS / Windows / Linux 的支持情况与 API 路径。
- 结论落入 ADR 0006。
- 005 的实施方案更新为「Wails 内置 ContentProtection + 平台状态 + 手工验证」。
- 无行为变更；无需 `make test` 之外的额外验证。
