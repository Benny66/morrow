package database

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"morrow/internal/appdir"
	"morrow/internal/model"
)

// TestInitAddsCheckInSourceToOldDB 旧库升级后，历史打卡必须自动得到 manual 来源。
func TestInitAddsCheckInSourceToOldDB(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp)

	path, err := appdir.File("migrationtest", "migrationtest.db")
	if err != nil {
		t.Fatalf("appdir.File 失败: %v", err)
	}

	raw, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开旧库失败: %v", err)
	}
	oldSchema := `CREATE TABLE check_ins (
		id integer PRIMARY KEY AUTOINCREMENT,
		created_at integer,
		updated_at integer,
		deleted_at datetime,
		reminder_id integer,
		scenario_id integer,
		action text,
		scheduled_at integer,
		acted_at integer,
		date_key text
	);
	INSERT INTO check_ins (reminder_id, scenario_id, action, scheduled_at, acted_at, date_key)
	VALUES (1, 1, 'done', 1, 1, '2026-10-22');`
	if err := raw.Exec(oldSchema).Error; err != nil {
		t.Fatalf("建旧表失败: %v", err)
	}
	sqlDB, err := raw.DB()
	if err != nil {
		t.Fatalf("获取旧库连接失败: %v", err)
	}
	_ = sqlDB.Close()

	db, err := Init("migrationtest")
	if err != nil {
		t.Fatalf("Init 升级旧库失败: %v", err)
	}
	sqlDB2, err := db.DB()
	if err != nil {
		t.Fatalf("获取新库连接失败: %v", err)
	}
	defer sqlDB2.Close()

	var got model.CheckIn
	if err := db.First(&got).Error; err != nil {
		t.Fatalf("查询旧打卡失败: %v", err)
	}
	if got.Source != model.SourceManual {
		t.Errorf("旧打卡 source = %q，期望默认 %q", got.Source, model.SourceManual)
	}
}
