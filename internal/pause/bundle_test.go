package pause

import "testing"

// TestNewBundleLockWatcherIndependentFromPause 保证久坐能读到锁屏时长，
// 即使用户关闭了「锁屏暂停」；同时不能因此把 ReasonLock 加回 Composite。
func TestNewBundleLockWatcherIndependentFromPause(t *testing.T) {
	original := lockWatcherStarter
	t.Cleanup(func() { lockWatcherStarter = original })

	started, stopped := 0, 0
	lockWatcherStarter = func(_ *LockDetector) func() {
		started++
		return func() { stopped++ }
	}

	bundle, err := NewBundle(Config{LockEnabled: false, LockWatcherEnabled: true})
	if err != nil {
		t.Fatalf("NewBundle 失败: %v", err)
	}
	if started != 1 {
		t.Fatalf("LockWatcherEnabled=true 时 watcher 启动次数 = %d，期望 1", started)
	}

	bundle.Lock.SetLocked(true)
	if bundle.Provider.State().IsPaused() {
		t.Error("LockEnabled=false 时锁屏不应进入 Composite（用户关掉了锁屏暂停）")
	}
	bundle.Stop()
	bundle.Stop()
	if stopped != 1 {
		t.Errorf("Stop 幂等失败：watcher stop 次数 = %d，期望 1", stopped)
	}
}

// TestNewBundleLockEnabledStillPauses 保证原有「锁屏暂停」行为不变。
func TestNewBundleLockEnabledStillPauses(t *testing.T) {
	original := lockWatcherStarter
	t.Cleanup(func() { lockWatcherStarter = original })

	started := 0
	lockWatcherStarter = func(_ *LockDetector) func() {
		started++
		return func() {}
	}

	bundle, err := NewBundle(Config{LockEnabled: true, LockWatcherEnabled: true})
	if err != nil {
		t.Fatalf("NewBundle 失败: %v", err)
	}
	if started != 1 {
		t.Fatalf("watcher 启动次数 = %d，期望 1", started)
	}
	bundle.Lock.SetLocked(true)
	if got := bundle.Provider.State().Reason; got != ReasonLock {
		t.Errorf("锁屏时 Provider.State().Reason = %q，期望 %q", got, ReasonLock)
	}
	bundle.Stop()
}

// TestNewBundleNoWatcherWhenDisabled 关闭 watcher 时不启动后台 goroutine。
func TestNewBundleNoWatcherWhenDisabled(t *testing.T) {
	original := lockWatcherStarter
	t.Cleanup(func() { lockWatcherStarter = original })

	started := 0
	lockWatcherStarter = func(_ *LockDetector) func() {
		started++
		return func() {}
	}

	bundle, err := NewBundle(Config{LockEnabled: true, LockWatcherEnabled: false})
	if err != nil {
		t.Fatalf("NewBundle 失败: %v", err)
	}
	if started != 0 {
		t.Errorf("LockWatcherEnabled=false 时 watcher 启动次数 = %d，期望 0", started)
	}
	bundle.Stop()
}
