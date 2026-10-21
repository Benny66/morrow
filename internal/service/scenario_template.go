package service

import (
	"fmt"
	"time"

	"gorm.io/gorm"

	"morrow/internal/apperr"
	"morrow/internal/model"
	"morrow/internal/reminder"
	"morrow/internal/scenariotemplate"
)

// ApplyTemplateResult 是模板实例化的结果。
//
// 同时返回场景、提醒与下一次计划提醒：完成页要一次性展示，避免前端再查一次。
type ApplyTemplateResult struct {
	Scenario   *model.Scenario  `json:"scenario"`
	Reminders  []model.Reminder `json:"reminders"`
	NextFireAt int64            `json:"next_fire_at"`
}

// ListScenarioTemplates 返回内置场景模板目录（不调用 AI、不需要数据库）。
func (s *Service) ListScenarioTemplates() scenariotemplate.Catalog {
	return scenariotemplate.LoadCatalog()
}

// PreviewScenarioTemplate 返回按工作时段与关注目标个性化后的提醒预览；不写库。
func (s *Service) PreviewScenarioTemplate(key, workStart, workEnd string, goals []string) (*scenariotemplate.Preview, error) {
	tpl, ok := scenariotemplate.Find(key)
	if !ok {
		return nil, apperr.NotFound("场景模板不存在")
	}
	preview, err := scenariotemplate.Personalize(tpl, scenariotemplate.Options{
		WorkStart: workStart,
		WorkEnd:   workEnd,
		Goals:     goals,
	})
	if err != nil {
		return nil, apperr.Wrap(err)
	}
	preview.NextFireAt = nextDraftFireAt(s.now(), preview.Reminders)
	return &preview, nil
}

// ApplyScenarioTemplate 把模板复制成一个普通场景及提醒项。
//
// 【事务语义】场景、提醒与「停用其他场景」必须原子：中途失败时不能留下一个
// 空场景或半套提醒，否则用户会在计划页看到一个无法解释的残缺计划。
func (s *Service) ApplyScenarioTemplate(key, workStart, workEnd string, goals []string, enable bool) (*ApplyTemplateResult, error) {
	tpl, ok := scenariotemplate.Find(key)
	if !ok {
		return nil, apperr.NotFound("场景模板不存在")
	}
	preview, err := scenariotemplate.Personalize(tpl, scenariotemplate.Options{
		WorkStart: workStart,
		WorkEnd:   workEnd,
		Goals:     goals,
	})
	if err != nil {
		return nil, apperr.Wrap(err)
	}
	if !preview.Valid {
		return nil, apperr.Validation("所选工作时段内没有关注目标对应的提醒，请调整时段或目标")
	}

	now := s.now()
	summaryStart, summaryEnd := workStart, workEnd
	if start, ok := reminder.ParseClock(workStart); ok {
		summaryStart = reminder.FormatClock(start)
	}
	if end, ok := reminder.ParseClock(workEnd); ok {
		summaryEnd = reminder.FormatClock(end)
	}
	summary := tpl.Summary
	if workStart != "" && workEnd != "" {
		summary = fmt.Sprintf("%s（保留 %d 条 · %s–%s）", tpl.Summary, len(preview.Reminders), summaryStart, summaryEnd)
	}

	scenario := &model.Scenario{
		Name:        tpl.Name,
		Description: tpl.Description,
		Summary:     summary,
		Enabled:     enable,
		GeneratedAt: now.Unix(),
	}
	created := make([]model.Reminder, 0, len(preview.Reminders))

	txErr := s.db.Transaction(func(tx *gorm.DB) error {
		// 新场景尚未创建，故这里可以停用全部现有场景，不需要排除自己。
		if enable {
			if err := tx.Model(&model.Scenario{}).Where("enabled = ?", true).
				Update("enabled", false).Error; err != nil {
				return fmt.Errorf("停用其他场景失败: %w", err)
			}
		}
		if err := tx.Create(scenario).Error; err != nil {
			return fmt.Errorf("创建场景失败: %w", err)
		}
		scenarioID := scenario.ID
		for index, item := range preview.Reminders {
			scenarioIDCopy := scenarioID
			record := model.Reminder{
				ScenarioID:  &scenarioIDCopy,
				Title:       item.Title,
				Content:     item.Content,
				Kind:        item.Kind,
				IntervalMin: item.IntervalMin,
				WindowStart: item.WindowStart,
				WindowEnd:   item.WindowEnd,
				DailyTarget: item.DailyTarget,
				Enabled:     true,
				SortOrder:   index,
			}
			if err := tx.Create(&record).Error; err != nil {
				return fmt.Errorf("保存模板提醒失败: %w", err)
			}
			created = append(created, record)
		}
		return nil
	})
	if txErr != nil {
		return nil, apperr.Wrap(txErr)
	}

	return &ApplyTemplateResult{
		Scenario:   scenario,
		Reminders:  created,
		NextFireAt: nextReminderFireAt(now, created),
	}, nil
}

// nextDraftFireAt 返回一组模板草稿中最早的计划触发时间；没有则 0。
func nextDraftFireAt(now time.Time, items []scenariotemplate.Reminder) int64 {
	var next int64
	for _, item := range items {
		point := reminder.NextFireAt(now, item.WindowStart, item.WindowEnd, item.IntervalMin)
		if point > 0 && (next == 0 || point < next) {
			next = point
		}
	}
	return next
}

// nextReminderFireAt 返回一组已落库提醒中最早的计划触发时间；没有则 0。
func nextReminderFireAt(now time.Time, items []model.Reminder) int64 {
	var next int64
	for _, item := range items {
		point := reminder.NextFireAt(now, item.WindowStart, item.WindowEnd, item.IntervalMin)
		if point > 0 && (next == 0 || point < next) {
			next = point
		}
	}
	return next
}
