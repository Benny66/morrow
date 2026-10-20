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

	"my-notice/internal/appdir"
	"my-notice/internal/apperr"
	"my-notice/internal/appinfo"
	"my-notice/internal/config"
	"my-notice/internal/dialog"
	"my-notice/internal/event"
	"my-notice/internal/reveal"
	"my-notice/internal/service"
	"my-notice/internal/tray"
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
	// 快捷键的当前注册状态：shortcutAcc 是【已注册到系统】的快捷键串，
	// 与 cfg.ShortcutToggleWindow（期望值）分开——注册失败时二者允许短暂不一致。
	// 读写发生在绑定调用与 ServiceStartup 两个 goroutine，用互斥锁串行化。
	shortcutMu  sync.Mutex
	shortcutAcc string
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
}

// NewApp 创建应用实例，注入服务聚合与配置（由 main.go 完成组合根组装）。
func NewApp(svc *service.Service, cfg *config.Config) *App {
	return &App{svc: svc, cfg: cfg}
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

// trayRunning 报告系统托盘是否真的起来了。
//
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
	return nil
}

// ServiceShutdown 实现 application.Service 的关闭钩子（对应 v2 的 OnShutdown）。
//
// 框架保证：收到过 Startup 的服务必然收到 Shutdown（除非关闭过程 panic），
// 且在 Options.OnShutdown 之后、按注册逆序调用。
func (a *App) ServiceShutdown() error {
	// 1. 停托盘
	if a.trayStop != nil {
		a.trayStop()
	}
	// 2. 采集窗口几何并落盘配置（一次 Save 同时写主题与窗口状态）
	a.captureWindowGeometry()
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

// gen:bind
