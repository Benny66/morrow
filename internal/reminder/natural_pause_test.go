package reminder

import (
	"testing"
	"time"

	"morrow/internal/model"
	"morrow/internal/pause"
	"morrow/internal/restmode"
)

// fakeActivity 是可控的系统空闲时长。
type fakeActivity struct {
	idle      time.Duration
	supported bool
}

func (f *fakeActivity) IdleDuration() (time.Duration, bool) {
	return f.idle, f.supported
}

func naturalPauseStore(kind string) *fakeStore {
	store := newFakeStore()
	store.scenario = &model.Scenario{BaseModel: model.BaseModel{ID: 1}, Name: "s"}
	store.reminders = []model.Reminder{{
		BaseModel:   model.BaseModel{ID: 10},
		ScenarioID:  uintPtr(1),
		Title:       "喝水",
		Kind:        kind,
		IntervalMin: 10,
		WindowStart: "09:00",
		WindowEnd:   "18:00",
		Enabled:     true,
	}}
	return store
}

func naturalPauseSettings(max int) Settings {
	return Settings{
		NotifyEnabled:          true,
		NaturalPauseEnabled:    true,
		NaturalPauseMaxSeconds: max,
	}
}

func newNaturalPauseEngine(
	store *fakeStore,
	activity *fakeActivity,
	settings *Settings,
	provider pause.Provider,
	rest *restmode.Machine,
	now *time.Time,
) (*Engine, *fakeNotifier, *fakeEmitter) {
	notifier := &fakeNotifier{}
	emitter := &fakeEmitter{}
	engine := NewEngine(Options{
		Store:         store,
		Now:           func() time.Time { return *now },
		Settings:      func() Settings { return *settings },
		Notifier:      notifier,
		Emitter:       emitter,
		PauseProvider: provider,
		Activity:      activity,
		RestMode:      rest,
	})
	return engine, notifier, emitter
}

// TestNaturalPauseDefersWhileTyping 输入时不 claim；停手后按正常通道投递。
func TestNaturalPauseDefersWhileTyping(t *testing.T) {
	now := time.Date(2026, 10, 20, 9, 30, 0, 0, time.Local)
	store := naturalPauseStore(model.KindWater)
	activity := &fakeActivity{idle: time.Second, supported: true}
	settings := naturalPauseSettings(30)
	engine, notifier, emitter := newNaturalPauseEngine(store, activity, &settings, nil, nil, &now)

	engine.Tick()
	if len(store.initial) != 0 {
		t.Fatalf("输入中不应 claim 任何触发点，实际 %d", len(store.initial))
	}
	if len(notifier.sent) != 0 {
		t.Errorf("输入中不应发通知，实际 %d", len(notifier.sent))
	}
	if !engine.DeferralState().Active {
		t.Error("输入中应有 active 顺延状态")
	}

	// 停手：空闲 10 秒 > 5 秒窗口 → 正常投递最近的 1 条。
	now = now.Add(20 * time.Second)
	activity.idle = 10 * time.Second
	engine.Tick()

	if len(notifier.sent) != 1 {
		t.Fatalf("停手后应发 1 条通知，实际 %d", len(notifier.sent))
	}
	if len(emitter.fired) != 1 {
		t.Fatalf("停手后应广播 1 条 fired，实际 %d", len(emitter.fired))
	}
	if emitter.fired[0].QuietDelivery {
		t.Error("停手后的正常投递不应标记 quiet_delivery")
	}
	if engine.DeferralState().Active {
		t.Error("投递后顺延状态应清空")
	}
}

// TestNaturalPauseQuietDeliveryAtCap 到上限仍在输入：安静投递，不弹全屏、不进休息模式。
func TestNaturalPauseQuietDeliveryAtCap(t *testing.T) {
	now := time.Date(2026, 10, 20, 9, 30, 0, 0, time.Local)
	store := naturalPauseStore(model.KindStand)
	activity := &fakeActivity{idle: time.Second, supported: true}
	settings := naturalPauseSettings(30)
	rest := restmode.NewMachine()
	engine, notifier, emitter := newNaturalPauseEngine(store, activity, &settings, nil, rest, &now)

	engine.Tick() // 开始顺延
	now = now.Add(31 * time.Second)
	engine.Tick() // 到上限，仍在输入

	if len(notifier.sent) != 1 {
		t.Fatalf("安静投递应仍发通知，实际 %d", len(notifier.sent))
	}
	if len(emitter.fired) != 1 {
		t.Fatalf("安静投递应广播 fired，实际 %d", len(emitter.fired))
	}
	if !emitter.fired[0].QuietDelivery {
		t.Error("到上限的投递应标记 quiet_delivery")
	}
	if !store.quiet[emitter.fired[0].FireID] {
		t.Error("安静投递应通过 Store.MarkQuietFire 标记触发记录")
	}
	if rest.State() != restmode.StateIdle {
		t.Errorf("安静投递不应进入休息模式，实际状态 %s", rest.State())
	}
	if engine.DeferralState().Active {
		t.Error("安静投递后顺延状态应清空")
	}
}

