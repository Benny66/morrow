//go:build !darwin

package pause

// RequiresScreenCapture 在非 macOS 平台返回 false（读几何不需要权限）。
func RequiresScreenCapture() bool { return false }

// FocusSupported 在非 macOS 平台返回 false（Windows/Linux 没有等价的公开能力）。
func FocusSupported() bool { return false }

// CalendarSupported 在非 macOS 平台返回 false。
func CalendarSupported() bool { return false }
