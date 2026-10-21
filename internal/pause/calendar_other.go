//go:build !darwin

package pause

// GetCalendarStatus 在非 macOS 平台返回未授权。
//
// Windows 的 Appointment API 是 UWP-only，桌面 Go 应用拿不到；Linux 不在支持范围。
// 上层 UI 通过 PlatformNeedsPermission=false 屏蔽相关设置项即可，不报错。
func GetCalendarStatus() CalendarStatus { return CalendarStatus{Granted: false} }

// RequestCalendarAccess 在非 macOS 平台恒返回 true（不需要授权、也没有对应能力）。
func RequestCalendarAccess() bool { return true }
