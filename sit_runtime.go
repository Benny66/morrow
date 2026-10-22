package main

// 久坐感知的运行时：把平台信号、配置、场景与持久化接到纯 sit.Tracker 上。
//
// 这一层只做编排：时钟/策略/文案在 internal/sit，数据库在 internal/service，
// 平台读取复用 internal/pause。绑定方法在 app.go（护栏只扫那一处）。

import (
	"context"
	"log/slog"
	"runtime"
	"strconv"
	"sync"
	"time"

	"morrow/internal/apperr"
	"morrow/internal/model"
	"morrow/internal/pause"
	"morrow/internal/reminder"
	"morrow/internal/restmode"
	"morrow/internal/service"
	"morrow/internal/sit"
)

// sitRuntimeSettings 是运行期读的配置快照；由 settingsMu 保护。
type sitRuntimeSettings struct {
	Enabled          bool
	ProfileEnabled   bool
	ThresholdMinutes int
	Tone             sit.Tone
	MutedDate        string
	PetVisible       bool
	QuietStart       string
	QuietEnd         string
}

// sitRuntime 持有 tracker 与跨 tick 的运行态。
type sitRuntime struct {
	app     *App
	tracker *sit.Tracker

	// opMu 串行化 5 秒 tick 与绑定动作；tracker 自己也有锁，但 session/break/
	// snooze 这些跨对象状态需要同一把锁，避免「起来走走」与 tick 同时收尾。
	opMu sync.Mutex

	cancel context.CancelFunc
	done   chan struct{}

	sessionID               uint
	lastClosedSessionID     uint
	lastClosedActiveSeconds int
	breakID                 uint
	lastPersisted           int
	lastPersistAt           time.Time
	snoozeUntil             time.Time
	// lastMemoryAttemptAt 是最近一次宠物主动/确认发言的时间，用于给独立记忆气泡
	// 一个 30 分钟尝试间隔；未展示而过期的气泡不会占预算，但也不能每 5 秒重试。
	lastMemoryAttemptAt time.Time
	// lastMemoryCheckAt 限制「没有值得说的事实」时的查询频率。
	lastMemoryCheckAt time.Time
	// focusWasActive / focusEndedAt 用于区分「专注刚结束」的记忆机会。
	focusWasActive bool
	focusEndedAt   time.Time

	// 应用画像采样运行态。
	appSessionID  uint
	lastAppID     string
	lastAppTouch  time.Time
	appReadMisses int
	profileStatus sit.ProfileStatus

	// 专注展示用的类别缓存：只在应用切换或用户纠正类别后重新解析。
	categoryAppID string
	categoryCache sit.Category
}

func newSitRuntime(app *App) *sitRuntime {
	return &sitRuntime{app: app, tracker: sit.NewTracker()}
}

// startSitRuntime 由 ServiceStartup 在提醒引擎之后调用。
func (a *App) startSitRuntime() {
	if a.svc == nil || a.cfg == nil || a.sit != nil {
		return
	}
	a.sit = newSitRuntime(a)
	if a.engine != nil {
		// 引擎在 004 只问一句「刚休息过吗」；其余调度语义不变。
		a.engine.SetBreakProvider(a.sit.tracker)
	}
	// 上次进程异常退出时可能留下开放的会话；先按最后心跳收尾，避免
	// 把崩溃后的时间算成前台使用/久坐。
	if n, err := a.svc.RecoverOpenAppUsageSessions(); err != nil {
		slog.Warn("收尾遗留应用会话失败", "err", err)
	} else if n > 0 {
		slog.Info("已收尾上次未关闭的应用会话", "count", n)
	}
	if n, err := a.svc.RecoverOpenSitSessions(); err != nil {
		slog.Warn("收尾遗留久坐会话失败", "err", err)
	} else if n > 0 {
		slog.Info("已收尾上次未关闭的久坐会话", "count", n)
	}
	a.sit.start()
	slog.Info("久坐感知已启动", "tick", "5s")
}

func (r *sitRuntime) start() {
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.done = make(chan struct{})
	go r.loop(ctx, r.done)
}

func (r *sitRuntime) stop() {
	if r.cancel == nil {
		return
	}
	r.cancel()
	select {
	case <-r.done:
	case <-time.After(3 * time.Second):
		slog.Warn("久坐感知未在超时内退出")
	}
	r.cancel = nil
}

func (r *sitRuntime) loop(ctx context.Context, done chan struct{}) {
	defer close(done)
	defer func() {
		if v := recover(); v != nil {
			slog.Error("久坐感知循环异常退出", "panic", v)
		}
	}()
	r.safeTick()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.safeTick()
		}
	}
}