// TestNaturalPauseNormalDeliveryEntersRestMode 停手后的正常投递仍应进入休息模式。
func TestNaturalPauseNormalDeliveryEntersRestMode(t *testing.T) {
	now := time.Date(2026, 10, 20, 9, 30, 0, 0, time.Local)
	store := naturalPauseStore(model.KindStand)
	activity := &fakeActivity{idle: time.Second, supported: true}
	settings := naturalPauseSettings(30)
	rest := restmode.NewMachine()
	engine, _, emitter := newNaturalPauseEngine(store, activity, &settings, nil, rest, &now)

	engine.Tick()
	now = now.Add(20 * time.Second)
	activity.idle = 10 * time.Second
	engine.Tick()

	if rest.State() != restmode.StateCountdown {
		t.Errorf("正常投递 stand 应进入休息倒计时，实际 %s", rest.State())
	}
	if len(emitter.fired) != 1 || emitter.fired[0].QuietDelivery {
		t.Errorf("正常投递应广播非 quiet 的 fired: %+v", emitter.fired)
	}
}

// TestNaturalPauseDoesNotDeferOneOff 一次性提醒不顺延。
func TestNaturalPauseDoesNotDeferOneOff(t *testing.T) {
	now := time.Date(2026, 10, 20, 10, 30, 0, 0, time.Local)
	store := newFakeStore()
	store.oneOffs = []model.Reminder{{
		BaseModel: model.BaseModel{ID: 100},
		Title:     "接孩子",
		Kind:      model.KindCustom,
		OnceAt:    now.Add(-10 * time.Second).Unix(),
		Enabled:   true,
	}}
	activity := &fakeActivity{idle: time.Second, supported: true}
	settings := naturalPauseSettings(30)
	engine, notifier, emitter := newNaturalPauseEngine(store, activity, &settings, nil, nil, &now)

	engine.Tick()
	if len(notifier.sent) != 1 || len(emitter.fired) != 1 {
		t.Fatalf("一次性提醒应立即投递: notifier=%d fired=%d", len(notifier.sent), len(emitter.fired))
	}
	if emitter.fired[0].QuietDelivery {
		t.Error("一次性提醒不应标记 quiet_delivery")
	}
	if engine.DeferralState().Active {
		t.Error("一次性提醒不应产生顺延状态")
	}
}

// TestNaturalPauseDisabled 设置 0 秒时行为与当前完全一致。
func TestNaturalPauseDisabled(t *testing.T) {
	now := time.Date(2026, 10, 20, 9, 30, 0, 0, time.Local)
	store := naturalPauseStore(model.KindWater)
	activity := &fakeActivity{idle: time.Second, supported: true}
	settings := Settings{NotifyEnabled: true, NaturalPauseEnabled: false, NaturalPauseMaxSeconds: 0}
	engine, notifier, emitter := newNaturalPauseEngine(store, activity, &settings, nil, nil, &now)

	engine.Tick()
	if len(notifier.sent) != 1 || len(emitter.fired) != 1 {
		t.Fatalf("关闭顺延后应立即投递: notifier=%d fired=%d", len(notifier.sent), len(emitter.fired))
	}
	if emitter.fired[0].QuietDelivery {
		t.Error("关闭顺延时不应标记 quiet_delivery")
	}
}

