package service

import (
	"errors"
	"fmt"

	"gorm.io/gorm"

	"morrow/internal/model"
	"morrow/internal/reminder"
)

// 本文件实现 internal/reminder 的 Store 接口。
//
// 【为什么由 service 来实现】调度引擎不持有 *gorm.DB（它要能在 go test 里脱离
// 数据库推演），而"谁能碰数据库"这条纪律在 service 层收口。两边靠 Go 的隐式接口
// 对接：引擎声明需要哪几个方法，service 提供它们，两边互不 import。

// ActiveScenario 返回当前启用的场景；没有启用任何场景时返回 (nil, nil)。
//
// 【返回 nil 而不是错误】引擎每 20 秒问一次"现在该提醒谁"，"没有启用场景"是一个
// 完全正常的答案。把它变成错误会让日志每秒多一行 Error，真出问题时反而找不到。
func (s *Service) ActiveScenario() (*model.Scenario, error) {
	var scenario model.Scenario
	if err := s.db.Where("enabled = ?", true).Order("id DESC").First(&scenario).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("查询启用场景失败: %w", err)
	}
	return &scenario, nil
}

// ActiveReminders 返回某场景下启用中的提醒项（按展示顺序）。
func (s *Service) ActiveReminders(scenarioID uint) ([]model.Reminder, error) {
	list := make([]model.Reminder, 0, 4)
	if err := s.db.Where("scenario_id = ? AND enabled = ?", scenarioID, true).
		Order("sort_order ASC, id ASC").Find(&list).Error; err != nil {
		return nil, fmt.Errorf("查询启用提醒失败: %w", err)
	}
	return list, nil
}

// ClaimFire 尝试占用 (reminderID, fireAt) 这个计划触发点，初始状态为 fired。
func (s *Service) ClaimFire(reminderID uint, fireAt int64) (uint, bool, error) {
	return s.ClaimFireWithStatus(reminderID, fireAt, model.FireFired)
}

// ClaimFireWithStatus 同 ClaimFire，但允许指定初始状态。
//
// 实现是「直接 INSERT，靠唯一索引判重」，而不是「先查再插」：
// 先查再插在两次 tick 之间、两个进程之间都存在窗口，唯一索引才是原子判据。
//
// 【为什么要让调用方指定初始状态】一次性提醒的「已错过」必须从第一笔写入起就是
// missed；若先插 fired 再更新，进程在两步之间崩溃会把一条从未送达的提醒显示成
// 待处理卡片。周期网格仍用 fired 初始状态——插入后即使崩溃，卡片可见至少不是丢失。
//
// 【FiredAt 显式写 0】它表示「实际送达时间」，而"占用"只是"这个点归我处理"。
func (s *Service) ClaimFireWithStatus(reminderID uint, fireAt int64, status string) (uint, bool, error) {
	fire := &model.ReminderFire{
		ReminderID: reminderID,
		FireAt:     fireAt,
		Status:     status,
		FiredAt:    0,
	}
	if err := s.db.Create(fire).Error; err != nil {
		if isUniqueViolation(err) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("写入触发记录失败: %w", err)
	}
	return fire.ID, true, nil
}

// DueOneOffs 返回到点且尚未产生任何触发记录的一次性提醒。
//
// 不依赖启用场景：一次性事件是全局的。fire 记录是「已经处理过」的标记，
// 因此这里排除任何已有 fire 的提醒；错过项由引擎写入 missed fire 后自然退出队列。
func (s *Service) DueOneOffs(now int64) ([]model.Reminder, error) {
	list := make([]model.Reminder, 0, 4)
	if err := s.db.Where("once_at > 0 AND enabled = ? AND once_at <= ? AND deleted_at IS NULL", true, now).
		Where("NOT EXISTS (SELECT 1 FROM reminder_fires f WHERE f.reminder_id = reminders.id AND f.deleted_at IS NULL)").
		Order("once_at ASC").Find(&list).Error; err != nil {
		return nil, fmt.Errorf("查询到点一次性提醒失败: %w", err)
	}
	return list, nil
}

