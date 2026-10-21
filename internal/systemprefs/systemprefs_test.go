package systemprefs

import (
	"strings"
	"testing"
)

// TestURLForCoversAllPanes 验证三类权限都有可用的深链，且面板名不重复。
//
// 【为什么必须测】URL 拼错时 `open` 仍会以 0 退出（系统打开隐私根页），
// 用户以为跳成功了却停在错误位置——典型的没有症状的失效。这里锁死前缀与 anchor。
func TestURLForCoversAllPanes(t *testing.T) {
	const prefix = "x-apple.systempreferences:com.apple.preference.security?Privacy_"
	wantAnchors := map[Pane]string{
		PaneScreenRecording: "ScreenCapture",
		PaneFocus:           "Focus",
		PaneCalendar:        "Calendars",
	}
	seen := map[string]bool{}
	for pane, anchor := range wantAnchors {
		u := URLFor(pane)
		if u == "" {
			t.Errorf("pane %q 缺少深链", pane)
			continue
		}
		if !strings.HasPrefix(u, prefix) {
			t.Errorf("pane %q 的深链前缀不对: %s", pane, u)
		}
		if got := strings.TrimPrefix(u, prefix); got != anchor {
			t.Errorf("pane %q 的 anchor = %q，期望 %q", pane, got, anchor)
		}
		if seen[u] {
			t.Errorf("pane %q 与其他 pane 共用同一个深链: %s", pane, u)
		}
		seen[u] = true
	}
}

// TestValidRejectsUnknown 验证未知 pane 被判非法。
func TestValidRejectsUnknown(t *testing.T) {
	if Valid(Pane("")) || Valid(Pane("microphone")) {
		t.Error("未知 pane 应被判为非法")
	}
	for _, p := range []Pane{PaneScreenRecording, PaneFocus, PaneCalendar} {
		if !Valid(p) {
			t.Errorf("已知 pane %q 应被判为合法", p)
		}
	}
}
