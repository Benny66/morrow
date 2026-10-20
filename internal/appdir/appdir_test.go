package appdir

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isolateHome 把「用户配置目录」重定向到测试临时目录，避免污染真实配置目录。
// 同时设 HOME 与 XDG_CONFIG_HOME：os.UserConfigDir() 在 darwin 读 HOME，
// 在 linux 优先读 XDG_CONFIG_HOME，都设才能跨平台隔离。
func isolateHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp)
	return tmp
}

// TestDirCreatesDirectory 数据目录 MUST 落在用户配置目录下且被自动创建。
func TestDirCreatesDirectory(t *testing.T) {
	base := isolateHome(t)

	dir, err := Dir("myapp")
	if err != nil {
		t.Fatalf("Dir 返回错误: %v", err)
	}
	if want := filepath.Join(base, "Library", "Application Support", "myapp"); dir != want {
		// darwin 走 Library/Application Support，linux 走 XDG_CONFIG_HOME
		if !strings.HasPrefix(dir, base) || !strings.HasSuffix(dir, "myapp") {
			t.Errorf("Dir = %q，期望落在 %q 之下且以 myapp 结尾", dir, base)
		}
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("数据目录未被创建: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("%s 不是目录", dir)
	}
}

// TestDirRejectsEscapingAppName 应用名含路径分隔符/上跳时 MUST 报错，
// 否则数据可被写到应用目录之外（如 "../../.ssh"）。
func TestDirRejectsEscapingAppName(t *testing.T) {
	isolateHome(t)

	for _, name := range []string{"", "   ", "..", ".", "a/b", `a\b`} {
		if _, err := Dir(name); err == nil {
			t.Errorf("Dir(%q) 未报错——恶意/空应用名必须被拒绝", name)
		}
	}
}

// TestFileJoinsUnderDataDir 文件路径 MUST 位于数据目录内。
func TestFileJoinsUnderDataDir(t *testing.T) {
	isolateHome(t)

	dir, err := Dir("myapp")
	if err != nil {
		t.Fatalf("Dir 返回错误: %v", err)
	}
	got, err := File("myapp", "config.json")
	if err != nil {
		t.Fatalf("File 返回错误: %v", err)
	}
	if want := filepath.Join(dir, "config.json"); got != want {
		t.Errorf("File = %q，期望 %q", got, want)
	}
}

// TestFileRejectsBadName 文件名不得为空、含路径分隔符，或指向上级。
// "." / ".." 不含分隔符，但会让 File 返回数据目录本身/父目录——必须与 Dir 的
// 应用名校验一致地被拒绝。
func TestFileRejectsBadName(t *testing.T) {
	isolateHome(t)

	for _, name := range []string{"", "  ", ".", "..", "../escape.json", "sub/x.json", `sub\x.json`} {
		if _, err := File("myapp", name); err == nil {
			t.Errorf("File(%q) 未报错——非法文件名必须被拒绝", name)
		}
	}
}

// TestDirIsUnderUserConfigDir 契约：数据目录必须由 os.UserConfigDir() 派生
// （决定「用户找得到日志」这件事，改了就改变了用户可见行为）。
func TestDirIsUnderUserConfigDir(t *testing.T) {
	isolateHome(t)

	dir, err := Dir("myapp")
	if err != nil {
		t.Fatalf("Dir 返回错误: %v", err)
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("os.UserConfigDir 失败: %v", err)
	}
	if !strings.HasPrefix(dir, configDir) {
		t.Errorf("数据目录 %q 不在用户配置目录 %q 之下", dir, configDir)
	}
}
