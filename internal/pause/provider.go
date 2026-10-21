// Package pause 提供智能免打扰的暂停信号。
//
// 暂停信号是「此刻该不该投递提醒」的单一真相：调度引擎在每个 tick 查询 Provider，
// 返回非 None 时跳过投递（顺延不补发）。各触发条件（空闲/锁屏/会议/全屏/专注/日历）
// 独立实现，可单独测试与关闭。
//
// 隐私边界：只读本机活跃度与前台应用身份，不采集屏幕内容、不调用摄像头、不上传。
// 全屏识别若需屏幕录制权限，只读窗口几何（见 ADR 0004）。
package pause

import (
	"sync"
	"time"
)

// PauseReason 是暂停原因的枚举（单一真相）。
//
// 【为什么收口成固定枚举而不是自由文本】前端/菜单栏/日志都要用它做分支判断，
// 自由文本会让下游为未知值兜底；固定枚举之后，未知值统一落到 ReasonNone，
// 调度引擎永远有可用的呈现。新增 reason 时必须同步更新前端镜像（见 guard/parity_test.go）。
type PauseReason string

// 暂停原因枚举（顺序即提示词里枚举的顺序）。
const (
	// ReasonNone 未暂停（正常调度）。
	//
	// 取值为 "none" 而不是空串：前端 pause.ts 的镜像常量与 parity guard 都要求
	// 非空字符串——空串会在解析器里被跳过，让 Go 侧的 None 变成「镜像缺失」。
	// 「未暂停」在 UI 侧也是一个明确的展示分支（暂停=false 时不显示原因），
	// 用可读的字面量比空串更可诊断。
	ReasonNone PauseReason = "none"
	// ReasonIdle 系统空闲（连续 N 分钟无键鼠输入）。
	ReasonIdle PauseReason = "idle"
	// ReasonLock 系统锁屏或休眠。
	ReasonLock PauseReason = "lock"
	// ReasonMeeting 前台为会议应用（Zoom/Teams/飞书/钉钉等）。
	ReasonMeeting PauseReason = "meeting"
	// ReasonFullscreen 前台窗口铺满当前屏幕。
	ReasonFullscreen PauseReason = "fullscreen"
	// ReasonFocus 系统专注模式开启（macOS Focus / Windows 等价能力）。
	ReasonFocus PauseReason = "focus"
	// ReasonCalendar 日历当前忙碌（只读布尔状态，不读事件内容）。
	ReasonCalendar PauseReason = "calendar"
	// ReasonPresentation 演示模式：用户主动进入，或在会议/全屏时经建议进入。
	// 优先级最高：用户的明确意图压过所有自动信号。
	ReasonPresentation PauseReason = "presentation"
)

// State 是暂停信号的当前状态。
type State struct {
	// Reason 暂停原因；ReasonNone 表示未暂停。
	Reason PauseReason
	// Since 暂停开始时间；Reason=None 时为零值。
	Since time.Time
}

// IsPaused 返回当前是否处于暂停状态。
//
// 【为什么同时判空串】Go 的零值 State{}.Reason 是 ""（不是 ReasonNone="none"）；
// 只判 ReasonNone 会让裸 State{} 被误认为「暂停中」。空串在语义上等价于
// 「未设置」，与 ReasonNone 是同一件事的两种表达。
func (s State) IsPaused() bool {
	return s.Reason != ReasonNone && s.Reason != ""
}

// Provider 是暂停信号的提供者接口。
//
// 【为什么是接口而不是具体实现】各触发条件（空闲/锁屏/会议/全屏/专注/日历）
// 独立实现，可单独测试与关闭；引擎只依赖接口，不关心具体触发源。
// 测试时注入 mock 即可推演各种暂停场景。
type Provider interface {
	// State 返回当前暂停状态。
	State() State
}

// IdleDetector 是空闲检测器（按系统空闲时长判定）。
type IdleDetector struct {
	// getIdle 返回系统连续空闲时长（由平台特定实现注入）。
	getIdle func() time.Duration
	// threshold 空闲阈值（默认 5 分钟）。
	threshold time.Duration

	mu     sync.Mutex
	active bool      // 上一轮 Detect 是否已触发（用于维护 since）
	since  time.Time // 首次触发时的时间戳
	now    func() time.Time
}

// NewIdleDetector 构造空闲检测器。
//
// getIdle 是平台特定的空闲时长获取函数（macOS 用 CGEventSourceSecondsSinceLastEventType，
// Windows 用 GetLastInputInfo）；threshold 是触发暂停的空闲时长（默认 5 分钟）。
func NewIdleDetector(getIdle func() time.Duration, threshold time.Duration) *IdleDetector {
	return &IdleDetector{
		getIdle:   getIdle,
		threshold: threshold,
		now:       time.Now,
	}
}

// Detect 返回当前空闲状态对应的暂停原因。
//
// 空闲时长 ≥ 阈值 → ReasonIdle；否则 → ReasonNone。
func (d *IdleDetector) Detect() PauseReason {
	if d.getIdle == nil {
		return ReasonNone
	}
	idle := d.getIdle()
	hit := idle >= d.threshold

	d.mu.Lock()
	defer d.mu.Unlock()
	if hit && !d.active {
		d.active = true
		d.since = d.now()
	} else if !hit && d.active {
		d.active = false
		d.since = time.Time{}
	}
	if hit {
		return ReasonIdle
	}
	return ReasonNone
}

// Since 返回进入空闲状态的时刻；未触发时为零值。
func (d *IdleDetector) Since() time.Time {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.since
}

// State 让 IdleDetector 满足 Provider 接口（单信号场景直接接入引擎）。
func (d *IdleDetector) State() State {
	if d.Detect() == ReasonNone {
		return State{Reason: ReasonNone}
	}
	return State{Reason: ReasonIdle, Since: d.Since()}
}
