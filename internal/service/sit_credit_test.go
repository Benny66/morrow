package service

import (
	"testing"
	"time"

	"morrow/internal/model"
)

func seedStandScenario(t *testing.T, svc *Service, kind string) (*model.Scenario, *model.Reminder) {
	t.Helper()
	scenario, err := svc.CreateScenario("抵扣验收", "久坐抵扣")
	if err != nil {
		t.Fatalf("创建场景失败: %v", err)
	}
	if err := svc.SetScenarioEnabled(scenario.ID, true); err != nil {
		t.Fatalf("启用场景失败: %v", err)
	}
	item, err := svc.CreateReminder(ReminderInput{
		ScenarioID:  scenario.ID,
		Title:       "起身活动",
		Content:     "站起来走两步",
		Kind:        kind,
		IntervalMin: 60,
		WindowStart: "09:00",
		WindowEnd:   "18:00",
		DailyTarget: 4,
		Enabled:     true,
	})
	if err != nil {
		t.Fatalf("创建提醒失败: %v", err)
	}
	return scenario, item
}

func TestCreditNearestStandMoveFromPending(t *testing.T) {
	svc := newReminderTestService(t)
	now := time.Date(2026, 10, 22, 10, 30, 0, 0, time.Local)
	svc.now = func() time.Time { return now }
	scenario, item := seedStandScenario(t, svc, model.KindStand)
	fireAt := now.Add(-5 * time.Minute).Unix()
	fireID := seedFired(t, svc, item.ID, fireAt)
	session, err := svc.StartSitSession(now.Add(-60 * time.Minute))
	if err != nil {
		t.Fatalf("StartSitSession 失败: %v", err)
	}

	res, err := svc.CreditNearestStandMove(scenario.ID, session.ID, now, model.SourceActiveBreak)
	if err != nil {
		t.Fatalf("CreditNearestStandMove 失败: %v", err)
	}
	if !res.Credited || res.ReminderID != item.ID || res.ReminderTitle != item.Title {
		t.Fatalf("抵扣结果 = %+v，期望命中最接近的 stand", res)
	}

	var fire model.ReminderFire
	if err := svc.db.First(&fire, fireID).Error; err != nil {
		t.Fatalf("查询触发失败: %v", err)
	}
	if fire.Status != model.FireDone {
		t.Errorf("待处理触发终态 = %q，期望 done", fire.Status)
	}
	var checkIn model.CheckIn
	if err := svc.db.First(&checkIn).Error; err != nil {
		t.Fatalf("查询打卡失败: %v", err)
	}
	if checkIn.Source != model.SourceActiveBreak || checkIn.ScheduledAt != fireAt {
		t.Errorf("打卡 = %+v，期望 source=active_break scheduled_at=%d", checkIn, fireAt)
	}
	var stored model.SitSession
	if err := svc.db.First(&stored, session.ID).Error; err != nil {
		t.Fatalf("查询会话失败: %v", err)
	}
	if stored.CreditedReminderID != item.ID || stored.CreditedCheckInID == 0 {
		t.Errorf("会话抵扣字段 = %d/%d，期望 reminder=%d checkin>0", stored.CreditedReminderID, stored.CreditedCheckInID, item.ID)
	}

	// 同一会话第二次抵扣必须被拒绝。
	again, err := svc.CreditNearestStandMove(scenario.ID, session.ID, now, model.SourceLockBreak)
	if err != nil {
		t.Fatalf("第二次抵扣不应报错: %v", err)
	}
	if again.Credited {
		t.Error("同一久坐会话不应重复抵扣")
	}
	var count int64
	if err := svc.db.Model(&model.CheckIn{}).Count(&count).Error; err != nil {
		t.Fatalf("统计打卡失败: %v", err)
	}
	if count != 1 {
		t.Errorf("打卡数 = %d，期望 1", count)
	}

	stats, err := svc.GetTodayStats()
	if err != nil {
		t.Fatalf("GetTodayStats 失败: %v", err)
	}
	if stats.DoneTotal != 1 {
		t.Errorf("今日完成总数 = %d，期望 1", stats.DoneTotal)
	}
}

