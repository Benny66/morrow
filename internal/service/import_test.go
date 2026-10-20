package service

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"morrow/internal/appdir"
	"morrow/internal/apperr"
)

// TestImportDatabaseRestoresData 恢复后 MUST 能通过服务读到备份里的数据——
// 这是「恢复」能力的全部意义；也顺带验证 s.db 已被替换为新连接。
func TestImportDatabaseRestoresData(t *testing.T) {
	isolateHome(t)

	// 现役库落在数据目录（ImportDatabase 按此路径做备份/替换），写入一条标记数据。
	livePath, err := appdir.File("importtest", "importtest.db")
	if err != nil {
		t.Fatalf("获取现役库路径失败: %v", err)
	}
	live, err := gorm.Open(sqlite.Open(livePath), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开现役库失败: %v", err)
	}
	if err := live.AutoMigrate(&exportProbe{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	if err := live.Create(&exportProbe{Name: "现役数据"}).Error; err != nil {
		t.Fatalf("写入现役数据失败: %v", err)
	}
	svc := New(live)

	// 备份库在数据目录之外，含不同的数据。
	backupPath := filepath.Join(t.TempDir(), "backup.db")
	backup, err := gorm.Open(sqlite.Open(backupPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开备份库失败: %v", err)
	}
	if err := backup.AutoMigrate(&exportProbe{}); err != nil {
		t.Fatalf("迁移备份库失败: %v", err)
	}
	if err := backup.Create(&exportProbe{Name: "备份数据"}).Error; err != nil {
		t.Fatalf("写入备份数据失败: %v", err)
	}

	if err := svc.ImportDatabase("importtest", backupPath); err != nil {
		t.Fatalf("ImportDatabase 失败: %v", err)
	}

	var rows []exportProbe
	if err := svc.db.Find(&rows).Error; err != nil {
		t.Fatalf("查询恢复后的库失败: %v", err)
	}
	if len(rows) != 1 || rows[0].Name != "备份数据" {
		t.Errorf("恢复后的数据 = %+v，期望 1 条 Name=备份数据", rows)
	}
}

// TestImportDatabaseRollsBackOnBadFile 源不是合法 SQLite 库时 MUST 报错且回滚：
// 现役库原样恢复、服务继续可用（重开成功、数据还在）。
func TestImportDatabaseRollsBackOnBadFile(t *testing.T) {
	isolateHome(t)

	livePath, err := appdir.File("importtest", "importtest.db")
	if err != nil {
		t.Fatalf("获取现役库路径失败: %v", err)
	}
	live, err := gorm.Open(sqlite.Open(livePath), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开现役库失败: %v", err)
	}
	if err := live.AutoMigrate(&exportProbe{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	if err := live.Create(&exportProbe{Name: "现役数据"}).Error; err != nil {
		t.Fatalf("写入现役数据失败: %v", err)
	}
	svc := New(live)

	badFile := filepath.Join(t.TempDir(), "not-a-db.db")
	if wErr := os.WriteFile(badFile, []byte("这不是一个 SQLite 数据库"), 0o644); wErr != nil {
		t.Fatalf("准备坏文件失败: %v", wErr)
	}

	importErr := svc.ImportDatabase("importtest", badFile)
	if importErr == nil {
		t.Fatal("导入坏文件未被拒绝")
	}

	// 回滚后服务必须可用且数据原样。
	var rows []exportProbe
	if qErr := svc.db.Find(&rows).Error; qErr != nil {
		t.Fatalf("回滚后查询失败（服务不可用）: %v", qErr)
	}
	if len(rows) != 1 || rows[0].Name != "现役数据" {
		t.Errorf("回滚后的数据 = %+v，期望 1 条 Name=现役数据", rows)
	}
	// 现役库文件本身也被还原（.pre-import 备份已改名回来）。
	if _, statErr := os.Stat(livePath); statErr != nil {
		t.Errorf("回滚后现役库文件不存在: %v", statErr)
	}
}

// TestImportDatabaseRejectsSourceInsideDataDir 源位于数据目录内 MUST 被拒——
// 数据目录里躺着现役库，拿「目录内文件」当源等于把库往自己身上复制（与导出同规则）。
func TestImportDatabaseRejectsSourceInsideDataDir(t *testing.T) {
	isolateHome(t)
	svc, _ := newExportTestService(t)

	dataDir, err := appdir.Dir("exporttest")
	if err != nil {
		t.Fatalf("获取数据目录失败: %v", err)
	}
	inside := filepath.Join(dataDir, "backup.db")
	if wErr := os.WriteFile(inside, []byte("x"), 0o644); wErr != nil {
		t.Fatalf("准备源文件失败: %v", wErr)
	}

	importErr := svc.ImportDatabase("exporttest", inside)
	if importErr == nil {
		t.Fatal("数据目录内的源未被拒绝")
	}
	var ae *apperr.Error
	if !errors.As(importErr, &ae) || ae.Code != apperr.CodeValidation {
		t.Errorf("错误码不是 validation: %v", importErr)
	}
}

// TestImportDatabaseRejectsEmptyAndMissing 空路径与不存在的源 MUST 被拒。
func TestImportDatabaseRejectsEmptyAndMissing(t *testing.T) {
	isolateHome(t)
	svc, _ := newExportTestService(t)

	for _, source := range []string{"", "   ", filepath.Join(t.TempDir(), "不存在.db")} {
		if err := svc.ImportDatabase("exporttest", source); err == nil {
			t.Errorf("源 %q 未被拒绝", source)
		}
	}
}