// SettleFire 把触发记录落成终态。firedAt 为 0 表示保持原值。
//
// 这是 Store 接口的实现（引擎在事务外调用它），内部转调 settleFire：
// 「一条触发记录怎么落终态」只能有一份实现——act() 需要在事务内做同一件事，
// 若各写一遍，两处的 fired_at 处理早晚会漂移。
func (s *Service) SettleFire(fireID uint, status string, firedAt int64) error {
	return s.settleFire(s.db, fireID, status, firedAt)
}

// settleFire 是落终态的唯一实现，接受任意 *gorm.DB（可以是事务句柄）。
func (s *Service) settleFire(tx *gorm.DB, fireID uint, status string, firedAt int64) error {
	updates := map[string]any{"status": status}
	if firedAt > 0 {
		updates["fired_at"] = firedAt
	}
	if err := tx.Model(&model.ReminderFire{}).Where("id = ?", fireID).
		Updates(updates).Error; err != nil {
		return fmt.Errorf("更新触发记录失败: %w", err)
	}
	return nil
}

// dueFireRow 是推迟补发查询的中间结构（显式 column，避免依赖字段名推导）。
type dueFireRow struct {
	// FireID 触发记录 ID。
	FireID uint `gorm:"column:fire_id"`
	// ReminderID 提醒项 ID。
	ReminderID uint `gorm:"column:reminder_id"`
	// ScenarioID 场景 ID。
	ScenarioID uint `gorm:"column:scenario_id"`
	// ScenarioName 场景名。
	ScenarioName string `gorm:"column:scenario_name"`
	// Title 提醒标题。
	Title string `gorm:"column:title"`
	// Content 提醒正文。
	Content string `gorm:"column:content"`
	// Kind 提醒类型。
	Kind string `gorm:"column:kind"`
	// IsOneOff 是否为一次性提醒。
	IsOneOff bool `gorm:"column:is_one_off"`
	// FireAt 计划触发时间。
	FireAt int64 `gorm:"column:fire_at"`
}

// DueSnoozeFires 返回到点待补发的推迟记录（status = snooze 且 fire_at <= now）。
//
// join 上 reminders/scenarios 并带上 deleted_at IS NULL：用户删掉提醒或场景之后，
// 它们名下残留的推迟记录必须立刻失效，否则会出现「点开一条已不存在的提醒」。
// 一次性提醒没有场景，故 scenarios 用 LEFT JOIN，场景名取空串。
func (s *Service) DueSnoozeFires(now int64) ([]reminder.DueFire, error) {
	rows := make([]dueFireRow, 0, 2)
	if err := s.db.Table("reminder_fires AS f").
		Select(`f.id AS fire_id, f.reminder_id AS reminder_id, f.fire_at AS fire_at,
			COALESCE(r.scenario_id, 0) AS scenario_id, COALESCE(sc.name, '') AS scenario_name,
			r.title AS title, r.content AS content, r.kind AS kind,
			CASE WHEN r.once_at > 0 THEN 1 ELSE 0 END AS is_one_off`).
		Joins("JOIN reminders AS r ON r.id = f.reminder_id AND r.deleted_at IS NULL").
		Joins("LEFT JOIN scenarios AS sc ON sc.id = r.scenario_id AND sc.deleted_at IS NULL").
		Where("f.status = ? AND f.fire_at <= ? AND f.deleted_at IS NULL", model.FireSnooze, now).
		Order("f.fire_at ASC").
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("查询待补发提醒失败: %w", err)
	}
	list := make([]reminder.DueFire, 0, len(rows))
	for _, row := range rows {
		list = append(list, reminder.DueFire{
			FireID:       row.FireID,
			ReminderID:   row.ReminderID,
			ScenarioID:   row.ScenarioID,
			ScenarioName: row.ScenarioName,
			Title:        row.Title,
			Content:      row.Content,
			Kind:         row.Kind,
			IsOneOff:     row.IsOneOff,
			FireAt:       row.FireAt,
		})
	}
	return list, nil
}