func (r *sitRuntime) safeTick() {
	defer func() {
		if v := recover(); v != nil {
			slog.Error("久坐感知单轮异常，已恢复", "panic", v)
		}
	}()
	r.tick()
}

func (r *sitRuntime) tick() {
	r.opMu.Lock()
	defer r.opMu.Unlock()

	if r.app == nil || r.app.svc == nil || r.app.cfg == nil {
		return
	}
	settings := r.app.currentSitRuntimeSettings()
	now := time.Now()
	idle := pause.GetIdleDuration()
	active, nextPlan := r.activeContext(now)
	muted := settings.MutedDate != "" && settings.MutedDate == now.Format(model.DateLayout)
	supported := pause.IdleSupported()

	bundle := r.app.pauseSnapshot()
	paused := false
	locked := false
	var lockedAt time.Time
	if bundle != nil {
		if state := bundle.Provider.State(); state.IsPaused() {
			paused = true
		}
		if bundle.Lock != nil {
			locked = bundle.Lock.IsLocked()
			lockedAt = bundle.Lock.Since()
		}
	}

	res := r.tracker.Observe(sit.TrackInput{
		Now:       now,
		Idle:      idle,
		Locked:    locked,
		LockedAt:  lockedAt,
		Active:    settings.Enabled && active,
		Muted:     muted,
		Supported: supported,
	})
	r.handleEvents(res.Events, now, settings)
	r.persist(now, res.ActiveSeconds)
	appID, appCategory := r.sampleAppUsage(now, settings, active, supported, locked, idle)
	focusID, focusCategory := appID, appCategory
	if idle >= sit.FocusSwitchGrace {
		// 长时间没有输入时按切走处理；宽限由 tracker 累计，不在这里立即结束。
		focusID, focusCategory = "", ""
	}
	r.tracker.ObserveFocus(sit.FocusInput{
		Now:      now,
		Enabled:  settings.ProfileEnabled && settings.Enabled && active && supported && !locked && idle < sit.ResetIdle,
		AppID:    focusID,
		Category: focusCategory,
	})
	res.Snapshot = r.tracker.Snapshot()
	focusActive := res.Snapshot.Focus != nil
	if r.focusWasActive && !focusActive {
		r.focusEndedAt = now
	}
	r.focusWasActive = focusActive

	quiet := reminder.InQuietHours(now, settings.QuietStart, settings.QuietEnd)
	pending := r.pendingCount()
	eligible := settings.Enabled &&
		active &&
		supported &&
		settings.PetVisible &&
		!muted &&
		!paused &&
		!quiet &&
		!r.restBusy() &&
		pending == 0 &&
		idle >= 5*time.Second &&
		res.Snapshot.Focus == nil &&
		res.Snapshot.State == sit.StateTracking
	if eligible {
		r.maybeSpeak(now, res, settings, nextPlan)
	}
}

func (r *sitRuntime) handleEvents(events []sit.Event, now time.Time, settings sitRuntimeSettings) {
	if r.app == nil || r.app.svc == nil {
		return
	}
	for _, ev := range events {
		switch ev.Kind {
		case sit.EventTrackingStarted:
			row, err := r.app.svc.StartSitSession(ev.At)
			if err != nil {
				slog.Warn("创建久坐会话失败", "err", err)
				continue
			}
			r.sessionID = row.ID
			r.lastPersisted = 0
			r.lastPersistAt = ev.At
			r.lastClosedActiveSeconds = 0
		case sit.EventTrackingEnded:
			if r.sessionID == 0 {
				continue
			}
			if err := r.app.svc.EndSitSession(
				r.sessionID,
				string(ev.EndReason),
				int(ev.ActiveSeconds.Seconds()),
				ev.At,
			); err != nil {
				slog.Warn("收尾久坐会话失败", "err", err)
			}
			r.lastClosedSessionID = r.sessionID
			r.lastClosedActiveSeconds = int(ev.ActiveSeconds.Seconds())
			r.sessionID = 0
			r.lastPersisted = 0
			if ev.EndReason == sit.EndReasonActiveBreak ||
				ev.EndReason == sit.EndReasonLockBreak ||
				ev.EndReason == sit.EndReasonIdleBreak {
				r.expireSpeeches(now)
			}
		case sit.EventBreakStarted:
			sessionID := r.sessionID
			if sessionID == 0 {
				sessionID = r.lastClosedSessionID
			}
			row, err := r.app.svc.StartSitBreak(sessionID, ev.At, string(ev.BreakKind), int(ev.Duration.Seconds()))
			if err != nil {
				slog.Warn("记录久坐休息失败", "err", err)
				continue
			}
			if ev.Duration > 0 {
				r.breakID = 0
			} else {
				r.breakID = row.ID
			}
			if ev.BreakKind == sit.BreakKindActive && settings.PetVisible {
				r.createSpeech(
					sit.BubbleGreeting,
					sit.GreetingStartTemplateID(settings.Tone),
					"",
					sit.BreakStartText(settings.Tone),
					now,
					settings,
				)
			}
		case sit.EventBreakEnded:
			if r.breakID != 0 {
				if err := r.app.svc.FinishSitBreak(r.breakID, int(ev.Duration.Seconds())); err != nil {
					slog.Warn("补休息时长失败", "err", err)
				}
				r.breakID = 0
			}
			if ev.BreakKind == sit.BreakKindLock {
				r.creditLockBreak(ev, now)
			}
			if ev.Text != "" && settings.PetVisible {
				r.expireSpeeches(now)
				// 欢迎回来本身就是记忆机会：记忆句搭同一条 greeting，不再额外冒独立气泡。
				ctx := r.memoryContext(now, settings, 0)
				text, templateID := sit.SelectGreetingText(ev.Duration, settings.Tone, ctx.GreetingTemplateIDs)
				memoryID := ""
				if quote := sit.SelectQuote(now, ctx.Facts, settings.Tone, ctx.QuoteIDs); quote != nil {
					text += " " + quote.Text
					memoryID = quote.ID
				}
				r.createSpeech(
					sit.BubbleGreeting,
					templateID,
					memoryID,
					text,
					now,
					settings,
				)
			}
		}
	}
}

