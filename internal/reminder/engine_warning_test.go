package reminder

import (
	"testing"
	"time"

	"morrow/internal/model"
	"morrow/internal/restmode"
)

// TestTickEntersWarningBeforeFire 验证到点前 60 秒 Machine 进入 Warning。
func TestTickEntersWarningBeforeFire(t *testing.T) {
	// now=10:00:30，下一个计划点 10:01:00（30 秒后，落在 60 秒预警窗口内）。
	// 【为什么不用 10:00:30 作为 fire】FirePoints 只产出整分钟对齐的点；
	// 若把 now 放在整分钟之前，同一 tick 会先把该点 dispatch 掉（进入 Countdown），
	// 就测不到 Warning 了。
	now := time.Date(2026, 10, 20, 10, 0, 30, 0, time.Local)
	nextPoint := time.Date(2026, 10, 20, 10, 1, 0, 0, time.Local)

	store := newFakeStore()
	store.scenario = &model.Scenario{BaseModel: model.BaseModel{ID: 1}, Name: "s"}
	store.reminders = []model.Reminder{{
		BaseModel:   model.BaseModel{ID: 10},
		ScenarioID:  uintPtr(1),
		Title:       "站起来",
		Kind:        model.KindStand,
		IntervalMin: 60,
		WindowStart: fmtWindow(nextPoint),
		WindowEnd:   "23:00",
		Enabled:     true,
	}}

	machine := restmode.NewMachineWithClock(func() time.Time { return now })
	engine := NewEngine(Options{
		Store:    store,
		Now:      func() time.Time { return now },
		Settings: func() Settings { return Settings{NotifyEnabled: false} },
		RestMode: machine,
	})
	engine.Tick()

	if machine.State() != restmode.StateWarning {
		t.Fatalf("距离 fire 30 秒时应进入 Warning，实际 %s", machine.State())
	}
	snap := machine.Snapshot()
	if snap.Kind != model.KindStand {
		t.Errorf("预警快照 kind 应为 stand，实际 %s", snap.Kind)
	}
	if snap.CueText == "" {
		t.Error("预警快照应带 CueText（气泡文案）")
	}
	if snap.RemainingSec <= 0 || snap.RemainingSec > 60 {
		t.Errorf("RemainingSec 应在 1–60 秒之间，实际 %d", snap.RemainingSec)
	}
}

// TestTickNoWarningWhenFireTooFar 验证 fire 距离 >60 秒时不预警。
func TestTickNoWarningWhenFireTooFar(t *testing.T) {
	// now=10:00:30，下一个计划点 10:02:00（90 秒后，超出预警窗口）。
	now := time.Date(2026, 10, 20, 10, 0, 30, 0, time.Local)
	nextPoint := time.Date(2026, 10, 20, 10, 2, 0, 0, time.Local)
	store := newFakeStore()
	store.scenario = &model.Scenario{BaseModel: model.BaseModel{ID: 1}, Name: "s"}
	store.reminders = []model.Reminder{{
		BaseModel:   model.BaseModel{ID: 10},
		ScenarioID:  uintPtr(1),
		Title:       "站起来",
		Kind:        model.KindStand,
		IntervalMin: 60,
		WindowStart: fmtWindow(nextPoint),
		WindowEnd:   "23:00",
		Enabled:     true,
	}}

	machine := restmode.NewMachineWithClock(func() time.Time { return now })
	engine := NewEngine(Options{
		Store:    store,
		Now:      func() time.Time { return now },
		Settings: func() Settings { return Settings{NotifyEnabled: false} },
		RestMode: machine,
	})
	engine.Tick()

	if machine.State() != restmode.StateIdle {
		t.Errorf("fire 距离 >60 秒时不应预警，实际状态 %s", machine.State())
	}
}

// TestTickSkipsWarningForWaterKind 验证 water 类不触发预警（US-13 语义一致）。
func TestTickSkipsWarningForWaterKind(t *testing.T) {
	now := time.Date(2026, 10, 20, 10, 0, 30, 0, time.Local)
	nextPoint := time.Date(2026, 10, 20, 10, 1, 0, 0, time.Local)
	store := newFakeStore()
	store.scenario = &model.Scenario{BaseModel: model.BaseModel{ID: 1}, Name: "s"}
	store.reminders = []model.Reminder{{
		BaseModel:   model.BaseModel{ID: 10},
		ScenarioID:  uintPtr(1),
		Title:       "喝水",
		Kind:        model.KindWater,
		IntervalMin: 60,
		WindowStart: fmtWindow(nextPoint),
		WindowEnd:   "23:00",
		Enabled:     true,
	}}

	machine := restmode.NewMachineWithClock(func() time.Time { return now })
	engine := NewEngine(Options{
		Store:    store,
		Now:      func() time.Time { return now },
		Settings: func() Settings { return Settings{NotifyEnabled: false} },
		RestMode: machine,
	})
	engine.Tick()

	if machine.State() != restmode.StateIdle {
		t.Errorf("water 类应不触发预警，实际状态 %s", machine.State())
	}
}

// TestTickWarningsCancelWhenNoCandidates 验证提醒项停用后 Warning 会撤销。
//
// 【为什么必须撤销】用户关了 stand 提醒 → 预警的语义（「马上要 fire」）不再成立；
// 不撤销的话宠物会永远挂着「1 分钟后休息」的气泡，产品预期就崩了。
func TestTickWarningsCancelWhenNoCandidates(t *testing.T) {
	machine := restmode.NewMachine()
	machine.EnterWarning(restmode.RestRequest{
		FireID: 1, Kind: model.KindStand, PlannedFireAt: 9999999999,
	})
	if machine.State() != restmode.StateWarning {
		t.Fatal("测试前提：应已处于 Warning")
	}

	// 场景里没有 stand 提醒了。
	store := newFakeStore()
	store.scenario = &model.Scenario{BaseModel: model.BaseModel{ID: 1}, Name: "s"}
	store.reminders = []model.Reminder{}
	engine := NewEngine(Options{
		Store:    store,
		Now:      func() time.Time { return time.Now() },
		Settings: func() Settings { return Settings{NotifyEnabled: false} },
		RestMode: machine,
	})
	engine.Tick()

	if machine.State() != restmode.StateIdle {
		t.Errorf("无候选项时应撤销预警，实际 %s", machine.State())
	}
}

// fmtWindow 把 time 转成 HH:MM 供 Reminder.WindowStart 使用。
func fmtWindow(t time.Time) string {
	return t.Format("15:04")
}
