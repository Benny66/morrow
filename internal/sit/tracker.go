package sit

import (
	"sync"
	"time"
)

// 久坐时钟的阈值（分级空闲）。
const (
	// ContinueIdle 以内的空闲继续计时。
	ContinueIdle = 5 * time.Minute
	// ResetIdle 及以上的空闲视为真实休息并清零。
	ResetIdle = 15 * time.Minute
	// breakLeaveIdle 主动休息后，空闲达到它才认为用户真的离开了。
	breakLeaveIdle = 30 * time.Second
	// breakWaitTimeout 用户点了起来走走但一直没离开时，最多等这么久。
	breakWaitTimeout = 2 * time.Minute
	// inputResumeIdle 低于它表示用户又回到输入状态。
	inputResumeIdle = 5 * time.Second
)

// EventKind 是 tracker 向运行层发出的事件类型。
type EventKind string

const (
	EventTrackingStarted EventKind = "tracking_started"
	EventTrackingEnded   EventKind = "tracking_ended"
	EventBreakStarted    EventKind = "break_started"
	EventBreakEnded      EventKind = "break_ended"
)

// Event 是 tracker 的一次状态变化；运行层据此落库/生成问候。
type Event struct {
	Kind EventKind
	At   time.Time
	// EndReason 仅 TrackingEnded 有效。
	EndReason EndReason
	// BreakKind 仅 BreakStarted / BreakEnded 有效。
	BreakKind BreakKind
	// ActiveSeconds 仅 TrackingEnded 有效。
	ActiveSeconds time.Duration
	// Duration 仅 BreakStarted（空闲休息）/ BreakEnded 有效。
	Duration time.Duration
	// Text 仅 BreakEnded 有效，给宠物气泡的欢迎/确认文案。
	Text string
}

// TrackInput 是每次 5 秒 tick 的输入；全部来自现有 pause/config/service。
type TrackInput struct {
	Now       time.Time
	Idle      time.Duration
	Locked    bool
	LockedAt  time.Time
	Active    bool
	Muted     bool
	Supported bool
}

// TickResult 是 tracker 一轮输出。
type TickResult struct {
	Snapshot         Snapshot
	Events           []Event
	SessionStartedAt time.Time
	// ActiveSeconds 当前会话累计的精确活跃秒数（Snapshot 只有分钟）。
	ActiveSeconds time.Duration
}

// Tracker 是久坐时钟/情绪/休息状态机。
//
// 纯逻辑：不碰数据库、不读平台；调用方把 idle/lock/场景条件喂进来。
// 所有导出方法持锁，tick 与绑定动作可以并发调用。
type Tracker struct {
	mu sync.Mutex

	state     State
	mood      Mood
	active    time.Duration
	session   time.Time
	supported bool
	muted     bool

	lastObserved time.Time
	lastIdle     time.Duration

	lastLocked  bool
	lockStarted time.Time

	breakWait    bool
	breakStarted time.Time
	breakSawIdle bool
	breakKind    BreakKind

	// lastBreakAt 是最近一次真实休息结束（或主动休息开始）的时刻；
	// 提醒引擎据此判断「10 分钟内到点的 stand/move 是否跳过」。
	lastBreakAt      time.Time
	idleBreakPending bool

	// 专注会话运行态：只保存应用标识用于判定切回；Snapshot 只输出类别与分钟。
	focusAppID     string
	focusCategory  Category
	focusAccum     time.Duration
	focusActive    bool
	focusLastSeen  time.Time
	focusAwaySince time.Time
}

// NewTracker 构造初始为 idle 的 tracker。
func NewTracker() *Tracker {
	return &Tracker{state: StateIdle, mood: MoodEnergetic}
}