func TestCreditNearestStandMoveNoPending(t *testing.T) {
	svc := newReminderTestService(t)
	now := time.Date(2026, 10, 22, 10, 30, 0, 0, time.Local)
	svc.now = func() time.Time { return now }
	scenario, item := seedStandScenario(t, svc, model.KindStand)
	session, err := svc.StartSitSession(now.Add(-60 * time.Minute))
	if err != nil {
		t.Fatalf("StartSitSession 失败: %v", err)
	}

	res, err := svc.CreditNearestStandMove(scenario.ID, session.ID, now, model.SourceActiveBreak)
	if err != nil {
		t.Fatalf("CreditNearestStandMove 失败: %v", err)
	}
	if !res.Credited || res.ReminderID != item.ID {
		t.Fatalf("抵扣结果 = %+v，期望命中最近到点的 stand", res)
	}
	var checkIn model.CheckIn
	if err := svc.db.First(&checkIn).Error; err != nil {
		t.Fatalf("查询打卡失败: %v", err)
	}
	if checkIn.ReminderID != item.ID || checkIn.Source != model.SourceActiveBreak || checkIn.ScheduledAt != 0 {
		t.Errorf("打卡 = %+v，期望 scheduled_at=0/source=active_break", checkIn)
	}
	stats, err := svc.GetTodayStats()
	if err != nil {
		t.Fatalf("GetTodayStats 失败: %v", err)
	}
	if stats.DoneTotal != 1 {
		t.Errorf("今日完成总数 = %d，期望 1", stats.DoneTotal)
	}
	// 第二次（模拟锁屏）不得重复。
	again, err := svc.CreditNearestStandMove(scenario.ID, session.ID, now, model.SourceLockBreak)
	if err != nil {
		t.Fatalf("第二次抵扣不应报错: %v", err)
	}
	if again.Credited {
		t.Error("同一久坐会话不应重复抵扣")
	}
}

func TestCreditNearestSkipsNonStandMove(t *testing.T) {
	svc := newReminderTestService(t)
	now := time.Date(2026, 10, 22, 10, 30, 0, 0, time.Local)
	svc.now = func() time.Time { return now }
	scenario, _ := seedStandScenario(t, svc, model.KindWater)
	session, err := svc.StartSitSession(now.Add(-60 * time.Minute))
	if err != nil {
		t.Fatalf("StartSitSession 失败: %v", err)
	}
	res, err := svc.CreditNearestStandMove(scenario.ID, session.ID, now, model.SourceActiveBreak)
	if err != nil {
		t.Fatalf("CreditNearestStandMove 失败: %v", err)
	}
	if res.Credited {
		t.Error("没有 stand/move 时不应抵扣")
	}
	var count int64
	if err := svc.db.Model(&model.CheckIn{}).Count(&count).Error; err != nil {
		t.Fatalf("统计打卡失败: %v", err)
	}
	if count != 0 {
		t.Errorf("没有 stand/move 时不应写 CheckIn，实际 %d", count)
	}
}

func TestCreditNearestPicksEarliestToday(t *testing.T) {
	svc := newReminderTestService(t)
	now := time.Date(2026, 10, 22, 10, 30, 0, 0, time.Local)
	svc.now = func() time.Time { return now }
	scenario, late := seedStandScenario(t, svc, model.KindMove)
	// 把默认 09:00 的 move 改成 12:00 开始；再建一条 11:00 的 stand。
	if err := svc.UpdateReminder(ReminderInput{
		ID:          late.ID,
		ScenarioID:  scenario.ID,
		Title:       late.Title,
		Content:     late.Content,
		Kind:        late.Kind,
		IntervalMin: 60,
		WindowStart: "12:00",
		WindowEnd:   "18:00",
		DailyTarget: 4,
		Enabled:     true,
	}); err != nil {
		t.Fatalf("更新提醒失败: %v", err)
	}
	early, err := svc.CreateReminder(ReminderInput{
		ScenarioID:  scenario.ID,
		Title:       "先起身",
		Kind:        model.KindStand,
		IntervalMin: 60,
		WindowStart: "10:00",
		WindowEnd:   "18:00",
		DailyTarget: 4,
		Enabled:     true,
	})
	if err != nil {
		t.Fatalf("创建第二条提醒失败: %v", err)
	}
	session, _ := svc.StartSitSession(now.Add(-60 * time.Minute))
	res, err := svc.CreditNearestStandMove(scenario.ID, session.ID, now, model.SourceActiveBreak)
	if err != nil {
		t.Fatalf("CreditNearestStandMove 失败: %v", err)
	}
	if !res.Credited || res.ReminderID != early.ID {
		t.Errorf("抵扣 = %+v，期望先到点的 stand %d", res, early.ID)
	}
}
