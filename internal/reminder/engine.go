package reminder

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"morrow/internal/model"
	"morrow/internal/pause"
	"morrow/internal/restmode"
)

// TickInterval 是调度心跳。
//
// 【为什么是 20 秒而不是 1 秒】提醒的语义是分钟级（最小间隔 1 分钟），20 秒的心跳
// 意味着最坏晚 20 秒送达，用户感知不到；换来的是一个稳定的单 goroutine 循环，
// 不必为毫秒精度引入并发与锁。心跳也不要再放大——超过 1 分钟就会出现
// 「整分钟触发的提醒被推到下一分钟」的可见延迟。
const TickInterval = 20 * time.Second

// stopWaitTimeout 是 Stop 等待循环退出的上界。超时只记日志，不阻塞应用退出。
const stopWaitTimeout = 3 * time.Second

// Options 是 Engine 的构造参数（全部为依赖注入，便于测试）。
type Options struct {
	// Store 持久化能力，必填。
	Store Store
	// Settings 设置快照提供者；nil 时按「不静默、不发通知、推迟 5 分钟」处理。
	Settings func() Settings
	// Notifier 系统通知；nil 表示不发通知（仅记录 + 广播事件）。
	Notifier Notifier
	// Emitter 事件出口；nil 表示不广播。
	Emitter Emitter
	// Now 时钟；nil 用 time.Now。测试注入假时钟即可推演跨天/静默/补发。
	Now func() time.Time
	// PauseProvider 暂停信号提供者；nil 表示不启用智能免打扰（始终正常投递）。
	PauseProvider pause.Provider
	// RestMode 休息模式状态机；nil 表示禁用休息模式（所有 kind 走简单提醒）。
	// 只有 stand/move/rest 三类会尝试进入；其余 kind 不受影响（见 dispatch 分支）。
	RestMode *restmode.Machine
}

// Engine 是本地提醒调度引擎。
//
// 并发模型（刻意简单）：
//   - 一个 goroutine + ticker 驱动 Tick，Tick 内全部串行；
//   - 「一次 tick 只补发最近 1 条」这类全局约束因此是可证明的，不需要锁；
//   - mu 只保护生命周期字段与内存备忘，且在 Tick 内部不会跨调用持有。
type Engine struct {
	store         Store
	settings      func() Settings
	notifier      Notifier
	emitter       Emitter
	now           func() time.Time
	pauseProvider pause.Provider
	restMode      *restmode.Machine

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
	// lastDay 记录内存备忘所属的自然日；跨天时清空。
	lastDay string
	// claimed 是「已处理过的计划时间点」的内存备忘。
	//
	// 【它不是真相】真相是 reminder_fires 上的唯一索引，这里只是省掉每个 tick
	// 对同一批历史时间点反复 INSERT 的开销（10 条提醒 × 每小时一次 = 每 tick
	// 上百次注定冲突的写入）。备忘为空时（如刚重启）首轮 tick 会重放当天全部
	// 时间点，其中已处理的会被唯一索引挡下，因此不会重复弹窗。
	claimed map[uint]map[int64]bool
}

// NewEngine 构造调度引擎（不启动）。
func NewEngine(opts Options) *Engine {
	engine := &Engine{
		store:         opts.Store,
		settings:      opts.Settings,
		notifier:      opts.Notifier,
		emitter:       opts.Emitter,
		now:           opts.Now,
		pauseProvider: opts.PauseProvider,
		restMode:      opts.RestMode,
		claimed:       map[uint]map[int64]bool{},
	}
	if engine.now == nil {
		engine.now = time.Now
	}
	if engine.settings == nil {
		engine.settings = func() Settings { return Settings{SnoozeMinutes: 5, NotifyEnabled: false} }
	}
	return engine
}

// Start 启动调度循环（已启动时是幂等的空操作）。
func (e *Engine) Start() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	e.cancel = cancel
	e.done = make(chan struct{})
	go e.loop(ctx, e.done)
}

// Stop 停止调度循环并等待其退出。
//
// 等待有上界：引擎只是在算时间与写库，3 秒足够；超时就放手让应用退出，
// 卡在退出流程里比少停一个 goroutine 更糟（用户看到的是"关不掉"）。
func (e *Engine) Stop() {
	e.mu.Lock()
	cancel, done := e.cancel, e.done
	e.cancel, e.done = nil, nil
	e.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	select {
	case <-done:
	case <-time.After(stopWaitTimeout):
		slog.Warn("提醒引擎未在超时内退出，继续关闭流程")
	}
}

