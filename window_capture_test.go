package main

import (
	"testing"

	"morrow/internal/config"
)

// TestPetWindowOptionsEnableContentProtection 宠物窗必须排除出屏幕捕获。
func TestPetWindowOptionsEnableContentProtection(t *testing.T) {
	opts := petWindowOptions(config.Default())
	if !opts.ContentProtectionEnabled {
		t.Error("宠物窗口未启用 ContentProtectionEnabled：共享屏幕会看到桌宠")
	}
}

// TestAlertWindowOptionsEnableContentProtection 强提醒遮罩同样不得进入捕获。
func TestAlertWindowOptionsEnableContentProtection(t *testing.T) {
	opts := alertWindowOptions(alertWindowName, 800, 600)
	if !opts.ContentProtectionEnabled {
		t.Error("强提醒窗口未启用 ContentProtectionEnabled：共享屏幕会看到遮罩")
	}
}
