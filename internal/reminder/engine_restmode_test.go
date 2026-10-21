package reminder

import (
	"testing"
	"time"

	"morrow/internal/model"
	"morrow/internal/restmode"
)

// TestDispatchEntersRestModeForStandKind 验证 stand 提醒到点后 Machine 进入 Countdown。
func TestDispatchEntersRestModeForStandKind(t *testing.T) {
	now := time.Date(2026, 10, 20, 10, 30, 0, 0, time.Local)
	store := newFakeStore()
	store.scenario = &model.Scenario{BaseModel: model.BaseModel{ID: 1}, Name: "s"}
	store.reminders = []model.Reminder{{
		BaseModel:   model.BaseModel{ID: 10},
		ScenarioID:  uintPtr(1),
		Title:       "站起来",
		Kind:        model.KindStand,
		IntervalMin: 60,
		WindowStart: "09:00",
		WindowEnd:   "18:00",
		Enabled:     true,
	}}

	machine := restmode.NewMachine()
	notifier := &fakeNotifier{}
	engine := NewEngine(Options{
		Store:    store,
		Now:      func() time.Time { return now },
		Settings: func() Settings { return Settings{NotifyEnabled: true} },
		Notifier: notifier,
		RestMode: machine,
	})
	engine.Tick()

	if machine.State() != restmode.StateCountdown {
		t.Errorf("stand 提醒到点后应进入 Countdown，实际 %s", machine.State())
	}
	cur := machine.Current()
	if cur.Kind != model.KindStand {
		t.Errorf("当前会话 kind 应为 stand，实际 %s", cur.Kind)
	}
	if cur.Duration != 120*time.Second {
		t.Errorf("stand 默认时长应为 120s，实际 %v", cur.Duration)
	}

	// 关键承诺：进入休息模式后【不发系统通知】——覆盖层已经是强提醒，
	// 同一秒再叠一条通知只会让用户看到两件事。
	if len(notifier.sent) != 0 {
		t.Errorf("rest mode 期间不应发系统通知，实际 %d 条", len(notifier.sent))
	}
}

// TestDispatchSkipsRestModeForWaterKind 验证 water 类不进休息模式（US-13）。
func TestDispatchSkipsRestModeForWaterKind(t *testing.T) {
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

	machine := restmode.NewMachine()
	notifier := &fakeNotifier{}
	engine := NewEngine(Options{
		Store:    store,
		Now:      func() time.Time { return now },
		Settings: func() Settings { return Settings{NotifyEnabled: true} },
		Notifier: notifier,
		RestMode: machine,
	})
	engine.Tick()

	if machine.State() != restmode.StateIdle {
		t.Errorf("water 类应保持 Idle，实际 %s", machine.State())
	}
	// water 类应正常发通知。
	if len(notifier.sent) == 0 {
		t.Error("water 类应正常发系统通知")
	}
}

// TestDispatchRestModeConflictFallsBackToNotification 验证休息中收到第二条
// 休息类提醒时，第二条走【普通通知】通道而不是覆盖当前会话。
//
// 【为什么这是关键回归】Machine.Enter 在 Countdown 状态下返回 false；
// dispatch 应该拿这个 false 走通知路径。若代码假设「kind 是休息类就静默」，
// 第二条提醒会彻底消失——用户既看不到覆盖层，也收不到通知。
func TestDispatchRestModeConflictFallsBackToNotification(t *testing.T) {
	now := time.Date(2026, 10, 20, 10, 30, 0, 0, time.Local)
	store := newFakeStore()
	store.scenario = &model.Scenario{BaseModel: model.BaseModel{ID: 1}, Name: "s"}
	store.reminders = []model.Reminder{{
		BaseModel:   model.BaseModel{ID: 10},
		ScenarioID:  uintPtr(1),
		Title:       "站起来",
		Kind:        model.KindStand,
		IntervalMin: 0, // 每日单点：一次 tick 只出一个点
		WindowStart: "10:00",
		WindowEnd:   "18:00",
		Enabled:     true,
	}, {
		BaseModel:   model.BaseModel{ID: 11},
		ScenarioID:  uintPtr(1),
		Title:       "走动",
		Kind:        model.KindMove,
		IntervalMin: 0,
		WindowStart: "10:00",
		WindowEnd:   "18:00",
		Enabled:     true,
	}}

	machine := restmode.NewMachine()
	notifier := &fakeNotifier{}
	engine := NewEngine(Options{
		Store:    store,
		Now:      func() time.Time { return now },
		Settings: func() Settings { return Settings{NotifyEnabled: true} },
		Notifier: notifier,
		RestMode: machine,
	})
	engine.Tick()

	// 两条都是 stand/move 到点；第一条进入 rest，第二条应走通知路径。
	if machine.State() != restmode.StateCountdown {
		t.Fatalf("第一条应进入 Countdown，实际 %s", machine.State())
	}
	if machine.Current().Kind != model.KindStand && machine.Current().Kind != model.KindMove {
		t.Errorf("Machine 当前应是 stand 或 move，实际 %s", machine.Current().Kind)
	}
	// 关键是【至少有一条通知】（另一条走通知路径）。
	// 顺序由 store.reminders 遍历顺序决定；这里只断言总数至少 1。
	if len(notifier.sent) == 0 {
		t.Error("两条 rest-kind 冲突时应有一条走通知路径")
	}
}

// TestDispatchRestModeNilMachineIsNoop 验证未注入 Machine 时不崩、走原路径。
func TestDispatchRestModeNilMachineIsNoop(t *testing.T) {
	now := time.Date(2026, 10, 20, 10, 30, 0, 0, time.Local)
	store := newFakeStore()
	store.scenario = &model.Scenario{BaseModel: model.BaseModel{ID: 1}, Name: "s"}
	store.reminders = []model.Reminder{{
		BaseModel:   model.BaseModel{ID: 10},
		ScenarioID:  uintPtr(1),
		Title:       "站起来",
		Kind:        model.KindStand,
		IntervalMin: 60,
		WindowStart: "09:00",
		WindowEnd:   "18:00",
		Enabled:     true,
	}}

	notifier := &fakeNotifier{}
	// 不注入 RestMode。
	engine := NewEngine(Options{
		Store:    store,
		Now:      func() time.Time { return now },
		Settings: func() Settings { return Settings{NotifyEnabled: true} },
		Notifier: notifier,
	})
	engine.Tick()

	if len(notifier.sent) == 0 {
		t.Error("无 Machine 时 stand 类应走原通知路径")
	}
}
