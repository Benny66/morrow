//go:build darwin

package pause

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit -framework Foundation
#import <AppKit/AppKit.h>
#import <Foundation/Foundation.h>
#include <stdlib.h>
#include <string.h>

// frontmostBundleID 返回当前前台应用的 bundleIdentifier；无前台或取不到时返回 NULL。
//
// 【为什么用 bundleIdentifier 而不是 localizedName / processName】
// bundle ID 是稳定唯一标识，用户改名不影响；且系统限制只能读元数据，
// 不会触发任何「读取窗口内容」的隐私边界（见 ADR 0004）。
static char* frontmostBundleID() {
    NSRunningApplication *app = [[NSWorkspace sharedWorkspace] frontmostApplication];
    if (app == nil) {
        return NULL;
    }
    NSString *bid = [app bundleIdentifier];
    if (bid == nil) {
        return NULL;
    }
    const char *utf = [bid UTF8String];
    if (utf == NULL) {
        return NULL;
    }
    size_t n = strlen(utf);
    char *copy = (char*)malloc(n + 1);
    if (copy == NULL) {
        return NULL;
    }
    memcpy(copy, utf, n + 1);
    return copy;
}
*/
import "C"
import (
	"unsafe"
)

// GetFrontmostApp 返回当前前台应用的 bundle ID；取不到时返回空串。
//
// 【为什么统一叫 GetFrontmostApp】detector 只关心「一个用于名单匹配的字符串」，
// 平台各自决定这个字符串是 bundle ID（macOS）还是进程名（Windows）。
// 名字统一后 bundle.go 只需要 `NewMeetingDetector(GetFrontmostApp)`，不用平台分叉。
func GetFrontmostApp() string {
	cs := C.frontmostBundleID()
	if cs == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(cs))
	return C.GoString(cs)
}
