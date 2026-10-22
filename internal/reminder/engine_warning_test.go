package reminder

import (
	"testing"
	"time"

	"morrow/internal/model"
	"morrow/internal/pause"
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

// standWarningStore 构造一条「10:00 到点、每 10 分钟一条」的 stand 提醒。
//
// 【为什么窗口起点就等于第一个触发点】沿用 TestTickEntersWarningBeforeFire 的办法：
// 当天没有更早的积压点，测试里就不会出现「先顺延 09:50 那一条」这种无关干扰。
func standWarningStore(firstFire time.Time) *fakeStore {
	store := newFakeStore()
	store.scenario = &model.Scenario{BaseModel: model.BaseModel{ID: 1}, Name: "s"}
	store.reminders = []model.Reminder{{
		BaseModel:   model.BaseModel{ID: 10},
		ScenarioID:  uintPtr(1),
		Title:       "起身活动",
		Kind:        model.KindStand,
		IntervalMin: 10,
		WindowStart: fmtWindow(firstFire),
		WindowEnd:   "23:00",
		Enabled:     true,
	}}
	return store
}

// TestWarningClearedAfterQuietDelivery 用户持续打字把提醒推到「顺延到上限 → 安静投递」后，
// 预警必须在下一轮 tick 撤销——安静投递永远不进休息模式，预警继续挂着就是在说谎。
//
// 【时间线照实机事故排】10:00 到点、用户一直在输入：顺延 30 秒到上限，
// 10:00:40 转安静投递；10:01:00 那一轮预警必须已经消失。
func TestWarningClearedAfterQuietDelivery(t *testing.T) {
	fire := time.Date(2026, 10, 22, 10, 0, 0, 0, time.Local)
	now := fire.Add(-30 * time.Second)

	store := standWarningStore(fire)
	machine := restmode.NewMachineWithClock(func() time.Time { return now })
	// idle=0 且平台支持 = 用户正在敲键盘。
	activity := &fakeActivity{idle: 0, supported: true}
	settings := Settings{
		NotifyEnabled:          false,
		NaturalPauseEnabled:    true,
		NaturalPauseMaxSeconds: 30,
	}
	engine := NewEngine(Options{
		Store:    store,
		Now:      func() time.Time { return now },
		Settings: func() Settings { return settings },
		RestMode: machine,
		Activity: activity,
	})

	engine.Tick()
	if machine.State() != restmode.StateWarning {
		t.Fatalf("测试前提：距 fire 30 秒时应已预警，实际 %s", machine.State())
	}

	// 推到顺延到上限之后：安静投递已经发生，触发点已落定。
	for _, step := range []time.Duration{0, 20 * time.Second, 40 * time.Second, time.Minute} {
		now = fire.Add(step)
		engine.Tick()
	}

	if _, claimed := store.claimed[fireKey(10, fire.Unix())]; !claimed {
		t.Fatal("测试前提：10:00 这个触发点应已被安静投递占用")
	}
	if machine.State() != restmode.StateIdle {
		t.Errorf("安静投递后预警应撤销，实际 %s（当前快照 %+v）", machine.State(), machine.Snapshot())
	}
}

// TestWarningClearedWhenReminderDisabled 预警发出后用户把那条提醒停用（或删除、或切走场景），
// 那次触发永远不会再落定——预警必须撤销，不能挂着「1 分钟后」等一个不再存在的 fire。
//
// 【为什么单独一条】它和安静投递是两种失效原因：那条有 fire 记录，这条永远不会有。
// 既有的 TestTickWarningsCancelWhenNoCandidates 用的是未来的 PlannedFireAt，
// 目标时刻已过的形态它盖不住（保活分支会把它留在 warning）。
func TestWarningClearedWhenReminderDisabled(t *testing.T) {
	fire := time.Date(2026, 10, 22, 10, 0, 0, 0, time.Local)
	now := fire.Add(-30 * time.Second)

	store := standWarningStore(fire)
	machine := restmode.NewMachineWithClock(func() time.Time { return now })
	engine := NewEngine(Options{
		Store:    store,
		Now:      func() time.Time { return now },
		Settings: func() Settings { return Settings{NotifyEnabled: false} },
		RestMode: machine,
	})

	engine.Tick()
	if machine.State() != restmode.StateWarning {
		t.Fatalf("测试前提：应已预警，实际 %s", machine.State())
	}

	// 用户在到点前关掉那条提醒：ActiveReminders 不再返回它。
	store.reminders = nil
	now = fire.Add(20 * time.Second)
	engine.Tick()

	if machine.State() != restmode.StateIdle {
		t.Errorf("提醒项已停用且目标时刻已过时预警应撤销，实际 %s（当前快照 %+v）",
			machine.State(), machine.Snapshot())
	}
}

// TestWarningSurvivesWhileDeferring 用户还在输入、那次触发只是顺延（并未落定）时，
// 预警要继续挂着——顺延不改变「下一分钟真的会弹」的承诺。
func TestWarningSurvivesWhileDeferring(t *testing.T) {
	fire := time.Date(2026, 10, 22, 10, 0, 0, 0, time.Local)
	now := fire.Add(-30 * time.Second)

	store := standWarningStore(fire)
	machine := restmode.NewMachineWithClock(func() time.Time { return now })
	activity := &fakeActivity{idle: 0, supported: true}
	settings := Settings{
		NotifyEnabled:          false,
		NaturalPauseEnabled:    true,
		NaturalPauseMaxSeconds: 30,
	}
	engine := NewEngine(Options{
		Store:    store,
		Now:      func() time.Time { return now },
		Settings: func() Settings { return settings },
		RestMode: machine,
		Activity: activity,
	})

	engine.Tick()
	// 只推到顺延上限之前：触发点仍未被占用。
	for _, step := range []time.Duration{0, 10 * time.Second, 20 * time.Second} {
		now = fire.Add(step)
		engine.Tick()
	}

	if _, claimed := store.claimed[fireKey(10, fire.Unix())]; claimed {
		t.Fatal("测试前提：顺延期间触发点不该被占用")
	}
	if machine.State() != restmode.StateWarning {
		t.Errorf("顺延期间预警应保留，实际 %s", machine.State())
	}
}

// TestWarningAtFireTickEntersCountdown 时钟恰好走到计划时刻的那一轮，正常路径必须直接
// 进入倒计时——预警修好之后不能把这条主路径一起弄丢。
//
// 【这条不锁 tickWarnings 与 tickGrid 的先后顺序】两次注入实验说明它锁不住：
// 即便这一轮先把预警撤销，tickGrid 随后 dispatch 时 Enter 也接受 idle，终态仍是 countdown。
// 顺序其实不是新判据的前提——判据只看「那次触发有没有被占用」，谁先跑读到的都是同一个事实。
func TestWarningAtFireTickEntersCountdown(t *testing.T) {
	fire := time.Date(2026, 10, 22, 10, 0, 0, 0, time.Local)
	now := fire.Add(-30 * time.Second)

	store := standWarningStore(fire)
	machine := restmode.NewMachineWithClock(func() time.Time { return now })
	engine := NewEngine(Options{
		Store:    store,
		Now:      func() time.Time { return now },
		Settings: func() Settings { return Settings{NotifyEnabled: false} },
		RestMode: machine,
	})

	engine.Tick()
	if machine.State() != restmode.StateWarning {
		t.Fatalf("测试前提：应已预警，实际 %s", machine.State())
	}

	// 恰好到点：这一轮的终态必须是倒计时，中间不许出现 idle。
	now = fire
	engine.Tick()
	if got := machine.State(); got != restmode.StateCountdown {
		t.Errorf("到点那一轮应进入倒计时，实际 %s", got)
	}
}

// TestWarningCanBeRaisedAgainAfterCancel 预警被撤销之后，下一条进入 60 秒窗口的
// rest 提醒仍要能正常预警——僵尸态挡路是这次事故里最隐蔽的一半。
func TestWarningCanBeRaisedAgainAfterCancel(t *testing.T) {
	fire := time.Date(2026, 10, 22, 10, 0, 0, 0, time.Local)
	next := fire.Add(10 * time.Minute)
	now := fire.Add(-30 * time.Second)

	store := standWarningStore(fire)
	machine := restmode.NewMachineWithClock(func() time.Time { return now })
	activity := &fakeActivity{idle: 0, supported: true}
	settings := Settings{
		NotifyEnabled:          false,
		NaturalPauseEnabled:    true,
		NaturalPauseMaxSeconds: 30,
	}
	engine := NewEngine(Options{
		Store:    store,
		Now:      func() time.Time { return now },
		Settings: func() Settings { return settings },
		RestMode: machine,
		Activity: activity,
	})

	// 第一条：安静投递收场，预警应当被撤销。
	for _, step := range []time.Duration{-30 * time.Second, 0, 40 * time.Second, time.Minute} {
		now = fire.Add(step)
		engine.Tick()
	}
	if machine.State() != restmode.StateIdle {
		t.Fatalf("测试前提：第一条预警应已撤销，实际 %s", machine.State())
	}

	// 第二条进入 60 秒窗口：应能重新预警，且剩余时间是真实的。
	now = next.Add(-40 * time.Second)
	engine.Tick()
	snap := machine.Snapshot()
	if snap.State != restmode.StateWarning {
		t.Fatalf("第二条提醒进入窗口时应重新预警，实际 %s", snap.State)
	}
	if snap.RemainingSec != 40 {
		t.Errorf("重新预警的剩余秒数应为 40，实际 %d", snap.RemainingSec)
	}
}

// TestWarningClearedWithinOneTickAfterMemoCleared 规格 §7 记下的已知代价：
// "是否已占用"读的是引擎内存备忘，跨自然日时被 rollDay 清空，此时判据会把已在 DB
// 里落定过的触发点当成"仍待投递"，撤销因此晚一个 tick（≤20 秒），随后自愈。
//
// 【为什么用 store.claimed 预填而不是改内部字段】它正好模拟"DB 有行、内存备忘为空"
// 这一唯一能造出该尾部的组合：DB 是真相，内存备忘只是加速。
// 【尾部为什么真有界】能跨过日切的预警只有一种形态：生效时段起点是 00:00 且触发点
// 正好落在 00:00（时段终点必须晚于起点，所以不会有跨午夜的点）。而 00:00 这个点在日切后
// 仍属于"今天"的 FirePoints，tickGrid 会重放它并写回备忘——于是最多晚一个 tick，不会永久挂住。
// 这条用例不是在庆祝这个缺陷，而是把上面这段推理钉成事实：如果哪天它变成永久挂住，红的是这条。
func TestWarningClearedWithinOneTickAfterMemoCleared(t *testing.T) {
	fire := time.Date(2026, 10, 22, 10, 0, 0, 0, time.Local)
	now := fire.Add(-30 * time.Second)

	store := standWarningStore(fire)
	// DB 里这个点早已落定（模拟跨日前就投递过），但引擎备忘是空的。
	store.claimed[fireKey(10, fire.Unix())] = true

	machine := restmode.NewMachineWithClock(func() time.Time { return now })
	engine := NewEngine(Options{
		Store:    store,
		Now:      func() time.Time { return now },
		Settings: func() Settings { return Settings{NotifyEnabled: false} },
		RestMode: machine,
	})

	engine.Tick()
	if machine.State() != restmode.StateWarning {
		t.Fatalf("测试前提：应已预警，实际 %s", machine.State())
	}

	// 第一个 tick：备忘还没有这条，判据认为仍待投递 → 保留；tickGrid 重放该点后写回备忘。
	now = fire.Add(20 * time.Second)
	engine.Tick()
	if machine.State() != restmode.StateWarning {
		t.Fatalf("备忘为空的首轮应只是晚一个 tick 撤销，实际 %s", machine.State())
	}
	if !e2Claimed(engine, 10, fire.Unix()) {
		t.Fatal("测试前提：tickGrid 重放历史点后必须写回内存备忘，否则下一轮仍撤不掉")
	}

	// 第二个 tick：撤销。
	now = fire.Add(40 * time.Second)
	engine.Tick()
	if machine.State() != restmode.StateIdle {
		t.Errorf("备忘补回后的下一轮应撤销预警（尾部不超过一个 tick），实际 %s", machine.State())
	}
}

// e2Claimed 读引擎的内存备忘，用于断言"重放历史点会写回备忘"这一自愈前提。
func e2Claimed(e *Engine, reminderID uint, fireAt int64) bool {
	return e.isClaimed(reminderID, fireAt)
}

// 【窗口起点刻意等于第一个触发点】否则 21:00–21:50 那一串积压点会在静默开始之前
// 就被正常投递，测试要断言的东西就被无关的点了污染。
func TestWarningClearedDuringQuietHours(t *testing.T) {
	fire := time.Date(2026, 10, 22, 22, 0, 0, 0, time.Local)
	now := fire.Add(-30 * time.Second)

	store := standWarningStore(fire) // 生效时段 22:00–23:00，第一个点正好是 22:00
	machine := restmode.NewMachineWithClock(func() time.Time { return now })
	settings := Settings{NotifyEnabled: false, QuietStart: "22:00", QuietEnd: "08:00"}
	engine := NewEngine(Options{
		Store:    store,
		Now:      func() time.Time { return now },
		Settings: func() Settings { return settings },
		RestMode: machine,
	})

	engine.Tick()
	if machine.State() != restmode.StateWarning {
		t.Fatalf("测试前提：应已预警，实际 %s", machine.State())
	}

	now = fire
	engine.Tick()
	if _, claimed := store.claimed[fireKey(10, fire.Unix())]; !claimed {
		t.Fatal("测试前提：静默时段到点应已占用该触发点（只记录不打扰）")
	}

	now = fire.Add(20 * time.Second)
	engine.Tick()
	if machine.State() != restmode.StateIdle {
		t.Errorf("静默时段落定后预警应撤销，实际 %s", machine.State())
	}
}

// TestWarningClearedAfterRealBreak 用户刚真的休息过，紧接着到点的 stand/move 被静默跳过
// （不投递、不算错过）；那次触发已落定，预警不该继续挂着。
func TestWarningClearedAfterRealBreak(t *testing.T) {
	fire := time.Date(2026, 10, 22, 10, 0, 0, 0, time.Local)
	now := fire.Add(-30 * time.Second)

	store := standWarningStore(fire)
	machine := restmode.NewMachineWithClock(func() time.Time { return now })
	engine := NewEngine(Options{
		Store:         store,
		Now:           func() time.Time { return now },
		Settings:      func() Settings { return Settings{NotifyEnabled: false} },
		RestMode:      machine,
		BreakProvider: &fakeBreakProvider{skip: true},
	})

	engine.Tick()
	if machine.State() != restmode.StateWarning {
		t.Fatalf("测试前提：应已预警，实际 %s", machine.State())
	}

	// 到点那一轮被静默跳过；下一轮预警必须消失。
	now = fire
	engine.Tick()
	now = fire.Add(20 * time.Second)
	engine.Tick()

	if machine.State() != restmode.StateIdle {
		t.Errorf("真实休息后跳过的触发点已落定，预警应撤销，实际 %s", machine.State())
	}
}

// TestWarningClearedWhenPaused 预警发出后用户锁屏 / 进会议：暂停本身就是「此刻不打扰」，
// 预警必须在同一轮撤销，不等那次触发落定。
//
// 【为什么不能只靠「落定即撤销」】暂停发生在到点**之前**（09:59:40 锁屏，10:00 那个点
// 还没到），不会有任何终态被写出；而暂停期间整轮不跑 tickWarnings，基于时间的宽限上限
// 也没机会执行——这正是 2026-10-22 实机挂住 23 分钟的形态（17:15 那条 stand 落在锁屏里）。
func TestWarningClearedWhenPaused(t *testing.T) {
	fire := time.Date(2026, 10, 22, 10, 0, 0, 0, time.Local)
	now := fire.Add(-30 * time.Second)

	store := standWarningStore(fire)
	machine := restmode.NewMachineWithClock(func() time.Time { return now })
	provider := &mockPauseProvider{state: pause.State{Reason: pause.ReasonNone}}
	engine := NewEngine(Options{
		Store:         store,
		Now:           func() time.Time { return now },
		Settings:      func() Settings { return Settings{NotifyEnabled: false} },
		RestMode:      machine,
		PauseProvider: provider,
	})

	engine.Tick()
	if machine.State() != restmode.StateWarning {
		t.Fatalf("测试前提：应已预警，实际 %s", machine.State())
	}

	provider.state = pause.State{Reason: pause.ReasonLock, Since: now.Add(-10 * time.Second)}
	now = fire.Add(-20 * time.Second)
	engine.Tick()

	if machine.State() != restmode.StateIdle {
		t.Errorf("进入暂停后预警应立即撤销，实际 %s（快照 %+v）", machine.State(), machine.Snapshot())
	}
	if _, claimed := store.claimed[fireKey(10, fire.Unix())]; claimed {
		t.Error("测试前提被破坏：暂停发生在到点之前，那次触发不该已被占用")
	}
}

// TestNoWarningRaisedWhilePaused 暂停期间不产生新预警——一条「1 分钟后休息」的气泡
// 对正在共享屏幕的用户没有意义。
func TestNoWarningRaisedWhilePaused(t *testing.T) {
	fire := time.Date(2026, 10, 22, 10, 0, 0, 0, time.Local)
	now := fire.Add(-30 * time.Second)

	store := standWarningStore(fire)
	machine := restmode.NewMachineWithClock(func() time.Time { return now })
	provider := &mockPauseProvider{
		state: pause.State{Reason: pause.ReasonMeeting, Since: now.Add(-time.Minute)},
	}
	engine := NewEngine(Options{
		Store:         store,
		Now:           func() time.Time { return now },
		Settings:      func() Settings { return Settings{NotifyEnabled: false} },
		RestMode:      machine,
		PauseProvider: provider,
	})

	engine.Tick()

	if machine.State() != restmode.StateIdle {
		t.Errorf("暂停期间不应新建预警，实际 %s", machine.State())
	}
}

// TestWarningRescannedAfterPauseResumes 暂停解除后按当时真实的 60 秒窗口重新判断：
// 窗口内还有待投递的触发就重新预警。
//
// 【为什么单列】它证明「暂停立即撤销」没把预警能力一起关掉：撤销回到 idle，
// 下一轮扫描才可能重新 EnterWarning（idle-only）。
func TestWarningRescannedAfterPauseResumes(t *testing.T) {
	fire := time.Date(2026, 10, 22, 10, 0, 0, 0, time.Local)
	now := fire.Add(-30 * time.Second)

	store := standWarningStore(fire)
	machine := restmode.NewMachineWithClock(func() time.Time { return now })
	provider := &mockPauseProvider{state: pause.State{Reason: pause.ReasonLock, Since: now}}
	engine := NewEngine(Options{
		Store:         store,
		Now:           func() time.Time { return now },
		Settings:      func() Settings { return Settings{NotifyEnabled: false} },
		RestMode:      machine,
		PauseProvider: provider,
	})

	engine.Tick()
	if machine.State() != restmode.StateIdle {
		t.Fatalf("暂停期间不该有预警，实际 %s", machine.State())
	}

	// 解锁：此刻距 10:00 还有 20 秒，落在窗口内 → 应重新预警。
	provider.state = pause.State{Reason: pause.ReasonNone}
	now = fire.Add(-20 * time.Second)
	engine.Tick()

	snap := machine.Snapshot()
	if snap.State != restmode.StateWarning {
		t.Fatalf("恢复后窗口内仍有待投递触发时应重新预警，实际 %s", snap.State)
	}
	if snap.RemainingSec != 20 {
		t.Errorf("重新预警的剩余秒数应为 20，实际 %d", snap.RemainingSec)
	}
}
