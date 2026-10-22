package sit

import "time"

// 专注会话阈值：同一应用连续使用满 10 分钟才算专注；
// 短暂切走 ≤120 秒不打断，超过则结束。只影响宠物开口时机。
const (
	FocusMin         = 10 * time.Minute
	FocusSwitchGrace = 2 * time.Minute
)

// FocusInput 是运行层每 5 秒喂给 Tracker 的前台应用事实。
//
// Enabled=false 表示画像关闭、场景失效、锁屏或长空闲——此时不保留专注态。
// AppID 为空但 Enabled=true 表示这一轮读不到前台应用：按短暂切走处理，
// 超过宽限才结束。应用标识只存在于 Tracker 内部，不进入 Snapshot。
type FocusInput struct {
	Now      time.Time
	Enabled  bool
	AppID    string
	Category Category
}

// SuppressesBubble 判断专注期间是否应隐藏这类宠物气泡。
//
// 只压软提醒与记忆；问候/用户动作反馈不属于打断，仍可展示。
func SuppressesBubble(focus *Focus, kind BubbleKind) bool {
	if focus == nil {
		return false
	}
	return kind == BubbleNudge || kind == BubbleMemory
}

// ObserveFocus 推进专注会话状态。只累积真正停留在专注应用上的时长；
// 宽限内的切走不计入分钟数，也不打断会话。
func (t *Tracker) ObserveFocus(in FocusInput) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !in.Enabled || (t.state != StateTracking && t.state != StateMuted) {
		t.clearFocusLocked()
		return
	}

	if t.focusAppID == "" {
		if in.AppID != "" {
			t.startFocusLocked(in)
		}
		return
	}

	if in.AppID == t.focusAppID {
		if !t.focusAwaySince.IsZero() {
			// 回到原应用：宽限内恢复，不把离开的时间算作专注。
			t.focusAwaySince = time.Time{}
			t.focusLastSeen = in.Now
		} else if !t.focusLastSeen.IsZero() && in.Now.After(t.focusLastSeen) {
			t.focusAccum += in.Now.Sub(t.focusLastSeen)
			t.focusLastSeen = in.Now
		} else {
			t.focusLastSeen = in.Now
		}
		if in.Category != "" {
			// 用户纠正类别后，同一应用的专注展示立即跟随。
			t.focusCategory = in.Category
		}
		if t.focusAccum >= FocusMin {
			t.focusActive = true
		}
		return
	}

	// 切到别的应用或读不到应用：先进入宽限，超过 120 秒才结束。
	if t.focusAwaySince.IsZero() {
		t.focusAwaySince = in.Now
		t.focusLastSeen = time.Time{}
	}
	if in.Now.Sub(t.focusAwaySince) > FocusSwitchGrace {
		t.clearFocusLocked()
		if in.AppID != "" {
			t.startFocusLocked(in)
		}
	}
}

// Focus 返回当前专注快照；不足 10 分钟或已结束时为 nil。
func (t *Tracker) Focus() *Focus {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.focusSnapshotLocked()
}

func (t *Tracker) startFocusLocked(in FocusInput) {
	t.focusAppID = in.AppID
	t.focusCategory = in.Category
	if t.focusCategory == "" {
		t.focusCategory = CategoryOther
	}
	t.focusAccum = 0
	t.focusActive = false
	t.focusLastSeen = in.Now
	t.focusAwaySince = time.Time{}
}

func (t *Tracker) clearFocusLocked() {
	t.focusAppID = ""
	t.focusCategory = ""
	t.focusAccum = 0
	t.focusActive = false
	t.focusLastSeen = time.Time{}
	t.focusAwaySince = time.Time{}
}

func (t *Tracker) focusSnapshotLocked() *Focus {
	if !t.focusActive || t.focusAppID == "" {
		return nil
	}
	return &Focus{
		Category: t.focusCategory,
		Minutes:  int(t.focusAccum / time.Minute),
	}
}
