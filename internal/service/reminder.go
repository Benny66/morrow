package service

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"

	"morrow/internal/apperr"
	"morrow/internal/model"
	"morrow/internal/reminder"
)

// 提醒项字段的合法范围（单一真相：写入口与 AI 解析共同引用这套上界）。
const (
	// maxReminderTitleRunes 标题字符上限。
	maxReminderTitleRunes = 50
	// maxReminderContentRunes 正文字符上限。
	maxReminderContentRunes = 200
	// maxReminderIntervalMin 间隔上限（12 小时）。0 是合法的，语义为「每日单点」。
	maxReminderIntervalMin = 720
	// maxReminderDailyTarget 每日目标上限。
	maxReminderDailyTarget = 99
)

// ReminderInput 是提醒项的新增/编辑入参。
//
// 【为什么需要它而不是直接用 model.Reminder】写入口需要「ID 为 0 表示新增」这类
// 只属于入参的语义，也需要显式列出"用户可写的字段有哪些"。直接把持久化模型当入参，
// 会让「哪些字段能被前端改」退化成靠约定（模型里还有 BaseModel 的时间字段）。
type ReminderInput struct {
	// ID 为 0 表示新增，非 0 表示编辑。
	ID uint `json:"id"`
	// ScenarioID 所属场景。一次性提醒必须为 0（它们不属于任何场景）。
	ScenarioID uint `json:"scenario_id"`
	// Title 通知标题。
	Title string `json:"title"`
	// Content 通知正文。
	Content string `json:"content"`
	// Kind 类型，见 model.Kind* 枚举（非法值会被归一化为 custom）。
	Kind string `json:"kind"`
	// IntervalMin 间隔分钟数，0 表示每天在起点只提醒一次（每日单点）。
	IntervalMin int `json:"interval_min"`
	// WindowStart 生效时段起点（HH:MM）。
	WindowStart string `json:"window_start"`
	// WindowEnd 生效时段终点（HH:MM），必须晚于 WindowStart。
	WindowEnd string `json:"window_end"`
	// DailyTarget 每日目标次数，0 表示不设目标。
	DailyTarget int `json:"daily_target"`
	// Enabled 是否参与调度。
	Enabled bool `json:"enabled"`
	// OnceAt 一次性提醒的绝对触发时刻（Unix 秒）。0 表示周期项；
	// >0 时必须没有场景，且 IntervalMin / DailyTarget 会被强制为 0。
	OnceAt int64 `json:"once_at"`
	// SortOrder 展示顺序。
	SortOrder int `json:"sort_order"`
}

// OneOffItem 是首页「即将到来」区的一条一次性提醒。
type OneOffItem struct {
	// ID 提醒项 ID。
	ID uint `json:"id"`
	// Title 标题。
	Title string `json:"title"`
	// Content 正文。
	Content string `json:"content"`
	// Kind 类型。
	Kind string `json:"kind"`
	// OnceAt 绝对触发时刻（Unix 秒）。
	OnceAt int64 `json:"once_at"`
	// Missed 是否已经错过（有 missed 触发记录）。
	Missed bool `json:"missed"`
}

// ListReminders 返回某场景下的全部提醒项（含已停用的，按展示顺序）。
//
// 停用的也返回：计划页需要展示它们并允许重新启用——"暂时关掉"不该等于"删除"。
func (s *Service) ListReminders(scenarioID uint) ([]model.Reminder, error) {
	list := make([]model.Reminder, 0, 4)
	if err := s.db.Where("scenario_id = ?", scenarioID).
		Order("sort_order ASC, id ASC").Find(&list).Error; err != nil {
		return nil, apperr.Wrap(fmt.Errorf("查询提醒项失败: %w", err))
	}
	return list, nil
}

// ListOneOffs 返回未触发与已错过的一次性提醒，按触发时刻升序。
//
// 已触发（fired/done/skip/deferred）的项不再出现在「即将到来」：它们要么在待处理
// 卡片里，要么已经处理完。已错过项保留可见，避免无声消失。
func (s *Service) ListOneOffs() ([]OneOffItem, error) {
	rows := make([]OneOffItem, 0, 4)
	if err := s.db.Table("reminders AS r").
		Select(`r.id AS id, r.title AS title, r.content AS content, r.kind AS kind,
			r.once_at AS once_at, CASE WHEN f.status = ? THEN 1 ELSE 0 END AS missed`, model.FireMissed).
		Joins("LEFT JOIN reminder_fires AS f ON f.reminder_id = r.id AND f.fire_at = r.once_at AND f.deleted_at IS NULL").
		Where("r.once_at > 0 AND r.enabled = ? AND r.deleted_at IS NULL", true).
		Where("f.id IS NULL OR f.status = ?", model.FireMissed).
		Order("r.once_at ASC").
		Scan(&rows).Error; err != nil {
		return nil, apperr.Wrap(fmt.Errorf("查询一次性提醒失败: %w", err))
	}
	return rows, nil
}