// creditLockBreak 满足「锁屏 ≥5 分钟 + 久坐 ≥30 分钟」时抵扣一次目标。
func (r *sitRuntime) creditLockBreak(ev sit.Event, now time.Time) {
	if r.app == nil || r.app.svc == nil || r.lastClosedSessionID == 0 {
		return
	}
	sitActive := time.Duration(r.lastClosedActiveSeconds) * time.Second
	if !sit.ShouldCreditLock(ev.Duration, sitActive) {
		return
	}
	scenario, err := r.app.svc.ActiveScenario()
	if err != nil || scenario == nil {
		return
	}
	res, err := r.app.svc.CreditNearestStandMove(scenario.ID, r.lastClosedSessionID, now, model.SourceLockBreak)
	if err != nil {
		slog.Warn("锁屏抵扣久坐目标失败", "err", err)
		return
	}
	if res.Credited {
		r.app.emitReminderChanged(reminder.ReasonCheckIn)
		slog.Info("锁屏抵扣久坐目标",
			"reminder", res.ReminderTitle, "lock_minutes", int(ev.Duration.Minutes()))
	}
	r.lastClosedActiveSeconds = 0
}

func (r *sitRuntime) createSpeech(kind sit.BubbleKind, templateID, memoryID, text string, now time.Time, settings sitRuntimeSettings) {
	if r.app == nil || r.app.svc == nil || !settings.PetVisible || text == "" {
		return
	}
	sessionID := r.sessionID
	if sessionID == 0 {
		sessionID = r.lastClosedSessionID
	}
	if _, err := r.app.svc.CreateSitSpeech(service.SitSpeechInput{
		DateKey:    now.Format(model.DateLayout),
		SessionID:  sessionID,
		Kind:       string(kind),
		TemplateID: templateID,
		MemoryID:   memoryID,
		Text:       text,
		Now:        now,
		TTL:        sit.BubbleTTL,
	}); err != nil {
		slog.Warn("创建宠物久坐气泡失败", "err", err)
		return
	}
	r.lastMemoryAttemptAt = now
}

// expireSpeeches 让未确认的旧气泡立即过期；失败只影响旧气泡，不阻断状态变更。
func (r *sitRuntime) expireSpeeches(now time.Time) {
	if r.app == nil || r.app.svc == nil {
		return
	}
	if err := r.app.svc.ExpireSitSpeeches(now); err != nil {
		slog.Warn("清理待展示久坐气泡失败", "err", err)
	}
}

// memoryContext 读取记忆事实与近期模板；失败时退回无记忆的基础文案。
func (r *sitRuntime) memoryContext(now time.Time, settings sitRuntimeSettings, continuous int) service.SitMemoryContext {
	if r.app == nil || r.app.svc == nil {
		return service.SitMemoryContext{}
	}
	ctx, err := r.app.svc.SitMemoryContext(now, settings.ProfileEnabled, continuous)
	if err != nil {
		slog.Warn("读取久坐记忆事实失败", "err", err)
		return service.SitMemoryContext{}
	}
	return ctx
}

