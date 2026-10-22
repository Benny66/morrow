package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"

	"morrow/internal/ai"
	"morrow/internal/appdir"
	"morrow/internal/apperr"
	"morrow/internal/appinfo"
	"morrow/internal/config"
	"morrow/internal/dialog"
	"morrow/internal/event"
	"morrow/internal/model"
	"morrow/internal/pause"
	"morrow/internal/presentation"
	"morrow/internal/reminder"
	"morrow/internal/restmode"
	"morrow/internal/reveal"
	"morrow/internal/scenariotemplate"
	"morrow/internal/service"
	"morrow/internal/sit"
	"morrow/internal/systemprefs"
	"morrow/internal/tray"
	// gen:import
)

// App 应用结构体，承载前端可调用的绑定方法。
// 分层纪律：App 不直接持有 *gorm.DB、不 import database，业务访问一律经 svc。
// 运行时服务（config/tray/dialog）由 App 持有并暴露为绑定方法。
//
// 与 v2 的差异：v2 有个 ctx context.Context 字段（startup 时存入，之后每个
// runtime.* 调用都要把它传进去）。v3 没有 runtime 包——能力长在对象上
// （窗口在 app.Window，对话框在 app.Dialog，事件在 app.Event），故改为持有
// *application.App。见 openspec/changes/wails-v3-ai-base/design.md D6。
type App struct {
	app      *application.App
	svc      *service.Service
	cfg      *config.Config
	trayStop func()
	// trayInst 是 tray.Start 返回的实例句柄；状态变化时调 Refresh 重建菜单。
	trayInst *tray.Instance
	// trayMu / lastTrayStatus 缓存上次刷过的菜单栏状态文本，只在文本变化时
	// 重建 NSMenu——避免每秒无谓刷新导致菜单栏闪（见 refreshTrayIfChanged）。
	// trayStatsLines / trayStatsAt 是今日进度的 5 秒缓存（见 cachedTrayStatsLines）。
	trayMu         sync.Mutex
	lastTrayStatus string
	trayStatsLines []string
	trayStatsAt    time.Time
	// notify 是官方通知服务实例（main.go 创建并注册进 Services，同包直接赋值）。
	// 持有它而非每次现取：ServiceStartup 由框架回调，绑定方法只能靠这个引用触达。
	notify *notifications.NotificationService
	// shortcutAcc / presentationShortcutAcc 是【已注册到系统】的快捷键串，与配置里的
	// 期望值分开——注册失败时二者允许短暂不一致。读写发生在绑定调用与 ServiceStartup
	// 两个 goroutine，由 shortcutMu 串行化。
	shortcutMu  sync.Mutex
	shortcutAcc string
	// presentationShortcutAcc 是演示模式快捷键的【已注册】串；
	// 与 cfg.ShortcutPresentation 分开，理由同上。
	presentationShortcutAcc string
	// petMu 串行化宠物窗口的显示/隐藏与尺寸变更。
	//
	// 三条入口会动它：设置页保存（SetPetSettings）、托盘/快捷方式式的显隐调用
	// （ShowPet/HidePet/TogglePet）、以及 ServiceShutdown 里的位置采集。
	// 它们分布在不同的绑定调用 goroutine 上，不加锁就可能出现
	// 「刚 Show 又被 Hide」这种用户看不懂的抖动。
	petMu sync.Mutex
	// petDockMu 保护 petDock 与 petDockCancel。
	//
	// 【为什么与 petMu 分开】petMu 串行化的是"窗口操作"；贴边状态则在位置轮询
	// goroutine 与绑定调用之间共享。合成一把锁会让轮询里的一次 SetPosition
	// 把设置页的保存操作一起堵住，没必要。
	// 加锁顺序固定为 petMu → petDockMu（见 capturePetGeometry / tryDockPet），
	// 反向获取会死锁。
	petDockMu sync.Mutex
	// petDock 是当前贴边状态：贴哪个边、是否收着、展开与收起各自的位置。
	petDock petDockState
	// petDockCancel 是位置轮询的取消函数；nil 表示未启动。
	petDockCancel context.CancelFunc
	// petDockIgnoreUntil 是贴边检测的忽略截止时间（Unix 毫秒）。
	//
	// 【为什么需要】展开时窗口回到的位置，正是当初触发吸附的那个位置——它仍然
	// 落在阈值内。若不停一轮，位置轮询会在展开后立刻把它重新吸附回去，用户看到
	// 「鼠标划过闪一下就没了」。用原子量而非锁保护：轮询 goroutine 读、绑定调用写。
	petDockIgnoreUntil atomic.Int64
	// petBubbleActive / petBubbleBudget 是气泡扩窗的运行期状态（受 petMu 保护）。
	//
	// active 表示窗口当前处于「气泡形态」；budget 是气泡自身需要的额外高度
	// （总高 - 基础高），缩放变化时用它重算总高。两者都不写配置：退出时持久化的是
	// 基础几何（见 capturePetGeometry）。
	petBubbleActive bool
	petBubbleBudget int
	// trayUp 记录系统托盘是否【真的】起来了，供关窗决策在运行期读取。
	//
	// 【为什么是运行期状态而不是编译期常量】托盘在 ServiceStartup 里启动，可能失败
	// （失败只记日志、不阻断启动），而关窗钩子在 main() 里注册——早于 ServiceStartup。
	// 二者相差一个启动阶段，只有运行期才知道答案。旧的 hide_window_*.go 用平台常量
	// 回答「要不要隐藏」，在 v3（macOS 也有真托盘）已经答错，故删除分叉、改问运行期。
	//
	// 用 atomic 而非普通 bool：写在 ServiceStartup（框架启动 goroutine），读在关窗
	// 钩子（事件循环），跨线程访问。make lint 的 go vet 会拦下裸 bool 的竞态写法。
	trayUp atomic.Bool

	// engine 是本地提醒调度引擎。
	//
	// 【为什么提醒不靠 AI】AI 只在"生成计划"时介入一次；提醒本身必须离线可用、
	// 必须便宜、必须准时——这三条都指向本地定时器。见 internal/reminder 的包注释。
	engine *reminder.Engine

	// pause 是智能免打扰的信号聚合器；nil 表示所有暂停信号都关了。
	// 由 startReminderEngine 装配，ServiceShutdown 里调 Stop 释放轮询 goroutine。
	pause *pause.Bundle

	// sit 是久坐感知运行时（5 秒 tick、Tracker、气泡/预算编排）。
	// 由 startSitRuntime 装配，ServiceShutdown 里先于 pause 停掉。
	sit *sitRuntime
	// presentation 是演示模式的临时状态机；不持久化，重启即关闭。
	// 它只保存状态，暂停/窗口显隐由 App 桥接（见 setPresentationMode）。
	presentation *presentation.Controller
	// pauseMu 串行化 pause 字段的读写与 restartPauseBundle 的整段替换。
	//
	// 【为什么必须有】restartPauseBundle 有「读 a.pause → 写 a.pause → Stop 旧的」三步，
	// 中间没锁的话两个并发的 SetPauseSettings 绑定调用会同时读到 old=A，
	// 然后各自 Stop(A) → close(stop) 第二次会 panic（bundle.Stop 内部虽然已经
	// 用 sync.Once 兜住，但字段读写本身也需要串行化以保证 a.pause 的一致性）。
	pauseMu sync.Mutex

	// restMode 是休息模式状态机（stand/move/rest 三类提醒进入的倒计时流程）。
	// 由 startReminderEngine 构造；restModeCancel 停掉每秒 tick 的自动完成 goroutine。
	restMode       *restmode.Machine
	restModeCancel context.CancelFunc

	// settingsMu 保护下面两个【跨 goroutine 读】的设置快照。
	//
	// 读方：引擎 goroutine 每 20 秒读一次静默时段/推迟分钟/通知开关；绑定方法
	// （生成计划、测试连接）读 AI 连接配置。写方：设置页的保存绑定方法。
	// 三者线程不同，而 config.json 的字段是普通值——不加锁就是数据竞争。
	settingsMu sync.RWMutex

	// reminderCfg 是引擎读取的行为设置快照。
	reminderCfg reminder.Settings

	// sitCfg 是久坐运行时读取的设置快照（开关/阈值/语气/静音日期/宠物可见/静默）。
	sitCfg sitRuntimeSettings

	// aiCfg 是 AI 连接配置快照（含明文密钥，只在后端内部流转）。
	aiCfg ai.Settings
}

// NewApp 创建应用实例，注入服务聚合与配置（由 main.go 完成组合根组装）。
func NewApp(svc *service.Service, cfg *config.Config) *App {
	app := &App{
		svc:          svc,
		cfg:          cfg,
		presentation: presentation.NewController(),
	}
	// 启动前先把设置拷进内存快照：引擎与绑定方法随后都从快照读取，
	// 避免它们在运行期各自去翻 *config.Config（那是并发读写的源头）。
	app.syncSettingsFromConfig()
	return app
}

// attach 注入应用实例。组合根在 application.New 之后、app.Run 之前调用，
// 而 ServiceStartup 与所有绑定方法都发生在 Run 之后，故 a.app 必然已就绪。
//
// 【刻意不导出】：v3 绑定的是导出方法，导出它等于给前端开一个能换掉应用实例的 API。
func (a *App) attach(app *application.App) {
	a.app = app
}

// mainWindowName 是主窗口在 WindowManager 里的注册名（见 main.go 的 NewWithOptions）。
const mainWindowName = "main"

// mainWindow 返回主窗口；窗口尚未创建或已被销毁时返回 nil。
//
// 【为什么按名字查，而不是 app.Window.Current()】Current() 走 native 的「当前活动
// 窗口」查询（darwin 上是 [NSApp keyWindow]）。窗口一旦 Hide() 就不再是 key window，
// 于是 Current() 返回 nil——而「恢复窗口」恰恰发生在窗口隐藏之后。本次报障
// 「关闭后没办法再次显示界面」的成因之一，就是托盘「显示」回调用了 Current()，
// 隐藏态下恒为 nil，于是静默什么都不做。按注册名查不受可见性影响：隐藏的窗口仍在
// WindowManager 的表里（只有销毁才会 Remove，见 window_manager.go 的 Remove）。
func (a *App) mainWindow() application.Window {
	if a.app == nil {
		return nil
	}
	if w, ok := a.app.Window.GetByName(mainWindowName); ok {
		return w
	}
	return nil
}

// petWindow 返回宠物窗口；窗口尚未创建或已被销毁时返回 nil。
//
// 与 mainWindow 同构：按注册名查，而不是 Window.Current()。Current() 走 native 的
// 「当前活动窗口」查询，宠物窗口失去焦点（用户切到别的应用）时它就返回 nil——
// 而宠物的显示/隐藏恰恰最常发生在它非活跃的时候。
//
// 【刻意不导出】：它是窗口对象的内部取值器，导出会被绑定给前端（前端拿到的是
// 序列化后的空对象，毫无意义）。
func (a *App) petWindow() application.Window {
	if a.app == nil {
		return nil
	}
	if w, ok := a.app.Window.GetByName(petWindowName); ok {
		return w
	}
	return nil
}

// allAlertWindows 返回主屏 + 所有副屏的 alert 窗口（US-18 多屏覆盖）。
//
// 主屏永远在结果第一位；副屏按创建顺序（alert-2, alert-3, ...）追加。
// ShowAlert / HideAlert 用它保证「一块屏处理完 = 全部收起」的原子性。
//
// 【为什么不再单独提供 alertWindow()】多屏之后所有调用点都应该是「全部屏幕」；
// 只取主屏的旧入口留着只会诱导下一次改动漏掉副屏。窗口按注册名查而不是
// Window.Current()：强提醒的显示发生在用户操作别的应用时，那一刻本窗口既不可见
// 也不是 key window，Current() 返回 nil。
func (a *App) allAlertWindows() []application.Window {
	if a.app == nil {
		return nil
	}
	wins := make([]application.Window, 0, 4)
	if w, ok := a.app.Window.GetByName(alertWindowName); ok {
		wins = append(wins, w)
	}
	// 副屏数量在应用启动时固定（换屏幕需要重启），从 2 起探到首个不存在的名字为止。
	for i := 2; ; i++ {
		w, ok := a.app.Window.GetByName(AlertSecondaryName(i))
		if !ok {
			break
		}
		wins = append(wins, w)
	}
	return wins
}

// trayRunning 报告系统托盘是否真的起来了。
// 关窗钩子（main.go 注册在 events.Common.WindowClosing 上）据此在【运行期】决定：
//   - true  → 隐藏窗口并拦下销毁，进程驻留托盘（R1）；
//   - false → 放行默认动作（销毁窗口），配合 Mac.ApplicationShouldTerminateAfterLastWindowClosed
//     让进程随之退出，绝不留一个看不见也点不到的无窗口进程（R3，亦即 design.md D13 的结论）。
//
// 【刻意不导出】：App 是注册的 Service，导出方法会被绑定给前端——等于给前端开一个
// 探测/影响关窗策略的 API。同 attach() 的取舍。
func (a *App) trayRunning() bool {
	return a.trayUp.Load()
}

// restoreWindow 让主窗口重新可见并置前。隐藏态下 Show() 会重新显示它。
//
// 【恢复路径的单一实现】：托盘菜单「显示」、托盘双击、macOS 点 Dock 图标
// （ApplicationShouldHandleReopen）、二次启动唤起——四条路径共用本函数。此前各自
// 在回调里各写一段（且都用 Window.Current()），正是「关窗后回不来」的成因。
//
// 记一条日志：下次用户报「点了托盘/重启还是不出来」时，先看这行是否出现，
// 即可区分是「回调没触发」还是「窗口没找到」。
func (a *App) restoreWindow() {
	w := a.mainWindow()
	if w == nil {
		slog.Warn("恢复窗口失败：主窗口不存在或已被销毁")
		return
	}
	w.Show()
	w.UnMinimise()
	slog.Info("窗口已恢复显示")
}

