package service

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"morrow/internal/apperr"
	"morrow/internal/model"
	"morrow/internal/pause"
	"morrow/internal/reminder"
	"morrow/internal/sit"
)

// CreditResult 是一次久坐目标抵扣的结果。
type CreditResult struct {
	Credited      bool
	ReminderID    uint
	ReminderTitle string
}

// AppUsage 是某天某应用的前台使用聚合（只在应用画像开启时有数据）。
type AppUsage struct {
	Platform    string       `json:"platform"`
	AppID       string       `json:"app_id"`
	DisplayName string       `json:"display_name"`
	Category    sit.Category `json:"category"`
	Minutes     int          `json:"minutes"`
	Sessions    int          `json:"sessions"`
}

// CategoryUsage 是某天行为类别的聚合。
type CategoryUsage struct {
	Category sit.Category `json:"category"`
	Minutes  int          `json:"minutes"`
	Sessions int          `json:"sessions"`
}

// SitDay 是某一天已收尾会话的久坐/休息聚合；当前开放会话由快照补充。
type SitDay struct {
	DateKey               string          `json:"date_key"`
	SittingMinutes        int             `json:"sitting_minutes"`
	LongestSessionMinutes int             `json:"longest_session_minutes"`
	ActiveBreaks          int             `json:"active_breaks"`
	LockBreaks            int             `json:"lock_breaks"`
	IdleBreaks            int             `json:"idle_breaks"`
	CreditedCount         int             `json:"credited_count"`
	Apps                  []AppUsage      `json:"apps"`
	Categories            []CategoryUsage `json:"categories"`
}

// errSitCreditAlready 是「本会话已抵扣」的内部哨兵，不向上暴露。
var errSitCreditAlready = errors.New("sit session already credited")

// SitBudget 是当前日期/当前久坐会话的主动发言预算事实。
type SitBudget struct {
	SessionNudges   int
	DailyProactive  int
	LastProactiveAt int64
	FirstNudgeAckAt int64
}

// StartSitSession 创建一段连续用机会话。
func (s *Service) StartSitSession(at time.Time) (*model.SitSession, error) {
	row := &model.SitSession{
		StartedAt:  at.Unix(),
		LastSeenAt: at.Unix(),
		DateKey:    at.Format(model.DateLayout),
	}
	if err := s.db.Create(row).Error; err != nil {
		return nil, apperr.Wrap(fmt.Errorf("创建久坐会话失败: %w", err))
	}
	return row, nil
}

// TouchSitSession 更新开放会话的活跃秒数与心跳；已收尾的会话忽略。
func (s *Service) TouchSitSession(id uint, activeSeconds int, at time.Time) error {
	if id == 0 {
		return nil
	}
	if activeSeconds < 0 {
		activeSeconds = 0
	}
	if err := s.db.Model(&model.SitSession{}).
		Where("id = ? AND ended_at = 0", id).
		Updates(map[string]any{
			"active_seconds": activeSeconds,
			"last_seen_at":   at.Unix(),
		}).Error; err != nil {
		return apperr.Wrap(fmt.Errorf("更新久坐会话失败: %w", err))
	}
	return nil
}

// EndSitSession 收尾一段久坐会话。
func (s *Service) EndSitSession(id uint, endReason string, activeSeconds int, at time.Time) error {
	if id == 0 {
		return nil
	}
	if activeSeconds < 0 {
		activeSeconds = 0
	}
	if err := s.db.Model(&model.SitSession{}).
		Where("id = ? AND ended_at = 0", id).
		Updates(map[string]any{
			"ended_at":       at.Unix(),
			"last_seen_at":   at.Unix(),
			"active_seconds": activeSeconds,
			"end_reason":     endReason,
		}).Error; err != nil {
		return apperr.Wrap(fmt.Errorf("收尾久坐会话失败: %w", err))
	}
	return nil
}

// StartSitBreak 记录一次真实休息；durationSec > 0 表示时长已知（如空闲休息）。
func (s *Service) StartSitBreak(sessionID uint, at time.Time, kind string, durationSec int) (*model.SitBreak, error) {
	if durationSec < 0 {
		durationSec = 0
	}
	row := &model.SitBreak{
		SitSessionID: sessionID,
		At:           at.Unix(),
		Kind:         kind,
		DurationSec:  durationSec,
	}
	if err := s.db.Create(row).Error; err != nil {
		return nil, apperr.Wrap(fmt.Errorf("记录休息失败: %w", err))
	}
	return row, nil
}

