package main

import (
	"context"
	"errors"
	"testing"

	"morrow/internal/appinfo"
	"morrow/internal/config"
	"morrow/internal/update"
)

type fakeUpdateChecker struct {
	release *update.Release
	err     error
}

func (f fakeUpdateChecker) Latest(context.Context) (*update.Release, error) {
	return f.release, f.err
}

func newUpdateTestApp(t *testing.T, version string) *App {
	t.Helper()
	old := appinfo.InjectedVersion
	appinfo.InjectedVersion = version
	t.Cleanup(func() { appinfo.InjectedVersion = old })
	cfg := config.Default()
	cfg.SkippedUpdateVersion = ""
	return NewApp(nil, cfg)
}

func TestRunUpdateCheckFindsNewer(t *testing.T) {
	a := newUpdateTestApp(t, "1.1.1")
	a.updateChecker = fakeUpdateChecker{release: &update.Release{
		Version: "1.2.0",
		Notes:   "修复说明",
		URL:     "https://github.com/Benny66/morrow/releases/tag/v1.2.0",
	}}
	state, err := a.runUpdateCheck(context.Background(), true)
	if err != nil {
		t.Fatalf("runUpdateCheck: %v", err)
	}
	if !state.Available || state.Latest != "1.2.0" || state.URL == "" || state.CheckedAt == 0 {
		t.Fatalf("unexpected state: %+v", state)
	}
	if got := a.GetUpdateState(); got.Latest != "1.2.0" {
		t.Fatalf("GetUpdateState 未保存结果: %+v", got)
	}
}

func TestRunUpdateCheckIgnoresSameVersion(t *testing.T) {
	a := newUpdateTestApp(t, "1.1.1")
	a.updateChecker = fakeUpdateChecker{release: &update.Release{Version: "1.1.1"}}
	state, err := a.runUpdateCheck(context.Background(), true)
	if err != nil {
		t.Fatalf("runUpdateCheck: %v", err)
	}
	if state.Available {
		t.Fatalf("同版本不应提示更新: %+v", state)
	}
}

func TestRunUpdateCheckSkippedVersion(t *testing.T) {
	a := newUpdateTestApp(t, "1.1.1")
	a.updateSkipped = "1.2.0"
	a.updateChecker = fakeUpdateChecker{release: &update.Release{Version: "1.2.0", URL: "https://example.test"}}
	state, err := a.runUpdateCheck(context.Background(), true)
	if err != nil {
		t.Fatalf("runUpdateCheck: %v", err)
	}
	if !state.Available || !state.Skipped {
		t.Fatalf("应标记为已忽略: %+v", state)
	}
}

func TestRunUpdateCheckManualError(t *testing.T) {
	a := newUpdateTestApp(t, "1.1.1")
	a.updateChecker = fakeUpdateChecker{err: errors.New("network down")}
	if _, err := a.runUpdateCheck(context.Background(), true); err == nil {
		t.Fatal("手动检查失败应返回错误")
	}
	// 自动检查失败只返回错误给调用方记日志，不改状态。
	if _, err := a.runUpdateCheck(context.Background(), false); err == nil {
		t.Fatal("自动检查失败也应返回错误供日志使用")
	}
}

func TestRunUpdateCheckDevVersion(t *testing.T) {
	a := newUpdateTestApp(t, "dev")
	a.updateChecker = fakeUpdateChecker{release: &update.Release{Version: "9.9.9"}}
	if _, err := a.runUpdateCheck(context.Background(), true); err == nil {
		t.Fatal("开发版手动检查应给出明确提示")
	}
	state, err := a.runUpdateCheck(context.Background(), false)
	if err != nil {
		t.Fatalf("开发版自动检查不应报错: %v", err)
	}
	if state.Available {
		t.Fatal("开发版不应提示更新")
	}
}
