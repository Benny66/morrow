// Package restmode 是场景化休息模式的状态机与内置引导内容。
//
// 与 reminder 引擎的关系：引擎按 kind 分流——water/toilet/custom 走原有的
// 「弹窗 + 待处理卡片」通道；stand/move/rest 三种进入本包管理的休息模式，
// 覆盖层倒计时 + 一条与类型匹配的引导动作；结束（自然/主动）后写 CheckIn。
//
// 【为什么要单独一个包而不是塞进 engine】休息模式有自己的时间维度（几十秒到几分钟），
// 与提醒的分钟级 tick 不在一个粒度；状态转换由前端触发（进入 / 完成 / 跳过），
// 不是调度器推的。分开之后引擎只关心「该不该弹、什么时候弹」，本包只关心
// 「弹出来之后那 2 分钟里用户和它在做什么」。
//
// 【隐私承诺】休息模式的内容全部内置（见 cue.go），不调 AI、不联网、离线可用；
// 见 CONTEXT.md 的「引导动作」。
package restmode

import (
	"sync"
	"time"
)

// State 是休息模式的状态枚举（单一真相）。
//
// 前端 lib/restmode.ts 有一份镜像；guard/parity_test.go 会双向对账。
type State string

const (
	// StateIdle 未在休息。
	StateIdle State = "idle"
	// StateWarning 到点前 60 秒的预警（宠物气泡 + 菜单栏文案）。
	// 【与 Countdown 分开】预警只【告知】，不打断用户；到点才真的进入 Cover。
	// 【存活多久由引擎判据决定】判据是「它预告的那次触发是否仍待投递」，
	// 见 internal/reminder/engine.go 的 warningStillPending 与 ADR 0012；
	// 本包只负责"处于预警时该说什么"，不决定它能挂多久。
	StateWarning State = "warning"
	// StateCountdown 倒计时进行中。
	StateCountdown State = "countdown"
	// StateDone 倒计时自然结束；已由服务端记完成。
	StateDone State = "done"
	// StateSkipped 用户在倒计时中主动跳过（点「提前结束」→「不算」）。
	StateSkipped State = "skipped"
	// StateEarlyCompleted 用户在倒计时中主动结束（点「提前结束」→「算完成」）。
	// 【为什么与 Done 分开】两者的动作日志与后续提醒节奏不同：Done 是「做完了」，
	// EarlyCompleted 是「没做完但用户觉得够了」。合并会丢掉这条产品洞察。
	StateEarlyCompleted State = "early_completed"
)

// WarningLeadTime 是 fire 前多久发出预警的提前量（US-9 明确 60 秒）。
//
// 【为什么做成导出的常量】引擎扫预警、UI 渲染「N 秒后开始」文案都依赖这个数字；
// 一处真相避免"引擎按 60 秒触发、UI 写死 30 秒"的漂移。
// 【与措辞分档的关系】它只是预警的**起点**；预警能挂多久由引擎的存活判据决定
// （internal/reminder/engine.go 的 warningStillPending + ADR 0012），
// 本包只负责"处于预警时该说什么"。三档下界见 warningCueHalf / warningCueSoon。
const WarningLeadTime = 60 * time.Second

// RestRequest 是一次进入休息模式的请求。
//
// 【为什么由引擎而非前端触发 Enter】到点是引擎知道的；用户"应该进入休息模式"
// 这个决策必须发生在服务端，前端只是渲染。这样即使前端窗口还没打开、
// 提醒事件已经在服务端记了 FireFired，用户下次开窗口看到的还是完整的休息流程。
type RestRequest struct {
	// FireID 关联的 reminder_fires 记录 ID；完成/跳过都写回这一条。
	// 【Countdown 阶段是真实 fireID；Warning 阶段还没落 fire，这里存的是 reminderID 兜底】——
	// 所以预警阶段的存活判据不要读它，用 ReminderID + PlannedFireAt
	// （见 internal/reminder/engine.go 的 warningStillPending）。
	FireID uint
	// ReminderID 提醒项 ID（写入 CheckIn 需要）。
	ReminderID uint
	// ScenarioID 场景 ID（写入 CheckIn 需要）。
	ScenarioID uint
	// Kind 决定引导动作与默认时长：stand/move/rest。
	Kind string
	// Title 提醒标题（覆盖层顶部显示）。
	Title string
	// Duration 倒计时总时长。
	Duration time.Duration
	// PlannedFireAt Warning 阶段记录计划 fire 的 Unix 秒；用于「到点但 tick
	// 尚未 dispatch」的窗口内不误撤销预警。Countdown 阶段可留零。
	PlannedFireAt int64
}