// SwapProvider 原子替换暂停信号提供者，用于设置页保存后让开关立即生效。
//
// 【为什么需要它】Bundle 里的 detector 列表在装配时就按 Enabled 决定；如果只写
// config 不重建，运行期引擎仍按旧列表判定，用户会看到「关了开关但会议中还是暂停」。
// 每次 tick 都读 e.pauseProvider，所以只要在锁里换一下引用，下一轮就生效——
// 无需重启 goroutine、不丢任何待发提醒。
//
// 【nil 传入 = 关闭免打扰】引擎里的判断是 `if e.pauseProvider != nil`，传 nil
// 会让所有信号静默（正常投递），而不是崩。
func (e *Engine) SwapProvider(p pause.Provider) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.pauseProvider = p
}

// loop 是调度循环。
func (e *Engine) loop(ctx context.Context, done chan struct{}) {
	defer close(done)
	// 长驻 goroutine 的 panic 必须兜住：引擎挂掉不会有任何界面提示，
	// 只是提醒从此不再响（正是铁律里说的"没有症状的失效"）。
	// 在这里至少留一条带 panic 值的日志。
	defer func() {
		if r := recover(); r != nil {
			slog.Error("提醒引擎异常退出", "panic", r)
		}
	}()

	// 启动即跑一轮：应用启动（含休眠唤醒后重启）时需要立刻补发错过的提醒，
	// 等 20 秒才跑第一轮会让"打开应用后什么都没发生"成为第一印象。
	e.safeTick()
	ticker := time.NewTicker(TickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.safeTick()
		}
	}
}

// safeTick 包一层 recover，让单轮调度的意外不终止整个循环。
func (e *Engine) safeTick() {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("提醒引擎单轮调度异常，已恢复", "panic", r)
		}
	}()
	e.Tick()
}

// Tick 执行一轮调度。
//
// 导出是为了让测试与"手动触发"能直接驱动它（`reminder:sweep` 之类的调试入口
// 将来可以复用它），不必等 ticker。
func (e *Engine) Tick() {
	if e.store == nil {
		return
	}
	now := e.now()
	e.rollDay(now)
	settings := e.settings()
	quiet := InQuietHours(now, settings.QuietStart, settings.QuietEnd)

	// provider 是可能热替换的（SetPauseSettings 会走 SwapProvider）；
	// 读一次拷进本地变量，之后整轮都用这个快照，避免轮中途被换。
	e.mu.Lock()
	provider := e.pauseProvider
	e.mu.Unlock()
	paused := false
	if provider != nil {
		if state := provider.State(); state.IsPaused() {
			paused = true
			slog.Info("智能免打扰生效", "reason", state.Reason, "since", state.Since)
		}
	}

	if err := e.tickOneOffs(now, settings, paused); err != nil {
		slog.Error("一次性提醒调度失败", "err", err)
	}
	// 到点前 60 秒的休息预警：stand/move/rest 类若下一 fire 落在窗口内，
	// Machine 进入 Warning；Pet.vue 与菜单栏据此显示预警文案。
	// 【暂停期跳过预警】用户此刻在开会/锁屏，一条「1 分钟后休息」气泡没意义。
	if !paused && e.restMode != nil {
		if err := e.tickWarnings(now); err != nil {
			slog.Warn("休息模式预警扫描失败", "err", err)
		}
	}
	if err := e.tickGrid(now, settings, quiet, paused); err != nil {
		slog.Error("提醒调度失败", "err", err)
	}
	if paused {
		// 暂停期间的推迟记录同样不补弹（推迟本身就是「等下再提醒」，
		// 用户此刻正在被暂停的情境里；下一轮恢复后若已过点会按 missed 保留可见）。
		return
	}
	if err := e.tickSnoozes(now, settings, quiet); err != nil {
		slog.Error("推迟提醒补发失败", "err", err)
	}
}