// FinishSitBreak 补齐主动/锁屏休息的时长。
func (s *Service) FinishSitBreak(id uint, durationSec int) error {
	if id == 0 {
		return nil
	}
	if durationSec < 0 {
		durationSec = 0
	}
	if err := s.db.Model(&model.SitBreak{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"duration_sec": durationSec,
		}).Error; err != nil {
		return apperr.Wrap(fmt.Errorf("更新休息时长失败: %w", err))
	}
	return nil
}

// SitSpeechInput 创建一条待展示宠物发言所需的事实。
// 用结构体而不是位置参数，避免 templateID/memoryID/text 三个字符串错位。
type SitSpeechInput struct {
	DateKey    string
	SessionID  uint
	Kind       string
	TemplateID string
	MemoryID   string
	Text       string
	Now        time.Time
	TTL        time.Duration
}

// CreateSitSpeech 创建一条待展示的宠物发言。
func (s *Service) CreateSitSpeech(in SitSpeechInput) (*model.SitSpeech, error) {
	ttl := in.TTL
	if ttl <= 0 {
		ttl = time.Minute
	}
	row := &model.SitSpeech{
		DateKey:      in.DateKey,
		SitSessionID: in.SessionID,
		Kind:         in.Kind,
		TemplateID:   in.TemplateID,
		MemoryID:     in.MemoryID,
		Text:         in.Text,
		ExpiresAt:    in.Now.Add(ttl).Unix(),
	}
	if err := s.db.Create(row).Error; err != nil {
		return nil, apperr.Wrap(fmt.Errorf("创建宠物发言失败: %w", err))
	}
	return row, nil
}

