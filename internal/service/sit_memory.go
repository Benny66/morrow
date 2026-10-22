package service

import (
	"fmt"
	"time"

	"morrow/internal/apperr"
	"morrow/internal/model"
	"morrow/internal/sit"
)

// SitMemoryContext 是一次记忆句选择所需的派生事实与近期已用模板。
type SitMemoryContext struct {
	Facts               sit.MemoryFacts
	QuoteIDs            []string
	NudgeTemplateIDs    []string
	GreetingTemplateIDs []string
}

// SitMemoryContext 汇总最近 28 天的类别级事实与 3 天内的模板使用历史。
//
// profileEnabled=false 时不查询任何应用会话，避免关闭画像后仍读取应用身份。
func (s *Service) SitMemoryContext(now time.Time, profileEnabled bool, currentContinuous int) (SitMemoryContext, error) {
	var out SitMemoryContext
	localNow := now.In(time.Local)
	today := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, time.Local)
	start := today.AddDate(0, 0, -27)
	weekStart := today.AddDate(0, 0, -6)

	days := make([]sit.DayFact, 0, 28)
	dayIndex := map[string]int{}
	sessionDay := map[uint]string{}
	for i := 0; i < 28; i++ {
		date := start.AddDate(0, 0, i)
		dayIndex[date.Format(model.DateLayout)] = len(days)
		days = append(days, sit.DayFact{Date: date})
	}

	var sessions []model.SitSession
	if err := s.db.
		Where("date_key >= ? AND date_key <= ?", start.Format(model.DateLayout), today.Format(model.DateLayout)).
		Find(&sessions).Error; err != nil {
		return out, apperr.Wrap(fmt.Errorf("读取久坐记忆事实失败: %w", err))
	}
	for _, row := range sessions {
		idx, ok := dayIndex[row.DateKey]
		if !ok {
			continue
		}
		day := &days[idx]
		minutes := row.ActiveSeconds / 60
		day.SittingMinutes += minutes
		if minutes > day.LongestMinutes {
			day.LongestMinutes = minutes
		}
		sessionDay[row.ID] = row.DateKey
		started := time.Unix(row.StartedAt, 0).In(time.Local)
		switch hour := started.Hour(); {
		case hour >= 12 && hour < 18:
			day.AfternoonMinutes += minutes
		case hour >= 18 && hour < 22:
			day.EveningMinutes += minutes
		case hour >= 22 || hour < 6:
			day.LateNightMinutes += minutes
		}
	}

	var breaks []model.SitBreak
	if err := s.db.
		Where("at >= ?", start.Unix()).
		Find(&breaks).Error; err != nil {
		return out, apperr.Wrap(fmt.Errorf("读取休息记忆事实失败: %w", err))
	}
	for _, row := range breaks {
		dateKey, ok := sessionDay[row.SitSessionID]
		if !ok {
			continue
		}
		idx, ok := dayIndex[dateKey]
		if !ok {
			continue
		}
		switch row.Kind {
		case string(sit.BreakKindActive):
			days[idx].ActiveBreaks++
		case string(sit.BreakKindLock):
			days[idx].LockBreaks++
		case string(sit.BreakKindIdle):
			days[idx].IdleBreaks++
		}
	}

	var skips, snoozes int64
	if err := s.db.Model(&model.CheckIn{}).
		Where("date_key >= ? AND action = ?", weekStart.Format(model.DateLayout), model.ActionSkip).
		Count(&skips).Error; err != nil {
		return out, apperr.Wrap(fmt.Errorf("统计跳过次数失败: %w", err))
	}
	if err := s.db.Model(&model.CheckIn{}).
		Where("date_key >= ? AND action = ?", weekStart.Format(model.DateLayout), model.ActionSnooze).
		Count(&snoozes).Error; err != nil {
		return out, apperr.Wrap(fmt.Errorf("统计推迟次数失败: %w", err))
	}

	if profileEnabled {
		var apps []model.AppUsageSession
		if err := s.db.
			Where("date_key >= ? AND ended_at > 0", weekStart.Format(model.DateLayout)).
			Find(&apps).Error; err != nil {
			return out, apperr.Wrap(fmt.Errorf("读取类别记忆事实失败: %w", err))
		}
		overrides, err := s.loadCategoryOverrides()
		if err != nil {
			return out, err
		}
		categorySeconds := map[int]map[sit.Category]int64{}
		for _, row := range apps {
			idx, ok := dayIndex[row.DateKey]
			if !ok {
				continue
			}
			duration := row.EndedAt - row.StartedAt
			if duration <= 0 {
				continue
			}
			category := s.resolveCategory(overrides, row.Platform, row.AppID)
			if categorySeconds[idx] == nil {
				categorySeconds[idx] = map[sit.Category]int64{}
			}
			categorySeconds[idx][category] += duration
		}
		for idx, byCategory := range categorySeconds {
			days[idx].CategoryMinutes = map[sit.Category]int{}
			for category, seconds := range byCategory {
				days[idx].CategoryMinutes[category] = int(seconds / 60)
			}
		}
	}

	historySince := now.Add(-72 * time.Hour).Unix()
	var speeches []model.SitSpeech
	if err := s.db.
		Where("created_at >= ?", historySince).
		Find(&speeches).Error; err != nil {
		return out, apperr.Wrap(fmt.Errorf("读取记忆模板历史失败: %w", err))
	}
	for _, row := range speeches {
		if row.MemoryID != "" {
			out.QuoteIDs = append(out.QuoteIDs, row.MemoryID)
		}
		switch row.Kind {
		case string(sit.BubbleNudge):
			out.NudgeTemplateIDs = append(out.NudgeTemplateIDs, row.TemplateID)
		case string(sit.BubbleGreeting):
			out.GreetingTemplateIDs = append(out.GreetingTemplateIDs, row.TemplateID)
		}
	}

	out.Facts = sit.DeriveFacts(localNow, days, int(skips), int(snoozes), profileEnabled, currentContinuous)
	return out, nil
}