// tickWarnings 扫描当前场景下的 stand/move/rest 提醒；下一条 fire 若落在
// WarningLeadTime 内且尚未预警，就驱动 Machine 进入 Warning。
//
// 【为什么每次 tick 都全量扫】提醒数量是十几条级别，扫一次的开销比一次 SQL
// 还低；做增量缓存反而会增加「提醒项被删/停用后缓存过期」的复杂度。
//
// 【预警发出后提醒项被删怎么办】下一轮扫不到那条了；这里显式 CancelWarning：
// 预警的语义是「马上要 fire 了」，fire 不存在就该撤销，不能让宠物挂着假信号。
func (e *Engine) tickWarnings(now time.Time) error {
	// Countdown / Done / Skipped / EarlyCompleted 状态下不覆盖当前会话。
	// Idle 与 Warning 可以刷新。
	st := e.restMode.State()
	if st != restmode.StateIdle && st != restmode.StateWarning {
		return nil
	}
	scenario, err := e.store.ActiveScenario()
	if err != nil {
		return err
	}
	if scenario == nil {
		e.restMode.CancelWarning()
		return nil
	}
	reminders, err := e.store.ActiveReminders(scenario.ID)
	if err != nil {
		return err
	}
	type candidate struct {
		remID uint
		at    int64
		kind  string
		title string
	}
	var best *candidate
	nowUnix := now.Unix()
	leadSec := int64(restmode.WarningLeadTime / time.Second)
	for _, item := range reminders {
		if !restmode.IsRestKind(item.Kind) {
			continue
		}
		next := NextFireAt(now, item.WindowStart, item.WindowEnd, item.IntervalMin)
		if next == 0 {
			continue
		}
		delta := next - nowUnix
		if delta <= 0 || delta > leadSec {
			continue
		}
		if best == nil || next < best.at {
			best = &candidate{remID: item.ID, at: next, kind: item.Kind, title: item.Title}
		}
	}
	if best == nil {
		// 【到点瞬间的空窗】若当前正在预警、且预警的目标时刻已到（但本轮 tickGrid
		// 还没跑），保留预警状态不撤销——tickGrid 会在同一 tick 里 dispatch 并
		// 走 Enter 把状态推进到 Countdown。撤销会留下 0–20 秒的 Idle 空窗。
		if st == restmode.StateWarning {
			cur := e.restMode.Current()
			if cur.PlannedFireAt > 0 && cur.PlannedFireAt <= nowUnix {
				return nil
			}
		}
		e.restMode.CancelWarning()
		return nil
	}
	// 【去重】同一 kind + 同一目标 fire 时刻不重复预警；否则每次 tick（20s）
	// 都会重置 start，Remaining 就无法稳定递减。
	if cur := e.restMode.Current(); st == restmode.StateWarning &&
		cur.Kind == best.kind && cur.PlannedFireAt == best.at {
		return nil
	}
	e.restMode.EnterWarning(restmode.RestRequest{
		FireID:        best.remID,
		ReminderID:    best.remID,
		ScenarioID:    scenario.ID,
		Kind:          best.kind,
		Title:         best.title,
		Duration:      restmode.CueFor(best.kind).Duration,
		PlannedFireAt: best.at,
	})
	return nil
}

// tickOneOffs 处理全局一次性提醒：到点且未产生 fire 的提醒只处理一次。
//
// 【为什么不依赖启用场景】一次性事件独立于场景；场景停用/切换都不该让它静默。
// 【为什么错过也在这一轮落库】否则它每个 tick 都会被重新捞出，且用户永远看不到
// 「已错过」的痕迹。fire 记录本身就是该提醒已被处理过的标记。
//
// 【paused=true 时的语义】暂停期间到点的一次性提醒直接标 FireMissed，不补弹：
// 用户此刻在会议 / 屏幕锁定 / 空闲里；一条指定时刻的事件如果错过就是错过，
// 恢复后弹一句「刚才 10:30 让你接孩子」已经没意义。保留可见是最低承诺。
func (e *Engine) tickOneOffs(now time.Time, settings Settings, paused bool) error {
	due, err := e.store.DueOneOffs(now.Unix())
	if err != nil {
		return err
	}
	missedAny := false
	for _, item := range due {
		outcome := OneOffOutcome(item.OnceAt, now.Unix(), OneOffGrace)
		if outcome == OneOffNotDue {
			continue
		}
		status := model.FireFired
		if outcome == OneOffMissed || paused {
			status = model.FireMissed
		}
		fireID, claimed, claimErr := e.store.ClaimFireWithStatus(item.ID, item.OnceAt, status)
		if claimErr != nil {
			slog.Error("占用一次性提醒失败", "reminder_id", item.ID, "once_at", item.OnceAt, "err", claimErr)
			continue
		}
		if !claimed {
			continue
		}
		// 暂停期间到点 = 用户看不到；不通知也不广播 fired，只走 missed 通道。
		if outcome == OneOffMissed || paused {
			missedAny = true
			slog.Info("一次性提醒已错过", "reminder", item.Title, "once_at", item.OnceAt, "paused", paused)
			continue
		}
		e.dispatch(DueFire{
			FireID:     fireID,
			ReminderID: item.ID,
			Title:      item.Title,
			Content:    item.Content,
			Kind:       item.Kind,
			IsOneOff:   true,
			FireAt:     item.OnceAt,
		}, now, settings, false)
	}
	// 错过的项没有 fired 事件，但列表状态已经变了；不广播的话首页会一直把
	// 它显示成「即将到来」，直到用户手动刷新或发生别的变更。
	if missedAny && e.emitter != nil {
		e.emitter.EmitChanged(ChangedPayload{Reason: ReasonPlanChanged})
	}
	return nil
}

