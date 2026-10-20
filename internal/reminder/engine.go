package reminder

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"morrow/internal/model"
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
}

// Engine 是本地提醒调度引擎。
//
// 并发模型（刻意简单）：
//   - 一个 goroutine + ticker 驱动 Tick，Tick 内全部串行；
//   - 「一次 tick 只补发最近 1 条」这类全局约束因此是可证明的，不需要锁；
//   - mu 只保护生命周期字段与内存备忘，且在 Tick 内部不会跨调用持有。
type Engine struct {
	store    Store
	settings func() Settings
	notifier Notifier
	emitter  Emitter
	now      func() time.Time

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
		store:    opts.Store,
		settings: opts.Settings,
		notifier: opts.Notifier,
		emitter:  opts.Emitter,
		now:      opts.Now,
		claimed:  map[uint]map[int64]bool{},
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

	if err := e.tickOneOffs(now, settings); err != nil {
		slog.Error("一次性提醒调度失败", "err", err)
	}
	if err := e.tickGrid(now, settings, quiet); err != nil {
		slog.Error("提醒调度失败", "err", err)
	}
	if err := e.tickSnoozes(now, settings, quiet); err != nil {
		slog.Error("推迟提醒补发失败", "err", err)
	}
}

// tickOneOffs 处理全局一次性提醒：到点且未产生 fire 的提醒只处理一次。
//
// 【为什么不依赖启用场景】一次性事件独立于场景；场景停用/切换都不该让它静默。
// 【为什么错过也在这一轮落库】否则它每个 tick 都会被重新捞出，且用户永远看不到
// 「已错过」的痕迹。fire 记录本身就是该提醒已被处理过的标记。
func (e *Engine) tickOneOffs(now time.Time, settings Settings) error {
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
		if outcome == OneOffMissed {
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
		if outcome == OneOffMissed {
			missedAny = true
			slog.Info("一次性提醒已错过", "reminder", item.Title, "once_at", item.OnceAt)
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
func (e *Engine) tickGrid(now time.Time, settings Settings, quiet bool) error {
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
		for _, point := range points {
			if point > nowUnix {
				break // 未来的点留给后续 tick
			}
			if e.isClaimed(item.ID, point) {
				continue
			}
			fireID, claimed, claimErr := e.store.ClaimFire(item.ID, point)
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

	if settings.NotifyEnabled && e.notifier != nil {
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
	slog.Info("提醒已送达", "reminder", fire.Title, "fire_at", fire.FireAt)
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
