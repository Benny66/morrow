package pause

import (
	"sync"
	"testing"
	"time"
)

// TestLockDetectorPausedWhenLocked 验证锁屏/休眠时 LockDetector 返回 ReasonLock。
func TestLockDetectorPausedWhenLocked(t *testing.T) {
	d := NewLockDetector()
	// 初始状态：未锁屏。
	if d.Detect() != ReasonNone {
		t.Error("初始状态应为 ReasonNone（未锁屏）")
	}

	// 模拟锁屏事件。
	d.SetLocked(true)
	if d.Detect() != ReasonLock {
		t.Errorf("锁屏后应返回 ReasonLock，实际为 %s", d.Detect())
	}

	// 模拟解锁事件。
	d.SetLocked(false)
	if d.Detect() != ReasonNone {
		t.Errorf("解锁后应返回 ReasonNone，实际为 %s", d.Detect())
	}
}

// TestLockDetectorPausedWhenSleeping 验证休眠时 LockDetector 返回 ReasonLock。
func TestLockDetectorPausedWhenSleeping(t *testing.T) {
	d := NewLockDetector()
	// 模拟休眠事件（willSleepNotification）。
	d.SetSleeping(true)
	if d.Detect() != ReasonLock {
		t.Errorf("休眠时应返回 ReasonLock，实际为 %s", d.Detect())
	}

	// 模拟唤醒事件（didWakeNotification）。
	d.SetSleeping(false)
	if d.Detect() != ReasonNone {
		t.Errorf("唤醒后应返回 ReasonNone，实际为 %s", d.Detect())
	}
}

// TestLockDetectorBothLockedAndSleeping 验证同时锁屏与休眠仍返回单一 ReasonLock。
func TestLockDetectorBothLockedAndSleeping(t *testing.T) {
	d := NewLockDetector()
	d.SetLocked(true)
	d.SetSleeping(true)
	if d.Detect() != ReasonLock {
		t.Errorf("锁屏+休眠时应返回 ReasonLock，实际为 %s", d.Detect())
	}
	// 只解锁不休眠：仍锁着（休眠未结束）→ 应保持 paused。
	// 【注意】这里以「任一信号为 true 即视为锁屏」为原则；两个信号独立开关，
	// 只要有任何一个还在锁着/休眠，就不该恢复投递。
	if d.IsLocked() {
		// 预期：仍在锁着。
	} else {
		t.Error("锁屏+休眠时 IsLocked() 应为 true")
	}
	// 只唤醒不休眠。
	d.SetSleeping(false)
	if d.Detect() != ReasonLock {
		t.Error("仍有锁屏信号时应保持 ReasonLock")
	}
	d.SetLocked(false)
	if d.Detect() != ReasonNone {
		t.Error("两个信号都清除后应返回 ReasonNone")
	}
}

// TestLockDetectorSince 验证锁屏开始时间被记录。
func TestLockDetectorSince(t *testing.T) {
	d := NewLockDetector()
	before := time.Now().Add(-time.Second)
	d.SetLocked(true)
	after := time.Now().Add(time.Second)

	since := d.Since()
	if since.Before(before) || since.After(after) {
		t.Errorf("Since 应在 SetLocked 前后区间内，实际为 %v", since)
	}
}

// TestCompositeProviderLockWinsOverIdle 验证优先级：锁屏 > 空闲。
//
// 【为什么锁屏优先于空闲】锁屏是明确的「人不在」信号，空闲是「看起来人不在」；
// 前者更可信，日志与状态展示应该显示更精确的那个原因。
func TestCompositeProviderLockWinsOverIdle(t *testing.T) {
	lock := NewLockDetector()
	lock.SetLocked(true)

	idle := NewIdleDetector(func() time.Duration {
		return 10 * time.Minute
	}, 5*time.Minute)

	provider := NewCompositeProvider(lock, idle)
	state := provider.State()
	if state.Reason != ReasonLock {
		t.Errorf("锁屏应优先于空闲，实际原因为 %s", state.Reason)
	}
	if !state.IsPaused() {
		t.Error("任一子信号触发时 ShouldBePaused")
	}
}

// TestCompositeProviderIdleOnly 验证仅空闲信号触发时返回 ReasonIdle。
func TestCompositeProviderIdleOnly(t *testing.T) {
	lock := NewLockDetector()
	idle := NewIdleDetector(func() time.Duration {
		return 6 * time.Minute
	}, 5*time.Minute)

	provider := NewCompositeProvider(lock, idle)
	state := provider.State()
	if state.Reason != ReasonIdle {
		t.Errorf("仅空闲触发时应返回 ReasonIdle，实际为 %s", state.Reason)
	}
}

// TestCompositeProviderNothingTriggered 验证无信号触发时返回 ReasonNone。
func TestCompositeProviderNothingTriggered(t *testing.T) {
	lock := NewLockDetector()
	idle := NewIdleDetector(func() time.Duration {
		return 1 * time.Minute
	}, 5*time.Minute)

	provider := NewCompositeProvider(lock, idle)
	state := provider.State()
	if state.Reason != ReasonNone {
		t.Errorf("无信号触发时应返回 ReasonNone，实际为 %s", state.Reason)
	}
	if state.IsPaused() {
		t.Error("无信号触发时 IsPaused 应为 false")
	}
}

// TestLockDetectorConcurrentAccess 验证并发读写不产生数据竞争。
//
// 【为什么要单独测】平台层（NSWorkspace observer）从系统通知线程调用 SetLocked，
// 引擎的调度 goroutine 从另一个线程调用 Detect。没有同步原语的话会在 -race 下变红。
func TestLockDetectorConcurrentAccess(t *testing.T) {
	d := NewLockDetector()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			d.SetLocked(i%2 == 0)
		}(i)
		go func() {
			defer wg.Done()
			_ = d.Detect()
			_ = d.IsLocked()
			_ = d.Since()
		}()
	}
	wg.Wait()
}
