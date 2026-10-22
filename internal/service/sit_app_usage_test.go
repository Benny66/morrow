package service

import (
	"errors"
	"testing"
	"time"

	"morrow/internal/apperr"
	"morrow/internal/model"
	"morrow/internal/sit"
)

func TestSitDayAppUsageAggregates(t *testing.T) {
	svc := newReminderTestService(t)
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.Local)
	svc.now = func() time.Time { return now }
	dateKey := now.Format(model.DateLayout)

	seed := func(appID string, start, end time.Time) {
		t.Helper()
		row, err := svc.StartAppUsageSession("darwin", appID, start)
		if err != nil {
			t.Fatalf("StartAppUsageSession 失败: %v", err)
		}
		if err := svc.EndAppUsageSession(row.ID, string(sit.EndReasonAppSwitch), end); err != nil {
			t.Fatalf("EndAppUsageSession 失败: %v", err)
		}
	}
	seed("com.microsoft.VSCode", now.Add(-2*time.Hour), now.Add(-90*time.Minute))
	seed("com.microsoft.VSCode", now.Add(-60*time.Minute), now.Add(-50*time.Minute))
	seed("com.google.Chrome", now.Add(-40*time.Minute), now.Add(-35*time.Minute))
	seed("us.zoom.xos", now.Add(-30*time.Minute), now.Add(-25*time.Minute))

	// 一个仍开放的应用会话：结束时间为空，按 now 计算时长。
	open, err := svc.StartAppUsageSession("darwin", "com.figma.Desktop", now.Add(-10*time.Minute))
	if err != nil {
		t.Fatalf("StartAppUsageSession 失败: %v", err)
	}
	if err := svc.TouchAppUsageSession(open.ID, now.Add(-2*time.Minute)); err != nil {
		t.Fatalf("TouchAppUsageSession 失败: %v", err)
	}

	day, err := svc.GetSitDay(dateKey)
	if err != nil {
		t.Fatalf("GetSitDay 失败: %v", err)
	}
	if len(day.Apps) != 4 {
		t.Fatalf("应用数 = %d，期望 4: %+v", len(day.Apps), day.Apps)
	}
	byID := map[string]AppUsage{}
	for _, app := range day.Apps {
		byID[app.AppID] = app
	}
	if got := byID["com.microsoft.VSCode"]; got.Minutes != 40 || got.Sessions != 2 || got.Category != sit.CategoryCoding {
		t.Errorf("VS Code = %+v，期望 40 分钟/2 会话/coding", got)
	}
	if got := byID["com.google.Chrome"]; got.Minutes != 5 || got.Category != sit.CategoryBrowsing {
		t.Errorf("Chrome = %+v，期望 5 分钟/browsing", got)
	}
	if got := byID["us.zoom.xos"]; got.Minutes != 5 || got.Category != sit.CategoryCommunication {
		t.Errorf("Zoom = %+v，期望 5 分钟/communication（会议名单）", got)
	}
	if got := byID["com.figma.Desktop"]; got.Minutes != 10 || got.Category != sit.CategoryDesign {
		t.Errorf("Figma 开放会话 = %+v，期望 10 分钟/design", got)
	}

	cat := map[sit.Category]int{}
	for _, c := range day.Categories {
		cat[c.Category] = c.Minutes
	}
	if cat[sit.CategoryCoding] != 40 || cat[sit.CategoryBrowsing] != 5 ||
		cat[sit.CategoryCommunication] != 5 || cat[sit.CategoryDesign] != 10 {
		t.Errorf("类别聚合 = %+v", day.Categories)
	}
}

