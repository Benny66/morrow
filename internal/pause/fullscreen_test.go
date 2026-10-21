package pause

import (
	"sync"
	"testing"
	"time"
)

// TestIsFullscreen 验证几何比较：窗口是否覆盖指定显示器的可用区域。
//
// 【边界值】
//   - 完全匹配 → true
//   - 稍微小于（比如留 1px 边框）→ true（阈值容忍）
//   - 明显小于（如最大化但保留菜单栏）→ false
//   - 位于另一块屏幕 → 需要按所在显示器判定，不能拿主屏 bounds 硬比
func TestIsFullscreen(t *testing.T) {
	screen := Rect{X: 0, Y: 0, Width: 1920, Height: 1080}
	cases := []struct {
		name string
		win  Rect
		want bool
	}{
		{"精确铺满", Rect{0, 0, 1920, 1080}, true},
		{"覆盖全屏且略超出（多屏坐标）", Rect{0, 0, 2560, 1440}, true},
		{"留 1px 边框", Rect{1, 1, 1918, 1078}, true},
		{"留 5px 边框（阈值内）", Rect{5, 5, 1910, 1070}, true},
		{"最大化但留菜单栏 25px（1920×1055/1920×1080=高度 97.7%，可能过阈）",
			Rect{0, 25, 1920, 1055}, true},
		{"最大化但留 Dock 与菜单栏（1920×955）", Rect{0, 25, 1920, 955}, false},
		{"窗口只覆盖一半", Rect{0, 0, 960, 540}, false},
		{"负尺寸", Rect{0, 0, -1, 1080}, false},
		{"零宽", Rect{0, 0, 0, 1080}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsFullscreenRect(tc.win, screen); got != tc.want {
				t.Errorf("IsFullscreenRect(%v, %v)=%v, want %v", tc.win, screen, got, tc.want)
			}
		})
	}
}

// TestFullscreenDetectorMatchesFrontWindow 验证 detector 按当前前台窗口变化。
func TestFullscreenDetectorMatchesFrontWindow(t *testing.T) {
	var (
		win       Rect
		permitted = true
	)
	d := NewFullscreenDetector(func() FrontWindowInfo {
		return FrontWindowInfo{
			Window:    win,
			Screen:    Rect{0, 0, 1920, 1080},
			Permitted: permitted,
		}
	})

	// 未铺满 → ReasonNone
	win = Rect{100, 100, 800, 600}
	if d.Detect() != ReasonNone {
		t.Error("小窗口不应触发全屏暂停")
	}

	// 铺满 → ReasonFullscreen
	win = Rect{0, 0, 1920, 1080}
	if d.Detect() != ReasonFullscreen {
		t.Errorf("铺满窗口应触发全屏暂停，实际 %s", d.Detect())
	}

	// 权限被撤销 → 立刻降级为 ReasonNone（用户拒绝时不再拿假信号打扰用户）
	permitted = false
	if d.Detect() != ReasonNone {
		t.Error("权限缺失时应降级为 ReasonNone")
	}
}

// TestFullscreenDetectorPermissionDeniedSkipsForever 验证权限缺失时不反复触发。
//
// 【为什么单独测】规范说不弹二次授权窗；这里保证 detector 层面同样「一次拒绝、
// 一直降级」，不会因为轮询导致上层再次弹窗。
func TestFullscreenDetectorPermissionDeniedSkipsForever(t *testing.T) {
	d := NewFullscreenDetector(func() FrontWindowInfo {
		return FrontWindowInfo{
			Window:    Rect{0, 0, 1920, 1080},
			Screen:    Rect{0, 0, 1920, 1080},
			Permitted: false,
		}
	})
	for i := 0; i < 5; i++ {
		if d.Detect() != ReasonNone {
			t.Fatalf("第 %d 次 Detect 应返回 ReasonNone", i+1)
		}
	}
}

