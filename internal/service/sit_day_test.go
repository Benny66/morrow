package service

import (
	"testing"
	"time"

	"morrow/internal/model"
	"morrow/internal/sit"
)

func TestSitDayAggregates(t *testing.T) {
	svc := newReminderTestService(t)
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.Local)
	svc.now = func() time.Time { return now }
	dateKey := now.Format(model.DateLayout)

	first, err := svc.StartSitSession(now.Add(-3 * time.Hour))
	if err != nil {
		t.Fatalf("StartSitSession 失败: %v", err)
	}
	if err := svc.TouchSitSession(first.ID, 3600, now.Add(-2*time.Hour)); err != nil {
		t.Fatalf("TouchSitSession 失败: %v", err)
	}
	if err := svc.EndSitSession(first.ID, string(sit.EndReasonActiveBreak), 3600, now.Add(-2*time.Hour)); err != nil {
		t.Fatalf("EndSitSession 失败: %v", err)
	}
	if _, err := svc.StartSitBreak(first.ID, now.Add(-2*time.Hour), string(sit.BreakKindActive), 120); err != nil {
		t.Fatalf("StartSitBreak active 失败: %v", err)
	}

	second, err := svc.StartSitSession(now.Add(-90 * time.Minute))
	if err != nil {
		t.Fatalf("StartSitSession 失败: %v", err)
	}
	if err := svc.TouchSitSession(second.ID, 600, now.Add(-80*time.Minute)); err != nil {
		t.Fatalf("TouchSitSession 失败: %v", err)
	}
	if err := svc.EndSitSession(second.ID, string(sit.EndReasonLockBreak), 600, now.Add(-80*time.Minute)); err != nil {
		t.Fatalf("EndSitSession 失败: %v", err)
	}
	if err := svc.db.Model(&model.SitSession{}).Where("id = ?", second.ID).
		Update("credited_reminder_id", 1).Error; err != nil {
		t.Fatalf("标记抵扣失败: %v", err)
	}
	if _, err := svc.StartSitBreak(second.ID, now.Add(-80*time.Minute), string(sit.BreakKindLock), 300); err != nil {
		t.Fatalf("StartSitBreak lock 失败: %v", err)
	}
	if _, err := svc.StartSitBreak(second.ID, now.Add(-70*time.Minute), string(sit.BreakKindIdle), 900); err != nil {
		t.Fatalf("StartSitBreak idle 失败: %v", err)
	}

	day, err := svc.GetSitDay(dateKey)
	if err != nil {
		t.Fatalf("GetSitDay 失败: %v", err)
	}
	if day.SittingMinutes != 70 || day.LongestSessionMinutes != 60 {
		t.Errorf("久坐聚合 = %d/%d，期望 70/60", day.SittingMinutes, day.LongestSessionMinutes)
	}
	if day.ActiveBreaks != 1 || day.LockBreaks != 1 || day.IdleBreaks != 1 {
		t.Errorf("休息计数 = %d/%d/%d，期望 1/1/1", day.ActiveBreaks, day.LockBreaks, day.IdleBreaks)
	}
	if day.CreditedCount != 1 {
		t.Errorf("抵扣计数 = %d，期望 1", day.CreditedCount)
	}

	trend, err := svc.GetSitTrend(7)
	if err != nil {
		t.Fatalf("GetSitTrend 失败: %v", err)
	}
	if len(trend) != 7 {
		t.Fatalf("趋势天数 = %d，期望 7", len(trend))
	}
	if trend[6].DateKey != dateKey || trend[6].SittingMinutes != 70 {
		t.Errorf("趋势最后一天 = %+v，期望今天 70 分钟", trend[6])
	}
	if trend[0].SittingMinutes != 0 {
		t.Errorf("无数据的历史日应为 0，得到 %d", trend[0].SittingMinutes)
	}
}

func TestSitDayRejectsInvalidDate(t *testing.T) {
	svc := newReminderTestService(t)
	if _, err := svc.GetSitDay("2026/10/22"); err == nil {
		t.Error("非法日期应返回 validation 错误")
	}
}

