package pause

import (
	"sync"
	"time"
)

// Config 是构造 CompositeProvider 所需的参数。
//
// 【为什么单独一个 Config 结构体而不是散参数】各 Detector 的开关与阈值来自 config.json；
// 用一个结构体收口，调用方只需要 cfg → Config 的一次转换，未来加 detector 不用改签名。
type Config struct {
	// IdleEnabled 是否启用空闲检测。
	IdleEnabled bool
	// IdleThreshold 空闲阈值（默认 5 分钟，由调用方夹取）。
	IdleThreshold time.Duration

	// LockEnabled 是否启用锁屏/休眠检测。
	LockEnabled bool

	// MeetingEnabled 是否启用会议应用前台检测。
	MeetingEnabled bool

	// FullscreenEnabled 是否启用全屏窗口检测。
	FullscreenEnabled bool

	// FocusEnabled 是否启用系统专注联动（macOS INFocusStatusCenter）。
	FocusEnabled bool
	// CalendarEnabled 是否启用日历忙闲感知（macOS EventKit）。
	CalendarEnabled bool
}

// Bundle 是一次装配的产物：CompositeProvider + 关闭函数。
type Bundle struct {
	// Provider 是给引擎和 UI 用的暂停信号聚合器。
	Provider *CompositeProvider
	// Lock 是锁屏检测器（可能被外部事件源驱动，测试时可直接调 Set 方法）。
	Lock *LockDetector
	// Meeting 是会议检测器；测试与诊断入口。
	Meeting *MeetingDetector
	// Fullscreen 是全屏检测器；测试与诊断入口。
	Fullscreen *FullscreenDetector
	// Focus 是系统专注检测器；测试与诊断入口。
	Focus *FocusDetector
	// Calendar 是日历忙闲检测器；测试与诊断入口。
	Calendar *CalendarDetector
	// Presentation 是用户主动进入演示模式的信号；始终存在，即使其它信号全关。
	Presentation *PresentationDetector
	// Stop 释放后台监听资源；调用一次后不应再调。
	Stop func()
}

// NewBundle 按配置装配暂停信号，并启动平台层的锁屏监听。
//
// 返回的 Bundle.Stop 需要在应用退出前调用；否则 goroutine 会一直轮询。
// cfg 里所有 Enabled 都为 false 时，仍返回一个可用 provider：自动信号全部不触发，
// 但用户主动进入演示模式时仍会暂停。
func NewBundle(cfg Config) (*Bundle, error) {
	lock := NewLockDetector()
	meeting := NewMeetingDetector(GetFrontmostApp)
	fullscreen := NewFullscreenDetector(GetFrontWindowInfo)
	focus := NewFocusDetector(GetFocusStatus)
	calendar := NewCalendarDetector(GetCalendarStatus)
	presentation := NewPresentationDetector()
	detectors := []Detector{presentation}

	if cfg.IdleEnabled {
		idle := NewIdleDetector(GetIdleDuration, cfg.IdleThreshold)
		detectors = append(detectors, idle)
	}
	if cfg.LockEnabled {
		detectors = append(detectors, lock)
	}
	if cfg.MeetingEnabled {
		detectors = append(detectors, meeting)
	}
	if cfg.FullscreenEnabled {
		detectors = append(detectors, fullscreen)
	}
	if cfg.FocusEnabled {
		detectors = append(detectors, focus)
	}
	if cfg.CalendarEnabled {
		detectors = append(detectors, calendar)
	}

	provider := NewCompositeProvider(detectors...)
	var stopLockWatcher func()
	if cfg.LockEnabled {
		stopLockWatcher = startLockWatcher(lock)
	}
	var stopMeetingWatcher func()
	if cfg.MeetingEnabled {
		stopMeetingWatcher = startMeetingWatcher(meeting)
	}
	var stopFullscreenWatcher func()
	if cfg.FullscreenEnabled {
		stopFullscreenWatcher = startFullscreenWatcher(fullscreen)
	}
	// 专注与日历都是【事件驱动】型：macOS 系统弹一下就会变；2 秒轮询够快，
	// 且比 1 秒更省——这两个 API 每次都走 IPC，不像 idle/lock 是纯本地。
	var stopFocusWatcher func()
	if cfg.FocusEnabled {
		stopFocusWatcher = startFocusWatcher(focus)
	}
	var stopCalendarWatcher func()
	if cfg.CalendarEnabled {
		stopCalendarWatcher = startCalendarWatcher(calendar)
	}

	// 【Stop 必须幂等】调用方（restartPauseBundle + ServiceShutdown）可能在
	// 竞态下重复调；裸 close(stop) 第二次会 panic: close of closed channel。
	// sync.Once 是最便宜的兜底。
	var stopOnce sync.Once
	return &Bundle{
		Provider:     provider,
		Lock:         lock,
		Meeting:      meeting,
		Fullscreen:   fullscreen,
		Focus:        focus,
		Calendar:     calendar,
		Presentation: presentation,
		Stop: func() {
			stopOnce.Do(func() {
				if stopLockWatcher != nil {
					stopLockWatcher()
				}
				if stopMeetingWatcher != nil {
					stopMeetingWatcher()
				}
				if stopFullscreenWatcher != nil {
					stopFullscreenWatcher()
				}
				if stopFocusWatcher != nil {
					stopFocusWatcher()
				}
				if stopCalendarWatcher != nil {
					stopCalendarWatcher()
				}
			})
		},
	}, nil
}

// startFocusWatcher 2 秒轮询专注状态。
func startFocusWatcher(d *FocusDetector) func() {
	return startGenericWatcher(2*time.Second, func() { _ = d.Detect() })
}

// startCalendarWatcher 2 秒轮询日历忙闲。
func startCalendarWatcher(d *CalendarDetector) func() {
	return startGenericWatcher(2*time.Second, func() { _ = d.Detect() })
}

// startGenericWatcher 是"按间隔调 fn"的通用 goroutine。三个 detector 结构一样，
// 用同一个模板避免每加一路信号就多复制一段 select 代码。
func startGenericWatcher(interval time.Duration, fn func()) func() {
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				fn()
			}
		}
	}()
	return func() {
		close(stop)
		<-done
	}
}

// startFullscreenWatcher 1 秒轮询驱动 detector.Detect()，与 startMeetingWatcher 同构。
func startFullscreenWatcher(d *FullscreenDetector) func() {
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				_ = d.Detect()
			}
		}
	}()
	return func() {
		close(stop)
		<-done
	}
}

// startMeetingWatcher 启动会议应用轮询：1 秒一次调用 detector.Detect()，
// 让它维护 since 时间戳。
//
// 【为什么让 watcher 主动驱动 Detect】Detector 的 since 语义依赖「首次进入 → 记录时刻」
// 的状态转换；如果只在引擎 tick 时查，用户从 09:00:00 起开会、09:00:20 才第一次
// tick，就会把 since 记成 09:00:20 而不是真实的开会时刻。轮询保证 1 秒内触发状态转换。
func startMeetingWatcher(d *MeetingDetector) func() {
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				_ = d.Detect()
			}
		}
	}()
	return func() {
		close(stop)
		<-done
	}
}
