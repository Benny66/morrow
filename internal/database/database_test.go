package database

import (
	"os"
	"runtime"
	"testing"

	"morrow/internal/appdir"
)

// TestInitRestrictsDatabasePermissions 数据库含久坐/应用画像等敏感数据，
// 在支持的平台上必须是仅当前用户可读（0600）。
func TestInitRestrictsDatabasePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 依赖用户目录 ACL，不检查 POSIX mode")
	}
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp)

	db, err := Init("permissiontest")
	if err != nil {
		t.Fatalf("Init 失败: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("获取 sql.DB 失败: %v", err)
	}
	defer sqlDB.Close()

	path, err := appdir.File("permissiontest", "permissiontest.db")
	if err != nil {
		t.Fatalf("appdir.File 失败: %v", err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat 数据库失败: %v", err)
	}
	if got := st.Mode().Perm(); got != 0o600 {
		t.Errorf("数据库权限 = %o，期望 600", got)
	}
}
