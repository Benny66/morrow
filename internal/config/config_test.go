package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"morrow/internal/appdir"
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

// TestClampIdleMinutes 空闲阈值 MUST 夹到 3/5/10 三档中最接近的一档。
//
// 【为什么单独测】clampIdleMinutes 是设置页下拉之外的第二道防线（用户手改 config.json），
// 逻辑错会让 4 归 5 或 8 归 5 这类偏差出现，直接影响暂停灵敏度。
func TestClampIdleMinutes(t *testing.T) {
	cases := []struct {
		in   int
		want int
	}{
		{-100, 3},
		{0, 3},
		{2, 3},
		{3, 3},
		{4, 3}, // 与 5 差 1、与 3 差 1；并列取更保守的 3
		{5, 5},
		{6, 5},
		{7, 5},
		{8, 10}, // 与 10 差 2、与 5 差 3
		{9, 10},
		{10, 10},
		{999, 10},
	}
	for _, c := range cases {
		if got := clampIdleMinutes(c.in); got != c.want {
			t.Errorf("clampIdleMinutes(%d) = %d，期望 %d", c.in, got, c.want)
		}
	}
}

// TestPauseIdleMinutesValueUsesClamp 保证 PauseIdleMinutesValue 的合法范围与
// clampIdleMinutes 一致（3/5/10 三档之外都回落到 DefaultPauseIdleMinutes=5）。
func TestPauseIdleMinutesValueUsesClamp(t *testing.T) {
	cases := []struct {
		name string
		in   int
		want int
	}{
		{"3 档保留", 3, 3},
		{"5 档保留", 5, 5},
		{"10 档保留", 10, 10},
		{"越界回落到默认 5", 30, DefaultPauseIdleMinutes},
		{"负值回落到默认 5", -1, DefaultPauseIdleMinutes},
		{"4 是合法范围但非档位：保留原值", 4, 4}, // PauseIdleMinutesValue 只夹范围不夹档位
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := &Config{PauseIdleMinutes: c.in}
			if got := cfg.PauseIdleMinutesValue(); got != c.want {
				t.Errorf("PauseIdleMinutesValue(%d) = %d，期望 %d", c.in, got, c.want)
			}
		})
	}
}

// TestSavePauseSettingsPersistsFlags 保存后 4 个 flag MUST 都能落盘。
func TestSavePauseSettingsPersistsFlags(t *testing.T) {
	isolateHome(t)
	cfg := Default()
	cfg.path = filepath.Join(t.TempDir(), "config.json")
	in := PauseSettings{
		IdleEnabled:       false,
		LockEnabled:       true,
		MeetingEnabled:    false,
		FullscreenEnabled: true,
		IdleMinutes:       10,
	}
	if err := cfg.SavePauseSettings(in); err != nil {
		t.Fatalf("SavePauseSettings 失败: %v", err)
	}
	if cfg.PauseIdleEnabled || !cfg.PauseLockEnabled || cfg.PauseMeetingEnabled || !cfg.PauseFullscreenEnabled {
		t.Errorf("4 个开关未按预期落盘：%+v", cfg)
	}
	if cfg.PauseIdleMinutes != 10 {
		t.Errorf("PauseIdleMinutes 应为 10，实际 %d", cfg.PauseIdleMinutes)
	}
}

// TestSavePauseSettingsClampsIdleMinutes 非法阈值 MUST 被夹到合法档位（不报错）。
func TestSavePauseSettingsClampsIdleMinutes(t *testing.T) {
	isolateHome(t)
	cfg := Default()
	cfg.path = filepath.Join(t.TempDir(), "config.json")
	if err := cfg.SavePauseSettings(PauseSettings{IdleMinutes: 8}); err != nil {
		t.Fatalf("SavePauseSettings 失败: %v", err)
	}
	if cfg.PauseIdleMinutes != 10 {
		t.Errorf("IdleMinutes=8 应被夹到最近档位 10，实际 %d", cfg.PauseIdleMinutes)
	}
}

// TestUninterruptedSettingsDefaults 演示建议与自然停顿顺延的默认值。
// 默认开着：两者都是「不打扰」的温和增强，关掉会让新用户回到原始模式。
func TestUninterruptedSettingsDefaults(t *testing.T) {
	cfg := Default()
	got := cfg.LoadReminderSettings()
	if !got.PresentationSuggestEnabled {
		t.Error("PresentationSuggestEnabled 默认应为 true")
	}
	if !got.NaturalPauseEnabled {
		t.Error("NaturalPauseEnabled 默认应为 true")
	}
	if got.NaturalPauseMaxSeconds != DefaultNaturalPauseMaxSeconds {
		t.Errorf("NaturalPauseMaxSeconds 默认 = %d，期望 %d",
			got.NaturalPauseMaxSeconds, DefaultNaturalPauseMaxSeconds)
	}
}

// TestUninterruptedSettingsRoundTrip 保存后三个字段 MUST 原样读回。
func TestUninterruptedSettingsRoundTrip(t *testing.T) {
	isolateHome(t)
	cfg := Default()
	cfg.path = filepath.Join(t.TempDir(), "config.json")

	in := cfg.LoadReminderSettings()
	in.PresentationSuggestEnabled = false
	in.NaturalPauseEnabled = false
	in.NaturalPauseMaxSeconds = 30
	if err := cfg.SaveReminderSettings(in); err != nil {
		t.Fatalf("SaveReminderSettings 失败: %v", err)
	}

	got := cfg.LoadReminderSettings()
	if got.PresentationSuggestEnabled || got.NaturalPauseEnabled {
		t.Errorf("布尔开关未按预期保存: %+v", got)
	}
	if got.NaturalPauseMaxSeconds != 30 {
		t.Errorf("NaturalPauseMaxSeconds = %d，期望 30", got.NaturalPauseMaxSeconds)
	}
}

