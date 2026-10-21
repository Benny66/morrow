package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"morrow/internal/apperr"
	"morrow/internal/model"
)

func newTemplateTestService(t *testing.T) (*Service, time.Time) {
	t.Helper()
	now := time.Date(2026, time.March, 2, 8, 0, 0, 0, time.Local)
	return newOneOffTestService(t, now), now
}

func countScenarios(t *testing.T, svc *Service) int64 {
	t.Helper()
	var count int64
	if err := svc.db.Model(&model.Scenario{}).Count(&count).Error; err != nil {
		t.Fatalf("统计场景失败: %v", err)
	}
	return count
}

func countReminders(t *testing.T, svc *Service) int64 {
	t.Helper()
	var count int64
	if err := svc.db.Model(&model.Reminder{}).Count(&count).Error; err != nil {
		t.Fatalf("统计提醒失败: %v", err)
	}
	return count
}

func TestListScenarioTemplates(t *testing.T) {
	svc, _ := newTemplateTestService(t)
	catalog := svc.ListScenarioTemplates()
	if len(catalog.Templates) != 5 || len(catalog.Goals) != 5 {
		t.Fatalf("目录 = %d 个模板 / %d 个目标，期望 5/5", len(catalog.Templates), len(catalog.Goals))
	}
}

func TestApplyScenarioTemplateFullCopy(t *testing.T) {
	svc, now := newTemplateTestService(t)
	result, err := svc.ApplyScenarioTemplate("sedentary-office", "", "", nil, false)
	if err != nil {
		t.Fatalf("应用模板失败: %v", err)
	}
	if result == nil || result.Scenario == nil {
		t.Fatal("结果缺少场景")
	}
	scenario := result.Scenario
	if scenario.ID == 0 {
		t.Fatal("场景未落库")
	}
	if scenario.Name != "久坐办公" {
		t.Errorf("场景名 = %q，期望 久坐办公", scenario.Name)
	}
	if strings.TrimSpace(scenario.Description) == "" {
		t.Error("场景描述不应为空")
	}
	if scenario.Summary != "标准坐班节奏：喝水、起身、走动、护眼与如厕均衡覆盖。" {
		t.Errorf("场景摘要 = %q", scenario.Summary)
	}
	if scenario.Enabled {
		t.Error("场景页路径应默认停用")
	}
	if scenario.GeneratedAt != now.Unix() {
		t.Errorf("GeneratedAt = %d，期望 %d", scenario.GeneratedAt, now.Unix())
	}
	if len(result.Reminders) != 5 {
		t.Fatalf("提醒数 = %d，期望 5", len(result.Reminders))
	}
	wantKinds := []string{model.KindWater, model.KindStand, model.KindMove, model.KindRest, model.KindToilet}
	for i, item := range result.Reminders {
		if item.ScenarioID == nil || *item.ScenarioID != scenario.ID {
			t.Errorf("提醒[%d] 场景 ID 不符: %+v", i, item.ScenarioID)
		}
		if item.Kind != wantKinds[i] {
			t.Errorf("提醒[%d] kind = %q，期望 %q", i, item.Kind, wantKinds[i])
		}
		if item.SortOrder != i {
			t.Errorf("提醒[%d] SortOrder = %d，期望 %d", i, item.SortOrder, i)
		}
		if !item.Enabled {
			t.Errorf("提醒[%d] 应启用", i)
		}
	}
	if result.NextFireAt <= now.Unix() {
		t.Errorf("NextFireAt = %d，应晚于当前时刻 %d", result.NextFireAt, now.Unix())
	}
	if got := countScenarios(t, svc); got != 1 {
		t.Errorf("场景数 = %d，期望 1", got)
	}
	if got := countReminders(t, svc); got != 5 {
		t.Errorf("提醒数 = %d，期望 5", got)
	}
}

