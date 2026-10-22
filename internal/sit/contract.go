// Package sit 是久坐感知的领域层：时钟/策略/记忆/类别与对外契约。
//
// 本包不 import Wails、gorm 或 config；平台读取由组合根注入，持久化由
// service 实现，模板纯本地。前端镜像见 frontend/src/lib/sit.ts。
package sit

// State 是久坐运行状态。
type State string

const (
	StateIdle        State = "idle"
	StateTracking    State = "tracking"
	StateBreakWait   State = "break_wait"
	StateMuted       State = "muted"
	StateUnsupported State = "unsupported"
)

// Mood 是宠物情绪层，不参与计时。
type Mood string

const (
	MoodEnergetic Mood = "energetic"
	MoodConcerned Mood = "concerned"
	MoodDroopy    Mood = "droopy"
)

// Tone 是宠物语气档位；默认毒舌（ADR 0011）。
type Tone string

const (
	ToneSassy  Tone = "sassy"
	ToneLively Tone = "lively"
	ToneGentle Tone = "gentle"
)

// DefaultTone 是默认语气（与 ADR 0011 一致）。
const DefaultTone = ToneSassy

// NormalizeTone 把任意输入归一到合法档位；未知值回落毒舌。
func NormalizeTone(v string) Tone {
	switch Tone(v) {
	case ToneSassy, ToneLively, ToneGentle:
		return Tone(v)
	default:
		return DefaultTone
	}
}

// Category 是行为类别。
type Category string

const (
	CategoryCoding        Category = "coding"
	CategoryDesign        Category = "design"
	CategoryBrowsing      Category = "browsing"
	CategoryCommunication Category = "communication"
	CategoryMedia         Category = "media"
	CategoryOther         Category = "other"
)

// AllCategories 返回全部合法类别（顺序即展示顺序）。
func AllCategories() []Category {
	return []Category{
		CategoryCoding, CategoryDesign, CategoryBrowsing,
		CategoryCommunication, CategoryMedia, CategoryOther,
	}
}

// ValidCategory 判断类别是否在固定集合内（用户纠正与持久化边界共用）。
func ValidCategory(c Category) bool {
	for _, candidate := range AllCategories() {
		if candidate == c {
			return true
		}
	}
	return false
}

// BubbleKind 是宠物气泡类型。
type BubbleKind string

const (
	BubbleNudge    BubbleKind = "nudge"
	BubbleMemory   BubbleKind = "memory"
	BubbleGreeting BubbleKind = "greeting"
)

// ActionID 是气泡动作标识；前端只认这些 ID，不在前端复制业务分支。
type ActionID string

const (
	ActionStartBreak ActionID = "start_break"
	ActionSnooze10   ActionID = "snooze_10"
	ActionMore       ActionID = "more"
	ActionDismiss    ActionID = "dismiss"
	ActionOpenApp    ActionID = "open_app"
)

// EndReason 是应用/久坐会话的结束原因。
//
// 应用会话用 app_switch / idle / lock / feature_off / abandoned；
// 久坐会话用 active_break / lock_break / idle_break / feature_off / abandoned。
type EndReason string

const (
	EndReasonAppSwitch   EndReason = "app_switch"
	EndReasonIdle        EndReason = "idle"
	EndReasonLock        EndReason = "lock"
	EndReasonActiveBreak EndReason = "active_break"
	EndReasonLockBreak   EndReason = "lock_break"
	EndReasonIdleBreak   EndReason = "idle_break"
	EndReasonFeatureOff  EndReason = "feature_off"
	EndReasonAbandoned   EndReason = "abandoned"
	EndReasonUnknown     EndReason = "unknown"
)

// BreakKind 是休息类型。
type BreakKind string

const (
	BreakKindActive BreakKind = "active"
	BreakKindLock   BreakKind = "lock"
	BreakKindIdle   BreakKind = "idle"
)

// ProfileStatus 是应用画像的读取状态（设置/久坐页的可见降级）。
type ProfileStatus string

const (
	ProfileStatusOK          ProfileStatus = "ok"
	ProfileStatusUnsupported ProfileStatus = "unsupported"
	ProfileStatusReadError   ProfileStatus = "read_error"
	ProfileStatusNoData      ProfileStatus = "no_data"
)

// 久坐软提醒阈值档位与默认值。
const (
	DefaultNudgeThresholdMinutes = 60
)

// NudgeThresholdOptions 是设置页可选的阈值。
func NudgeThresholdOptions() []int { return []int{45, 60, 90} }

// ClampNudgeThreshold 把任意输入夹到 45/60/90 中最接近的一档；
// 非正值回落默认 60（手改坏了也不让策略拿到 0）。
func ClampNudgeThreshold(v int) int {
	if v <= 0 {
		return DefaultNudgeThresholdMinutes
	}
	best := NudgeThresholdOptions()[0]
	bestDiff := abs(v - best)
	for _, o := range NudgeThresholdOptions()[1:] {
		if d := abs(v - o); d < bestDiff {
			best, bestDiff = o, d
		}
	}
	return best
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// Focus 是当前专注会话的可见信息；只含类别与分钟，不含应用名。
type Focus struct {
	Category Category `json:"category"`
	Minutes  int      `json:"minutes"`
}

// Bubble 是宠物待展示气泡。
type Bubble struct {
	ID        string     `json:"id"`
	Kind      BubbleKind `json:"kind"`
	Title     string     `json:"title"`
	Text      string     `json:"text"`
	Actions   []ActionID `json:"actions"`
	ExpiresAt int64      `json:"expires_at"`
}

// Snapshot 是宠物窗口轮询的久坐快照。
//
// 【契约红线】这里不允许出现应用名、应用标识或窗口标题。具体应用名只在
// 主窗「久坐」页通过独立查询可见。
type Snapshot struct {
	State                 State   `json:"state"`
	Mood                  Mood    `json:"mood"`
	Tone                  Tone    `json:"tone"`
	ContinuousMinutes     int     `json:"continuous_minutes"`
	TodaySittingMinutes   int     `json:"today_sitting_minutes"`
	LongestSessionMinutes int     `json:"longest_session_minutes"`
	ProfileEnabled        bool    `json:"profile_enabled"`
	ProfileSupported      bool    `json:"profile_supported"`
	PlatformSupported     bool    `json:"platform_supported"`
	MutedToday            bool    `json:"muted_today"`
	BudgetRemaining       int     `json:"budget_remaining"`
	Focus                 *Focus  `json:"focus,omitempty"`
	Bubble                *Bubble `json:"bubble,omitempty"`
}

// SitActionResult 是气泡动作/设置动作的可见回执。
type SitActionResult struct {
	OK            bool   `json:"ok"`
	Message       string `json:"message"`
	Credited      bool   `json:"credited"`
	CreditedTitle string `json:"credited_title"`
}
