package config

import (
	"testing"
	"time"

	"morrow/internal/model"
	"morrow/internal/sit"
)

// TestSitDefaults 保证新安装/旧配置都有可用的久坐默认值。
func TestSitDefaults(t *testing.T) {
	isolateHome(t)
	cfg, err := Load("sittest")
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if !cfg.SitEnabled {
		t.Error("SitEnabled 默认应为 true")
	}
	if cfg.SitProfileEnabled {
		t.Error("SitProfileEnabled 默认应为 false（需显式同意）")
	}
	if cfg.SitProfileAsked {
		t.Error("SitProfileAsked 默认应为 false")
	}
	if cfg.SitNudgeThresholdMinutes != sit.DefaultNudgeThresholdMinutes {
		t.Errorf("阈值默认 = %d，期望 %d", cfg.SitNudgeThresholdMinutes, sit.DefaultNudgeThresholdMinutes)
	}
	if got := sit.NormalizeTone(cfg.SitTone); got != sit.DefaultTone {
		t.Errorf("语气默认 = %q，期望 %q", got, sit.DefaultTone)
	}

	settings := cfg.LoadSitSettings()
	if !settings.Enabled || settings.ProfileEnabled || settings.ProfileAsked {
		t.Errorf("SitSettings 默认值不对: %+v", settings)
	}
	if settings.ThresholdMinutes != 60 || settings.Tone != sit.ToneSassy {
		t.Errorf("SitSettings 阈值/语气 = (%d,%q)，期望 (60,sassy)", settings.ThresholdMinutes, settings.Tone)
	}
}

// TestSitSettingsClamp 非法阈值夹到最近档，非法语气回落毒舌。
func TestSitSettingsClamp(t *testing.T) {
	isolateHome(t)
	cfg, err := Load("sitclamp")
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if err := cfg.SaveSitSettings(SitSettings{
		Enabled:          true,
		ThresholdMinutes: 47,
		Tone:             sit.Tone("unknown"),
	}); err != nil {
		t.Fatalf("保存失败: %v", err)
	}
	got := cfg.LoadSitSettings()
	if got.ThresholdMinutes != 45 {
		t.Errorf("47 应夹到 45，实际 %d", got.ThresholdMinutes)
	}
	if got.Tone != sit.ToneSassy {
		t.Errorf("非法语气应回落到 sassy，实际 %q", got.Tone)
	}
	if err := cfg.SaveSitSettings(SitSettings{Enabled: true, ThresholdMinutes: 999, Tone: sit.ToneGentle}); err != nil {
		t.Fatalf("保存失败: %v", err)
	}
	if got := cfg.LoadSitSettings().ThresholdMinutes; got != 90 {
		t.Errorf("999 应夹到 90，实际 %d", got)
	}
}

// TestSitSettingsRoundTrip 开关/同意/阈值/语气必须落盘并可读回。
func TestSitSettingsRoundTrip(t *testing.T) {
	isolateHome(t)
	cfg, err := Load("sitsave")
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	want := SitSettings{
		Enabled:          false,
		ProfileEnabled:   true,
		ProfileAsked:     true,
		ThresholdMinutes: 90,
		Tone:             sit.ToneGentle,
	}
	if err := cfg.SaveSitSettings(want); err != nil {
		t.Fatalf("保存失败: %v", err)
	}
	reloaded, err := Load("sitsave")
	if err != nil {
		t.Fatalf("重新 Load 失败: %v", err)
	}
	got := reloaded.LoadSitSettings()
	if got.Enabled != want.Enabled || got.ProfileEnabled != want.ProfileEnabled ||
		got.ProfileAsked != want.ProfileAsked || got.ThresholdMinutes != want.ThresholdMinutes ||
		got.Tone != want.Tone {
		t.Errorf("往返后 = %+v，期望 %+v", got, want)
	}
}

// TestSitMutedToday 只静音当天，跨日自然恢复。
func TestSitMutedToday(t *testing.T) {
	isolateHome(t)
	cfg, err := Load("sitmute")
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if cfg.SitMutedToday() {
		t.Error("新配置不应处于今天静音")
	}
	if err := cfg.MuteSitToday(); err != nil {
		t.Fatalf("静音今天失败: %v", err)
	}
	if !cfg.SitMutedToday() {
		t.Error("静音后 SitMutedToday 应为 true")
	}
	if got := cfg.LoadSitSettings().MutedToday; !got {
		t.Error("SitSettings.MutedToday 应为 true")
	}
	// 昨天的静音日期不应影响今天。
	cfg.SitMutedDate = time.Now().AddDate(0, 0, -1).Format(model.DateLayout)
	if cfg.SitMutedToday() {
		t.Error("昨天的静音日期不应让今天处于静音")
	}
}
