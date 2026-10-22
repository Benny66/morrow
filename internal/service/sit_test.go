package service

import (
	"testing"
	"time"

	"morrow/internal/model"
	"morrow/internal/sit"
)

func TestSitSessionLifecycle(t *testing.T) {
	svc := newReminderTestService(t)
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.Local)
	dateKey := now.Format(model.DateLayout)

	first, err := svc.StartSitSession(now)
	if err != nil {
		t.Fatalf("StartSitSession 失败: %v", err)
	}
	if err := svc.TouchSitSession(first.ID, 120, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("TouchSitSession 失败: %v", err)
	}
	if err := svc.EndSitSession(first.ID, string(sit.EndReasonIdleBreak), 120, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("EndSitSession 失败: %v", err)
	}

	total, longest, err := svc.SitDayClosedTotals(dateKey)
	if err != nil {
		t.Fatalf("SitDayClosedTotals 失败: %v", err)
	}
	if total != 120 || longest != 120 {
		t.Errorf("收尾后总计 = (%d,%d)，期望 (120,120)", total, longest)
	}

	second, err := svc.StartSitSession(now.Add(10 * time.Minute))
	if err != nil {
		t.Fatalf("StartSitSession 失败: %v", err)
	}
	if err := svc.TouchSitSession(second.ID, 60, now.Add(11*time.Minute)); err != nil {
		t.Fatalf("TouchSitSession 失败: %v", err)
	}
	// 开放会话不计入 closed totals；运行层用内存时钟补。
	total, longest, err = svc.SitDayClosedTotals(dateKey)
	if err != nil {
		t.Fatalf("SitDayClosedTotals 失败: %v", err)
	}
	if total != 120 || longest != 120 {
		t.Errorf("开放会话不应进 closed totals，得到 (%d,%d)", total, longest)
	}
	if err := svc.EndSitSession(second.ID, string(sit.EndReasonActiveBreak), 60, now.Add(11*time.Minute)); err != nil {
		t.Fatalf("EndSitSession 失败: %v", err)
	}
	total, longest, err = svc.SitDayClosedTotals(dateKey)
	if err != nil {
		t.Fatalf("SitDayClosedTotals 失败: %v", err)
	}
	if total != 180 || longest != 120 {
		t.Errorf("两段收尾后总计 = (%d,%d)，期望 (180,120)", total, longest)
	}
}

func TestSitSpeechPendingAckBudget(t *testing.T) {
	svc := newReminderTestService(t)
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.Local)
	dateKey := now.Format(model.DateLayout)
	session, err := svc.StartSitSession(now)
	if err != nil {
		t.Fatalf("StartSitSession 失败: %v", err)
	}

	nudge, err := svc.CreateSitSpeech(dateKey, session.ID, string(sit.BubbleNudge), "nudge.sassy.base", "", "坐了很久了", now, time.Minute)
	if err != nil {
		t.Fatalf("CreateSitSpeech 失败: %v", err)
	}
	pending, err := svc.PendingSitSpeech(now)
	if err != nil || pending == nil || pending.ID != nudge.ID {
		t.Fatalf("PendingSitSpeech = %+v, err=%v，期望刚创建的软提醒", pending, err)
	}
	if ok, err := svc.AckSitSpeech(nudge.ID, now); err != nil || !ok {
		t.Fatalf("AckSitSpeech = %v, err=%v，期望 true", ok, err)
	}
	// 幂等：再次确认不报错；预算也只计一次。
	if ok, err := svc.AckSitSpeech(nudge.ID, now); err != nil || !ok {
		t.Fatalf("重复确认应幂等 true，得到 %v, err=%v", ok, err)
	}
	pending, err = svc.PendingSitSpeech(now)
	if err != nil || pending != nil {
		t.Fatalf("确认后不应再有 pending，得到 %+v, err=%v", pending, err)
	}

	budget, err := svc.SitBudget(dateKey, session.ID)
	if err != nil {
		t.Fatalf("SitBudget 失败: %v", err)
	}
	if budget.DailyProactive != 1 || budget.SessionNudges != 1 {
		t.Errorf("预算 = %+v，期望 daily=1 session=1", budget)
	}
	if budget.LastProactiveAt != now.Unix() || budget.FirstNudgeAckAt != now.Unix() {
		t.Errorf("首/末确认时间 = (%d,%d)，期望 %d", budget.FirstNudgeAckAt, budget.LastProactiveAt, now.Unix())
	}

	// greeting 不占主动发言预算。
	greeting, err := svc.CreateSitSpeech(dateKey, session.ID, string(sit.BubbleGreeting), "greeting.base", "", "回来啦", now.Add(time.Minute), time.Minute)
	if err != nil {
		t.Fatalf("CreateSitSpeech greeting 失败: %v", err)
	}
	if ok, err := svc.AckSitSpeech(greeting.ID, now.Add(time.Minute)); err != nil || !ok {
		t.Fatalf("确认 greeting 失败: %v %v", ok, err)
	}
	budget, err = svc.SitBudget(dateKey, session.ID)
	if err != nil {
		t.Fatalf("SitBudget 失败: %v", err)
	}
	if budget.DailyProactive != 1 || budget.SessionNudges != 1 {
		t.Errorf("greeting 不应占预算，得到 %+v", budget)
	}

	// 独立记忆气泡与软提醒共享每日预算；只按 Ack 计数。
	memory, err := svc.CreateSitSpeech(dateKey, session.ID, string(sit.BubbleMemory), "memory.sassy.longest", "longest", "今天坐了很久", now.Add(2*time.Minute), time.Minute)
	if err != nil {
		t.Fatalf("CreateSitSpeech memory 失败: %v", err)
	}
	budget, err = svc.SitBudget(dateKey, session.ID)
	if err != nil {
		t.Fatalf("SitBudget 失败: %v", err)
	}
	if budget.DailyProactive != 1 {
		t.Errorf("未展示的记忆气泡不应占预算，得到 %+v", budget)
	}
	if ok, err := svc.AckSitSpeech(memory.ID, now.Add(2*time.Minute)); err != nil || !ok {
		t.Fatalf("确认 memory 失败: %v %v", ok, err)
	}
	budget, err = svc.SitBudget(dateKey, session.ID)
	if err != nil {
		t.Fatalf("SitBudget 失败: %v", err)
	}
	if budget.DailyProactive != 2 || budget.SessionNudges != 1 {
		t.Errorf("记忆气泡应占每日预算、不占会话软提醒次数，得到 %+v", budget)
	}
}