// TestFullscreenDetectorSinceTracksTransition 验证 since 在切换时正确重置。
func TestFullscreenDetectorSinceTracksTransition(t *testing.T) {
	var win Rect
	base := time.Date(2026, 10, 20, 14, 0, 0, 0, time.UTC)
	seq := []time.Time{base, base.Add(3 * time.Second)}
	i := 0
	d := NewFullscreenDetector(func() FrontWindowInfo {
		return FrontWindowInfo{Window: win, Screen: Rect{0, 0, 1920, 1080}, Permitted: true}
	})
	d.now = func() time.Time {
		t := seq[i]
		if i < len(seq)-1 {
			i++
		}
		return t
	}

	win = Rect{0, 0, 1920, 1080}
	d.Detect()
	if s := d.Since(); !s.Equal(base) {
		t.Errorf("首次进入应记 base，实际 %v", s)
	}

	// 保持全屏：since 不变。
	d.Detect()
	if s := d.Since(); !s.Equal(base) {
		t.Errorf("持续全屏时 since 不应变化")
	}

	// 退出全屏：since 归零。
	win = Rect{100, 100, 800, 600}
	d.Detect()
	if !d.Since().IsZero() {
		t.Errorf("退出全屏后 since 应归零")
	}

	// 再次进入：since 是新时刻。
	win = Rect{0, 0, 1920, 1080}
	d.Detect()
	if s := d.Since(); s.Equal(base) {
		t.Errorf("重新进入应记新 since（不应沿用旧值）")
	}
}

// TestFullscreenPriorityOverIdle 验证优先级：全屏 > 空闲。
//
// 【为什么】用户在投屏或看视频时键盘是静止的；单说「空闲」会让人误以为「离开座位」，
// 但「全屏」更精确描述了当前状态。
func TestFullscreenPriorityOverIdle(t *testing.T) {
	fs := NewFullscreenDetector(func() FrontWindowInfo {
		return FrontWindowInfo{
			Window: Rect{0, 0, 1920, 1080}, Screen: Rect{0, 0, 1920, 1080}, Permitted: true,
		}
	})
	idle := NewIdleDetector(func() time.Duration { return 10 * time.Minute }, 5*time.Minute)
	provider := NewCompositeProvider(fs, idle)
	if s := provider.State(); s.Reason != ReasonFullscreen {
		t.Errorf("全屏应优先于空闲，实际 %s", s.Reason)
	}
}

// TestFullscreenMeetingWinsOverFullscreen 验证优先级：会议 > 全屏。
//
// 【为什么】Zoom/飞书的会议窗口常常本身就是全屏或近似全屏；此时「在开会」
// 比「在 fullscreen」更精确，文案应显示「检测到会议应用」而不是「全屏」。
func TestFullscreenMeetingWinsOverFullscreen(t *testing.T) {
	fs := NewFullscreenDetector(func() FrontWindowInfo {
		return FrontWindowInfo{
			Window: Rect{0, 0, 1920, 1080}, Screen: Rect{0, 0, 1920, 1080}, Permitted: true,
		}
	})
	meeting := NewMeetingDetector(func() string { return "us.zoom.xos" })
	provider := NewCompositeProvider(fs, meeting)
	if s := provider.State(); s.Reason != ReasonMeeting {
		t.Errorf("会议应优先于全屏，实际 %s", s.Reason)
	}
}

// TestFullscreenDetectorConcurrentAccess 验证并发安全。
func TestFullscreenDetectorConcurrentAccess(t *testing.T) {
	d := NewFullscreenDetector(func() FrontWindowInfo {
		if time.Now().UnixNano()%2 == 0 {
			return FrontWindowInfo{
				Window: Rect{0, 0, 1920, 1080}, Screen: Rect{0, 0, 1920, 1080}, Permitted: true,
			}
		}
		return FrontWindowInfo{
			Window: Rect{10, 10, 800, 600}, Screen: Rect{0, 0, 1920, 1080}, Permitted: true,
		}
	})
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); _ = d.Detect() }()
		go func() { defer wg.Done(); _ = d.Since(); _ = d.State() }()
	}
	wg.Wait()
}

// TestMultiMonitorFullscreenUsesWindowOwnScreen 验证多屏场景：窗口覆盖它【自己】
// 所在的显示器就算全屏，不能拿主屏 bounds 硬比。
//
// 【为什么要测】副屏 2560×1440、主屏 1920×1080 时，用户在副屏放全屏视频，
// 拿主屏 bounds 比较会误判为「超出屏幕」或「窗口不合法」；这里保证比较基准
// 是窗口所在屏的 bounds。
func TestMultiMonitorFullscreenUsesWindowOwnScreen(t *testing.T) {
	// 平台层负责找出窗口所在显示器并把它的 bounds 一并返回；
	// detector 只信任输入的 screen rect。
	secondary := Rect{X: 1920, Y: 0, Width: 2560, Height: 1440}
	win := Rect{X: 1920, Y: 0, Width: 2560, Height: 1440}
	if !IsFullscreenRect(win, secondary) {
		t.Error("副屏上的铺满窗口应被判为全屏")
	}
}
