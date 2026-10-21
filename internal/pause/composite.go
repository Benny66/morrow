package pause

import (
	"sort"
	"time"
)

// Detector 是单个暂停信号的抽象（IdleDetector / LockDetector 都满足）。
//
// 【为什么不直接依赖 Provider 接口】Provider.State() 返回 State（含 since），
// 而 Detector 只需要按「当前是否触发 + 触发原因」说话；since 由 CompositeProvider
// 自己按优先级拼装即可，各 Detector 无需重复维护这个字段。
type Detector interface {
	// Detect 返回当前触发的暂停原因；未触发时返回 ReasonNone。
	Detect() PauseReason
	// Since 返回该信号进入触发状态的时刻；未触发时为零值。
	Since() time.Time
}

// reasonPriority 定义暂停原因的展示优先级（数字越小越优先）。
//
// 【为什么要有优先级】同一时刻可能多个信号并存（用户锁屏 + 系统判定空闲），
// 单一 reason 只能选一个。选择原则是「越具体的信号越优先」：
//   - 演示模式：用户主动进入，最高
//   - 锁屏/休眠：人明确不在电脑前，最可信
//   - 专注/日历：系统或日程层面已声明「不要打扰」
//   - 会议应用：具体应用在前台，可信度高但可能误判
//   - 全屏：窗口状态，最模糊（在看视频？在做演示？在工作？）
//   - 空闲：只是键鼠没动，最弱
var reasonPriority = map[PauseReason]int{
	ReasonPresentation: -1,
	ReasonLock:         0,
	ReasonFocus:        1,
	ReasonCalendar:     2,
	ReasonMeeting:      3,
	ReasonFullscreen:   4,
	ReasonIdle:         5,
}

// CompositeProvider 聚合多个 Detector，输出单一 State。
//
// 【它是什么】智能免打扰的「决策层」：把空闲/锁屏/会议/全屏/专注/日历的信号
// 汇成一个原因，供调度引擎和 UI 消费。
//
// 【为什么不做 goroutine / 不做订阅】所有 Detector 都是被动查询：状态改变时
// 平台观察者写入 Detector，引擎在 tick 里查询 Composite。没有事件循环，也就
// 没有生命周期管理要处理。
type CompositeProvider struct {
	detectors []Detector
}

// NewCompositeProvider 构造聚合提供者。detectors 的顺序不影响结果（内部按优先级排序）。
func NewCompositeProvider(detectors ...Detector) *CompositeProvider {
	return &CompositeProvider{detectors: detectors}
}

// State 返回当前最高优先级的暂停信号。
//
// 无信号触发时返回 ReasonNone（未暂停）。
func (c *CompositeProvider) State() State {
	var bestPause PauseReason = ReasonNone
	var bestSince time.Time
	for _, d := range c.detectors {
		r := d.Detect()
		if r == ReasonNone {
			continue
		}
		if bestPause == ReasonNone || reasonPriority[r] < reasonPriority[bestPause] {
			bestPause = r
			bestSince = d.Since()
		}
	}
	if bestPause == ReasonNone {
		return State{Reason: ReasonNone}
	}
	return State{Reason: bestPause, Since: bestSince}
}

// ActiveReasons 返回当前所有触发的原因（按优先级排序），供调试与 UI 展示。
//
// 【为什么导出】菜单/日志可能需要「同时锁屏 + 空闲」这类组合信息；只导出 State
// 会丢掉次要信号。目前只有菜单栏会用到，但成本极低（每次遍历一次 detectors）。
func (c *CompositeProvider) ActiveReasons() []PauseReason {
	out := make([]PauseReason, 0, len(c.detectors))
	for _, d := range c.detectors {
		r := d.Detect()
		if r != ReasonNone {
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return reasonPriority[out[i]] < reasonPriority[out[j]]
	})
	return out
}

// IdleDetector 现在也满足 Detector 接口（补上 Since 方法）。
//
// 【为什么在这里加而不是挪到 provider.go】IdleDetector 的 since 语义是「首次
// 越过阈值的时刻」；由于空闲时长本身就是连续查询的结果，最简单的做法是
// 「首次 Detect=Idle 时记一次，之后不重置」。挪到 provider.go 会破坏 provider.go
// 「只放共享类型」的定位。