func (r *sitRuntime) persist(now time.Time, active time.Duration) {
	if r.app == nil || r.app.svc == nil || r.sessionID == 0 {
		return
	}
	if !r.lastPersistAt.IsZero() && now.Sub(r.lastPersistAt) < time.Minute {
		return
	}
	seconds := int(active.Seconds())
	if seconds < 0 {
		seconds = 0
	}
	if err := r.app.svc.TouchSitSession(r.sessionID, seconds, now); err != nil {
		slog.Warn("心跳更新久坐会话失败", "err", err)
		return
	}
	r.lastPersisted = seconds
	r.lastPersistAt = now
}

func (r *sitRuntime) maybeSpeak(now time.Time, res sit.TickResult, settings sitRuntimeSettings, nextPlan int64) {
	if r.app == nil || r.app.svc == nil {
		return
	}
	today := now.Format(model.DateLayout)
	budget, err := r.app.svc.SitBudget(today, r.sessionID)
	if err != nil {
		slog.Warn("读取久坐预算失败", "err", err)
		return
	}
	pending, err := r.app.svc.PendingSitSpeech(now)
	if err != nil {
		slog.Warn("查询待展示气泡失败", "err", err)
		return
	}
	if pending != nil {
		return
	}
	// 软提醒优先；它已经可以搭一句记忆，没必要再单独冒一条记忆气泡。
	if r.maybeNudge(now, res, settings, nextPlan, budget) {
		return
	}
	r.maybeMemory(now, res, settings, nextPlan, budget)
}

func (r *sitRuntime) maybeNudge(now time.Time, res sit.TickResult, settings sitRuntimeSettings, nextPlan int64, budget service.SitBudget) bool {
	if r.app == nil || r.app.svc == nil {
		return false
	}
	snoozeUntil := int64(0)
	if !r.snoozeUntil.IsZero() {
		snoozeUntil = r.snoozeUntil.Unix()
	}
	decision := sit.DecideNudge(sit.NudgeInput{
		Now:              now,
		Continuous:       res.ActiveSeconds,
		ThresholdMinutes: settings.ThresholdMinutes,
		SessionStartedAt: res.SessionStartedAt,
		NextStandMoveAt:  nextPlan,
		SessionNudges:    budget.SessionNudges,
		DailyProactive:   budget.DailyProactive,
		LastProactiveAt:  budget.LastProactiveAt,
		FirstNudgeAckAt:  budget.FirstNudgeAckAt,
		SnoozeUntil:      snoozeUntil,
		FocusActive:      res.Snapshot.Focus != nil,
		Eligible:         true,
	})
	if !decision.Nudge {
		return false
	}
	today := now.Format(model.DateLayout)
	ctx := r.memoryContext(now, settings, res.Snapshot.ContinuousMinutes)
	text, templateID := sit.SelectNudgeText(res.ActiveSeconds, settings.Tone, ctx.NudgeTemplateIDs)
	memoryID := ""
	if quote := sit.SelectQuote(now, ctx.Facts, settings.Tone, ctx.QuoteIDs); quote != nil {
		text += " " + quote.Text
		memoryID = quote.ID
	}
	if _, err := r.app.svc.CreateSitSpeech(service.SitSpeechInput{
		DateKey:    today,
		SessionID:  r.sessionID,
		Kind:       string(sit.BubbleNudge),
		TemplateID: templateID,
		MemoryID:   memoryID,
		Text:       text,
		Now:        now,
		TTL:        sit.BubbleTTL,
	}); err != nil {
		slog.Warn("创建久坐软提醒失败", "err", err)
		return false
	}
	r.lastMemoryAttemptAt = now
	r.snoozeUntil = time.Time{}
	slog.Info("久坐软提醒已生成",
		"minutes", res.Snapshot.ContinuousMinutes,
		"reason", decision.Reason,
		"template_id", templateID,
		"memory_id", memoryID,
		"session_nudges", budget.SessionNudges,
		"daily", budget.DailyProactive)
	return true
}

