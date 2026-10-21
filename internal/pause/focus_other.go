//go:build !darwin && !windows

package pause

// GetFocusStatus 在非目标平台返回未授权（Linux 不在支持范围）。
func GetFocusStatus() FocusStatus { return FocusStatus{Granted: false} }

// RequestFocusAccess 在非目标平台恒返回 true（不需要授权、也没有对应能力）。
func RequestFocusAccess() bool { return true }