// PendingSitSpeech 返回最早一条未确认且未过期的宠物发言。
func (s *Service) PendingSitSpeech(now time.Time) (*model.SitSpeech, error) {
	var rows []model.SitSpeech
	if err := s.db.
		Where("acked_at = 0 AND expires_at > ?", now.Unix()).
		Order("id ASC").
		Limit(1).
		Find(&rows).Error; err != nil {
		return nil, apperr.Wrap(fmt.Errorf("查询宠物发言失败: %w", err))
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// SitSpeech 按 ID 查一条宠物发言；不存在返回 nil。
func (s *Service) SitSpeech(id uint) (*model.SitSpeech, error) {
	var row model.SitSpeech
	if err := s.db.First(&row, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, apperr.Wrap(fmt.Errorf("查询宠物发言失败: %w", err))
	}
	return &row, nil
}

// AckSitSpeech 标记一条宠物发言已展示；幂等。
func (s *Service) AckSitSpeech(id uint, now time.Time) (bool, error) {
	if id == 0 {
		return false, nil
	}
	var row model.SitSpeech
	if err := s.db.First(&row, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, apperr.Wrap(fmt.Errorf("查询宠物发言失败: %w", err))
	}
	if row.AckedAt != 0 {
		return true, nil
	}
	res := s.db.Model(&model.SitSpeech{}).
		Where("id = ? AND acked_at = 0", id).
		Update("acked_at", now.Unix())
	if res.Error != nil {
		return false, apperr.Wrap(fmt.Errorf("确认宠物发言失败: %w", res.Error))
	}
	return res.RowsAffected > 0, nil
}

// ExpireSitSpeeches 让所有未确认发言立即过期（今天先这样/切换语气时清场）。
func (s *Service) ExpireSitSpeeches(now time.Time) error {
	if err := s.db.Model(&model.SitSpeech{}).
		Where("acked_at = 0 AND expires_at > ?", now.Unix()).
		Update("expires_at", now.Unix()).Error; err != nil {
		return apperr.Wrap(fmt.Errorf("清理宠物发言失败: %w", err))
	}
	return nil
}

// SitBudget 返回今天的主动发言预算与当前会话的软提醒事实。
func (s *Service) SitBudget(dateKey string, sessionID uint) (SitBudget, error) {
	var out SitBudget
	countKinds := []string{string(sit.BubbleNudge), string(sit.BubbleMemory)}

	var daily int64
	if err := s.db.Model(&model.SitSpeech{}).
		Where("date_key = ? AND acked_at > 0 AND kind IN ?", dateKey, countKinds).
		Count(&daily).Error; err != nil {
		return out, apperr.Wrap(fmt.Errorf("统计主动发言失败: %w", err))
	}
	out.DailyProactive = int(daily)

	if sessionID != 0 {
		var session int64
		if err := s.db.Model(&model.SitSpeech{}).
			Where("sit_session_id = ? AND acked_at > 0 AND kind = ?", sessionID, string(sit.BubbleNudge)).
			Count(&session).Error; err != nil {
			return out, apperr.Wrap(fmt.Errorf("统计会话软提醒失败: %w", err))
		}
		out.SessionNudges = int(session)

		var last int64
		if err := s.db.Model(&model.SitSpeech{}).
			Where("date_key = ? AND acked_at > 0 AND kind IN ?", dateKey, countKinds).
			Select("COALESCE(MAX(acked_at), 0)").
			Row().Scan(&last); err != nil {
			return out, apperr.Wrap(fmt.Errorf("统计最近主动发言失败: %w", err))
		}
		out.LastProactiveAt = last

		var first int64
		if err := s.db.Model(&model.SitSpeech{}).
			Where("sit_session_id = ? AND acked_at > 0 AND kind = ?", sessionID, string(sit.BubbleNudge)).
			Select("COALESCE(MIN(acked_at), 0)").
			Row().Scan(&first); err != nil {
			return out, apperr.Wrap(fmt.Errorf("统计首条软提醒失败: %w", err))
		}
		out.FirstNudgeAckAt = first
	}
	return out, nil
}

// SitDayClosedTotals 返回某天已收尾会话的久坐秒数总和与最长会话。
// 开放会话由运行层用内存时钟补上，避免读到最长 60 秒前的旧值。
func (s *Service) SitDayClosedTotals(dateKey string) (totalSeconds int, longestSeconds int, err error) {
	var row struct {
		Total   int64
		Longest int64
	}
	if err := s.db.Model(&model.SitSession{}).
		Select("COALESCE(SUM(active_seconds), 0) AS total, COALESCE(MAX(active_seconds), 0) AS longest").
		Where("date_key = ? AND ended_at > 0", dateKey).
		Scan(&row).Error; err != nil {
		return 0, 0, apperr.Wrap(fmt.Errorf("统计今日久坐失败: %w", err))
	}
	return int(row.Total), int(row.Longest), nil
}

// CreditNearestStandMove 把一次真实休息抵扣到当前场景最近到点的 stand/move：
//   - 优先补完当前场景已存在的待处理 stand/move；
//   - 否则选今天仍有剩余触发点、下一条最先到点的那条；
//   - 同一个 sitSessionID 只允许一次（active break 与锁屏共用）。
func (s *Service) CreditNearestStandMove(scenarioID, sitSessionID uint, now time.Time, source string) (CreditResult, error) {
	s.sitCreditMu.Lock()
	defer s.sitCreditMu.Unlock()

	var out CreditResult
	normalizedSource := normalizeCheckInSource(source)

	if sitSessionID != 0 {
		var session model.SitSession
		if err := s.db.First(&session, sitSessionID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return out, apperr.NotFound("久坐会话不存在")
			}
			return out, apperr.Wrap(fmt.Errorf("查询久坐会话失败: %w", err))
		}
		if session.CreditedReminderID != 0 || session.CreditedCheckInID != 0 {
			return out, nil
		}
	}

	// 1) 当前场景内待处理的 stand/move：优先补完最早的一条。
	pending, err := s.ListPendingFires()
	if err != nil {
		return out, err
	}
	for _, fire := range pending {
		if fire.ScenarioID != scenarioID {
			continue
		}
		if fire.Kind != model.KindStand && fire.Kind != model.KindMove {
			continue
		}
		if err := s.actWithSource(fire.FireID, model.ActionDone, 0, normalizedSource); err != nil {
			return out, err
		}
		checkInID := uint(0)
		var checkIn model.CheckIn
		if err := s.db.
			Where("reminder_id = ? AND scheduled_at = ? AND source = ?", fire.ReminderID, fire.FireAt, normalizedSource).
			Order("id DESC").
			First(&checkIn).Error; err == nil {
			checkInID = checkIn.ID
		}
		if sitSessionID != 0 {
			if err := s.db.Model(&model.SitSession{}).
				Where("id = ? AND credited_reminder_id = 0 AND credited_check_in_id = 0", sitSessionID).
				Updates(map[string]any{
					"credited_reminder_id": fire.ReminderID,
					"credited_check_in_id": checkInID,
				}).Error; err != nil {
				return out, apperr.Wrap(fmt.Errorf("标记久坐会话抵扣失败: %w", err))
			}
		}
		return CreditResult{Credited: true, ReminderID: fire.ReminderID, ReminderTitle: fire.Title}, nil
	}

	// 2) 今天仍有剩余触发点的 stand/move 中选下一条最先到点的。
	items, err := s.ActiveReminders(scenarioID)
	if err != nil {
		return out, err
	}
	today := now.Format(model.DateLayout)
	var best *model.Reminder
	var bestNext int64
	for i := range items {
		item := &items[i]
		if !item.Enabled || item.OnceAt != 0 {
			continue
		}
		if item.Kind != model.KindStand && item.Kind != model.KindMove {
			continue
		}
		next := reminder.NextFireAt(now, item.WindowStart, item.WindowEnd, item.IntervalMin)
		if next <= 0 {
			continue
		}
		if time.Unix(next, 0).Format(model.DateLayout) != today {
			continue
		}
		if best == nil || next < bestNext {
			best = item
			bestNext = next
		}
	}
	if best == nil {
		return out, nil
	}

	txErr := s.db.Transaction(func(tx *gorm.DB) error {
		if sitSessionID != 0 {
			var session model.SitSession
			if err := tx.First(&session, sitSessionID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return apperr.NotFound("久坐会话不存在")
				}
				return err
			}
			if session.CreditedReminderID != 0 || session.CreditedCheckInID != 0 {
				return errSitCreditAlready
			}
		}
		record := &model.CheckIn{
			ReminderID:  best.ID,
			ScenarioID:  scenarioID,
			Action:      model.ActionDone,
			Source:      normalizedSource,
			ScheduledAt: 0,
			ActedAt:     now.Unix(),
			DateKey:     today,
		}
		if err := tx.Create(record).Error; err != nil {
			return fmt.Errorf("写入主动休息打卡失败: %w", err)
		}
		if sitSessionID != 0 {
			res := tx.Model(&model.SitSession{}).
				Where("id = ? AND credited_reminder_id = 0 AND credited_check_in_id = 0", sitSessionID).
				Updates(map[string]any{
					"credited_reminder_id": best.ID,
					"credited_check_in_id": record.ID,
				})
			if res.Error != nil {
				return fmt.Errorf("标记久坐会话抵扣失败: %w", res.Error)
			}
			if res.RowsAffected == 0 {
				return errSitCreditAlready
			}
		}
		return nil
	})
	if errors.Is(txErr, errSitCreditAlready) {
		return out, nil
	}
	if txErr != nil {
		return out, apperr.Wrap(txErr)
	}
	return CreditResult{
		Credited:      true,
		ReminderID:    best.ID,
		ReminderTitle: best.Title,
	}, nil
}