// maybeMemory 尝试生成独立记忆气泡。
//
// 预算/间隔/正式提醒空档先做廉价判断，避免为了“没有事实”每 5 秒查一次历史。
func (r *sitRuntime) maybeMemory(now time.Time, res sit.TickResult, settings sitRuntimeSettings, nextPlan int64, budget service.SitBudget) {
	if r.app == nil || r.app.svc == nil {
		return
	}
	window := sit.MemoryWindow(sit.MemoryInput{
		Now:             now,
		NextStandMoveAt: nextPlan,
		DailyProactive:  budget.DailyProactive,
		LastProactiveAt: budget.LastProactiveAt,
		Eligible:        true,
		// MemoryWindow 不读 HasQuote；这里只做廉价时间窗判断。
		HasQuote: true,
	})
	if !window.Speak {
		return
	}
	if !r.lastMemoryAttemptAt.IsZero() && now.Sub(r.lastMemoryAttemptAt) < sit.ProactiveGap {
		return
	}
	if !r.lastMemoryCheckAt.IsZero() && now.Sub(r.lastMemoryCheckAt) < time.Minute {
		return
	}
	r.lastMemoryCheckAt = now

	ctx := r.memoryContext(now, settings, res.Snapshot.ContinuousMinutes)
	quote := sit.SelectQuote(now, ctx.Facts, settings.Tone, ctx.QuoteIDs)
	decision := sit.DecideMemory(sit.MemoryInput{
		Now:             now,
		NextStandMoveAt: nextPlan,
		DailyProactive:  budget.DailyProactive,
		LastProactiveAt: budget.LastProactiveAt,
		Eligible:        true,
		HasQuote:        quote != nil,
	})
	if !decision.Speak {
		return
	}
	if _, err := r.app.svc.CreateSitSpeech(service.SitSpeechInput{
		DateKey:    now.Format(model.DateLayout),
		SessionID:  r.sessionID,
		Kind:       string(sit.BubbleMemory),
		TemplateID: quote.TemplateID,
		MemoryID:   quote.ID,
		Text:       quote.Text,
		Now:        now,
		TTL:        sit.BubbleTTL,
	}); err != nil {
		slog.Warn("创建记忆气泡失败", "err", err)
		return
	}
	r.lastMemoryAttemptAt = now
	trigger := "natural_pause"
	if !r.focusEndedAt.IsZero() && now.Sub(r.focusEndedAt) <= 10*time.Minute {
		trigger = "focus_end"
		r.focusEndedAt = time.Time{}
	}
	slog.Info("独立记忆气泡已生成",
		"memory_id", quote.ID,
		"template_id", quote.TemplateID,
		"trigger", trigger,
		"daily", budget.DailyProactive)
}

func (r *sitRuntime) activeContext(now time.Time) (bool, int64) {
	if r.app == nil || r.app.svc == nil {
		return false, 0
	}
	scenario, err := r.app.svc.ActiveScenario()
	if err != nil || scenario == nil {
		return false, 0
	}
	items, err := r.app.svc.ActiveReminders(scenario.ID)
	if err != nil {
		return false, 0
	}
	nowMin := now.Hour()*60 + now.Minute()
	has := false
	inWindow := false
	next := int64(0)
	for _, item := range items {
		if !item.Enabled || item.OnceAt != 0 {
			continue
		}
		if item.Kind != model.KindStand && item.Kind != model.KindMove {
			continue
		}
		has = true
		start, startOK := reminder.ParseClock(item.WindowStart)
		end, endOK := reminder.ParseClock(item.WindowEnd)
		if startOK && endOK && end > start && nowMin >= start && nowMin <= end {
			inWindow = true
		}
		n := reminder.NextFireAt(now, item.WindowStart, item.WindowEnd, item.IntervalMin)
		if n > 0 && (next == 0 || n < next) {
			next = n
		}
	}
	return has && inWindow, next
}

func (r *sitRuntime) pendingCount() int {
	if r.app == nil || r.app.svc == nil {
		return 0
	}
	list, err := r.app.svc.ListPendingFires()
	if err != nil {
		slog.Warn("统计待处理提醒失败", "err", err)
		return 0
	}
	return len(list)
}

func (r *sitRuntime) restBusy() bool {
	if r.app == nil || r.app.restMode == nil {
		return false
	}
	switch r.app.restMode.State() {
	case restmode.StateWarning, restmode.StateCountdown:
		return true
	default:
		return false
	}
}

// clearProfileData 清空应用画像与久坐数据并重置内存状态；CheckIn 由 service 保留。
func (r *sitRuntime) clearProfileData() error {
	r.opMu.Lock()
	defer r.opMu.Unlock()
	if r.app == nil || r.app.svc == nil {
		return apperr.New(apperr.CodeInternal, "久坐感知尚未就绪")
	}
	if err := r.app.svc.ClearSitProfile(); err != nil {
		return err
	}
	r.resetInMemoryLocked()
	return nil
}

// resetRuntimeState 在数据库被整体替换后清掉旧会话/采样运行态。
func (r *sitRuntime) resetRuntimeState() {
	r.opMu.Lock()
	defer r.opMu.Unlock()
	r.resetInMemoryLocked()
}

