package service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"morrow/internal/apperr"
	"morrow/internal/model"
	"morrow/internal/reminder"
)

const (
	// streakLookbackDays 是连续达标天数的回溯窗口。
	// 60 天足够覆盖"连续打卡两个月"这类成就，又不会把一次查询变成全表扫描。
	streakLookbackDays = 60
	// pendingFireWindow 是待处理列表的时间窗口：只看最近 24 小时弹出的提醒。
	// 不做清理任务，靠窗口自然让陈旧卡片消失——用户不会对"昨天上午该喝水"
	// 有任何可操作性，留着只会让卡片区变成垃圾场。
	pendingFireWindow = 24 * time.Hour
)

// PendingFire 是首页「待处理」卡片的数据。
type PendingFire struct {
	// FireID 触发记录 ID，用户的动作针对它。
	FireID uint `json:"fire_id"`
	// ReminderID 提醒项 ID。
	ReminderID uint `json:"reminder_id"`
	// ScenarioID 场景 ID。
	ScenarioID uint `json:"scenario_id"`
	// ScenarioName 场景名（卡片上做归属提示）。
	ScenarioName string `json:"scenario_name"`
	// Title 提醒标题。
	Title string `json:"title"`
	// Content 提醒正文。
	Content string `json:"content"`
	// Kind 类型，决定图标与量词。
	Kind string `json:"kind"`
	// IsOneOff 是否为一次性提醒（无场景、只响这一回）。
	IsOneOff bool `json:"is_one_off"`
	// FireAt 计划触发时间（Unix 秒）。
	FireAt int64 `json:"fire_at"`
	// QuietDelivery 是否为自然停顿顺延到上限后的安静投递。
	// Alert.vue 据此不弹全屏；首页待处理区仍照常展示。
	QuietDelivery bool `json:"quiet_delivery"`
}

// ReminderProgress 是单条提醒的今日进度。
type ReminderProgress struct {
	// ReminderID 提醒项 ID。
	ReminderID uint `json:"reminder_id"`
	// Title 标题。
	Title string `json:"title"`
	// Kind 类型，决定进度条的量词（杯/次）。
	Kind string `json:"kind"`
	// DoneCount 今日完成次数。
	DoneCount int `json:"done_count"`
	// DailyTarget 今日目标次数，0 表示不设目标。
	DailyTarget int `json:"daily_target"`
	// Streak 连续达标天数（目标为 0 时恒为 0）。
	Streak int `json:"streak"`
	// NextFireAt 该提醒的下一个计划触发时间（Unix 秒，0 表示彻底算不出来）。
	NextFireAt int64 `json:"next_fire_at"`
}

// TodayStats 是仪表盘一屏所需的全部数据。
//
// 【为什么是一个聚合而不是五个绑定方法】仪表盘每次刷新都要这五项；拆开意味着
// 五次 IPC（每次都要一次序列化与一次错误处理），且前端还得自己拼"刷新是否完整"。
// 聚合成一个方法后，"刷新"是一次原子读取，页面状态不会出现半新半旧。
type TodayStats struct {
	// DateKey 统计所属的本地日期（YYYY-MM-DD）。
	DateKey string `json:"date_key"`
	// Scenario 当前启用的场景；未启用时为 null。
	Scenario *model.Scenario `json:"scenario"`
	// Reminders 该场景下每条启用提醒的今日进度。
	Reminders []ReminderProgress `json:"reminders"`
	// PendingCount 待处理提醒条数。
	PendingCount int `json:"pending_count"`
	// DoneTotal 今日完成总次数（用于顶部概览）。
	DoneTotal int `json:"done_total"`
	// TargetTotal 今日目标总次数（各条目标之和，用于总体进度条）。
	TargetTotal int `json:"target_total"`
	// NextFireAt 下一条提醒的时间（Unix 秒，0 表示暂无）。
	NextFireAt int64 `json:"next_fire_at"`
	// NextTitle 下一条提醒的标题（与 NextFireAt 配套展示）。
	NextTitle string `json:"next_title"`
}

// checkInDayCount 是「某天某提醒的完成次数」的查询结果。
type checkInDayCount struct {
	// ReminderID 提醒项 ID。
	ReminderID uint `gorm:"column:reminder_id"`
	// DateKey 日期（YYYY-MM-DD）。
	DateKey string `gorm:"column:date_key"`
	// Cnt 当日完成次数。
	Cnt int `gorm:"column:cnt"`
}