// Reset 清空运行态（清空数据后调用）；平台能力保留。
func (t *Tracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.state = StateIdle
	t.mood = MoodEnergetic
	t.active = 0
	t.session = time.Time{}
	t.muted = false
	t.lastObserved = time.Time{}
	t.lastIdle = 0
	t.lastLocked = false
	t.lockStarted = time.Time{}
	t.breakWait = false
	t.breakStarted = time.Time{}
	t.breakSawIdle = false
	t.breakKind = ""
	t.lastBreakAt = time.Time{}
	t.idleBreakPending = false
	t.clearFocusLocked()
}

// Snapshot 返回当前状态（不推进时钟）；运行层补齐今日/预算/气泡。
func (t *Tracker) Snapshot() Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.snapshotLocked()
}

// Observe 推进一轮时钟。delta 用相邻 tick 的 now 计算，避免依赖真实 time.Now。
func (t *Tracker) Observe(in TrackInput) (res TickResult) {
	t.mu.Lock()
	defer t.mu.Unlock()
	defer func() {
		res.ActiveSeconds = t.active
		res.SessionStartedAt = t.session
	}()

	delta := time.Duration(0)
	if !t.lastObserved.IsZero() && in.Now.After(t.lastObserved) {
		delta = in.Now.Sub(t.lastObserved)
	}
	first := t.lastObserved.IsZero()

	// 平台不支持：收尾一切，进入 unsupported。
	if !in.Supported {
		t.supported = false
		if t.state == StateTracking || t.state == StateMuted || t.state == StateBreakWait {
			res.Events = append(res.Events, t.endTrackingLocked(in.Now, EndReasonFeatureOff)...)
		}
		t.breakWait = false
		t.state = StateUnsupported
		t.mood = MoodEnergetic
		t.active = 0
		t.session = time.Time{}
		t.lastObserved, t.lastIdle = in.Now, in.Idle
		res.Snapshot = t.snapshotLocked()
		return res
	}
	t.supported = true

	// 先处理锁屏：锁屏是真实休息，优先级高于普通输入。
	if in.Locked {
		if !t.lastLocked {
			lockAt := in.LockedAt
			if lockAt.IsZero() || lockAt.After(in.Now) {
				lockAt = in.Now
			}
			if t.breakWait {
				res.Events = append(res.Events, t.endBreakLocked(in.Now, BreakKindActive, t.breakTextLocked(in.Now, BreakKindActive))...)
				t.breakWait = false
			} else if t.state == StateTracking || t.state == StateMuted {
				res.Events = append(res.Events, t.endTrackingLocked(in.Now, EndReasonLockBreak)...)
			}
			// 只要之前有久坐会话/主动休息，锁屏本身就是一次真实休息；
			// 先前没有会话时不凭空记一条锁屏休息。
			if len(res.Events) > 0 {
				t.breakStarted = lockAt
				t.breakKind = BreakKindLock
				res.Events = append(res.Events, Event{
					Kind:      EventBreakStarted,
					At:        lockAt,
					BreakKind: BreakKindLock,
				})
			}
			t.lastLocked = true
			t.lockStarted = lockAt
			t.state = StateIdle
			t.mood = MoodEnergetic
			t.active = 0
			t.session = time.Time{}
			t.lastObserved, t.lastIdle = in.Now, in.Idle
			res.Snapshot = t.snapshotLocked()
			return res
		}
		// 持续锁屏：不推进久坐时钟。
		t.state = StateIdle
		t.mood = MoodEnergetic
		t.active = 0
		t.lastObserved, t.lastIdle = in.Now, in.Idle
		res.Snapshot = t.snapshotLocked()
		return res
	}
	if t.lastLocked {
		// 解锁：结束锁屏休息；若仍在有效场景内，本 tick 随后可以重新开始 tracking。
		if !t.breakStarted.IsZero() {
			dur := in.Now.Sub(t.breakStarted)
			if dur < 0 {
				dur = 0
			}
			res.Events = append(res.Events, Event{
				Kind:      EventBreakEnded,
				At:        in.Now,
				BreakKind: BreakKindLock,
				Duration:  dur,
			})
			t.lastBreakAt = in.Now
		}
		t.lastLocked = false
		t.lockStarted = time.Time{}
		t.breakStarted = time.Time{}
		t.breakKind = ""
	}

	// 主动休息等待中。
	if t.breakWait {
		if in.Idle >= breakLeaveIdle {
			t.breakSawIdle = true
		}
		leave := t.breakSawIdle && in.Idle < inputResumeIdle
		timeout := !t.breakSawIdle && in.Now.Sub(t.breakStarted) >= breakWaitTimeout
		if leave || timeout {
			text := t.breakTextLocked(in.Now, t.breakKind)
			if timeout {
				text = "好，先记下这次休息。"
			}
			res.Events = append(res.Events, t.endBreakLocked(in.Now, t.breakKind, text)...)
			t.breakWait = false
			t.breakStarted = time.Time{}
			t.breakKind = ""
			t.active = 0
			t.mood = MoodEnergetic
			if in.Active {
				res.Events = append(res.Events, t.startTrackingLocked(in, delta)...)
			} else {
				t.state = StateIdle
				t.session = time.Time{}
			}
			t.lastObserved, t.lastIdle = in.Now, in.Idle
			res.Snapshot = t.snapshotLocked()
			res.SessionStartedAt = t.session
			return res
		}
		t.state = StateBreakWait
		t.mood = MoodEnergetic
		t.active = 0
		t.lastObserved, t.lastIdle = in.Now, in.Idle
		res.Snapshot = t.snapshotLocked()
		return res
	}

	if !in.Active {
		if t.state == StateTracking || t.state == StateMuted {
			res.Events = append(res.Events, t.endTrackingLocked(in.Now, EndReasonFeatureOff)...)
		}
		t.state = StateIdle
		t.mood = MoodEnergetic
		t.active = 0
		t.session = time.Time{}
		t.lastObserved, t.lastIdle = in.Now, in.Idle
		res.Snapshot = t.snapshotLocked()
		return res
	}

	// 已跟踪：先处理长空闲清零，再按分级空闲累计。
	if (t.state == StateTracking || t.state == StateMuted) && in.Idle >= ResetIdle {
		res.Events = append(res.Events, t.endTrackingLocked(in.Now, EndReasonIdleBreak)...)
		t.active = 0
		t.state = StateIdle
		t.mood = MoodEnergetic
		t.session = time.Time{}
		t.idleBreakPending = true
		res.Events = append(res.Events, Event{
			Kind:      EventBreakStarted,
			At:        in.Now,
			BreakKind: BreakKindIdle,
			Duration:  in.Idle,
		})
		t.lastObserved, t.lastIdle = in.Now, in.Idle
		res.Snapshot = t.snapshotLocked()
		return res
	}

	if t.state != StateTracking && t.state != StateMuted {
		res.Events = append(res.Events, t.startTrackingLocked(in, delta)...)
	} else {
		t.active += activeIncrement(t.lastIdle, in.Idle, delta)
	}
	if in.Muted {
		t.muted = true
		t.state = StateMuted
		t.mood = MoodEnergetic
	} else {
		t.muted = false
		t.state = StateTracking
		t.mood = moodFor(t.active)
	}
	if first {
		t.active = 0
	}
	t.lastObserved, t.lastIdle = in.Now, in.Idle
	res.Snapshot = t.snapshotLocked()
	res.SessionStartedAt = t.session
	return res
}

