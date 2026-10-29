package service

import (
	"testing"
	"time"

	"morrow/internal/model"
)

// TestTodayStatsIncludesOneOffWithoutScenario 断言没有启用场景时，
// 首页「下一条」仍能看到最近的一次性提醒。
func TestTodayStatsIncludesOneOffWithoutScenario(t *testing.T) {
	now := time.Date(2026, time.March, 2, 15, 0, 0, 0, time.Local)
	svc := newOneOffTestService(t, now)
	item := createTestOneOff(t, svc, now)

	stats, err := svc.GetTodayStats()
	if err != nil {
		t.Fatalf("读取今日统计失败: %v", err)
	}
	if stats.Scenario != nil {
		t.Fatalf("未启用场景时应返回 nil，实际 %+v", stats.Scenario)
	}
	if stats.NextFireAt != item.OnceAt || stats.NextTitle != item.Title {
		t.Errorf("下一条 = (%d, %q)，期望 (%d, %q)",
			stats.NextFireAt, stats.NextTitle, item.OnceAt, item.Title)
	}
	if stats.PendingCount != 0 {
		t.Errorf("待处理数 = %d，期望 0", stats.PendingCount)
	}
}

// TestTodayStatsPendingCountIncludesOneOff 断言已触发的一次性提醒计入待处理数。
func TestTodayStatsPendingCountIncludesOneOff(t *testing.T) {
	now := time.Date(2026, time.March, 2, 15, 0, 0, 0, time.Local)
	svc := newOneOffTestService(t, now)
	item := createTestOneOff(t, svc, now)
	if _, _, err := svc.ClaimFireWithStatus(item.ID, item.OnceAt, model.FireFired); err != nil {
		t.Fatalf("占用触发失败: %v", err)
	}

	stats, err := svc.GetTodayStats()
	if err != nil {
		t.Fatalf("读取今日统计失败: %v", err)
	}
	if stats.PendingCount != 1 {
		t.Errorf("待处理数 = %d，期望 1", stats.PendingCount)
	}
}

// TestTodayStatsKeepsEarlierOneOff 断言一次性与周期项同时存在时取更早者。
func TestTodayStatsKeepsEarlierOneOff(t *testing.T) {
	now := time.Date(2026, time.March, 2, 15, 0, 0, 0, time.Local)
	svc := newOneOffTestService(t, now)
	scenario, err := svc.CreateScenario("日常", "每小时起身")
	if err != nil {
		t.Fatalf("创建场景失败: %v", err)
	}
	if err := svc.SetScenarioEnabled(scenario.ID, true); err != nil {
		t.Fatalf("启用场景失败: %v", err)
	}
	if _, err := svc.CreateReminder(ReminderInput{
		ScenarioID:  scenario.ID,
		Title:       "起身",
		Kind:        model.KindStand,
		IntervalMin: 60,
		WindowStart: "09:00",
		WindowEnd:   "18:00",
		Enabled:     true,
	}); err != nil {
		t.Fatalf("创建周期提醒失败: %v", err)
	}
	oneOff := createTestOneOff(t, svc, now) // now+1h；周期项下一条也是 16:00，先断言不早于它

	stats, err := svc.GetTodayStats()
	if err != nil {
		t.Fatalf("读取今日统计失败: %v", err)
	}
	if stats.NextFireAt != oneOff.OnceAt {
		t.Errorf("下一条 = %d，期望一次性 %d", stats.NextFireAt, oneOff.OnceAt)
	}

	// 把一次性改到 17:30：周期项 16:00 更早，下一条应回到周期项。
	later := now.Add(2*time.Hour + 30*time.Minute).Unix()
	if err := svc.UpdateReminder(ReminderInput{
		ID:     oneOff.ID,
		Title:  oneOff.Title,
		OnceAt: later,
	}); err != nil {
		t.Fatalf("改期失败: %v", err)
	}
	stats, err = svc.GetTodayStats()
	if err != nil {
		t.Fatalf("再次读取今日统计失败: %v", err)
	}
	wantRecurring := now.Add(time.Hour).Unix()
	if stats.NextFireAt != wantRecurring {
		t.Errorf("下一条 = %d，期望周期项 %d", stats.NextFireAt, wantRecurring)
	}
}

// TestOneOffCompletionDoesNotAffectStatsOrPet 断言一次性完成不进入今日目标、
// 连续天数或宠物生长。
func TestOneOffCompletionDoesNotAffectStatsOrPet(t *testing.T) {
	now := time.Date(2026, time.March, 2, 15, 0, 0, 0, time.Local)
	svc := newOneOffTestService(t, now)
	scenario, err := svc.CreateScenario("日常", "每天喝水")
	if err != nil {
		t.Fatalf("创建场景失败: %v", err)
	}
	if err := svc.SetScenarioEnabled(scenario.ID, true); err != nil {
		t.Fatalf("启用场景失败: %v", err)
	}
	if _, err := svc.CreateReminder(ReminderInput{
		ScenarioID:  scenario.ID,
		Title:       "喝水",
		Kind:        model.KindWater,
		IntervalMin: 60,
		WindowStart: "09:00",
		WindowEnd:   "18:00",
		DailyTarget: 8,
		Enabled:     true,
	}); err != nil {
		t.Fatalf("创建周期提醒失败: %v", err)
	}
	oneOff := createTestOneOff(t, svc, now)
	fireID, claimed, err := svc.ClaimFire(oneOff.ID, oneOff.OnceAt)
	if err != nil || !claimed {
		t.Fatalf("占用触发失败: claimed=%v err=%v", claimed, err)
	}
	if err := svc.CompleteReminder(fireID); err != nil {
		t.Fatalf("完成一次性提醒失败: %v", err)
	}

	stats, err := svc.GetTodayStats()
	if err != nil {
		t.Fatalf("读取今日统计失败: %v", err)
	}
	if stats.DoneTotal != 0 {
		t.Errorf("今日完成 = %d，一次性完成不应计入", stats.DoneTotal)
	}
	if stats.TargetTotal != 8 {
		t.Errorf("今日目标 = %d，期望周期项的 8", stats.TargetTotal)
	}
	if stats.PendingCount != 0 {
		t.Errorf("待处理数 = %d，期望 0", stats.PendingCount)
	}
	if len(stats.Reminders) != 1 || stats.Reminders[0].Streak != 0 {
		t.Errorf("一次性完成不应影响周期项进度/连续天数: %+v", stats.Reminders)
	}

	growth, _, err := svc.PetGrowth(0, false)
	if err != nil {
		t.Fatalf("读取宠物生长失败: %v", err)
	}
	if growth.Stage != GrowthStageSeedling {
		t.Errorf("宠物档位 = %d，一次性完成不应改变它", growth.Stage)
	}
}