// pendingFireRow 是待处理清单的查询中间结构（显式 column，避免依赖字段名推导）。
type pendingFireRow struct {
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

// nextFireRow 是「下一条推进的提醒」的查询结果。
type nextFireRow struct {
	// FireAt 触发时间。
	FireAt int64 `gorm:"column:fire_at"`
	// Title 提醒标题。
	Title string `gorm:"column:title"`
}

// CompleteReminder 把一条提醒标成「完成」。
func (s *Service) CompleteReminder(fireID uint) error {
	return s.act(fireID, model.ActionDone, 0)
}

// SkipReminder 把一条提醒标成「跳过」。
//
// 跳过与完成都记一条 CheckIn，但只有完成计入进度：这样"我看见了但这条不需要"
// 与"我做到了"在统计上不会混为一谈。
func (s *Service) SkipReminder(fireID uint) error {
	return s.act(fireID, model.ActionSkip, 0)
}

// SnoozeReminder 把一条提醒推迟 snoozeMinutes 分钟（<=0 时取默认值）。
func (s *Service) SnoozeReminder(fireID uint, snoozeMinutes int) error {
	return s.act(fireID, model.ActionSnooze, snoozeMinutes)
}

// act 是三种打卡动作的唯一实现。
//
// 一次动作要同时落下两处事实：
//   - 触发记录的状态（决定它是否还出现在待处理列表里）；
//   - 打卡记录（决定统计与连续天数）——记录永不修改，只追加。
func (s *Service) act(fireID uint, action string, snoozeMinutes int) error {
	var fire model.ReminderFire
	if err := s.db.First(&fire, fireID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apperr.NotFound("该提醒已失效，可能已被处理")
		}
		return apperr.Wrap(fmt.Errorf("查询提醒记录失败: %w", err))
	}
	// 【只接受「待处理」（fired）的记录】没有这道守卫，对一条已经 done/skip 的记录
	// 再点一次「完成」会把它改回另一个终态并【再记一条 CheckIn】——今日完成次数虚高，
	// 而界面上完全看不出发生了什么。前端卡片消失有一瞬间延迟，重复点击是真实存在的。
	if fire.Status != model.FireFired {
		return apperr.Validation("该提醒已处理")
	}
	var item model.Reminder
	if err := s.db.First(&item, fire.ReminderID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apperr.NotFound("该提醒项已被删除")
		}
		return apperr.Wrap(fmt.Errorf("查询提醒项失败: %w", err))
	}

	now := s.now()

	// 三种动作的差别只有两处：原记录落成什么终态，以及要不要再落一条待补发的推迟记录。
	// 其余（校验、事务边界、打卡记录）完全一致，故先把差异算出来，再统一在【一个事务】里落库。
	finalStatus := ""
	nextFireAt := int64(0)
	switch action {
	case model.ActionDone:
		finalStatus = model.FireDone
	case model.ActionSkip:
		finalStatus = model.FireSkip
	case model.ActionSnooze:
		// 推迟要落【两种不同状态】，这是本函数最容易写错的一处：
		//   - 原记录 → FireDeferred（终态，本身不再触达）；
		//   - 新记录 → FireSnooze（待补发，fire_at 在未来）。
		// 若两者共用 snooze，DueSnoozeFires（status=snooze AND fire_at<=now）会在下一轮
		// tick 把 fire_at 还停在过去的原记录捞回来重弹——「推迟 5 分钟」变成「20 秒后再弹」。
		finalStatus = model.FireDeferred
		minutes := snoozeMinutes
		if minutes <= 0 {
			minutes = defaultSnoozeMinutes
		}
		if minutes > maxSnoozeMinutes {
			minutes = maxSnoozeMinutes
		}
		nextFireAt = now.Unix() + int64(minutes)*60
	default:
		return apperr.Validation("未知的打卡动作")
	}

	// 【为什么状态变更与打卡记录必须在同一个事务里】
	// 只改状态、打卡记录落不上时，用户界面上看到"已完成"，而今日统计少记一次——
	// 这正是铁律里说的「没有症状的失效」：不报错、不崩溃，只是数据慢慢不对。
	txErr := s.db.Transaction(func(tx *gorm.DB) error {
		if nextFireAt > 0 {
			// 1) 先清掉目标时刻上可能已存在的记录，避免撞 (reminder_id, fire_at)
			//    唯一索引。该时刻在未来，能落在上面的只可能是上一次推迟留下的记录
			//    （网格点只在到点时才 INSERT），先删掉即可稳定插入。
			if err := tx.Unscoped().
				Where("reminder_id = ? AND fire_at = ?", fire.ReminderID, nextFireAt).
				Delete(&model.ReminderFire{}).Error; err != nil {
				return fmt.Errorf("清理旧的推迟记录失败: %w", err)
			}
			// 2) 同一条提醒同时只允许一个待补发的推迟：把还没到期的旧推迟一并作废，
			//    否则用户在不同卡片上各点一次「推迟」，会得到两条都会响的记录。
			if err := tx.Model(&model.ReminderFire{}).
				Where("reminder_id = ? AND status = ? AND fire_at > ?",
					fire.ReminderID, model.FireSnooze, now.Unix()).
				Update("status", model.FireDeferred).Error; err != nil {
				return fmt.Errorf("作废旧的推迟记录失败: %w", err)
			}
		}

		// 3) 原记录落终态。与引擎共用 settleFire 这一份实现（不在这里另写一遍 Update）；
		//    firedAt 传 0 表示保持原值——「实际送达时间」不该被打卡动作改写。
		if err := s.settleFire(tx, fire.ID, finalStatus, 0); err != nil {
			return err
		}

		// 4) 落新的待补发记录（仅推迟动作）。
		if nextFireAt > 0 {
			nextFire := &model.ReminderFire{
				ReminderID: fire.ReminderID,
				FireAt:     nextFireAt,
				Status:     model.FireSnooze,
			}
			if err := tx.Create(nextFire).Error; err != nil {
				return fmt.Errorf("安排推迟提醒失败: %w", err)
			}
		}

		// 5) 打卡记录。周期项记录永不修改，只追加；一次性项不参与统计，不写。
		if item.OnceAt == 0 {
			scenarioID := uint(0)
			if item.ScenarioID != nil {
				scenarioID = *item.ScenarioID
			}
			record := &model.CheckIn{
				ReminderID:  fire.ReminderID,
				ScenarioID:  scenarioID,
				Action:      action,
				ScheduledAt: fire.FireAt,
				ActedAt:     now.Unix(),
				DateKey:     now.Format(model.DateLayout),
			}
			if err := tx.Create(record).Error; err != nil {
				return fmt.Errorf("记录打卡失败: %w", err)
			}
		}

		return nil
	})
	if txErr != nil {
		return apperr.Wrap(txErr)
	}
	return nil
}

