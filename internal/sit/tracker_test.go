package sit

import (
	"testing"
	"time"
)

func observe(tr *Tracker, at time.Time, idle time.Duration, active bool) TickResult {
	return tr.Observe(TrackInput{
		Now:       at,
		Idle:      idle,
		Active:    active,
		Supported: true,
	})
}

func TestTrackerClockAndMood(t *testing.T) {
	start := time.Date(2026, 10, 22, 9, 0, 0, 0, time.Local)
	tr := NewTracker()

	res := observe(tr, start, 0, true)
	if res.Snapshot.State != StateTracking || len(res.Events) != 1 || res.Events[0].Kind != EventTrackingStarted {
		t.Fatalf("首次活动应进入 tracking 并发出 started，得到 %+v / %+v", res.Snapshot, res.Events)
	}
	if res.Snapshot.ContinuousMinutes != 0 {
		t.Errorf("刚进入 tracking 不应已有久坐分钟，得到 %d", res.Snapshot.ContinuousMinutes)
	}

	res = observe(tr, start.Add(10*time.Minute), 0, true)
	if res.Snapshot.ContinuousMinutes != 10 || res.Snapshot.Mood != MoodEnergetic {
		t.Fatalf("10 分钟应精神且累计 10 分钟，得到 %+v", res.Snapshot)
	}
	res = observe(tr, start.Add(46*time.Minute), 0, true)
	if res.Snapshot.Mood != MoodConcerned {
		t.Fatalf("46 分钟应进入担心，得到 %q", res.Snapshot.Mood)
	}
	res = observe(tr, start.Add(91*time.Minute), 0, true)
	if res.Snapshot.Mood != MoodDroopy {
		t.Fatalf("91 分钟应芽耷拉，得到 %q", res.Snapshot.Mood)
	}

	// 长空闲：前 5 分钟仍算活跃，之后暂停；不立即清零。
	res = observe(tr, start.Add(101*time.Minute), 10*time.Minute, true)
	if res.Snapshot.State != StateTracking {
		t.Fatalf("10 分钟空闲应保持 tracking，得到 %q", res.Snapshot.State)
	}
	if res.Snapshot.ContinuousMinutes < 90 || res.Snapshot.ContinuousMinutes > 100 {
		t.Errorf("10 分钟空闲应只累计到 5 分钟之后暂停，得到 %d", res.Snapshot.ContinuousMinutes)
	}

	// 15 分钟空闲：真实休息，清零并记一条空闲休息。
	res = observe(tr, start.Add(106*time.Minute), 15*time.Minute, true)
	if res.Snapshot.State != StateIdle || res.Snapshot.ContinuousMinutes != 0 {
		t.Fatalf("15 分钟空闲应清零，得到 %+v", res.Snapshot)
	}
	assertEvent(t, res.Events, EventTrackingEnded)
	assertEvent(t, res.Events, EventBreakStarted)
}

func TestTrackerActiveBreakAndWelcome(t *testing.T) {
	start := time.Date(2026, 10, 22, 9, 0, 0, 0, time.Local)
	tr := NewTracker()
	observe(tr, start, 0, true)
	observe(tr, start.Add(61*time.Minute), 0, true)

	res := tr.StartBreak(start.Add(61 * time.Minute))
	if res.Snapshot.State != StateBreakWait {
		t.Fatalf("起来走走应进入 break_wait，得到 %q", res.Snapshot.State)
	}
	assertEvent(t, res.Events, EventTrackingEnded)
	assertEvent(t, res.Events, EventBreakStarted)

	// 用户真的离开了：空闲 ≥30 秒。
	observe(tr, start.Add(61*time.Minute+40*time.Second), 40*time.Second, true)
	// 回来：空闲低于 5 秒。
	res = observe(tr, start.Add(64*time.Minute), 2*time.Second, true)
	if res.Snapshot.State != StateTracking {
		t.Fatalf("回来应重新 tracking，得到 %q", res.Snapshot.State)
	}
	ended := assertEvent(t, res.Events, EventBreakEnded)
	if ended.BreakKind != BreakKindActive || ended.Duration < 2*time.Minute {
		t.Fatalf("主动休息时长/类型不对: %+v", ended)
	}
	if ended.Text == "" {
		t.Error("回来时应带欢迎文案")
	}
}

