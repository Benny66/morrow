package pause

import (
	"sync"
	"time"
)

// FocusStatus 是「系统专注模式」的读取结果。
//
// 【为什么带 Granted】macOS 的 INFocusStatusCenter 需要用户授权才能读到 enabled；
// 拒绝授权时把 Enabled 硬置 false 会让「用户没开专注 vs 用户没授权」两种情况
// 变成同一份数据，Detector 也就没法区分。分开两个字段最诚实。
type FocusStatus struct {
	// Granted 是否已拿到读取权限。Windows / Linux 恒 true 表示「不需要授权」。
	Granted bool
	// Enabled 系统专注是否开启。Granted=false 时无意义（调用方应忽略）。
	Enabled bool
}

// FocusDetector 是系统专注模式的暂停信号检测器。
//
// 平台层通过 getStatus 回调返回 FocusStatus；测试可以直接注入 mock。
// macOS 用 INFocusStatusCenter（需要用户授权）；Windows 用 SHQueryUserNotificationState
// 里等价的状态（若无则静默缺席）。见 ADR 0005 关于「只读忙闲」的取舍。
type FocusDetector struct {
	getStatus func() FocusStatus

	mu    sync.Mutex
	inHit bool
	since time.Time
	now   func() time.Time
}

// NewFocusDetector 构造专注模式检测器。
func NewFocusDetector(getStatus func() FocusStatus) *FocusDetector {
	return &FocusDetector{
		getStatus: getStatus,
		now:       time.Now,
	}
}

// Detect 返回当前专注状态对应的暂停原因。
//
// 未授权或未开启 → ReasonNone；开启 → ReasonFocus。
func (d *FocusDetector) Detect() PauseReason {
	if d.getStatus == nil {
		return ReasonNone
	}
	st := d.getStatus()
	hit := st.Granted && st.Enabled

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
		return ReasonFocus
	}
	return ReasonNone
}

// Since 返回进入专注状态的时刻；未触发时为零值。
func (d *FocusDetector) Since() time.Time {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.since
}

// State 让 FocusDetector 满足 Provider 接口。
func (d *FocusDetector) State() State {
	if d.Detect() == ReasonNone {
		return State{Reason: ReasonNone}
	}
	return State{Reason: ReasonFocus, Since: d.Since()}
}