// tickGrid 处理调度网格：算出今天所有已到点的计划触发点，占用未处理的那几个，
// 其中只送达最近 1 条。
//
// 【paused=true 时】分两类处理，对应 CONTEXT 的两条语义：
//   - 每日单点（IntervalMin=0）：计划时刻已过 → ClaimFireWithStatus(missed) 保留可见，
//     不投递。恢复后它已经落库 missed，不会被重新 claim。
//   - 周期项（IntervalMin>0）：完全跳过 claim，让恢复后的第一个 tick 按原间隔
//     重新计时——「顺延不补发」的下半句。
func (e *Engine) tickGrid(now time.Time, settings Settings, quiet bool, paused bool) error {
	scenario, err := e.store.ActiveScenario()
	if err != nil {
		return err
	}
	// 没有启用场景 = 不该有任何提醒。这是"停用场景后世界立刻安静"的实现方式：
	// 引擎不再看这些提醒项，而残留的待处理卡片仍可由用户处理完。
	if scenario == nil {
		return nil
	}
	reminders, err := e.store.ActiveReminders(scenario.ID)
	if err != nil {
		return err
	}

	for _, item := range reminders {
		points := FirePoints(now, item.WindowStart, item.WindowEnd, item.IntervalMin)
		if len(points) == 0 {
			continue
		}
		// claimedPoint 是"这一轮新占用到的"触发点。
		type claimedPoint struct {
			fireID uint
			fireAt int64
		}
		fresh := make([]claimedPoint, 0, len(points))
		nowUnix := now.Unix()
		isDaily := item.IntervalMin == 0
		for _, point := range points {
			if point > nowUnix {
				break // 未来的点留给后续 tick
			}
			if e.isClaimed(item.ID, point) {
				continue
			}
			// 暂停期间的周期项：claim 为 FireSilent，让它「已处理」，恢复后
			// 下一 tick 不会再看到它；下一个点按原间隔重新计时——「顺延不补发」。
			// 【为什么不能只内存 markClaimed】内存备忘一重启就没了，DB 里这个点
			// 没记录，恢复后第一次 tick 就会把它捞回来 fired——「不补发」当场失效。
			if paused && !isDaily {
				if _, _, claimErr := e.store.ClaimFireWithStatus(item.ID, point, model.FireSilent); claimErr != nil {
					slog.Warn("暂停期占用周期点失败", "reminder_id", item.ID, "fire_at", point, "err", claimErr)
					continue
				}
				e.markClaimed(item.ID, point)
				continue
			}
			initialStatus := model.FireFired
			if paused && isDaily {
				initialStatus = model.FireMissed
			}
			fireID, claimed, claimErr := e.store.ClaimFireWithStatus(item.ID, point, initialStatus)
			if claimErr != nil {
				slog.Error("占用提醒触发点失败", "reminder_id", item.ID, "fire_at", point, "err", claimErr)
				continue
			}
			// 无论是否抢到都记进备忘：抢不到说明这个点历史上已处理过。
			e.markClaimed(item.ID, point)
			if claimed {
				fresh = append(fresh, claimedPoint{fireID: fireID, fireAt: point})
			}
		}
		if len(fresh) == 0 {
			continue
		}

		// 【补发策略】只送达最近 1 条，其余直接判 missed。
		// 这是"合上盖子睡两小时后打开，一次弹十几条通知"的唯一防线。
		// 想改成全部送达之前请先想清楚：用户会因此关掉整类通知。
		latest := fresh[len(fresh)-1]
		for _, stale := range fresh[:len(fresh)-1] {
			if settleErr := e.store.SettleFire(stale.fireID, model.FireMissed, 0); settleErr != nil {
				slog.Warn("标记错过提醒失败", "fire_id", stale.fireID, "err", settleErr)
			}
		}
		// 每日单点在暂停期已经在 claim 时落 missed；不投递、不广播 fired。
		if paused && isDaily {
			continue
		}
		e.dispatch(DueFire{
			FireID:       latest.fireID,
			ReminderID:   item.ID,
			ScenarioID:   scenario.ID,
			ScenarioName: scenario.Name,
			Title:        item.Title,
			Content:      item.Content,
			Kind:         item.Kind,
			FireAt:       latest.fireAt,
		}, now, settings, quiet)
	}
	return nil
}

