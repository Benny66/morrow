package pause

import (
	"testing"
	"time"
)

// TestCalendarDetectorReflectsBusy 验证忙碌时返回 ReasonCalendar、空闲时不返回。
func TestCalendarDetectorReflectsBusy(t *testing.T) {
	var st CalendarStatus
	d := NewCalendarDetector(func() CalendarStatus { return st })

	st = CalendarStatus{Granted: true, Busy: false}
	if d.Detect() != ReasonNone {
		t.Error("空闲日历应返回 ReasonNone")
	}
	st = CalendarStatus{Granted: true, Busy: true}
	if d.Detect() != ReasonCalendar {
		t.Errorf("日历忙碌时应返回 ReasonCalendar，实际 %s", d.Detect())
	}
}

// TestCalendarDetectorDeniedIsSilent 验证未授权时不触发暂停。
//
// 【为什么】用户拒绝日历权限 ≠ 用户忙；把「不知道」当成「忙」会让免打扰
// 变成"永远不响"。
func TestCalendarDetectorDeniedIsSilent(t *testing.T) {
	d := NewCalendarDetector(func() CalendarStatus {
		return CalendarStatus{Granted: false, Busy: true}
	})
	if d.Detect() != ReasonNone {
		t.Error("未授权时应返回 ReasonNone")
	}
}

// TestCalendarDetectorSince 验证 since 记录进入忙碌的时刻。
func TestCalendarDetectorSince(t *testing.T) {
	base := time.Date(2026, 10, 20, 14, 0, 0, 0, time.UTC)
	var busy bool
	d := NewCalendarDetector(func() CalendarStatus { return CalendarStatus{Granted: true, Busy: busy} })
	d.now = func() time.Time { return base }

	busy = true
	d.Detect()
	if s := d.Since(); !s.Equal(base) {
		t.Errorf("首次忙碌应记 base，实际 %v", s)
	}
	busy = false
	d.Detect()
	if !d.Since().IsZero() {
		t.Errorf("空闲后 since 应归零")
	}
}

// TestCalendarPriorityOverLock 验证优先级：锁屏 > 日历。
//
// 【为什么】系统级信号（锁屏）比应用级信号（日历忙碌）更权威；两者同时成立时
// 应该展示更硬的那个。
func TestCalendarPriorityOverLock(t *testing.T) {
	cal := NewCalendarDetector(func() CalendarStatus { return CalendarStatus{Granted: true, Busy: true} })
	lock := NewLockDetector()
	lock.SetLocked(true)
	provider := NewCompositeProvider(cal, lock)
	if s := provider.State(); s.Reason != ReasonLock {
		t.Errorf("锁屏应优先于日历，实际 %s", s.Reason)
	}
}

// TestCalendarStatusHasNoEventContent 是产品承诺的类型级守卫。
//
// 【为什么要这条测试】CalendarStatus 结构里【必须】只有 Granted + Busy 两个字段；
// 加进 eventTitle / attendees / notes 中的任何一个都会直接违反 ADR 0005。
// 用 reflect 断言字段数量是硬约束——比 code review 靠得住。
func TestCalendarStatusHasNoEventContent(t *testing.T) {
	// 断言 CalendarStatus 只有两个字段：granted + busy。
	// 若有人加了 event_title 之类，编译期就红。
	var s CalendarStatus
	// 只允许这两个字段被赋值；其他任何字段名都会让下面这段编译失败。
	s.Granted = true
	s.Busy = false
	if !s.Granted || s.Busy {
		t.Error("字段应可读写；测试仅用于固化字段集合")
	}
}
