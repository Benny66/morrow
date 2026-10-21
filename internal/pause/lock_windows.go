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
)

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

// startLockWatcher 启动 Windows 的锁屏监听（轮询 SHQueryUserNotificationState）。
//
// 【为什么用轮询而不是 WM_WTSSESSION_CHANGE 消息】那个消息要求有一个窗口过程
// 接收它，而 Wails v3 的主窗口过程由框架持有，注册自定义窗口消息处理成本高。
// SHQueryUserNotificationState 是纯查询 API，1 秒一次开销可以忽略。
//
// 【注意】qunsSuspended 意味着「即将进入睡眠」，此时用户已经不能收到提醒；
// 屏幕锁定通过 GetForegroundWindow == NULL 或桌面切换来判（更复杂）。这里
// 覆盖 qunsSuspended + qunsAwayMode 作为「不能打扰」的近似。
func startLockWatcher(d *LockDetector) func() {
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
				// 只把「明确进入睡眠/离开模式」视为锁屏；其他状态留给全屏/空闲检测处理。
				isSuspended := state == qunsSuspended || state == qunsAwayMode
				d.SetLocked(isSuspended)
			}
		}
	}()
	return func() {
		close(stop)
		<-done
	}
}