func TestTrackerActiveBreakTimeoutWithoutLeaving(t *testing.T) {
	start := time.Date(2026, 10, 22, 9, 0, 0, 0, time.Local)
	tr := NewTracker()
	observe(tr, start, 0, true)
	tr.StartBreak(start.Add(61 * time.Minute))

	observe(tr, start.Add(62*time.Minute), time.Second, true)
	res := observe(tr, start.Add(64*time.Minute), time.Second, true)
	if res.Snapshot.State != StateTracking {
		t.Fatalf("未离开超时应回到 tracking，得到 %q", res.Snapshot.State)
	}
	ended := assertEvent(t, res.Events, EventBreakEnded)
	if ended.Text == "" {
		t.Error("超时也应有轻量确认，而不是永远等待")
	}
}

func TestTrackerLockBreak(t *testing.T) {
	start := time.Date(2026, 10, 22, 9, 0, 0, 0, time.Local)
	tr := NewTracker()
	observe(tr, start, 0, true)
	observe(tr, start.Add(31*time.Minute), 0, true)
	lockAt := start.Add(31 * time.Minute)

	res := tr.Observe(TrackInput{
		Now: lockAt, Idle: 0, Locked: true, LockedAt: lockAt, Active: true, Supported: true,
	})
	if res.Snapshot.State != StateIdle {
		t.Fatalf("锁屏应结束 tracking，得到 %q", res.Snapshot.State)
	}
	assertEvent(t, res.Events, EventTrackingEnded)
	assertEvent(t, res.Events, EventBreakStarted)

	res = tr.Observe(TrackInput{
		Now: lockAt.Add(6 * time.Minute), Idle: 0, Locked: false, Active: true, Supported: true,
	})
	if res.Snapshot.State != StateTracking {
		t.Fatalf("解锁后应重新 tracking，得到 %q", res.Snapshot.State)
	}
	ended := assertEvent(t, res.Events, EventBreakEnded)
	if ended.Duration < 5*time.Minute {
		t.Errorf("锁屏休息时长 = %v，期望约 6 分钟", ended.Duration)
	}
}

func TestTrackerFeatureOffAndUnsupported(t *testing.T) {
	start := time.Date(2026, 10, 22, 9, 0, 0, 0, time.Local)
	tr := NewTracker()
	observe(tr, start, 0, true)

	res := observe(tr, start.Add(time.Minute), 0, false)
	if res.Snapshot.State != StateIdle {
		t.Fatalf("场景失效应回到 idle，得到 %q", res.Snapshot.State)
	}
	assertEvent(t, res.Events, EventTrackingEnded)

	observe(tr, start.Add(2*time.Minute), 0, true)
	res = tr.Observe(TrackInput{Now: start.Add(3 * time.Minute), Supported: false, Active: true})
	if res.Snapshot.State != StateUnsupported {
		t.Fatalf("平台不支持应进入 unsupported，得到 %q", res.Snapshot.State)
	}
	assertEvent(t, res.Events, EventTrackingEnded)
}

func TestTrackerMutedStillCounts(t *testing.T) {
	start := time.Date(2026, 10, 22, 9, 0, 0, 0, time.Local)
	tr := NewTracker()
	res := tr.Observe(TrackInput{
		Now: start, Active: true, Muted: true, Supported: true,
	})
	if res.Snapshot.State != StateMuted || res.Snapshot.Mood != MoodEnergetic {
		t.Fatalf("静音当天应保持 muted 且情绪中性，得到 %+v", res.Snapshot)
	}
	res = tr.Observe(TrackInput{
		Now: start.Add(10 * time.Minute), Active: true, Muted: true, Supported: true,
	})
	if res.Snapshot.ContinuousMinutes != 10 {
		t.Errorf("静音只影响发言/情绪，不应停止久坐累计，得到 %d", res.Snapshot.ContinuousMinutes)
	}
}