// StartBreak 处理「起来走走」：结束当前久坐会话，进入等待回来。
func (t *Tracker) StartBreak(now time.Time) (res TickResult) {
	t.mu.Lock()
	defer t.mu.Unlock()
	defer func() {
		res.ActiveSeconds = t.active
		res.SessionStartedAt = t.session
	}()
	if t.state != StateTracking && t.state != StateMuted {
		res.Snapshot = t.snapshotLocked()
		return res
	}
	res.Events = append(res.Events, t.endTrackingLocked(now, EndReasonActiveBreak)...)
	t.breakWait = true
	t.breakStarted = now
	t.breakSawIdle = false
	t.breakKind = BreakKindActive
	t.state = StateBreakWait
	t.mood = MoodEnergetic
	t.active = 0
	t.session = time.Time{}
	t.lastBreakAt = now
	res.Events = append(res.Events, Event{
		Kind:      EventBreakStarted,
		At:        now,
		BreakKind: BreakKindActive,
	})
	res.Snapshot = t.snapshotLocked()
	return res
}

// ShouldSkipStandMove 实现 reminder.BreakProvider：真实休息后 10 分钟内的
// stand/move 触发点由引擎静默跳过。reminderID/fireAt 当前规则不区分。
func (t *Tracker) ShouldSkipStandMove(_ uint, _ int64, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.lastBreakAt.IsZero() {
		return false
	}
	d := now.Sub(t.lastBreakAt)
	return d >= 0 && d <= SkipWindow
}

