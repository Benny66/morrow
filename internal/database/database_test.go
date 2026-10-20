package database

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// verifyModel 仅用于验证迁移框架可用，非业务模型。
// 证明：纯 Go 驱动(glebarez) + gorm AutoMigrate 能正确建表。
type verifyModel struct {
	gorm.Model
	Name string `gorm:"size:50" json:"name"`
}

func TestAutoMigrateCreatesTable(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开内存库失败: %v", err)
	}

	if err := db.AutoMigrate(&verifyModel{}); err != nil {
		t.Fatalf("AutoMigrate 失败: %v", err)
	}

	// 断言表确实被创建（用 sqlite_master 查询）
	var count int64
	if err := db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='verify_models'").Scan(&count).Error; err != nil {
		t.Fatalf("查询 sqlite_master 失败: %v", err)
	}
	if count != 1 {
		t.Fatalf("期望 verify_models 表存在，实际 count=%d", count)
	}
}