// resetInMemoryLocked 只清内存状态；调用方负责持锁与数据库动作。
func (r *sitRuntime) resetInMemoryLocked() {
	r.tracker.Reset()
	r.sessionID = 0
	r.lastClosedSessionID = 0
	r.lastClosedActiveSeconds = 0
	r.breakID = 0
	r.lastPersisted = 0
	r.lastPersistAt = time.Time{}
	r.snoozeUntil = time.Time{}
	r.lastMemoryAttemptAt = time.Time{}
	r.lastMemoryCheckAt = time.Time{}
	r.focusWasActive = false
	r.focusEndedAt = time.Time{}
	r.appSessionID = 0
	r.lastAppID = ""
	r.lastAppTouch = time.Time{}
	r.appReadMisses = 0
	r.categoryAppID = ""
	r.categoryCache = ""
	if r.app != nil && r.app.currentSitRuntimeSettings().ProfileEnabled {
		r.profileStatus = sit.ProfileStatusNoData
	} else {
		r.profileStatus = ""
	}
}

// sampleAppUsage 是 5 秒前台应用采样：只在画像开启、场景生效、未锁屏/长空闲时记录。
// 同时返回当前前台应用与派生类别，供专注会话使用（应用标识不进入 Snapshot）。
func (r *sitRuntime) sampleAppUsage(now time.Time, settings sitRuntimeSettings, active, supported, locked bool, idle time.Duration) (appID string, category sit.Category) {
	if r.app == nil || r.app.svc == nil {
		return "", ""
	}
	if !settings.ProfileEnabled {
		r.closeAppSession(now, sit.EndReasonFeatureOff)
		r.profileStatus = ""
		return "", ""
	}
	if !supported || (runtime.GOOS != "darwin" && runtime.GOOS != "windows") {
		r.closeAppSession(now, sit.EndReasonFeatureOff)
		r.profileStatus = sit.ProfileStatusUnsupported
		return "", ""
	}
	if !settings.Enabled || !active || locked || idle >= sit.ResetIdle {
		reason := sit.EndReasonFeatureOff
		if locked {
			reason = sit.EndReasonLock
		} else if idle >= sit.ResetIdle {
			reason = sit.EndReasonIdle
		}
		r.closeAppSession(now, reason)
		r.profileStatus = sit.ProfileStatusNoData
		return "", ""
	}

	appID = pause.GetFrontmostApp()
	if appID == "" {
		r.appReadMisses++
		r.closeAppSession(now, sit.EndReasonUnknown)
		if r.appReadMisses >= 3 {
			r.profileStatus = sit.ProfileStatusReadError
		} else {
			r.profileStatus = sit.ProfileStatusNoData
		}
		return "", ""
	}
	r.appReadMisses = 0

	if sit.IsSelfApp(runtime.GOOS, appID) || sit.IsSystemUIApp(runtime.GOOS, appID) {
		// Morrow 自己与登录/锁屏界面不算工作应用；会话收尾，但仍视为读取正常。
		r.closeAppSession(now, sit.EndReasonAppSwitch)
		r.profileStatus = sit.ProfileStatusOK
		return "", ""
	}

	category = r.resolveAppCategory(appID)

	if r.appSessionID != 0 && r.lastAppID == appID {
		if r.lastAppTouch.IsZero() || now.Sub(r.lastAppTouch) >= time.Minute {
			if err := r.app.svc.TouchAppUsageSession(r.appSessionID, now); err != nil {
				slog.Warn("应用会话心跳失败", "err", err)
				return appID, category
			}
			r.lastAppTouch = now
		}
		r.profileStatus = sit.ProfileStatusOK
		return appID, category
	}

	r.closeAppSession(now, sit.EndReasonAppSwitch)
	row, err := r.app.svc.StartAppUsageSession(runtime.GOOS, appID, now)
	if err != nil {
		slog.Warn("创建应用会话失败", "err", err)
		r.profileStatus = sit.ProfileStatusReadError
		return appID, category
	}
	r.appSessionID = row.ID
	r.lastAppID = appID
	r.lastAppTouch = now
	r.profileStatus = sit.ProfileStatusOK
	return appID, category
}

// resolveAppCategory 缓存当前应用的派生类别，避免每 5 秒读一次纠正表。
func (r *sitRuntime) resolveAppCategory(appID string) sit.Category {
	if appID == "" {
		return ""
	}
	if r.categoryAppID == appID && r.categoryCache != "" {
		return r.categoryCache
	}
	category, err := r.app.svc.AppCategory(runtime.GOOS, appID)
	if err != nil {
		slog.Warn("读取应用类别失败", "err", err)
		category = sit.CategoryFor(runtime.GOOS, appID, false)
	}
	r.categoryAppID = appID
	r.categoryCache = category
	return category
}

// invalidateAppCategory 由类别纠正绑定调用；下一轮采样重新解析。
func (r *sitRuntime) invalidateAppCategory() {
	r.opMu.Lock()
	defer r.opMu.Unlock()
	r.categoryAppID = ""
	r.categoryCache = ""
}

