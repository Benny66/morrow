package service

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"morrow/internal/appdir"
	"morrow/internal/apperr"
	"morrow/internal/model"
	"morrow/internal/sit"
)

// exportProbe 仅用于导出测试，非业务模型。
// 刻意不内嵌 model.BaseModel：本测试只关心「导出文件能不能被独立连接读出数据」。
type exportProbe struct {
	ID   uint `gorm:"primarykey"`
	Name string
}

// newExportTestService 建一个临时库（含少量数据）并返回服务实例。
func newExportTestService(t *testing.T) (*Service, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "src.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	if err := db.AutoMigrate(&exportProbe{}); err != nil {
		t.Fatalf("迁移测试模型失败: %v", err)
	}
	if err := db.Create(&exportProbe{Name: "第一条"}).Error; err != nil {
		t.Fatalf("写入测试数据失败: %v", err)
	}
	return New(db), dbPath
}

// isolateHome 把数据目录重定向到临时目录，避免污染真实配置目录。
func isolateHome(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp)
}

// TestExportDatabaseProducesUsableCopy 导出的文件 MUST 是一个能独立打开、
// 且数据完好的库——这是「备份」这个能力的全部意义。
func TestExportDatabaseProducesUsableCopy(t *testing.T) {
	isolateHome(t)
	svc, _ := newExportTestService(t)

	target := filepath.Join(t.TempDir(), "backup.db")
	if err := svc.ExportDatabase("exporttest", target, true); err != nil {
		t.Fatalf("ExportDatabase 失败: %v", err)
	}

	// 用【独立连接】打开导出文件，断言表与数据都在
	backup, err := gorm.Open(sqlite.Open(target), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开导出文件失败（导出产物不是可用库）: %v", err)
	}
	var rows []exportProbe
	if err := backup.Find(&rows).Error; err != nil {
		t.Fatalf("查询导出文件失败: %v", err)
	}
	if len(rows) != 1 || rows[0].Name != "第一条" {
		t.Errorf("导出的数据 = %+v，期望 1 条 Name=第一条", rows)
	}
}

// TestExportDatabaseSitProfileOption 验证导出勾选：包含时保留画像，取消时
// 副本没有画像/久坐表但 CheckIn 保留，且副本可重新迁移使用。
func TestExportDatabaseSitProfileOption(t *testing.T) {
	isolateHome(t)
	svc := newReminderTestService(t)
	now := time.Now()
	dateKey := now.Format(model.DateLayout)

	app, err := svc.StartAppUsageSession("darwin", "com.google.Chrome", now.Add(-10*time.Minute))
	if err != nil {
		t.Fatalf("StartAppUsageSession 失败: %v", err)
	}
	if err := svc.EndAppUsageSession(app.ID, string(sit.EndReasonAppSwitch), now.Add(-5*time.Minute)); err != nil {
		t.Fatalf("EndAppUsageSession 失败: %v", err)
	}
	if err := svc.SetAppCategory("darwin", "com.google.Chrome", string(sit.CategoryMedia)); err != nil {
		t.Fatalf("SetAppCategory 失败: %v", err)
	}
	session, err := svc.StartSitSession(now.Add(-30 * time.Minute))
	if err != nil {
		t.Fatalf("StartSitSession 失败: %v", err)
	}
	if _, err := svc.StartSitBreak(session.ID, now.Add(-20*time.Minute), string(sit.BreakKindActive), 60); err != nil {
		t.Fatalf("StartSitBreak 失败: %v", err)
	}
	if _, err := svc.CreateSitSpeech(SitSpeechInput{
		DateKey: dateKey, SessionID: session.ID, Kind: string(sit.BubbleNudge),
		TemplateID: "nudge.test", Text: "坐久了", Now: now, TTL: time.Minute,
	}); err != nil {
		t.Fatalf("CreateSitSpeech 失败: %v", err)
	}
	if err := svc.db.Create(&model.CheckIn{
		ReminderID: 1, ScenarioID: 1, Action: model.ActionDone, Source: model.SourceActiveBreak,
		ActedAt: now.Unix(), DateKey: dateKey,
	}).Error; err != nil {
		t.Fatalf("写入 CheckIn 失败: %v", err)
	}

	openBackup := func(path string) (*gorm.DB, func()) {
		t.Helper()
		db, openErr := gorm.Open(sqlite.Open(path), &gorm.Config{})
		if openErr != nil {
			t.Fatalf("打开导出文件失败: %v", openErr)
		}
		sqlDB, sqlErr := db.DB()
		if sqlErr != nil {
			t.Fatalf("获取导出连接失败: %v", sqlErr)
		}
		return db, func() { _ = sqlDB.Close() }
	}

	withProfile := filepath.Join(t.TempDir(), "with-profile.db")
	if err := svc.ExportDatabase("exporttest", withProfile, true); err != nil {
		t.Fatalf("包含画像导出失败: %v", err)
	}
	full, closeFull := openBackup(withProfile)
	for _, table := range []any{&model.AppUsageSession{}, &model.AppCategoryOverride{}, &model.SitSession{}, &model.SitBreak{}, &model.SitSpeech{}} {
		if !full.Migrator().HasTable(table) {
			t.Errorf("包含画像的副本缺少表 %T", table)
		}
	}
	var appCount int64
	if err := full.Model(&model.AppUsageSession{}).Count(&appCount).Error; err != nil {
		t.Fatalf("统计应用会话失败: %v", err)
	}
	if appCount != 1 {
		t.Errorf("包含画像的应用会话 = %d，期望 1", appCount)
	}
	closeFull()

	withoutProfile := filepath.Join(t.TempDir(), "without-profile.db")
	if err := svc.ExportDatabase("exporttest", withoutProfile, false); err != nil {
		t.Fatalf("取消画像导出失败: %v", err)
	}
	trimmed, closeTrimmed := openBackup(withoutProfile)
	for _, table := range []any{&model.AppUsageSession{}, &model.AppCategoryOverride{}, &model.SitSession{}, &model.SitBreak{}, &model.SitSpeech{}} {
		if trimmed.Migrator().HasTable(table) {
			t.Errorf("取消画像的副本不应包含表 %T", table)
		}
	}
	var checks int64
	if err := trimmed.Model(&model.CheckIn{}).Count(&checks).Error; err != nil {
		t.Fatalf("统计 CheckIn 失败: %v", err)
	}
	if checks != 1 {
		t.Errorf("取消画像后 CheckIn = %d，期望保留 1 条", checks)
	}
	// 恢复/迁移路径：重新建表后副本仍可用，打卡数据不丢。
	if err := trimmed.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatalf("导出副本重新迁移失败: %v", err)
	}
	for _, table := range []any{&model.AppUsageSession{}, &model.SitSession{}} {
		if !trimmed.Migrator().HasTable(table) {
			t.Errorf("重新迁移后缺少表 %T", table)
		}
	}
	if err := trimmed.Model(&model.CheckIn{}).Count(&checks).Error; err != nil {
		t.Fatalf("迁移后统计 CheckIn 失败: %v", err)
	}
	if checks != 1 {
		t.Errorf("迁移后 CheckIn = %d，期望 1", checks)
	}
	closeTrimmed()

	entries, readErr := os.ReadDir(filepath.Dir(withoutProfile))
	if readErr != nil {
		t.Fatalf("读取导出目录失败: %v", readErr)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(withoutProfile) {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Errorf("取消画像导出后目录内容 = %v，期望仅目标文件", names)
	}

	if runtime.GOOS != "windows" {
		info, statErr := os.Stat(withoutProfile)
		if statErr != nil {
			t.Fatalf("读取导出文件权限失败: %v", statErr)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("导出文件权限 = %o，期望 600", got)
		}
	}
}