func TestSitSpeechExpire(t *testing.T) {
	svc := newReminderTestService(t)
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.Local)
	dateKey := now.Format(model.DateLayout)
	session, _ := svc.StartSitSession(now)
	if _, err := svc.CreateSitSpeech(dateKey, session.ID, string(sit.BubbleNudge), "nudge.base", "", "坐久了", now, time.Minute); err != nil {
		t.Fatalf("CreateSitSpeech 失败: %v", err)
	}
	if _, err := svc.CreateSitSpeech(dateKey, session.ID, string(sit.BubbleMemory), "memory.gentle.longest", "longest", "记忆", now, time.Minute); err != nil {
		t.Fatalf("CreateSitSpeech memory 失败: %v", err)
	}
	if err := svc.ExpireSitSpeeches(now.Add(time.Second)); err != nil {
		t.Fatalf("ExpireSitSpeeches 失败: %v", err)
	}
	pending, err := svc.PendingSitSpeech(now.Add(2 * time.Second))
	if err != nil || pending != nil {
		t.Fatalf("过期后不应有 pending，得到 %+v, err=%v", pending, err)
	}
	budget, err := svc.SitBudget(dateKey, session.ID)
	if err != nil {
		t.Fatalf("SitBudget 失败: %v", err)
	}
	if budget.DailyProactive != 0 || budget.SessionNudges != 0 {
		t.Errorf("未确认展示的发言不应占预算，得到 %+v", budget)
	}
}

func TestSitBreak(t *testing.T) {
	svc := newReminderTestService(t)
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.Local)
	session, err := svc.StartSitSession(now)
	if err != nil {
		t.Fatalf("StartSitSession 失败: %v", err)
	}
	br, err := svc.StartSitBreak(session.ID, now, string(sit.BreakKindActive), 0)
	if err != nil {
		t.Fatalf("StartSitBreak 失败: %v", err)
	}
	if err := svc.FinishSitBreak(br.ID, 180); err != nil {
		t.Fatalf("FinishSitBreak 失败: %v", err)
	}
	var got model.SitBreak
	if err := svc.db.First(&got, br.ID).Error; err != nil {
		t.Fatalf("查询休息失败: %v", err)
	}
	if got.DurationSec != 180 || got.Kind != string(sit.BreakKindActive) {
		t.Errorf("休息记录 = %+v，期望 active/180", got)
	}
}
