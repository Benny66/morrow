package reminder

import (
	"log/slog"
	"time"
)

// ActivityProvider 返回系统连续空闲时长。
//
// 第二个返回值表示平台是否支持；不支持时顺延功能关闭，而不是把 0 当成
// 「用户一直在输入」——那会让提醒永远延后。
type ActivityProvider interface {
	IdleDuration() (time.Duration, bool)
}

// naturalPauseActiveWindow 是「用户正在输入」的判定窗口。
//
// 5 秒内有过键鼠输入 = 正在输入；与暂停的空闲阈值（分钟级）不是同一个概念：
// 前者只决定「是否顺延几十秒」，后者决定「是否整轮不投递」。
const naturalPauseActiveWindow = 5 * time.Second

// deliveryMode 是 natural pause 对一条投递的裁决。
type deliveryMode int

const (
	// deliveryNormal 正常投递（可能弹全屏、可能进休息模式）。
	deliveryNormal deliveryMode = iota
	// deliveryDeferMore 本轮不投递，继续顺延。
	deliveryDeferMore
	// deliveryQuiet 到达上限、用户仍在输入：安静投递（不弹全屏、不进休息模式）。
	deliveryQuiet
)

// deferralScope 区分周期提醒与推迟补发两条顺延表。
type deferralScope string

const (
	scopeGrid   deferralScope = "grid"
	scopeSnooze deferralScope = "snooze"
)

// deferral 记录一次顺延的开始时间与当时的上限。
// max 在开始时固定：中途改设置不改变已经开始的这一轮。
type deferral struct {
	startedAt time.Time
	max       time.Duration
}

// naturalPauseMax 返回当前设置下的顺延上限；0 表示关闭。
func naturalPauseMax(settings Settings) time.Duration {
	if !settings.NaturalPauseEnabled || settings.NaturalPauseMaxSeconds <= 0 {
		return 0
	}
	return time.Duration(settings.NaturalPauseMaxSeconds) * time.Second
}

// activityActive 返回用户是否正在输入。
func (e *Engine) activityActive() bool {
	if e.activity == nil {
		return false
	}
	idle, supported := e.activity.IdleDuration()
	return supported && idle < naturalPauseActiveWindow
}

// deferralMode 裁决一条投递本轮该正常投递、继续顺延还是安静投递。
//
// 按【提醒项/触发记录】而不是按单个时间点记录：用户持续输入时，同一提醒
// 积压的多个点应作为一个整体顺延，恢复后仍走「只送达最近 1 条」的既有策略。
func (e *Engine) deferralMode(scope deferralScope, key uint, now time.Time, settings Settings, hasDue bool) deliveryMode {
	max := naturalPauseMax(settings)
	if max <= 0 || !hasDue {
		e.finishDeferral(scope, key, now, "disabled_or_no_due")
		return deliveryNormal
	}
	if !e.activityActive() {
		e.finishDeferral(scope, key, now, "activity_stopped")
		return deliveryNormal
	}

	e.mu.Lock()
	table := e.deferralTableLocked(scope)
	d, exists := table[key]
	if !exists {
		table[key] = deferral{startedAt: now, max: max}
		e.mu.Unlock()
		slog.Info("自然停顿顺延开始", "scope", scope, "id", key, "max", max)
		return deliveryDeferMore
	}
	if now.Sub(d.startedAt) < d.max {
		e.mu.Unlock()
		return deliveryDeferMore
	}
	delay := now.Sub(d.startedAt)
	delete(table, key)
	e.mu.Unlock()
	slog.Info("自然停顿顺延到上限，转安静投递",
		"scope", scope, "id", key, "delay", delay, "max", d.max, "quiet_delivery", true)
	return deliveryQuiet
}

// finishDeferral 清理顺延状态；只有真的存在过顺延才记日志，避免每 tick 刷屏。
func (e *Engine) finishDeferral(scope deferralScope, key uint, now time.Time, reason string) {
	e.mu.Lock()
	table := e.deferralTableLocked(scope)
	d, exists := table[key]
	if exists {
		delete(table, key)
	}
	e.mu.Unlock()
	if exists {
		slog.Info("自然停顿顺延结束",
			"scope", scope, "id", key, "delay", now.Sub(d.startedAt), "reason", reason)
	}
}

// deferralTableLocked 返回对应 scope 的顺延表；调用方必须持有 e.mu。
func (e *Engine) deferralTableLocked(scope deferralScope) map[uint]deferral {
	if scope == scopeSnooze {
		return e.snoozeDeferrals
	}
	return e.gridDeferrals
}

// clearGridDeferrals 只清周期提醒的顺延状态（场景停用时使用）。
func (e *Engine) clearGridDeferrals() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.gridDeferrals = map[uint]deferral{}
}

// pruneGridDeferralsLocked 清掉已不在启用场景里的提醒留下的顺延记录。
// 调用方必须持有 e.mu。
func (e *Engine) pruneGridDeferralsLocked(seen map[uint]bool) {
	for id := range e.gridDeferrals {
		if !seen[id] {
			delete(e.gridDeferrals, id)
		}
	}
}

// pruneSnoozeDeferralsLocked 清掉已不在待补发列表里的推迟记录。
// 调用方必须持有 e.mu。
func (e *Engine) pruneSnoozeDeferralsLocked(seen map[uint]bool) {
	for id := range e.snoozeDeferrals {
		if !seen[id] {
			delete(e.snoozeDeferrals, id)
		}
	}
}

// clearDeferrals 清空所有顺延状态（暂停、跨天、退出时使用）。
func (e *Engine) clearDeferrals() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.clearDeferralsLocked()
}

// clearDeferralsLocked 同上；调用方必须持有 e.mu。
func (e *Engine) clearDeferralsLocked() {
	e.gridDeferrals = map[uint]deferral{}
	e.snoozeDeferrals = map[uint]deferral{}
}

// DeferralState 是顺延状态的可绑定视图（宠物气泡用）。
type DeferralState struct {
	// Active 是否有提醒正在顺延。
	Active bool
	// Since 最早一轮顺延的开始时间；Active=false 时为零值。
	Since time.Time
}

// DeferralState 返回当前顺延状态。
func (e *Engine) DeferralState() DeferralState {
	e.mu.Lock()
	defer e.mu.Unlock()

	var earliest time.Time
	for _, d := range e.gridDeferrals {
		if earliest.IsZero() || d.startedAt.Before(earliest) {
			earliest = d.startedAt
		}
	}
	for _, d := range e.snoozeDeferrals {
		if earliest.IsZero() || d.startedAt.Before(earliest) {
			earliest = d.startedAt
		}
	}
	return DeferralState{Active: !earliest.IsZero(), Since: earliest}
}