// GetSitDay 返回某一天已收尾会话的聚合；开放会话由运行时快照补充。
func (s *Service) GetSitDay(dateKey string) (SitDay, error) {
	if _, err := time.ParseInLocation(model.DateLayout, dateKey, time.Local); err != nil {
		return SitDay{}, apperr.Validation("日期格式应为 YYYY-MM-DD")
	}
	out := SitDay{DateKey: dateKey, Apps: []AppUsage{}, Categories: []CategoryUsage{}}
	// 未来日期不返回任何聚合：避免手工改库或时钟漂移时把未来数据算进页面。
	if dateKey > s.now().Format(model.DateLayout) {
		return out, nil
	}

	var totals struct {
		Total   int64
		Longest int64
	}
	if err := s.db.Model(&model.SitSession{}).
		Select("COALESCE(SUM(active_seconds), 0) AS total, COALESCE(MAX(active_seconds), 0) AS longest").
		Where("date_key = ? AND ended_at > 0 AND deleted_at IS NULL", dateKey).
		Scan(&totals).Error; err != nil {
		return out, apperr.Wrap(fmt.Errorf("统计久坐日聚合失败: %w", err))
	}
	out.SittingMinutes = int(totals.Total) / 60
	out.LongestSessionMinutes = int(totals.Longest) / 60

	var breaks []struct {
		Kind string
		Cnt  int
	}
	if err := s.db.Table("sit_breaks AS b").
		Select("b.kind AS kind, COUNT(*) AS cnt").
		Joins("JOIN sit_sessions AS s ON s.id = b.sit_session_id AND s.deleted_at IS NULL").
		Where("s.date_key = ? AND b.deleted_at IS NULL", dateKey).
		Group("b.kind").
		Scan(&breaks).Error; err != nil {
		return out, apperr.Wrap(fmt.Errorf("统计休息类型失败: %w", err))
	}
	for _, row := range breaks {
		switch row.Kind {
		case string(sit.BreakKindActive):
			out.ActiveBreaks = row.Cnt
		case string(sit.BreakKindLock):
			out.LockBreaks = row.Cnt
		case string(sit.BreakKindIdle):
			out.IdleBreaks = row.Cnt
		}
	}

	var credited int64
	if err := s.db.Model(&model.SitSession{}).
		Where("date_key = ? AND deleted_at IS NULL AND (credited_reminder_id > 0 OR credited_check_in_id > 0)", dateKey).
		Count(&credited).Error; err != nil {
		return out, apperr.Wrap(fmt.Errorf("统计抵扣次数失败: %w", err))
	}
	out.CreditedCount = int(credited)

	// 应用画像明细（关闭画像/无数据时为空）。
	nowUnix := s.now().Unix()
	var appRows []struct {
		Platform string
		AppID    string
		Duration int64
		Sessions int
	}
	if err := s.db.Model(&model.AppUsageSession{}).
		Select("platform, app_id, SUM(CASE WHEN ended_at > 0 THEN ended_at - started_at ELSE ? - started_at END) AS duration, COUNT(*) AS sessions", nowUnix).
		Where("date_key = ? AND deleted_at IS NULL", dateKey).
		Group("platform, app_id").
		Scan(&appRows).Error; err != nil {
		return out, apperr.Wrap(fmt.Errorf("统计应用使用失败: %w", err))
	}
	// 用户纠正优先于内置映射；纠正按 platform+app_id 派生，不改写原始会话行。
	overrides, err := s.loadCategoryOverrides()
	if err != nil {
		return out, err
	}

	categoryTotals := map[sit.Category]*CategoryUsage{}
	for _, row := range appRows {
		category := s.resolveCategory(overrides, row.Platform, row.AppID)
		minutes := int(row.Duration) / 60
		out.Apps = append(out.Apps, AppUsage{
			Platform:    row.Platform,
			AppID:       row.AppID,
			DisplayName: sit.AppDisplayName(row.AppID),
			Category:    category,
			Minutes:     minutes,
			Sessions:    row.Sessions,
		})
		total, ok := categoryTotals[category]
		if !ok {
			total = &CategoryUsage{Category: category}
			categoryTotals[category] = total
		}
		total.Minutes += minutes
		total.Sessions += row.Sessions
	}
	sort.SliceStable(out.Apps, func(i, j int) bool {
		if out.Apps[i].Minutes == out.Apps[j].Minutes {
			return out.Apps[i].DisplayName < out.Apps[j].DisplayName
		}
		return out.Apps[i].Minutes > out.Apps[j].Minutes
	})
	out.Categories = make([]CategoryUsage, 0, len(categoryTotals))
	for _, total := range categoryTotals {
		out.Categories = append(out.Categories, *total)
	}
	sort.SliceStable(out.Categories, func(i, j int) bool {
		return out.Categories[i].Minutes > out.Categories[j].Minutes
	})
	return out, nil
}

