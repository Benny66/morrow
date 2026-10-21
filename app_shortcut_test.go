package main

import (
	"testing"

	"morrow/internal/config"
)

// TestGetPresentationShortcutStatus 配置值与注册状态分开回传：
// 配置了但未生效时，设置页仍要能看到用户填了什么。
func TestGetPresentationShortcutStatus(t *testing.T) {
	app := &App{
		cfg:                     &config.Config{ShortcutPresentation: "CmdOrCtrl+Shift+P"},
		presentationShortcutAcc: "CmdOrCtrl+Shift+P",
	}
	got := app.GetPresentationShortcut()
	if got.Accelerator != "CmdOrCtrl+Shift+P" {
		t.Errorf("Accelerator = %q", got.Accelerator)
	}
	if !got.Registered {
		t.Error("已注册时 Registered 应为 true")
	}

	app.presentationShortcutAcc = ""
	got = app.GetPresentationShortcut()
	if got.Accelerator != "CmdOrCtrl+Shift+P" {
		t.Error("注册失败时仍应回显配置值")
	}
	if got.Registered {
		t.Error("未注册时 Registered 应为 false")
	}
}
