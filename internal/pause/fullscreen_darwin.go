//go:build darwin

package pause

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework CoreGraphics -framework Foundation
#import <CoreGraphics/CoreGraphics.h>
#import <Foundation/Foundation.h>
#import <Availability.h>

typedef struct {
    int win_x, win_y, win_w, win_h;
    int scr_x, scr_y, scr_w, scr_h;
    int has_window;   // 是否找到了 layer==0 的可见窗口
    int bounds_ok;    // 是否成功读到了 bounds（缺 bounds 视为权限问题）
} FrontInfo;

static FrontInfo readFrontmostWindow(void) {
    FrontInfo out;
    out.win_x = out.win_y = out.win_w = out.win_h = 0;
    out.scr_x = out.scr_y = out.scr_w = out.scr_h = 0;
    out.has_window = 0;
    out.bounds_ok = 0;

    CFArrayRef list = CGWindowListCopyWindowInfo(
        kCGWindowListOptionOnScreenOnly | kCGWindowListExcludeDesktopElements,
        kCGNullWindowID);
    if (list == NULL) return out;
    CFIndex n = CFArrayGetCount(list);
    if (n == 0) { CFRelease(list); return out; }

    // 找 layer == 0（kCGNormalWindowLevel）的第一个窗口——这就是用户眼前的那个。
    // 忽略 Dock(20)/桌面(0x7fffffff)/菜单栏等其他 layer。
    for (CFIndex i = 0; i < n; i++) {
        CFDictionaryRef w = (CFDictionaryRef)CFArrayGetValueAtIndex(list, i);
        int layer = -1;
        CFTypeRef layerRef = CFDictionaryGetValue(w, kCGWindowLayer);
        if (layerRef != NULL && CFGetTypeID(layerRef) == CFNumberGetTypeID()) {
            CFNumberGetValue((CFNumberRef)layerRef, kCFNumberIntType, &layer);
        }
        if (layer != 0) continue;

        out.has_window = 1;
        CFDictionaryRef bounds = (CFDictionaryRef)CFDictionaryGetValue(w, kCGWindowBounds);
        if (bounds == NULL) break;

        double x = 0, y = 0, width = 0, height = 0;
        CFTypeRef v;
        if ((v = CFDictionaryGetValue(bounds, CFSTR("X"))) && CFGetTypeID(v) == CFNumberGetTypeID())
            CFNumberGetValue((CFNumberRef)v, kCFNumberDoubleType, &x);
        if ((v = CFDictionaryGetValue(bounds, CFSTR("Y"))) && CFGetTypeID(v) == CFNumberGetTypeID())
            CFNumberGetValue((CFNumberRef)v, kCFNumberDoubleType, &y);
        if ((v = CFDictionaryGetValue(bounds, CFSTR("Width"))) && CFGetTypeID(v) == CFNumberGetTypeID())
            CFNumberGetValue((CFNumberRef)v, kCFNumberDoubleType, &width);
        if ((v = CFDictionaryGetValue(bounds, CFSTR("Height"))) && CFGetTypeID(v) == CFNumberGetTypeID())
            CFNumberGetValue((CFNumberRef)v, kCFNumberDoubleType, &height);

        if (width <= 0 || height <= 0) break; // 视为读不到

        out.win_x = (int)x;
        out.win_y = (int)y;
        out.win_w = (int)width;
        out.win_h = (int)height;
        out.bounds_ok = 1;

        // 找窗口中心所在显示器：用 CGGetActiveDisplayList 枚举，比较中心点落在谁的 bounds。
        CGDirectDisplayID displays[8];
        uint32_t dCount = 0;
        CGGetActiveDisplayList(8, displays, &dCount);
        CGDirectDisplayID chosenID = CGMainDisplayID();
        CGFloat cx = x + width / 2.0;
        CGFloat cy = y + height / 2.0;
        for (uint32_t k = 0; k < dCount; k++) {
            CGRect b = CGDisplayBounds(displays[k]);
            if (cx >= b.origin.x && cx < b.origin.x + b.size.width &&
                cy >= b.origin.y && cy < b.origin.y + b.size.height) {
                chosenID = displays[k];
                break;
            }
        }
        CGRect sb = CGDisplayBounds(chosenID);
        out.scr_x = (int)sb.origin.x;
        out.scr_y = (int)sb.origin.y;
        out.scr_w = (int)sb.size.width;
        out.scr_h = (int)sb.size.height;
        break;
    }
    CFRelease(list);
    return out;
}

// preflightScreenCapture 检查是否有屏幕录制权限（macOS 10.15+）。
//
// 【为什么不直接返回 CGPreflightScreenCaptureAccess()】那个 API 是 10.15+ 才有的；
// 老系统上调用会崩溃。用 dlsym 运行时探测——找不到符号就返回 true（视为不需要权限，
// 反正老系统上读 window bounds 也不需要权限）。
static bool preflightScreenCapture(void) {
    if (@available(macOS 10.15, *)) {
        return CGPreflightScreenCaptureAccess();
    }
    return true;
}

static bool requestScreenCapture(void) {
    if (@available(macOS 10.15, *)) {
        return CGRequestScreenCaptureAccess();
    }
    return true;
}
*/
import "C"

// GetFrontWindowInfo 返回当前前台窗口与所在显示器的几何。
//
// 【隐私边界】只读 kCGWindowBounds + kCGWindowLayer（几何与层级）；
// 不读 kCGWindowName（标题）、不截图、不落盘、不上传。见 ADR 0004。
//
// 【Permitted=false 的两种成因】
//   - macOS 10.15+ 无屏幕录制权限，bounds 全部被系统清空（返回 0×0）
//   - 系统窗口列表为空（登录状态、锁屏前）——此时不应该报「全屏」
//
// 上层看到 Permitted=false 就走降级（不暂停、不投递）。
func GetFrontWindowInfo() FrontWindowInfo {
	c := C.readFrontmostWindow()
	info := FrontWindowInfo{
		Window: Rect{X: int(c.win_x), Y: int(c.win_y), Width: int(c.win_w), Height: int(c.win_h)},
		Screen: Rect{X: int(c.scr_x), Y: int(c.scr_y), Width: int(c.scr_w), Height: int(c.scr_h)},
	}
	if c.has_window == 0 {
		// 没有可判定的窗口：视为未触发（正常投递），但不算权限缺失。
		info.Permitted = false
		return info
	}
	info.Permitted = c.bounds_ok != 0
	if !info.Permitted {
		info.PermissionDeniedOnce = !bool(C.preflightScreenCapture())
	}
	return info
}

// ScreenCaptureGranted 返回当前是否已有屏幕录制权限（供 UI 展示状态）。
func ScreenCaptureGranted() bool {
	return bool(C.preflightScreenCapture())
}

// RequestScreenCapture 触发 macOS 的屏幕录制授权弹窗。
//
// 返回 true = 已授权（或系统认为不需要），false = 用户拒绝。
//
// 【调用时机】只在用户【主动】开启「全屏检测」设置项时调用一次；
// 用户拒绝后记录到 config，不再重复弹窗（macOS 二次调用只会跳系统设置）。
func RequestScreenCapture() bool {
	return bool(C.requestScreenCapture())
}