func TestApplyScenarioTemplatePersonalizedEnabled(t *testing.T) {
	svc, now := newTemplateTestService(t)
	old, err := svc.CreateScenario("旧场景", "旧计划")
	if err != nil {
		t.Fatalf("创建旧场景失败: %v", err)
	}
	if err := svc.SetScenarioEnabled(old.ID, true); err != nil {
		t.Fatalf("启用旧场景失败: %v", err)
	}

	result, err := svc.ApplyScenarioTemplate("sedentary-office", "10:00", "11:00",
		[]string{model.KindWater, model.KindMove}, true)
	if err != nil {
		t.Fatalf("个性化应用模板失败: %v", err)
	}
	if !result.Scenario.Enabled {
		t.Error("引导路径应自动启用")
	}
	if result.Scenario.GeneratedAt != now.Unix() {
		t.Errorf("GeneratedAt = %d，期望 %d", result.Scenario.GeneratedAt, now.Unix())
	}
	if !strings.Contains(result.Scenario.Summary, "保留 2 条") || !strings.Contains(result.Scenario.Summary, "10:00–11:00") {
		t.Errorf("摘要未体现保留条数/工作时段: %q", result.Scenario.Summary)
	}
	if len(result.Reminders) != 2 {
		t.Fatalf("提醒数 = %d，期望 2", len(result.Reminders))
	}
	for _, item := range result.Reminders {
		if item.Kind != model.KindWater && item.Kind != model.KindMove {
			t.Errorf("保留了两类之外的提醒: %q", item.Kind)
		}
		if item.WindowStart != "10:00" || item.WindowEnd != "11:00" {
			t.Errorf("%s 窗口 = %s–%s，期望 10:00–11:00", item.Kind, item.WindowStart, item.WindowEnd)
		}
		if item.DailyTarget != 1 {
			t.Errorf("%s 目标 = %d，期望夹到 1", item.Kind, item.DailyTarget)
		}
	}

	// 单选语义：旧场景应被停用。
	var oldAfter model.Scenario
	if err := svc.db.First(&oldAfter, old.ID).Error; err != nil {
		t.Fatalf("读取旧场景失败: %v", err)
	}
	if oldAfter.Enabled {
		t.Error("启用新场景后旧场景仍处于启用状态")
	}
	if result.NextFireAt <= now.Unix() {
		t.Errorf("NextFireAt = %d，应晚于当前时刻", result.NextFireAt)
	}
}

func TestApplyScenarioTemplateRejectsMissingGoal(t *testing.T) {
	svc, _ := newTemplateTestService(t)
	_, err := svc.ApplyScenarioTemplate("sedentary-office", "17:00", "17:10",
		[]string{model.KindWater, model.KindMove}, false)
	if err == nil {
		t.Fatal("缺失目标应返回错误")
	}
	var ae *apperr.Error
	if !errors.As(err, &ae) || ae.Code != apperr.CodeValidation {
		t.Fatalf("期望 validation 错误，实际 %v", err)
	}
	if got := countScenarios(t, svc); got != 0 {
		t.Errorf("失败后仍写入 %d 个场景", got)
	}
	if got := countReminders(t, svc); got != 0 {
		t.Errorf("失败后仍写入 %d 条提醒", got)
	}
}

func TestApplyScenarioTemplateDuplicateCopies(t *testing.T) {
	svc, _ := newTemplateTestService(t)
	first, err := svc.ApplyScenarioTemplate("eye-care-first", "", "", nil, false)
	if err != nil {
		t.Fatalf("第一次应用失败: %v", err)
	}
	second, err := svc.ApplyScenarioTemplate("eye-care-first", "", "", nil, false)
	if err != nil {
		t.Fatalf("第二次应用失败: %v", err)
	}
	if first.Scenario.ID == second.Scenario.ID {
		t.Fatal("两次应用返回了同一个场景")
	}
	if got := countScenarios(t, svc); got != 2 {
		t.Errorf("场景数 = %d，期望 2", got)
	}
	if got := countReminders(t, svc); got != 10 {
		t.Errorf("提醒数 = %d，期望 10", got)
	}
}

