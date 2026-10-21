package scenariotemplate

import (
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"morrow/internal/apperr"
	"morrow/internal/model"
	"morrow/internal/reminder"
)

// maxFires 返回窗口内的最大触发次数（与实现共用同一真相）。
func maxFires(start, end string, interval int) int {
	day := time.Date(2026, 10, 21, 0, 0, 0, 0, time.Local)
	return len(reminder.FirePoints(day, start, end, interval))
}

func assertValidation(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("期望 validation 错误，实际 nil")
	}
	var ae *apperr.Error
	if !errors.As(err, &ae) {
		t.Fatalf("期望 *apperr.Error，实际 %T: %v", err, err)
	}
	if ae.Code != apperr.CodeValidation {
		t.Fatalf("期望 code=%s，实际 %s（%s）", apperr.CodeValidation, ae.Code, ae.Message)
	}
}

func TestGoalKindsExactlyFive(t *testing.T) {
	want := []string{
		model.KindWater, model.KindStand, model.KindMove, model.KindRest, model.KindToilet,
	}
	got := GoalKinds()
	if len(got) != len(want) {
		t.Fatalf("GoalKinds 长度 = %d，期望 %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("GoalKinds[%d] = %q，期望 %q", i, got[i], want[i])
		}
	}
}

func TestCatalogInvariants(t *testing.T) {
	catalog := LoadCatalog()

	// 解析到 0 个模板必须失败：护栏要能感知自己瞎了。
	if len(catalog.Templates) != 5 {
		t.Fatalf("模板数量 = %d，期望 5", len(catalog.Templates))
	}
	if len(catalog.Goals) != 5 {
		t.Fatalf("目标数量 = %d，期望 5", len(catalog.Goals))
	}

	wantKeys := []string{
		"sedentary-office", "meeting-heavy", "exam-sprint", "work-from-home", "eye-care-first",
	}
	keys := map[string]bool{}
	for i, tpl := range catalog.Templates {
		if tpl.Key != wantKeys[i] {
			t.Errorf("模板[%d] key = %q，期望 %q", i, tpl.Key, wantKeys[i])
		}
		if keys[tpl.Key] {
			t.Errorf("模板 key 重复: %q", tpl.Key)
		}
		keys[tpl.Key] = true

		if strings.TrimSpace(tpl.Name) == "" || strings.TrimSpace(tpl.Description) == "" || strings.TrimSpace(tpl.Summary) == "" {
			t.Errorf("模板 %q 的名称/描述/摘要不能为空", tpl.Key)
		}
		if utf8.RuneCountInString(tpl.Name) > 50 {
			t.Errorf("模板 %q 名称超过 50 字", tpl.Key)
		}

		covered := map[string]bool{}
		for _, item := range tpl.Reminders {
			if !isGoalKind(item.Kind) {
				t.Errorf("模板 %q 的提醒类型非法: %q", tpl.Key, item.Kind)
			}
			covered[item.Kind] = true

			if strings.TrimSpace(item.Title) == "" || utf8.RuneCountInString(item.Title) > 50 {
				t.Errorf("模板 %q 提醒标题为空或超过 50 字: %q", tpl.Key, item.Title)
			}
			if strings.TrimSpace(item.Content) == "" || utf8.RuneCountInString(item.Content) > 200 {
				t.Errorf("模板 %q 提醒正文为空或超过 200 字: %q", tpl.Key, item.Content)
			}
			if !reminder.ValidateWindow(item.WindowStart, item.WindowEnd) {
				t.Errorf("模板 %q 提醒窗口非法: %s–%s", tpl.Key, item.WindowStart, item.WindowEnd)
			}
			if item.IntervalMin < 0 {
				t.Errorf("模板 %q 提醒间隔为负: %d", tpl.Key, item.IntervalMin)
			}
			if item.DailyTarget < 0 {
				t.Errorf("模板 %q 提醒目标为负: %d", tpl.Key, item.DailyTarget)
			}
			if n := maxFires(item.WindowStart, item.WindowEnd, item.IntervalMin); item.DailyTarget > n {
				t.Errorf("模板 %q 的 %s 目标 %d 超过窗口内次数 %d", tpl.Key, item.Kind, item.DailyTarget, n)
			}
		}
		for _, kind := range catalog.Goals {
			if !covered[kind] {
				t.Errorf("模板 %q 未覆盖目标 %q", tpl.Key, kind)
			}
		}
	}
}

