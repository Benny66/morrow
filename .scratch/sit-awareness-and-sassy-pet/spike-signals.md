# 001 信号 spike 结论（2026-10-22）

- 机器：macOS 25.4.0 arm64；Go 1.26.2；产品构建目标 macOS / Windows。
- 探针：临时 `internal/pause/probe_spike_test.go`（已删除），直接调用现有 `pause` 包。
- 目的：确认前台应用、锁屏、5 秒采样与展示名能否支撑 002/004/006；不修改产品行为。

## 1. macOS 前台应用身份

实测（探针运行期间前台为 Chrome）：

```text
SPIKE frontmost="com.google.Chrome"
SPIKE idle_supported=true idle=7.328585s
SPIKE lock_initial=false since=0001-01-01 00:00:00 +0000 UTC
SPIKE duration=1m0.005s cpu=22.725ms seen=map[com.google.Chrome:12]
```

结论：

- `pause.GetFrontmostApp()` 在 macOS 返回稳定的 bundle ID；无需屏幕录制、辅助功能或摄像头权限。
- 现有 `NSWorkspace.frontmostApplication.bundleIdentifier` 只返回标识，不返回展示名；**不读 `localizedName` 也可以**，用内置展示名映射，未知回退显示 bundle ID。
- Morrow 自身需要在采样器里按 bundle ID 排除（root 组合根可注入常量，不放进 `internal/sit` 的平台细节）。
- 应用切换在 5 秒粒度下的误差取决于切换时刻；探针期间没有切换，无法量化。006 用真实应用切换补测；5 秒是本功能可接受的采样粒度。

## 2. Windows 前台应用身份（代码级，待实机验证）

- 现有 `pause.GetFrontmostApp()` 返回 `GetForegroundWindow` 所属进程的**小写进程名（不含 .exe）**，不返回窗口标题，符合隐私边界。
- **UWP / Store 应用风险**：前台窗口很可能属于 `ApplicationFrameHost.exe`，当前 API 无法仅凭进程名区分具体 UWP 应用。不要在 006 承诺「每个应用都能识别」；未知/宿主统一归「其他」，页面允许用户改类别。
- 若要提升 UWP 精度，需要额外的 Windows 实机 spike（`GetApplicationUserModelId` / 包信息 / UI Automation），并在不读窗口标题的前提下验证权限与稳定性。本轮不阻塞主流程。
- Windows 交叉编译 `GOOS=windows GOARCH=amd64 go test -c ./internal/pause` 通过；运行时行为仍需真实 Windows 机器验证。

## 3. 锁屏信号与时长

### macOS

- 现有 `LockDetector` 通过 `CGSessionCopyCurrentDictionary` 的 `CGSSessionScreenIsLocked` 轮询（1 秒）+ IOKit 休眠兜底；`Since()` 在首次进入锁定/休眠时记录，解锁后清零。
- 探针确认 `NewBundle(Config{LockEnabled:true})` 能启动 watcher，`Bundle.Lock` 可读，初始 `IsLocked=false`、`Since` 为零；没有崩溃、没有权限提示。
- **未实测真实锁屏/解锁转换**（自动锁屏会打断当前会话）。004 的验收必须在 macOS 上手工做一次：锁屏 1 分钟（只清零不抵扣）与锁屏 6 分钟且已坐 31 分钟（抵扣一次）。

### Windows（重要缺口）

- 现有 `lock_windows.go` 的 `SHQueryUserNotificationState` 只把 `qunsSuspended`（即将睡眠）与 `qunsAwayMode`（离开模式）当作锁定；**不覆盖 Win+L 屏幕锁定**。代码注释已明确「屏幕锁定通过 GetForegroundWindow == NULL 或桌面切换来判（更复杂）」。
- 结论：按当前实现，Windows 的「锁屏 6 分钟自动抵扣」在普通 Win+L 场景下不会触发；只能覆盖休眠/离开模式。
- 002 的处理：
  1. 先把 `LockWatcherEnabled` 与 `LockEnabled` 解耦，保证用户关闭「锁屏暂停」时久坐仍能读到锁屏时长。
  2. 在 `lock_windows.go` 增加 Win+L 检测：推荐轮询 `OpenInputDesktop`（锁屏时返回 NULL，解锁后恢复；只读桌面句柄，不读内容）或 `WTSRegisterSessionNotification` 消息窗口。两者都需要真实 Windows 验证；优先选实现更小、无需窗口消息循环的方案。
  3. 若实机验证失败：004/设置页明确显示「Windows 锁屏记账不可用」，退化为空闲休息，不静默假装抵扣。
- 该项是 004 的阻塞风险；002 完成后需在 Windows 实机上做锁屏 1 分钟 / 6 分钟两档验收。

## 4. 5 秒采样开销

- 60 秒内每 5 秒读一次前台应用 + 锁屏状态：CPU 时间 22.7ms，约占单核 0.038%；没有明显能耗压力。
- 探针没有写数据库；006 的真实采样必须按「应用切换才落会话」实现，不允许每 5 秒写库。
- 仍建议 006 在真实 macOS/Windows 上各跑 10–30 分钟，观察 CPU、电量与数据库增长；**本次 60 秒样本只能说明没有明显异常，不能证明长跑无问题**（单次样本不作为长期结论）。

## 5. 展示名与隐私

- macOS：bundle ID → 内置中文名；未知显示原始 bundle ID。
- Windows：进程名 → 内置中文名；未知显示进程名；UWP 可能统一显示「未知应用」。
- 不读窗口标题、屏幕内容、键鼠输入内容；不用摄像头/麦克风；不上传；日志不记录应用名或原始标识。
- 展示名映射只存在本地；用户可在久坐页纠正类别，纠正不改变原始应用标识。

## 6. 对后续 ticket 的输入

- **002**：新增 `LockWatcherEnabled`；按本笔记补齐 Windows Win+L 检测或明确降级；新增锁屏读取的测试/文档。应用身份模型用 `platform + app_id`；UWP 统一归「其他」。
- **004**：macOS 锁屏记账可做；Windows 必须等 Win+L 实机验证，失败时降级并在设置页可见。
- **006**：UWP 不承诺逐应用识别；未知归「其他」；5 秒采样 + 会话切分 + 60 秒心跳；真实长跑与切换精度补测。
- **007**：类别纠正对 UWP「未知」同样可用，但无法把多个 UWP 应用区分开；页面文案不承诺。

## 7. 人工验证清单（后续 ticket 执行）

- macOS：锁屏 1 分钟 / 6 分钟；解锁后看 `Since`、抵扣次数与首页进度。
- Windows：Win+L 锁屏 1 分钟 / 6 分钟；确认 `OpenInputDesktop`/WTS 方案；UWP 应用前台时看类别与页面展示。
- 两个平台：连续 30 分钟 5 秒采样，记录 CPU、电量、数据库行数；应用切换 20 次看会话切分。
