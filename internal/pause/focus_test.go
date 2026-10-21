package pause

import (
	"sync"
	"testing"
	"time"
)

// TestFocusDetectorReflectsEnabled 验证 FocusDetector 按回调结果切换原因。
func TestFocusDetectorReflectsEnabled(t *testing.T) {
	var enabled bool
	d := NewFocusDetector(func() FocusStatus {
		if enabled {
			return FocusStatus{Granted: true, Enabled: true}
		}
		return FocusStatus{Granted: true, Enabled: false}
	})

	if d.Detect() != ReasonNone {
		t.Error("未开启专注时应返回 ReasonNone")
	}
	enabled = true
	if d.Detect() != ReasonFocus {
		t.Errorf("专注开启时应返回 ReasonFocus，实际 %s", d.Detect())
	}
}

// TestFocusDetectorDeniedIsSilent 验证未授权时不触发暂停。
//
// 【为什么单独测】用户没给权限时不能【假装专注】导致暂停——那会让「我没授权
// 但应用照旧不听我的」变成不可解释的失效。授权缺席 = 检测缺席 = 不暂停。
func TestFocusDetectorDeniedIsSilent(t *testing.T) {
	d := NewFocusDetector(func() FocusStatus {
		return FocusStatus{Granted: false, Enabled: true} // Enabled 就算 true 也不生效
	})
	if d.Detect() != ReasonNone {
		t.Error("未授权时应返回 ReasonNone")
	}
}

// TestFocusDetectorSince 验证专注开启的时刻被记为 since。
func TestFocusDetectorSince(t *testing.T) {
	base := time.Date(2026, 10, 20, 14, 0, 0, 0, time.UTC)
	seq := []time.Time{base, base.Add(3 * time.Second)}
	i := 0
	var enabled bool
	d := NewFocusDetector(func() FocusStatus { return FocusStatus{Granted: true, Enabled: enabled} })
	d.now = func() time.Time {
		t := seq[i]
		if i < len(seq)-1 {
			i++
		}
		return t
	}

	enabled = true
	d.Detect()
	if s := d.Since(); !s.Equal(base) {
		t.Errorf("首次开启应记 base，实际 %v", s)
	}
	// 保持开启：since 不变。
	d.Detect()
	if s := d.Since(); !s.Equal(base) {
		t.Errorf("持续开启时 since 不应变化")
	}
	// 关闭：归零。
	enabled = false
	d.Detect()
	if !d.Since().IsZero() {
		t.Errorf("关闭后 since 应归零")
	}
}

// TestFocusPriorityOverMeeting 验证优先级：专注 > 会议。
//
// 【为什么】系统专注是用户在 OS 层【明确声明】「不要被打扰」，语义比
// 「前台是 Zoom」更权威；两者同时成立时应该展示「专注模式」这个更硬的原因。
func TestFocusPriorityOverMeeting(t *testing.T) {
	focus := NewFocusDetector(func() FocusStatus { return FocusStatus{Granted: true, Enabled: true} })
	meeting := NewMeetingDetector(func() string { return "us.zoom.xos" })
	provider := NewCompositeProvider(focus, meeting)
	if s := provider.State(); s.Reason != ReasonFocus {
		t.Errorf("专注应优先于会议，实际 %s", s.Reason)
	}
}

// TestFocusDetectorConcurrentAccess 验证并发安全。
func TestFocusDetectorConcurrentAccess(t *testing.T) {
	var mu sync.Mutex
	enabled := false
	d := NewFocusDetector(func() FocusStatus {
		mu.Lock()
		defer mu.Unlock()
		return FocusStatus{Granted: true, Enabled: enabled}
	})
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			mu.Lock()
			enabled = i%2 == 0
			mu.Unlock()
		}(i)
		go func() {
			defer wg.Done()
			_ = d.Detect()
			_ = d.Since()
			_ = d.State()
		}()
	}
	wg.Wait()
}