// CreateReminder 新增一条提醒项（周期项或一次性项）。
func (s *Service) CreateReminder(input ReminderInput) (*model.Reminder, error) {
	items, err := s.CreateReminders([]ReminderInput{input})
	if err != nil {
		return nil, err
	}
	return &items[0], nil
}

// CreateReminders 原子创建一批提醒项：全部校验通过才写入，任一失败整批回滚。
//
// 这是快速添加「确认草稿」的写入口。逐条调用 CreateReminder 会让第三条失败时
// 前两条已经落库，用户看到报错却多了半套提醒；这里先全部归一化/校验，再在一个
// 事务里写入。单条路径也复用它，保证校验只有一份。
func (s *Service) CreateReminders(inputs []ReminderInput) ([]model.Reminder, error) {
	if len(inputs) == 0 {
		return nil, apperr.Validation("没有要创建的提醒")
	}
	drafts := make([]model.Reminder, len(inputs))
	for index, input := range inputs {
		draft, invalid := s.normalizeReminderInput(input)
		if invalid != nil {
			if len(inputs) > 1 {
				return nil, apperr.Validation(fmt.Sprintf("第 %d 条：%s", index+1, invalid.Message))
			}
			return nil, invalid
		}
		if draft.ScenarioID != nil {
			if _, err := s.GetScenario(*draft.ScenarioID); err != nil {
				return nil, err
			}
		}
		drafts[index] = draft
	}

	txErr := s.db.Transaction(func(tx *gorm.DB) error {
		for index := range drafts {
			if err := tx.Create(&drafts[index]).Error; err != nil {
				return fmt.Errorf("创建第 %d 条提醒失败: %w", index+1, err)
			}
		}
		return nil
	})
	if txErr != nil {
		return nil, apperr.Wrap(txErr)
	}
	return drafts, nil
}

// UpdateReminder 编辑一条提醒项。
//
// 只更新可写字段，且不允许改 ScenarioID，也不允许在周期项/一次性项之间切换
// （跨场景移动或类型切换会让触发记录与打卡记录的归属变得难以解释）。
func (s *Service) UpdateReminder(input ReminderInput) error {
	if input.ID == 0 {
		return apperr.Validation("缺少提醒项 ID")
	}
	var existing model.Reminder
	if err := s.db.First(&existing, input.ID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apperr.NotFound("提醒项不存在")
		}
		return apperr.Wrap(fmt.Errorf("查询提醒项失败: %w", err))
	}
	draft, invalid := s.normalizeReminderInput(input)
	if invalid != nil {
		return invalid
	}

	if existing.OnceAt > 0 {
		if draft.OnceAt == 0 {
			return apperr.Validation("不能把一次性提醒改成周期提醒")
		}
		// 已触发/已错过的一次性项只能删除后重建：改期会让已经产生的触发记录
		// 与新的 once_at 脱节，用户会看到「响过一次的提醒又回来了」。
		var fireCount int64
		if err := s.db.Model(&model.ReminderFire{}).
			Where("reminder_id = ?", existing.ID).Count(&fireCount).Error; err != nil {
			return apperr.Wrap(fmt.Errorf("查询触发记录失败: %w", err))
		}
		if fireCount > 0 {
			return apperr.Validation("已触发或已错过的一次性提醒不能编辑，请删除后重新创建")
		}
		updates := map[string]any{
			"title":        draft.Title,
			"content":      draft.Content,
			"kind":         draft.Kind,
			"interval_min": 0,
			"window_start": "",
			"window_end":   "",
			"daily_target": 0,
			"enabled":      true,
			"once_at":      draft.OnceAt,
			"sort_order":   0,
		}
		if err := s.db.Model(&model.Reminder{}).Where("id = ?", input.ID).Updates(updates).Error; err != nil {
			return apperr.Wrap(fmt.Errorf("更新一次性提醒失败: %w", err))
		}
		return nil
	}

	if draft.OnceAt > 0 {
		return apperr.Validation("不能把周期提醒改成一次性提醒")
	}
	updates := map[string]any{
		"title":        draft.Title,
		"content":      draft.Content,
		"kind":         draft.Kind,
		"interval_min": draft.IntervalMin,
		"window_start": draft.WindowStart,
		"window_end":   draft.WindowEnd,
		"daily_target": draft.DailyTarget,
		"enabled":      draft.Enabled,
		"sort_order":   draft.SortOrder,
	}
	if err := s.db.Model(&model.Reminder{}).Where("id = ?", input.ID).Updates(updates).Error; err != nil {
		return apperr.Wrap(fmt.Errorf("更新提醒项失败: %w", err))
	}
	return nil
}

// SetReminderEnabled 启用/停用单条周期提醒。
//
// 一次性提醒没有启停语义：停用会让用户明确指定的事件静默消失，只能编辑或删除。
func (s *Service) SetReminderEnabled(id uint, enabled bool) error {
	var item model.Reminder
	if err := s.db.First(&item, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apperr.NotFound("提醒项不存在")
		}
		return apperr.Wrap(fmt.Errorf("查询提醒项失败: %w", err))
	}
	if item.OnceAt > 0 {
		return apperr.Validation("一次性提醒不支持启用/停用，请编辑或删除")
	}
	result := s.db.Model(&model.Reminder{}).Where("id = ?", id).Update("enabled", enabled)
	if result.Error != nil {
		return apperr.Wrap(fmt.Errorf("更新提醒状态失败: %w", result.Error))
	}
	if result.RowsAffected == 0 {
		return apperr.NotFound("提醒项不存在")
	}
	return nil
}