// RecoverOpenAppUsageSessions 收尾上次进程崩溃/强退时遗留的开放应用会话。
//
// 结束时间取最后心跳（没有心跳就取开始时间），避免把崩溃后的整段时间
// 算进前台使用；end_reason 记为 abandoned。
func (s *Service) RecoverOpenAppUsageSessions() (int64, error) {
	res := s.db.Model(&model.AppUsageSession{}).
		Where("ended_at = 0").
		Updates(map[string]any{
			"ended_at":   gorm.Expr("CASE WHEN last_seen_at > started_at THEN last_seen_at ELSE started_at END"),
			"end_reason": string(sit.EndReasonAbandoned),
		})
	if res.Error != nil {
		return 0, apperr.Wrap(fmt.Errorf("收尾遗留应用会话失败: %w", res.Error))
	}
	return res.RowsAffected, nil
}

// RecoverOpenSitSessions 收尾上次进程崩溃/强退时遗留的开放久坐会话。
//
// active_seconds 保留最后持久化的心跳值，只把结束时间钉到最后心跳，
// 避免把停机时间算进今日久坐。
func (s *Service) RecoverOpenSitSessions() (int64, error) {
	res := s.db.Model(&model.SitSession{}).
		Where("ended_at = 0").
		Updates(map[string]any{
			"ended_at":   gorm.Expr("CASE WHEN last_seen_at > started_at THEN last_seen_at ELSE started_at END"),
			"end_reason": string(sit.EndReasonAbandoned),
		})
	if res.Error != nil {
		return 0, apperr.Wrap(fmt.Errorf("收尾遗留久坐会话失败: %w", res.Error))
	}
	return res.RowsAffected, nil
}

