package sit

import "time"

// 软提醒策略常量（首版）。
const (
	// NudgeGapFromPlan 临近 stand/move 触发点时让正式提醒承担，不再冒泡。
	NudgeGapFromPlan = 15 * time.Minute
	// NudgeRepeatAfter 忽略首条后的第二次最小间隔（与阈值相加取较晚者）。
	NudgeRepeatAfter = 30 * time.Minute
	// ProactiveGap 两次主动发言的最小间隔；用户点「再坐 10 分钟」的追问例外。
	ProactiveGap = 30 * time.Minute
	// ProactiveBudget 每天主动发言总预算（软提醒 + 独立记忆气泡）。
	ProactiveBudget = 5
	// MaxNudgesPerSession 单个久坐会话最多软提醒次数。
	MaxNudgesPerSession = 2
	// SnoozeDelay 「再坐 10 分钟」的延后时长。
	SnoozeDelay = 10 * time.Minute
	// SkipWindow 真实休息后到点的 stand/move 静默跳过窗口。
	SkipWindow = 10 * time.Minute
	// LockCreditMin 锁屏自动抵扣的最短锁屏时长。
	LockCreditMin = 5 * time.Minute
	// LockSitMin 锁屏自动抵扣要求锁屏前已经连续久坐的最短时长。
	LockSitMin = 30 * time.Minute
)

// ShouldCreditLock 判断一次锁屏是否满足自动抵扣门槛：
// 锁屏 ≥5 分钟且锁屏开始前本次久坐 ≥30 分钟。
func ShouldCreditLock(lockDuration, sitActive time.Duration) bool {
	return lockDuration >= LockCreditMin && sitActive >= LockSitMin
}

// NudgeInput 是软提醒决策所需的全部外部事实。
type NudgeInput struct {
	Now              time.Time
	Continuous       time.Duration
	ThresholdMinutes int
	SessionStartedAt time.Time

	// NextStandMoveAt 当前场景下一条 stand/move 触发点（Unix 秒）；0 表示今天没有。
	NextStandMoveAt int64

	SessionNudges  int
	DailyProactive int
	// LastProactiveAt 今天最近一次已确认展示的软提醒/记忆气泡（Unix 秒）。
	LastProactiveAt int64
	// FirstNudgeAckAt 本会话首条软提醒被确认展示的时间（Unix 秒）。
	FirstNudgeAckAt int64
	// SnoozeUntil 用户点「再坐 10 分钟」后的下次允许时间（Unix 秒）；0 表示无。
	SnoozeUntil int64

	// FocusActive 表示当前处于专注会话；专注期间不打断，等切出/停顿后再评估。
	FocusActive bool

	// Eligible 由运行层算好：场景/时段/暂停/静默/待处理/休息/输入停顿/宠物可见。
	Eligible bool
}

// NudgeDecision 是策略输出；Reason 只用于日志与测试。
type NudgeDecision struct {
	Nudge  bool
	Reason string
}

// MemoryInput 是独立记忆气泡的决策事实；事实本身已由 SelectQuote 选出。
type MemoryInput struct {
	Now             time.Time
	NextStandMoveAt int64
	DailyProactive  int
	LastProactiveAt int64

	// Eligible 由运行层算好：自然停顿、专注/暂停/静默/待处理/休息都不冒泡。
	Eligible bool
	// HasQuote 表示确实存在一条值得说且 3 天内未重复的事实。
	HasQuote bool
}

// MemoryDecision 是独立记忆气泡是否开口的决定。
type MemoryDecision struct {
	Speak  bool
	Reason string
}

// MemoryWindow 只检查记忆气泡的时间窗与预算，不要求已有事实。
//
// 运行层先用它做廉价前置判断，避免为了“没有事实”每 5 秒查一次历史；
// DecideMemory 复用同一份判断，防止两套条件漂移。
func MemoryWindow(in MemoryInput) MemoryDecision {
	if !in.Eligible {
		return MemoryDecision{Reason: "not_eligible"}
	}
	if in.DailyProactive >= ProactiveBudget {
		return MemoryDecision{Reason: "daily_budget"}
	}
	if in.LastProactiveAt > 0 && in.Now.Sub(time.Unix(in.LastProactiveAt, 0)) < ProactiveGap {
		return MemoryDecision{Reason: "proactive_gap"}
	}
	if in.NextStandMoveAt > 0 {
		if time.Unix(in.NextStandMoveAt, 0).Sub(in.Now) < NudgeGapFromPlan {
			return MemoryDecision{Reason: "plan_too_close"}
		}
	}
	return MemoryDecision{Speak: true, Reason: "window_open"}
}

// DecideMemory 决定此刻是否生成一条独立记忆气泡。
//
// 记忆与软提醒共享每日 5 次预算与 30 分钟间隔，并且不抢 15 分钟内的
// stand/move 正式提醒；没有值得说的事实就保持沉默。
func DecideMemory(in MemoryInput) MemoryDecision {
	if window := MemoryWindow(in); !window.Speak {
		return window
	}
	if !in.HasQuote {
		return MemoryDecision{Reason: "no_fact"}
	}
	return MemoryDecision{Speak: true, Reason: "due"}
}

// DecideNudge 决定此刻是否生成一条久坐软提醒。
func DecideNudge(in NudgeInput) NudgeDecision {
	if !in.Eligible {
		return NudgeDecision{Reason: "not_eligible"}
	}
	if in.FocusActive {
		return NudgeDecision{Reason: "focus"}
	}
	threshold := time.Duration(ClampNudgeThreshold(in.ThresholdMinutes)) * time.Minute
	if in.Continuous < threshold {
		return NudgeDecision{Reason: "below_threshold"}
	}
	if in.SessionNudges >= MaxNudgesPerSession {
		return NudgeDecision{Reason: "session_limit"}
	}
	if in.DailyProactive >= ProactiveBudget {
		return NudgeDecision{Reason: "daily_budget"}
	}
	if in.NextStandMoveAt > 0 {
		d := time.Unix(in.NextStandMoveAt, 0).Sub(in.Now)
		if d < NudgeGapFromPlan {
			return NudgeDecision{Reason: "plan_too_close"}
		}
	}

	// 用户主动要求「再坐 10 分钟」：到点后绕过 30 分钟主动发言间隔，
	// 但仍受会话次数与每日预算约束。
	nowUnix := in.Now.Unix()
	if in.SnoozeUntil > 0 {
		if nowUnix < in.SnoozeUntil {
			return NudgeDecision{Reason: "snoozed"}
		}
		if in.SessionNudges >= 1 && in.SessionNudges < MaxNudgesPerSession {
			return NudgeDecision{Nudge: true, Reason: "snooze_due"}
		}
	}

	if in.LastProactiveAt > 0 && in.Now.Sub(time.Unix(in.LastProactiveAt, 0)) < ProactiveGap {
		return NudgeDecision{Reason: "proactive_gap"}
	}

	if in.SessionNudges == 0 {
		return NudgeDecision{Nudge: true, Reason: "due"}
	}
	if in.FirstNudgeAckAt <= 0 {
		return NudgeDecision{Reason: "first_not_acked"}
	}
	secondFromStart := in.SessionStartedAt.Add(threshold + NudgeRepeatAfter)
	secondFromAck := time.Unix(in.FirstNudgeAckAt, 0).Add(NudgeRepeatAfter)
	earliest := secondFromStart
	if secondFromAck.After(earliest) {
		earliest = secondFromAck
	}
	if in.Now.Before(earliest) {
		return NudgeDecision{Reason: "wait_second"}
	}
	return NudgeDecision{Nudge: true, Reason: "second"}
}
