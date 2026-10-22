//go:build windows

package pause

import (
	"syscall"
	"time"
	"unsafe"
)

var (
	shell32                          = syscall.NewLazyDLL("shell32.dll")
	procSHQueryUserNotificationState = shell32.NewProc("SHQueryUserNotificationState")
	procOpenInputDesktop             = user32.NewProc("OpenInputDesktop")
	procCloseDesktop                 = user32.NewProc("CloseDesktop")
)

const (
	// desktopReadObjects 是 OpenInputDesktop 的最小访问权限（DESKTOP_READOBJECTS）。
	desktopReadObjects = 0x0001
	// errorAccessDenied 是锁屏时 OpenInputDesktop 最常见失败码。
	errorAccessDenied = syscall.Errno(5)
)

// isInputDesktopLocked 通过「能否打开输入桌面」判断 Win+L 锁屏。
//
// 【为什么不只用 SHQueryUserNotificationState】它只覆盖即将睡眠/离开模式，
// 不覆盖 Win+L。锁屏后输入桌面不可打开，OpenInputDesktop 返回 NULL 且
// GetLastError=ERROR_ACCESS_DENIED；解锁后恢复。
//
// 【为什么只把 ACCESS_DENIED 当作锁定】其他失败（企业策略、权限受限）不代表
// 用户锁屏；误判会让久坐抵扣凭空发生。失败时返回 false，保留 suspend/away 兜底。
func isInputDesktopLocked() bool {
	h, _, err := procOpenInputDesktop.Call(
		uintptr(0),
		uintptr(0),
		uintptr(desktopReadObjects),
	)
	if h != 0 {
		procCloseDesktop.Call(h)
		return false
	}
	if errno, ok := err.(syscall.Errno); ok && errno == errorAccessDenied {
		return true
	}
	return false
}

// QUNS 是 SHQueryUserNotificationState 的返回值枚举（见 shellapi.h）。
type QUNS int

const (
	qunsRunning              QUNS = 1 // 正常运行
	qunsSuspended            QUNS = 2 // 系统即将进入睡眠
	qunsRunningButDndEnabled QUNS = 3 // 专注模式开启（Windows 10 及以后）
	qunsAwayMode             QUNS = 4 // 离开模式（媒体播放中）
	qunsRunningDndEnabled    QUNS = 5 // 专注模式
	qunsAcquiredDisplay      QUNS = 6 // 显示器已获取
	qunsBusyAppRunning       QUNS = 7 // 有忙碌应用（如全屏演示）
)

// queryUserNotificationState 返回当前用户通知状态；失败返回 0。
func queryUserNotificationState() QUNS {
	var state uint32
	r, _, _ := procSHQueryUserNotificationState.Call(uintptr(unsafe.Pointer(&state)))
	if r != 0 { // S_OK = 0
		return 0
	}
	return QUNS(state)
}

// startLockWatcher 启动 Windows 的锁屏监听（轮询）。
//
// 【为什么用轮询而不是 WM_WTSSESSION_CHANGE 消息】那个消息要求有一个窗口过程
// 接收它，而 Wails v3 的主窗口过程由框架持有，注册自定义窗口消息处理成本高。
// 纯查询 API 1 秒一次开销可以忽略。
//
// 信号由两部分组成：
//   - SHQueryUserNotificationState：即将睡眠 / 离开模式；
//   - OpenInputDesktop：Win+L 锁屏（锁屏时输入桌面不可打开）。
//
// 【真实 Windows 验证】OpenInputDesktop 在企业策略/受限终端下可能不可用；
// 004 必须在真实 Windows 上做 Win+L 1 分钟 / 6 分钟两档验收。不可用时只保留
// suspend/away，并在设置页明确「Windows 锁屏记账不可用」，不静默假装成功。
func startLockWatcher(d *LockDetector) func() {
	// 首轮同步读取：久坐运行时在启动后立即 tick，若等第一次 1 秒轮询，
	// 锁屏启动会被短暂当成未锁屏，凭空生成一段久坐与锁屏休息。
	state := queryUserNotificationState()
	d.SetLocked(state == qunsSuspended || state == qunsAwayMode || isInputDesktopLocked())
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				state := queryUserNotificationState()
				isSuspended := state == qunsSuspended || state == qunsAwayMode
				d.SetLocked(isSuspended || isInputDesktopLocked())
			}
		}
	}()
	return func() {
		close(stop)
		<-done
	}
}