func TestRecoverOpenSitSessionsClosesAtLastSeen(t *testing.T) {
	svc := newReminderTestService(t)
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.Local)
	svc.now = func() time.Time { return now }
	dateKey := now.Format(model.DateLayout)

	open, err := svc.StartSitSession(now.Add(-30 * time.Minute))
	if err != nil {
		t.Fatalf("StartSitSession 失败: %v", err)
	}
	if err := svc.TouchSitSession(open.ID, 600, now.Add(-10*time.Minute)); err != nil {
		t.Fatalf("TouchSitSession 失败: %v", err)
	}
	closed, err := svc.StartSitSession(now.Add(-2 * time.Hour))
	if err != nil {
		t.Fatalf("StartSitSession 失败: %v", err)
	}
	if err := svc.EndSitSession(closed.ID, string(sit.EndReasonActiveBreak), 900, now.Add(-time.Hour)); err != nil {
		t.Fatalf("EndSitSession 失败: %v", err)
	}

	n, err := svc.RecoverOpenSitSessions()
	if err != nil {
		t.Fatalf("RecoverOpenSitSessions 失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("收尾数量 = %d，期望 1", n)
	}
	var openRow model.SitSession
	if err := svc.db.First(&openRow, open.ID).Error; err != nil {
		t.Fatalf("读取开放会话失败: %v", err)
	}
	if openRow.EndedAt != now.Add(-10*time.Minute).Unix() || openRow.EndReason != string(sit.EndReasonAbandoned) || openRow.ActiveSeconds != 600 {
		t.Errorf("恢复后的会话 = ended %d/active %d/reason %s", openRow.EndedAt, openRow.ActiveSeconds, openRow.EndReason)
	}
	var closedRow model.SitSession
	if err := svc.db.First(&closedRow, closed.ID).Error; err != nil {
		t.Fatalf("读取已收尾会话失败: %v", err)
	}
	if closedRow.EndReason != string(sit.EndReasonActiveBreak) {
		t.Errorf("已收尾会话被改写为 %q", closedRow.EndReason)
	}
	total, _, err := svc.SitDayClosedTotals(dateKey)
	if err != nil {
		t.Fatalf("SitDayClosedTotals 失败: %v", err)
	}
	if total != 1500 {
		t.Errorf("恢复后日累计 = %d 秒，期望 1500", total)
	}
}

func TestClearSitProfileKeepsCheckIns(t *testing.T) {
	svc := newReminderTestService(t)
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.Local)
	svc.now = func() time.Time { return now }
	dateKey := now.Format(model.DateLayout)

	session, err := svc.StartSitSession(now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("StartSitSession 失败: %v", err)
	}
	if _, err := svc.StartSitBreak(session.ID, now.Add(-30*time.Minute), string(sit.BreakKindActive), 120); err != nil {
		t.Fatalf("StartSitBreak 失败: %v", err)
	}
	if _, err := svc.CreateSitSpeech(SitSpeechInput{
		DateKey: dateKey, SessionID: session.ID, Kind: string(sit.BubbleNudge),
		TemplateID: "nudge.base", Text: "坐久了", Now: now, TTL: time.Minute,
	}); err != nil {
		t.Fatalf("CreateSitSpeech 失败: %v", err)
	}
	app, err := svc.StartAppUsageSession("darwin", "com.example.unknown", now.Add(-40*time.Minute))
	if err != nil {
		t.Fatalf("StartAppUsageSession 失败: %v", err)
	}
	if err := svc.EndAppUsageSession(app.ID, string(sit.EndReasonAppSwitch), now.Add(-20*time.Minute)); err != nil {
		t.Fatalf("EndAppUsageSession 失败: %v", err)
	}
	if err := svc.SetAppCategory("darwin", "com.example.unknown", string(sit.CategoryDesign)); err != nil {
		t.Fatalf("SetAppCategory 失败: %v", err)
	}
	if err := svc.db.Create(&model.CheckIn{
		ReminderID:  1,
		ScenarioID:  1,
		Action:      model.ActionDone,
		Source:      model.SourceActiveBreak,
		ScheduledAt: 0,
		ActedAt:     now.Unix(),
		DateKey:     dateKey,
	}).Error; err != nil {
		t.Fatalf("写入 CheckIn 失败: %v", err)
	}

	for i := 0; i < 2; i++ { // 幂等：重复清空不报错
		if err := svc.ClearSitProfile(); err != nil {
			t.Fatalf("第 %d 次 ClearSitProfile 失败: %v", i+1, err)
		}
	}
	for _, table := range []any{
		&model.AppUsageSession{},
		&model.AppCategoryOverride{},
		&model.SitSession{},
		&model.SitBreak{},
		&model.SitSpeech{},
	} {
		var count int64
		if err := svc.db.Unscoped().Model(table).Count(&count).Error; err != nil {
			t.Fatalf("统计表失败: %v", err)
		}
		if count != 0 {
			t.Errorf("%T 清空后仍有 %d 行", table, count)
		}
	}
	var checks int64
	if err := svc.db.Model(&model.CheckIn{}).Count(&checks).Error; err != nil {
		t.Fatalf("统计 CheckIn 失败: %v", err)
	}
	if checks != 1 {
		t.Errorf("CheckIn 应保留 1 条，实际 %d", checks)
	}
	day, err := svc.GetSitDay(dateKey)
	if err != nil {
		t.Fatalf("GetSitDay 失败: %v", err)
	}
	if day.SittingMinutes != 0 || day.ActiveBreaks != 0 || day.CreditedCount != 0 || len(day.Apps) != 0 {
		t.Errorf("清空后日聚合应为空，得到 %+v", day)
	}
}