// tickSnoozes 补发到点的推迟记录。
//
// 推迟没有走内存定时器，而是落成一条 fire_at = 现在 + N 分钟的记录：
// 这样"推迟后关掉应用再打开"仍然是推迟状态，内存定时器做不到这一点。
func (e *Engine) tickSnoozes(now time.Time, settings Settings, quiet bool) error {
	due, err := e.store.DueSnoozeFires(now.Unix())
	if err != nil {
		return err
	}
	for _, fire := range due {
		e.dispatch(fire, now, settings, quiet)
	}
	return nil
}

// dispatch 送达一条提醒：落状态 → 发通知 → 广播事件。
func (e *Engine) dispatch(fire DueFire, now time.Time, settings Settings, quiet bool) {
	// 静默时段：照常记录（统计里能看到这段时间确实有过提醒），但不打扰。
	// 一次性提醒例外：用户明确指定了时刻，静默设置不应把它吞掉。
	// 顺序仍是"先落库再判断"，因为 silent 本身也是一个要落库的终态。
	if quiet && !fire.IsOneOff {
		if err := e.store.SettleFire(fire.FireID, model.FireSilent, now.Unix()); err != nil {
			slog.Warn("记录静默提醒失败", "fire_id", fire.FireID, "err", err)
			return
		}
		slog.Info("静默时段，仅记录不通知",
			"reminder", fire.Title, "fire_at", fire.FireAt)
		return
	}

	// 【先落 fired 再发通知，顺序不能反】通知可能因为未打包/未授权而失败，
	// 但这条提醒对用户仍然有效——首页的待处理卡片才是可靠的交付通道。
	if err := e.store.SettleFire(fire.FireID, model.FireFired, now.Unix()); err != nil {
		slog.Error("记录提醒触发失败", "fire_id", fire.FireID, "err", err)
		return
	}

	// 尝试进入休息模式（stand/move/rest 三类）。返回 true 表示已进入。
	//
	// 【为什么在进入成功后【跳过系统通知】】休息模式覆盖层本身就是「必须处理才消失」
	// 的强提醒；再叠一条系统通知只会让用户在同一秒看到两件事，与「陪伴」定位相悖。
	// 【为什么 Enter 失败时继续走通知】上一条还没结束（比如机器正在 stand 中），
	// 这条 move 就该走「普通提醒 + 待处理卡片」通道——用户可以看到有第二条在等，
	// 而不是让这条彻底静默丢失。
	inRestMode := false
	if e.restMode != nil && restmode.IsRestKind(fire.Kind) {
		dur := restmode.CueFor(fire.Kind).Duration
		inRestMode = e.restMode.Enter(restmode.RestRequest{
			FireID:     fire.FireID,
			ReminderID: fire.ReminderID,
			ScenarioID: fire.ScenarioID,
			Kind:       fire.Kind,
			Title:      fire.Title,
			Duration:   dur,
		})
	}

	if !inRestMode && settings.NotifyEnabled && e.notifier != nil {
		notification := Notification{
			ID:       fire.NotificationID(),
			FireID:   fire.FireID,
			Title:    fire.NotificationTitle(),
			Subtitle: fire.ScenarioName,
			Body:     fire.Content,
		}
		if err := e.notifier.Notify(notification); err != nil {
			// 不中断：卡片通道照常工作。日志里留痕，便于排查通知权限问题。
			slog.Warn("发送系统通知失败", "reminder", fire.Title, "err", err)
		}
	}

	if e.emitter != nil {
		e.emitter.EmitFired(FiredPayload{
			FireID:     fire.FireID,
			ReminderID: fire.ReminderID,
			ScenarioID: fire.ScenarioID,
			Title:      fire.Title,
			Content:    fire.Content,
			Kind:       fire.Kind,
			IsOneOff:   fire.IsOneOff,
			FireAt:     fire.FireAt,
		})
	}
	slog.Info("提醒已送达", "reminder", fire.Title, "fire_at", fire.FireAt, "rest_mode", inRestMode)
}

// rollDay 在跨自然日时清空内存备忘。
func (e *Engine) rollDay(now time.Time) {
	key := now.Format(model.DateLayout)
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.lastDay == key {
		return
	}
	e.lastDay = key
	e.claimed = map[uint]map[int64]bool{}
}

// isClaimed 查询内存备忘。
func (e *Engine) isClaimed(reminderID uint, fireAt int64) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.claimed[reminderID][fireAt]
}

// markClaimed 写入内存备忘。
func (e *Engine) markClaimed(reminderID uint, fireAt int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	day, ok := e.claimed[reminderID]
	if !ok {
		day = map[int64]bool{}
		e.claimed[reminderID] = day
	}
	day[fireAt] = true
}
