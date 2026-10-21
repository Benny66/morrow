//go:build !darwin && !windows

package pause

// GetFrontmostApp 在非目标平台返回空串（Linux 不在支持范围，见 spec）。
//
// 与 idle/lock 的 stub 同理：让包能在任何平台编译；会议检测在 Linux 上静默缺席。
func GetFrontmostApp() string { return "" }