// TestNaturalPauseMaxSecondsClamps 只接受 0/30/60；其他值回落默认 60。
// 0 是合法值（关闭顺延），不能与「非法值」混为一谈。
func TestNaturalPauseMaxSecondsClamps(t *testing.T) {
	cases := []struct {
		in   int
		want int
	}{
		{0, 0},
		{30, 30},
		{60, 60},
		{15, DefaultNaturalPauseMaxSeconds},
		{-1, DefaultNaturalPauseMaxSeconds},
		{999, DefaultNaturalPauseMaxSeconds},
	}
	for _, c := range cases {
		isolateHome(t)
		cfg := Default()
		cfg.path = filepath.Join(t.TempDir(), "config.json")
		in := cfg.LoadReminderSettings()
		in.NaturalPauseMaxSeconds = c.in
		if err := cfg.SaveReminderSettings(in); err != nil {
			t.Fatalf("SaveReminderSettings 失败: %v", err)
		}
		if got := cfg.LoadReminderSettings().NaturalPauseMaxSeconds; got != c.want {
			t.Errorf("NaturalPauseMaxSeconds(%d) = %d，期望 %d", c.in, got, c.want)
		}
	}
}

// TestShortcutPresentationRoundTrip 演示模式快捷键 MUST 落盘并可读回。
func TestShortcutPresentationRoundTrip(t *testing.T) {
	isolateHome(t)
	cfg, err := Load("shortcuttest")
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	cfg.ShortcutPresentation = "CmdOrCtrl+Shift+P"
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}

	reloaded, err := Load("shortcuttest")
	if err != nil {
		t.Fatalf("重新 Load 失败: %v", err)
	}
	if reloaded.ShortcutPresentation != "CmdOrCtrl+Shift+P" {
		t.Errorf("ShortcutPresentation = %q，期望 CmdOrCtrl+Shift+P", reloaded.ShortcutPresentation)
	}
}

// TestOnboardingCompletedDefaultsFalseAndPersists 首次引导标记默认 false，写完可读回。
func TestOnboardingCompletedDefaultsFalseAndPersists(t *testing.T) {
	isolateHome(t)

	cfg, err := Load("onboardingtest")
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if cfg.OnboardingCompleted {
		t.Fatal("首次引导标记默认应为 false")
	}

	cfg.OnboardingCompleted = true
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}
	reloaded, err := Load("onboardingtest")
	if err != nil {
		t.Fatalf("重新 Load 失败: %v", err)
	}
	if !reloaded.OnboardingCompleted {
		t.Fatal("首次引导标记未持久化")
	}
}

// TestLoadOldConfigWithoutOnboardingField 旧配置缺 onboarding_completed 时按 false 处理。
func TestLoadOldConfigWithoutOnboardingField(t *testing.T) {
	isolateHome(t)

	path, err := appdir.File("oldcfgtest", "config.json")
	if err != nil {
		t.Fatalf("appdir.File 失败: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"theme":"light"}`), 0o644); err != nil {
		t.Fatalf("写入旧配置失败: %v", err)
	}

	cfg, err := Load("oldcfgtest")
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if cfg.OnboardingCompleted {
		t.Fatal("旧配置缺 onboarding_completed 时应为 false")
	}
}

// TestPetShapeSeedMigration 锁定「只有旧配置才播种」的判据。
//
// 全新安装的 Default 必须已播种（否则第一次 PetGrowth 会把今天完成率写进高水位）；
// 旧配置缺 pet_shape_seeded 键时必须回到未播种，让 App 层做一次性迁移。
func TestPetShapeSeedMigration(t *testing.T) {
	isolateHome(t)

	// 1. 全新安装：Load 创建默认文件，不需要迁移。
	fresh, err := Load("freshpet")
	if err != nil {
		t.Fatalf("Load 全新配置失败: %v", err)
	}
	if !fresh.PetShapeSeeded {
		t.Error("全新安装应已播种（PetShapeSeeded=true）")
	}
	if fresh.PetShapeHighWater != 0 {
		t.Errorf("全新安装高水位 = %d，期望 0", fresh.PetShapeHighWater)
	}

	// 2. 旧版配置：缺 key，必须标记为待播种。
	path, err := appdir.File("oldpet", "config.json")
	if err != nil {
		t.Fatalf("appdir.File 失败: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"theme":"light"}`), 0o644); err != nil {
		t.Fatalf("写入旧配置失败: %v", err)
	}
	old, err := Load("oldpet")
	if err != nil {
		t.Fatalf("Load 旧配置失败: %v", err)
	}
	if old.PetShapeSeeded {
		t.Error("旧配置缺少 pet_shape_seeded 时应待播种（false）")
	}

	// 3. 新配置已有 key：原样保留，不重复迁移。
	path, err = appdir.File("newpet", "config.json")
	if err != nil {
		t.Fatalf("appdir.File 失败: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"theme":"dark","pet_shape_seeded":true,"pet_shape_high_water":3}`), 0o644); err != nil {
		t.Fatalf("写入新配置失败: %v", err)
	}
	current, err := Load("newpet")
	if err != nil {
		t.Fatalf("Load 新配置失败: %v", err)
	}
	if !current.PetShapeSeeded || current.PetShapeHighWater != 3 {
		t.Errorf("已有播种字段应原样保留，实际 seeded=%v high=%d",
			current.PetShapeSeeded, current.PetShapeHighWater)
	}
}