// DeleteReminder 删除一条提醒项（软删），并清掉它名下未处理的触发记录。
//
// 触发记录必须一起清：否则已经"响过"的那条提醒会以 fired 状态留在待处理列表里，
// 点「完成」时找不到提醒项而报错。
func (s *Service) DeleteReminder(id uint) error {
	txErr := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Unscoped().Where("reminder_id = ?", id).
			Delete(&model.ReminderFire{}).Error; err != nil {
			return fmt.Errorf("删除触发记录失败: %w", err)
		}
		if err := tx.Delete(&model.Reminder{}, id).Error; err != nil {
			return fmt.Errorf("删除提醒项失败: %w", err)
		}
		return nil
	})
	if txErr != nil {
		return apperr.Wrap(txErr)
	}
	return nil
}

// normalizeReminderInput 归一化并校验提醒项入参。
func (s *Service) normalizeReminderInput(input ReminderInput) (model.Reminder, *apperr.Error) {
	title, content, invalid := normalizeReminderText(input)
	if invalid != nil {
		return model.Reminder{}, invalid
	}
	if input.OnceAt < 0 {
		return model.Reminder{}, apperr.Validation("一次性提醒的时间非法")
	}
	if input.OnceAt > 0 {
		return s.normalizeOneOff(input, title, content)
	}

	// 时段与间隔：interval=0 是每日单点，仍需要 end > start 才能被调度层算出点。
	windowStart, windowEnd := input.WindowStart, input.WindowEnd
	intervalMin := input.IntervalMin
	if !reminder.ValidateWindow(windowStart, windowEnd) {
		return model.Reminder{}, apperr.Validation("生效时段非法：需为 HH:MM，且结束时间晚于开始时间")
	}
	if intervalMin < 0 || intervalMin > maxReminderIntervalMin {
		return model.Reminder{}, apperr.Validation(
			fmt.Sprintf("间隔需在 0–%d 分钟之间（0 表示每天同一时刻只提醒一次）", maxReminderIntervalMin))
	}
	if input.DailyTarget < 0 || input.DailyTarget > maxReminderDailyTarget {
		return model.Reminder{}, apperr.Validation(
			fmt.Sprintf("每日目标需在 0–%d 之间（0 表示不设目标）", maxReminderDailyTarget))
	}

	// 校验已经保证能解析成功，故这里忽略 ParseClock 的 ok。
	start, _ := reminder.ParseClock(windowStart)
	end, _ := reminder.ParseClock(windowEnd)

	scenarioID := input.ScenarioID
	return model.Reminder{
		ScenarioID:  &scenarioID,
		Title:       title,
		Content:     content,
		Kind:        model.NormalizeKind(input.Kind),
		IntervalMin: intervalMin,
		WindowStart: reminder.FormatClock(start),
		WindowEnd:   reminder.FormatClock(end),
		DailyTarget: input.DailyTarget,
		Enabled:     input.Enabled,
		SortOrder:   input.SortOrder,
	}, nil
}

// normalizeOneOff 归一化一次性提醒：无场景、绝对时刻、间隔/目标恒为 0。
func (s *Service) normalizeOneOff(input ReminderInput, title, content string) (model.Reminder, *apperr.Error) {
	if input.ScenarioID != 0 {
		return model.Reminder{}, apperr.Validation("一次性提醒不归属场景")
	}
	if input.OnceAt <= s.now().Unix() {
		return model.Reminder{}, apperr.Validation("提醒时间已过去，请选择将来的时间")
	}
	return model.Reminder{
		ScenarioID:  nil,
		Title:       title,
		Content:     content,
		Kind:        model.NormalizeKind(input.Kind),
		IntervalMin: 0,
		OnceAt:      input.OnceAt,
		WindowStart: "",
		WindowEnd:   "",
		DailyTarget: 0,
		Enabled:     true,
		SortOrder:   0,
	}, nil
}

// normalizeReminderText 校验并清洗标题与正文（两类提醒共用）。
func normalizeReminderText(input ReminderInput) (string, string, *apperr.Error) {
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return "", "", apperr.Validation("提醒标题不能为空")
	}
	if utf8.RuneCountInString(title) > maxReminderTitleRunes {
		return "", "", apperr.Validation(
			fmt.Sprintf("提醒标题不能超过 %d 个字", maxReminderTitleRunes))
	}
	content := strings.TrimSpace(input.Content)
	if utf8.RuneCountInString(content) > maxReminderContentRunes {
		return "", "", apperr.Validation(
			fmt.Sprintf("提醒正文不能超过 %d 个字", maxReminderContentRunes))
	}
	return title, content, nil
}
