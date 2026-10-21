package presentation

import (
	"testing"
	"time"
)

// TestControllerManualEnterExit 手动进入/退出的状态与时间戳。
func TestControllerManualEnterExit(t *testing.T) {
	t0 := time.Date(2026, 10, 21, 9, 0, 0, 0, time.Local)
	c := NewController()

	if snap := c.Snapshot(); snap.Active {
		t.Fatal("新 Controller 不应处于演示模式")
	}

	c.Enter(SourceManual, t0)
	snap := c.Snapshot()
	if !snap.Active {
		t.Error("Enter 后 Active 应为 true")
	}
	if snap.Source != SourceManual {
		t.Errorf("Source = %q，期望 %q", snap.Source, SourceManual)
	}
	if !snap.Since.Equal(t0) {
		t.Errorf("Since = %v，期望 %v", snap.Since, t0)
	}

	// 幂等：已进入时再次 Enter 不重置来源与开始时间。
	c.Enter(SourceSuggested, t0.Add(time.Minute))
	snap = c.Snapshot()
	if snap.Source != SourceManual || !snap.Since.Equal(t0) {
		t.Errorf("重复 Enter 改变了状态: source=%q since=%v", snap.Source, snap.Since)
	}

	t1 := t0.Add(10 * time.Minute)
	c.Exit(t1)
	snap = c.Snapshot()
	if snap.Active {
		t.Error("Exit 后 Active 应为 false")
	}
	if snap.Source != "" {
		t.Errorf("Exit 后 Source = %q，期望空串", snap.Source)
	}
	if !snap.ExitedAt.Equal(t1) {
		t.Errorf("ExitedAt = %v，期望 %v", snap.ExitedAt, t1)
	}

	// 幂等：未进入时再次 Exit 不改变最近退出时间。
	c.Exit(t1.Add(time.Minute))
	if !c.Snapshot().ExitedAt.Equal(t1) {
		t.Error("重复 Exit 改变了 ExitedAt")
	}
}

// TestSourceValid 来源枚举只接受 manual / suggested。
func TestSourceValid(t *testing.T) {
	for _, s := range []Source{SourceManual, SourceSuggested} {
		if !s.Valid() {
			t.Errorf("%q 应为合法来源", s)
		}
	}
	for _, s := range []Source{"", "auto", "unknown"} {
		if s.Valid() {
			t.Errorf("%q 不应为合法来源", s)
		}
	}
}

// TestSuggestionAfterDelayAndDismiss 触发持续 5 秒后才建议；忽略后本次会话不再建议。
func TestSuggestionAfterDelayAndDismiss(t *testing.T) {
	t0 := testTime()
	c := NewController()

	c.Observe(TriggerMeeting, true, t0)
	if c.Snapshot().SuggestionVisible {
		t.Error("触发瞬间不应建议")
	}
	c.Observe(TriggerMeeting, true, t0.Add(SuggestionDelay-time.Second))
	if c.Snapshot().SuggestionVisible {
		t.Error("未满 5 秒不应建议")
	}
	c.Observe(TriggerMeeting, true, t0.Add(SuggestionDelay))
	if !c.Snapshot().SuggestionVisible {
		t.Fatal("满 5 秒应建议")
	}

	c.DismissSuggestion()
	if c.Snapshot().SuggestionVisible {
		t.Error("忽略后不应继续建议")
	}
	c.Observe(TriggerMeeting, true, t0.Add(SuggestionDelay+time.Minute))
	if c.Snapshot().SuggestionVisible {
		t.Error("同一触发会话内忽略后不应再建议")
	}

	// 触发消失后重新进入 → 新会话，重新允许建议。
	c.Observe(TriggerNone, true, t0.Add(2*time.Minute))
	c.Observe(TriggerMeeting, true, t0.Add(2*time.Minute+time.Second))
	c.Observe(TriggerMeeting, true, t0.Add(2*time.Minute+SuggestionDelay+time.Second))
	if !c.Snapshot().SuggestionVisible {
		t.Error("新会话满 5 秒应重新建议")
	}
}

// TestSuggestionRespectsSetting 设置关闭时不建议，重新打开后恢复。
func TestSuggestionRespectsSetting(t *testing.T) {
	t0 := testTime()
	c := NewController()
	c.Observe(TriggerMeeting, false, t0)
	c.Observe(TriggerMeeting, false, t0.Add(SuggestionDelay))
	if c.Snapshot().SuggestionVisible {
		t.Error("设置关闭时不应建议")
	}
	c.Observe(TriggerMeeting, true, t0.Add(SuggestionDelay+time.Second))
	if !c.Snapshot().SuggestionVisible {
		t.Error("设置打开后应恢复建议")
	}
}