func TestPersonalizeAnyGoalPairIsUsable(t *testing.T) {
	catalog := LoadCatalog()
	goals := catalog.Goals
	for _, tpl := range catalog.Templates {
		for i := 0; i < len(goals); i++ {
			for j := i + 1; j < len(goals); j++ {
				pair := []string{goals[i], goals[j]}
				t.Run(tpl.Key+"/"+goals[i]+"+"+goals[j], func(t *testing.T) {
					preview, err := Personalize(tpl, Options{WorkStart: "09:00", WorkEnd: "18:00", Goals: pair})
					if err != nil {
						t.Fatalf("Personalize 失败: %v", err)
					}
					if !preview.Valid {
						t.Fatalf("期望 Valid=true，实际 false（missing=%v）", preview.MissingGoals)
					}
					if len(preview.MissingGoals) != 0 {
						t.Fatalf("期望无缺失目标，实际 %v", preview.MissingGoals)
					}
					if len(preview.Reminders) < 2 {
						t.Fatalf("至少应有 2 条提醒，实际 %d", len(preview.Reminders))
					}
					selected := map[string]bool{pair[0]: true, pair[1]: true}
					for _, item := range preview.Reminders {
						if !selected[item.Kind] {
							t.Errorf("保留了两类之外的提醒: %q", item.Kind)
						}
						if n := maxFires(item.WindowStart, item.WindowEnd, item.IntervalMin); item.DailyTarget > n {
							t.Errorf("%s 目标 %d 超过窗口内次数 %d", item.Kind, item.DailyTarget, n)
						}
					}
				})
			}
		}
	}
}

func TestPersonalizeClipsAndClamps(t *testing.T) {
	tpl, ok := Find("sedentary-office")
	if !ok {
		t.Fatal("找不到 sedentary-office")
	}
	preview, err := Personalize(tpl, Options{
		WorkStart: "10:00", WorkEnd: "11:00",
		Goals: []string{model.KindWater, model.KindMove},
	})
	if err != nil {
		t.Fatalf("Personalize 失败: %v", err)
	}
	if !preview.Valid || len(preview.Reminders) != 2 {
		t.Fatalf("期望 2 条有效提醒，实际 valid=%v len=%d", preview.Valid, len(preview.Reminders))
	}
	for _, item := range preview.Reminders {
		if item.WindowStart != "10:00" || item.WindowEnd != "11:00" {
			t.Errorf("%s 窗口 = %s–%s，期望 10:00–11:00", item.Kind, item.WindowStart, item.WindowEnd)
		}
		if item.DailyTarget != 1 {
			t.Errorf("%s 目标 = %d，期望夹到 1", item.Kind, item.DailyTarget)
		}
	}
}

func TestPersonalizeReportsMissingGoal(t *testing.T) {
	tpl, ok := Find("sedentary-office")
	if !ok {
		t.Fatal("找不到 sedentary-office")
	}
	preview, err := Personalize(tpl, Options{
		WorkStart: "17:00", WorkEnd: "17:10",
		Goals: []string{model.KindWater, model.KindMove},
	})
	if err != nil {
		t.Fatalf("Personalize 失败: %v", err)
	}
	if preview.Valid {
		t.Fatal("期望 Valid=false（move 在该时段没有提醒）")
	}
	if len(preview.MissingGoals) != 1 || preview.MissingGoals[0] != model.KindMove {
		t.Fatalf("MissingGoals = %v，期望 [%s]", preview.MissingGoals, model.KindMove)
	}
	if len(preview.Reminders) != 1 || preview.Reminders[0].Kind != model.KindWater {
		t.Fatalf("期望只保留 water，实际 %+v", preview.Reminders)
	}
	if preview.Reminders[0].WindowStart != "17:00" || preview.Reminders[0].WindowEnd != "17:10" {
		t.Errorf("water 窗口 = %s–%s，期望 17:00–17:10", preview.Reminders[0].WindowStart, preview.Reminders[0].WindowEnd)
	}
	if preview.Reminders[0].DailyTarget != 1 {
		t.Errorf("water 目标 = %d，期望夹到 1", preview.Reminders[0].DailyTarget)
	}
}

