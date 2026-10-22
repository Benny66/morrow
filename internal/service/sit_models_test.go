package service

import (
	"testing"

	"morrow/internal/model"
)

// TestSitModelsMigrate 保证新表随 AllModels() 一起建出来；漏登记会让后续
// tracker/service 在运行期才报 no such table。
func TestSitModelsMigrate(t *testing.T) {
	svc := newReminderTestService(t)
	tables := []string{
		"app_usage_sessions",
		"sit_sessions",
		"sit_breaks",
		"sit_speeches",
		"app_category_overrides",
	}
	for _, table := range tables {
		if !svc.db.Migrator().HasTable(table) {
			t.Errorf("表 %s 未创建——模型可能未登记进 AllModels()", table)
		}
	}
	if !svc.db.Migrator().HasColumn(&model.CheckIn{}, "source") {
		t.Error("check_ins.source 列未创建")
	}
}

// TestAppCategoryOverrideUnique 同一平台+应用只允许一条纠正。
func TestAppCategoryOverrideUnique(t *testing.T) {
	svc := newReminderTestService(t)
	first := model.AppCategoryOverride{Platform: "darwin", AppID: "com.example.app", Category: "design"}
	if err := svc.db.Create(&first).Error; err != nil {
		t.Fatalf("写入第一条纠正失败: %v", err)
	}
	second := model.AppCategoryOverride{Platform: "darwin", AppID: "com.example.app", Category: "media"}
	if err := svc.db.Create(&second).Error; err == nil {
		t.Error("同一平台+应用写入第二条纠正应失败（唯一索引未生效）")
	}
}
