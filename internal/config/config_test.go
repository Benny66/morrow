package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"my-notice/internal/appdir"
)

// isolateHome 把数据目录重定向到测试临时目录，避免污染真实配置目录。
// HOME 与 XDG_CONFIG_HOME 都设：os.UserConfigDir() 在 darwin 读 HOME，linux 优先读后者。
func isolateHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp)
	return tmp
}

// TestWindowSizeClamps 非法窗口尺寸 MUST 回退默认值。
// 配置是用户可写文件，手改坏了不能把窗口弄成不可用。
func TestWindowSizeClamps(t *testing.T) {
	cases := []struct {
		name          string
		width, height int
		wantW, wantH  int
	}{
		{"未记录（零值）回退默认", 0, 0, DefaultWindowWidth, DefaultWindowHeight},
		{"负数回退默认", -100, -100, DefaultWindowWidth, DefaultWindowHeight},
		{"过小回退默认", MinWindowWidth - 1, MinWindowHeight - 1, DefaultWindowWidth, DefaultWindowHeight},
		{"恰好等于下界可用", MinWindowWidth, MinWindowHeight, MinWindowWidth, MinWindowHeight},
		{"合法值原样保留", 1440, 900, 1440, 900},
		{"超宽屏不夹取上限", 3840, 2160, 3840, 2160},
		{"仅宽非法则仅宽回退", 100, 900, DefaultWindowWidth, 900},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := &Config{WindowWidth: c.width, WindowHeight: c.height}
			gotW, gotH := cfg.WindowSize()
			if gotW != c.wantW || gotH != c.wantH {
				t.Errorf("WindowSize() = (%d,%d)，期望 (%d,%d)", gotW, gotH, c.wantW, c.wantH)
			}
		})
	}
}

// TestLoadOldConfigWithoutWindowFields 旧版 config.json（无窗口字段）MUST 能正常加载，
// 缺字段即零值 → WindowSize 回退默认。保证升级用户不需要手工改配置。
func TestLoadOldConfigWithoutWindowFields(t *testing.T) {
	isolateHome(t)

	path, err := appdir.File("cfgtest", "config.json")
	if err != nil {
		t.Fatalf("appdir.File 失败: %v", err)
	}
	// 模拟旧版：只有 theme
	if err := os.WriteFile(path, []byte(`{"theme":"light"}`), 0o644); err != nil {
		t.Fatalf("写入旧配置失败: %v", err)
	}

	cfg, err := Load("cfgtest")
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if cfg.Theme != "light" {
		t.Errorf("theme = %q，期望 light（旧字段必须保留）", cfg.Theme)
	}
	if w, h := cfg.WindowSize(); w != DefaultWindowWidth || h != DefaultWindowHeight {
		t.Errorf("WindowSize() = (%d,%d)，期望默认值", w, h)
	}
}

// TestSavePersistsWindowGeometry 窗口几何 MUST 落盘并可读回。
func TestSavePersistsWindowGeometry(t *testing.T) {
	isolateHome(t)

	cfg, err := Load("geomtest")
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	cfg.WindowWidth, cfg.WindowHeight, cfg.WindowMaximised = 1280, 800, true
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}

	reloaded, err := Load("geomtest")
	if err != nil {
		t.Fatalf("重新 Load 失败: %v", err)
	}
	if w, h := reloaded.WindowSize(); w != 1280 || h != 800 {
		t.Errorf("尺寸 = (%d,%d)，期望 (1280,800)", w, h)
	}
	if !reloaded.WindowMaximised {
		t.Error("最大化状态未持久化")
	}
}

// TestSaveUsesSnakeCaseTags 落盘 JSON MUST 用 snake_case 字段名（与 CLAUDE.md 的 JSON tag 规范一致）。
func TestSaveUsesSnakeCaseTags(t *testing.T) {
	isolateHome(t)

	cfg, err := Load("tagtest")
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	cfg.WindowWidth, cfg.WindowHeight = 1280, 800
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}

	path, err := appdir.File("tagtest", "config.json")
	if err != nil {
		t.Fatalf("appdir.File 失败: %v", err)
	}
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("读取配置失败: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("解析配置失败: %v", err)
	}
	for _, key := range []string{"window_width", "window_height", "window_maximised"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("落盘 JSON 缺少字段 %q（应为 snake_case，实际键: %v）", key, raw)
		}
	}
}
