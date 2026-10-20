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
	"morrow/internal/reminder"
	"morrow/internal/reveal"
	"morrow/internal/service"
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
	// notify 是官方通知服务实例（main.go 创建并注册进 Services，同包直接赋值）。
	// 持有它而非每次现取：ServiceStartup 由框架回调，绑定方法只能靠这个引用触达。
	notify *notifications.NotificationService
	// shortcutAcc 是【已注册到系统】的快捷键串，与 cfg.ShortcutToggleWindow（期望值）
	// 分开——注册失败时二者允许短暂不一致。读写发生在绑定调用与 ServiceStartup
	// 两个 goroutine，由 shortcutMu 串行化。
	shortcutMu  sync.Mutex
	shortcutAcc string
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

	// settingsMu 保护下面两个【跨 goroutine 读】的设置快照。
	//
	// 读方：引擎 goroutine 每 20 秒读一次静默时段/推迟分钟/通知开关；绑定方法
	// （生成计划、测试连接）读 AI 连接配置。写方：设置页的保存绑定方法。
	// 三者线程不同，而 config.json 的字段是普通值——不加锁就是数据竞争。
	settingsMu sync.RWMutex

	// reminderCfg 是引擎读取的行为设置快照。
	reminderCfg reminder.Settings

	// aiCfg 是 AI 连接配置快照（含明文密钥，只在后端内部流转）。
	aiCfg ai.Settings
}