func TestRecoverOpenAppUsageSessionsClosesAtLastSeen(t *testing.T) {
	svc := newReminderTestService(t)
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.Local)
	svc.now = func() time.Time { return now }

	touched, err := svc.StartAppUsageSession("darwin", "com.apple.Safari", now.Add(-30*time.Minute))
	if err != nil {
		t.Fatalf("StartAppUsageSession 失败: %v", err)
	}
	if err := svc.TouchAppUsageSession(touched.ID, now.Add(-10*time.Minute)); err != nil {
		t.Fatalf("TouchAppUsageSession 失败: %v", err)
	}
	untouched, err := svc.StartAppUsageSession("darwin", "com.apple.Terminal", now.Add(-5*time.Minute))
	if err != nil {
		t.Fatalf("StartAppUsageSession 失败: %v", err)
	}
	closed, err := svc.StartAppUsageSession("darwin", "com.google.Chrome", now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("StartAppUsageSession 失败: %v", err)
	}
	if err := svc.EndAppUsageSession(closed.ID, string(sit.EndReasonAppSwitch), now.Add(-50*time.Minute)); err != nil {
		t.Fatalf("EndAppUsageSession 失败: %v", err)
	}

	n, err := svc.RecoverOpenAppUsageSessions()
	if err != nil {
		t.Fatalf("RecoverOpenAppUsageSessions 失败: %v", err)
	}
	if n != 2 {
		t.Fatalf("收尾数量 = %d，期望 2", n)
	}

	var safari model.AppUsageSession
	if err := svc.db.First(&safari, touched.ID).Error; err != nil {
		t.Fatalf("读取 Safari 会话失败: %v", err)
	}
	if safari.EndedAt != now.Add(-10*time.Minute).Unix() || safari.EndReason != string(sit.EndReasonAbandoned) {
		t.Errorf("有心跳的会话 = ended %d/%s，期望按最后心跳收尾为 abandoned", safari.EndedAt, safari.EndReason)
	}
	var terminal model.AppUsageSession
	if err := svc.db.First(&terminal, untouched.ID).Error; err != nil {
		t.Fatalf("读取终端会话失败: %v", err)
	}
	if terminal.EndedAt != now.Add(-5*time.Minute).Unix() || terminal.EndReason != string(sit.EndReasonAbandoned) {
		t.Errorf("无心跳的会话 = ended %d/%s，期望按开始时间收尾为 abandoned", terminal.EndedAt, terminal.EndReason)
	}
	var chrome model.AppUsageSession
	if err := svc.db.First(&chrome, closed.ID).Error; err != nil {
		t.Fatalf("读取 Chrome 会话失败: %v", err)
	}
	if chrome.EndReason != string(sit.EndReasonAppSwitch) {
		t.Errorf("已收尾会话被改写为 %q", chrome.EndReason)
	}

	day, err := svc.GetSitDay(now.Format(model.DateLayout))
	if err != nil {
		t.Fatalf("GetSitDay 失败: %v", err)
	}
	for _, app := range day.Apps {
		if app.AppID == "com.apple.Safari" && app.Minutes != 20 {
			t.Errorf("Safari 恢复后分钟数 = %d，期望 20（不应算到崩溃后）", app.Minutes)
		}
	}
}

