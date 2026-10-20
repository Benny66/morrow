package reveal

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestOpenRejectsMissingDir 目录不存在 MUST 返回明确错误，
// 而不是把平台命令（open/explorer/xdg-open）的报错原样抛给用户。
func TestOpenRejectsMissingDir(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "not-there")
	if err := Open(missing); err == nil {
		t.Errorf("Open(%q) 未报错——不存在的目录必须被拒绝", missing)
	}
}

// TestOpenRejectsFile 传入文件而非目录时 MUST 报错（打开的目标是目录）。
func TestOpenRejectsFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("准备测试文件失败: %v", err)
	}
	if err := Open(file); err == nil {
		t.Errorf("Open(%q) 未报错——文件不是目录，必须被拒绝", file)
	}
}

// TestOpenerPerPlatform 已支持平台 MUST 给出命令名，未知平台 MUST 报错。
func TestOpenerPerPlatform(t *testing.T) {
	name, err := opener()
	switch runtime.GOOS {
	case "darwin", "windows", "linux":
		if err != nil {
			t.Fatalf("已支持平台 %s 不应报错: %v", runtime.GOOS, err)
		}
		if name == "" {
			t.Errorf("平台 %s 返回了空命令名", runtime.GOOS)
		}
	default:
		if err == nil {
			t.Errorf("未知平台 %s 应返回错误（不得静默降级成 no-op）", runtime.GOOS)
		}
	}
}