func TestTrackerReset(t *testing.T) {
	start := time.Date(2026, 10, 22, 9, 0, 0, 0, time.Local)
	tr := NewTracker()
	observe(tr, start, 0, true)
	tr.StartBreak(start.Add(61 * time.Minute))
	tr.Reset()
	snap := tr.Snapshot()
	if snap.State != StateIdle || snap.Mood != MoodEnergetic || snap.ContinuousMinutes != 0 {
		t.Fatalf("Reset 后应回到 idle/精神/0 分钟，得到 %+v", snap)
	}
	if tr.ShouldSkipStandMove(1, start.Unix(), start) {
		t.Error("Reset 后不应再保留跳过窗口")
	}
	res := observe(tr, start.Add(70*time.Minute), 0, true)
	if res.Snapshot.State != StateTracking {
		t.Errorf("Reset 后仍应能重新 tracking，得到 %q", res.Snapshot.State)
	}
}

func TestTrackerSkipWindowAfterActiveBreak(t *testing.T) {
	start := time.Date(2026, 10, 22, 9, 0, 0, 0, time.Local)
	tr := NewTracker()
	observe(tr, start, 0, true)
	tr.StartBreak(start.Add(61 * time.Minute))

	if !tr.ShouldSkipStandMove(1, start.Add(62*time.Minute).Unix(), start.Add(62*time.Minute)) {
		t.Error("主动休息后 1 分钟内的 stand/move 应跳过")
	}
	if tr.ShouldSkipStandMove(1, start.Add(72*time.Minute).Unix(), start.Add(72*time.Minute)) {
		t.Error("主动休息后 11 分钟不应再跳过")
	}
}

func TestTrackerSkipWindowAfterIdleReturn(t *testing.T) {
	start := time.Date(2026, 10, 22, 9, 0, 0, 0, time.Local)
	tr := NewTracker()
	observe(tr, start, 0, true)
	observe(tr, start.Add(61*time.Minute), 16*time.Minute, true) // 空闲休息
	returnAt := start.Add(62 * time.Minute)
	observe(tr, returnAt, 0, true) // 回到输入

	if !tr.ShouldSkipStandMove(1, returnAt.Add(4*time.Minute).Unix(), returnAt.Add(4*time.Minute)) {
		t.Error("空闲休息回来 4 分钟内应跳过")
	}
	if tr.ShouldSkipStandMove(1, returnAt.Add(11*time.Minute).Unix(), returnAt.Add(11*time.Minute)) {
		t.Error("空闲休息回来 11 分钟后不应再跳过")
	}
}

func TestTrackerSkipWindowAfterLock(t *testing.T) {
	start := time.Date(2026, 10, 22, 9, 0, 0, 0, time.Local)
	tr := NewTracker()
	observe(tr, start, 0, true)
	lockAt := start.Add(31 * time.Minute)
	tr.Observe(TrackInput{Now: lockAt, Locked: true, LockedAt: lockAt, Active: true, Supported: true})
	unlockAt := lockAt.Add(6 * time.Minute)
	tr.Observe(TrackInput{Now: unlockAt, Locked: false, Active: true, Supported: true})

	if !tr.ShouldSkipStandMove(1, unlockAt.Add(3*time.Minute).Unix(), unlockAt.Add(3*time.Minute)) {
		t.Error("解锁后 3 分钟内应跳过")
	}
	if tr.ShouldSkipStandMove(1, unlockAt.Add(11*time.Minute).Unix(), unlockAt.Add(11*time.Minute)) {
		t.Error("解锁 11 分钟后不应再跳过")
	}
}

func assertEvent(t *testing.T, events []Event, kind EventKind) Event {
	t.Helper()
	for _, e := range events {
		if e.Kind == kind {
			return e
		}
	}
	t.Fatalf("事件列表里没有 %s: %+v", kind, events)
	return Event{}
}