// TestNaturalPauseUnsupportedPlatform 平台不支持空闲读取时静默退回正常投递。
func TestNaturalPauseUnsupportedPlatform(t *testing.T) {
	now := time.Date(2026, 10, 20, 9, 30, 0, 0, time.Local)
	store := naturalPauseStore(model.KindWater)
	activity := &fakeActivity{idle: 0, supported: false}
	settings := naturalPauseSettings(30)
	engine, notifier, _ := newNaturalPauseEngine(store, activity, &settings, nil, nil, &now)

	engine.Tick()
	if len(notifier.sent) != 1 {
		t.Errorf("平台不支持时应正常投递，实际通知 %d", len(notifier.sent))
	}
	if engine.DeferralState().Active {
		t.Error("平台不支持时不应产生顺延状态")
	}
}

// TestNaturalPausePausePrecedence 暂停优先：暂停时清空顺延，周期点按暂停语义处理。
func TestNaturalPausePausePrecedence(t *testing.T) {
	now := time.Date(2026, 10, 20, 9, 30, 0, 0, time.Local)
	store := naturalPauseStore(model.KindWater)
	activity := &fakeActivity{idle: time.Second, supported: true}
	settings := naturalPauseSettings(30)
	provider := &mockPauseProvider{}
	engine, notifier, _ := newNaturalPauseEngine(store, activity, &settings, provider, nil, &now)

	engine.Tick() // 开始顺延
	if !engine.DeferralState().Active {
		t.Fatal("前置条件：应有顺延状态")
	}

	provider.state = pause.State{Reason: pause.ReasonMeeting, Since: now}
	engine.Tick()
	if engine.DeferralState().Active {
		t.Error("进入暂停后应清空顺延状态")
	}
	if len(notifier.sent) != 0 {
		t.Errorf("暂停期间不应发通知，实际 %d", len(notifier.sent))
	}
	for id, st := range store.initial {
		if st != model.FireSilent {
			t.Errorf("暂停期间周期点 (fireID=%d) 应为 FireSilent，实际 %s", id, st)
		}
	}
}

// TestNaturalPauseSnoozeDeferral 推迟补发同样适用顺延与安静投递。
func TestNaturalPauseSnoozeDeferral(t *testing.T) {
	now := time.Date(2026, 10, 20, 9, 30, 0, 0, time.Local)
	store := newFakeStore()
	store.snoozes = []DueFire{{
		FireID:     77,
		ReminderID: 10,
		Title:      "喝水",
		Kind:       model.KindWater,
		FireAt:     now.Unix(),
	}}
	activity := &fakeActivity{idle: time.Second, supported: true}
	settings := naturalPauseSettings(30)
	engine, notifier, emitter := newNaturalPauseEngine(store, activity, &settings, nil, nil, &now)

	engine.Tick()
	if len(notifier.sent) != 0 {
		t.Fatalf("推迟补发输入中不应发通知，实际 %d", len(notifier.sent))
	}
	if !engine.DeferralState().Active {
		t.Error("推迟补发应产生顺延状态")
	}

	// fakeStore 的 DueSnoozeFires 读后清空；真实 DB 不会，这里重新放回模拟下轮查询。
	store.snoozes = []DueFire{{
		FireID:     77,
		ReminderID: 10,
		Title:      "喝水",
		Kind:       model.KindWater,
		FireAt:     now.Unix(),
	}}
	now = now.Add(31 * time.Second)
	engine.Tick()
	if len(notifier.sent) != 1 || len(emitter.fired) != 1 {
		t.Fatalf("到上限应安静投递: notifier=%d fired=%d", len(notifier.sent), len(emitter.fired))
	}
	if !emitter.fired[0].QuietDelivery {
		t.Error("推迟补发到上限应标记 quiet_delivery")
	}
	if engine.DeferralState().Active {
		t.Error("投递后顺延状态应清空")
	}
}

// TestNaturalPausePrunesRemovedReminder 提醒被删/停用后顺延状态应被清理。
func TestNaturalPausePrunesRemovedReminder(t *testing.T) {
	now := time.Date(2026, 10, 20, 9, 30, 0, 0, time.Local)
	store := naturalPauseStore(model.KindWater)
	activity := &fakeActivity{idle: time.Second, supported: true}
	settings := naturalPauseSettings(30)
	engine, _, _ := newNaturalPauseEngine(store, activity, &settings, nil, nil, &now)

	engine.Tick()
	if !engine.DeferralState().Active {
		t.Fatal("前置条件：应有顺延状态")
	}
	store.reminders = nil
	engine.Tick()
	if engine.DeferralState().Active {
		t.Error("提醒移除后顺延状态应被清理")
	}
}