func TestPersonalizeNoOverlapWithoutGoalsIsInvalid(t *testing.T) {
	tpl, ok := Find("sedentary-office")
	if !ok {
		t.Fatal("找不到 sedentary-office")
	}
	preview, err := Personalize(tpl, Options{WorkStart: "03:00", WorkEnd: "04:00"})
	if err != nil {
		t.Fatalf("Personalize 失败: %v", err)
	}
	if preview.Valid {
		t.Fatal("完全无交集时 Valid 应为 false")
	}
	if len(preview.Reminders) != 0 {
		t.Fatalf("期望 0 条提醒，实际 %d", len(preview.Reminders))
	}
	if len(preview.MissingGoals) != 0 {
		t.Fatalf("无目标时不应报告缺失目标，实际 %v", preview.MissingGoals)
	}
}

func TestPersonalizeWithoutOptionsReturnsAll(t *testing.T) {
	tpl, ok := Find("eye-care-first")
	if !ok {
		t.Fatal("找不到 eye-care-first")
	}
	preview, err := Personalize(tpl, Options{})
	if err != nil {
		t.Fatalf("Personalize 失败: %v", err)
	}
	if !preview.Valid {
		t.Fatal("全量模板应 Valid=true")
	}
	if len(preview.Reminders) != len(tpl.Reminders) {
		t.Fatalf("提醒数 = %d，期望 %d", len(preview.Reminders), len(tpl.Reminders))
	}
	for i := range tpl.Reminders {
		if preview.Reminders[i] != tpl.Reminders[i] {
			t.Errorf("第 %d 条提醒被意外修改: %+v → %+v", i, tpl.Reminders[i], preview.Reminders[i])
		}
	}
}

func TestPersonalizeValidation(t *testing.T) {
	tpl, ok := Find("sedentary-office")
	if !ok {
		t.Fatal("找不到 sedentary-office")
	}
	cases := []struct {
		name string
		opts Options
	}{
		{"只给工作开始", Options{WorkStart: "09:00"}},
		{"只给工作结束", Options{WorkEnd: "18:00"}},
		{"开始等于结束", Options{WorkStart: "09:00", WorkEnd: "09:00"}},
		{"开始晚于结束", Options{WorkStart: "18:00", WorkEnd: "09:00"}},
		{"时间格式非法", Options{WorkStart: "25:00", WorkEnd: "26:00"}},
		{"只选一个目标", Options{Goals: []string{model.KindWater}}},
		{"选三个目标", Options{Goals: []string{model.KindWater, model.KindStand, model.KindMove}}},
		{"目标重复", Options{Goals: []string{model.KindWater, model.KindWater}}},
		{"目标非法", Options{Goals: []string{model.KindWater, model.KindCustom}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Personalize(tpl, tc.opts)
			assertValidation(t, err)
		})
	}
}

func TestLoadCatalogReturnsFreshCopies(t *testing.T) {
	first := LoadCatalog()
	first.Templates[0].Name = "被改坏"
	first.Templates[0].Reminders[0].Title = "被改坏"
	first.Goals[0] = "被改坏"

	second := LoadCatalog()
	if second.Templates[0].Name == "被改坏" || second.Templates[0].Reminders[0].Title == "被改坏" {
		t.Fatal("LoadCatalog 返回了共享数据，调用方修改污染了目录")
	}
	if second.Goals[0] == "被改坏" {
		t.Fatal("GoalKinds 返回了共享切片")
	}
}