func (t *Tracker) startTrackingLocked(in TrackInput, delta time.Duration) []Event {
	t.session = in.Now
	t.active = 0
	if t.idleBreakPending {
		// 空闲休息的「结束」是用户回来这一刻；跳过窗口从这里开始算。
		t.lastBreakAt = in.Now
		t.idleBreakPending = false
	}
	if in.Muted {
		t.muted = true
		t.state = StateMuted
	} else {
		t.muted = false
		t.state = StateTracking
	}
	t.mood = MoodEnergetic
	return []Event{{Kind: EventTrackingStarted, At: in.Now}}
}

func (t *Tracker) endTrackingLocked(now time.Time, reason EndReason) []Event {
	active := t.active
	t.state = StateIdle
	t.mood = MoodEnergetic
	t.session = time.Time{}
	t.active = 0
	// 久坐会话结束（真实休息/切场景/关功能）时专注也结束。
	t.clearFocusLocked()
	return []Event{{
		Kind:          EventTrackingEnded,
		At:            now,
		EndReason:     reason,
		ActiveSeconds: active,
	}}
}

func (t *Tracker) endBreakLocked(now time.Time, kind BreakKind, text string) []Event {
	dur := now.Sub(t.breakStarted)
	if dur < 0 {
		dur = 0
	}
	return []Event{{
		Kind:      EventBreakEnded,
		At:        now,
		BreakKind: kind,
		Duration:  dur,
		Text:      text,
	}}
}

func (t *Tracker) breakTextLocked(now time.Time, kind BreakKind) string {
	if kind != BreakKindActive {
		return ""
	}
	dur := now.Sub(t.breakStarted)
	if dur < 0 {
		dur = 0
	}
	mins := int(dur.Round(time.Minute) / time.Minute)
	if mins < 1 {
		return "回来啦。"
	}
	return "回来啦，这次休息了 " + itoa(mins) + " 分钟。"
}

func (t *Tracker) snapshotLocked() Snapshot {
	return Snapshot{
		State:             t.state,
		Mood:              t.mood,
		Tone:              DefaultTone,
		ContinuousMinutes: int(t.active / time.Minute),
		ProfileEnabled:    false,
		ProfileSupported:  false,
		PlatformSupported: t.supported,
		Focus:             t.focusSnapshotLocked(),
	}
}

func activeIncrement(prevIdle, idle, delta time.Duration) time.Duration {
	if delta <= 0 {
		return 0
	}
	if prevIdle >= ContinueIdle {
		return 0
	}
	if idle <= ContinueIdle {
		return delta
	}
	remaining := ContinueIdle - prevIdle
	if remaining < 0 {
		remaining = 0
	}
	if delta < remaining {
		return delta
	}
	return remaining
}

func moodFor(active time.Duration) Mood {
	switch {
	case active >= MoodDroopyAfter:
		return MoodDroopy
	case active >= MoodConcernedAfter:
		return MoodConcerned
	default:
		return MoodEnergetic
	}
}

// MoodConcernedAfter / MoodDroopyAfter 是情绪阈值（默认阈值 60 时的 45/90）。
const (
	MoodConcernedAfter = 45 * time.Minute
	MoodDroopyAfter    = 90 * time.Minute
)

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