func (r *sitRuntime) closeAppSession(now time.Time, reason sit.EndReason) {
	if r.app == nil || r.app.svc == nil || r.appSessionID == 0 {
		return
	}
	if err := r.app.svc.EndAppUsageSession(r.appSessionID, string(reason), now); err != nil {
		slog.Warn("收尾应用会话失败", "err", err)
	}
	r.appSessionID = 0
	r.lastAppID = ""
	r.lastAppTouch = time.Time{}
}

// setProfileEnabled 是应用画像同意的唯一入口：开/关都写 asked，关闭可选删除历史。
func (r *sitRuntime) setProfileEnabled(enabled, deleteHistory bool) error {
	r.opMu.Lock()
	defer r.opMu.Unlock()
	if r.app == nil || r.app.cfg == nil || r.app.svc == nil {
		return apperr.New(apperr.CodeInternal, "久坐感知尚未就绪")
	}
	settings := r.app.cfg.LoadSitSettings()
	settings.ProfileEnabled = enabled
	settings.ProfileAsked = true
	if err := r.app.cfg.SaveSitSettings(settings); err != nil {
		return apperr.Wrap(err)
	}
	r.app.syncSettingsFromConfig()
	now := time.Now()
	if !enabled {
		r.closeAppSession(now, sit.EndReasonFeatureOff)
		r.profileStatus = ""
		// 画像关闭后不留旧专注态；应用标识缓存也一并清掉。
		r.tracker.ObserveFocus(sit.FocusInput{Now: now})
		r.categoryAppID = ""
		r.categoryCache = ""
		if deleteHistory {
			if err := r.app.svc.ClearAppProfile(); err != nil {
				return err
			}
		}
		return nil
	}
	r.profileStatus = sit.ProfileStatusNoData
	r.appReadMisses = 0
	return nil
}

func (r *sitRuntime) currentProfileStatus() sit.ProfileStatus {
	r.opMu.Lock()
	defer r.opMu.Unlock()
	return r.profileStatus
}

func (r *sitRuntime) snapshot() sit.Snapshot {
	r.opMu.Lock()
	defer r.opMu.Unlock()

	settings := r.app.currentSitRuntimeSettings()
	now := time.Now()
	today := now.Format(model.DateLayout)
	base := r.tracker.Snapshot()
	base.Tone = sit.NormalizeTone(string(settings.Tone))
	base.PlatformSupported = pause.IdleSupported()
	base.ProfileSupported = runtime.GOOS == "darwin" || runtime.GOOS == "windows"
	base.ProfileEnabled = settings.ProfileEnabled && base.ProfileSupported
	base.MutedToday = settings.MutedDate != "" && settings.MutedDate == today
	if !settings.Enabled {
		base.State = sit.StateIdle
		base.Mood = sit.MoodEnergetic
	}
	if r.app == nil || r.app.svc == nil {
		return base
	}
	closed, longest, err := r.app.svc.SitDayClosedTotals(today)
	if err != nil {
		slog.Warn("读取今日久坐统计失败", "err", err)
	}
	current := base.ContinuousMinutes * 60
	base.TodaySittingMinutes = (closed + current) / 60
	if current > longest {
		longest = current
	}
	base.LongestSessionMinutes = longest / 60
	budget, err := r.app.svc.SitBudget(today, r.sessionID)
	if err != nil {
		slog.Warn("读取久坐预算失败", "err", err)
		base.BudgetRemaining = sit.ProactiveBudget
	} else {
		base.BudgetRemaining = sit.ProactiveBudget - budget.DailyProactive
		if base.BudgetRemaining < 0 {
			base.BudgetRemaining = 0
		}
	}
	pending, err := r.app.svc.PendingSitSpeech(now)
	if err == nil && pending != nil {
		kind := sit.BubbleKind(pending.Kind)
		// 专注期间不展示会打断的软提醒/记忆气泡；问候与用户动作反馈仍可见。
		if !sit.SuppressesBubble(base.Focus, kind) {
			base.Bubble = &sit.Bubble{
				ID:        strconv.FormatUint(uint64(pending.ID), 10),
				Kind:      kind,
				Title:     sit.BubbleTitleFor(kind, pending.TemplateID),
				Text:      pending.Text,
				Actions:   sit.BubbleActions(kind),
				ExpiresAt: pending.ExpiresAt,
			}
		}
	}
	return base
}