// TestSuggestedAutoExitAfterGrace 建议进入后，触发消失 30 秒才退出。
func TestSuggestedAutoExitAfterGrace(t *testing.T) {
	t0 := testTime()
	c := NewController()
	c.Enter(SourceSuggested, t0)

	if c.Observe(TriggerMeeting, true, t0.Add(time.Minute)) {
		t.Fatal("触发仍在时不应自动退出")
	}
	if !c.Snapshot().Active {
		t.Fatal("触发仍在时应保持演示")
	}

	gone := t0.Add(2 * time.Minute)
	if c.Observe(TriggerNone, true, gone) {
		t.Fatal("触发刚消失时不应立即退出")
	}
	if !c.Snapshot().Active {
		t.Fatal("30 秒宽限内应保持演示")
	}
	if c.Observe(TriggerNone, true, gone.Add(AutoExitDelay-time.Second)) {
		t.Fatal("未满 30 秒不应退出")
	}
	if !c.Observe(TriggerNone, true, gone.Add(AutoExitDelay)) {
		t.Fatal("满 30 秒应自动退出")
	}
	snap := c.Snapshot()
	if snap.Active {
		t.Error("自动退出后 Active 应为 false")
	}
	if snap.ExitedAt.IsZero() {
		t.Error("自动退出后应记录 ExitedAt")
	}
}

// TestSuggestedAutoExitCancelled 30 秒内触发恢复 → 取消退出。
func TestSuggestedAutoExitCancelled(t *testing.T) {
	t0 := testTime()
	c := NewController()
	c.Enter(SourceSuggested, t0)

	gone := t0.Add(time.Minute)
	c.Observe(TriggerNone, true, gone)
	c.Observe(TriggerNone, true, gone.Add(10*time.Second))
	// 触发恢复，取消待退出。
	c.Observe(TriggerMeeting, true, gone.Add(20*time.Second))
	if !c.Snapshot().Active {
		t.Fatal("触发恢复后应保持演示")
	}
	// 再次消失后重新计时 30 秒。
	gone2 := gone.Add(40 * time.Second)
	c.Observe(TriggerNone, true, gone2)
	if c.Observe(TriggerNone, true, gone2.Add(AutoExitDelay-time.Second)) {
		t.Fatal("重新计时未满 30 秒不应退出")
	}
	if !c.Observe(TriggerNone, true, gone2.Add(AutoExitDelay)) {
		t.Fatal("重新计时满 30 秒应退出")
	}
}

// TestManualDoesNotAutoExit 手动进入不因触发消失而退出。
func TestManualDoesNotAutoExit(t *testing.T) {
	t0 := testTime()
	c := NewController()
	c.Enter(SourceManual, t0)
	for i := 0; i < 5; i++ {
		if c.Observe(TriggerNone, true, t0.Add(time.Duration(i+1)*time.Minute)) {
			t.Fatal("手动演示不应自动退出")
		}
	}
	if !c.Snapshot().Active {
		t.Error("手动演示应保持到手动退出")
	}
}

// TestEnterSuppressesSuggestionInSession 用户已手动进入/退出后，同一会议会话不再建议。
func TestEnterSuppressesSuggestionInSession(t *testing.T) {
	t0 := testTime()
	c := NewController()
	c.Observe(TriggerMeeting, true, t0)
	c.Observe(TriggerMeeting, true, t0.Add(SuggestionDelay))
	if !c.Snapshot().SuggestionVisible {
		t.Fatal("前置条件：应已建议")
	}

	c.Enter(SourceManual, t0.Add(SuggestionDelay))
	c.Exit(t0.Add(SuggestionDelay + time.Minute))
	c.Observe(TriggerMeeting, true, t0.Add(SuggestionDelay+2*time.Minute))
	if c.Snapshot().SuggestionVisible {
		t.Error("同一会议会话内手动进入/退出后不应再次建议")
	}
}

// TestManualBeforeTriggerSuppressesSuggestion 先手动进入、会议中途退出，
// 也不应在同一会议会话里再次建议。
func TestManualBeforeTriggerSuppressesSuggestion(t *testing.T) {
	t0 := testTime()
	c := NewController()
	c.Enter(SourceManual, t0)

	// 会议在手动演示期间开始：不能重置 suggestedThisSession。
	c.Observe(TriggerMeeting, true, t0.Add(time.Second))
	c.Exit(t0.Add(2 * time.Second))
	c.Observe(TriggerMeeting, true, t0.Add(SuggestionDelay+3*time.Second))
	if c.Snapshot().SuggestionVisible {
		t.Error("先手动进入再退出，不应在同一会议会话里再次建议")
	}
}

func testTime() time.Time {
	return time.Date(2026, 10, 21, 9, 0, 0, 0, time.Local)
}