// TestExportDatabaseOverwritesExisting 目标已存在时 MUST 覆盖成功——
// 用户在系统「保存」对话框里已经确认过覆盖，这里再报「文件已存在」是打自己的脸。
func TestExportDatabaseOverwritesExisting(t *testing.T) {
	isolateHome(t)
	svc, _ := newExportTestService(t)

	target := filepath.Join(t.TempDir(), "backup.db")
	if err := os.WriteFile(target, []byte("这是一个旧文件"), 0o644); err != nil {
		t.Fatalf("准备旧文件失败: %v", err)
	}
	if err := svc.ExportDatabase("exporttest", target, true); err != nil {
		t.Fatalf("覆盖式导出失败: %v", err)
	}

	backup, err := gorm.Open(sqlite.Open(target), &gorm.Config{})
	if err != nil {
		t.Fatalf("覆盖后的文件不是可用库: %v", err)
	}
	var n int64
	if err := backup.Model(&exportProbe{}).Count(&n).Error; err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if n != 1 {
		t.Errorf("覆盖后数据条数 = %d，期望 1", n)
	}
}

// TestExportDatabaseLeavesNoTempFile 成功路径 MUST 不残留中转文件。
func TestExportDatabaseLeavesNoTempFile(t *testing.T) {
	isolateHome(t)
	svc, _ := newExportTestService(t)

	dir := t.TempDir()
	target := filepath.Join(dir, "backup.db")
	if err := svc.ExportDatabase("exporttest", target, true); err != nil {
		t.Fatalf("ExportDatabase 失败: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读取导出目录失败: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "backup.db" {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("导出目录内容 = %v，期望仅 backup.db（中转文件未清理）", names)
	}
}

// TestExportDatabaseRejectsTargetInsideDataDir 导出到数据目录内 MUST 被拒——
// 否则等于把库往自己身上复制，且产物会与库/日志混在一处，清理时容易误伤。
func TestExportDatabaseRejectsTargetInsideDataDir(t *testing.T) {
	isolateHome(t)
	svc, _ := newExportTestService(t)

	// 触发数据目录创建，拿到真实路径
	dataDir, err := appdir.Dir("exporttest")
	if err != nil {
		t.Fatalf("获取数据目录失败: %v", err)
	}

	for _, target := range []string{
		filepath.Join(dataDir, "backup.db"),
		filepath.Join(dataDir, "sub", "backup.db"),
		dataDir,
	} {
		exportErr := svc.ExportDatabase("exporttest", target, true)
		if exportErr == nil {
			t.Errorf("导出到 %q 未被拒绝", target)
			continue
		}
		var ae *apperr.Error
		if !errors.As(exportErr, &ae) || ae.Code != apperr.CodeValidation {
			t.Errorf("导出到 %q 的错误码不是 validation: %v", target, exportErr)
		}
	}
}

// TestExportDatabaseRejectsEmptyTarget 空路径 MUST 被拒。
func TestExportDatabaseRejectsEmptyTarget(t *testing.T) {
	isolateHome(t)
	svc, _ := newExportTestService(t)

	for _, target := range []string{"", "   "} {
		if err := svc.ExportDatabase("exporttest", target, true); err == nil {
			t.Errorf("空路径 %q 未被拒绝", target)
		}
	}
}