// ServiceStartup 实现 application.Service 的生命周期钩子（对应 v2 的 OnStartup）。
//
// 签名由框架约定（ctx + ServiceOptions），返回 error 会让启动中止——
// 这就是 v2 的 OnStartup 没有的语义：起不来就明确失败，而不是带着半个状态继续跑。
func (a *App) ServiceStartup(_ context.Context, _ application.ServiceOptions) error {
	// 启动系统托盘。v2 时代 macOS 上是 no-op，v3 三平台都有（见 internal/tray 的说明）。
	inst, stop, err := tray.Start(a.app, tray.Options{
		Icon:    trayIcon(),
		Tooltip: appName,
	}, tray.Handlers{
		OnShow: func() {
			// 与 Dock 点击、二次启动共用同一实现（见 restoreWindow）。
			a.restoreWindow()
		},
		OnShowPending: func() {
			// 先拉窗口，再广播一个前端订阅的事件让 Home.vue 滚到 pending 区。
			// 【为什么用事件而不是路由 push】路由 push 需要前端知道 hash 结构；
			// 事件让「菜单栏点了查看待处理」这件事在后端一次决定，前端只需响应。
			a.restoreWindow()
			if a.app != nil {
				a.app.Event.Emit(event.Name("app", event.ActionShowPending), nil)
			}
		},
		OnTogglePresentation: a.togglePresentationMode,
		PresentationActive:   a.isPresentationActive,
		OnQuit: func() {
			a.app.Quit()
		},
		// StatusLines 让菜单栏承载 US-15 的三项进度与 US-17 的暂停原因。
		StatusLines: a.trayStatusLines,
	})
	if err != nil {
		// 托盘起不来不该让整个应用拒绝启动：它只是入口之一，窗口才是主入口。
		// 但必须留下日志——否则用户报「托盘没了」时无从查起。
		slog.Error("启动托盘失败", "err", err)
		return nil
	}
	a.trayStop = stop
	a.trayInst = inst
	// 标记托盘已就绪：关窗钩子据此决定「隐藏」还是「放行退出」（见 main.go 的钩子）。
	a.trayUp.Store(true)
	slog.Info("系统托盘已启动")

	// 注册全局快捷键（唤起/隐藏主窗口）。配置为空 = 用户未启用，直接跳过。
	// 注册失败（快捷键被其他应用占用等）只记日志、不阻断启动——它是增强入口，
	// 不是主流程；错误会在用户下次于设置页保存时如实呈现。
	if acc := a.cfg.ShortcutToggleWindow; strings.TrimSpace(acc) != "" {
		if err := a.registerToggleShortcut(acc); err != nil {
			slog.Error("注册全局快捷键失败", "accelerator", acc, "err", err)
		} else {
			slog.Info("全局快捷键已注册", "accelerator", acc)
		}
	}
	if acc := strings.TrimSpace(a.cfg.ShortcutPresentation); acc != "" {
		if err := a.registerPresentationShortcut(acc); err != nil {
			slog.Error("注册演示模式快捷键失败", "accelerator", acc, "err", err)
		} else {
			slog.Info("演示模式快捷键已注册", "accelerator", acc)
		}
	}

	// 接管系统通知的交互回调（点通知本体 / 点通知上的「完成」「推迟」按钮）。
	// 只设置回调，不触发任何平台调用，故可以在通知服务的 ServiceStartup 之前执行。
	a.bindNotificationResponses()

	// 启动本地提醒调度引擎。放在最后：它一旦跑起来就会立刻 tick 一轮并可能发通知，
	// 而通知需要 ServiceStartup 里的平台初始化先完成——它比本函数注册得晚
	// （见 main.go 的 Services 顺序），故首轮 tick 的通知失败会走"退回卡片"分支。
	a.startReminderEngine()

	// 启动久坐感知：依赖 pause bundle、restMode 与 service，必须在提醒引擎之后。
	a.startSitRuntime()

	// 启动宠物贴边监测：轮询窗口位置，拖到屏幕边缘时自动收起。
	// 放在最后，与提醒链路无依赖——它只读窗口位置。
	a.startPetDockWatcher()
	return nil
}

// ServiceShutdown 实现 application.Service 的关闭钩子（对应 v2 的 OnShutdown）。
//
// 框架保证：收到过 Startup 的服务必然收到 Shutdown（除非关闭过程 panic），
// 且在 Options.OnShutdown 之后、按注册逆序调用。
func (a *App) ServiceShutdown() error {
	// 0. 先停提醒引擎：它会写数据库，必须在关库之前安静下来。
	if a.engine != nil {
		a.engine.Stop()
	}

	// 0.1 停久坐感知：它每 5 秒读窗口/配置并写库，必须在 pause bundle 与数据库关闭前停。
	if a.sit != nil {
		a.sit.stop()
		a.sit = nil
	}

	// 0.3 停智能免打扰的信号采集：锁屏轮询 goroutine 依赖 LockDetector，
	// 引擎停了它继续跑没有意义（也没有消费者）。
	// 读一次拷贝出来再解锁 Stop，避免与并发的 restartPauseBundle 死锁。
	a.pauseMu.Lock()
	bundleToStop := a.pause
	a.pauseMu.Unlock()
	if bundleToStop != nil && bundleToStop.Stop != nil {
		bundleToStop.Stop()
	}

	// 0.4 停休息模式自动完成 ticker：1 秒轮询依赖 svc，svc 关库后它继续跑会
	// 报错；且在关引擎之后已经不会有新的 Enter 请求。
	if a.restModeCancel != nil {
		a.restModeCancel()
		a.restModeCancel = nil
	}

	// 0.5 停宠物贴边监测：它每 500ms 读一次窗口位置，窗口随后会被销毁，
	// 让轮询继续跑只会在退出流程里刷无意义的错误日志。
	a.stopPetDockWatcher()
	// 1. 停托盘
	if a.trayStop != nil {
		a.trayStop()
	}
	// 2. 采集窗口几何并落盘配置（一次 Save 同时写主题、窗口状态与宠物位置）
	a.captureWindowGeometry()
	a.capturePetGeometry()
	if a.cfg != nil {
		if err := a.cfg.Save(); err != nil {
			slog.Error("保存配置失败", "err", err)
		}
	}
	// 3. 关数据库
	if err := a.svc.Close(); err != nil {
		slog.Error("关闭数据库失败", "err", err)
	}
	return nil
}

// captureWindowGeometry 把当前窗口尺寸与最大化状态写入配置，供下次启动还原。
//
// 注意：窗口【最大化时不去读尺寸】——此时读到的是屏幕尺寸，回填后用户下次取消最大化
// 会得到一个占满屏幕的"还原"尺寸。保持上次的非最大化尺寸才符合直觉。
// 读失败或窗口不可用不阻断关闭：窗口几何是体验优化，不该让退出流程失败。
func (a *App) captureWindowGeometry() {
	if a.cfg == nil {
		return
	}
	w := a.mainWindow()
	if w == nil {
		return
	}
	maximised := w.IsMaximised()
	if !maximised {
		if width, height := w.Size(); width > 0 && height > 0 {
			a.cfg.WindowWidth, a.cfg.WindowHeight = width, height
		}
	}
	a.cfg.WindowMaximised = maximised

	// 落盘前记一条：用户报「窗口大小没记住」时，先看这行是否出现、值是否合理。
	slog.Info("窗口几何已记录",
		"width", a.cfg.WindowWidth, "height", a.cfg.WindowHeight, "maximised", maximised)
}

// capturePetGeometry 把宠物窗口当前位置写入配置，供下次启动还原。
//
// 与 captureWindowGeometry 同一套取舍：窗口不存在或读不到位置时直接跳过，
// 不让退出流程失败——记不住位置只是每次回到默认落点，不该演变成关不掉应用。
//
// 持 petMu：此刻可能仍有设置页的绑定调用在改窗口尺寸，串行化避免读到中间态。
func (a *App) capturePetGeometry() {
	if a.cfg == nil {
		return
	}
	a.petMu.Lock()
	defer a.petMu.Unlock()
	w := a.petWindow()
	if w == nil {
		return
	}
	x, y := w.Position()
	// 扩窗中退出：持久化的必须是基础几何。否则下次启动会按扩出来的矩形
	// 还原窗口，宠物凭空下移一段。
	if a.petBubbleActive {
		base := a.petCurrentBaseLocked(w)
		x, y = base.X, base.Y
	}
	// 收起态下窗口在屏幕边缘外，直接存会得到一对屏幕外坐标；下次启动时
	// petRestorePosition 会判定"这块位置没有屏幕"，把宠物丢回主屏右下角——
	// 等于用户这次拖动白费。故收起时存展开位置。
	a.petDockMu.Lock()
	if a.petDock.collapsed && a.petDock.side != "" {
		x, y = a.petDock.homeX, a.petDock.homeY
	}
	a.petDockMu.Unlock()
	a.cfg.PetX, a.cfg.PetY = x, y
	slog.Info("宠物窗口位置已记录", "x", x, "y", y)
}

// ---------------------------------------------------------------------------
// 绑定方法：原生对话框
// ---------------------------------------------------------------------------

// SelectFile 弹出选择文件对话框，返回所选路径。
func (a *App) SelectFile(title string) (string, error) {
	return dialog.SelectFile(a.app, title)
}

// SelectDirectory 弹出选择目录对话框，返回所选路径。
func (a *App) SelectDirectory(title string) (string, error) {
	return dialog.SelectDirectory(a.app, title)
}

// SelectSaveFile 弹出保存文件对话框，返回目标路径。
func (a *App) SelectSaveFile(title, defaultFilename string) (string, error) {
	return dialog.SelectSaveFile(a.app, title, defaultFilename)
}

// Confirm 弹出确认框，返回用户是否确认。
func (a *App) Confirm(title, message string) (bool, error) {
	return dialog.Confirm(a.app, title, message)
}

// ---------------------------------------------------------------------------
// 绑定方法：配置
// ---------------------------------------------------------------------------

// GetTheme 返回当前主题设置。
func (a *App) GetTheme() string {
	return a.cfg.Theme
}

// SetTheme 保存主题设置并写回配置文件。
// 入参非法返回 apperr.Validation（契约：业务错误经 apperr 构造，前端据 code 分流）。
func (a *App) SetTheme(theme string) error {
	// 合法值：dark / light / ""（空串 = 未设置，语义为"跟随系统"，主题三档）。
	if theme != "" && theme != "dark" && theme != "light" {
		return apperr.Validation("主题取值非法，仅支持 dark/light")
	}
	a.cfg.Theme = theme
	if err := a.cfg.Save(); err != nil {
		// 落盘失败是系统错误，经 Wrap 归一化为 internal（原始错误保留在 Detail）。
		return apperr.Wrap(err)
	}
	return nil
}

// ShouldShowOnboarding 判断启动时是否进入首次引导。
//
// 【为什么在这里自动补标记】升级用户没有 onboarding_completed 键，但已经有
// 场景；若只按标记判断，他们会被关进一个本不需要的向导。这里以「有场景」
// 作为既成事实，补写标记后放行。落盘失败只记日志：不能因为配置文件不可写
// 就把已有数据的用户挡在引导里。
func (a *App) ShouldShowOnboarding() (bool, error) {
	if a.cfg.OnboardingCompleted {
		return false, nil
	}
	scenarios, err := a.svc.ListScenarios()
	if err != nil {
		return false, apperr.Wrap(err)
	}
	if len(scenarios) > 0 {
		a.cfg.OnboardingCompleted = true
		if saveErr := a.cfg.Save(); saveErr != nil {
			slog.Warn("自动补齐首次引导完成标记失败", "error", saveErr)
		}
		return false, nil
	}
	return true, nil
}

