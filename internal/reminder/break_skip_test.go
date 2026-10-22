package reminder

import (
	"testing"
	"time"

	"morrow/internal/model"
)

type fakeBreakProvider struct {
	skip       bool
	calls      int
	lastID     uint
	lastFireAt int64
}

func (f *fakeBreakProvider) ShouldSkipStandMove(reminderID uint, fireAt int64, _ time.Time) bool {
	f.calls++
	f.lastID = reminderID
	f.lastFireAt = fireAt
	return f.skip
}

func testSkipEngine(store *fakeStore, now time.Time, provider BreakProvider) (*Engine, *fakeNotifier, *fakeEmitter) {
	notifier := &fakeNotifier{}
	emitter := &fakeEmitter{}
	engine := NewEngine(Options{
		Store:         store,
		Now:           func() time.Time { return now },
		Settings:      func() Settings { return Settings{NotifyEnabled: true} },
		Notifier:      notifier,
		Emitter:       emitter,
		BreakProvider: provider,
	})
	return engine, notifier, emitter
}

func TestGridSkipsStandMoveAfterBreak(t *testing.T) {
	store := newFakeStore()
	store.scenario = &model.Scenario{BaseModel: model.BaseModel{ID: 1}, Name: "s"}
	store.reminders = []model.Reminder{{
		BaseModel:   model.BaseModel{ID: 7},
		ScenarioID:  uintPtr(1),
		Title:       "起身活动",
		Kind:        model.KindStand,
		IntervalMin: 60,
		WindowStart: "09:00",
		WindowEnd:   "18:00",
		Enabled:     true,
	}}
	now := localTime(2026, time.March, 2, 10, 30)
	provider := &fakeBreakProvider{skip: true}
	engine, notifier, emitter := testSkipEngine(store, now, provider)
	engine.Tick()

	if len(notifier.sent) != 0 || len(emitter.fired) != 0 {
		t.Errorf("休息后跳过时不应通知/广播，得到 %d/%d", len(notifier.sent), len(emitter.fired))
	}
	if provider.calls != 1 || provider.lastID != 7 {
		t.Errorf("BreakProvider 调用 = %d, lastID=%d，期望 1/7", provider.calls, provider.lastID)
	}
	if got := localTime(2026, time.March, 2, 10, 0).Unix(); provider.lastFireAt != got {
		t.Errorf("BreakProvider fireAt = %d，期望最新到点 %d", provider.lastFireAt, got)
	}
	silent, missed := 0, 0
	for _, status := range store.settled {
		switch status {
		case model.FireSilent:
			silent++
		case model.FireMissed:
			missed++
		}
	}
	if silent != 1 || missed != 1 {
		t.Errorf("静默/错过 = %d/%d，期望 1/1（只跳过最新一条，旧点仍记错过）", silent, missed)
	}
}

func TestGridDoesNotSkipWater(t *testing.T) {
	store := newFakeStore()
	store.scenario = &model.Scenario{BaseModel: model.BaseModel{ID: 1}, Name: "s"}
	store.reminders = []model.Reminder{{
		BaseModel:   model.BaseModel{ID: 8},
		ScenarioID:  uintPtr(1),
		Title:       "喝水",
		Kind:        model.KindWater,
		IntervalMin: 60,
		WindowStart: "09:00",
		WindowEnd:   "18:00",
		Enabled:     true,
	}}
	now := localTime(2026, time.March, 2, 10, 30)
	provider := &fakeBreakProvider{skip: true}
	engine, notifier, _ := testSkipEngine(store, now, provider)
	engine.Tick()

	if len(notifier.sent) != 1 {
		t.Errorf("喝水提醒不应被休息跳过，通知数 = %d", len(notifier.sent))
	}
	if provider.calls != 0 {
		t.Errorf("非 stand/move 不应询问 BreakProvider，实际 %d 次", provider.calls)
	}
}

func TestGridSkipFalseDeliversNormally(t *testing.T) {
	store := newFakeStore()
	store.scenario = &model.Scenario{BaseModel: model.BaseModel{ID: 1}, Name: "s"}
	store.reminders = []model.Reminder{{
		BaseModel:   model.BaseModel{ID: 9},
		ScenarioID:  uintPtr(1),
		Title:       "走动",
		Kind:        model.KindMove,
		IntervalMin: 60,
		WindowStart: "09:00",
		WindowEnd:   "18:00",
		Enabled:     true,
	}}
	now := localTime(2026, time.March, 2, 10, 30)
	provider := &fakeBreakProvider{skip: false}
	engine, notifier, emitter := testSkipEngine(store, now, provider)
	engine.Tick()

	if len(notifier.sent) != 1 || len(emitter.fired) != 1 {
		t.Errorf("未休息时应正常投递，得到通知/事件 %d/%d", len(notifier.sent), len(emitter.fired))
	}
	if provider.calls != 1 {
		t.Errorf("BreakProvider 调用 = %d，期望 1", provider.calls)
	}
}

// TestSnoozeIsNotSkippedByBreak 用户主动推迟的补发必须照常送达，不因休息跳过。
func TestSnoozeIsNotSkippedByBreak(t *testing.T) {
	store := newFakeStore()
	store.snoozes = []DueFire{{
		FireID:     99,
		ReminderID: 9,
		ScenarioID: 1,
		Title:      "走动",
		Kind:       model.KindMove,
		FireAt:     localTime(2026, time.March, 2, 10, 0).Unix(),
	}}
	now := localTime(2026, time.March, 2, 10, 30)
	provider := &fakeBreakProvider{skip: true}
	engine, notifier, emitter := testSkipEngine(store, now, provider)
	engine.Tick()

	if len(notifier.sent) != 1 || len(emitter.fired) != 1 {
		t.Errorf("推迟补发不应被跳过，得到通知/事件 %d/%d", len(notifier.sent), len(emitter.fired))
	}
	if provider.calls != 0 {
		t.Errorf("推迟补发不应询问 BreakProvider，实际 %d 次", provider.calls)
	}
}