// defaultSnoozeMinutes / maxSnoozeMinutes 是本层对推迟时长的兜底与上界。
//
// 与 config 的 Min/MaxSnoozeMinutes 同值但用途不同：config 校验的是"用户设置"，
// 这里是"调用方传入"（来自设置快照或通知按钮，可能已过期或被手改）。
const (
	// defaultSnoozeMinutes 调用方没给推迟时长时的兜底。
	defaultSnoozeMinutes = 5
	// maxSnoozeMinutes 推迟时长上界，避免一条提醒被推迟到明天。
	maxSnoozeMinutes = 120
)

// ListPendingFires 返回待处理提醒（status = fired 且未被处理）。
func (s *Service) ListPendingFires() ([]PendingFire, error) {
	since := s.now().Add(-pendingFireWindow).Unix()
	rows := make([]pendingFireRow, 0, 4)
	if err := s.db.Table("reminder_fires AS f").
		Select(`f.id AS fire_id, f.reminder_id AS reminder_id, f.fire_at AS fire_at,
			COALESCE(r.scenario_id, 0) AS scenario_id, COALESCE(sc.name, '') AS scenario_name,
			r.title AS title, r.content AS content, r.kind AS kind,
			CASE WHEN r.once_at > 0 THEN 1 ELSE 0 END AS is_one_off`).
		Joins("JOIN reminders AS r ON r.id = f.reminder_id AND r.deleted_at IS NULL").
		Joins("LEFT JOIN scenarios AS sc ON sc.id = r.scenario_id AND sc.deleted_at IS NULL").
		Where("f.status = ? AND f.fire_at >= ? AND f.deleted_at IS NULL", model.FireFired, since).
		Order("f.fire_at ASC").
		Scan(&rows).Error; err != nil {
		return nil, apperr.Wrap(fmt.Errorf("查询待处理提醒失败: %w", err))
	}
	list := make([]PendingFire, 0, len(rows))
	for _, row := range rows {
		list = append(list, PendingFire{
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
	// 安静投递标记只在内存；顺手清掉已经不在待处理里的旧 ID，避免无界增长。
	live := make(map[uint]bool, len(list))
	s.quietMu.Lock()
	for i := range list {
		list[i].QuietDelivery = s.quietFires[list[i].FireID]
		live[list[i].FireID] = true
	}
	for id := range s.quietFires {
		if !live[id] {
			delete(s.quietFires, id)
		}
	}
	s.quietMu.Unlock()
	return list, nil
}

// MissedFire 是首页「已错过」分组的一条数据（US-6）。
//
// 与 PendingFire 分开是刻意的：错过项【不能】被"完成 / 推迟 / 跳过"——它们没有
// FireID 意义上的可操作性；只保留「看到过」这一层回执。字段少一半，前端也不
// 需要给每一行渲染三个按钮。
type MissedFire struct {
	// FireID 记录本身的 ID（供去重与埋点，不参与交互）。
	FireID uint `json:"fire_id"`
	// ReminderID 提醒项 ID。
	ReminderID uint `json:"reminder_id"`
	// ScenarioID 场景 ID（一次性提醒为 0）。
	ScenarioID uint `json:"scenario_id"`
	// ScenarioName 场景名。
	ScenarioName string `json:"scenario_name"`
	// Title 提醒标题。
	Title string `json:"title"`
	// Kind 类型。
	Kind string `json:"kind"`
	// IsOneOff 是否为一次性提醒。
	IsOneOff bool `json:"is_one_off"`
	// FireAt 计划触发时间（Unix 秒）；UI 显示「09:30 错过了」。
	FireAt int64 `json:"fire_at"`
}

// ListMissedToday 返回今日已错过的提醒（暂停期跨过 / 补发窗口挤压导致的 missed 记录）。
//
// 【与 US-6 对应】「暂停期间错过的每日单点或一次性提醒按『已错过』保留可见，
// 不补弹」。引擎在暂停期把这两种情况都 claim 为 FireMissed；本查询把它们捞回来。
//
// 【为什么用 strftime 派生 date_key 而不是「算本地今天零点」】model.DateLayout 的
// 注释把这条铁律讲得很清楚：「用 Unix 时间戳做范围查询意味着每个统计点都要先算
// 『本地今天零点的时间戳』，而那个值在时区/夏令时变化时极易算错」。SQLite 的
// `strftime('%Y-%m-%d', fire_at, 'unixepoch', 'localtime')` 与 Go 侧
// `time.Unix(fire_at, 0).Format(DateLayout)` 语义一致（都跟随进程时区），
// 且不需要在 reminder_fires 表加 date_key 字段（该字段目前只 CheckIn 有）。
//
// 【为什么只看今天】"昨天上午 09:30 有一次舒展没做"对用户已经没有价值；只保留
// 今天让列表长度有界，避免"用户 3 天没开机 → 首页 300 条已错过"。
func (s *Service) ListMissedToday() ([]MissedFire, error) {
	todayKey := s.now().Format(model.DateLayout)
	rows := make([]pendingFireRow, 0, 8)
	if err := s.db.Table("reminder_fires AS f").
		Select(`f.id AS fire_id, f.reminder_id AS reminder_id, f.fire_at AS fire_at,
			COALESCE(r.scenario_id, 0) AS scenario_id, COALESCE(sc.name, '') AS scenario_name,
			r.title AS title, r.content AS content, r.kind AS kind,
			CASE WHEN r.once_at > 0 THEN 1 ELSE 0 END AS is_one_off`).
		Joins("JOIN reminders AS r ON r.id = f.reminder_id AND r.deleted_at IS NULL").
		Joins("LEFT JOIN scenarios AS sc ON sc.id = r.scenario_id AND sc.deleted_at IS NULL").
		Where("f.status = ? AND f.deleted_at IS NULL", model.FireMissed).
		Where("strftime('%Y-%m-%d', f.fire_at, 'unixepoch', 'localtime') = ?", todayKey).
		Order("f.fire_at ASC").
		Scan(&rows).Error; err != nil {
		return nil, apperr.Wrap(fmt.Errorf("查询已错过提醒失败: %w", err))
	}
	list := make([]MissedFire, 0, len(rows))
	for _, row := range rows {
		list = append(list, MissedFire{
			FireID:       row.FireID,
			ReminderID:   row.ReminderID,
			ScenarioID:   row.ScenarioID,
			ScenarioName: row.ScenarioName,
			Title:        row.Title,
			Kind:         row.Kind,
			IsOneOff:     row.IsOneOff,
			FireAt:       row.FireAt,
		})
	}
	return list, nil
}

// GetTodayStats 聚合仪表盘所需的今日数据。
func (s *Service) GetTodayStats() (*TodayStats, error) {
	now := s.now()
	today := now.Format(model.DateLayout)
	stats := &TodayStats{
		DateKey:   today,
		Reminders: []ReminderProgress{},
	}

	pendingCount, err := s.countPendingFires(now)
	if err != nil {
		return nil, err
	}
	stats.PendingCount = pendingCount

	// 一次性提醒不依赖场景：即使没有启用场景，首页也要显示下一条。
	oneOffAt, oneOffTitle, oneOffErr := s.nextOneOff(now.Unix())
	if oneOffErr != nil {
		return nil, apperr.Wrap(oneOffErr)
	}
	if oneOffAt > 0 {
		stats.NextFireAt = oneOffAt
		stats.NextTitle = oneOffTitle
	}

	scenario, err := s.ActiveScenario()
	if err != nil {
		return nil, apperr.Wrap(err)
	}
	stats.Scenario = scenario
	// 没有启用场景时仍返回待处理数据：已经弹出来的卡片必须能被处理完，
	// 否则用户停用场景后会被几张点不掉的卡片困住。
	if scenario == nil {
		return stats, nil
	}

	reminders, err := s.ListReminders(scenario.ID)
	if err != nil {
		return nil, err
	}

	countsByReminder, err := s.completedByReminder(now, today)
	if err != nil {
		return nil, err
	}

	for _, item := range reminders {
		if !item.Enabled {
			continue
		}
		counts := countsByReminder[item.ID]
		done := 0
		if counts != nil {
			done = counts[today]
		}
		progress := ReminderProgress{
			ReminderID:  item.ID,
			Title:       item.Title,
			Kind:        item.Kind,
			DoneCount:   done,
			DailyTarget: item.DailyTarget,
			Streak:      streakDays(counts, item.DailyTarget, now),
			NextFireAt:  reminder.NextFireAt(now, item.WindowStart, item.WindowEnd, item.IntervalMin),
		}
		stats.Reminders = append(stats.Reminders, progress)
		stats.DoneTotal += progress.DoneCount
		stats.TargetTotal += progress.DailyTarget
		if progress.NextFireAt > 0 && (stats.NextFireAt == 0 || progress.NextFireAt < stats.NextFireAt) {
			stats.NextFireAt = progress.NextFireAt
			stats.NextTitle = item.Title
		}
	}

	// 推迟中的提醒可能早于网格上的下一个点，倒计时应以更早的那个为准。
	snoozeAt, snoozeTitle, snoozeErr := s.nextSnoozeFire(now.Unix())
	if snoozeErr == nil && snoozeAt > 0 && (stats.NextFireAt == 0 || snoozeAt < stats.NextFireAt) {
		stats.NextFireAt = snoozeAt
		stats.NextTitle = snoozeTitle
	}
	return stats, nil
}

// countPendingFires 统计待处理提醒条数。
func (s *Service) countPendingFires(now time.Time) (int, error) {
	since := now.Add(-pendingFireWindow).Unix()
	var total int64
	if err := s.db.Model(&model.ReminderFire{}).
		Where("status = ? AND fire_at >= ?", model.FireFired, since).
		Count(&total).Error; err != nil {
		return 0, apperr.Wrap(fmt.Errorf("统计待处理提醒失败: %w", err))
	}
	return int(total), nil
}

// nextOneOff 返回最近一条尚未产生触发记录的一次性提醒。
//
// 已触发/已错过/已推迟的项都有 fire 记录，不再算「下一条」；推迟项的触达时间
// 由 nextSnoozeFire 单独提供。
func (s *Service) nextOneOff(nowUnix int64) (int64, string, error) {
	rows := make([]nextFireRow, 0, 1)
	if err := s.db.Table("reminders AS r").
		Select("r.once_at AS fire_at, r.title AS title").
		Where("r.once_at > 0 AND r.enabled = ? AND r.deleted_at IS NULL AND r.once_at > ?", true, nowUnix).
		Where("NOT EXISTS (SELECT 1 FROM reminder_fires f WHERE f.reminder_id = r.id AND f.deleted_at IS NULL)").
		Order("r.once_at ASC").
		Limit(1).
		Scan(&rows).Error; err != nil {
		return 0, "", fmt.Errorf("查询下一条一次性提醒失败: %w", err)
	}
	if len(rows) == 0 {
		return 0, "", nil
	}
	return rows[0].FireAt, rows[0].Title, nil
}

// nextSnoozeFire 返回最近一条待补发的推迟记录（时间与标题）。
func (s *Service) nextSnoozeFire(nowUnix int64) (int64, string, error) {
	rows := make([]nextFireRow, 0, 1)
	if err := s.db.Table("reminder_fires AS f").
		Select("f.fire_at AS fire_at, r.title AS title").
		Joins("JOIN reminders AS r ON r.id = f.reminder_id AND r.deleted_at IS NULL").
		Where("f.status = ? AND f.fire_at > ? AND f.deleted_at IS NULL", model.FireSnooze, nowUnix).
		Order("f.fire_at ASC").
		Limit(1).
		Scan(&rows).Error; err != nil {
		return 0, "", err
	}
	if len(rows) == 0 {
		return 0, "", nil
	}
	return rows[0].FireAt, rows[0].Title, nil
}

// completedByReminder 返回「提醒 ID → (日期 → 当日完成次数)」。
//
// 一次查询取回回溯窗口内的全部完成记录：逐条提醒各查一次会让仪表盘刷新变成
// N+1 次查询，而一次聚合扫描在这个数据量级（每条提醒每天个位数）几乎无成本。
func (s *Service) completedByReminder(now time.Time, today string) (map[uint]map[string]int, error) {
	since := now.AddDate(0, 0, -streakLookbackDays).Format(model.DateLayout)
	rows := make([]checkInDayCount, 0, 64)
	if err := s.db.Model(&model.CheckIn{}).
		Select("reminder_id, date_key, COUNT(*) AS cnt").
		Where("action = ? AND date_key >= ? AND date_key <= ?", model.ActionDone, since, today).
		Group("reminder_id, date_key").
		Scan(&rows).Error; err != nil {
		return nil, apperr.Wrap(fmt.Errorf("统计今日完成次数失败: %w", err))
	}
	out := make(map[uint]map[string]int, len(rows))
	for _, row := range rows {
		byDay, ok := out[row.ReminderID]
		if !ok {
			byDay = make(map[string]int, streakLookbackDays)
			out[row.ReminderID] = byDay
		}
		byDay[row.DateKey] = row.Cnt
	}
	return out, nil
}

// streakDays 计算连续达标天数。
//
// 语义（这三条决定用户体验，改之前想清楚）：
//   - 今天已达标 → 从今天往回数；
//   - 今天还没达标 → 从昨天往回数（今天还有一整天，不能算"断签"）；
//   - 目标为 0（未设目标）→ 无法判定达标，返回 0。
func streakDays(counts map[string]int, target int, now time.Time) int {
	if target <= 0 || counts == nil {
		return 0
	}
	day := now
	if counts[day.Format(model.DateLayout)] < target {
		day = day.AddDate(0, 0, -1)
	}
	streak := 0
	for i := 0; i < streakLookbackDays; i++ {
		if counts[day.Format(model.DateLayout)] < target {
			break
		}
		streak++
		day = day.AddDate(0, 0, -1)
	}
	return streak
}

// isUniqueViolation 判断错误是否为唯一约束冲突。
//
// 【为什么按文案判断而不是错误码】glebarez/sqlite 把 modernc 的 sqlite 错误包成了
// 普通 error，没有稳定类型可断言；驱动文末的 result code（如 2067）随版本变，
// 而 "UNIQUE constraint failed" 是 SQLite 自身的固定措辞。
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "unique constraint failed")
}
