package sit

import (
	"strings"
	"testing"
	"time"
)

func TestDeriveFacts(t *testing.T) {
	// 2026-10-21 是周三。
	now := time.Date(2026, 10, 21, 19, 0, 0, 0, time.Local)
	today := time.Date(2026, 10, 21, 0, 0, 0, 0, time.Local)
	days := []DayFact{
		{
			Date: today, SittingMinutes: 120, LongestMinutes: 60,
			ActiveBreaks: 2, LockBreaks: 1, IdleBreaks: 1,
			AfternoonMinutes: 30, EveningMinutes: 90,
			CategoryMinutes: map[Category]int{CategoryCoding: 70, CategoryBrowsing: 50},
		},
		{
			Date: today.AddDate(0, 0, -1), SittingMinutes: 60, LongestMinutes: 30,
			ActiveBreaks: 1,
		},
		{
			Date: today.AddDate(0, 0, -7), SittingMinutes: 90, LongestMinutes: 45,
			ActiveBreaks: 1,
		},
		{
			Date: today.AddDate(0, 0, -8), SittingMinutes: 30, LongestMinutes: 20,
			ActiveBreaks: 0,
		},
		{
			Date: today.AddDate(0, 0, -14), SittingMinutes: 45, LongestMinutes: 25,
		},
	}

	facts := DeriveFacts(now, days, 3, 1, true, 120)
	if facts.TodaySittingMinutes != 120 || facts.TodayLongestMinutes != 60 || facts.TodayBreaks != 4 || facts.TodayActiveBreaks != 2 {
		t.Errorf("今日事实 = %+v", facts)
	}
	if facts.RecentSittingMinutes != 180 || facts.PriorSittingMinutes != 120 {
		t.Errorf("7 天/前 7 天久坐 = %d/%d，期望 180/120", facts.RecentSittingMinutes, facts.PriorSittingMinutes)
	}
	if facts.RecentActiveBreaks != 3 || facts.PriorActiveBreaks != 1 {
		t.Errorf("7 天/前 7 天主动起身 = %d/%d，期望 3/1", facts.RecentActiveBreaks, facts.PriorActiveBreaks)
	}
	if facts.EveningMinutes != 90 || facts.AfternoonMinutes != 30 {
		t.Errorf("时段事实 = evening %d / afternoon %d", facts.EveningMinutes, facts.AfternoonMinutes)
	}
	if facts.WeekdayDays[int(time.Wednesday)] != 3 || facts.WeekdayMinutes[int(time.Wednesday)] != 255 {
		t.Errorf("周三规律 = %d 天/%d 分钟", facts.WeekdayDays[int(time.Wednesday)], facts.WeekdayMinutes[int(time.Wednesday)])
	}
	if facts.TodayCategoryMinutes[CategoryCoding] != 70 || !facts.HasProfile {
		t.Errorf("类别事实 = %+v, hasProfile=%v", facts.TodayCategoryMinutes, facts.HasProfile)
	}
	if facts.RecentSkips != 3 || facts.RecentSnoozes != 1 || facts.CurrentContinuousMinutes != 120 {
		t.Errorf("跳过/推迟/连续 = %d/%d/%d", facts.RecentSkips, facts.RecentSnoozes, facts.CurrentContinuousMinutes)
	}
}

func TestSelectQuoteTimeRelevanceAndDedup(t *testing.T) {
	now := time.Date(2026, 10, 21, 19, 0, 0, 0, time.Local)
	facts := MemoryFacts{
		TodaySittingMinutes:  120,
		TodayLongestMinutes:  120,
		TodayActiveBreaks:    1,
		TodayBreaks:          1,
		EveningMinutes:       120,
		RecentSittingMinutes: 120,
		PriorSittingMinutes:  120,
		HasProfile:           true,
		TodayCategoryMinutes: map[Category]int{CategoryCoding: 90},
	}
	quote := SelectQuote(now, facts, ToneSassy, nil)
	if quote == nil || quote.ID != "evening" {
		t.Fatalf("晚间应优先选择 evening，得到 %+v", quote)
	}
	if !strings.Contains(quote.Text, "120") || strings.Contains(quote.Text, "com.") {
		t.Errorf("晚间记忆句 = %q", quote.Text)
	}
	if quote.TemplateID != "memory.sassy.evening" {
		t.Errorf("模板 ID = %q", quote.TemplateID)
	}

	next := SelectQuote(now, facts, ToneSassy, []string{"evening"})
	if next == nil || next.ID == "evening" {
		t.Fatalf("3 天内用过的 evening 不应重复，得到 %+v", next)
	}
	if next.ID != "longest" && next.ID != "category" {
		t.Errorf("跳过 evening 后应选择其他事实，得到 %+v", next)
	}

	all := SelectQuote(now, facts, ToneSassy, []string{"evening", "longest", "category", "weekday", "week_up_breaks", "week_up_sitting", "week_down_sitting", "breaks", "skips"})
	if all != nil {
		t.Errorf("所有候选都近期用过时应返回 nil，得到 %+v", all)
	}
}

func TestSelectQuoteWeekComparisonAndCategory(t *testing.T) {
	now := time.Date(2026, 10, 21, 10, 0, 0, 0, time.Local)

	positive := MemoryFacts{PriorSittingMinutes: 100, RecentSittingMinutes: 70}
	quote := SelectQuote(now, positive, ToneGentle, nil)
	if quote == nil || quote.ID != "week_up_sitting" || !strings.Contains(quote.Text, "30") {
		t.Fatalf("久坐下降应引用周对比，得到 %+v", quote)
	}

	negative := MemoryFacts{PriorSittingMinutes: 100, RecentSittingMinutes: 130, TodayLongestMinutes: 50}
	quote = SelectQuote(now, negative, ToneSassy, nil)
	if quote == nil || quote.ID != "week_down_sitting" {
		t.Fatalf("久坐上升应引用负向周对比，得到 %+v", quote)
	}

	category := MemoryFacts{
		HasProfile:           true,
		TodayCategoryMinutes: map[Category]int{CategoryCoding: 45},
	}
	quote = SelectQuote(now, category, ToneLively, nil)
	if quote == nil || quote.ID != "category" || !strings.Contains(quote.Text, "写代码") || !strings.Contains(quote.Text, "45") {
		t.Fatalf("类别记忆应使用中文类别与分钟，得到 %+v", quote)
	}
}

func TestSelectQuoteNeedsData(t *testing.T) {
	now := time.Date(2026, 10, 21, 10, 0, 0, 0, time.Local)
	if quote := SelectQuote(now, MemoryFacts{}, ToneSassy, nil); quote != nil {
		t.Fatalf("没有任何事实时不应生成记忆句，得到 %+v", quote)
	}
}

func TestSelectQuoteUsesToneAndNoAppIdentity(t *testing.T) {
	now := time.Date(2026, 10, 21, 10, 0, 0, 0, time.Local)
	facts := MemoryFacts{TodayLongestMinutes: 60, TodaySittingMinutes: 60}
	sassy := SelectQuote(now, facts, ToneSassy, nil)
	gentle := SelectQuote(now, facts, ToneGentle, nil)
	if sassy == nil || gentle == nil {
		t.Fatal("应生成记忆句")
	}
	if sassy.Text == gentle.Text {
		t.Errorf("三档语气不应使用相同记忆句: %q", sassy.Text)
	}
	for _, text := range []string{sassy.Text, gentle.Text} {
		if strings.Contains(text, "Chrome") || strings.Contains(text, "com.") {
			t.Errorf("记忆句不应出现应用名: %q", text)
		}
	}
}
