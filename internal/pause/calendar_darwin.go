//go:build darwin

package pause

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework EventKit -framework Foundation
#import <EventKit/EventKit.h>
#import <Foundation/Foundation.h>

// 【为什么关掉这一条弃用警告】代码里保留了 macOS 12–13 的旧 API 分支，
// 它们只在 @available 的 else 分支里执行。clang 仍会对这些「编译期存在、
// 运行期已判过版本」的引用发 -Wdeprecated-declarations；而 cgo 编译产生的
// 警告在 CI 上会让构建失败。semantics 由 @available 保证，警告在这里是噪音。
#define MORROW_ALLOW_DEPRECATED_BEGIN \
    _Pragma("clang diagnostic push") \
    _Pragma("clang diagnostic ignored \"-Wdeprecated-declarations\"")
#define MORROW_ALLOW_DEPRECATED_END _Pragma("clang diagnostic pop")

MORROW_ALLOW_DEPRECATED_BEGIN

// calendarAuthorized 判断是否已获得【完整读取】日历的权限。
//
// 【为什么不用 EKAuthorizationStatusAuthorized】macOS 14 起它被拆成
// FullAccess / WriteOnly 并标记为 deprecated；12–13 仍然只有旧常量。
// 按系统版本分支判断，两边都表达「能读事件」这一个语义。
static bool calendarAuthorized(void) {
    EKAuthorizationStatus status = [EKEventStore authorizationStatusForEntityType:EKEntityTypeEvent];
    if (@available(macOS 14.0, *)) {
        return status == EKAuthorizationStatusFullAccess;
    }
    return status == EKAuthorizationStatusAuthorized;
}

// calendarBusyNow 通过 EventKit 读取【当前时刻】是否落在忙碌事件里。
//
// 【严格只读 busy】不访问 EKEvent 的 title / attendees / notes / location 任何字段；
// 只用 startDate / endDate / availability 判断此刻是否被占用。见 ADR 0005。
//
// 【为什么不落盘、不缓存查询结果】每次都现场算：一次 fetch 返回若干条 event，
// 但只从中读 availability；查询完即丢，不进任何全局状态。
//
// 【authorized】未授权时 out_granted=0；调用方必须据此降级。
static int calendarBusyNow(int *out_granted) {
    *out_granted = 0;
    if (!calendarAuthorized()) {
        return 0;
    }
    *out_granted = 1;

    EKEventStore *store = [[EKEventStore alloc] init];
    NSDate *now = [NSDate date];
    NSDate *start = [now dateByAddingTimeInterval:-1 * 60];        // 前 1 分钟
    NSDate *end = [now dateByAddingTimeInterval:60];                // 后 1 分钟
    NSPredicate *pred = [store predicateForEventsWithStartDate:start endDate:end calendars:nil];
    if (pred == nil) return 0;
    NSArray<EKEvent *> *events = [store eventsMatchingPredicate:pred];
    for (EKEvent *ev in events) {
        // 只判 availability：非 Free 视为忙碌。
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
//
// 【14+ 必须用 requestFullAccessToEvents】旧的 requestAccessToEntityType:
// 在 macOS 14 起被标记 deprecated，且不再授予完整读取权限——继续用会在新系统上
// 「弹了窗但永远读不到事件」。按版本分支调用。
static int requestCalendarAccess(void) {
    if (calendarAuthorized()) {
        return 1;
    }
    EKAuthorizationStatus status = [EKEventStore authorizationStatusForEntityType:EKEntityTypeEvent];
    if (status == EKAuthorizationStatusDenied || status == EKAuthorizationStatusRestricted) {
        return 0;
    }
    // NotDetermined：发起请求。completionHandler 里不做任何事——我们不关心回调时机，
    // 只想触发系统弹窗。下一次 detector.Detect 会读到新状态。
    EKEventStore *store = [[EKEventStore alloc] init];
    if (@available(macOS 14.0, *)) {
        [store requestFullAccessToEventsWithCompletion:^(BOOL granted, NSError *error) {
            (void)granted; (void)error;
        }];
    } else {
        [store requestAccessToEntityType:EKEntityTypeEvent completion:^(BOOL granted, NSError *error) {
            (void)granted; (void)error;
        }];
    }
    return 0;
}

MORROW_ALLOW_DEPRECATED_END
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
