//go:build windows

package pause

import (
	"syscall"
	"time"
	"unsafe"
)

var (
	user32             = syscall.NewLazyDLL("user32.dll")
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	procGetLastInput   = user32.NewProc("GetLastInputInfo")
	procGetTickCount64 = kernel32.NewProc("GetTickCount64")
)

// lastInputInfo 是 Windows LASTINPUTINFO 结构体（见 winuser.h）。
type lastInputInfo struct {
	cbSize uint32
	dwTime uint32
}

// getTickCount64 返回系统启动以来的毫秒数（Windows 上的稳定单调时钟）。
//
// 【为什么用 GetTickCount64 而不是 GetTickCount】后者是 32 位，49.7 天后归零，
// 归零瞬间 idle 计算会得到一个巨大的负数（无符号回绕后是天文数字），
// 让「用户明明在动键盘」误判为「已经空闲了 4 亿年」。
// 代价是 Win7 以下不支持——那部分用户量对健康提醒这个定位可以忽略。
func getTickCount64() uint64 {
	r, _, _ := procGetTickCount64.Call()
	return uint64(r)
}

// GetIdleDuration 返回 Windows 系统连续空闲时长。
//
// GetLastInputInfo 的 dwTime 与 GetTickCount64 同源（都是系统启动以来的毫秒数），
// 二者相减即为空闲时长。API 不需要任何权限，是系统公开的只读接口。
func GetIdleDuration() time.Duration {
	var lii lastInputInfo
	lii.cbSize = uint32(unsafe.Sizeof(lii))

	ret, _, _ := procGetLastInput.Call(uintptr(unsafe.Pointer(&lii)))
	if ret == 0 {
		return 0
	}
	// lii.dwTime 是 32 位；GetTickCount64 是 64 位。低 32 位对齐。
	// 49.7 天不重启会溢出，此时空闲计算会失真——但 Windows 上「连续 49.7 天
	// 不重启的机器」是服务器场景，桌面健康提醒本就不面向它。
	now := getTickCount64()
	last := uint64(lii.dwTime)
	// 处理 32 位回绕：如果 last 大于 now，说明 dwTime 是上一次回绕前的时刻。
	if last > now {
		return 0
	}
	idleMs := now - last
	return time.Duration(idleMs) * time.Millisecond
}
