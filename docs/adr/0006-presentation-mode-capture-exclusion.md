# 演示模式用前台信号触发，并以窗口捕获排除兜底

A+B 切片把「会议/共享屏幕时不打扰、不被看见」作为 P0。演示模式只依据前台应用身份、全屏几何与用户主动操作触发，不读取屏幕内容、窗口标题，不检测摄像头或麦克风；同时把宠物与遮罩窗口在系统允许时排除出屏幕捕获（macOS `NSWindow.sharingType = .none`；Windows `SetWindowDisplayAffinity(WDA_EXCLUDEFROMCAPTURE)`），作为检测失败时的安全网。选择「手动/建议进入 + 捕获排除」而不是自动分析屏幕内容，是因为后者会破坏 ADR 0004 的本地与无采集承诺，且共享屏幕本身没有可靠的跨平台公开信号；捕获排除不读取内容，能兜住漏判。退出演示模式后按原间隔恢复，不补发。

## 捕获排除的实现路径（spike 结论，2026-10-21）

Wails v3 beta.23 已内置窗口内容保护，不需要自建 windowguard / cgo / 原生句柄包：

- `WebviewWindowOptions.ContentProtectionEnabled` 与 `WebviewWindow.SetContentProtection(bool)` 是公开 API。
- macOS：`setSharingType:NSWindowSharingNone`（`webview_window_darwin.go`）。
- Windows：`SetWindowDisplayAffinity(WDA_EXCLUDEFROMCAPTURE)`；失败时由 Wails 回退（`webview_window_windows.go`）。
- Linux / Android / iOS / server：no-op，必须在 UI 中如实降级。

落地方式：宠物与强提醒窗口在创建时设置 `ContentProtectionEnabled: true`；平台支持状态用构建标签判断。真实共享屏幕验证放到 005 与 009。

## 备选方案

- **用屏幕录制/像素分析判断是否在共享**：识别率更高，但需要读取屏幕内容，直接违反 ADR 0004，放弃。
- **用摄像头/麦克风判断会议**：误判与隐私成本高（LookAway 路线），放弃。
- **只做手动暂停，不做捕获排除**：实现最简单，但漏判时桌宠仍会出现在共享画面里，不能兑现「不被看见」。
- **只做捕获排除，不暂停提醒**：宠物看不见了，但强提醒仍会弹给用户自己，开会时依旧被打断。

## 后果

- 演示模式需要平台级捕获排除能力；不支持时必须在 UI 中如实降级，不能静默失败。
- 自动进入演示模式默认关闭，只做「建议进入」，避免误判打断用户。
- 后续若要用屏幕内容或像素分析提升识别率，必须新开 ADR 重新评估隐私承诺。
