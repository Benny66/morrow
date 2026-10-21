//go:build windows

package pause

import (
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

var (
	procGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	// kernel32 已在 idle_windows.go 声明；这里复用它。
	procOpenProcess               = kernel32.NewProc("OpenProcess")
	procQueryFullProcessImageName = kernel32.NewProc("QueryFullProcessImageNameW")
	procCloseHandle               = kernel32.NewProc("CloseHandle")
)

const (
	// processQueryLimitedInformation 是 QueryFullProcessImageNameW 需要的最小权限。
	// 【为什么不用 PROCESS_QUERY_INFORMATION】前者在受限环境（如企业 EDR/受管终端）
	// 也能拿到路径；后者会因权限不足失败，让会议检测静默失效。
	processQueryLimitedInformation = 0x1000
)

// GetFrontmostApp 返回当前前台进程的进程名（小写、不含 .exe）。
//
// 【为什么统一叫 GetFrontmostApp】见 meeting_darwin.go 同名方法的说明。
//
// 【为什么不返回完整路径】名单匹配只需文件名；返回完整路径会让用户目录泄露到
// 日志（例如 "C:\\Users\\zhangsan\\...\\zoom.exe"），且大小写不敏感匹配更稳。
func GetFrontmostApp() string {
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return ""
	}
	var pid uint32
	procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid == 0 {
		return ""
	}
	hProc, _, _ := procOpenProcess.Call(
		uintptr(processQueryLimitedInformation),
		uintptr(0),
		uintptr(pid),
	)
	if hProc == 0 {
		return ""
	}
	defer procCloseHandle.Call(hProc)

	buf := make([]uint16, 1024)
	size := uint32(len(buf))
	ok, _, _ := procQueryFullProcessImageName.Call(
		hProc,
		uintptr(0),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
	)
	if ok == 0 {
		return ""
	}
	base := filepath.Base(syscall.UTF16ToString(buf[:size]))
	return strings.TrimSuffix(strings.ToLower(base), ".exe")
}
