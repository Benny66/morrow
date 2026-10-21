//go:build windows

package pause

// GetFocusStatus 返回 Windows 上等价的「专注」状态。
//
// Windows 10+ 有 SHQueryUserNotificationState 能返回 `QueryStatusBusyAppRunning`
// 与 `QueryStatusDndEnabled` 等状态，语义与 macOS 的 Focus 近似。这里刻意保持
// 「不实现」——原因见 ADR 0005：P1 允许只做 macOS，Windows 保持 P0 行为。
// 返回 Granted=false 让 FocusDetector 静默缺席；上层在 Settings.vue 里判
// PlatformNeedsPermission=false 就不显示相关 UI。
func GetFocusStatus() FocusStatus {
	return FocusStatus{Granted: false, Enabled: false}
}

// RequestFocusAccess 在 Windows 上恒返回 true（没有需要申请的权限）。
func RequestFocusAccess() bool { return true }
