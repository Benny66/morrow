package reminder

import (
	"testing"
	"time"

	"morrow/internal/model"
)

// TestOneOffOutcome 覆盖一次性提醒的三种处置边界。
func TestOneOffOutcome(t *testing.T) {
	onceAt := localTime(2026, time.March, 2, 16, 0)
	cases := []struct {
		name  string
		now   time.Time
		grace time.Duration
		want  OneOffState
	}{
		{"未到点", onceAt.Add(-time.Second), OneOffGrace, OneOffNotDue},
		{"正好到点", onceAt, OneOffGrace, OneOffDue},
		{"容差内", onceAt.Add(59 * time.Second), OneOffGrace, OneOffDue},
		{"正好容差边界", onceAt.Add(60 * time.Second), OneOffGrace, OneOffDue},
		{"超过容差", onceAt.Add(61 * time.Second), OneOffGrace, OneOffMissed},
		{"零容差到点", onceAt, 0, OneOffDue},
		{"零容差过点", onceAt.Add(time.Second), 0, OneOffMissed},
		{"未设置", time.Time{}, OneOffGrace, OneOffNotDue},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := OneOffOutcome(onceAt.Unix(), c.now.Unix(), c.grace); got != c.want {
				t.Errorf("OneOffOutcome(onceAt=%v, now=%v, grace=%v) = %v，期望 %v",
					onceAt, c.now, c.grace, got, c.want)
			}
		})
	}
	if got := OneOffOutcome(0, onceAt.Unix(), OneOffGrace); got != OneOffNotDue {
		t.Errorf("未设置 once_at 时 = %v，期望 %v", got, OneOffNotDue)
	}
}

// newOneOff 构造一条待触发的一次性提醒。
func newOneOff(id uint, onceAt time.Time) model.Reminder {
	return model.Reminder{
		BaseModel: model.BaseModel{ID: id},
		Title:     "吃苹果",
		Content:   "洗一个苹果慢慢吃",
		Kind:      model.KindCustom,
		OnceAt:    onceAt.Unix(),
		Enabled:   true,
	}
}

// TestTickOneOffFiresWithinGrace 断言一次性提醒到点后在容差内触发，
// 即使没有启用任何场景也照常送达。
func TestTickOneOffFiresWithinGrace(t *testing.T) {
	now := localTime(2026, time.March, 2, 16, 0)
	store := newFakeStore()
	store.oneOffs = []model.Reminder{newOneOff(42, now.Add(-30*time.Second))}
	engine, notifier, emitter := newTestEngine(store, now, Settings{NotifyEnabled: true})

	engine.Tick()

	if len(notifier.sent) != 1 {
		t.Fatalf("通知数 = %d，期望 1", len(notifier.sent))
	}
	if len(emitter.fired) != 1 {
		t.Fatalf("事件数 = %d，期望 1", len(emitter.fired))
	}
	if !emitter.fired[0].IsOneOff {
		t.Error("事件载荷未标记一次性提醒")
	}
	if emitter.fired[0].Title != "吃苹果" {
		t.Errorf("事件标题 = %q，期望 吃苹果", emitter.fired[0].Title)
	}
	if store.settled[1] != model.FireFired {
		t.Errorf("触发状态 = %q，期望 %q", store.settled[1], model.FireFired)
	}
}

// TestTickOneOffMissedBeyondGrace 断言超过容差后只记 missed，不通知不广播。
func TestTickOneOffMissedBeyondGrace(t *testing.T) {
	now := localTime(2026, time.March, 2, 16, 2)
	store := newFakeStore()
	store.oneOffs = []model.Reminder{newOneOff(42, now.Add(-61*time.Second))}
	engine, notifier, emitter := newTestEngine(store, now, Settings{NotifyEnabled: true})

	engine.Tick()

	if len(notifier.sent) != 0 {
		t.Errorf("错过的一次性提醒不应通知，实际 %d 条", len(notifier.sent))
	}
	if len(emitter.fired) != 0 {
		t.Errorf("错过的一次性提醒不应广播，实际 %d 条", len(emitter.fired))
	}
	if len(emitter.changed) != 1 {
		t.Errorf("错过状态变更应广播 1 条 changed，实际 %d 条", len(emitter.changed))
	}
	if store.initial[1] != model.FireMissed {
		t.Errorf("fire 初始状态 = %q，期望 %q", store.initial[1], model.FireMissed)
	}
}

// TestTickOneOffBypassesQuietHours 断言一次性提醒不受静默时段限制。
func TestTickOneOffBypassesQuietHours(t *testing.T) {
	now := localTime(2026, time.March, 2, 23, 0)
	store := newFakeStore()
	store.oneOffs = []model.Reminder{newOneOff(42, now)}
	settings := Settings{QuietStart: "22:00", QuietEnd: "08:00", NotifyEnabled: true}
	engine, notifier, emitter := newTestEngine(store, now, settings)

	engine.Tick()

	if len(notifier.sent) != 1 {
		t.Fatalf("静默时段内一次性提醒应通知，实际 %d 条", len(notifier.sent))
	}
	if len(emitter.fired) != 1 {
		t.Fatalf("静默时段内一次性提醒应广播，实际 %d 条", len(emitter.fired))
	}
}

// TestTickOneOffDoesNotRepeat 断言重复 tick 不会重复触发。
func TestTickOneOffDoesNotRepeat(t *testing.T) {
	now := localTime(2026, time.March, 2, 16, 0)
	store := newFakeStore()
	store.oneOffs = []model.Reminder{newOneOff(42, now.Add(-10*time.Second))}
	engine, notifier, emitter := newTestEngine(store, now, Settings{NotifyEnabled: true})

	engine.Tick()
	engine.Tick()

	if len(notifier.sent) != 1 || len(emitter.fired) != 1 {
		t.Errorf("重复 tick 后通知/事件数 = %d/%d，期望 1/1", len(notifier.sent), len(emitter.fired))
	}
}

// TestTickSnoozedOneOffBypassesQuietHours 断言一次性提醒的推迟补发也不受静默限制。
func TestTickSnoozedOneOffBypassesQuietHours(t *testing.T) {
	now := localTime(2026, time.March, 2, 23, 0)
	store := newFakeStore()
	store.snoozes = []DueFire{{
		FireID:     99,
		ReminderID: 42,
		Title:      "吃苹果",
		Kind:       model.KindCustom,
		FireAt:     now.Add(-time.Minute).Unix(),
		IsOneOff:   true,
	}}
	settings := Settings{QuietStart: "22:00", QuietEnd: "08:00", NotifyEnabled: true}
	engine, notifier, emitter := newTestEngine(store, now, settings)

	engine.Tick()

	if len(notifier.sent) != 1 || len(emitter.fired) != 1 {
		t.Errorf("推迟补发应绕过静默，通知/事件数 = %d/%d", len(notifier.sent), len(emitter.fired))
	}
}