func (r *sitRuntime) takeActiveBreak(bubbleID string) (sit.SitActionResult, error) {
	r.opMu.Lock()
	defer r.opMu.Unlock()

	if r.app == nil || r.app.svc == nil {
		return sit.SitActionResult{}, apperr.New(apperr.CodeInternal, "久坐感知尚未就绪")
	}
	now := time.Now()
	if id := parseSitBubbleID(bubbleID); id != 0 {
		_, _ = r.app.svc.AckSitSpeech(id, now)
	}
	sessionID := r.sessionID
	res := r.tracker.StartBreak(now)
	if len(res.Events) == 0 {
		return sit.SitActionResult{OK: false, Message: "现在还没有需要处理的久坐"}, nil
	}
	settings := r.app.currentSitRuntimeSettings()
	r.handleEvents(res.Events, now, settings)
	r.persist(now, res.ActiveSeconds)
	r.snoozeUntil = time.Time{}

	action := sit.SitActionResult{OK: true, Message: "这次休息记下了"}
	if sessionID != 0 {
		scenario, err := r.app.svc.ActiveScenario()
		if err == nil && scenario != nil {
			credit, creditErr := r.app.svc.CreditNearestStandMove(
				scenario.ID, sessionID, now, model.SourceActiveBreak)
			if creditErr != nil {
				slog.Warn("主动休息抵扣久坐目标失败", "err", creditErr)
			} else if credit.Credited {
				action.Credited = true
				action.CreditedTitle = credit.ReminderTitle
				action.Message = "这次记在「" + credit.ReminderTitle + "」上了"
				r.app.emitReminderChanged(reminder.ReasonCheckIn)
				slog.Info("主动休息抵扣久坐目标", "reminder", credit.ReminderTitle)
			}
		}
	}
	return action, nil
}

func (r *sitRuntime) snooze(bubbleID string) (sit.SitActionResult, error) {
	r.opMu.Lock()
	defer r.opMu.Unlock()

	if r.app == nil || r.app.svc == nil {
		return sit.SitActionResult{}, apperr.New(apperr.CodeInternal, "久坐感知尚未就绪")
	}
	id := parseSitBubbleID(bubbleID)
	if id == 0 {
		return sit.SitActionResult{}, apperr.Validation("这个气泡已经失效了")
	}
	speech, err := r.app.svc.SitSpeech(id)
	if err != nil {
		return sit.SitActionResult{}, err
	}
	if speech == nil || speech.Kind != string(sit.BubbleNudge) {
		return sit.SitActionResult{}, apperr.Validation("只有久坐软提醒可以再坐 10 分钟")
	}
	now := time.Now()
	if _, err := r.app.svc.AckSitSpeech(id, now); err != nil {
		return sit.SitActionResult{}, err
	}
	r.snoozeUntil = now.Add(sit.SnoozeDelay)
	return sit.SitActionResult{OK: true, Message: "好，10 分钟后再提醒你"}, nil
}

func (r *sitRuntime) muteToday() (sit.SitActionResult, error) {
	r.opMu.Lock()
	defer r.opMu.Unlock()

	if r.app == nil || r.app.cfg == nil || r.app.svc == nil {
		return sit.SitActionResult{}, apperr.New(apperr.CodeInternal, "久坐感知尚未就绪")
	}
	if err := r.app.cfg.MuteSitToday(); err != nil {
		return sit.SitActionResult{}, apperr.Wrap(err)
	}
	r.app.syncSettingsFromConfig()
	now := time.Now()
	r.expireSpeeches(now)
	r.snoozeUntil = time.Time{}
	settings := r.app.currentSitRuntimeSettings()
	r.createSpeech(
		sit.BubbleGreeting,
		sit.MuteConfirmTemplateID(settings.Tone),
		"",
		sit.MuteConfirmText(settings.Tone),
		now,
		settings,
	)
	return sit.SitActionResult{OK: true, Message: "好，今天不催你了"}, nil
}

func (r *sitRuntime) setTone(tone string) (sit.SitActionResult, error) {
	r.opMu.Lock()
	defer r.opMu.Unlock()

	if r.app == nil || r.app.cfg == nil || r.app.svc == nil {
		return sit.SitActionResult{}, apperr.New(apperr.CodeInternal, "久坐感知尚未就绪")
	}
	settings := r.app.cfg.LoadSitSettings()
	settings.Tone = sit.NormalizeTone(tone)
	if err := r.app.cfg.SaveSitSettings(settings); err != nil {
		return sit.SitActionResult{}, apperr.Wrap(err)
	}
	r.app.syncSettingsFromConfig()
	now := time.Now()
	r.expireSpeeches(now)
	live := r.app.currentSitRuntimeSettings()
	r.createSpeech(
		sit.BubbleGreeting,
		sit.ToneConfirmTemplateID(live.Tone),
		"",
		sit.ToneConfirmText(live.Tone),
		now,
		live,
	)
	return sit.SitActionResult{OK: true, Message: "好，我好好说"}, nil
}

func parseSitBubbleID(id string) uint {
	n, err := strconv.ParseUint(id, 10, 32)
	if err != nil {
		return 0
	}
	return uint(n)
}
