package service

import (
	"testing"
	"time"

	"morrow/internal/model"
)

func newOneOffTestService(t *testing.T, now time.Time) *Service {
	t.Helper()
	svc := newReminderTestService(t)
	svc.now = func() time.Time { return now }
	return svc
}

func createTestOneOff(t *testing.T, svc *Service, now time.Time) *model.Reminder {
	t.Helper()
	item, err := svc.CreateReminder(ReminderInput{
		Title:       "吃苹果",
		Content:     "洗一个苹果慢慢吃",
		Kind:        model.KindCustom,
		OnceAt:      now.Add(time.Hour).Unix(),
		IntervalMin: 60,
		DailyTarget: 8,
		Enabled:     false,
	})
	if err != nil {
		t.Fatalf("创建一次性提醒失败: %v", err)
	}
	return item
}

// TestCreateOneOffWithoutScenario 断言一次性提醒无需场景，且间隔/目标被强制归零。
func TestCreateOneOffWithoutScenario(t *testing.T) {
	now := time.Date(2026, time.March, 2, 15, 0, 0, 0, time.Local)
	svc := newOneOffTestService(t, now)
	item := createTestOneOff(t, svc, now)

	if item.ScenarioID != nil {
		t.Errorf("一次性提醒不应有场景，实际 %v", *item.ScenarioID)
	}
	if item.OnceAt != now.Add(time.Hour).Unix() {
		t.Errorf("once_at = %d，期望 %d", item.OnceAt, now.Add(time.Hour).Unix())
	}
	if item.IntervalMin != 0 || item.DailyTarget != 0 {
		t.Errorf("一次性提醒的间隔/目标应归零，实际 %d/%d", item.IntervalMin, item.DailyTarget)
	}
	if !item.Enabled {
		t.Error("一次性提醒应始终启用")
	}

	list, err := svc.ListOneOffs()
	if err != nil {
		t.Fatalf("查询一次性提醒失败: %v", err)
	}
	if len(list) != 1 || list[0].Missed || list[0].ID != item.ID {
		t.Fatalf("即将到来列表不符: %+v", list)
	}
}

// TestCreateOneOffRejectsPast 断言过去时间在写入口被拒绝。
func TestCreateOneOffRejectsPast(t *testing.T) {
	now := time.Date(2026, time.March, 2, 15, 0, 0, 0, time.Local)
	svc := newOneOffTestService(t, now)
	_, err := svc.CreateReminder(ReminderInput{
		Title:  "过去",
		OnceAt: now.Add(-time.Second).Unix(),
	})
	if err == nil {
		t.Fatal("过去时间应被拒绝")
	}
}

// TestCreateOneOffRejectsScenario 断言一次性提醒不能被挂到场景上。
func TestCreateOneOffRejectsScenario(t *testing.T) {
	now := time.Date(2026, time.March, 2, 15, 0, 0, 0, time.Local)
	svc := newOneOffTestService(t, now)
	_, err := svc.CreateReminder(ReminderInput{
		Title:      "带场景",
		ScenarioID: 1,
		OnceAt:     now.Add(time.Hour).Unix(),
	})
	if err == nil {
		t.Fatal("一次性提醒带场景应被拒绝")
	}
}

// TestDueOneOffsExcludesClaimed 断言到点查询只返回尚未产生 fire 的项。
func TestDueOneOffsExcludesClaimed(t *testing.T) {
	now := time.Date(2026, time.March, 2, 15, 0, 0, 0, time.Local)
	svc := newOneOffTestService(t, now)
	item := createTestOneOff(t, svc, now)

	due, err := svc.DueOneOffs(now.Add(2 * time.Hour).Unix())
	if err != nil {
		t.Fatalf("查询到点项失败: %v", err)
	}
	if len(due) != 1 || due[0].ID != item.ID {
		t.Fatalf("到点项不符: %+v", due)
	}

	if _, claimed, err := svc.ClaimFireWithStatus(item.ID, item.OnceAt, model.FireFired); err != nil || !claimed {
		t.Fatalf("占用触发失败: claimed=%v err=%v", claimed, err)
	}
	due, err = svc.DueOneOffs(now.Add(2 * time.Hour).Unix())
	if err != nil {
		t.Fatalf("再次查询到点项失败: %v", err)
	}
	if len(due) != 0 {
		t.Errorf("已产生 fire 的项仍出现在到点队列: %+v", due)
	}
}

// TestListOneOffsShowsMissed 断言错过的项仍可见并带 missed 标记。
func TestListOneOffsShowsMissed(t *testing.T) {
	now := time.Date(2026, time.March, 2, 15, 0, 0, 0, time.Local)
	svc := newOneOffTestService(t, now)
	item := createTestOneOff(t, svc, now)
	if _, _, err := svc.ClaimFireWithStatus(item.ID, item.OnceAt, model.FireMissed); err != nil {
		t.Fatalf("写入 missed 记录失败: %v", err)
	}

	list, err := svc.ListOneOffs()
	if err != nil {
		t.Fatalf("查询一次性提醒失败: %v", err)
	}
	if len(list) != 1 || !list[0].Missed {
		t.Fatalf("错过项未正确展示: %+v", list)
	}
}

