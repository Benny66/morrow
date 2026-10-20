package ai

import (
	"strings"
	"testing"
	"time"

	"morrow/internal/model"
)

func quickAddNow() time.Time {
	return time.Date(2026, time.October, 20, 15, 0, 0, 0, time.Local)
}

// TestParseQuickAddOnceExplicitDate 断言显式日期被原样解析成绝对时刻。
func TestParseQuickAddOnceExplicitDate(t *testing.T) {
	raw := `{"summary":"明天下午吃苹果","drafts":[
		{"title":"吃苹果","content":"洗一个苹果慢慢吃","kind":"custom","schedule_type":"once","once_date":"2026-10-21","once_time":"16:00"}
	]}`
	plan, err := ParseQuickAddText(raw, quickAddNow())
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(plan.Drafts) != 1 {
		t.Fatalf("草稿数 = %d，期望 1", len(plan.Drafts))
	}
	draft := plan.Drafts[0]
	want := time.Date(2026, time.October, 21, 16, 0, 0, 0, time.Local).Unix()
	if draft.OnceAt != want {
		t.Errorf("once_at = %d，期望 %d", draft.OnceAt, want)
	}
	if draft.ScheduleType != "once" || draft.Title != "吃苹果" || draft.Kind != model.KindCustom {
		t.Errorf("草稿字段不符: %+v", draft)
	}
}

// TestParseQuickAddSmartDate 断言缺日期时的今天/明天规则。
func TestParseQuickAddSmartDate(t *testing.T) {
	now := quickAddNow()
	cases := []struct {
		name string
		time string
		want time.Time
	}{
		{"今天未到", "16:00", time.Date(2026, time.October, 20, 16, 0, 0, 0, time.Local)},
		{"今天已过", "10:00", time.Date(2026, time.October, 21, 10, 0, 0, 0, time.Local)},
		{"正好现在", "15:00", time.Date(2026, time.October, 21, 15, 0, 0, 0, time.Local)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			raw := `{"drafts":[{"title":"x","schedule_type":"once","once_time":"` + c.time + `"}]}`
			plan, err := ParseQuickAddText(raw, now)
			if err != nil {
				t.Fatalf("解析失败: %v", err)
			}
			if got := plan.Drafts[0].OnceAt; got != c.want.Unix() {
				t.Errorf("once_at = %d，期望 %d (%v)", got, c.want.Unix(), c.want)
			}
		})
	}
}

// TestParseQuickAddRejectsExplicitPast 断言显式过去日期不会被静默改到未来。
func TestParseQuickAddRejectsExplicitPast(t *testing.T) {
	raw := `{"drafts":[{"title":"x","schedule_type":"once","once_date":"2026-10-19","once_time":"16:00"}]}`
	if _, err := ParseQuickAddText(raw, quickAddNow()); err == nil {
		t.Fatal("显式过去日期应被拒绝")
	}
}

// TestParseQuickAddStripsFence 断言代码围栏与前后废话被容忍。
func TestParseQuickAddStripsFence(t *testing.T) {
	raw := "好的：\n```json\n{\"drafts\":[{\"title\":\"吃药\",\"schedule_type\":\"once\",\"once_time\":\"16:00\"}]}\n```"
	plan, err := ParseQuickAddText(raw, quickAddNow())
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(plan.Drafts) != 1 || plan.Drafts[0].Title != "吃药" {
		t.Fatalf("解析结果不符: %+v", plan)
	}
}

// TestParseQuickAddOmitted 断言超过上限时保留前 10 条并记录省略数。
func TestParseQuickAddOmitted(t *testing.T) {
	drafts := make([]string, 0, 12)
	for i := 0; i < 12; i++ {
		drafts = append(drafts, `{"title":"x","schedule_type":"once","once_time":"16:00"}`)
	}
	raw := `{"drafts":[` + strings.Join(drafts, ",") + `]}`
	plan, err := ParseQuickAddText(raw, quickAddNow())
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(plan.Drafts) != maxQuickAddDrafts || plan.Omitted != 2 {
		t.Errorf("草稿/省略 = %d/%d，期望 %d/2", len(plan.Drafts), plan.Omitted, maxQuickAddDrafts)
	}
}

// TestParseQuickAddDailyDraft 断言周期草稿也能被解析（T5 使用；T4 服务层会拒绝）。
func TestParseQuickAddDailyDraft(t *testing.T) {
	raw := `{"drafts":[{"title":"喝水","schedule_type":"daily","interval_min":0,"window_start":"09:00","window_end":"18:00"}]}`
	plan, err := ParseQuickAddText(raw, quickAddNow())
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	got := plan.Drafts[0]
	if got.ScheduleType != "daily" || got.IntervalMin != 0 || got.WindowStart != "09:00" {
		t.Errorf("周期草稿字段不符: %+v", got)
	}
}

// TestBuildQuickAddPromptHasClockAndKinds 断言提示词带上当前时间/时区与 kind 枚举，
// 且不把「一次性」误写成周期性。
func TestBuildQuickAddPromptHasClockAndKinds(t *testing.T) {
	now := quickAddNow()
	prompt := buildQuickAddSystemPrompt(now)
	if !strings.Contains(prompt, now.Format("2006-01-02 15:04")) {
		t.Error("提示词缺少当前日期时间")
	}
	if !strings.Contains(prompt, now.Format("MST")) {
		t.Error("提示词缺少时区")
	}
	for _, kind := range model.AllKinds() {
		if !strings.Contains(prompt, kind) {
			t.Errorf("提示词缺少 kind %q", kind)
		}
	}
	if !strings.Contains(prompt, "once") {
		t.Error("提示词缺少一次性类型")
	}
	if !strings.Contains(prompt, "daily") {
		t.Error("提示词缺少周期类型")
	}
	if !strings.Contains(prompt, "每天") {
		t.Error("提示词缺少周期分类规则")
	}
}
