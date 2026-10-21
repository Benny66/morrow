//go:build darwin

package pause

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Foundation
#import <Foundation/Foundation.h>
#import <objc/message.h>
#import <objc/runtime.h>
#import <stdlib.h>
// ^ 注：objc_msgSend 的类型转换需要 <objc/message.h>。

// focusEnabled 通过 INFocusStatusCenter 读取系统专注是否开启。
//
// 【为什么用协议反射而不是直接 import Intents】INFocusStatusCenter 是 macOS 12+
// 的公开 API；用 NSClassFromString 反射拿类，能保持编译目标覆盖到 macOS 11，
// 老系统上返回 Granted=false 让上层静默降级。
// 【为什么不用 entitlement】非沙盒桌面应用可以走 API 请求路径；系统会弹
// 「允许 Morrow 读取专注状态」的原生授权窗。
//
// 【authorized vs isFocusEnabled】INFocusStatusCenter.default 有一个 authorized 属性；
// 未授权时读 focusStatus 会返回 nil。这两个都要判，只判一个会误报。
static int focusEnabled(int *out_granted) {
    *out_granted = 0;
    Class center = NSClassFromString(@"INFocusStatusCenter");
    if (center == nil) return 0;
    id defaultCenter = [center performSelector:@selector(defaultCenter)];
    if (defaultCenter == nil) return 0;
    // authorized: BOOL 属性
    SEL authSel = NSSelectorFromString(@"authorized");
    if (![defaultCenter respondsToSelector:authSel]) return 0;
    BOOL authorized = ((BOOL (*)(id, SEL))objc_msgSend)(defaultCenter, authSel);
    if (!authorized) return 0;
    *out_granted = 1;
    // focusStatus.isEnabled
    SEL statusSel = NSSelectorFromString(@"focusStatus");
    if (![defaultCenter respondsToSelector:statusSel]) return 0;
    id status = ((id (*)(id, SEL))objc_msgSend)(defaultCenter, statusSel);
    if (status == nil) return 0;
    SEL enabledSel = NSSelectorFromString(@"isEnabled");
    if (![status respondsToSelector:enabledSel]) return 0;
    BOOL enabled = ((BOOL (*)(id, SEL))objc_msgSend)(status, enabledSel);
    return enabled ? 1 : 0;
}

// requestFocusAccess 触发 macOS 的专注状态授权流程。
//
// 【注意】系统 API 里没有"直接弹窗申请专注状态权限"的入口——授权是在应用首次
// 调用 startFocusStatusUpdateHandler 时由系统弹的。我们这里主动调一下这个
// handler 让弹窗出现；返回当下已授权状态（第一次调用大概率返回 false，
// 用户点允许后下一次调用才会 true）。
static int requestFocusAccess(void) {
    Class center = NSClassFromString(@"INFocusStatusCenter");
    if (center == nil) return 0;
    id defaultCenter = [center performSelector:@selector(defaultCenter)];
    if (defaultCenter == nil) return 0;
    SEL reqSel = NSSelectorFromString(@"requestAuthorizationWithCompletionHandler:");
    if (![defaultCenter respondsToSelector:reqSel]) return 0;
    // 用一个空的 completionHandler；系统弹窗是异步的，本次调用不阻塞。
    void (^block)(BOOL) = ^(BOOL granted) { (void)granted; };
    ((void (*)(id, SEL, void*))objc_msgSend)(defaultCenter, reqSel, block);
    return 1;
}
*/
import "C"

// GetFocusStatus 返回当前系统专注状态。
func GetFocusStatus() FocusStatus {
	var granted C.int
	enabled := C.focusEnabled(&granted)
	return FocusStatus{
		Granted: granted != 0,
		Enabled: enabled != 0,
	}
}

// RequestFocusAccess 触发系统授权弹窗；返回是否【已经】授权。
//
// 第一次调用大概率返回 false（弹窗已发出但用户还没点），第二次起才会 true。
// 上层可以在 Settings.vue 保存「专注联动」开关时调这个函数，随后由 FocusDetector
// 在轮询里读取真实状态；不阻塞 UI。
func RequestFocusAccess() bool {
	return C.requestFocusAccess() != 0
}