// TestOneOffPendingFireHasNoScenario 断言待处理列表能返回无场景的一次性项。
func TestOneOffPendingFireHasNoScenario(t *testing.T) {
	now := time.Date(2026, time.March, 2, 15, 0, 0, 0, time.Local)
	svc := newOneOffTestService(t, now)
	item := createTestOneOff(t, svc, now)
	if _, _, err := svc.ClaimFireWithStatus(item.ID, item.OnceAt, model.FireFired); err != nil {
		t.Fatalf("占用触发失败: %v", err)
	}

	list, err := svc.ListPendingFires()
	if err != nil {
		t.Fatalf("查询待处理失败: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("待处理条数 = %d，期望 1", len(list))
	}
	got := list[0]
	if !got.IsOneOff {
		t.Error("待处理项未标记一次性")
	}
	if got.ScenarioID != 0 || got.ScenarioName != "" {
		t.Errorf("一次性项不应带场景，实际 id=%d name=%q", got.ScenarioID, got.ScenarioName)
	}
	if got.FireAt != item.OnceAt {
		t.Errorf("fire_at = %d，期望 %d", got.FireAt, item.OnceAt)
	}
}

// TestOneOffCompletionSkipsCheckIn 断言一次性项的动作只改 fire，不写统计。
func TestOneOffCompletionSkipsCheckIn(t *testing.T) {
	now := time.Date(2026, time.March, 2, 15, 0, 0, 0, time.Local)
	svc := newOneOffTestService(t, now)
	item := createTestOneOff(t, svc, now)
	fireID, claimed, err := svc.ClaimFire(item.ID, item.OnceAt)
	if err != nil || !claimed {
		t.Fatalf("占用触发失败: claimed=%v err=%v", claimed, err)
	}
	if err := svc.CompleteReminder(fireID); err != nil {
		t.Fatalf("完成一次性提醒失败: %v", err)
	}

	var fire model.ReminderFire
	if err := svc.db.First(&fire, fireID).Error; err != nil {
		t.Fatalf("查询触发记录失败: %v", err)
	}
	if fire.Status != model.FireDone {
		t.Errorf("fire 状态 = %q，期望 %q", fire.Status, model.FireDone)
	}
	var count int64
	if err := svc.db.Model(&model.CheckIn{}).Count(&count).Error; err != nil {
		t.Fatalf("统计打卡失败: %v", err)
	}
	if count != 0 {
		t.Errorf("一次性提醒不应写打卡记录，实际 %d 条", count)
	}
	list, err := svc.ListOneOffs()
	if err != nil {
		t.Fatalf("查询一次性提醒失败: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("已完成的一次性提醒仍出现在即将到来: %+v", list)
	}
}

// TestOneOffSnoozeIsDeliverableWithoutScenario 断言推迟查询能捞出无场景的一次性项。
func TestOneOffSnoozeIsDeliverableWithoutScenario(t *testing.T) {
	now := time.Date(2026, time.March, 2, 15, 0, 0, 0, time.Local)
	svc := newOneOffTestService(t, now)
	item := createTestOneOff(t, svc, now)
	fireID, claimed, err := svc.ClaimFire(item.ID, item.OnceAt)
	if err != nil || !claimed {
		t.Fatalf("占用触发失败: claimed=%v err=%v", claimed, err)
	}
	if err := svc.SnoozeReminder(fireID, 5); err != nil {
		t.Fatalf("推迟失败: %v", err)
	}

	due, err := svc.DueSnoozeFires(now.Add(10 * time.Minute).Unix())
	if err != nil {
		t.Fatalf("查询推迟失败: %v", err)
	}
	if len(due) != 1 {
		t.Fatalf("推迟记录数 = %d，期望 1", len(due))
	}
	if !due[0].IsOneOff {
		t.Error("推迟记录未标记一次性")
	}
	if due[0].ScenarioID != 0 || due[0].ScenarioName != "" {
		t.Errorf("一次性推迟不应带场景，实际 id=%d name=%q", due[0].ScenarioID, due[0].ScenarioName)
	}
}

// TestUpdateOneOffPending 断言未触发的一次性项可以改期。
func TestUpdateOneOffPending(t *testing.T) {
	now := time.Date(2026, time.March, 2, 15, 0, 0, 0, time.Local)
	svc := newOneOffTestService(t, now)
	item := createTestOneOff(t, svc, now)
	newAt := now.Add(3 * time.Hour).Unix()
	if err := svc.UpdateReminder(ReminderInput{
		ID:      item.ID,
		Title:   "吃苹果（改期）",
		OnceAt:  newAt,
		Enabled: true,
	}); err != nil {
		t.Fatalf("改期失败: %v", err)
	}
	var stored model.Reminder
	if err := svc.db.First(&stored, item.ID).Error; err != nil {
		t.Fatalf("查询提醒失败: %v", err)
	}
	if stored.OnceAt != newAt || stored.Title != "吃苹果（改期）" {
		t.Errorf("改期未生效: %+v", stored)
	}
}

// TestUpdateOneOffAfterFireRejected 断言已触发/已错过的一次性项不能改期复活。
func TestUpdateOneOffAfterFireRejected(t *testing.T) {
	now := time.Date(2026, time.March, 2, 15, 0, 0, 0, time.Local)
	svc := newOneOffTestService(t, now)
	item := createTestOneOff(t, svc, now)
	if _, _, err := svc.ClaimFireWithStatus(item.ID, item.OnceAt, model.FireFired); err != nil {
		t.Fatalf("占用触发失败: %v", err)
	}
	err := svc.UpdateReminder(ReminderInput{
		ID:     item.ID,
		Title:  "复活",
		OnceAt: now.Add(3 * time.Hour).Unix(),
	})
	if err == nil {
		t.Fatal("已触发的一次性项改期应被拒绝")
	}
}

// TestSetReminderEnabledRejectsOneOff 断言一次性项没有启停语义。
func TestSetReminderEnabledRejectsOneOff(t *testing.T) {
	now := time.Date(2026, time.March, 2, 15, 0, 0, 0, time.Local)
	svc := newOneOffTestService(t, now)
	item := createTestOneOff(t, svc, now)
	if err := svc.SetReminderEnabled(item.ID, false); err == nil {
		t.Fatal("停用一次性项应被拒绝")
	}
}
