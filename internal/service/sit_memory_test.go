package service

import (
	"testing"
	"time"

	"morrow/internal/model"
	"morrow/internal/sit"
)

func TestSitMemoryContextFactsAndHistory(t *testing.T) {
	svc := newReminderTestService(t)
	now := time.Date(2026, 10, 21, 21, 0, 0, 0, time.Local)
	svc.now = func() time.Time { return now }
	dateKey := now.Format(model.DateLayout)

	seedSession := func(start time.Time, activeSeconds int) uint {
		t.Helper()
		row, err := svc.StartSitSession(start)
		if err != nil {
			t.Fatalf("StartSitSession 失败: %v", err)
		}
		if err := svc.EndSitSession(row.ID, string(sit.EndReasonActiveBreak), activeSeconds, start.Add(time.Duration(activeSeconds)*time.Second)); err != nil {
			t.Fatalf("EndSitSession 失败: %v", err)
		}
		return row.ID
	}
	todaySession := seedSession(time.Date(2026, 10, 21, 19, 0, 0, 0, time.Local), 7200)
	if _, err := svc.StartSitBreak(todaySession, now.Add(-30*time.Minute), string(sit.BreakKindActive), 120); err != nil {
		t.Fatalf("StartSitBreak 失败: %v", err)
	}
	seedSession(time.Date(2026, 10, 20, 10, 0, 0, 0, time.Local), 3600)
	seedSession(time.Date(2026, 10, 14, 10, 0, 0, 0, time.Local), 5400)
	seedSession(time.Date(2026, 10, 13, 10, 0, 0, 0, time.Local), 1800)

	seedApp := func(appID string, start time.Time, minutes int) {
		t.Helper()
		row, err := svc.StartAppUsageSession("darwin", appID, start)
		if err != nil {
			t.Fatalf("StartAppUsageSession 失败: %v", err)
		}
		if err := svc.EndAppUsageSession(row.ID, string(sit.EndReasonAppSwitch), start.Add(time.Duration(minutes)*time.Minute)); err != nil {
			t.Fatalf("EndAppUsageSession 失败: %v", err)
		}
	}
	seedApp("com.microsoft.VSCode", now.Add(-3*time.Hour), 70)
	seedApp("com.google.Chrome", now.Add(-2*time.Hour), 50)
	if err := svc.SetAppCategory("darwin", "com.google.Chrome", string(sit.CategoryMedia)); err != nil {
		t.Fatalf("SetAppCategory 失败: %v", err)
	}

	for i, action := range []string{model.ActionSkip, model.ActionSkip, model.ActionSkip, model.ActionSnooze} {
		if err := svc.db.Create(&model.CheckIn{
			ReminderID: 1, ScenarioID: 1, Action: action,
			ActedAt: now.Add(-time.Duration(i) * time.Hour).Unix(), DateKey: dateKey,
		}).Error; err != nil {
			t.Fatalf("写入 CheckIn 失败: %v", err)
		}
	}

	recent, err := svc.CreateSitSpeech(dateKey, 0, string(sit.BubbleNudge), "nudge.sassy.base", "evening", "晚上坐久了", now, time.Minute)
	if err != nil {
		t.Fatalf("创建近期发言失败: %v", err)
	}
	old, err := svc.CreateSitSpeech(dateKey, 0, string(sit.BubbleNudge), "nudge.sassy.2", "old_fact", "旧记忆", now, time.Minute)
	if err != nil {
		t.Fatalf("创建旧发言失败: %v", err)
	}
	if err := svc.db.Model(&model.SitSpeech{}).Where("id = ?", old.ID).
		Update("created_at", now.Add(-4*24*time.Hour).Unix()).Error; err != nil {
		t.Fatalf("调整旧发言时间失败: %v", err)
	}
	ctx, err := svc.SitMemoryContext(now, true, 120)
	if err != nil {
		t.Fatalf("SitMemoryContext 失败: %v", err)
	}
	if ctx.Facts.TodaySittingMinutes != 120 || ctx.Facts.TodayActiveBreaks != 1 {
		t.Errorf("今日事实 = %+v", ctx.Facts)
	}
	if ctx.Facts.RecentSittingMinutes != 180 || ctx.Facts.PriorSittingMinutes != 120 {
		t.Errorf("周对比久坐 = %d/%d，期望 180/120", ctx.Facts.RecentSittingMinutes, ctx.Facts.PriorSittingMinutes)
	}
	if ctx.Facts.EveningMinutes != 120 {
		t.Errorf("晚间分钟 = %d，期望 120", ctx.Facts.EveningMinutes)
	}
	if got := ctx.Facts.TodayCategoryMinutes[sit.CategoryCoding]; got != 70 {
		t.Errorf("写代码分钟 = %d，期望 70", got)
	}
	if got := ctx.Facts.TodayCategoryMinutes[sit.CategoryMedia]; got != 50 {
		t.Errorf("纠正后的 Chrome 应计入影音，得到 %d", got)
	}
	if ctx.Facts.RecentSkips != 3 || ctx.Facts.RecentSnoozes != 1 {
		t.Errorf("跳过/推迟 = %d/%d", ctx.Facts.RecentSkips, ctx.Facts.RecentSnoozes)
	}
	if len(ctx.QuoteIDs) != 1 || ctx.QuoteIDs[0] != "evening" {
		t.Errorf("3 天内记忆 ID = %v，期望仅 evening", ctx.QuoteIDs)
	}
	if len(ctx.NudgeTemplateIDs) < 1 || ctx.NudgeTemplateIDs[0] != "nudge.sassy.base" {
		t.Errorf("近期软提醒模板 = %v", ctx.NudgeTemplateIDs)
	}
	if recent.ID == 0 {
		t.Error("近期发言 ID 为空")
	}

	basic, err := svc.SitMemoryContext(now, false, 120)
	if err != nil {
		t.Fatalf("基础模式 SitMemoryContext 失败: %v", err)
	}
	if basic.Facts.HasProfile || len(basic.Facts.TodayCategoryMinutes) != 0 {
		t.Errorf("关闭画像不应有类别事实: %+v", basic.Facts)
	}
	if basic.Facts.TodaySittingMinutes != 120 {
		t.Errorf("基础模式仍应保留时间事实，得到 %d", basic.Facts.TodaySittingMinutes)
	}
}
