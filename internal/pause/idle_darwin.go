//go:build darwin

package pause

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework CoreGraphics -framework AppKit
#import <CoreGraphics/CoreGraphics.h>
#import <AppKit/AppKit.h>
#include <stdlib.h>

// getIdleSeconds 返回系统连续空闲秒数（macOS 实现）。
//
// 使用 CGEventSourceSecondsSinceLastEventType 读取最后一次键鼠输入距今的秒数。
// 该 API 不需要任何权限，是系统公开的只读接口。
//
// 【签名注意】第二个参数是 CGEventSourceStateID（不是 CGEventSourceRef）——
// 常见误用是传一个 CGEventSourceCreate 的 source；编译期就会报 incompatible pointer。
// 直接用组合状态 kCGEventSourceCombinedState 覆盖键盘与鼠标。
static double getIdleSeconds() {
    double key = CGEventSourceSecondsSinceLastEventType(
        kCGEventSourceStateHIDSystemState, kCGEventKeyDown);
    double mouse = CGEventSourceSecondsSinceLastEventType(
        kCGEventSourceStateHIDSystemState, kCGEventMouseMoved);
    return key < mouse ? key : mouse;
}
*/
import "C"
import "time"

// GetIdleDuration 返回 macOS 系统连续空闲时长。
//
// 通过 CGEventSourceSecondsSinceLastEventType 读取最后一次键鼠输入距今的时间。
// 该 API 不需要任何权限，是系统公开的只读接口。
func GetIdleDuration() time.Duration {
	seconds := C.getIdleSeconds()
	return time.Duration(float64(seconds) * float64(time.Second))
}

// IdleSupported 返回当前平台是否提供空闲时长读取。
func IdleSupported() bool { return true }
