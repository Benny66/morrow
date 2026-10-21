package pause

import (
	"sync"
	"time"
)

// LockDetector 是锁屏/休眠检测器（按系统通知事件驱动）。
//
// macOS 通过 NSWorkspace 的 willSleepNotification / didWakeNotification 与
// screensDidSleepNotification / screensDidWakeNotification 监听；
// Windows 通过 WM_WTSSESSION_CHANGE 消息监听（见 lock_windows.go）。
//
// 【为什么由事件推送而不是轮询】锁屏/休眠是低频事件，一旦发生时用户希望
// 立刻停止投递；轮询至少一个 tick（20 秒）延迟，而事件可以在毫秒级响应。
//
// 【为什么这里不 import 平台包】本文件只维护状态；平台层由各自的 _darwin / _windows
// 文件通过 SetLocked / SetSleeping 驱动，这样锁屏/休眠逻辑本身可以纯单元测试。
type LockDetector struct {
	mu       sync.RWMutex
	locked   bool      // 屏幕是否锁定
	sleeping bool      // 系统是否休眠
	since    time.Time // 首次触发时的时间戳
	now      func() time.Time
}

// NewLockDetector 构造锁屏检测器（初始状态为未锁屏/未休眠）。
func NewLockDetector() *LockDetector {
	return &LockDetector{now: time.Now}
}

// SetLocked 更新锁屏状态（true = 锁屏，false = 解锁）。
//
// 由平台观察者从系统通知线程调用；本方法内部加锁，可安全并发。
func (d *LockDetector) SetLocked(v bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.locked == v {
		return
	}
	d.locked = v
	d.updateSinceLocked()
}

// SetSleeping 更新休眠状态（true = 系统即将/正在休眠，false = 唤醒）。
func (d *LockDetector) SetSleeping(v bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.sleeping == v {
		return
	}
	d.sleeping = v
	d.updateSinceLocked()
}

// updateSinceLocked 维护 since：任一信号从「无」到「有」时记新时刻；
// 两个信号都清除时归零。锁状态在过渡之间保留原 since（否则「锁屏→休眠→解锁」
// 会让暂停开始时间在中间被重置）。
//
// 调用方必须已持有 d.mu 写锁。
func (d *LockDetector) updateSinceLocked() {
	active := d.locked || d.sleeping
	if !active {
		d.since = time.Time{}
		return
	}
	if d.since.IsZero() {
		d.since = d.now()
	}
}

// IsLocked 返回当前是否处于「锁屏或休眠」状态。
func (d *LockDetector) IsLocked() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.locked || d.sleeping
}

// Since 返回暂停开始时间；未处于暂停状态时为零值。
func (d *LockDetector) Since() time.Time {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.since
}

// Detect 返回当前锁屏/休眠状态对应的暂停原因。
func (d *LockDetector) Detect() PauseReason {
	if d.IsLocked() {
		return ReasonLock
	}
	return ReasonNone
}

// State 让 LockDetector 满足 Provider 接口。
func (d *LockDetector) State() State {
	if !d.IsLocked() {
		return State{Reason: ReasonNone}
	}
	return State{Reason: ReasonLock, Since: d.Since()}
}
