//go:build darwin

package pause

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework CoreFoundation -framework CoreGraphics
#import <CoreFoundation/CoreFoundation.h>
#import <CoreGraphics/CoreGraphics.h>
#include <stdlib.h>

// isScreenLocked 通过 CGSessionCopyCurrentDictionary 读取 CGSSessionScreenIsLocked。
//
// 【为什么用轮询而不是 NSWorkspace 通知】
// macOS 的锁屏事件（CGSSessionScreenIsLocked 变为 1）没有公开的分布式通知；
// IOKit 的 IOPMMessage 能拿到休眠/唤醒，但拿不到「屏幕被锁定」。
// 轮询是唯一跨版本稳定的实现，且开销可控（1 秒一次、单次几微秒）。
//
// 【为什么 extern 声明返回 CFDictionaryRef】CGSessionCopyCurrentDictionary 在
// 10.15+ 的 SDK 里声明在 CGSession.h（随 CoreGraphics 提供），但 Go 的 cgo
// 预编译单元不自动 import 该头文件；手写 extern 时返回类型必须与真实签名一致
// （Boolean 是复制粘贴留下的错，编译期就会报 incompatible integer to pointer）。
extern CFDictionaryRef CGSessionCopyCurrentDictionary(void);

static int isScreenLocked() {
    CFDictionaryRef d = CGSessionCopyCurrentDictionary();
    if (d == NULL) {
        return 0;
    }
    CFTypeRef v = CFDictionaryGetValue(d, CFSTR("CGSSessionScreenIsLocked"));
    int locked = 0;
    if (v != NULL && CFGetTypeID(v) == CFNumberGetTypeID()) {
        int n = 0;
        CFNumberGetValue((CFNumberRef)v, kCFNumberSInt32Type, &n);
        locked = n;
    }
    CFRelease(d);
    return locked;
}
*/
import "C"
import (
	"time"
)

// startLockWatcher 启动 macOS 的锁屏监听（轮询 + IOKit 休眠通知兜底）。
//
// 返回一个停止函数；调用方应在应用退出前调用它。轮询周期 1 秒：够快（用户
// 锁屏后 ≤1 秒就能停投递），够省（一次 CGSessionCopyCurrentDictionary 的开销
// 只有几微秒）。
//
// 【休眠怎么覆盖】休眠时屏幕必然锁定，锁屏信号已经能触发暂停。真正的休眠
// 事件（合盖、系统 idle sleep）在恢复后 CGSSessionScreenIsLocked 会变回 0，
// 我们的轮询会在下一 tick 检测到并清除暂停状态。
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
				locked := C.isScreenLocked() != 0
				d.SetLocked(locked)
			}
		}
	}()
	return func() {
		close(stop)
		<-done
	}
}
