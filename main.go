package main

import (
	"embed"
	"log"
	"log/slog"
	"os"
	"runtime"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	logservice "github.com/wailsapp/wails/v3/pkg/services/log"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"

	"morrow/internal/apperr"
	"morrow/internal/appinfo"
	"morrow/internal/config"
	"morrow/internal/database"
	"morrow/internal/logging"
	"morrow/internal/service"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var trayIconBytes []byte

// appName 应用名，用于窗口标题与用户配置目录下的数据文件、日志定位。
const appName = "Morrow"

// bootTimer 记录组合根各阶段的耗时，输出启动时间线的 Go 侧半段。
//
// 为什么需要：此前启动只有「应用启动」与「前端已挂载」两点之差，实测 359–733ms
// （同一天两次运行差 2×），中间是黑盒——不知道该优化 webview、前端产物还是组合根。
// 前端那半段由 frontend/src/main.ts 记录，两半靠 app.log 的绝对时间戳拼起来
// （见 openspec/changes/perf-instrumentation/design.md D1）。
//
// 该打点由 internal/guard/wiring_test.go 核对数量：删掉没有任何症状，
// 只是启动优化重新变成盲调。
type bootTimer struct {
	start time.Time // 进程起点
	last  time.Time // 上一阶段结束点
}

// newBootTimer 以调用时刻为进程起点。
func newBootTimer() *bootTimer {
	now := time.Now()
	return &bootTimer{start: now, last: now}
}

// stage 记录一个阶段的耗时与该阶段结束时的累计耗时，并推进计时起点。
func (b *bootTimer) stage(name string) {
	now := time.Now()
	slog.Info("启动阶段",
		"stage", name,
		"ms", now.Sub(b.last).Milliseconds(),
		"total_ms", now.Sub(b.start).Milliseconds(),
	)
	b.last = now
}

func main() {
	boot := newBootTimer()

	// 日志：开发态 Debug，生产态 Info（构建模式判定见 buildmode_*.go）。
	logLevel := slog.LevelDebug
	if !debugBuild {
		logLevel = slog.LevelInfo
	}
	closeLog, err := logging.Init(logging.Options{
		AppName: appName,
		Level:   logLevel,
		Version: appinfo.Resolve(),
	})
	if err != nil {
		log.Fatalf("日志初始化失败: %v", err)
	}
	defer closeLog()
	boot.stage("日志初始化")

	// 组合根：数据库 → 服务 → 配置 → App
	db, err := database.Init(appName)
	if err != nil {
		log.Fatalf("数据库初始化失败: %v", err)
	}
	boot.stage("数据库初始化")

	cfg, err := config.Load(appName)
	if err != nil {
		log.Fatalf("配置加载失败: %v", err)
	}
	appSvc := NewApp(service.New(db), cfg)
	boot.stage("配置加载与组装")

	// MCP 鉴权透传：配置了 token 且以 WAILS_MCP=1 启动时，把它交给运行时 MCP。
	// 【必须在 application.New 之前】：MCP server 在框架启动流程里读环境变量
	// （v3.0.0 mcp_enabled.go 的 startMCPServer → WAILS_MCP_TOKEN），跑起来再设无效。
	// 配置为空 = 不加鉴权（MCP 仅 -tags mcp 构建存在，loopback 是默认绑定面，
	// 见 openspec/changes/builtin-toolkit/design.md D7；生产构建无 MCP 由护栏 17 把关）。
	if os.Getenv("WAILS_MCP") == "1" && cfg.MCPToken != "" {
		os.Setenv("WAILS_MCP_TOKEN", cfg.MCPToken)
		slog.Info("已启用 MCP Bearer 鉴权（WAILS_MCP_TOKEN）")
	}

	// 通知服务：组合根创建，注册进 Services（框架回调其 ServiceStartup/Shutdown），
	// 同一实例交给 App 供绑定方法触达。放这里而不是 application.New 之后再建：
	// Services 列表在 New 的参数里，必须提前就位。
	notifySvc := notifications.New()
	appSvc.notify = notifySvc

	// 窗口几何：在【创建期】给定尺寸与最大化状态，首帧即正确。
	// 不能在运行期设——应用启动后窗口立即显示，二者存在竞态，
	// 用户会看到窗口跳一下（见 openspec/changes/runtime-pipeline/design.md D1）。
	// v3 把这件事写得更显式：几何直接是 NewWithOptions 的参数，没有"以后再设"的入口。
	winWidth, winHeight := cfg.WindowSize()
	startState := application.WindowStateNormal
	if cfg.WindowMaximised {
		startState = application.WindowStateMaximised
	}

	// ---- 第 1 段：建应用（服务 / 资源 / 日志 / 错误协议 / 单实例）----
	// 先声明后赋值（而不是 :=）：OnSecondInstanceLaunch 闭包要引用 wailsApp，
	// 而 := 的变量在自己的初始化表达式里还不可见。
	var wailsApp *application.App
	wailsApp = application.New(application.Options{
		Name:        appName,
		Description: appName,
		// 绑定方法经 Service 注册（v2 是 Bind: []interface{}{...}）。
		// 只有【导出方法】会被绑定，且 ServiceStartup/ServiceShutdown 由框架排除。
		Services: []application.Service{
			application.NewService(appSvc),
			// 前端日志出口。v2 靠 runtime.LogInfo 那套经 WailsInvoke 送给
			// options.App.Logger；v3 把这条通道【整个拿掉了】——Options.Logger 的
			// 文档明确写着「用于 Wails 系统消息，不是应用消息」，前端运行时里也
			// 不再有 LogDebug/LogInfo。取代它的是官方 log 服务，故必须显式注册。
			//
			// 不注册的症状：前端日志（含 lib/log.ts 记的绑定调用失败、启动时间线）
			// 全部只剩 console，而打包后的应用没有 DevTools——等于没记。
			// 与 D4 说的「后端 logger 不接则日志静默丢失」是同一类失效，故同样由
			// internal/guard/wiring_test.go 兜住。
			//
			// Logger 传 logging.Frontend() 而非 slog.Default()：前端日志需要一个
			// source=frontend 标记，否则与 Go 业务日志在文件里完全同形（见该函数注释）。
			application.NewService(logservice.NewWithConfig(&logservice.Config{
				Logger:   logging.Frontend(),
				LogLevel: logLevel,
			})),
			// 系统通知服务（官方 services/notifications）。注册是唯一正路：
			// 框架借此回调 ServiceStartup 完成平台侧初始化（macOS 的
			// UNUserNotificationCenter delegate 等），绑定方法经 App.notify 触达。
			application.NewService(notifySvc),
		},
		// Wails 自身的系统日志接到 slog：不接的话，框架内部错误（资源加载失败、
		// webview 异常）【只写 stdout】——打包后无人可见，且不接不会有任何症状。
		// 故这条接线由 internal/guard/wiring_test.go 强制。
		//
		// v3 的类型就是 *slog.Logger（v2 是自定义 logger.Logger 接口，项目为此写了
		// logging.WailsAdapter 做桥接）。适配器已删除，但【接线条目保留】——
		// 少了它，日志静默丢失这件事没有任何症状（见 internal/logging/logging.go）。
		//
		// 注意这里【只】管框架系统消息与 Go 业务日志。前端日志走的是上面注册的
		// log 服务，不是这个字段——v3 明确把两者分开了（见该处注释）。
		Logger:   slog.Default(),
		LogLevel: logLevel,
		// 错误协议：把 Go 的 error 序列化为 JSON，前端 JSON.parse 还原为 {code,message}。
		// v3 的签名是 func(error) []byte（v2 是 func(error) any，且【必须】返回字符串——
		// 因为前端会走 new Error(payload) 把对象强转成 "[object Object]"）。
		// v3 由框架直接序列化，不再经过那条会丢结构的路径，故现在返回字节。
		MarshalError: apperr.MarshalError,
		// 单实例锁：二次启动唤起已有窗口。
		// 注意 v3 的字段名是 UniqueID（v2 是 UniqueId），数据里是 WorkingDir（v2 是 WorkingDirectory）。
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: appName + "-single-instance",
			OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
				slog.Info("检测到二次启动，唤起已有窗口", "args", data.Args)
				// 与托盘「显示」、Dock 点击共用同一恢复实现。
				// 旧代码用 wailsApp.Window.Current()——窗口隐藏时它返回 nil，于是二次启动
				// 也唤不出窗口（本次报障 R2 里「再次启动」这条路径同样哑火的原因）。
				appSvc.restoreWindow()
				// 把二次启动的启动参数推给前端（复用事件契约）。
				// 首次启动的参数不经事件——那时前端还没订阅，必然丢；用 GetLaunchArgs 查询。
				appSvc.notifySecondInstance(data.Args, data.WorkingDir)
			},
		},
		// 资源服务：把嵌进二进制的 frontend/dist 交给内置 asset server。
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		// macOS 应用级选项：ApplicationShouldTerminateAfterLastWindowClosed【必须显式设为 false】。
		//
		// 【为什么不是 true（这一条是本仓实测 + 上游共同结论，别改回去）】：
		//   - 该字段的语义是「最后一个窗口关闭后是否退出应用」，它由 AppKit 在窗口不再可见时
		//     触发。而本仓关窗链路对托盘在的情形调用 Hide()（销毁被 Cancel 拦下，窗口只是
		//     隐藏）。A/B 实测（2026-09-20）：同一 Window.Close() 路径，true 时关窗钩子正常
		//     隐藏（日志出现「窗口已隐藏到托盘」）后进程随即退出，破坏 R1「进程驻留」；false
		//     时进程驻留、Dock 点击可恢复。这正是上游 issue #4389 记录的行为
		//     （webview_window_darwin.m 里有一段专门为它加的短路注释），也是上游官方范例
		//     examples/hide-window/main.go 设 false、而非 true 的原因。
		//   - v3 该字段默认即 false（上游 application_options_test.go 有断言），故这里显式
		//     写出 false 不是「沿用默认」，而是把一个曾经踩过坑的取值钉死在源码里、
		//     由 internal/guard/wiring_test.go 的护栏 20 强制。
		//
		// 【那 R3（托盘没起来时关闭必须退出）靠什么保证】不靠本字段，靠关窗钩子在托盘缺失
		// 分支里【显式 Quit()】（见下方 WindowClosing 钩子）：销毁窗口本身不会让 macOS 进程
		// 退出，只有显式 Quit 才能既退出又不残留无窗口进程。
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
		// macOS：透明标题栏 + 交通灯 inset + 内容顶到顶，不自绘三按钮。
		// v3 的常量是包级 var（不是函数调用），且写在【窗口】选项里（v2 在 options.App.Mac）。
	})

	// ---- 第 2 段：建窗（几何 / 无边框 / 平台选项）----
	window := wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:   "main",
		Title:  appName,
		Width:  winWidth,
		Height: winHeight,
		// URL 用资源服务的根；v2 没有这个字段（由 AssetServer 决定）。
		URL: "/",
		// 生产态关掉 DevTools：发布版本不该给用户留一个调试入口。
		// 与 buildmode_*.go 的 debugBuild 同源——dev 与 build 各自正确，不需要额外开关。
		DevToolsEnabled: debugBuild,
		// 无边框按平台分叉：Windows/Linux 自绘标题栏（三按钮），macOS 用原生交通灯。
		// macOS 必须 false，否则会跳过 NSWindowStyleMaskTitled，交通灯消失（见 design D3）。
		Frameless: frameless(),
		// 与 tokens 的 --bg-base 一致，避免启动白闪。
		// v3 是值类型 + 构造器（v2 是 *options.RGBA 指针）。
		BackgroundColour: application.NewRGB(14, 15, 18),
		StartState:       startState,
		Mac: application.MacWindow{
			TitleBar: application.MacTitleBarHiddenInset,
		},
		// Windows：保留无边框窗口的圆角/阴影装饰，跟随系统明暗主题。
		// 注意 Theme 的常量在 application 包级，不在 windows 子包（v2 是 windows.SystemDefault）。
		Windows: application.WindowsWindow{
			Theme: application.SystemDefault,
		},
	})

	// ---- 第 2b 段：建宠物窗口（透明置顶小窗）----
	//
	// 与主窗【并列】创建，而不是在主窗里开一个子窗口：
	//   - 它要浮在别的应用之上（AlwaysOnTop），是一个独立的顶层窗口；
	//   - 它隐藏/显示与主窗完全解耦（关掉宠物不影响主窗，反之亦然）。
	//
	// URL 用 hash 路由指向 /#/pet：与主窗共用同一份前端产物（assets 已整体嵌入），
	// 该路由的 meta.bare 让 App.vue 跳过 AppShell——否则宠物窗口里会画出侧栏与标题栏。
	//
	// 建窗参数集中在 pet_window.go 的 petWindowOptions 里（透明/置顶/位置/尺寸换算
	// 互相咬合，放一处才不会各写一套）。
	petWin := wailsApp.Window.NewWithOptions(petWindowOptions(cfg))
	// 位置单独设置：建窗参数里的 X/Y 是【相对屏幕】的坐标，而 Position()/SetPosition()
	// 用全局坐标。混用会让多屏用户每次启动都被拽回主屏（详见 petRestorePosition）。
	if petWin != nil {
		px, py := petRestorePosition(wailsApp, cfg)
		petWin.SetPosition(px, py)
	}

	// ---- 第 2c 段：建全屏强提醒窗口（透明覆盖层，启动即隐藏）----
	//
	// 与宠物窗口并列的第三个窗口，理由同样是"独立的顶层窗口"：它要铺满整块屏幕并浮在
	// 所有窗口之上，做成主窗的子窗口既盖不住别的应用，也会被主窗的显隐牵连。
	//
	// 【尺寸与位置分开给】建窗参数里的 Width/Height 只是首帧尺寸，几何的唯一真相是
	// alertGeometry（主屏 WorkArea）。位置必须用 SetPosition 设全局坐标——建窗参数的
	// X/Y 是"相对屏幕"的坐标，混用会让多屏用户每次到点都盖错屏幕。
	//
	// 【Hidden: true 在建窗参数里】不是创建后再 Hide（那会先闪一下全屏遮罩）。
	// 何时显示由前端按"有没有待处理提醒"决定，见 Alert.vue 的状态机说明。
	//
	// 【多屏覆盖（US-18）】每块屏幕各起一个 alert 窗口：primary 完整 UI，副屏简化。
	// 副屏从 "alert-2" 起编号；主屏仍叫 "alert"，让 app.go 的 allAlertWindows() 不用改。
	// 换屏幕后需要重启应用才会重排窗口——桌面健康提醒的场景里插拔显示器极罕见。
	alertWindows := make([]application.Window, 0, 4)
	for i, r := range alertGeometries(wailsApp) {
		name := alertWindowName
		if i > 0 {
			name = AlertSecondaryName(i + 1)
		}
		win := wailsApp.Window.NewWithOptions(alertWindowOptions(name, r.Width, r.Height))
		if win != nil {
			win.SetSize(r.Width, r.Height)
			win.SetPosition(r.X, r.Y)
			alertWindows = append(alertWindows, win)
		}
	}
	// 【它的 WindowClosing 钩子不在这里注册】必须排在主窗那条钩子【之后】，
	// 理由见下方 alert 钩子处的注释（护栏只解析 main.go 里第一个钩子）。

	// 强提醒的 Go 侧兜底：引擎每次 emit reminder:fired 时，也把 alert 窗口推出来一次。
	//
	// 【为什么不能只靠前端】整条链路的触发是「隐藏的 alert 窗口前端收到事件 → 自己
	// 判断 → 调 ShowAlert()」，后端从不主动推窗口。而 WKWebView 对 ordered-out 的
	// 窗口会降优先级、可能节流甚至挂起页面——一旦那一页被挂起，遮罩永远不出现，
	// 且没有任何日志（本仓最忌讳的"没有症状的失效"）。
	//
	// 【职责边界】这里只保证"窗出来"：不判断有没有待处理、不渲染内容、不调 Focus()；
	// 但演示模式期间必须拒绝推窗——否则全屏遮罩会在共享画面里闪一下。
	// 内容由前端渲染（那一页恢复可见时会在 visibilitychange 上自己 syncVisibility），
	// 有没有待处理也由前端判断；抢焦点比晚出现半秒更讨厌（用户可能正在别处打字）。
	// 用 GetByName 而不是调 App 的绑定方法：绑定层一行都不动（护栏 3 要复算方法 ID）。
	//
	// 【关于并发】这里读 cfg.StrongAlertEnabled 不加锁，与设置页的写入没有互斥——
	// 与本仓其它 cfg 字段（如负责开关宠物的 PetVisible）的既有取舍一致：
	// 单个 bool 不会撕裂读，最坏是晚一个 tick 才反映新值，不影响正确性。
	alertShowHook = func() {
		if !cfg.StrongAlertEnabled || appSvc.isPresentationActive() {
			return
		}
		if w, ok := wailsApp.Window.GetByName(alertWindowName); ok && w != nil {
			w.Show()
		}
	}

	// macOS：挂应用菜单（AppMenu + EditMenu role）。
	// 为什么必须挂：webview 里的 <input>/<textarea> 快捷键（Cmd+C/V/X/A/Z）不是
	// webview 自带的，而是由响应链上的应用菜单承接——不挂菜单，输入框的复制粘贴
	// 全军覆没。EditMenu role 由框架展开为标准编辑项（撤销/重做/剪切/复制/粘贴/
	// 全选），AppMenu 提供「关于/隐藏/退出」等标准应用项。
	//
	// 只 darwin：Windows/Linux 本仓是无边框自绘标题栏，挂原生菜单条反而改变形态
	// （design.md D8）。role 展开的完整菜单项清单以上游 roles.go 为准，本仓不复刻。
	// 该接线由 internal/guard/wiring_test.go 的护栏 25 强制（此失效完全无症状）。
	if runtime.GOOS == "darwin" {
		appMenu := wailsApp.Menu.New()
		appMenu.AddRole(application.AppMenu)
		appMenu.AddRole(application.EditMenu)
		wailsApp.Menu.SetApplicationMenu(appMenu)
		slog.Info("已挂载 macOS 应用菜单（AppMenu + EditMenu）")
	}

	// 关闭窗口 = 隐藏到托盘（三平台统一）。
	//
	// 【为什么不再按平台分叉】旧的 hide_window_darwin.go 立论是「macOS 无托盘常驻」，
	// 该立论在 v3 已失效——v3 起 macOS 也有真托盘（见 internal/tray/tray.go 的文件头，
	// 以及 app.go 里三平台一律 tray.Start）。分叉删掉后，关窗决策改为【运行期】询问
	// 「托盘是否真的起来了」（appSvc.trayRunning）——这才是真正的判据。
	//
	// 【必须用 RegisterHook，不能用 OnWindowEvent】：hooks 同步顺序执行，任一 hook
	// Cancel() 后框架立即返回，默认动作（销毁窗口）被拦下；而 listeners 是在 goroutine
	// 里各跑各的（框架自己在 Common.WindowClosing 上就挂了一个销毁 listener，见
	// webview_window.go），抢不过默认动作——写错了不会报错，只是窗口照常销毁。
	// （已对着 v3.0.0 的 HandleWindowEvent 实现核实。）
	//
	// 【R1/R3】托盘在 → Hide() + Cancel()：窗口保留、页面状态不丢、进程驻留；
	// 托盘没起来 → 拦下默认动作并【显式 Quit()】：既退出又不残留无窗口进程。
	// （为什么不放行默认动作：v3 的 Mac.ApplicationShouldTerminateAfterLastWindowClosed
	// 必须为 false（见 Options.Mac 注释），此时「销毁最后一个窗口」不会让 macOS 进程退出，
	// 只放行销毁反而留下无窗口进程。）
	window.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if appSvc.trayRunning() {
			window.Hide()
			e.Cancel()
			// R4：隐藏记一条，供「关窗后回不来」这类报障下次直接从日志判断。
			slog.Info("窗口已隐藏到托盘", "window", window.Name())
			return
		}
		// 托盘没起来：真正退出应用——先拦下默认动作，再走 Quit() 的优雅关闭
		// （停托盘 → 落盘配置 → 关库），绝不留一个看不见也点不到的无窗口进程。
		slog.Info("托盘未运行，关闭窗口即退出应用")
		e.Cancel()
		wailsApp.Quit()
	})

	// 强提醒窗口的关窗拦截。
	//
	// 【为什么拦】全屏遮罩看起来就是个对话框，用户按 Cmd+W / 关窗快捷键是自然反应。
	// 不拦的话框架会【销毁】这个窗口——而它只在启动时创建一次，此后到点再没有
	// 任何入口能把它显示回来（ShowAlert 只会拿到 nil 并回报 not_found）。
	// 故与主窗同一套取舍：拦下销毁，改为隐藏（父级理由见上方主窗钩子的长注释）。
	//
	// 【为什么必须排主窗钩子之后】internal/guard/wiring_test.go 的护栏 19/20 只解析
	// main.go 里**第一个** WindowClosing 钩子来验证关窗决策（托盘在→隐藏、托盘缺失→
	// 退出）。alert 的钩子是无条件隐藏、与托盘无关，排在前面会把主窗那条挤出去、
	// 让护栏误判（护栏 20 会直接变红）。顺序即契约，不要为了"就近创建"把它挪回建窗旁边。
	for _, win := range alertWindows {
		w := win
		w.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
			w.Hide()
			e.Cancel()
			slog.Info("强提醒窗口被请求关闭，已改为隐藏")
		})
	}

	// macOS 点 Dock 图标恢复窗口（四条恢复路径之一）。
	//
	// AppKit 的 applicationShouldHandleReopen 由框架转成
	// events.Mac.ApplicationShouldHandleReopen——本仓此前【无人监听】（已 grep 确认），
	// 故 Dock 点击在修复前完全无效。这里与托盘「显示」/双击、二次启动共用 restoreWindow。
	// 该事件仅 macOS 会派发，其他平台注册后永不触发（impl.on 是 no-op），故不额外分叉。
	wailsApp.Event.OnApplicationEvent(events.Mac.ApplicationShouldHandleReopen, func(_ *application.ApplicationEvent) {
		slog.Info("Dock 图标被点击，恢复窗口")
		appSvc.restoreWindow()
	})

	// ---- 第 3 段：跑事件循环 ----
	// 注入应用实例：App 的绑定方法与 ServiceStartup 都需要它（见 app.go 的 attach）。
	// 必须在 Run 之前——Run 会立刻触发 ServiceStartup，那时 a.app 得已经就位。
	appSvc.attach(wailsApp)

	// ServiceShutdown（app.go）在这个函数返回前被框架调用，负责回收资源。
	if err := wailsApp.Run(); err != nil {
		log.Fatalf("应用运行失败: %v", err)
	}
}

// trayIcon 返回托盘图标字节。
func trayIcon() []byte {
	return trayIconBytes
}
