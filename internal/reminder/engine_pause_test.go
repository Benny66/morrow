package reminder

import (
	"testing"
	"time"

	"morrow/internal/model"
	"morrow/internal/pause"
)

// mockPauseProvider 是暂停信号提供者的 mock 实现。
type mockPauseProvider struct {
	state pause.State
}

func (m *mockPauseProvider) State() pause.State {
	return m.state
}

// TestEnginePauseDefersPeriodic 验证空闲暂停期间周期性提醒落 FireSilent、不投递。
//
// 【与 CONTEXT 「顺延不补发」对应】暂停期间到点的周期项既不能弹（顺延）、
// 又必须在 DB 里落一个终态（不补发）；否则重启后同一批点会重新被 claim。
// FireSilent 的语义正好是「已到点但选择不通知」，与静默时段共用一个状态。
func TestEnginePauseDefersPeriodic(t *testing.T) {
	store := newFakeStore()
	store.scenario = &model.Scenario{BaseModel: model.BaseModel{ID: 1}, Name: "s"}
	store.reminders = []model.Reminder{{
		BaseModel:   model.BaseModel{ID: 10},
		ScenarioID:  uintPtr(1),
		Title:       "喝水",
		Kind:        model.KindWater,
		IntervalMin: 60,
		WindowStart: "09:00",
		WindowEnd:   "18:00",
		Enabled:     true,
	}}

	now := time.Date(2026, 10, 20, 10, 30, 0, 0, time.Local)
	provider := &mockPauseProvider{
		state: pause.State{Reason: pause.ReasonIdle, Since: now.Add(-6 * time.Minute)},
	}
	notifier := &fakeNotifier{}

	engine := NewEngine(Options{
		Store:         store,
		Now:           func() time.Time { return now },
		Settings:      func() Settings { return Settings{NotifyEnabled: true} },
		Notifier:      notifier,
		PauseProvider: provider,
	})
	engine.Tick()

	// 09:00 与 10:00 两个点都应 claim 为 FireSilent。
	if len(store.initial) == 0 {
		t.Fatal("暂停期间周期点必须落 DB 终态，否则重启会重新积压")
	}
	for id, st := range store.initial {
		if st != model.FireSilent {
			t.Errorf("暂停期间的周期点 (fireID=%d) 应落 %s，实际 %s", id, model.FireSilent, st)
		}
	}
	if len(notifier.sent) != 0 {
		t.Errorf("暂停期间不应发通知，实际 %d 条", len(notifier.sent))
	}
}

// TestEnginePauseMarksOneOffAsMissed 验证暂停期间到点的一次性提醒记为「已错过」。
//
// 【与 CONTEXT 「顺延不补发」第二段对应】一次性提醒的计划时刻整个落在暂停窗口内，
// 不补弹但要在首页保留可见；否则用户离开 10 分钟回来，一条重要事件悄无声息地消失。
func TestEnginePauseMarksOneOffAsMissed(t *testing.T) {
	now := time.Date(2026, 10, 20, 10, 30, 0, 0, time.Local)
	// 计划时刻在暂停 10 秒之前 → 按 grace 判是 OneOffDue（本应 fired），
	// 但 paused 强制降级为 missed——这正是 US-6 的语义。
	onceAt := now.Add(-10 * time.Second).Unix()
	store := newFakeStore()
	store.oneOffs = []model.Reminder{{
		BaseModel: model.BaseModel{ID: 100},
		Title:     "接孩子",
		Kind:      model.KindCustom,
		OnceAt:    onceAt,
		Enabled:   true,
	}}

	provider := &mockPauseProvider{
		state: pause.State{Reason: pause.ReasonMeeting, Since: now.Add(-5 * time.Minute)},
	}
	notifier := &fakeNotifier{}
	engine := NewEngine(Options{
		Store:         store,
		Now:           func() time.Time { return now },
		Settings:      func() Settings { return Settings{NotifyEnabled: true} },
		Notifier:      notifier,
		PauseProvider: provider,
	})
	engine.Tick()

	if len(store.initial) != 1 {
		t.Fatalf("应 claim 一次，实际 %d", len(store.initial))
	}
	for _, st := range store.initial {
		if st != model.FireMissed {
			t.Errorf("暂停期间一次性提醒应落 %s，实际 %s", model.FireMissed, st)
		}
	}
	if len(notifier.sent) != 0 {
		t.Errorf("暂停期间不应发通知，实际 %d 条", len(notifier.sent))
	}
}