// NewApp 创建应用实例，注入服务聚合与配置（由 main.go 完成组合根组装）。
func NewApp(svc *service.Service, cfg *config.Config) *App {
	app := &App{svc: svc, cfg: cfg}
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

// alertWindow 返回全屏强提醒窗口；窗口尚未创建或已被销毁时返回 nil。
//
// 与 mainWindow / petWindow 同构：按注册名查，而不是 Window.Current()。强提醒的
// 显示恰恰发生在用户正在操作别的应用时——那一刻本窗口既不可见也不是 key window，
// Current() 会返回 nil，于是"到点了但什么都没发生"，且不会有任何报错。
//
// 【刻意不导出】：它是窗口对象的内部取值器，导出会被绑定给前端（前端拿到的是
// 序列化后的空对象，毫无意义）。前端要的是 ShowAlert / HideAlert 这两个动作。
func (a *App) alertWindow() application.Window {
	if a.app == nil {
		return nil
	}
	if w, ok := a.app.Window.GetByName(alertWindowName); ok {
		return w
	}
	return nil
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
	stop, err := tray.Start(a.app, tray.Options{
		Icon:    trayIcon(),
		Tooltip: appName,
	}, tray.Handlers{
		OnShow: func() {
			// 与 Dock 点击、二次启动共用同一实现（见 restoreWindow）。
			a.restoreWindow()
		},
		OnQuit: func() {
			a.app.Quit()
		},
	})
	if err != nil {
		// 托盘起不来不该让整个应用拒绝启动：它只是入口之一，窗口才是主入口。
		// 但必须留下日志——否则用户报「托盘没了」时无从查起。
		slog.Error("启动托盘失败", "err", err)
		return nil
	}
	a.trayStop = stop
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

	// 接管系统通知的交互回调（点通知本体 / 点通知上的「完成」「推迟」按钮）。
	// 只设置回调，不触发任何平台调用，故可以在通知服务的 ServiceStartup 之前执行。
	a.bindNotificationResponses()

	// 启动本地提醒调度引擎。放在最后：它一旦跑起来就会立刻 tick 一轮并可能发通知，
	// 而通知需要 ServiceStartup 里的平台初始化先完成——它比本函数注册得晚
	// （见 main.go 的 Services 顺序），故首轮 tick 的通知失败会走"退回卡片"分支。
	a.startReminderEngine()

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

// ExportDatabase 把数据库完整导出到 targetPath（通常来自 SelectSaveFile）。
// 目标已存在会被覆盖（用户已在系统保存对话框确认过）；位于数据目录内会被拒绝。
func (a *App) ExportDatabase(targetPath string) error {
	return a.svc.ExportDatabase(appName, targetPath)
}

// ImportDatabase 用 sourcePath（通常来自 SelectFile，即此前导出的备份）替换当前数据库。
// 失败自动回滚到导入前状态（见 internal/service/import.go）；成功后建议重启应用，
// 让各页面以新库数据重新加载（前端在恢复成功提示中说明）。
func (a *App) ImportDatabase(sourcePath string) error {
	return a.svc.ImportDatabase(appName, sourcePath)
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
// 先注销旧注册再注册新的；新串注册失败时回滚旧注册，保证系统状态与调用前一致。
// 持锁操作：shortcutAcc 是跨 goroutine 的共享状态（见 App 字段注释）。
func (a *App) registerToggleShortcut(acc string) error {
	a.shortcutMu.Lock()
	defer a.shortcutMu.Unlock()

	if a.shortcutAcc != "" {
		// 注销失败不阻断重注册：快捷键可能已被系统侧清掉（注销返回错误但目标本来就是空的）。
		_ = a.app.GlobalShortcut.Unregister(a.shortcutAcc) // 失败无副作用，见上注
	}
	a.shortcutAcc = ""

	if acc == "" {
		return nil
	}
	if err := a.app.GlobalShortcut.Register(acc, a.toggleMainWindow); err != nil {
		// 回滚：尽力把旧快捷键恢复注册（失败只记日志——已经处于降级状态）。
		if prev := a.cfg.ShortcutToggleWindow; strings.TrimSpace(prev) != "" {
			if reErr := a.app.GlobalShortcut.Register(prev, a.toggleMainWindow); reErr != nil {
				slog.Error("回滚全局快捷键注册失败", "accelerator", prev, "err", reErr)
			} else {
				a.shortcutAcc = prev
			}
		}
		return err
	}
	a.shortcutAcc = acc
	return nil
}

// SetToggleWindowShortcut 设置「唤起/隐藏主窗口」全局快捷键并落盘。
// 空串 = 关闭该快捷键。非法串（注册失败，常见原因：被其他应用占用）不落盘。
func (a *App) SetToggleWindowShortcut(acc string) error {
	acc = strings.TrimSpace(acc)
	if err := a.registerToggleShortcut(acc); err != nil {
		// 面向用户的提示 + 原始错误保留在 Detail（排查"被谁占用"要用）。
		slog.Error("注册全局快捷键失败", "accelerator", acc, "err", err)
		ae := apperr.Wrap(err)
		ae.Message = "注册快捷键失败（可能已被其他应用占用）"
		return ae
	}
	a.cfg.ShortcutToggleWindow = acc
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
		QuietStart:    a.cfg.QuietStart,
		QuietEnd:      a.cfg.QuietEnd,
		SnoozeMinutes: a.cfg.SnoozeMinutes,
		NotifyEnabled: a.cfg.NotifyEnabled,
	}
	aiSettings := a.cfg.AISettings()

	a.settingsMu.Lock()
	a.reminderCfg = reminderSettings
	a.aiCfg = aiSettings
	a.settingsMu.Unlock()
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
	a.engine = reminder.NewEngine(reminder.Options{
		Store:    a.svc,
		Settings: a.currentReminderSettings,
		Notifier: &reminderNotifier{notify: a.notify},
		Emitter:  appEmitter{app: a.app},
	})
	a.engine.Start()
	slog.Info("提醒引擎已启动", "tick", reminder.TickInterval.String())
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

// SetPetSettings 保存宠物设置，并【立即】作用到宠物窗口：
// 开关决定显示/隐藏，缩放决定窗口尺寸（画布放大后窗口不放大就会被裁掉）。
//
// 返回一句面向用户的提示（前端直接显示），失败经 apperr 上抛。
func (a *App) SetPetSettings(in config.PetSettings) (string, error) {
	if err := a.cfg.SavePetSettings(in); err != nil {
		return "", apperr.Wrap(err)
	}

	a.petMu.Lock()
	defer a.petMu.Unlock()
	if w := a.petWindow(); w != nil {
		if in.PetVisible {
			// 同 setPetVisible：先还原到展开位置，否则 Show 出来的是屏幕外那块。
			a.restorePetFromDockLocked(w)
			w.Show()
		} else {
			w.Hide()
		}
		// 尺寸只在可见时调整：隐藏态下改尺寸在部分平台会顺带把窗口顶到前台。
		if in.PetVisible {
			width, height := petWindowSize(a.cfg.PetScaleValue())
			w.SetSize(width, height)
		}
	}
	slog.Info("宠物设置已保存", "visible", in.PetVisible, "scale", a.cfg.PetScaleValue())
	return "已保存", nil
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
	if visible {
		// 若之前贴边收着，先把窗口移回展开位置再 Show：
		// 否则用户看到的是屏幕外那块，会以为宠物丢了。
		a.restorePetFromDockLocked(w)
		w.Show()
	} else {
		w.Hide()
	}

	a.cfg.PetVisible = visible
	if err := a.cfg.Save(); err != nil {
		slog.Error("保存宠物窗口开关失败", "err", err)
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

// ShowAlert 显示全屏强提醒窗口并置前。
//
// 失败一律经 apperr（绑定层的错误协议，前端按 code 分流）；窗口不存在是
// "尚未创建或已被销毁"，属于 not_found，而不是内部错误。
func (a *App) ShowAlert() error {
	w := a.alertWindow()
	if w == nil {
		return apperr.NotFound("强提醒窗口尚未创建")
	}
	w.Show()
	// AlwaysOnTop 保证它压在别的应用之上，但不会被系统主动聚焦；Focus 让键盘事件
	// （用户可能直接按回车点「完成」）落到这个窗口上。
	w.Focus()
	return nil
}

// HideAlert 隐藏全屏强提醒窗口。
//
// 窗口只隐藏不销毁：它的前端状态（已拉到的待处理列表、正在跑的倒计时）在
// 下一条提醒到达时可以继续用，重建一个窗口则要重新加载整个前端产物。
func (a *App) HideAlert() error {
	w := a.alertWindow()
	if w == nil {
		return apperr.NotFound("强提醒窗口尚未创建")
	}
	w.Hide()
	return nil
}

// gen:bind
