//go:build darwin

package pause

// RequiresScreenCapture 返回当前平台是否需要屏幕录制权限才能读窗口几何。
//
// 【为什么单独一个 helper 而不是让 UI 判 OS】UI 需要显示「去系统设置开启」按钮
// 的分支只和「这个平台会不会拒我」有关；Windows 上读几何永远成功，弹这个按钮
// 只是噪音。这个 helper 让 UI 层写「如果 pause.RequiresScreenCapture() 才显示」。
func RequiresScreenCapture() bool { return true }

// FocusSupported 报告当前平台是否具备系统专注读取能力。
func FocusSupported() bool { return true }

// CalendarSupported 报告当前平台是否具备日历忙闲读取能力。
func CalendarSupported() bool { return true }
