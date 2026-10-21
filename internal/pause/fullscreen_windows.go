//go:build windows

package pause

import (
	"unsafe"
)

var (
	procGetWindowRect     = user32.NewProc("GetWindowRect")
	procMonitorFromWindow = user32.NewProc("MonitorFromWindow")
	procGetMonitorInfo    = user32.NewProc("GetMonitorInfoW")
)

// rect 对应 Win RECT 结构。
type winRect struct {
	left   int32
	top    int32
	right  int32
	bottom int32
}

// monitorInfo 对应 Win MONITORINFO 结构（dwFlags 只用到 rcMonitor）。
type monitorInfo struct {
	cbSize    uint32
	rcMonitor winRect
	rcWork    winRect
	dwFlags   uint32
}

const (
	monitorDefaultToNull = 0 // MONITOR_DEFAULTTONULL
	monitorDefaultToPrim = 2 // MONITOR_DEFAULTTOPRIMARY
)

// GetFrontWindowInfo 返回当前前台窗口与所在显示器的几何。
//
// 【隐私边界】只读几何（GetWindowRect + GetMonitorInfo）；不调用任何截屏 API、
// 不读标题、不落盘、不上传。Windows 上这些几何 API 无需权限。
//
// 【rcMonitor vs rcWork】rcMonitor 是显示器完整区域，rcWork 是去掉任务栏后的可用区。
// 用户在真·全屏（F11 或媒体播放器全屏）时窗口覆盖 rcMonitor；
// 用 rcMonitor 判定，最大化（只到 rcWork）就不会被误判。
func GetFrontWindowInfo() FrontWindowInfo {
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return FrontWindowInfo{Permitted: false}
	}

	var wr winRect
	ok, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&wr)))
	if ok == 0 {
		return FrontWindowInfo{Permitted: false}
	}

	// 找窗口所在显示器；不在任何显示器上（离屏缓存窗口）时以主屏为基准。
	hMon, _, _ := procMonitorFromWindow.Call(hwnd, uintptr(monitorDefaultToPrim))
	if hMon == 0 {
		return FrontWindowInfo{Permitted: false}
	}
	var mi monitorInfo
	mi.cbSize = uint32(unsafe.Sizeof(mi))
	ok, _, _ = procGetMonitorInfo.Call(hMon, uintptr(unsafe.Pointer(&mi)))
	if ok == 0 {
		return FrontWindowInfo{Permitted: false}
	}

	info := FrontWindowInfo{
		Window: Rect{
			X:      int(wr.left),
			Y:      int(wr.top),
			Width:  int(wr.right - wr.left),
			Height: int(wr.bottom - wr.top),
		},
		Screen: Rect{
			X:      int(mi.rcMonitor.left),
			Y:      int(mi.rcMonitor.top),
			Width:  int(mi.rcMonitor.right - mi.rcMonitor.left),
			Height: int(mi.rcMonitor.bottom - mi.rcMonitor.top),
		},
		Permitted: true, // Windows 上读几何无需权限
	}
	return info
}

// ScreenCaptureGranted 在 Windows 上恒返回 true（读几何不需要权限）。
func ScreenCaptureGranted() bool { return true }

// RequestScreenCapture 在 Windows 上不需要弹窗；返回 true。
func RequestScreenCapture() bool { return true }