// appCategoryKey 是纠正表的进程内查找键；\x00 不会出现在 bundle ID / 进程名里。
func appCategoryKey(platform, appID string) string {
	return platform + "\x00" + appID
}

// loadCategoryOverrides 读取全部用户纠正；表很小，一次加载供批量聚合使用。
func (s *Service) loadCategoryOverrides() (map[string]sit.Category, error) {
	overrides := map[string]sit.Category{}
	var rows []model.AppCategoryOverride
	if err := s.db.Select("platform", "app_id", "category").Find(&rows).Error; err != nil {
		return nil, apperr.Wrap(fmt.Errorf("读取应用类别纠正失败: %w", err))
	}
	for _, row := range rows {
		category := sit.Category(row.Category)
		if sit.ValidCategory(category) {
			overrides[appCategoryKey(row.Platform, row.AppID)] = category
		}
	}
	return overrides, nil
}

// resolveCategory 按「用户纠正 > 内置映射」派生类别；API 聚合与专注运行时共用。
func (s *Service) resolveCategory(overrides map[string]sit.Category, platform, appID string) sit.Category {
	if category, ok := overrides[appCategoryKey(platform, appID)]; ok {
		return category
	}
	isMeeting := false
	switch platform {
	case "darwin":
		isMeeting = pause.IsMeetingBundleID(appID)
	case "windows":
		isMeeting = pause.IsMeetingProcessName(appID)
	}
	return sit.CategoryFor(platform, appID, isMeeting)
}

// AppCategory 返回单个应用的派生类别（含用户纠正），供专注会话显示使用。
func (s *Service) AppCategory(platform, appID string) (sit.Category, error) {
	overrides, err := s.loadCategoryOverrides()
	if err != nil {
		return sit.CategoryOther, err
	}
	return s.resolveCategory(overrides, platform, appID), nil
}

// SetAppCategory 写入或更新用户对某个应用的行为类别纠正。
//
// 只存纠正表，不改写 app_usage_sessions；类别在读取时派生，历史与后续视图
// 共用同一套纠正。未知应用同样可以纠正。
func (s *Service) SetAppCategory(platform, appID, category string) error {
	platform = strings.TrimSpace(platform)
	appID = strings.TrimSpace(appID)
	if platform == "" || appID == "" {
		return apperr.Validation("应用标识不能为空")
	}
	value := sit.Category(strings.TrimSpace(category))
	if !sit.ValidCategory(value) {
		return apperr.Validation("未知的行为类别: " + string(value))
	}
	row := &model.AppCategoryOverride{
		Platform: platform,
		AppID:    appID,
		Category: string(value),
	}
	if err := s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "platform"}, {Name: "app_id"}},
		DoUpdates: clause.Assignments(map[string]any{"category": string(value), "updated_at": s.now().Unix()}),
	}).Create(row).Error; err != nil {
		return apperr.Wrap(fmt.Errorf("保存应用类别纠正失败: %w", err))
	}
	return nil
}

