# 001 — 信号 spike：前台应用、锁屏与采样边界

**Blocked by**: 无

**状态**: 已完成（2026-10-22；Windows 实机验证待 004）

> 规格：`docs/specs/2026-10-22-sit-awareness-and-sassy-pet.md` 的 7.7、7.9、E-1、E-2、E-10、B-3、B-6。
> 这是风险前置，不是用户可见切片；不改产品行为。

## 目标

在写应用画像与锁屏抵扣之前，用最小探针确认四个信号的可用性，并把结论写成一份决策笔记，供 002 / 004 / 006 直接落地。任何一项不可用，都要在笔记里给出降级方案，不允许实现阶段才发现。

## 要回答的问题

1. **macOS 前台应用身份**
   - `NSWorkspace.frontmostApplication.bundleIdentifier` 在未签名 / 已签名的开发态都可用吗？是否需要任何权限？
   - `localizedName` 能否作为展示名？如果不用 `localizedName`，内置展示名映射的覆盖率如何？
   - Morrow 自身（bundle ID）如何排除？

2. **Windows 前台应用身份**
   - 现有 `GetForegroundWindow` + `GetWindowThreadProcessId` + `QueryFullProcessImageNameW` 在普通 Win32 应用上是否稳定？
   - UWP / Store 应用是否都落到 `ApplicationFrameHost`？如果是，能否在不读窗口标题的前提下拿到真实应用标识（例如 `GetApplicationUserModelId`、包信息）？拿不到时是否接受统一归「其他」？
   - 进程名大小写、`.exe` 后缀、受限终端（EDR）权限是否影响读取？

3. **锁屏信号与时长**
   - macOS：现有 `pause.LockDetector` 的 `Since()` 是否能覆盖锁屏与休眠？解锁后 `Since` 是否清零？
   - Windows：现有 `lock_windows.go` 的 `SHQueryUserNotificationState` 只覆盖休眠 / 离开模式；**Win+L 是否完全检测不到？**
   - 若 Win+L 检测不到，评估可选方案：
     - `WTSRegisterSessionNotification` + message-only window；
     - 轮询 `OpenInputDesktop` / `GetForegroundWindow == NULL`；
     - 接受只在休眠/离开模式记账，Win+L 只按空闲休息处理。
   - 记录各方案的额外窗口/消息循环成本、权限与误判风险；给出推荐。

4. **5 秒采样与展示名**
   - 每 5 秒读一次前台应用，连续运行 10–30 分钟的空闲 CPU / 能耗增量是多少？
   - 应用切换在 5 秒粒度下的会话切分误差是否可接受？
   - 用内置展示名映射时，常见应用（VS Code / Figma / Chrome / 飞书 / Zoom / 微信 / 影音）的覆盖率是多少？未知应用如何展示？

5. **平台与隐私**
   - 以上读取是否都不需要屏幕录制、摄像头、麦克风、辅助功能等新权限？
   - 是否有任何读取会碰到窗口标题或屏幕内容？必须明确没有。

## 涉及层

- [ ] 研究 / 探针：只写一次性脚本或临时调试代码，不进入产品路径。
- [ ] 结论笔记：`.scratch/sit-awareness-and-sassy-pet/spike-signals.md`
  - 每个问题：结论、证据（命令 / 日志 / 截图）、置信度、降级方案。
  - 对 002 的输入：`app_usage_sessions` 的 `platform` / `app_id` 形态、展示名策略。
  - 对 004 的输入：锁屏时长能否可靠读取；Windows 是否需要 WTS 实现；不能时的降级文案。
  - 对 006 的输入：UWP 是否统一归「其他」；类别映射的维护边界。
- [ ] 测试：不新增产品测试；探针代码在提交前删除或留在 `.scratch` 笔记里。

## 验收标准

- 五个问题都有明确结论，不用「应该可以」这类推断。
- 明确写出：Windows Win+L 是否可检测；若不可，004 的锁屏抵扣在 Windows 上如何降级。
- 明确写出：UWP 应用是否可区分；若不可，006 是否接受统一归「其他」。
- 明确写出：5 秒采样是否有可感知的性能/能耗代价；若有，给出替代频率。
- 结论笔记被 002 / 004 / 006 引用；实施阶段不再重复验证。
- 不改变现有产品行为；`make test` / `make lint` 保持通过。

## 实现记录（2026-10-22）

- 结论笔记：`.scratch/sit-awareness-and-sassy-pet/spike-signals.md`。
- macOS 实测：`GetFrontmostApp()` 返回 `com.google.Chrome`；空闲读取可用；锁屏 watcher 启动正常（未实测真实锁屏转换）；60 秒 5 秒轮询 CPU 22.7ms。
- Windows：交叉编译通过；代码级确认 `GetFrontmostApp` 返回进程名、UWP 可能落到 `ApplicationFrameHost`、现有 `lock_windows.go` 不覆盖 Win+L。002 需在 Windows 实机上补 `OpenInputDesktop` 或 WTS 方案；004 的锁屏抵扣在 Windows 上依赖该验证，失败则降级并可见。
- 临时探针 `internal/pause/probe_spike_test.go` 已删除；未改产品行为。
