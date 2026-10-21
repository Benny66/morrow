package main

import (
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"morrow/internal/config"
	"morrow/internal/model"
	"morrow/internal/service"
)

func newOnboardingTestApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)

	db, err := gorm.Open(sqlite.Open(filepath.Join(dir, "onboarding.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatalf("迁移业务模型失败: %v", err)
	}
	cfg, err := config.Load("onboarding-test")
	if err != nil {
		t.Fatalf("加载测试配置失败: %v", err)
	}
	return &App{svc: service.New(db), cfg: cfg}
}

func TestShouldShowOnboardingFreshInstall(t *testing.T) {
	app := newOnboardingTestApp(t)
	show, err := app.ShouldShowOnboarding()
	if err != nil {
		t.Fatalf("ShouldShowOnboarding 失败: %v", err)
	}
	if !show {
		t.Fatal("干净安装应显示首次引导")
	}
	if app.cfg.OnboardingCompleted {
		t.Fatal("仅查询不应写完成标记")
	}
}

func TestShouldShowOnboardingExistingScenarioAutoCompletes(t *testing.T) {
	app := newOnboardingTestApp(t)
	if _, err := app.svc.CreateScenario("日常", "每小时喝水"); err != nil {
		t.Fatalf("创建场景失败: %v", err)
	}

	show, err := app.ShouldShowOnboarding()
	if err != nil {
		t.Fatalf("ShouldShowOnboarding 失败: %v", err)
	}
	if show {
		t.Fatal("已有场景的升级用户不应看到引导")
	}
	if !app.cfg.OnboardingCompleted {
		t.Fatal("有场景时应自动补齐完成标记")
	}
	reloaded, err := config.Load("onboarding-test")
	if err != nil {
		t.Fatalf("重新加载配置失败: %v", err)
	}
	if !reloaded.OnboardingCompleted {
		t.Fatal("自动补齐的标记未落盘")
	}
}

func TestShouldShowOnboardingCompletedFlagWins(t *testing.T) {
	app := newOnboardingTestApp(t)
	app.cfg.OnboardingCompleted = true
	show, err := app.ShouldShowOnboarding()
	if err != nil {
		t.Fatalf("ShouldShowOnboarding 失败: %v", err)
	}
	if show {
		t.Fatal("完成标记为 true 时不应显示引导")
	}
}

func TestCompleteOnboardingPersists(t *testing.T) {
	app := newOnboardingTestApp(t)
	if err := app.CompleteOnboarding(); err != nil {
		t.Fatalf("CompleteOnboarding 失败: %v", err)
	}
	if !app.cfg.OnboardingCompleted {
		t.Fatal("内存中的完成标记未置位")
	}
	reloaded, err := config.Load("onboarding-test")
	if err != nil {
		t.Fatalf("重新加载配置失败: %v", err)
	}
	if !reloaded.OnboardingCompleted {
		t.Fatal("完成标记未落盘")
	}
}