func TestSetAppCategoryAppliesToHistoryAndTrend(t *testing.T) {
	svc := newReminderTestService(t)
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.Local)
	svc.now = func() time.Time { return now }
	today := now.Format(model.DateLayout)
	yesterday := now.AddDate(0, 0, -1)

	seed := func(appID string, start, end time.Time) {
		t.Helper()
		row, err := svc.StartAppUsageSession("darwin", appID, start)
		if err != nil {
			t.Fatalf("StartAppUsageSession 失败: %v", err)
		}
		if err := svc.EndAppUsageSession(row.ID, string(sit.EndReasonAppSwitch), end); err != nil {
			t.Fatalf("EndAppUsageSession 失败: %v", err)
		}
	}
	seed("com.google.Chrome", now.Add(-2*time.Hour), now.Add(-90*time.Minute))
	seed("com.google.Chrome", now.Add(-60*time.Minute), now.Add(-30*time.Minute))
	seed("com.google.Chrome", yesterday.Add(-time.Hour), yesterday.Add(-30*time.Minute))
	seed("com.apple.Terminal", now.Add(-20*time.Minute), now.Add(-10*time.Minute))
	seed("com.example.unknown", now.Add(-8*time.Minute), now.Add(-5*time.Minute))

	before, err := svc.GetSitDay(today)
	if err != nil {
		t.Fatalf("GetSitDay 失败: %v", err)
	}
	if got := appByID(before, "com.google.Chrome"); got.Category != sit.CategoryBrowsing {
		t.Fatalf("纠正前 Chrome = %+v，期望 browsing", got)
	}

	if err := svc.SetAppCategory("darwin", "com.google.Chrome", string(sit.CategoryMedia)); err != nil {
		t.Fatalf("SetAppCategory 失败: %v", err)
	}
	if err := svc.SetAppCategory("darwin", "com.example.unknown", string(sit.CategoryDesign)); err != nil {
		t.Fatalf("未知应用纠正失败: %v", err)
	}
	if err := svc.SetAppCategory("darwin", "com.google.Chrome", string(sit.CategoryBrowsing)); err != nil {
		t.Fatalf("重复纠正失败: %v", err)
	}
	if err := svc.SetAppCategory("darwin", "com.google.Chrome", string(sit.CategoryMedia)); err != nil {
		t.Fatalf("再次纠正失败: %v", err)
	}

	after, err := svc.GetSitDay(today)
	if err != nil {
		t.Fatalf("GetSitDay 失败: %v", err)
	}
	if got := appByID(after, "com.google.Chrome"); got.Category != sit.CategoryMedia {
		t.Errorf("纠正后 Chrome = %+v，期望 media", got)
	}
	if got := appByID(after, "com.example.unknown"); got.Category != sit.CategoryDesign {
		t.Errorf("未知应用纠正后 = %+v，期望 design", got)
	}
	if got := appByID(after, "com.apple.Terminal"); got.Category != sit.CategoryCoding {
		t.Errorf("未纠正应用类别被误改: %+v", got)
	}
	media := 0
	for _, c := range after.Categories {
		if c.Category == sit.CategoryMedia {
			media = c.Minutes
		}
	}
	if media == 0 {
		t.Errorf("纠正后类别分布没有影音: %+v", after.Categories)
	}

	// 原始会话行不被改写：桌面端只派生类别，不写 category 列。
	var raw []model.AppUsageSession
	if err := svc.db.Where("app_id = ?", "com.google.Chrome").Find(&raw).Error; err != nil {
		t.Fatalf("读取原始会话失败: %v", err)
	}
	if len(raw) != 3 {
		t.Errorf("原始 Chrome 会话 = %d 条，期望 3（纠正不应改写或新增）", len(raw))
	}
	var overrideCount int64
	if err := svc.db.Model(&model.AppCategoryOverride{}).Where("app_id = ?", "com.google.Chrome").Count(&overrideCount).Error; err != nil {
		t.Fatalf("统计纠正失败: %v", err)
	}
	if overrideCount != 1 {
		t.Errorf("重复纠正后应只有 1 行，实际 %d", overrideCount)
	}

	trend, err := svc.GetSitTrend(2)
	if err != nil {
		t.Fatalf("GetSitTrend 失败: %v", err)
	}
	for _, day := range trend {
		if got := appByID(day, "com.google.Chrome"); got.Category != sit.CategoryMedia {
			t.Errorf("%s 趋势 Chrome 类别 = %q，期望 media", day.DateKey, got.Category)
		}
	}

	var ae *apperr.Error
	if err := svc.SetAppCategory("darwin", "com.google.Chrome", "unknown-category"); err == nil ||
		!errors.As(err, &ae) || ae.Code != apperr.CodeValidation {
		t.Errorf("非法类别应返回 validation，得到 %v", err)
	}
	if err := svc.SetAppCategory("", "com.google.Chrome", string(sit.CategoryOther)); err == nil {
		t.Error("空平台应被拒绝")
	}
}

