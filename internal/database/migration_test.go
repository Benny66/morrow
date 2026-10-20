package database

import (
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// oldReminderSchema 是本次变更前的 reminders 表（只用于迁移测试）。
//
// 用原生 SQL 建表而不是旧结构体：测试要固定「磁盘上真实存在的旧列」，
// 结构体一旦跟着模型改，这个测试就会失去意义。
const oldReminderSchema = `
CREATE TABLE reminders (
	id integer PRIMARY KEY AUTOINCREMENT,
	created_at integer,
	updated_at integer,
	deleted_at datetime,
	scenario_id integer NOT NULL,
	title text NOT NULL,
	content text,
	kind text NOT NULL DEFAULT 'custom',
	interval_min integer NOT NULL DEFAULT 60,
	window_start text NOT NULL DEFAULT '09:00',
	window_end text NOT NULL DEFAULT '18:00',
	daily_target integer NOT NULL DEFAULT 0,
	enabled numeric NOT NULL DEFAULT 1,
	once numeric NOT NULL DEFAULT 0,
	sort_order integer NOT NULL DEFAULT 0
);`

// TestMigrateRemindersRebuildsOldSchema 守住旧库升级：once=true 变每日单点、
// once 列消失、scenario_id 变为可空、数据不丢、迁移可重复执行。
func TestMigrateRemindersRebuildsOldSchema(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "migration-test.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}

	if err := db.Exec(oldReminderSchema).Error; err != nil {
		t.Fatalf("建旧表失败: %v", err)
	}
	// 旧模型在 scenario_id / deleted_at 上带索引；表改名后索引名仍占用原名，
	// 新表创建时必须先把它们丢掉。这个 fixture 必须包含索引，否则测不出该冲突。
	for _, indexSQL := range []string{
		`CREATE INDEX idx_reminders_scenario_id ON reminders(scenario_id)`,
		`CREATE INDEX idx_reminders_deleted_at ON reminders(deleted_at)`,
	} {
		if err := db.Exec(indexSQL).Error; err != nil {
			t.Fatalf("建旧索引失败: %v", err)
		}
	}
	inserts := []string{
		`INSERT INTO reminders (scenario_id, title, content, kind, interval_min, window_start, window_end, daily_target, enabled, once, sort_order)
		 VALUES (1, '喝水', '慢慢喝完', 'water', 60, '09:00', '18:00', 8, 1, 1, 0)`,
		`INSERT INTO reminders (scenario_id, title, content, kind, interval_min, window_start, window_end, daily_target, enabled, once, sort_order)
		 VALUES (1, '起身', '活动五分钟', 'stand', 45, '09:00', '18:00', 0, 1, 0, 1)`,
	}
	for _, sql := range inserts {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatalf("写入旧数据失败: %v", err)
		}
	}

	if err := migrate(db); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	columns := map[string]int{}
	var infos []reminderColumn
	if err := db.Raw("PRAGMA table_info(reminders)").Scan(&infos).Error; err != nil {
		t.Fatalf("读取列信息失败: %v", err)
	}
	for _, info := range infos {
		columns[info.Name] = info.NotNull
	}
	if _, ok := columns["once"]; ok {
		t.Error("迁移后 once 列仍存在")
	}
	if _, ok := columns["once_at"]; !ok {
		t.Error("迁移后缺少 once_at 列")
	}
	if notNull, ok := columns["scenario_id"]; !ok || notNull != 0 {
		t.Errorf("scenario_id 应为可空，实际 notnull=%d (存在=%v)", notNull, ok)
	}

	type migratedRow struct {
		ID          uint  `gorm:"column:id"`
		ScenarioID  *uint `gorm:"column:scenario_id"`
		IntervalMin int   `gorm:"column:interval_min"`
		OnceAt      int64 `gorm:"column:once_at"`
		Title       string
	}
	rows := make([]migratedRow, 0, 2)
	if err := db.Raw("SELECT id, scenario_id, interval_min, once_at, title FROM reminders ORDER BY id").
		Scan(&rows).Error; err != nil {
		t.Fatalf("读取迁移结果失败: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("迁移后行数 = %d，期望 2", len(rows))
	}
	if rows[0].Title != "喝水" || rows[0].IntervalMin != 0 || rows[0].OnceAt != 0 {
		t.Errorf("once=true 行迁移不正确: %+v", rows[0])
	}
	if rows[1].Title != "起身" || rows[1].IntervalMin != 45 || rows[1].OnceAt != 0 {
		t.Errorf("once=false 行迁移不正确: %+v", rows[1])
	}
	if rows[0].ScenarioID == nil || *rows[0].ScenarioID != 1 {
		t.Errorf("scenario_id 丢失: %+v", rows[0].ScenarioID)
	}

	// 新表必须允许一次性提醒写入 NULL 场景。
	if err := db.Exec(`INSERT INTO reminders (scenario_id, title, once_at, interval_min, enabled)
		VALUES (NULL, '独立提醒', 1760000000, 0, 1)`).Error; err != nil {
		t.Fatalf("新表不允许 scenario_id=NULL: %v", err)
	}

	// 迁移必须幂等：再次启动不能重建或丢数据。
	if err := migrate(db); err != nil {
		t.Fatalf("第二次迁移失败: %v", err)
	}
	var count int64
	if err := db.Model(&struct{}{}).Table("reminders").Count(&count).Error; err != nil {
		t.Fatalf("统计行数失败: %v", err)
	}
	if count != 3 {
		t.Errorf("第二次迁移后行数 = %d，期望 3", count)
	}
}
