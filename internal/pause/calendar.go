package pause

import (
	"sync"
	"time"
)

// CalendarStatus 是「当前日历是否忙碌」的读取结果。
//
// 【刻意只暴露两个字段】ADR 0005 明确：只读布尔状态，绝不读取事件标题、
// 参与人、备注、地点。加进任何 event-derived 字段都会破坏这条承诺；见
// calendar_test.go 里的类型级守卫。
type CalendarStatus struct {
	// Granted 是否已拿到读取权限。
	Granted bool
	// Busy 当前时刻是否落在某个标记为忙碌的事件里。
	Busy bool
}

// CalendarDetector 是日历忙闲的暂停信号检测器。
//
// macOS 用 EventKit 读取「此刻」是否 busy；平台层 getStatus 回调每 tick 一次。
// Windows / Linux 上没有等价的公开能力（Windows 的 Appointment API 是 UWP-only，
// 桌面应用拿不到），检测器静默缺席。
type CalendarDetector struct {
	getStatus func() CalendarStatus

	mu    sync.Mutex
	inHit bool
	since time.Time
	now   func() time.Time
}

// NewCalendarDetector 构造日历检测器。
func NewCalendarDetector(getStatus func() CalendarStatus) *CalendarDetector {
	return &CalendarDetector{
		getStatus: getStatus,
		now:       time.Now,
	}
}

// Detect 返回当前日历忙碌状态对应的暂停原因。
func (d *CalendarDetector) Detect() PauseReason {
	if d.getStatus == nil {
		return ReasonNone
	}
	st := d.getStatus()
	hit := st.Granted && st.Busy

	d.mu.Lock()
	defer d.mu.Unlock()
	if hit && !d.inHit {
		d.inHit = true
		d.since = d.now()
	} else if !hit && d.inHit {
		d.inHit = false
		d.since = time.Time{}
	}
	if hit {
		return ReasonCalendar
	}
	return ReasonNone
}

// Since 返回进入忙碌的时刻；未触发时为零值。
func (d *CalendarDetector) Since() time.Time {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.since
}

// State 让 CalendarDetector 满足 Provider 接口。
func (d *CalendarDetector) State() State {
	if d.Detect() == ReasonNone {
		return State{Reason: ReasonNone}
	}
	return State{Reason: ReasonCalendar, Since: d.Since()}
}
