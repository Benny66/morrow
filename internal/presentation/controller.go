// Package presentation 维护演示模式的临时状态。
//
// 它不依赖 Wails，也不依赖 pause：控制器只回答「现在是否在演示、来源是什么、
// 什么时候进入/退出、是否应该建议进入」，由 App 把状态桥接给暂停聚合、托盘与
// 窗口显隐。这样状态机可以在没有窗口/事件循环的环境里被完整测试。
package presentation

import (
	"sync"
	"time"
)

// Source 是演示模式的进入来源。
type Source string

const (
	// SourceManual 用户从菜单栏或快捷键主动进入。
	SourceManual Source = "manual"
	// SourceSuggested 用户在会议/全屏建议下确认进入。
	SourceSuggested Source = "suggested"
)

// Valid 判断来源枚举是否合法。
func (s Source) Valid() bool {
	return s == SourceManual || s == SourceSuggested
}

// Trigger 是触发演示建议的外部信号。
//
// 只关心「会议」与「全屏」两类：空闲/锁屏/专注/日历不构成「你正在共享屏幕」的
// 可信理由，在这些状态下建议进入演示模式反而像误报。
type Trigger string

const (
	// TriggerNone 没有会议/全屏触发。
	TriggerNone Trigger = ""
	// TriggerMeeting 前台为会议应用。
	TriggerMeeting Trigger = "meeting"
	// TriggerFullscreen 前台为全屏应用。
	TriggerFullscreen Trigger = "fullscreen"
)

// Active 返回触发是否属于「会议/全屏」。
func (t Trigger) Active() bool {
	return t == TriggerMeeting || t == TriggerFullscreen
}

const (
	// SuggestionDelay 触发持续多久后才建议进入演示模式。
	SuggestionDelay = 5 * time.Second
	// AutoExitDelay 建议进入的演示模式在触发消失后多久自动退出。
	AutoExitDelay = 30 * time.Second
)

// Snapshot 是控制器的一次性只读快照。
type Snapshot struct {
	// Active 是否处于演示模式。
	Active bool
	// Source 进入来源；未激活时为空串。
	Source Source
	// Since 进入时间；未激活时为零值。
	Since time.Time
	// ExitedAt 最近一次退出时间；从未退出时为零值。
	ExitedAt time.Time
	// SuggestionVisible 是否应显示「进入演示模式」的建议。
	SuggestionVisible bool
}

// Controller 是演示模式的唯一状态机。
type Controller struct {
	mu       sync.Mutex
	active   bool
	source   Source
	since    time.Time
	exitedAt time.Time

	// triggerSince 是当前会议/全屏触发会话的开始时刻；TriggerNone 时归零。
	triggerSince time.Time
	// autoExitAt 是建议进入的演示模式计划自动退出的时刻；零值表示未计划。
	autoExitAt time.Time
	// suggestionVisible 是当前是否满足建议条件（待处理/休息的 UI 优先级由前端决定）。
	suggestionVisible bool
	// suggestedThisSession 表示本次触发会话已经建议过、用户已处理或已手动进出；
	// 触发消失后重置。
	suggestedThisSession bool
}

// NewController 创建一个关闭状态的控制器。
func NewController() *Controller {
	return &Controller{}
}

// Enter 进入演示模式。已进入时幂等，保留首次来源与开始时间。
//
// 进入即视为「本次触发会话已经做过选择」：同一场会议里退出后不再重复建议。
func (c *Controller) Enter(source Source, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active {
		return
	}
	c.active = true
	c.source = source
	c.since = now
	c.suggestionVisible = false
	c.suggestedThisSession = true
	c.autoExitAt = time.Time{}
}

// Exit 退出演示模式。未进入时幂等，不改变最近退出时间。
//
// 不重置 suggestedThisSession：手动退出后若会议仍在进行，不应立刻再建议。
// 该标记会在触发消失后的 Observe 里重置。
func (c *Controller) Exit(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.active {
		return
	}
	c.active = false
	c.source = ""
	c.since = time.Time{}
	c.exitedAt = now
	c.suggestionVisible = false
	c.autoExitAt = time.Time{}
}

// DismissSuggestion 记录用户忽略了本次建议；同一触发会话不再建议。
func (c *Controller) DismissSuggestion() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.suggestedThisSession = true
	c.suggestionVisible = false
}

// Observe 推进一次外部触发观察，返回是否发生了「建议进入的演示模式自动退出」。
//
// 建议进入的演示模式在触发消失后等待 AutoExitDelay 再退出；期间触发恢复则取消。
// 手动进入的演示模式不自动退出。
func (c *Controller) Observe(trigger Trigger, suggestEnabled bool, now time.Time) (autoExited bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if trigger.Active() {
		if c.triggerSince.IsZero() {
			c.triggerSince = now
			// 已经在演示中（手动或建议进入）说明用户本次会话已做过选择；
			// 不要因为会议刚开始就把抑制标记重置，否则手动退出后会被立刻再建议。
			if !c.active {
				c.suggestedThisSession = false
			}
		}
		c.suggestionVisible = !c.active && suggestEnabled &&
			!c.suggestedThisSession && now.Sub(c.triggerSince) >= SuggestionDelay
		// 触发恢复：取消待自动退出。
		c.autoExitAt = time.Time{}
		return false
	}

	// 触发消失。
	c.triggerSince = time.Time{}
	c.suggestionVisible = false
	if !c.active {
		c.suggestedThisSession = false
		c.autoExitAt = time.Time{}
		return false
	}
	if c.source != SourceSuggested {
		c.autoExitAt = time.Time{}
		return false
	}
	if c.autoExitAt.IsZero() {
		c.autoExitAt = now.Add(AutoExitDelay)
		return false
	}
	if now.Before(c.autoExitAt) {
		return false
	}

	c.active = false
	c.source = ""
	c.since = time.Time{}
	c.exitedAt = now
	c.autoExitAt = time.Time{}
	c.suggestedThisSession = false
	return true
}

// Snapshot 返回当前状态的只读快照。
func (c *Controller) Snapshot() Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return Snapshot{
		Active:            c.active,
		Source:            c.source,
		Since:             c.since,
		ExitedAt:          c.exitedAt,
		SuggestionVisible: c.suggestionVisible,
	}
}
