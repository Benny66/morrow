package pause

import (
	"sync"
	"time"
)

// PresentationDetector 是「用户主动进入演示模式」的暂停信号。
//
// 它与自动检测器不同：状态由 App 在用户操作时写入，而不是轮询系统。
// 它不读取屏幕内容/窗口标题，也不依赖平台能力；捕获排除是独立的窗口能力。
type PresentationDetector struct {
	mu     sync.Mutex
	active bool
	since  time.Time
	// now 可在测试中替换，避免依赖真实时钟。
	now func() time.Time
}

// NewPresentationDetector 创建一个未激活的演示信号。
func NewPresentationDetector() *PresentationDetector {
	return &PresentationDetector{now: time.Now}
}

// SetActive 切换演示状态。重复设置同一值幂等：
// 已激活时保留首次 since，避免状态刷新把暂停时长重置。
func (d *PresentationDetector) SetActive(active bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if active {
		if !d.active {
			d.active = true
			d.since = d.now()
		}
		return
	}
	d.active = false
	d.since = time.Time{}
}

// Detect 实现 Detector。
func (d *PresentationDetector) Detect() PauseReason {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.active {
		return ReasonPresentation
	}
	return ReasonNone
}

// Since 实现 Detector。
func (d *PresentationDetector) Since() time.Time {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.since
}
