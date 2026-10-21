package pause

import (
	"testing"
	"time"
)

// TestPresentationDetector 手动演示开关的状态与 since 语义。
func TestPresentationDetector(t *testing.T) {
	t0 := time.Date(2026, 10, 21, 9, 0, 0, 0, time.Local)
	d := NewPresentationDetector()
	d.now = func() time.Time { return t0 }

	if got := d.Detect(); got != ReasonNone {
		t.Errorf("初始 Detect = %q，期望 %q", got, ReasonNone)
	}

	d.SetActive(true)
	if got := d.Detect(); got != ReasonPresentation {
		t.Errorf("激活后 Detect = %q，期望 %q", got, ReasonPresentation)
	}
	if !d.Since().Equal(t0) {
		t.Errorf("Since = %v，期望 %v", d.Since(), t0)
	}

	// 幂等：重复激活不重置 since。
	d.SetActive(true)
	if !d.Since().Equal(t0) {
		t.Error("重复 SetActive(true) 重置了 Since")
	}

	d.SetActive(false)
	if got := d.Detect(); got != ReasonNone {
		t.Errorf("关闭后 Detect = %q，期望 %q", got, ReasonNone)
	}
	if !d.Since().IsZero() {
		t.Error("关闭后 Since 应归零")
	}
}

// TestPresentationDetectorPriority 演示开关在真实组合器里压过锁屏。
func TestPresentationDetectorPriority(t *testing.T) {
	now := time.Now()
	presentation := NewPresentationDetector()
	presentation.SetActive(true)
	provider := NewCompositeProvider(
		staticDetector{reason: ReasonLock, since: now},
		presentation,
	)
	if got := provider.State().Reason; got != ReasonPresentation {
		t.Errorf("锁屏 + 演示并存时 State().Reason = %q，期望 %q", got, ReasonPresentation)
	}
}