// Snapshot 是给定时刻休息模式的可绑定视图。
//
// 【为什么不直接暴露 *Machine】Machine 内部有 mutex 与可变字段；JSON 序列化
// 会把 sync.Mutex 的 lock 状态也暴露出来（其实 sync.Mutex 不实现 MarshalJSON，
// 会走 struct tag 递归，最终 panic 或字段丢失），也不利于将来重构成别的实现。
type Snapshot struct {
	// State 当前状态。
	State State `json:"state"`
	// FireID 关联的 fire（Idle 时为 0）。
	FireID uint `json:"fire_id"`
	// ReminderID 关联的提醒项。
	ReminderID uint `json:"reminder_id"`
	// ScenarioID 关联的场景。
	ScenarioID uint `json:"scenario_id"`
	// Kind stand/move/rest。
	Kind string `json:"kind"`
	// Title 提醒标题。
	Title string `json:"title"`
	// CueText 内置引导动作文案（Idle 时为空）。
	CueText string `json:"cue_text"`
	// TotalSec 倒计时总秒数。
	TotalSec int `json:"total_sec"`
	// RemainingSec 剩余秒数（夹到 0 以上）。
	RemainingSec int `json:"remaining_sec"`
}

// Machine 是休息模式状态机。
//
// 并发模型：所有导出方法都持锁；引擎与绑定方法可以从不同 goroutine 调用。
type Machine struct {
	mu    sync.Mutex
	state State
	cur   RestRequest
	start time.Time // 进入 Countdown 的时刻
	// now 是业务时钟；测试注入假时钟即可推演 Remaining。
	now func() time.Time
}

// NewMachine 构造状态机（初始 Idle）。
func NewMachine() *Machine {
	return &Machine{state: StateIdle, now: time.Now}
}

// NewMachineWithClock 构造一个使用指定时钟的状态机（测试与组合根可用）。
//
// 【为什么要导出】reminder 包的集成测试需要把「引擎时钟」与「状态机时钟」
// 对齐到同一个假时刻；否则 Snapshot.RemainingSec 会用真实时间算出一个
// 越界值（0）。生产路径用 NewMachine 保持 time.Now 就够了。
func NewMachineWithClock(now func() time.Time) *Machine {
	if now == nil {
		now = time.Now
	}
	return &Machine{state: StateIdle, now: now}
}

// State 返回当前状态。
func (m *Machine) State() State {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

// Enter 尝试进入休息模式；返回 true 表示进入成功，false 表示已有会话在跑。
//
// 【Idle 与 Warning 都接受】Warning 阶段到点后引擎会调 Enter 把状态推进到
// Countdown；Idle 直接进 Countdown 也允许（引擎 tick 恰好错过预警窗口）。
// 其他状态（Countdown/Done/Skipped/EarlyCompleted）拒绝。
//
// 【拒绝覆盖的原因见测试】休息中一条新到点的提醒不该打断当前会话——
// 那会让前一条 FireID 停在 fired 状态没人处理，用户下一次 tick 就能看到两张待处理卡片。
// 引擎在收到 false 时会把新到点当普通提醒处理（弹窗 + 待处理），保持旧行为。
func (m *Machine) Enter(req RestRequest) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state != StateIdle && m.state != StateWarning {
		return false
	}
	m.cur = req
	m.start = m.now()
	m.state = StateCountdown
	return true
}

// EnterWarning 尝试进入预警状态；只在 Idle 下成功。
//
// 【为什么 Idle-only】Countdown 中不接受新预警（用户此刻正被覆盖层锁住，
// 一条未来 30 秒后要开始的休息来了也只让他更焦虑）；Done/Skipped 短暂存在，
// 下一 tick 就归零，不需要单独处理。
func (m *Machine) EnterWarning(req RestRequest) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state != StateIdle {
		return false
	}
	m.cur = req
	m.start = m.now()
	m.state = StateWarning
	return true
}

// CancelWarning 撤销当前预警（Idle 化）；返回是否撤消成功。
//
// 【为什么要能撤销】如果提醒项在预警发出后被删除/停用（用户在设置里关掉 stand），
// 下一轮 tick 就不会有对应的 Enter 调用；不撤销的话宠物会永远挂着「1 分钟后休息」
// 的假信号——US-9 的语义就断了。
func (m *Machine) CancelWarning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state != StateWarning {
		return false
	}
	m.state = StateIdle
	m.cur = RestRequest{}
	m.start = time.Time{}
	return true
}