func TestPreviewScenarioTemplateDoesNotWrite(t *testing.T) {
	svc, now := newTemplateTestService(t)
	preview, err := svc.PreviewScenarioTemplate("sedentary-office", "10:00", "11:00",
		[]string{model.KindWater, model.KindMove})
	if err != nil {
		t.Fatalf("预览失败: %v", err)
	}
	if !preview.Valid || len(preview.Reminders) != 2 {
		t.Fatalf("预览无效: valid=%v len=%d", preview.Valid, len(preview.Reminders))
	}
	if preview.NextFireAt <= now.Unix() {
		t.Errorf("NextFireAt = %d，应晚于当前时刻", preview.NextFireAt)
	}
	if got := countScenarios(t, svc); got != 0 {
		t.Errorf("预览写入了 %d 个场景", got)
	}
	if got := countReminders(t, svc); got != 0 {
		t.Errorf("预览写入了 %d 条提醒", got)
	}

	missing, err := svc.PreviewScenarioTemplate("sedentary-office", "17:00", "17:10",
		[]string{model.KindWater, model.KindMove})
	if err != nil {
		t.Fatalf("缺失目标预览失败: %v", err)
	}
	if missing.Valid {
		t.Error("缺失目标时 Valid 应为 false")
	}
	if len(missing.MissingGoals) != 1 || missing.MissingGoals[0] != model.KindMove {
		t.Errorf("MissingGoals = %v，期望 [move]", missing.MissingGoals)
	}
}

func TestApplyScenarioTemplateUnknownKey(t *testing.T) {
	svc, _ := newTemplateTestService(t)
	_, err := svc.ApplyScenarioTemplate("no-such-template", "", "", nil, false)
	if err == nil {
		t.Fatal("未知模板应返回错误")
	}
	var ae *apperr.Error
	if !errors.As(err, &ae) || ae.Code != apperr.CodeNotFound {
		t.Fatalf("期望 not_found 错误，实际 %v", err)
	}
}

func TestApplyScenarioTemplateInvalidOptions(t *testing.T) {
	svc, _ := newTemplateTestService(t)
	_, err := svc.ApplyScenarioTemplate("sedentary-office", "09:00", "18:00",
		[]string{model.KindWater}, false)
	if err == nil {
		t.Fatal("单个目标应返回错误")
	}
	var ae *apperr.Error
	if !errors.As(err, &ae) || ae.Code != apperr.CodeValidation {
		t.Fatalf("期望 validation 错误，实际 %v", err)
	}
	if got := countScenarios(t, svc); got != 0 {
		t.Errorf("非法入参写入了 %d 个场景", got)
	}
}

func TestPersonalizePreviewTargetsMatchTemplate(t *testing.T) {
	svc, _ := newTemplateTestService(t)
	catalog := svc.ListScenarioTemplates()
	for _, tpl := range catalog.Templates {
		preview, err := svc.PreviewScenarioTemplate(tpl.Key, "", "", nil)
		if err != nil {
			t.Fatalf("预览 %s 失败: %v", tpl.Key, err)
		}
		if len(preview.Reminders) != len(tpl.Reminders) {
			t.Fatalf("%s 预览提醒数 = %d，期望 %d", tpl.Key, len(preview.Reminders), len(tpl.Reminders))
		}
		for i := range tpl.Reminders {
			want := tpl.Reminders[i]
			got := preview.Reminders[i]
			if got != want {
				t.Errorf("%s 第 %d 条提醒 = %+v，期望 %+v", tpl.Key, i, got, want)
			}
		}
	}
}

// TestApplyScenarioTemplateNormalizesSummaryTimes 摘要里的时段统一用 HH:MM。
//
// 引导的时间选择器总是补零，但绑定是公开入口；这里锁住「9:00」也会落成
// 「09:00」，避免摘要出现两种写法。
func TestApplyScenarioTemplateNormalizesSummaryTimes(t *testing.T) {
	svc, _ := newTemplateTestService(t)
	result, err := svc.ApplyScenarioTemplate("sedentary-office", "9:00", "18:00",
		[]string{model.KindWater, model.KindRest}, false)
	if err != nil {
		t.Fatalf("应用模板失败: %v", err)
	}
	if !strings.Contains(result.Scenario.Summary, "09:00–18:00") {
		t.Errorf("摘要未归一化时段: %q", result.Scenario.Summary)
	}
}
