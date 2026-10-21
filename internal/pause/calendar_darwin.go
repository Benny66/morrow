//go:build darwin

package pause

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework EventKit -framework Foundation
#import <EventKit/EventKit.h>
#import <Foundation/Foundation.h>

// calendarBusyNow 通过 EventKit 读取【当前时刻】是否落在忙碌事件里。
//
// 【严格只读 busy】不访问 EKEvent 的 title / attendees / notes / location 任何字段；
// 只用 eventForReferenceDate + EKOccurrenceMatchingOptions 得到 busyStatus。
// 见 ADR 0005。
//
// 【为什么不落盘、不缓存查询结果】每次都现场算：一次 fetch 返回若干条 event，
// 但只从中读 busyStatus；查询完即丢，不进任何全局状态。
//
// 【authorized】未授权时 out_granted=0；调用方必须据此降级。
static int calendarBusyNow(int *out_granted) {
    *out_granted = 0;
    EKEventStore *store = [[EKEventStore alloc] init];
    // macOS 上 EKAuthorizationStatus 三态：NotDetermined / Authorized / Denied。
    // 非 macOS 沙盒应用可以用 requestAccess 走弹窗；我们已经在上层调用里请求过一次，
    // 这里只读现状。
    EKAuthorizationStatus status = [EKEventStore authorizationStatusForEntityType:EKEntityTypeEvent];
    if (status != EKAuthorizationStatusAuthorized) {
        return 0;
    }
    *out_granted = 1;

    NSDate *now = [NSDate date];
    NSDate *start = [now dateByAddingTimeInterval:-1 * 60];        // 前 1 分钟
    NSDate *end = [now dateByAddingTimeInterval:60];                // 后 1 分钟
    NSPredicate *pred = [store predicateForEventsWithStartDate:start endDate:end calendars:nil];
    if (pred == nil) return 0;
    NSArray<EKEvent *> *events = [store eventsMatchingPredicate:pred];
    for (EKEvent *ev in events) {
        // 只判 busyStatus：availability != Free 视为忙碌。
        // 【不读 title / notes / location / attendees】ADR 0005 的硬边界。
        if (ev.availability != EKEventAvailabilityFree) {
            NSTimeInterval s = [ev.startDate timeIntervalSinceDate:now];
            NSTimeInterval e = [ev.endDate timeIntervalSinceDate:now];
            if (s <= 0 && e > 0) {
                return 1;
            }
        }
    }
    return 0;
}

// requestCalendarAccess 触发 macOS 的日历授权弹窗。
//
// 【异步语义】系统弹窗是异步的；本次调用不等用户点，返回当下已授权状态。
// 上层在 Settings.vue 保存日历开关时调一下，随后 detector 每 tick 读真实状态。
static int requestCalendarAccess(void) {
    EKAuthorizationStatus status = [EKEventStore authorizationStatusForEntityType:EKEntityTypeEvent];
    if (status == EKAuthorizationStatusAuthorized) {
        return 1;
    }
    if (status == EKAuthorizationStatusDenied || status == EKAuthorizationStatusRestricted) {
        return 0;
    }
    // NotDetermined：发起请求。completionHandler 里不做任何事——我们不关心回调时机，
    // 只想触发系统弹窗。下一次 detector.Detect 会读到新状态。
    EKEventStore *store = [[EKEventStore alloc] init];
    [store requestAccessToEntityType:EKEntityTypeEvent completion:^(BOOL granted, NSError *error) {
        (void)granted; (void)error;
    }];
    return 0;
}
*/
import "C"

// GetCalendarStatus 返回当前时刻日历是否忙碌。
func GetCalendarStatus() CalendarStatus {
	var granted C.int
	busy := C.calendarBusyNow(&granted)
	return CalendarStatus{
		Granted: granted != 0,
		Busy:    busy != 0,
	}
}

// RequestCalendarAccess 触发系统授权弹窗；返回当下是否已授权。
func RequestCalendarAccess() bool {
	return C.requestCalendarAccess() != 0
}
