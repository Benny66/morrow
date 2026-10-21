//go:build !darwin && !windows

package pause

// GetFrontWindowInfo 在非目标平台返回空信息（Permitted=false 让 detector 静默降级）。
//
// 与 idle/lock/meeting 的 stub 同理：Linux 不在支持范围，但包要能编译。
func GetFrontWindowInfo() FrontWindowInfo { return FrontWindowInfo{Permitted: false} }

// ScreenCaptureGranted 在非目标平台恒返回 true（没有需要申请的权限）。
func ScreenCaptureGranted() bool { return true }

// RequestScreenCapture 在非目标平台恒返回 true（无需弹窗）。
func RequestScreenCapture() bool { return true }