func TestAppCategoryUsesOverride(t *testing.T) {
	svc := newReminderTestService(t)

	if got, err := svc.AppCategory("darwin", "com.google.Chrome"); err != nil || got != sit.CategoryBrowsing {
		t.Fatalf("Chrome 默认类别 = %q/%v，期望 browsing", got, err)
	}
	if got, err := svc.AppCategory("darwin", "us.zoom.xos"); err != nil || got != sit.CategoryCommunication {
		t.Fatalf("Zoom 应经会议名单归入 communication，得到 %q/%v", got, err)
	}
	if got, err := svc.AppCategory("darwin", "com.example.unknown"); err != nil || got != sit.CategoryOther {
		t.Fatalf("未知应用应归 other，得到 %q/%v", got, err)
	}
	if err := svc.SetAppCategory("darwin", "com.example.unknown", string(sit.CategoryDesign)); err != nil {
		t.Fatalf("SetAppCategory 失败: %v", err)
	}
	if got, err := svc.AppCategory("darwin", "com.example.unknown"); err != nil || got != sit.CategoryDesign {
		t.Fatalf("纠正后未知应用类别 = %q/%v，期望 design", got, err)
	}
}

func TestGetSitDayFutureReturnsEmpty(t *testing.T) {
	svc := newReminderTestService(t)
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.Local)
	svc.now = func() time.Time { return now }

	future := now.AddDate(0, 0, 1)
	row, err := svc.StartAppUsageSession("darwin", "com.google.Chrome", future)
	if err != nil {
		t.Fatalf("StartAppUsageSession 失败: %v", err)
	}
	if err := svc.EndAppUsageSession(row.ID, string(sit.EndReasonAppSwitch), future.Add(time.Hour)); err != nil {
		t.Fatalf("EndAppUsageSession 失败: %v", err)
	}

	day, err := svc.GetSitDay(future.Format(model.DateLayout))
	if err != nil {
		t.Fatalf("未来日期查询不应报错: %v", err)
	}
	if day.SittingMinutes != 0 || len(day.Apps) != 0 || len(day.Categories) != 0 {
		t.Errorf("未来日期应返回空聚合，得到 %+v", day)
	}
}

func TestGetSitDayCrossMonthAndYear(t *testing.T) {
	svc := newReminderTestService(t)
	now := time.Date(2026, 1, 2, 12, 0, 0, 0, time.Local)
	svc.now = func() time.Time { return now }

	seed := func(appID string, start, end time.Time) {
		t.Helper()
		row, err := svc.StartAppUsageSession("darwin", appID, start)
		if err != nil {
			t.Fatalf("StartAppUsageSession 失败: %v", err)
		}
		if err := svc.EndAppUsageSession(row.ID, string(sit.EndReasonAppSwitch), end); err != nil {
			t.Fatalf("EndAppUsageSession 失败: %v", err)
		}
	}
	seed("com.google.Chrome", time.Date(2025, 12, 31, 10, 0, 0, 0, time.Local), time.Date(2025, 12, 31, 10, 30, 0, 0, time.Local))
	seed("com.apple.Terminal", time.Date(2026, 1, 1, 9, 0, 0, 0, time.Local), time.Date(2026, 1, 1, 9, 15, 0, 0, time.Local))

	for _, tc := range []struct {
		dateKey string
		appID   string
		minutes int
	}{
		{"2025-12-31", "com.google.Chrome", 30},
		{"2026-01-01", "com.apple.Terminal", 15},
	} {
		day, err := svc.GetSitDay(tc.dateKey)
		if err != nil {
			t.Fatalf("GetSitDay(%s) 失败: %v", tc.dateKey, err)
		}
		got := appByID(day, tc.appID)
		if got.Minutes != tc.minutes {
			t.Errorf("%s %s = %d 分钟，期望 %d", tc.dateKey, tc.appID, got.Minutes, tc.minutes)
		}
	}

	trend, err := svc.GetSitTrend(3)
	if err != nil {
		t.Fatalf("GetSitTrend 失败: %v", err)
	}
	if len(trend) != 3 || trend[0].DateKey != "2025-12-31" || trend[1].DateKey != "2026-01-01" || trend[2].DateKey != "2026-01-02" {
		t.Errorf("跨年趋势日期 = %+v", trend)
	}
}

