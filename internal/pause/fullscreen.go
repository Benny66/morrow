package pause

import (
	"sync"
	"time"
)

// Rect 是窗口/显示器的几何矩形（左上角坐标 + 尺寸）。
//
// 【为什么单独定义而不是复用 platform 层类型】macOS 用 CGRect、Windows 用 RECT，
// 两者的字段名和坐标系都不一样；detector 只关心「是不是铺满」，用一个扁平
// 结构体收口，平台层负责翻译。
type Rect struct {
	X, Y   int
	Width  int
	Height int
}

// fullscreenAreaRatio 是判定「铺满」的面积比阈值（0.95 = 95%）。
//
// 【为什么允许 5% 误差】部分应用（Keynote 演示、Chrome 视频全屏）会保留
// 1–2px 边框避免与相邻窗口黏在一起；严格 100% 会漏掉这类真实全屏。
// 【为什么阈值不能再低】低于 95% 时，一个最大化但仍可见菜单栏/Dock 的窗口
// （通常占屏 ~93%）也会被误判为全屏。95% 是这两者之间比较稳的分界。
const fullscreenAreaRatio = 0.95

// IsFullscreenRect 判断窗口 rect 是否覆盖 screen rect。
//
// 判定：面积 ≥ 95% 且宽高各自 ≥ 90%。
//
// 【为什么还要判宽高】面积大不代表形状对：一个 800×2000 的窗口面积可以逼近 1920×1080，
// 但它根本不是「全屏」形状；宽高各自卡阈值把这类瘦长窗口排除掉。
//
// 【为什么用宽高阈值 90% 不用 95%】菜单栏通常 24–25px 高（1080 屏上 ≈2%），
// Dock 通常 70px（≈6%）；90% 允许这些装饰条被让出来。
func IsFullscreenRect(win, screen Rect) bool {
	if win.Width <= 0 || win.Height <= 0 {
		return false
	}
	if screen.Width <= 0 || screen.Height <= 0 {
		return false
	}
	wRatio := float64(win.Width) / float64(screen.Width)
	hRatio := float64(win.Height) / float64(screen.Height)
	if wRatio < 0.9 || hRatio < 0.9 {
		return false
	}
	areaRatio := (float64(win.Width) * float64(win.Height)) / (float64(screen.Width) * float64(screen.Height))
	return areaRatio >= fullscreenAreaRatio
}

// FrontWindowInfo 是「当前前台窗口」的平台无关视图。
//
// 【为什么带 Permitted】macOS 上读窗口几何可能需要屏幕录制权限；无权限时
// 平台层应返回 Permitted=false，detector 直接降级而不是把 (0,0,0,0) 当成
// 「小窗口」误判（那样会一直不暂停，用户看不到任何提示）。
type FrontWindowInfo struct {
	// Window 是前台窗口的 rect（屏幕全局坐标）。
	Window Rect
	// Screen 是窗口【所在】显示器的 rect（多屏时不是主屏）。
	Screen Rect
	// Permitted 是读取窗口几何是否有权限；平台不需要权限时恒为 true。
	Permitted bool
	// PermissionDeniedOnce 提示上层「这次是因为权限缺失读不到」，
	// 让 UI 层可以弹一次申请窗（之后靠 ScreenRecordingPermissionRequested 记住用户已选）。
	PermissionDeniedOnce bool
}

// FullscreenDetector 是前台窗口全屏检测器。
//
// 平台层通过 getFront 回调返回 FrontWindowInfo：
//   - macOS：CGWindowListCopyWindowInfo 读几何（可能需要屏幕录制权限）
//   - Windows：GetForegroundWindow + GetWindowRect + MonitorFromWindow
//
// 检测逻辑：窗口 rect 是否覆盖它所在显示器的可用区域。
type FullscreenDetector struct {
	getFront func() FrontWindowInfo

	mu    sync.Mutex
	inHit bool
	since time.Time
	now   func() time.Time
}

// NewFullscreenDetector 构造全屏检测器。
func NewFullscreenDetector(getFront func() FrontWindowInfo) *FullscreenDetector {
	return &FullscreenDetector{
		getFront: getFront,
		now:      time.Now,
	}
}

// Detect 返回当前全屏状态对应的暂停原因。
//
// 权限被拒 / 无前台窗口 / 判定非全屏 → ReasonNone。
//
// 【为什么权限缺失时不暂停】「暂停投递」的正当性来自「用户正在做不能被
// 打断的事」；没有窗口几何就读不到用户在做什么，硬暂停会把「没授权」变成
// 「没提醒」——比未开启此功能还糟糕。降级为「当作不知道」是更诚实的做法。
func (d *FullscreenDetector) Detect() PauseReason {
	if d.getFront == nil {
		return ReasonNone
	}
	info := d.getFront()
	hit := info.Permitted && IsFullscreenRect(info.Window, info.Screen)

	d.mu.Lock()
	defer d.mu.Unlock()
	if hit && !d.inHit {
		d.inHit = true
		d.since = d.now()
	} else if !hit && d.inHit {
		d.inHit = false
		d.since = time.Time{}
	}
	if hit {
		return ReasonFullscreen
	}
	return ReasonNone
}

// Since 返回进入全屏状态的时刻；未触发时为零值。
func (d *FullscreenDetector) Since() time.Time {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.since
}

// State 让 FullscreenDetector 满足 Provider 接口。
func (d *FullscreenDetector) State() State {
	if d.Detect() == ReasonNone {
		return State{Reason: ReasonNone}
	}
	return State{Reason: ReasonFullscreen, Since: d.Since()}
}