// CompleteOnboarding 记录首次引导已完成；完成与跳过走同一条路径。
func (a *App) CompleteOnboarding() error {
	a.cfg.OnboardingCompleted = true
	if err := a.cfg.Save(); err != nil {
		return apperr.Wrap(err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// 绑定方法：应用数据目录
// ---------------------------------------------------------------------------

// GetDataDir 返回应用数据目录的绝对路径（数据库、配置、日志同目录）。
// 供前端展示/复制，便于用户自行定位日志。
func (a *App) GetDataDir() (string, error) {
	dir, err := appdir.Dir(appName)
	if err != nil {
		return "", apperr.Wrap(err)
	}
	return dir, nil
}

// OpenDataDir 在系统文件管理器中打开应用数据目录。
func (a *App) OpenDataDir() error {
	dir, err := appdir.Dir(appName)
	if err != nil {
		return apperr.Wrap(err)
	}
	if err := reveal.Open(dir); err != nil {
		return apperr.Wrap(err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// 绑定方法：应用信息与数据库导出
// ---------------------------------------------------------------------------

// GetAppInfo 返回版本、平台与关键路径，供「关于」界面与排障使用。
// 排障时让用户念一次本方法的返回值，版本/日志位置就都齐了。
func (a *App) GetAppInfo() (appinfo.Info, error) {
	info, err := appinfo.Current(appName)
	if err != nil {
		return appinfo.Info{}, apperr.Wrap(err)
	}
	return info, nil
}

// GetLaunchArgs 返回本次启动的启动参数（不含可执行文件路径）。
//
// 二次启动（应用已在运行时再次唤起）的参数【不】经此返回，而是经
// `app:second-instance` 事件推送——那一次前端已经挂载，事件不会丢。
func (a *App) GetLaunchArgs() []string {
	if len(os.Args) <= 1 {
		return []string{}
	}
	// 返回副本：os.Args 是全局切片，直接把内部状态交出去不合适。
	out := make([]string, len(os.Args)-1)
	copy(out, os.Args[1:])
	return out
}

// ExportDatabase 把数据库导出到 targetPath（通常来自 SelectSaveFile）。
// includeSitProfile=false 时副本不含应用与久坐明细；CheckIn 与提醒数据保留。
// 目标已存在会被覆盖（用户已在系统保存对话框确认过）；位于数据目录内会被拒绝。
func (a *App) ExportDatabase(targetPath string, includeSitProfile bool) error {
	return a.svc.ExportDatabase(appName, targetPath, includeSitProfile)
}

// ImportDatabase 用 sourcePath（通常来自 SelectFile，即此前导出的备份）替换当前数据库。
// 失败自动回滚到导入前状态（见 internal/service/import.go）；成功后建议重启应用，
// 让各页面以新库数据重新加载（前端在恢复成功提示中说明）。
//
// 导入会整体替换数据库；先停久坐采样，成功后收尾备份里可能残留的开放会话并重置
// 内存 Tracker，避免旧 session/id 继续写进新库。
func (a *App) ImportDatabase(sourcePath string) error {
	if a.svc == nil {
		return apperr.New(apperr.CodeInternal, "服务尚未初始化")
	}
	running := a.sit != nil && a.sit.cancel != nil
	if running {
		a.sit.stop()
	}
	err := a.svc.ImportDatabase(appName, sourcePath)
	if a.sit != nil {
		if err == nil {
			if n, recErr := a.svc.RecoverOpenAppUsageSessions(); recErr != nil {
				slog.Warn("恢复数据库后收尾应用会话失败", "err", recErr)
			} else if n > 0 {
				slog.Info("恢复数据库后收尾应用会话", "count", n)
			}
			if n, recErr := a.svc.RecoverOpenSitSessions(); recErr != nil {
				slog.Warn("恢复数据库后收尾久坐会话失败", "err", recErr)
			} else if n > 0 {
				slog.Info("恢复数据库后收尾久坐会话", "count", n)
			}
			a.sit.resetRuntimeState()
		}
		if running {
			a.sit.start()
		}
	}
	return err
}

// ActionSecondInstance 是「应用被二次启动」的事件动作（单一真相）。
// 前端镜像在 frontend/src/lib/app.ts 的 AppEvent，由 internal/guard/parity_test.go 断言一致。
const ActionSecondInstance = "second-instance"

// SecondInstancePayload 是 `app:second-instance` 事件的载荷。
// 前端侧的类型镜像见 frontend/src/lib/app.ts 的 SecondInstancePayload，字段名需一致。
type SecondInstancePayload struct {
	// Args 二次启动时的命令行参数。
	Args []string `json:"args"`
	// WorkingDirectory 二次启动时的工作目录。
	WorkingDirectory string `json:"working_dir"`
}

// notifySecondInstance 把二次启动的参数推给前端（事件名走事件契约 <domain>:<action>）。
//
// v2 时代这里有一句 `if a.ctx == nil { warn; return }` 的防御分支——那是在防
// 「startup 还没跑，ctx 还没存」这个状态。v3 里应用实例由组合根显式注入
// （见 attach），而二次启动回调只可能在应用跑起来之后触发，这个状态不存在了，
// 分支随之删除。少一个不可能发生的分支。
func (a *App) notifySecondInstance(args []string, workDir string) {
	a.app.Event.Emit(event.Name("app", ActionSecondInstance), SecondInstancePayload{
		Args:             args,
		WorkingDirectory: workDir,
	})
}

// ---------------------------------------------------------------------------
// 绑定方法：剪贴板
// ---------------------------------------------------------------------------

// SetClipboardText 把文本写入系统剪贴板。v3 的 Clipboard.SetText 以 bool 报告
// 结果而非 error，false（平台写入失败）在此归一化为错误返回。
func (a *App) SetClipboardText(text string) error {
	if !a.app.Clipboard.SetText(text) {
		return apperr.New(apperr.CodeInternal, "写入剪贴板失败")
	}
	return nil
}

// GetClipboardText 读取系统剪贴板文本。
// 剪贴板内容不是文本（如用户刚复制的图片）时返回错误——这比返回空串诚实，
// 调用方能区分「剪贴板为空」和「剪贴板里没有文本」。
func (a *App) GetClipboardText() (string, error) {
	text, ok := a.app.Clipboard.Text()
	if !ok {
		return "", apperr.New(apperr.CodeInternal, "读取剪贴板失败：内容为空或不是文本")
	}
	return text, nil
}

// ---------------------------------------------------------------------------
// 绑定方法：系统通知
// ---------------------------------------------------------------------------

// RequestNotificationPermission 请求系统通知授权，返回是否授予。
// 首次调用会弹系统授权框；macOS dev 态（无打包 bundle）会返回平台错误，如实上抛。
func (a *App) RequestNotificationPermission() (bool, error) {
	if a.notify == nil {
		return false, apperr.New(apperr.CodeInternal, "通知服务未注册")
	}
	granted, err := a.notify.RequestNotificationAuthorization()
	if err != nil {
		return false, apperr.Wrap(err)
	}
	return granted, nil
}

// SendNotification 发送一条系统通知。
func (a *App) SendNotification(title, body string) error {
	if a.notify == nil {
		return apperr.New(apperr.CodeInternal, "通知服务未注册")
	}
	if strings.TrimSpace(title) == "" {
		return apperr.Validation("通知标题不能为空")
	}
	// ID 带时间戳：同 ID 的通知在部分平台会被合并/更新，这里要的是逐条送达。
	opts := notifications.NotificationOptions{
		ID:    fmt.Sprintf("app-%d", time.Now().UnixMilli()),
		Title: title,
		Body:  body,
	}
	if err := a.notify.SendNotification(opts); err != nil {
		return apperr.Wrap(err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// 绑定方法：开机自启
// ---------------------------------------------------------------------------

// GetAutostartEnabled 返回系统里真实的自启注册状态（macOS: SMAppService /
// launchd；Windows: 注册表 Run 键；Linux: .desktop 文件）——查系统，不查内存。
func (a *App) GetAutostartEnabled() (bool, error) {
	enabled, err := a.app.Autostart.IsEnabled()
	if err != nil {
		return false, apperr.Wrap(err)
	}
	return enabled, nil
}

// SetAutostart 启用/停用开机自启。
// macOS 注意：SMAppService 只对打包后的 .app 有效，dev 态调用会返回平台错误。
func (a *App) SetAutostart(enabled bool) error {
	var err error
	if enabled {
		// Identifier 留空：由框架按 bundle id（macOS）/应用名（Win/Linux）派生，
		// 与安装形态保持一致，不自己另起一套命名。
		err = a.app.Autostart.EnableWithOptions(application.AutostartOptions{})
	} else {
		err = a.app.Autostart.Disable()
	}
	if err != nil {
		return apperr.Wrap(err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// 绑定方法：全局快捷键（唤起/隐藏主窗口）
// ---------------------------------------------------------------------------

// registerToggleShortcut 把「唤起/隐藏主窗口」快捷键注册为 acc（空串=注销）。
// 调用方持有 shortcutMu 的语义见 registerShortcut。
func (a *App) registerToggleShortcut(acc string) error {
	a.shortcutMu.Lock()
	defer a.shortcutMu.Unlock()

	fallback := ""
	if a.cfg != nil {
		fallback = a.cfg.ShortcutToggleWindow
	}
	return a.registerShortcut(&a.shortcutAcc, acc, fallback, a.toggleMainWindow)
}

// registerPresentationShortcut 把「切换演示模式」快捷键注册为 acc（空串=注销）。
func (a *App) registerPresentationShortcut(acc string) error {
	a.shortcutMu.Lock()
	defer a.shortcutMu.Unlock()

	fallback := ""
	if a.cfg != nil {
		fallback = a.cfg.ShortcutPresentation
	}
	return a.registerShortcut(&a.presentationShortcutAcc, acc, fallback, a.togglePresentationMode)
}

// registerShortcut 是两条全局快捷键的唯一注册实现。
//
// 先注销旧注册再注册新的；新串失败时尽力恢复 fallback，保证系统状态与调用前一致。
// 调用方必须持有 shortcutMu。
func (a *App) registerShortcut(current *string, acc, fallback string, callback func()) error {
	if *current != "" {
		// 注销失败不阻断重注册：快捷键可能已被系统侧清掉（注销返回错误但目标本来就是空的）。
		_ = a.app.GlobalShortcut.Unregister(*current)
	}
	*current = ""

	if acc == "" {
		return nil
	}
	if err := a.app.GlobalShortcut.Register(acc, callback); err != nil {
		if prev := strings.TrimSpace(fallback); prev != "" {
			if reErr := a.app.GlobalShortcut.Register(prev, callback); reErr != nil {
				slog.Error("回滚全局快捷键注册失败", "accelerator", prev, "err", reErr)
			} else {
				*current = prev
			}
		}
		return err
	}
	*current = acc
	return nil
}

// SetToggleWindowShortcut 设置「唤起/隐藏主窗口」全局快捷键并落盘。
// 空串 = 关闭该快捷键。非法串（注册失败，常见原因：被其他应用占用）不落盘。
func (a *App) SetToggleWindowShortcut(acc string) error {
	acc = strings.TrimSpace(acc)

	// 与 SetPresentationShortcut 同样：配置写入放在 shortcutMu 内，
	// 与 registerToggleShortcut 回滚时对旧值的读取保持一致。
	a.shortcutMu.Lock()
	err := a.registerShortcut(&a.shortcutAcc, acc, a.cfg.ShortcutToggleWindow, a.toggleMainWindow)
	if err == nil {
		a.cfg.ShortcutToggleWindow = acc
	}
	a.shortcutMu.Unlock()

	if err != nil {
		// 面向用户的提示 + 原始错误保留在 Detail（排查"被谁占用"要用）。
		slog.Error("注册全局快捷键失败", "accelerator", acc, "err", err)
		ae := apperr.Wrap(err)
		ae.Message = "注册快捷键失败（可能已被其他应用占用）"
		return ae
	}
	if err := a.cfg.Save(); err != nil {
		return apperr.Wrap(err)
	}
	return nil
}

// GetToggleWindowShortcut 返回当前生效的全局快捷键（空串 = 未启用）。
func (a *App) GetToggleWindowShortcut() string {
	a.shortcutMu.Lock()
	defer a.shortcutMu.Unlock()
	return a.shortcutAcc
}

// SetPresentationShortcut 设置「切换演示模式」全局快捷键并落盘。
// 空串 = 关闭。注册失败不落盘，返回面向用户的错误。
func (a *App) SetPresentationShortcut(acc string) error {
	acc = strings.TrimSpace(acc)

	// 配置写入与 GetPresentationShortcut / registerPresentationShortcut 的读取
	// 共用 shortcutMu，避免设置页加载与保存并发时出现字符串头竞态。
	a.shortcutMu.Lock()
	err := a.registerShortcut(&a.presentationShortcutAcc, acc, a.cfg.ShortcutPresentation, a.togglePresentationMode)
	if err == nil {
		a.cfg.ShortcutPresentation = acc
	}
	a.shortcutMu.Unlock()

	if err != nil {
		slog.Error("注册演示模式快捷键失败", "accelerator", acc, "err", err)
		ae := apperr.Wrap(err)
		ae.Message = "注册快捷键失败（可能已被其他应用或唤起主窗快捷键占用）"
		return ae
	}
	if err := a.cfg.Save(); err != nil {
		return apperr.Wrap(err)
	}
	return nil
}

// GetPresentationShortcut 返回配置的演示快捷键与当前是否已注册。
//
// 配置值与注册状态分开返回：启动时注册失败也要让用户看到自己配置了什么，
// 否则设置页会把一个"配置了但没生效"的快捷键显示成空白。
func (a *App) GetPresentationShortcut() config.PresentationShortcut {
	a.shortcutMu.Lock()
	defer a.shortcutMu.Unlock()

	out := config.PresentationShortcut{}
	if a.cfg != nil {
		// 手改配置可能带空格；启动注册用的是 trim 后的值，回显也统一 trim。
		out.Accelerator = strings.TrimSpace(a.cfg.ShortcutPresentation)
	}
	out.Registered = strings.TrimSpace(a.presentationShortcutAcc) != "" &&
		a.presentationShortcutAcc == out.Accelerator
	return out
}

// toggleMainWindow 是全局快捷键的回调：窗口可见 → 隐藏到托盘（托盘不在则不动，
// 避免重演「无窗口进程」——R3 的教训）；不可见 → 恢复显示（与托盘/Dock/二次启动共用实现）。
func (a *App) toggleMainWindow() {
	w := a.mainWindow()
	if w == nil {
		return
	}
	if w.IsVisible() {
		if a.trayRunning() {
			w.Hide()
			slog.Info("全局快捷键：窗口已隐藏到托盘")
		}
		return
	}
	a.restoreWindow()
}

// ---------------------------------------------------------------------------
// 绑定方法：MCP Token
// ---------------------------------------------------------------------------

// GetMCPToken 返回已配置的 MCP Bearer token（空串 = 未设置，MCP 走上游默认的
// loopback 免鉴权）。dev-mcp 场景下重启应用后生效（token 在启动期经环境变量透传）。
func (a *App) GetMCPToken() string {
	return a.cfg.MCPToken
}

// RotateMCPToken 生成并落盘一个新的随机 token（64 位 hex），返回新值。
// 注意：只改配置，【不热更新】正在运行的 MCP 服务——重启后生效。
func (a *App) RotateMCPToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		ae := apperr.Wrap(err)
		ae.Message = "生成随机 token 失败"
		return "", ae
	}
	token := hex.EncodeToString(buf)
	a.cfg.MCPToken = token
	if err := a.cfg.Save(); err != nil {
		return "", apperr.Wrap(err)
	}
	return token, nil
}

// ---------------------------------------------------------------------------
// 提醒领域：设置快照与引擎生命周期
// ---------------------------------------------------------------------------

// syncSettingsFromConfig 把 config.json 里的两份运行期设置拷进内存快照。
//
// 每次保存设置后调用一次。之所以要快照而不是直接读 cfg：
// 引擎与绑定方法在各自的 goroutine 里读，而 config.Config 的字段是普通值，
// 直接读就是数据竞争；拷成一份由 settingsMu 保护的快照后，读路径只有一把读锁。
func (a *App) syncSettingsFromConfig() {
	if a.cfg == nil {
		return
	}
	reminderSettings := reminder.Settings{
		QuietStart:             a.cfg.QuietStart,
		QuietEnd:               a.cfg.QuietEnd,
		SnoozeMinutes:          a.cfg.SnoozeMinutes,
		NotifyEnabled:          a.cfg.NotifyEnabled,
		NaturalPauseEnabled:    a.cfg.NaturalPauseEnabled,
		NaturalPauseMaxSeconds: a.cfg.NaturalPauseMaxSeconds,
	}
	aiSettings := a.cfg.AISettings()
	sitSettings := a.cfg.LoadSitSettings()
	sitCfg := sitRuntimeSettings{
		Enabled:          sitSettings.Enabled,
		ProfileEnabled:   sitSettings.ProfileEnabled,
		ThresholdMinutes: sitSettings.ThresholdMinutes,
		Tone:             sitSettings.Tone,
		MutedDate:        a.cfg.SitMutedDate,
		PetVisible:       a.cfg.PetVisible,
		QuietStart:       a.cfg.QuietStart,
		QuietEnd:         a.cfg.QuietEnd,
	}

	a.settingsMu.Lock()
	a.reminderCfg = reminderSettings
	a.sitCfg = sitCfg
	a.aiCfg = aiSettings
	a.settingsMu.Unlock()
}

// currentSitRuntimeSettings 返回久坐运行时的设置快照。
func (a *App) currentSitRuntimeSettings() sitRuntimeSettings {
	a.settingsMu.RLock()
	defer a.settingsMu.RUnlock()
	return a.sitCfg
}

// currentReminderSettings 返回提醒行为设置快照。
func (a *App) currentReminderSettings() reminder.Settings {
	a.settingsMu.RLock()
	defer a.settingsMu.RUnlock()
	return a.reminderCfg
}

// currentAISettings 返回 AI 连接配置快照（含明文密钥，只在后端内部使用）。
func (a *App) currentAISettings() ai.Settings {
	a.settingsMu.RLock()
	defer a.settingsMu.RUnlock()
	return a.aiCfg
}

// emitReminderChanged 广播"提醒数据变了"，让各页面重新拉取。
//
// 打卡、推迟、计划编辑、场景切换都走它：前端各页面各自持有一份列表视图，
// 没有这条广播就只能靠轮询或用户手动刷新。
func (a *App) emitReminderChanged(reason string) {
	if a.app == nil {
		return
	}
	a.app.Event.Emit(reminder.ChangedEventName(), reminder.ChangedPayload{Reason: reason})
}

// startReminderEngine 组装并启动本地提醒引擎。
//
// 【为什么在 App 而不是 main.go】引擎需要三样东西：service（持久化能力）、
// config 里的行为设置、notifications 服务。组合根把前两个交给了 App，
// 第三个也挂在 App 上（见 main.go 里 notifySvc 的注入），故 App 是唯一
// 同时握有三者的地方。start/stop 也因此挂在 ServiceStartup/ServiceShutdown 上。
func (a *App) startReminderEngine() {
	if a.svc == nil {
		return
	}
	a.syncSettingsFromConfig()
	// 装配智能免打扰信号（空闲 + 锁屏）。所有开关都在设置里可关；
	// 装配失败（未来若引入依赖外部服务的 detector）只记日志、不阻断引擎启动——
	// 免打扰缺席只是少了便利，引擎缺席是整个提醒功能消失。
	bundle, err := pause.NewBundle(bundleConfigFrom(a.cfg))
	if err != nil {
		slog.Error("装配智能免打扰失败，提醒将不受暂停影响", "err", err)
	} else {
		a.applyPresentationToBundle(bundle)
		a.pauseMu.Lock()
		a.pause = bundle
		a.pauseMu.Unlock()
		a.svc.SetPauseService(service.NewPauseService(bundle.Provider))
	}
	var provider pause.Provider
	if bundle := a.pauseSnapshot(); bundle != nil {
		provider = bundle.Provider
	}
	a.engine = reminder.NewEngine(reminder.Options{
		Store:         a.svc,
		Settings:      a.currentReminderSettings,
		Notifier:      &reminderNotifier{notify: a.notify},
		Emitter:       appEmitter{app: a.app},
		PauseProvider: provider,
		Activity:      systemActivity{},
		RestMode:      a.ensureRestMode(),
	})
	a.engine.Start()
	slog.Info("提醒引擎已启动", "tick", reminder.TickInterval.String())
}

// ensureRestMode 惰性构造 restmode.Machine 并挂一个「倒计时到 0 自动完成」的
// 1 秒 ticker goroutine。返回 Machine 引用；重复调用不会重建。
//
// 【为什么用 1 秒 tick 而不是让前端调 Complete】US-11 承诺倒计时结束【自动】记
// 完成 CheckIn——如果依赖前端 Alert.vue 的 tick，用户关掉窗口或断网时状态会
// 永远停在 Countdown，下次开机也不会自愈。服务端 tick 是唯一可靠的兜底。
//
// 【1 秒 tick 是否浪费】Idle 状态下每次 tick 只做两次判断（state + remaining），
// 一秒钟可以忽略；比让引擎 TickInterval 从 20s 缩到 1s 划算得多。
func (a *App) ensureRestMode() *restmode.Machine {
	if a.restMode != nil {
		return a.restMode
	}
	a.restMode = restmode.NewMachine()
	ctx, cancel := context.WithCancel(context.Background())
	a.restModeCancel = cancel
	go a.restModeAutoComplete(ctx)
	return a.restMode
}

// restModeAutoComplete 是 1 秒 tick 的 goroutine：Countdown 到点自动落 CheckIn + 广播。
//
// 单次动作失败不终止循环（比如 DB 短暂不可用），下一轮还会重试——
// 直到状态转走或用户手动介入。
func (a *App) restModeAutoComplete(ctx context.Context) {
	// 循环里的 panic 兜住：这条 goroutine 挂了 = 自动完成失效 = 用户看到
	// 「倒计时到 0 却没记完成」，是典型的没有症状的失效。
	defer func() {
		if r := recover(); r != nil {
			slog.Error("休息模式自动完成循环异常退出", "panic", r)
		}
	}()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.tickRestMode()
			a.tickPresentation(time.Now())
		}
	}
}

func (a *App) tickRestMode() {
	if a.restMode == nil {
		return
	}
	// 菜单栏文案在 Countdown 期间每秒变化（mm:ss）；其他状态下大多不变。
	// 用 refreshTrayIfChanged 兜住「每秒重建 NSMenu」的浪费——文本没变就不动它。
	a.refreshTrayIfChanged()
	st := a.restMode.State()
	if st != restmode.StateCountdown {
		return
	}
	if a.restMode.Remaining() > 0 {
		return
	}
	// 倒计时到点：把状态从 Countdown 转到 Done；再落 CheckIn；再 Reset。
	// 三步分开的原因见 machine.Reset 的注释——落库失败时状态还是 Done，
	// 用户可以重试；一失败就回 Idle 会让 CheckIn 丢。
	if !a.restMode.Complete() {
		return
	}
	cur := a.restMode.Current()
	if cur.FireID != 0 && a.svc != nil {
		if err := a.svc.CompleteReminder(cur.FireID); err != nil {
			if !restCompletionSettled(err) {
				slog.Warn("休息模式自动记完成失败", "fire_id", cur.FireID, "err", err)
				// 状态保持 Done 但不 Reset；下一轮 tick 重试。
				return
			}
			// 已被宠物、首页或系统通知提前处理：会话已经结算，照常 Reset。
			slog.Info("休息模式自动完成：该提醒已结算，视为完成", "fire_id", cur.FireID)
		}
	}
	a.restMode.Reset()
	a.emitRestModeState()
	// 自动完成同样改变了待处理数据；不广播 changed 会让宠物一直挂着这条
	// 已被写成 done 的提醒——用户再点完成只会得到「该提醒已处理」。
	a.emitReminderChanged(reminder.ReasonCheckIn)
	// Done→Idle 的过渡在 1 秒内跑完，前端可能永远看不到 Done 状态；
	// 单独广播 completed 让 UI 播一次「做完了」的瞬时反馈（US-11）。
	if a.app != nil {
		a.app.Event.Emit("restmode:completed", cur.FireID)
	}
}

// restCompletionSettled 判断休息模式自动完成的错误是否表示「会话已结算」。
//
// validation（该提醒已处理）与 not_found（记录不存在）说明这条已被宠物、首页或
// 系统通知提前处理——会话已经没有未结算的事实，可以照常 Reset。内部错误必须
// 保持 Done 等待重试，否则 CheckIn 会静默丢失。
func restCompletionSettled(err error) bool {
	if err == nil {
		return false
	}
	switch apperr.Wrap(err).Code {
	case apperr.CodeValidation, apperr.CodeNotFound:
		return true
	default:
		return false
	}
}

// refreshTrayIfChanged 只在状态文本变化时重建菜单，避免每秒无谓刷 NSMenu。
//
// 【为什么要缓存】macOS 的 SetMenu 会让菜单栏图标闪一下；空闲/无休息时每秒刷
// 会看到菜单栏在抖。加一层 lastStatus 判等，代价是一次字符串比较，收益是稳定。
func (a *App) refreshTrayIfChanged() {
	a.refreshTray(false)
}

// refreshTray 重建菜单。force=true 用于「状态行没变、但菜单项文案变了」的场景
// （例如进入/退出演示模式时，状态行可能仍被休息模式占用）。
func (a *App) refreshTray(force bool) {
	if a.trayInst == nil {
		return
	}
	next := strings.Join(a.trayStatusLines(), "\n")
	a.trayMu.Lock()
	changed := next != a.lastTrayStatus
	if changed {
		a.lastTrayStatus = next
	}
	a.trayMu.Unlock()
	if changed || force {
		a.trayInst.Refresh()
	}
}

// emitRestModeState 把当前休息模式快照广播给前端（Alert.vue 订阅）。
func (a *App) emitRestModeState() {
	if a.app == nil || a.restMode == nil {
		return
	}
	a.app.Event.Emit("restmode:state", a.restMode.Snapshot())
	// 菜单栏文案随状态变化（预警 / 陪做 / 空闲）——US-9 / US-15 / US-17 的落地。
	a.refreshTrayIfChanged()
}

// trayStatusText 返回托盘菜单顶部的一行状态文本（三优先级）。
//
// 优先展示【正在影响提醒投递】的状态：
//  1. 休息模式 Countdown —「陪你休息 mm:ss」（此刻用户已被覆盖层锁住）
//  2. 休息模式 Warning —「1 分钟后休息」（软提示）
//  3. 智能免打扰 —「会议中 / 空闲 / 锁屏 / ...」
//
// 三个都不成立时返回空串，让 tray 侧隐藏顶部行。
func (a *App) trayStatusText() string {
	if a.restMode != nil {
		snap := a.restMode.Snapshot()
		switch snap.State {
		case restmode.StateCountdown:
			return formatTrayRemaining("陪你休息", snap.RemainingSec)
		case restmode.StateWarning:
			return "1 分钟后休息"
		}
	}
	if bundle := a.pauseSnapshot(); bundle != nil {
		if st := bundle.Provider.State(); st.IsPaused() {
			label := pauseReasonZh(st.Reason)
			if st.Reason == pause.ReasonPresentation && !captureProtectionSupported() {
				label += "（捕获排除不可用）"
			}
			return "免打扰：" + label
		}
	}
	return ""
}

// trayStatusLines 返回托盘菜单顶部的多行状态文本（US-15 + US-17）。
//
// 布局：
//
//	今日 X/Y · 待处理 N            ← US-15 的「今日进度 + 待处理数量」
//	下一条 HH:MM · 标题             ← US-15 的「下一条提醒时间」
//	陪你休息 mm:ss / 免打扰：xxx    ← US-17 的暂停原因（仅在暂停 / 休息时出现）
//
// 【为什么把 US-15 的三项拆两行】macOS NSMenuItem 不会自动折行；一行塞太多会挤。
// 拆两行后每行都在 20 字符内，视觉上像一张「今日速览卡」。
// 【Stats 拉不到怎么办】返回单行「今日 — · 待处理 —」，让菜单不至于因为一次
// 查询失败变成空白（用户会以为菜单坏了）。
//
// 【为什么 stats 要缓存】本函数被 refreshTrayIfChanged 每秒调一次；
// GetTodayStats 一次跑 3–4 条 SQL。1 秒 1 次对 SQLite 来说不致命，但完全没必要——
// 完成/待处理计数的变化频率是「用户点一次」级别，不是秒级。5 秒 TTL
// 放在这里既保证状态行的暂停/休息段仍然秒级更新，又把 SQL 压到 1/5。
func (a *App) trayStatusLines() []string {
	lines := make([]string, 0, 3)

	if a.svc != nil {
		statsLines := a.cachedTrayStatsLines()
		lines = append(lines, statsLines...)
	}

	if status := a.trayStatusText(); status != "" {
		lines = append(lines, status)
	}
	return lines
}

// trayStatsTTL 是今日进度的缓存寿命。5 秒是「用户点完打卡回菜单能看到更新」的上界。
const trayStatsTTL = 5 * time.Second

// cachedTrayStatsLines 缓存 GetTodayStats 的结果，按 trayStatsTTL 过期。
func (a *App) cachedTrayStatsLines() []string {
	nowT := time.Now()
	a.trayMu.Lock()
	if nowT.Sub(a.trayStatsAt) < trayStatsTTL && a.trayStatsLines != nil {
		out := a.trayStatsLines
		a.trayMu.Unlock()
		return out
	}
	a.trayMu.Unlock()

	lines := make([]string, 0, 2)
	stats, err := a.svc.GetTodayStats()
	if err != nil || stats == nil {
		lines = append(lines, "今日 — · 待处理 —")
	} else {
		if stats.TargetTotal > 0 {
			lines = append(lines, fmt.Sprintf("今日 %d/%d · 待处理 %d",
				stats.DoneTotal, stats.TargetTotal, stats.PendingCount))
		} else {
			lines = append(lines, fmt.Sprintf("今日 %d · 待处理 %d",
				stats.DoneTotal, stats.PendingCount))
		}
		if stats.NextFireAt > 0 {
			next := time.Unix(stats.NextFireAt, 0).Format("15:04")
			title := stats.NextTitle
			// 中文按 rune 截，避免 emoji / 汉字被 UTF-8 半字节切断。
			if rs := []rune(title); len(rs) > 12 {
				title = string(rs[:11]) + "…"
			}
			lines = append(lines, fmt.Sprintf("下一条 %s · %s", next, title))
		}
	}

	a.trayMu.Lock()
	a.trayStatsLines = lines
	a.trayStatsAt = nowT
	a.trayMu.Unlock()
	return lines
}

// pauseSnapshot 是 a.pause 的原子读；restartPauseBundle 会在锁下换引用，
// 读方一律走这里避免看到中间态。
func (a *App) pauseSnapshot() *pause.Bundle {
	a.pauseMu.Lock()
	defer a.pauseMu.Unlock()
	return a.pause
}

// formatTrayRemaining 把剩余秒数拼成「陪你休息 mm:ss」，用于菜单栏文案。
func formatTrayRemaining(label string, sec int) string {
	if sec < 0 {
		sec = 0
	}
	m := sec / 60
	s := sec % 60
	return label + " " + padTwo(m) + ":" + padTwo(s)
}

func padTwo(n int) string {
	if n < 10 {
		return "0" + itoa(n)
	}
	return itoa(n)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// pauseReasonZh 把 pause.PauseReason 翻成中文。
//
// 【为什么在 App 而不是 pause 包】pause 包保持纯逻辑；文案是 UI 层的责任。
// 三处（宠物气泡、菜单栏、Alert 覆盖层）都各自决定怎么呈现，不在数据层混入。
func pauseReasonZh(r pause.PauseReason) string {
	switch r {
	case pause.ReasonPresentation:
		return "演示中"
	case pause.ReasonIdle:
		return "空闲"
	case pause.ReasonLock:
		return "锁屏"
	case pause.ReasonMeeting:
		return "会议中"
	case pause.ReasonFullscreen:
		return "全屏中"
	case pause.ReasonFocus:
		return "专注模式"
	case pause.ReasonCalendar:
		return "日历忙碌"
	}
	return "免打扰"
}

// ---------------------------------------------------------------------------
// 绑定方法：场景
// ---------------------------------------------------------------------------

// ListScenarios 返回全部场景，最近创建的在前。
func (a *App) ListScenarios() ([]model.Scenario, error) {
	list, err := a.svc.ListScenarios()
	if err != nil {
		return nil, apperr.Wrap(err)
	}
	return list, nil
}

// GetActiveScenario 返回当前启用的场景；未启用任何场景时返回 nil。
func (a *App) GetActiveScenario() (*model.Scenario, error) {
	scenario, err := a.svc.ActiveScenario()
	if err != nil {
		return nil, apperr.Wrap(err)
	}
	return scenario, nil
}

// CreateScenario 新建场景（默认不启用）。
func (a *App) CreateScenario(name, description string) (*model.Scenario, error) {
	scenario, err := a.svc.CreateScenario(name, description)
	if err != nil {
		return nil, apperr.Wrap(err)
	}
	a.emitReminderChanged(reminder.ReasonScenarioChanged)
	return scenario, nil
}

// UpdateScenario 修改场景的名称与描述。
func (a *App) UpdateScenario(id uint, name, description string) error {
	if err := a.svc.UpdateScenario(id, name, description); err != nil {
		return apperr.Wrap(err)
	}
	a.emitReminderChanged(reminder.ReasonScenarioChanged)
	return nil
}

// SetScenarioEnabled 启用/停用场景。启用会自动停用其他场景（通知不能多路轰炸）。
func (a *App) SetScenarioEnabled(id uint, enabled bool) error {
	if err := a.svc.SetScenarioEnabled(id, enabled); err != nil {
		return apperr.Wrap(err)
	}
	a.emitReminderChanged(reminder.ReasonScenarioChanged)
	return nil
}

// DeleteScenario 删除场景，并级联清理其提醒项、触发记录与打卡记录。
func (a *App) DeleteScenario(id uint) error {
	if err := a.svc.DeleteScenario(id); err != nil {
		return apperr.Wrap(err)
	}
	a.emitReminderChanged(reminder.ReasonScenarioChanged)
	return nil
}

// GenerateScenarioPlan 调用 AI 为指定场景生成整套提醒计划，并替换原有提醒项。
//
// 这是 AI 在运行期唯一出现的地方：生成一次，之后全靠本地调度。
func (a *App) GenerateScenarioPlan(id uint) (*service.GeneratePlanResult, error) {
	result, err := a.svc.GeneratePlan(a.currentAISettings(), id)
	if err != nil {
		return nil, apperr.Wrap(err)
	}
	a.emitReminderChanged(reminder.ReasonPlanChanged)
	return result, nil
}

// ListScenarioTemplates 返回内置场景模板目录（确定性内容，不调用 AI）。
func (a *App) ListScenarioTemplates() scenariotemplate.Catalog {
	return a.svc.ListScenarioTemplates()
}

// PreviewScenarioTemplate 返回按工作时段与关注目标个性化后的提醒预览；不写库。
func (a *App) PreviewScenarioTemplate(key, workStart, workEnd string, goals []string) (*scenariotemplate.Preview, error) {
	preview, err := a.svc.PreviewScenarioTemplate(key, workStart, workEnd, goals)
	if err != nil {
		return nil, apperr.Wrap(err)
	}
	return preview, nil
}

// ApplyScenarioTemplate 把模板复制成普通场景及提醒项；成功后广播数据变更。
//
// enable=true 只由首次引导使用：它沿用场景单选语义，会停用其他场景；
// 场景页路径传 false，避免静默替换用户当前启用的计划。
func (a *App) ApplyScenarioTemplate(key, workStart, workEnd string, goals []string, enable bool) (*service.ApplyTemplateResult, error) {
	result, err := a.svc.ApplyScenarioTemplate(key, workStart, workEnd, goals, enable)
	if err != nil {
		return nil, apperr.Wrap(err)
	}
	a.emitReminderChanged(reminder.ReasonPlanChanged)
	return result, nil
}

// GeneratePlanFromInput 用「自然语言描述 + 附件」生成一份提醒计划草稿（不落库）。
//
// 【为什么传路径而不是字节】前端用 SelectFile 拿到的就是路径；把大文件先读进
// 渲染层再经 IPC 搬到 Go 侧，等于同一份内容搬两遍、内存里存两份。
// 【为什么返回草稿而不是直接落库】生成可能不合用户意，直接写库会替换掉现有计划；
// 草稿由前端预览、用户确认后再逐条 CreateReminder（复用带完整校验的写入路径）。
// 【失败提示的分工】未配 Key / 附件格式不支持由 service 给出可执行的 apperr；
// 模型不支持图片则由端点返回 4xx，其原始 message 会进 Detail 供排查。
func (a *App) GeneratePlanFromInput(description string, filePaths []string) (*ai.Plan, error) {
	plan, err := a.svc.GeneratePlanFromInput(a.currentAISettings(), description, filePaths)
	if err != nil {
		return nil, apperr.Wrap(err)
	}
	return plan, nil
}

// GenerateQuickAddDrafts 把一句话 + 可选附件解析成快速添加草稿（不落库）。
//
// 与 Plan 页的 GeneratePlanFromInput 分开：快速添加按「绝对时刻」产出草稿，
// 不与任何场景绑定；确认前用户可以逐条编辑。filePaths 走磁盘读取，inlineImages
// 是粘贴/拖拽进来的图片字节（不落盘）。
func (a *App) GenerateQuickAddDrafts(description string, filePaths []string, inlineImages []service.InlineImage) (*ai.QuickAddPlan, error) {
	plan, err := a.svc.GenerateQuickAddDrafts(a.currentAISettings(), description, filePaths, inlineImages)
	if err != nil {
		return nil, apperr.Wrap(err)
	}
	return plan, nil
}

// ---------------------------------------------------------------------------
// 绑定方法：提醒项
// ---------------------------------------------------------------------------

// ListReminders 返回指定场景下的全部提醒项（含已停用的）。
func (a *App) ListReminders(scenarioID uint) ([]model.Reminder, error) {
	list, err := a.svc.ListReminders(scenarioID)
	if err != nil {
		return nil, apperr.Wrap(err)
	}
	return list, nil
}

// ListOneOffs 返回未触发与已错过的一次性提醒（不依赖场景）。
func (a *App) ListOneOffs() ([]service.OneOffItem, error) {
	list, err := a.svc.ListOneOffs()
	if err != nil {
		return nil, apperr.Wrap(err)
	}
	return list, nil
}

// CreateReminder 新增一条提醒项。
func (a *App) CreateReminder(input service.ReminderInput) (*model.Reminder, error) {
	item, err := a.svc.CreateReminder(input)
	if err != nil {
		return nil, apperr.Wrap(err)
	}
	a.emitReminderChanged(reminder.ReasonPlanChanged)
	return item, nil
}

// CreateReminders 原子创建一批提醒项（快速添加确认草稿）。
func (a *App) CreateReminders(inputs []service.ReminderInput) ([]model.Reminder, error) {
	items, err := a.svc.CreateReminders(inputs)
	if err != nil {
		return nil, apperr.Wrap(err)
	}
	a.emitReminderChanged(reminder.ReasonPlanChanged)
	return items, nil
}

// UpdateReminder 编辑一条提醒项。
func (a *App) UpdateReminder(input service.ReminderInput) error {
	if err := a.svc.UpdateReminder(input); err != nil {
		return apperr.Wrap(err)
	}
	a.emitReminderChanged(reminder.ReasonPlanChanged)
	return nil
}

// DeleteReminder 删除一条提醒项（同时清掉它名下未处理的触发记录）。
func (a *App) DeleteReminder(id uint) error {
	if err := a.svc.DeleteReminder(id); err != nil {
		return apperr.Wrap(err)
	}
	a.emitReminderChanged(reminder.ReasonPlanChanged)
	return nil
}

// SetReminderEnabled 启用/停用单条提醒项。
func (a *App) SetReminderEnabled(id uint, enabled bool) error {
	if err := a.svc.SetReminderEnabled(id, enabled); err != nil {
		return apperr.Wrap(err)
	}
	a.emitReminderChanged(reminder.ReasonPlanChanged)
	return nil
}

// ---------------------------------------------------------------------------
// 绑定方法：今日统计与打卡
// ---------------------------------------------------------------------------

// GetTodayStats 返回仪表盘所需的今日聚合数据。
func (a *App) GetTodayStats() (*service.TodayStats, error) {
	stats, err := a.svc.GetTodayStats()
	if err != nil {
		return nil, apperr.Wrap(err)
	}
	return stats, nil
}

// GetPetGrowth 返回宠物「芽」的生长档位（今日完成率驱动，极轻量，供宠物窗口轮询）。
func (a *App) GetPetGrowth() (*service.PetGrowth, error) {
	g, err := a.svc.PetGrowth()
	if err != nil {
		return nil, apperr.Wrap(err)
	}
	return g, nil
}

// ListPendingFires 返回待处理的提醒（首页卡片）。
func (a *App) ListPendingFires() ([]service.PendingFire, error) {
	list, err := a.svc.ListPendingFires()
	if err != nil {
		return nil, apperr.Wrap(err)
	}
	return list, nil
}

// ListMissedToday 返回今日已错过的提醒（首页「已错过」分组）。
//
// 【与 US-6 对应】智能免打扰期间被跳过的每日单点与一次性提醒在此可见；
// 不补弹、不投递，只保留「今天确实有过这条安排」的回执。
func (a *App) ListMissedToday() ([]service.MissedFire, error) {
	list, err := a.svc.ListMissedToday()
	if err != nil {
		return nil, apperr.Wrap(err)
	}
	return list, nil
}

// CompleteReminder 把一条提醒标成「完成」。
func (a *App) CompleteReminder(fireID uint) error {
	if err := a.svc.CompleteReminder(fireID); err != nil {
		return apperr.Wrap(err)
	}
	a.emitReminderChanged(reminder.ReasonCheckIn)
	return nil
}

// SnoozeReminder 推迟一条提醒。推迟分钟数取「设置页 → 推迟分钟数」。
func (a *App) SnoozeReminder(fireID uint) error {
	minutes := a.currentReminderSettings().SnoozeMinutes
	if err := a.svc.SnoozeReminder(fireID, minutes); err != nil {
		return apperr.Wrap(err)
	}
	a.emitReminderChanged(reminder.ReasonCheckIn)
	return nil
}

// SkipReminder 跳过一条提醒（记一条打卡，但不计入今日完成数）。
func (a *App) SkipReminder(fireID uint) error {
	if err := a.svc.SkipReminder(fireID); err != nil {
		return apperr.Wrap(err)
	}
	a.emitReminderChanged(reminder.ReasonCheckIn)
	return nil
}

// ---------------------------------------------------------------------------
// 绑定方法：AI 配置
// ---------------------------------------------------------------------------

// GetAISettings 返回脱敏后的 AI 连接配置（绝不返回明文密钥）。
func (a *App) GetAISettings() (ai.PublicSettings, error) {
	return ai.NewPublicSettings(a.currentAISettings()), nil
}

// SetAISettings 保存 AI 连接配置。apiKey 传空串表示"保持原值不变"——
// 前端手里只有脱敏串，用户不改密钥时无法回传明文（见 config.SaveAISettings）。
func (a *App) SetAISettings(baseURL, model, apiKey string) error {
	if err := a.cfg.SaveAISettings(baseURL, model, apiKey); err != nil {
		return apperr.Wrap(err)
	}
	a.syncSettingsFromConfig()
	return nil
}

// TestAIConnection 用当前配置试连一次 AI 服务，返回可展示的成功说明。
func (a *App) TestAIConnection() (string, error) {
	message, err := a.svc.TestAIConnection(a.currentAISettings())
	if err != nil {
		return "", apperr.Wrap(err)
	}
	return message, nil
}

// ---------------------------------------------------------------------------
// 绑定方法：提醒行为设置（静默时段 / 推迟分钟数 / 通知开关）
// ---------------------------------------------------------------------------

// GetReminderSettings 返回当前提醒行为设置。
func (a *App) GetReminderSettings() (config.ReminderSettings, error) {
	return a.cfg.LoadReminderSettings(), nil
}

// SetReminderSettings 保存提醒行为设置。
func (a *App) SetReminderSettings(in config.ReminderSettings) error {
	if err := a.cfg.SaveReminderSettings(in); err != nil {
		return apperr.Wrap(err)
	}
	a.syncSettingsFromConfig()
	return nil
}

// ---------------------------------------------------------------------------
// 绑定方法：桌面宠物
//
// 宠物是「提醒的化身」：到点提醒时它切到等待动画并冒气泡说话。故这一组方法
// 只做三件事——列素材、取素材、控窗口；提醒状态本身由前端订阅既有事件得到
// （reminder:fired / reminder:changed），后端不为宠物新增事件。
// ---------------------------------------------------------------------------

// ListPetPacks 返回全部可用宠物包（内置 + 数据目录导入的 + Codex 客户端已装的）。
func (a *App) ListPetPacks() ([]service.PetPack, error) {
	packs, err := a.svc.ListPetPacks(appName)
	if err != nil {
		return nil, apperr.Wrap(err)
	}
	return packs, nil
}

// GetPetSprite 返回指定宠物包精灵图的 data URI（内置包没有精灵图，会返回错误）。
func (a *App) GetPetSprite(packID string) (string, error) {
	sprite, err := a.svc.GetPetSprite(appName, packID)
	if err != nil {
		return "", apperr.Wrap(err)
	}
	return sprite, nil
}

// ImportPetPack 让用户选一个宠物包文件（pet.json 或精灵图），复制进应用数据目录。
//
// 返回 nil（无错误）表示用户取消了选择——那不是失败，前端应当安静地什么都不做。
// 对话框在绑定层弹（service 层不认识 wails 的对话框，它只有文件系统）。
func (a *App) ImportPetPack() (*service.PetPack, error) {
	path, err := dialog.SelectFile(a.app, "选择宠物包文件（pet.json 或 spritesheet.webp）")
	if err != nil {
		return nil, apperr.Wrap(err)
	}
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	pack, importErr := a.svc.ImportPetPack(appName, path)
	if importErr != nil {
		return nil, apperr.Wrap(importErr)
	}
	return &pack, nil
}

// GetPetSettings 返回宠物设置（开关 / 缩放 / 选中的素材包）。
func (a *App) GetPetSettings() (config.PetSettings, error) {
	return a.cfg.LoadPetSettings(), nil
}

// petShouldBeVisible 是宠物窗口是否应显示的唯一判断：
// 用户显隐偏好 && 当前不在演示模式。
//
// 演示模式只改运行期可见性，不改写 cfg.PetVisible——用户把宠物藏起来的意图
// 不该被一次会议覆盖。
func (a *App) petShouldBeVisible() bool {
	if a.cfg == nil {
		return false
	}
	if a.presentation != nil && a.presentation.Snapshot().Active {
		return false
	}
	return a.cfg.PetVisible
}

// applyPetPresentationVisibility 在演示模式切换时同步宠物窗口。
//
// 【为什么不调用 setPetVisible】那会写配置。这里只 Show/Hide 当前窗口；
// 也不 restorePetFromDockLocked——贴边收起是用户的状态，退出演示后应原样恢复。
func (a *App) applyPetPresentationVisibility() {
	a.petMu.Lock()
	defer a.petMu.Unlock()
	w := a.petWindow()
	if w == nil {
		return
	}
	if a.petShouldBeVisible() {
		w.Show()
		return
	}
	w.Hide()
}

// SetPetSettings 保存宠物设置，并【立即】作用到宠物窗口：
// 开关决定显示/隐藏，缩放决定窗口尺寸（画布放大后窗口不放大就会被裁掉）。
//
// 返回一句面向用户的提示（前端直接显示），失败经 apperr 上抛。
func (a *App) SetPetSettings(in config.PetSettings) (string, error) {
	a.petMu.Lock()
	defer a.petMu.Unlock()

	w := a.petWindow()
	wasBubble := a.petBubbleActive
	budget := a.petBubbleBudget
	// 扩窗中改缩放：先用【旧】缩放把窗口还原到基础几何。SavePetSettings 之后
	// cfg 里的 scale 就是新值，再反算会把旧矩形按新尺寸解释，锚点会漂。
	if w != nil && wasBubble {
		a.restorePetBubbleLocked(w)
	}
	if err := a.cfg.SavePetSettings(in); err != nil {
		return "", apperr.Wrap(err)
	}
	a.syncSettingsFromConfig()

	if w != nil {
		if in.PetVisible {
			if a.petShouldBeVisible() {
				// 同 setPetVisible：先还原到展开位置，否则 Show 出来的是屏幕外那块。
				a.restorePetFromDockLocked(w)
				w.Show()
			}
			// 即使演示模式隐藏着，也把尺寸/位置更新好，退出时不会带着旧尺寸出现。
			old := petWindowRect(w)
			width, height := petWindowSize(a.cfg.PetScaleValue())
			x := old.X + (old.W-width)/2
			y := old.Y + old.H - height
			w.SetSize(width, height)
			w.SetPosition(x, y)
			if wasBubble {
				// 保持气泡形态：用旧预算按新基础高度重算，前端随后会再校准。
				a.expandPetBubbleLocked(w, height+budget)
			}
			if !a.petShouldBeVisible() {
				w.Hide()
			}
		} else {
			w.Hide()
		}
	}
	slog.Info("宠物设置已保存", "visible", in.PetVisible, "scale", a.cfg.PetScaleValue())
	return "已保存", nil
}

// ---------------------------------------------------------------------------
// 绑定方法：久坐感知
// ---------------------------------------------------------------------------

// GetSitSnapshot 返回宠物窗口轮询的久坐快照。
//
// 【不返回 error】这是 4 秒一次的轮询；内部 DB 异常只记日志并返回上一轮可见的
// 空快照，避免轮询把错误日志刷爆。需要错误可见的写在设置/动作绑定里。
func (a *App) GetSitSnapshot() sit.Snapshot {
	if a.sit == nil {
		return sit.Snapshot{
			State:             sit.StateIdle,
			PlatformSupported: pause.IdleSupported(),
		}
	}
	return a.sit.snapshot()
}

// GetSitSettings 返回久坐设置（只读字段按当前平台补齐）。
func (a *App) GetSitSettings() (config.SitSettings, error) {
	if a.cfg == nil {
		return config.SitSettings{}, apperr.New(apperr.CodeInternal, "配置尚未初始化")
	}
	settings := a.cfg.LoadSitSettings()
	settings.PlatformSupported = pause.IdleSupported()
	settings.ProfileStatus = ""
	if !settings.PlatformSupported {
		settings.ProfileStatus = sit.ProfileStatusUnsupported
	} else if settings.ProfileEnabled {
		if a.sit != nil {
			settings.ProfileStatus = a.sit.currentProfileStatus()
			if settings.ProfileStatus == "" {
				settings.ProfileStatus = sit.ProfileStatusNoData
			}
		} else {
			settings.ProfileStatus = sit.ProfileStatusNoData
		}
	}
	return settings, nil
}

// SetSitSettings 保存久坐设置；运行时下一次 tick 生效。
func (a *App) SetSitSettings(in config.SitSettings) error {
	if a.cfg == nil {
		return apperr.New(apperr.CodeInternal, "配置尚未初始化")
	}
	oldTone := sit.NormalizeTone(a.cfg.SitTone)
	if err := a.cfg.SaveSitSettings(in); err != nil {
		return apperr.Wrap(err)
	}
	a.syncSettingsFromConfig()
	// 语气切换后旧语气的待展示气泡不再弹出，下一条用新语气。
	if oldTone != sit.NormalizeTone(string(in.Tone)) && a.svc != nil {
		_ = a.svc.ExpireSitSpeeches(time.Now())
	}
	return nil
}

// SetSitProfileEnabled 记录应用画像同意：开启/关闭都写 asked；关闭时可删除历史。
func (a *App) SetSitProfileEnabled(enabled bool, deleteHistory bool) error {
	if a.sit == nil {
		return apperr.New(apperr.CodeInternal, "久坐感知尚未就绪")
	}
	return a.sit.setProfileEnabled(enabled, deleteHistory)
}

// AckSitBubble 确认一条宠物久坐气泡已被真正渲染。
func (a *App) AckSitBubble(id string) bool {
	if a.svc == nil {
		return false
	}
	bubbleID := parseSitBubbleID(id)
	if bubbleID == 0 {
		return false
	}
	ok, err := a.svc.AckSitSpeech(bubbleID, time.Now())
	if err != nil {
		slog.Warn("确认久坐气泡失败", "err", err)
		return false
	}
	return ok
}

// TakeActiveBreak 处理「起来走走」：清零久坐、记录休息；目标抵扣在 004 接入。
func (a *App) TakeActiveBreak(bubbleID string) (sit.SitActionResult, error) {
	if a.sit == nil {
		return sit.SitActionResult{}, apperr.New(apperr.CodeInternal, "久坐感知尚未就绪")
	}
	return a.sit.takeActiveBreak(bubbleID)
}

// SnoozeSitNudge 处理「再坐 10 分钟」。
func (a *App) SnoozeSitNudge(bubbleID string) (sit.SitActionResult, error) {
	if a.sit == nil {
		return sit.SitActionResult{}, apperr.New(apperr.CodeInternal, "久坐感知尚未就绪")
	}
	return a.sit.snooze(bubbleID)
}

// MuteSitToday 处理「今天先这样」。
func (a *App) MuteSitToday() (sit.SitActionResult, error) {
	if a.sit == nil {
		return sit.SitActionResult{}, apperr.New(apperr.CodeInternal, "久坐感知尚未就绪")
	}
	return a.sit.muteToday()
}

// SetSitTone 切换宠物语气（「别这样说」/设置页共用）。
func (a *App) SetSitTone(tone string) (sit.SitActionResult, error) {
	if a.sit == nil {
		return sit.SitActionResult{}, apperr.New(apperr.CodeInternal, "久坐感知尚未就绪")
	}
	return a.sit.setTone(tone)
}

// SetAppCategory 保存用户对某个应用的行为类别纠正；历史与后续视图使用新类别。
func (a *App) SetAppCategory(platform string, appID string, category string) error {
	if a.svc == nil {
		return apperr.New(apperr.CodeInternal, "服务尚未初始化")
	}
	if err := a.svc.SetAppCategory(platform, appID, category); err != nil {
		return err
	}
	if a.sit != nil {
		a.sit.invalidateAppCategory()
	}
	return nil
}

// GetSitDay 返回某天的久坐/休息聚合；开放会话由 GetSitSnapshot 补充。
func (a *App) GetSitDay(date string) (service.SitDay, error) {
	if a.svc == nil {
		return service.SitDay{}, apperr.New(apperr.CodeInternal, "服务尚未初始化")
	}
	return a.svc.GetSitDay(date)
}

// GetSitTrend 返回最近 days 天（含今天）的久坐日聚合。
func (a *App) GetSitTrend(days int) ([]service.SitDay, error) {
	if a.svc == nil {
		return nil, apperr.New(apperr.CodeInternal, "服务尚未初始化")
	}
	return a.svc.GetSitTrend(days)
}

// ClearSitProfile 清空应用画像与久坐数据；CheckIn 与连续天数保留。
func (a *App) ClearSitProfile() error {
	if a.sit != nil {
		return a.sit.clearProfileData()
	}
	if a.svc == nil {
		return apperr.New(apperr.CodeInternal, "服务尚未初始化")
	}
	return a.svc.ClearSitProfile()
}

// SetPetBubbleVisible 切换宠物窗口的气泡形态。
//
// true：按气泡需要加宽到目标宽度，高度维持基础值（随后前端会调
// SetPetBubbleHeight 按实测高度加高）。false：恢复基础尺寸与位置。
// 扩窗是运行期状态，不写配置；贴边收起时忽略（气泡此时本来就不显示）。
func (a *App) SetPetBubbleVisible(visible bool) error {
	a.petMu.Lock()
	defer a.petMu.Unlock()

	w := a.petWindow()
	if w == nil {
		return apperr.NotFound("宠物窗口尚未创建")
	}
	if visible {
		// 用户把宠物关掉、或正在演示模式时不做扩窗。不用 w.IsVisible()：macOS 的 occlusionState
		// 会把「可见但非前台」的宠物窗口报成不可见（实测启动阶段恒为 false），
		// 用它会把适配整个拦掉。petShouldBeVisible 才是用户的真实意图。
		if !a.petShouldBeVisible() {
			return nil
		}
		if a.petDockCollapsed() {
			return nil
		}
		a.expandPetBubbleLocked(w, 0)
		return nil
	}
	a.restorePetBubbleLocked(w)
	return nil
}

// SetPetBubbleHeight 把宠物窗口高度调整到气泡所需的总高度（含宠物与内边距）。
//
// 只在气泡形态下生效；total 小于基础高度时按基础高度处理。
func (a *App) SetPetBubbleHeight(total int) error {
	a.petMu.Lock()
	defer a.petMu.Unlock()

	w := a.petWindow()
	if w == nil {
		return apperr.NotFound("宠物窗口尚未创建")
	}
	if !a.petBubbleActive {
		return nil
	}
	// 与 SetPetBubbleVisible 同一道门槛：用户把宠物关掉或正在演示模式时，
	// 不再改隐藏窗口的尺寸。
	if !a.petShouldBeVisible() {
		return nil
	}
	a.expandPetBubbleLocked(w, total)
	return nil
}

// ExpandPet 把贴边收起的宠物展开回原位置（宠物窗口里鼠标划过窄边时调用）。
//
// 【为什么绑定方法必须写在 app.go】本仓的护栏（internal/guard/bindings_test.go）
// 只扫描 app.go 来与生成的绑定产物对账。写在别的文件里，方法本身能编译、wails3
// 也能生成，但护栏会报「产物里有、app.go 里没有」的过期误报。
// 贴边状态机本身仍在 pet_dock.go。
//
// 保留 side：展开之后鼠标移开还要能收回去，所以这里只把 collapsed 置回 false。
func (a *App) ExpandPet() error {
	a.petDockMu.Lock()
	state := a.petDock
	a.petDockMu.Unlock()

	if state.side == "" || !state.collapsed {
		return nil
	}

	a.petMu.Lock()
	defer a.petMu.Unlock()
	w := a.petWindow()
	if w == nil {
		return apperr.New(apperr.CodeInternal, "宠物窗口不存在")
	}
	w.SetPosition(state.homeX, state.homeY)

	a.petDockMu.Lock()
	a.petDock.collapsed = false
	a.petDockMu.Unlock()
	// 展开后的位置仍贴着边，必须忽略一段时间的检测，否则下一轮轮询会立刻
	// 把它吸附回去（见 App.petDockIgnoreUntil）。
	a.petDockIgnoreUntil.Store(time.Now().Add(petDockExpandGrace).UnixMilli())

	slog.Info("宠物已贴边展开", "side", state.side)
	a.emitPetDock("")
	return nil
}

// CollapsePet 把宠物收回到上次贴的那个边（宠物窗口里鼠标移开一段时间后调用）。
//
// 只有在"曾经贴过边且当前是展开态"时才动作：从未贴过边的宠物不该因为
// 鼠标划过就突然跑到屏幕外去。
func (a *App) CollapsePet() error {
	a.petDockMu.Lock()
	state := a.petDock
	a.petDockMu.Unlock()

	if state.side == "" || state.collapsed {
		return nil
	}

	a.petMu.Lock()
	defer a.petMu.Unlock()
	w := a.petWindow()
	if w == nil {
		return apperr.New(apperr.CodeInternal, "宠物窗口不存在")
	}
	w.SetPosition(state.dockX, state.dockY)

	a.petDockMu.Lock()
	a.petDock.collapsed = true
	a.petDockMu.Unlock()

	slog.Info("宠物已贴边收起（悬停结束）", "side", state.side)
	a.emitPetDock(state.side)
	return nil
}

// ShowPet 显示宠物窗口（并记住"用户希望它显示"）。
func (a *App) ShowPet() error {
	return a.setPetVisible(true)
}

// HidePet 隐藏宠物窗口（并记住"用户希望它隐藏"）。
func (a *App) HidePet() error {
	return a.setPetVisible(false)
}

// TogglePet 在显示/隐藏之间切换宠物窗口。
func (a *App) TogglePet() error {
	return a.setPetVisible(!a.cfg.PetVisible)
}

// GetPauseState 返回当前暂停状态（前端轮询用）。
//
// 宠物窗口每 4 秒轮询一次，用于显示「我先不打扰」气泡；菜单栏同理。
// 未启用智能免打扰时返回 Paused=false，但仍带回演示模式字段。
func (a *App) GetPauseState() service.PauseState {
	state := service.PauseState{Paused: false, Reason: string(pause.ReasonNone)}
	if a.svc != nil && a.svc.Pause() != nil {
		state = a.svc.Pause().GetState()
	}
	if a.presentation == nil {
		return state
	}
	snap := a.presentation.Snapshot()
	state.PresentationActive = snap.Active
	state.PresentationSource = string(snap.Source)
	state.SuggestionVisible = snap.SuggestionVisible
	if !snap.ExitedAt.IsZero() {
		state.PresentationExitedAt = snap.ExitedAt.Unix()
	}
	// 建议与捕获排除的字段在这里补齐；建议在 006 接入，捕获排除是平台能力。
	state.CaptureExclusionSupported = captureProtectionSupported()
	return state
}

// GetDeferralState 返回自然停顿顺延状态（宠物气泡用）。
func (a *App) GetDeferralState() service.DeferralState {
	out := service.DeferralState{Supported: pause.IdleSupported()}
	if a.engine == nil {
		return out
	}
	snap := a.engine.DeferralState()
	out.Active = snap.Active
	if !snap.Since.IsZero() {
		out.Since = snap.Since.Unix()
	}
	return out
}

// SetPresentationMode 进入/退出演示模式。
//
// source 只在进入时使用：manual / suggested；退出时忽略。
func (a *App) SetPresentationMode(active bool, source string) error {
	return a.setPresentationMode(active, source)
}

// setPresentationMode 是绑定方法与托盘共用的唯一实现。
func (a *App) setPresentationMode(active bool, source string) error {
	if a.presentation == nil {
		return apperr.New(apperr.CodeInternal, "演示模式状态未初始化")
	}
	if active {
		src := presentation.Source(source)
		if !src.Valid() {
			return apperr.Validation("未知的演示模式来源: " + source)
		}
		a.presentation.Enter(src, time.Now())
	} else {
		a.presentation.Exit(time.Now())
	}
	a.applyPresentationToPause()
	if active {
		a.handleRestModeOnPresentationEnter()
	}
	a.applyPetPresentationVisibility()
	if active {
		if err := a.HideAlert(); err != nil {
			// 启动早期 / headless 下强提醒窗口可能尚未创建；不让它阻断演示模式。
			slog.Warn("进入演示模式时隐藏强提醒失败", "err", err)
		}
	}
	a.refreshTray(true)
	snap := a.presentation.Snapshot()
	slog.Info("演示模式已切换", "active", snap.Active, "source", snap.Source)
	return nil
}

// isPresentationActive 是演示模式的只读判断，供托盘与 Go 侧强提醒兜底复用。
func (a *App) isPresentationActive() bool {
	return a.presentation != nil && a.presentation.Snapshot().Active
}

// DismissPresentationSuggestion 记录用户忽略了本次「进入演示模式」建议。
func (a *App) DismissPresentationSuggestion() error {
	if a.presentation == nil {
		return nil
	}
	a.presentation.DismissSuggestion()
	return nil
}

// presentationTrigger 返回当前会议/全屏触发。
//
// 直接读 Meeting/Fullscreen 探测器，而不是 CompositeProvider.State()：
// 演示模式激活时 composite 会返回 presentation，底层触发就看不见了；自动退出
// 需要知道会议/全屏是否仍在。这里同时尊重用户对各信号的开关。
func (a *App) presentationTrigger() presentation.Trigger {
	bundle := a.pauseSnapshot()
	if bundle == nil || a.cfg == nil {
		return presentation.TriggerNone
	}
	if a.cfg.PauseMeetingEnabled && bundle.Meeting != nil &&
		bundle.Meeting.Detect() == pause.ReasonMeeting {
		return presentation.TriggerMeeting
	}
	if a.cfg.PauseFullscreenEnabled && bundle.Fullscreen != nil &&
		bundle.Fullscreen.Detect() == pause.ReasonFullscreen {
		return presentation.TriggerFullscreen
	}
	return presentation.TriggerNone
}

// tickPresentation 每秒推进建议/自动退出状态。
//
// 只有建议进入的演示模式会自动退出；手动进入的保持到用户手动退出。
func (a *App) tickPresentation(now time.Time) {
	if a.presentation == nil {
		return
	}
	suggestEnabled := a.cfg != nil && a.cfg.PresentationSuggestEnabled
	if a.presentation.Observe(a.presentationTrigger(), suggestEnabled, now) {
		// 控制器已经把状态切到 off；这里只做与手动退出相同的可见性收尾。
		a.applyPresentationToPause()
		a.applyPetPresentationVisibility()
		a.refreshTray(true)
		slog.Info("演示模式自动退出")
	}
}

// handleRestModeOnPresentationEnter 进入演示模式时结束进行中的休息会话。
//
// 预警只取消、不记录；倒计时走与「提前结束 → 不算」相同的路径，写 CheckIn(skip)
// 并归零，不弹确认框。用户此刻在会议/共享屏幕里，没有条件完成一次拉伸。
func (a *App) handleRestModeOnPresentationEnter() {
	if a.restMode == nil {
		return
	}
	switch a.restMode.State() {
	case restmode.StateWarning:
		a.restMode.CancelWarning()
	case restmode.StateCountdown:
		if _, err := a.SkipRestMode(); err != nil {
			slog.Warn("进入演示模式时跳过休息模式失败", "err", err)
		}
	}
}

// togglePresentationMode 是托盘菜单的入口。
func (a *App) togglePresentationMode() {
	active := !a.isPresentationActive()
	if err := a.setPresentationMode(active, string(presentation.SourceManual)); err != nil {
		slog.Error("切换演示模式失败", "err", err)
	}
}

// applyPresentationToPause 把控制器状态同步到当前 pause bundle。
//
// restartPauseBundle 会换掉整个 bundle；新 bundle 的 Presentation 默认关闭，
// 故重建后必须再同步一次，否则「保存免打扰设置会把演示模式丢掉」。
func (a *App) applyPresentationToPause() {
	if a.presentation == nil {
		return
	}
	active := a.presentation.Snapshot().Active
	a.pauseMu.Lock()
	defer a.pauseMu.Unlock()
	if a.pause != nil && a.pause.Presentation != nil {
		a.pause.Presentation.SetActive(active)
	}
}

// applyPresentationToBundle 在 bundle 装配完成、尚未替换前同步演示状态。
func (a *App) applyPresentationToBundle(b *pause.Bundle) {
	if b == nil || b.Presentation == nil || a.presentation == nil {
		return
	}
	b.Presentation.SetActive(a.presentation.Snapshot().Active)
}

// GetRestModeSnapshot 返回当前休息模式快照（Alert.vue 每次 tick 查询）。
//
// Idle 时返回一个只带 State 字段的空快照；前端拿到 State=idle 就收起覆盖层。
func (a *App) GetRestModeSnapshot() restmode.Snapshot {
	if a.restMode == nil {
		return restmode.Snapshot{State: restmode.StateIdle}
	}
	return a.restMode.Snapshot()
}

// CompleteRestMode 用户手动确认「算完成」：写 CheckIn(done) + 状态归零。
//
// 【为什么允许从 Countdown 与 Done 两种状态调用】
//   - Countdown：用户在倒计时中点「提前结束 → 算完成」（US-12）；
//   - Done：自动完成流程里 CheckIn 落库失败时，UI 显示「重试」按钮走这条路。
//
// Skip 状态下调用是 no-op——已经跳过的会话不能再改成完成，
// 否则用户「跳过」后又「完成」会把 CheckIn 表搞混。
func (a *App) CompleteRestMode() (string, error) {
	if a.restMode == nil {
		return "", nil
	}
	// Countdown 时先转 EarlyCompleted，让日志与自动完成区分开。
	if a.restMode.State() == restmode.StateCountdown {
		a.restMode.EarlyComplete()
	} else if a.restMode.State() != restmode.StateDone {
		// Skipped/Idle：不动。
		return "", nil
	}
	cur := a.restMode.Current()
	if cur.FireID != 0 && a.svc != nil {
		if err := a.svc.CompleteReminder(cur.FireID); err != nil {
			return "", apperr.Wrap(err)
		}
	}
	a.restMode.Reset()
	a.emitRestModeState()
	return "已完成", nil
}

// SkipRestMode 用户点「提前结束 → 不算」：写 CheckIn(skip) + 状态归零。
//
// Skip 与 Complete 都能从 Countdown 出发；从 Done（自然到点）出发不允许跳过——
// 用户已经"做完了"，再点跳过等于撤回一次打卡，产品语义会打脸。
func (a *App) SkipRestMode() (string, error) {
	if a.restMode == nil {
		return "", nil
	}
	if a.restMode.State() != restmode.StateCountdown {
		return "", nil
	}
	a.restMode.Skip()
	cur := a.restMode.Current()
	if cur.FireID != 0 && a.svc != nil {
		if err := a.svc.SkipReminder(cur.FireID); err != nil {
			return "", apperr.Wrap(err)
		}
	}
	a.restMode.Reset()
	a.emitRestModeState()
	return "已跳过", nil
}

// GetScreenRecordingStatus 返回当前屏幕录制权限的状态。
//
// 类型直接引用 service.ScreenRecordingStatus（其他可绑定 DTO 同处一包），
// 不用类型别名——wails3 生成器读的是返回值字面类型。
func (a *App) GetScreenRecordingStatus() service.ScreenRecordingStatus {
	return service.ScreenRecordingStatus{
		Granted:                 pause.ScreenCaptureGranted(),
		AskedBefore:             a.cfg.ScreenRecordingPermissionAsked,
		PlatformNeedsPermission: pause.RequiresScreenCapture(),
	}
}

// GetPauseSettings 返回智能免打扰的四路开关与阈值（设置页回显用）。
//
// 权限状态字段（Granted/Supported）由 pause 层现取；config 里的 Asked 只是
// 「弹过没有」，与用户当前能不能读状态无关。
func (a *App) GetPauseSettings() config.PauseSettings {
	s := a.cfg.LoadPauseSettings()
	s.ScreenRecordingGranted = pause.ScreenCaptureGranted()
	s.PlatformNeedsPermission = pause.RequiresScreenCapture()
	// 专注：Granted 现读；Supported 只在 macOS 12+ 上有（用 GetFocusStatus 是否
	// 走通做判据——非 macOS 平台 stub 恒返回 Granted=false）。
	focusStatus := pause.GetFocusStatus()
	s.FocusGranted = focusStatus.Granted
	s.FocusSupported = pause.FocusSupported()
	s.FocusDenied = pause.AuthorizationSnapshot{
		Granted:       focusStatus.Granted,
		AskedBefore:   s.FocusAsked,
		SupportedOnOS: s.FocusSupported,
	}.Denied()
	calStatus := pause.GetCalendarStatus()
	s.CalendarGranted = calStatus.Granted
	s.CalendarSupported = pause.CalendarSupported()
	s.CalendarDenied = pause.AuthorizationSnapshot{
		Granted:       calStatus.Granted,
		AskedBefore:   s.CalendarAsked,
		SupportedOnOS: s.CalendarSupported,
	}.Denied()
	return s
}

// RequestFocusAccess 走「应用说明 → 系统弹窗」两段式申请专注状态权限。
//
// 返回 granted：true=已授权；false=用户拒绝或弹窗尚未点。
// 【与屏幕录制同样的两段式】第一次弹应用内说明窗讲清「只读忙闲、不读事件」，
// 用户点【继续】后调系统 API 弹原生窗；拒绝过就不再骚扰（Asked 记 true）。
func (a *App) RequestFocusAccess() (bool, error) {
	if !pause.FocusSupported() {
		// 平台不支持 → 视为不需要授权；上层根据 FocusSupported 已经屏蔽了 UI。
		return true, nil
	}
	if pause.GetFocusStatus().Granted {
		return true, nil
	}
	if a.cfg.FocusAskedBefore {
		return false, nil
	}
	a.cfg.MarkFocusAsked()
	ok, err := dialog.RequestP1AccessConsent(a.app, "系统专注联动",
		"需要你授权 Morrow 读取【系统专注模式是否开启】这一个布尔状态。\n\n"+
			"不会读取事件标题、参与人、备注、地点；不进入 AI 提示词；不落盘、不上传。\n\n"+
			"如果拒绝，专注信号会静默缺席，空闲/锁屏/会议/全屏四路信号仍会正常工作。")
	if err != nil {
		return false, apperr.Wrap(err)
	}
	if !ok {
		return false, nil
	}
	pause.RequestFocusAccess()
	// 系统弹窗是异步的；本次调用不保证已授权。UI 下一次 GetPauseSettings 会看到
	// Granted=true（若用户点了允许）。
	return pause.GetFocusStatus().Granted, nil
}

// RequestCalendarAccess 与 RequestFocusAccess 同构，走日历授权。
func (a *App) RequestCalendarAccess() (bool, error) {
	if !pause.CalendarSupported() {
		return true, nil
	}
	if pause.GetCalendarStatus().Granted {
		return true, nil
	}
	if a.cfg.CalendarAskedBefore {
		return false, nil
	}
	a.cfg.MarkCalendarAsked()
	ok, err := dialog.RequestP1AccessConsent(a.app, "日历忙闲感知",
		"需要你授权 Morrow 读取【当前是否落在日历忙碌时段】这一个布尔状态。\n\n"+
			"不会读取事件标题、参与人、备注、地点；不进入 AI 提示词；不落盘、不上传。\n\n"+
			"如果拒绝，日历信号会静默缺席，其余四路信号仍会正常工作。")
	if err != nil {
		return false, apperr.Wrap(err)
	}
	if !ok {
		return false, nil
	}
	pause.RequestCalendarAccess()
	return pause.GetCalendarStatus().Granted, nil
}

// OpenPrivacySettings 打开 macOS「系统设置 → 隐私与安全」里对应权限的面板。
//
// pane 取值：screen_recording / focus / calendar（见 internal/systemprefs）。
//
// 【为什么要这个入口】三个敏感权限被拒后，应用内的说明窗不会再弹（系统只弹一次）；
// 没有直达链接时用户要自己翻三层菜单，多数人会直接放弃。设置页的「去系统设置」
// 按钮走这里。
//
// 【失败语义】只有「命令起不来」或「pane 非法」才返回错误；anchor 在当前 macOS
// 上不存在时系统会打开隐私根页并以成功退出——那种情况由前端的文字兜底覆盖，
// 后端不假装能判断（见 systemprefs.URLFor 的注释）。
func (a *App) OpenPrivacySettings(pane string) error {
	if !systemprefs.Supported() {
		return apperr.New(apperr.CodeInternal, "当前平台不支持跳转系统隐私设置")
	}
	p := systemprefs.Pane(pane)
	if !systemprefs.Valid(p) {
		return apperr.Validation("未知的隐私面板: " + pane)
	}
	if err := systemprefs.Open(p); err != nil {
		return apperr.Wrap(err)
	}
	slog.Info("已打开系统隐私设置", "pane", pane)
	return nil
}

// SetPauseSettings 校验并落盘智能免打扰设置，同时【重建】Bundle 让开关立即生效。
//
// 【为什么必须重建 Bundle】detector 列表在 NewBundle 里根据 Enabled 装配；
// 只写 config 不重建，运行期引擎仍然按旧列表判定，用户会看到「关了开关但会议中还是暂停」。
// 重建的代价很小（几个 struct + 一两个 goroutine），且引擎持有的 provider 通过
// SwapProvider 原子替换，不会打断当轮调度。
//
// 【关于 Fullscreen 的授权流程】这里不做说明窗与系统弹窗；那些走独立的
// RequestScreenCapture 绑定。若用户开了 FullscreenEnabled 但权限还没给，detector
// 会自动降级（不暂停、不投递）——功能"看起来开着"但没起作用，前端会在下一次
// GetPauseSettings 里通过 ScreenRecordingGranted=false 拿到状态并提示用户点授权。
func (a *App) SetPauseSettings(in config.PauseSettings) (string, error) {
	if err := a.cfg.SavePauseSettings(in); err != nil {
		return "", apperr.Wrap(err)
	}
	a.restartPauseBundle()
	return "已保存并生效", nil
}

// bundleConfigFrom 是 config.Config → pause.Config 的唯一映射。
//
// 【为什么单独抽出来】startReminderEngine 与 restartPauseBundle 都要拼装同一份
// 7 字段结构体，两处硬编码等于两份真相；加/减信号时忘了同步一处就会出现
// 「启动时启用了、保存设置后又被关掉」这种诡异漂移。
func bundleConfigFrom(c *config.Config) pause.Config {
	return pause.Config{
		IdleEnabled:        c.PauseIdleEnabled,
		IdleThreshold:      time.Duration(c.PauseIdleMinutesValue()) * time.Minute,
		LockEnabled:        c.PauseLockEnabled,
		LockWatcherEnabled: c.PauseLockEnabled || c.SitEnabled,
		MeetingEnabled:     c.PauseMeetingEnabled,
		FullscreenEnabled:  c.PauseFullscreenEnabled,
		FocusEnabled:       c.PauseFocusEnabled,
		CalendarEnabled:    c.PauseCalendarEnabled,
	}
}

// restartPauseBundle 按最新 config 重建 pause.Bundle 并原子替换引擎里的 provider。
//
// 停旧 bundle 前必须先构造新的：构造失败时旧的要能继续跑（否则一次配置错误就把免打扰
// 整体下线，用户看不到任何提示）。
//
// 【并发保护】整个替换在 pauseMu 下：两次并发的 SetPauseSettings 不会看到对方的中间态。
func (a *App) restartPauseBundle() {
	bundle, err := pause.NewBundle(bundleConfigFrom(a.cfg))
	if err != nil {
		slog.Error("重建智能免打扰信号失败，保留原有配置继续运行", "err", err)
		return
	}
	a.applyPresentationToBundle(bundle)
	a.pauseMu.Lock()
	old := a.pause
	a.pause = bundle
	a.pauseMu.Unlock()
	if a.svc != nil {
		a.svc.SetPauseService(service.NewPauseService(bundle.Provider))
	}
	if a.engine != nil {
		a.engine.SwapProvider(bundle.Provider)
	}
	// Bundle.Stop 内部有 sync.Once，重复调用（比如与 ServiceShutdown 竞态）不会 panic。
	if old != nil && old.Stop != nil {
		old.Stop()
	}
}

// RequestScreenCapture 走「应用说明 → 系统弹窗」两段式申请。
//
// 返回 granted 与 declined_permanently：
//   - granted=true：用户已授权，调用方可以开启 PauseFullscreenEnabled
//   - granted=false 且 declined_permanently=true：用户曾经拒绝或本次点了「暂不开启」，
//     UI 应改提示「请到 系统设置 → 隐私与安全 → 屏幕录制 里手动开启」
//
// 【为什么不让 pause 层直接调 CGRequestScreenCaptureAccess】原生弹窗的文案
// 是「允许 XX 控制这台电脑」，把「屏幕录制」说得太宽；ADR 0004 要求先讲清楚
// 「只读几何、不采集屏幕内容」再触发系统弹窗，多这一层说明是承诺的落地。
func (a *App) RequestScreenCapture() (granted bool, err error) {
	// 已经授权过：不再问，直接返回。
	if pause.ScreenCaptureGranted() {
		return true, nil
	}
	// 用户曾经明确拒绝过：不再弹说明窗，让上层引导去系统设置。
	if a.cfg.ScreenRecordingPermissionAsked {
		return false, nil
	}
	// 记一次「问过」——即使本次授权成功也记，避免状态漂移。
	a.cfg.ScreenRecordingPermissionAsked = true
	if saveErr := a.cfg.Save(); saveErr != nil {
		// 落盘失败仍继续：用户已经在系统弹窗里做选择，不因为写盘失败而撤销授权。
		slog.Warn("记录屏幕录制权限询问状态失败", "err", saveErr)
	}
	ok, dlgErr := dialog.RequestScreenCaptureConsent(a.app)
	if dlgErr != nil {
		return false, apperr.Wrap(dlgErr)
	}
	if !ok {
		return false, nil
	}
	return pause.RequestScreenCapture(), nil
}

// setPetVisible 是三个显隐绑定的唯一实现：改窗口 + 改配置 + 落盘。
//
// 【为什么显隐也要落盘】用户把宠物藏起来是有意的选择，重启后又冒出来会显得
// 应用"没记住我的话"。落盘失败只记日志：显隐已经生效，为一次写盘失败弹错
// 反而让用户以为没生效。
func (a *App) setPetVisible(visible bool) error {
	a.petMu.Lock()
	defer a.petMu.Unlock()

	w := a.petWindow()
	if w == nil {
		return apperr.NotFound("宠物窗口尚未创建")
	}

	a.cfg.PetVisible = visible
	if err := a.cfg.Save(); err != nil {
		slog.Error("保存宠物窗口开关失败", "err", err)
	}
	a.syncSettingsFromConfig()
	if a.petShouldBeVisible() {
		// 若之前贴边收着，先把窗口移回展开位置再 Show：
		// 否则用户看到的是屏幕外那块，会以为宠物丢了。
		a.restorePetFromDockLocked(w)
		w.Show()
	} else {
		w.Hide()
	}
	slog.Info("宠物窗口可见性已更新", "visible", visible)
	return nil
}

// OpenMainWindow 把主窗口拉到前面（宠物气泡被点击时调用）。
//
// 复用 restoreWindow：托盘「显示」、Dock 点击、二次启动、全局快捷键都走它，
// 四条恢复路径的取舍（隐藏态也能恢复）在那一处已经解决，不另写一份。
func (a *App) OpenMainWindow() error {
	a.restoreWindow()
	return nil
}

// ---------------------------------------------------------------------------
// 绑定方法：全屏强提醒窗口
//
// 【为什么只有"显示/隐藏"两个方法】窗口的几何在建窗时就定了（见 alert_window.go），
// 何时显示由前端决定（有没有待处理提醒 + 用户是否开了强提醒）。后端不参与判断：
// 它只有数据库与配置，没有"这一屏现在该不该打断用户"的上下文。
//
// 【为什么必须写在 app.go】internal/guard/bindings_test.go 只扫描本文件来与生成的
// 绑定产物对账；写在 alert_window.go 里方法本身能编译，但护栏会报「产物过期」。
// ---------------------------------------------------------------------------

// ShowAlert 显示【所有屏幕的】全屏强提醒窗口并置前主屏。
//
// 失败一律经 apperr（绑定层的错误协议，前端按 code 分流）；窗口不存在是
// "尚未创建或已被销毁"，属于 not_found，而不是内部错误。
//
// 【多屏语义（US-18）】每块屏上的 alert 窗口都被 Show()；主屏额外 Focus()
// 让键盘事件落到完整 UI 上。副屏窗口没有可交互按钮的键盘需求，Focus 只在主屏。
func (a *App) ShowAlert() error {
	wins := a.allAlertWindows()
	if len(wins) == 0 {
		return apperr.NotFound("强提醒窗口尚未创建")
	}
	for i, w := range wins {
		w.Show()
		if i == 0 {
			// AlwaysOnTop 保证它压在别的应用之上，但不会被系统主动聚焦；Focus 让键盘事件
			// （用户可能直接按回车点「完成」）落到这个窗口上。
			w.Focus()
		}
	}
	return nil
}

// HideAlert 隐藏【所有屏幕的】全屏强提醒窗口。
//
// 窗口只隐藏不销毁：它的前端状态（已拉到的待处理列表、正在跑的倒计时）在
// 下一条提醒到达时可以继续用，重建一个窗口则要重新加载整个前端产物。
//
// 【为什么必须一起 Hide】「任一屏幕处理完 = 所有屏幕的强提醒消失」（US-19）：
// 只隐藏被点击的那块屏会让副屏挂着一张已经处理过的旧卡片，视觉上非常诡异。
func (a *App) HideAlert() error {
	wins := a.allAlertWindows()
	if len(wins) == 0 {
		return apperr.NotFound("强提醒窗口尚未创建")
	}
	for _, w := range wins {
		w.Hide()
	}
	return nil
}

// gen:bind