func appByID(day SitDay, appID string) AppUsage {
	for _, app := range day.Apps {
		if app.AppID == appID {
			return app
		}
	}
	return AppUsage{}
}

func TestClearAppProfileKeepsSitAndCheckIns(t *testing.T) {
	svc := newReminderTestService(t)
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.Local)
	svc.now = func() time.Time { return now }
	dateKey := now.Format(model.DateLayout)

	app, err := svc.StartAppUsageSession("darwin", "com.google.Chrome", now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("StartAppUsageSession 失败: %v", err)
	}
	if err := svc.EndAppUsageSession(app.ID, string(sit.EndReasonAppSwitch), now.Add(-30*time.Minute)); err != nil {
		t.Fatalf("EndAppUsageSession 失败: %v", err)
	}
	if err := svc.db.Create(&model.AppCategoryOverride{
		Platform: "darwin", AppID: "com.google.Chrome", Category: string(sit.CategoryMedia),
	}).Error; err != nil {
		t.Fatalf("写入类别纠正失败: %v", err)
	}
	if _, err := svc.CreateSitSpeech(SitSpeechInput{
		DateKey: dateKey, Kind: string(sit.BubbleMemory), TemplateID: "memory.test",
		Text: "记忆", Now: now, TTL: time.Minute,
	}); err != nil {
		t.Fatalf("创建记忆发言失败: %v", err)
	}
	if _, err := svc.CreateSitSpeech(SitSpeechInput{
		DateKey: dateKey, Kind: string(sit.BubbleNudge), TemplateID: "nudge.test",
		Text: "提醒", Now: now, TTL: time.Minute,
	}); err != nil {
		t.Fatalf("创建软提醒发言失败: %v", err)
	}
	// 带记忆句的普通软提醒也属于派生记忆，关闭画像删除历史时应一并清掉。
	if _, err := svc.CreateSitSpeech(SitSpeechInput{
		DateKey: dateKey, Kind: string(sit.BubbleNudge), TemplateID: "nudge.test2",
		MemoryID: "evening", Text: "提醒 + 记忆", Now: now, TTL: time.Minute,
	}); err != nil {
		t.Fatalf("创建带记忆的软提醒失败: %v", err)
	}
	if err := svc.db.Create(&model.CheckIn{
		ReminderID: 1, ScenarioID: 1, Action: model.ActionDone, Source: model.SourceActiveBreak,
		ActedAt: now.Unix(), DateKey: dateKey,
	}).Error; err != nil {
		t.Fatalf("写入 CheckIn 失败: %v", err)
	}

	if err := svc.ClearAppProfile(); err != nil {
		t.Fatalf("ClearAppProfile 失败: %v", err)
	}
	for _, table := range []any{&model.AppUsageSession{}, &model.AppCategoryOverride{}} {
		var count int64
		if err := svc.db.Unscoped().Model(table).Count(&count).Error; err != nil {
			t.Fatalf("统计表失败: %v", err)
		}
		if count != 0 {
			t.Errorf("%T 清空后仍有 %d 行", table, count)
		}
	}
	var memoryCount, nudgeCount int64
	if err := svc.db.Model(&model.SitSpeech{}).Where("kind = ?", string(sit.BubbleMemory)).Count(&memoryCount).Error; err != nil {
		t.Fatalf("统计记忆失败: %v", err)
	}
	if err := svc.db.Model(&model.SitSpeech{}).Where("kind = ?", string(sit.BubbleNudge)).Count(&nudgeCount).Error; err != nil {
		t.Fatalf("统计软提醒失败: %v", err)
	}
	if memoryCount != 0 || nudgeCount != 1 {
		t.Errorf("记忆/软提醒 = %d/%d，期望 0/1", memoryCount, nudgeCount)
	}
	var checks int64
	if err := svc.db.Model(&model.CheckIn{}).Count(&checks).Error; err != nil {
		t.Fatalf("统计 CheckIn 失败: %v", err)
	}
	if checks != 1 {
		t.Errorf("CheckIn 应保留 1 条，实际 %d", checks)
	}
}
