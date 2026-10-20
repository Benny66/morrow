// Package database 负责建立 gorm + SQLite 连接，并执行模型迁移。
//
// 设计要点：
//   - 使用纯 Go 驱动 github.com/glebarez/sqlite，跨平台交叉编译无 CGO 依赖。
//   - 模型清单只读 model.AllModels()，不在此处另写一份。
package database

import (
	"fmt"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"my-notice/internal/appdir"
	"my-notice/internal/model"
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

	if err := migrate(db); err != nil {
		return nil, err
	}

	return db, nil
}

// migrate 遍历 model.AllModels() 执行 AutoMigrate。
func migrate(db *gorm.DB) error {
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		return fmt.Errorf("数据库迁移失败: %w", err)
	}
	return nil
}
