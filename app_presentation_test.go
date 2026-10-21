package main

import (
	"testing"
	"time"

	"morrow/internal/config"
	"morrow/internal/model"
	"morrow/internal/pause"
	"morrow/internal/presentation"
	"morrow/internal/restmode"
)

func testTime() time.Time {
	return time.Date(2026, 10, 21, 9, 0, 0, 0, time.Local)
}

// TestPetShouldBeVisibleFollowsPresentation 演示模式只影响运行期可见性，不改配置意图。
func TestPetShouldBeVisibleFollowsPresentation(t *testing.T) {
	controller := presentation.NewController()
	app := &App{
		cfg:          &config.Config{PetVisible: true},
		presentation: controller,
	}
	if !app.petShouldBeVisible() {
		t.Fatal("未演示 + 配置可见时应显示")
	}

	controller.Enter(presentation.SourceManual, testTime())
	if app.petShouldBeVisible() {
		t.Error("演示模式中不应显示宠物")
	}

	controller.Exit(testTime())
	if !app.petShouldBeVisible() {
		t.Error("退出演示后应恢复用户配置的可见性")
	}

	app.cfg.PetVisible = false
	if app.petShouldBeVisible() {
		t.Error("用户隐藏宠物时，退出演示后仍应保持隐藏")
	}
}

// TestGetPauseStatePresentationFields GetPauseState MUST 合并演示状态与退出时间。
func TestGetPauseStatePresentationFields(t *testing.T) {
	controller := presentation.NewController()
	app := &App{presentation: controller}

	controller.Enter(presentation.SourceManual, testTime())
	state := app.GetPauseState()
	if !state.PresentationActive || state.PresentationSource != string(presentation.SourceManual) {
		t.Errorf("演示中状态未回传: %+v", state)
	}
	if state.CaptureExclusionSupported != captureProtectionSupported() {
		t.Errorf("CaptureExclusionSupported = %v，期望 %v",
			state.CaptureExclusionSupported, captureProtectionSupported())
	}

	controller.Exit(testTime())
	state = app.GetPauseState()
	if state.PresentationActive {
		t.Error("退出后 PresentationActive 应为 false")
	}
	if state.PresentationExitedAt == 0 {
		t.Error("退出后 PresentationExitedAt 应记录时间")
	}
}

// TestSetPresentationModeSyncsPause 绑定入口 MUST 同步 pause bundle，并拒绝未知来源。
func TestSetPresentationModeSyncsPause(t *testing.T) {
	bundle := &pause.Bundle{Presentation: pause.NewPresentationDetector()}
	app := &App{
		cfg:          &config.Config{PetVisible: true},
		presentation: presentation.NewController(),
		pause:        bundle,
	}

	if err := app.SetPresentationMode(true, string(presentation.SourceManual)); err != nil {
		t.Fatalf("进入演示失败: %v", err)
	}
	if got := bundle.Presentation.Detect(); got != pause.ReasonPresentation {
		t.Errorf("pause bundle 未同步演示状态: %q", got)
	}
	if !app.GetPauseState().PresentationActive {
		t.Error("GetPauseState 未反映演示状态")
	}

	if err := app.SetPresentationMode(false, ""); err != nil {
		t.Fatalf("退出演示失败: %v", err)
	}
	if got := bundle.Presentation.Detect(); got != pause.ReasonNone {
		t.Errorf("退出后 pause bundle 仍暂停: %q", got)
	}

	if err := app.SetPresentationMode(true, "auto"); err == nil {
		t.Error("未知来源应返回错误")
	}
}

// TestGetPauseStateSuggestionAndDismiss 建议状态回传与忽略入口。
func TestGetPauseStateSuggestionAndDismiss(t *testing.T) {
	controller := presentation.NewController()
	app := &App{presentation: controller}
	t0 := testTime()

	controller.Observe(presentation.TriggerMeeting, true, t0)
	controller.Observe(presentation.TriggerMeeting, true, t0.Add(presentation.SuggestionDelay))
	if !app.GetPauseState().SuggestionVisible {
		t.Fatal("满 5 秒后 GetPauseState 应回传建议可见")
	}
	if err := app.DismissPresentationSuggestion(); err != nil {
		t.Fatalf("忽略建议失败: %v", err)
	}
	if app.GetPauseState().SuggestionVisible {
		t.Error("忽略后建议应不可见")
	}
}

// TestTickPresentationAutoExitsSuggested 建议进入的演示在触发消失 30 秒后自动退出。
func TestTickPresentationAutoExitsSuggested(t *testing.T) {
	controller := presentation.NewController()
	app := &App{
		cfg:          &config.Config{PetVisible: true},
		presentation: controller,
	}
	t0 := testTime()
	controller.Enter(presentation.SourceSuggested, t0)

	// 第一次观察：触发已消失，开始 30 秒宽限。
	app.tickPresentation(t0.Add(time.Second))
	if !app.GetPauseState().PresentationActive {
		t.Fatal("宽限期内不应退出")
	}
	// 宽限结束：自动退出。
	app.tickPresentation(t0.Add(presentation.AutoExitDelay + time.Second))
	state := app.GetPauseState()
	if state.PresentationActive {
		t.Error("宽限结束后应自动退出")
	}
	if state.PresentationExitedAt == 0 {
		t.Error("自动退出后应记录退出时间")
	}
}

// TestGetDeferralStateSupport 无引擎时也应回传平台是否支持顺延。
func TestGetDeferralStateSupport(t *testing.T) {
	app := &App{}
	got := app.GetDeferralState()
	if got.Active {
		t.Error("无引擎时不应有顺延")
	}
	if got.Supported != pause.IdleSupported() {
		t.Errorf("Supported = %v，期望 %v", got.Supported, pause.IdleSupported())
	}
}

// TestEnterPresentationSkipsRestCountdown 进入演示模式必须结束倒计时并归零，
// 不能把覆盖层留在屏幕共享里。
func TestEnterPresentationSkipsRestCountdown(t *testing.T) {
	machine := restmode.NewMachine()
	if !machine.Enter(restmode.RestRequest{
		FireID:   0,
		Kind:     model.KindStand,
		Title:    "站一会",
		Duration: time.Minute,
	}) {
		t.Fatal("无法进入休息倒计时")
	}
	app := &App{
		cfg:          &config.Config{PetVisible: true},
		presentation: presentation.NewController(),
		restMode:     machine,
	}

	if err := app.SetPresentationMode(true, string(presentation.SourceManual)); err != nil {
		t.Fatalf("进入演示失败: %v", err)
	}
	if got := machine.State(); got != restmode.StateIdle {
		t.Errorf("进入演示后休息状态 = %q，期望 idle", got)
	}
}

// TestEnterPresentationCancelsRestWarning 预警阶段只取消，不产生 CheckIn。
func TestEnterPresentationCancelsRestWarning(t *testing.T) {
	machine := restmode.NewMachine()
	if !machine.EnterWarning(restmode.RestRequest{
		Kind:     model.KindStand,
		Title:    "站一会",
		Duration: time.Minute,
	}) {
		t.Fatal("无法进入休息预警")
	}
	app := &App{
		cfg:          &config.Config{PetVisible: true},
		presentation: presentation.NewController(),
		restMode:     machine,
	}

	if err := app.SetPresentationMode(true, string(presentation.SourceManual)); err != nil {
		t.Fatalf("进入演示失败: %v", err)
	}
	if got := machine.State(); got != restmode.StateIdle {
		t.Errorf("进入演示后休息预警状态 = %q，期望 idle", got)
	}
}