// StartAppUsageSession 创建一段前台应用会话。
func (s *Service) StartAppUsageSession(platform, appID string, at time.Time) (*model.AppUsageSession, error) {
	row := &model.AppUsageSession{
		Platform:   platform,
		AppID:      appID,
		StartedAt:  at.Unix(),
		LastSeenAt: at.Unix(),
		DateKey:    at.Format(model.DateLayout),
	}
	if err := s.db.Create(row).Error; err != nil {
		return nil, apperr.Wrap(fmt.Errorf("创建应用会话失败: %w", err))
	}
	return row, nil
}

// TouchAppUsageSession 更新开放应用会话的心跳。
func (s *Service) TouchAppUsageSession(id uint, at time.Time) error {
	if id == 0 {
		return nil
	}
	if err := s.db.Model(&model.AppUsageSession{}).
		Where("id = ? AND ended_at = 0", id).
		Update("last_seen_at", at.Unix()).Error; err != nil {
		return apperr.Wrap(fmt.Errorf("更新应用会话失败: %w", err))
	}
	return nil
}

// EndAppUsageSession 收尾一段前台应用会话。
func (s *Service) EndAppUsageSession(id uint, endReason string, at time.Time) error {
	if id == 0 {
		return nil
	}
	if err := s.db.Model(&model.AppUsageSession{}).
		Where("id = ? AND ended_at = 0", id).
		Updates(map[string]any{
			"ended_at":     at.Unix(),
			"last_seen_at": at.Unix(),
			"end_reason":   endReason,
		}).Error; err != nil {
		return apperr.Wrap(fmt.Errorf("收尾应用会话失败: %w", err))
	}
	return nil
}

// ClearAppProfile 删除应用会话、类别纠正与记忆类发言；保留软提醒/欢迎与 CheckIn。
func (s *Service) ClearAppProfile() error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Unscoped().Where("1 = 1").Delete(&model.AppUsageSession{}).Error; err != nil {
			return apperr.Wrap(fmt.Errorf("清空应用会话失败: %w", err))
		}
		if err := tx.Unscoped().Where("1 = 1").Delete(&model.AppCategoryOverride{}).Error; err != nil {
			return apperr.Wrap(fmt.Errorf("清空应用类别纠正失败: %w", err))
		}
		if err := tx.Unscoped().Where("kind = ? OR memory_id <> ''", string(sit.BubbleMemory)).Delete(&model.SitSpeech{}).Error; err != nil {
			return apperr.Wrap(fmt.Errorf("清空记忆发言失败: %w", err))
		}
		return nil
	})
}

// GetSitTrend 返回最近 days 天（含今天）的日聚合，按日期升序。
func (s *Service) GetSitTrend(days int) ([]SitDay, error) {
	if days <= 0 {
		days = 7
	}
	if days > 30 {
		days = 30
	}
	now := s.now()
	out := make([]SitDay, 0, days)
	for i := days - 1; i >= 0; i-- {
		dateKey := now.AddDate(0, 0, -i).Format(model.DateLayout)
		day, err := s.GetSitDay(dateKey)
		if err != nil {
			return nil, err
		}
		out = append(out, day)
	}
	return out, nil
}

// ClearSitProfile 清空应用画像与久坐数据；CheckIn 与连续天数保留。
//
// 覆盖应用会话、类别纠正、久坐会话/休息/宠物发言（含派生记忆）；
// 不追溯改写历史打卡与完成率。重复调用安全。
func (s *Service) ClearSitProfile() error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		for _, table := range []any{
			&model.AppUsageSession{},
			&model.AppCategoryOverride{},
			&model.SitSpeech{},
			&model.SitBreak{},
			&model.SitSession{},
		} {
			if err := tx.Unscoped().Where("1 = 1").Delete(table).Error; err != nil {
				return apperr.Wrap(fmt.Errorf("清空久坐/应用数据失败: %w", err))
			}
		}
		return nil
	})
}
