//go:build !darwin && !windows

package pause

// startLockWatcher 在非目标平台返回一个立刻退出的 stop 函数。
//
// 与 idle_other.go 同理：Linux 不在支持范围，但包要能编译。锁屏暂停信号
// 静默缺席（LockDetector 永远处于未锁屏状态），其余流程不受影响。
func startLockWatcher(d *LockDetector) func() { return func() {} }