// TestEnginePauseMarksDailySingleAsMissed 验证暂停期间到点的每日单点提醒记为已错过。
func TestEnginePauseMarksDailySingleAsMissed(t *testing.T) {
	now := time.Date(2026, 10, 20, 10, 30, 0, 0, time.Local)
	store := newFakeStore()
	store.scenario = &model.Scenario{BaseModel: model.BaseModel{ID: 1}, Name: "s"}
	store.reminders = []model.Reminder{{
		BaseModel:   model.BaseModel{ID: 20},
		ScenarioID:  uintPtr(1),
		Title:       "上午舒展",
		Kind:        model.KindStand,
		IntervalMin: 0, // 每日单点
		WindowStart: "09:30",
		WindowEnd:   "18:00",
		Enabled:     true,
	}}

	provider := &mockPauseProvider{
		state: pause.State{Reason: pause.ReasonLock, Since: now.Add(-2 * time.Hour)},
	}
	notifier := &fakeNotifier{}
	engine := NewEngine(Options{
		Store:         store,
		Now:           func() time.Time { return now },
		Settings:      func() Settings { return Settings{NotifyEnabled: true} },
		Notifier:      notifier,
		PauseProvider: provider,
	})
	engine.Tick()

	// 每日单点走 FireMissed 通道（保留可见），与周期项的 FireSilent 分开。
	if len(store.initial) != 1 {
		t.Fatalf("每日单点应 claim 1 次，实际 %d", len(store.initial))
	}
	for _, st := range store.initial {
		if st != model.FireMissed {
			t.Errorf("每日单点在暂停期应落 %s，实际 %s", model.FireMissed, st)
		}
	}
	if len(notifier.sent) != 0 {
		t.Errorf("暂停期间每日单点不应发通知，实际 %d 条", len(notifier.sent))
	}
}

// TestEngineNoPauseBehavesNormally 验证不注入 provider 时正常投递（回归保护）。
func TestEngineNoPauseBehavesNormally(t *testing.T) {
	now := time.Date(2026, 10, 20, 10, 30, 0, 0, time.Local)
	store := newFakeStore()
	store.scenario = &model.Scenario{BaseModel: model.BaseModel{ID: 1}, Name: "s"}
	store.reminders = []model.Reminder{{
		BaseModel:   model.BaseModel{ID: 10},
		ScenarioID:  uintPtr(1),
		Title:       "喝水",
		Kind:        model.KindWater,
		IntervalMin: 60,
		WindowStart: "09:00",
		WindowEnd:   "18:00",
		Enabled:     true,
	}}

	notifier := &fakeNotifier{}
	engine := NewEngine(Options{
		Store:    store,
		Now:      func() time.Time { return now },
		Settings: func() Settings { return Settings{NotifyEnabled: true} },
		Notifier: notifier,
	})
	engine.Tick()

	if len(store.initial) == 0 {
		t.Fatal("无暂停时应 claim 触发点")
	}
	if len(notifier.sent) == 0 {
		t.Error("无暂停时应发通知")
	}
}

// TestEngineSwapProvider 验证 SwapProvider 原子替换 provider 生效。
//
// 【为什么单独测】SetPauseSettings 保存后立即 Swap，用户预期「关掉会议检测下一
// tick 就不暂停」；SwapProvider 若失效，UI 上的开关点了没反应。
// 这里用「暂停期间 claim 为 silent → 恢复后新点走 fired」这条完整链路验证。
func TestEngineSwapProvider(t *testing.T) {
	store := newFakeStore()
	store.scenario = &model.Scenario{BaseModel: model.BaseModel{ID: 1}, Name: "s"}
	store.reminders = []model.Reminder{{
		BaseModel:   model.BaseModel{ID: 10},
		ScenarioID:  uintPtr(1),
		Title:       "喝水",
		Kind:        model.KindWater,
		IntervalMin: 60,
		WindowStart: "09:00",
		WindowEnd:   "18:00",
		Enabled:     true,
	}}
	notifier := &fakeNotifier{}
	now := time.Date(2026, 10, 20, 10, 30, 0, 0, time.Local)
	clock := now
	pausedProvider := &mockPauseProvider{
		state: pause.State{Reason: pause.ReasonMeeting, Since: now},
	}
	engine := NewEngine(Options{
		Store:         store,
		Now:           func() time.Time { return clock },
		Settings:      func() Settings { return Settings{NotifyEnabled: true} },
		Notifier:      notifier,
		PauseProvider: pausedProvider,
	})

	// 第一次 tick（10:30，暂停）：09:00 / 10:00 都 claim 为 silent，不发通知。
	engine.Tick()
	if len(notifier.sent) != 0 {
		t.Fatalf("暂停期不应发通知，实际 %d 条", len(notifier.sent))
	}

	// Swap 到 nil 并推进到 11:00：新点 11:00 应 fired 并发通知。
	engine.SwapProvider(nil)
	clock = now.Add(30 * time.Minute)
	engine.Tick()
	if len(notifier.sent) == 0 {
		t.Error("Swap 后应恢复正常投递（新到达的 11:00 点应 fired），实际不发通知")
	}
}

func uintPtr(v uint) *uint { return &v }
