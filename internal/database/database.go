// Package database 负责建立 gorm + SQLite 连接，并执行模型迁移。
//
// 设计要点：
//   - 使用纯 Go 驱动 github.com/glebarez/sqlite，跨平台交叉编译无 CGO 依赖。
//   - 模型清单只读 model.AllModels()，不在此处另写一份。
package database

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"morrow/internal/appdir"
	"morrow/internal/model"
)

// Init 建立数据库连接并执行迁移，返回 *gorm.DB。
// 数据库文件位于应用数据目录下（见 internal/appdir），与配置、日志同目录。
func Init(appName string) (*gorm.DB, error) {
	dbPath, err := appdir.File(appName, appName+".db")
	if err != nil {
		return nil, err
	}

	// Logger 必须显式接入：gorm 默认 logger 写 stdout，打包后的 GUI 应用无人可见，
	// 慢查询与 SQL 错误会静默消失（同类问题见 internal/logging/wails.go）。
	// 该接线由 internal/guard/wiring_test.go 强制。
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: slogLogger{},
	})
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}

	// 数据库会包含久坐/应用使用画像等敏感数据，POSIX 平台收紧到仅当前用户可读。
	// Windows 依赖用户目录 ACL，不做 POSIX chmod。
	if runtime.GOOS != "windows" {
		if chmodErr := os.Chmod(dbPath, 0o600); chmodErr != nil {
			return nil, fmt.Errorf("收紧数据库权限失败: %w", chmodErr)
		}
	}

	if err := migrate(db); err != nil {
		return nil, err
	}

	return db, nil
}

// migrate 先做无法由 AutoMigrate 安全表达的列变更，再交给 AutoMigrate 补表/索引。
//
// 【为什么顺序不能反】SQLite 不支持直接修改列的 NOT NULL 或删除列；GORM 的
// AutoMigrate 在旧 reminders 表上会尝试重建，但会漏掉必填列（实测 title），
// 直接失败。故先由 migrateReminders 用显式 SQL 重建，再让 AutoMigrate 收尾。
func migrate(db *gorm.DB) error {
	if err := migrateReminders(db); err != nil {
		return err
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		return fmt.Errorf("数据库迁移失败: %w", err)
	}
	return nil
}

// reminderColumn 是 PRAGMA table_info 的一行（只取迁移需要的字段）。
type reminderColumn struct {
	Name    string `gorm:"column:name"`
	NotNull int    `gorm:"column:notnull"`
}

// remindersMigrateTmp 是重建 reminders 表时的临时表名。
const remindersMigrateTmp = "reminders_migrate_tmp"

// migrateReminders 把旧 reminders 表重建为当前模型：
//   - once=1 → interval_min=0（每日单点，不再于完成后停用）；
//   - 删除 once 列；
//   - scenario_id 改为可空（一次性提醒无场景）；
//   - 新增 once_at（旧数据一律 0）。
//
// 只在检测到旧结构时执行，迁移幂等。整段包在事务里：复制失败时旧表原样恢复。
func migrateReminders(db *gorm.DB) error {
	if !db.Migrator().HasTable("reminders") {
		return nil
	}

	var cols []reminderColumn
	if err := db.Raw("PRAGMA table_info(reminders)").Scan(&cols).Error; err != nil {
		return fmt.Errorf("读取 reminders 表结构失败: %w", err)
	}
	if len(cols) == 0 {
		return fmt.Errorf("reminders 表存在但读不到列信息，迁移中止")
	}

	hasOnce := false
	hasOnceAt := false
	scenarioNullable := false
	for _, col := range cols {
		switch col.Name {
		case "once":
			hasOnce = true
		case "once_at":
			hasOnceAt = true
		case "scenario_id":
			scenarioNullable = col.NotNull == 0
		}
	}
	if !hasOnce && hasOnceAt && scenarioNullable {
		return nil
	}

	intervalExpr := "interval_min"
	if hasOnce {
		intervalExpr = "CASE WHEN once = 1 THEN 0 ELSE interval_min END"
	}
	onceAtExpr := "0"
	if hasOnceAt {
		onceAtExpr = "once_at"
	}

	txErr := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("DROP TABLE IF EXISTS " + remindersMigrateTmp).Error; err != nil {
			return fmt.Errorf("清理迁移临时表失败: %w", err)
		}
		if err := tx.Exec("ALTER TABLE reminders RENAME TO " + remindersMigrateTmp).Error; err != nil {
			return fmt.Errorf("重命名旧 reminders 表失败: %w", err)
		}
		// 索引跟着旧表改名后仍占用 idx_reminders_* 原名。不先删掉，
		// CreateTable 建新索引时会撞 "index already exists"（实测启动直接失败）。
		if err := dropReminderIndexes(tx); err != nil {
			return err
		}
		if err := tx.Migrator().CreateTable(&model.Reminder{}); err != nil {
			return fmt.Errorf("创建新 reminders 表失败: %w", err)
		}
		copySQL := fmt.Sprintf(`INSERT INTO reminders (
	id, created_at, updated_at, deleted_at, scenario_id, title, content, kind,
	interval_min, window_start, window_end, daily_target, enabled, once_at, sort_order)
SELECT id, created_at, updated_at, deleted_at, scenario_id, title, content, kind,
	%s, window_start, window_end, daily_target, enabled, %s, sort_order
FROM %s`, intervalExpr, onceAtExpr, remindersMigrateTmp)
		if err := tx.Exec(copySQL).Error; err != nil {
			return fmt.Errorf("复制 reminders 数据失败: %w", err)
		}
		if err := tx.Exec("DROP TABLE " + remindersMigrateTmp).Error; err != nil {
			return fmt.Errorf("删除旧 reminders 表失败: %w", err)
		}
		return nil
	})
	if txErr != nil {
		return fmt.Errorf("迁移 reminders 表失败: %w", txErr)
	}
	return nil
}

// reminderIndexInfo 是 PRAGMA index_list 的一行（只取迁移需要的字段）。
type reminderIndexInfo struct {
	Name   string `gorm:"column:name"`
	Origin string `gorm:"column:origin"`
}

// dropReminderIndexes 删掉旧表上由 CREATE INDEX 创建的索引。
//
// 表改名不会改索引名，这些名字会被新表复用；autoindex（origin != "c"）
// 是约束自动生成的，不能用 DROP INDEX 删除，跳过。
func dropReminderIndexes(tx *gorm.DB) error {
	var indexes []reminderIndexInfo
	if err := tx.Raw("PRAGMA index_list(" + remindersMigrateTmp + ")").Scan(&indexes).Error; err != nil {
		return fmt.Errorf("读取旧 reminders 索引失败: %w", err)
	}
	for _, index := range indexes {
		if index.Origin != "c" {
			continue
		}
		name := strings.ReplaceAll(index.Name, `"`, `""`)
		if err := tx.Exec(`DROP INDEX IF EXISTS "` + name + `"`).Error; err != nil {
			return fmt.Errorf("删除旧索引 %s 失败: %w", index.Name, err)
		}
	}
	return nil
}