// Complete 记「倒计时自然结束」；从 Countdown 转到 Done。
//
// 【非 Countdown 时是 no-op】见测试用例 TestMachineCompleteAfterIdleIsNoop；
// 迟到的 Complete 事件不应把 Idle 或 Skipped 改成 Done。
// 返回值：true = 状态确实转到了 Done；false = 当前不在 Countdown，未改状态。
func (m *Machine) Complete() bool {
	return m.transition(StateCountdown, StateDone)
}

// EarlyComplete 用户主动点「提前结束」→「算完成」；从 Countdown 转到 EarlyCompleted。
func (m *Machine) EarlyComplete() bool {
	return m.transition(StateCountdown, StateEarlyCompleted)
}

// Skip 用户主动点「提前结束」→「不算」；从 Countdown 转到 Skipped。
func (m *Machine) Skip() bool {
	return m.transition(StateCountdown, StateSkipped)
}

// transition 是状态机的通用换态：仅当当前 == from 时才切到 to，返回是否切成。
func (m *Machine) transition(from, to State) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state != from {
		return false
	}
	m.state = to
	return true
}

// Reset 把非 Idle 的状态清回 Idle（在服务端已经消费掉 Done/Skipped 之后调用）。
//
// 【为什么不自动 Reset】绑定方法 CompleteRestMode 需要「先看到 Done 状态、
// 再落 CheckIn、最后 Reset」——三段分开才能保证落库失败时状态还在 Done，
// 用户可以重试；一失败就自动回 Idle 会让用户什么都看不到。
func (m *Machine) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = StateIdle
	m.cur = RestRequest{}
	m.start = time.Time{}
}

// Current 返回当前会话的请求（Idle 时为零值）。
func (m *Machine) Current() RestRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cur
}

// Remaining 返回当前剩余倒计时；非 Countdown / Warning 状态返回 0。
//
// 【两种状态的 Remaining 语义不同】
//   - Countdown：距离休息结束还有多久（用于覆盖层 mm:ss）；
//   - Warning：距离 fire 还有多久（WarningLeadTime - elapsed），用于气泡与菜单栏。
//
// 超期后夹到 0（不返回负数），前端直接渲染成 "0:00" 即可。
func (m *Machine) Remaining() time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.remainingLocked()
}

// remainingLocked 是 Remaining 的无锁内部实现；调用方必须持有 m.mu。
func (m *Machine) remainingLocked() time.Duration {
	switch m.state {
	case StateCountdown:
		left := m.cur.Duration - m.now().Sub(m.start)
		if left < 0 {
			return 0
		}
		return left
	case StateWarning:
		// Warning 的 Remaining = 距离真实 fire 还剩多久。
		// 【为什么要求 PlannedFireAt > 0】引擎的 tickWarnings 恒设这个字段；
		// 缺了就当作非法调用（老代码/测试忘设），返回 0 让 UI 显示「即将开始」，
		// 而不是按 start 时刻反推一个可能错的 60 秒。
		if m.cur.PlannedFireAt <= 0 {
			return 0
		}
		leftSec := m.cur.PlannedFireAt - m.now().Unix()
		if leftSec <= 0 {
			return 0
		}
		return time.Duration(leftSec) * time.Second
	}
	return 0
}

// Snapshot 返回当前状态的深拷贝（JSON 安全）。
func (m *Machine) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	snap := Snapshot{State: m.state}
	if m.state == StateIdle {
		return snap
	}
	snap.FireID = m.cur.FireID
	snap.ReminderID = m.cur.ReminderID
	snap.ScenarioID = m.cur.ScenarioID
	snap.Kind = m.cur.Kind
	snap.Title = m.cur.Title
	// 【先算剩余再算文案】预警的措辞按剩余时间分档，顺序写反就会用零值剩余去选文案
	// ——那是"到点却说 1 分钟后"的另一半来源，而且不会有任何报错。
	remaining := m.remainingLocked()
	if m.state == StateWarning {
		// 预警阶段用「N 秒后开始」的文案，而不是休息引导动作。
		snap.CueText = warningCueFor(m.cur.Kind, remaining)
		snap.TotalSec = int(WarningLeadTime / time.Second)
	} else {
		snap.CueText = CueFor(m.cur.Kind).Text
		snap.TotalSec = int(m.cur.Duration / time.Second)
	}
	snap.RemainingSec = int(remaining / time.Second)
	return snap
}
