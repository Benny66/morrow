package main

// 桌面宠物窗口的建窗参数与几何换算。
//
// 【为什么单独成文件】宠物窗口有两条互相咬合的约定，放一起才不会各写一套：
//   - 窗口是【全透明 + 无边框 + 置顶】的，任何一处背景不透明都会让它变成一个方块；
//   - 窗口尺寸随精灵图缩放联动（画布按 scale 放大，窗口必须一起放大，否则宠物被裁掉）。
//
// 建窗本身仍发生在 main.go 的组合根里（与主窗并列），本文件只提供选项。

import (
	"math"

	"github.com/wailsapp/wails/v3/pkg/application"

	"morrow/internal/config"
)

// petWindowName 是宠物窗口在 WindowManager 里的注册名。
// app.go 的 petWindow() 按这个名字回查窗口（与 mainWindow 同构）。
const petWindowName = "pet"

// 宠物窗口在 scale = 1.0 时的尺寸。
//
// 宽 240：精灵图单帧 192 宽 + 两侧各 24 的 padding，够放一个完整宠物，
// 又不至于让透明窗口在屏幕上占掉一大块点击区域（透明区域同样会吃掉鼠标事件）。
//
// 高为什么是 380 而不是 320：窗口里除了 208 高的宠物，上方还有气泡
// （场景名 + 标题 + 最多两行正文 + 完成/推迟两个按钮 + 「打开客户端」+ 尾巴）。
// 而 .pet 是 justify-content: flex-end + overflow: hidden——**超出部分从顶部裁掉**，
// 高度不够时气泡的场景名/标题正好被吃掉，用户只看到一个半截气泡。
//
// 【为什么从 360 加到 380】气泡顶部新增了场景名那一行（12px × 1.4 ≈ 17px，
// 加 2px 间距 ≈ 20px）。原来的 360 已经是"刚好够"（实测气泡 + 间隙 + 宠物 + 内边距
// ≈ 359.6），再加 20px 就是 380——不加的直接症状是场景名被从顶部裁掉，而
// overflow: hidden 不会报错，只会让人以为"这个标签没生效"。
// 缩放时按同一比例放大（见 petWindowSize），故这里只需保证 1.0 倍够用。
const (
	petBaseWidth  = 240
	petBaseHeight = 380
)

// petWindowSize 返回给定缩放倍数下的窗口像素尺寸。
//
// 与前端 Pet.vue 的换算必须一致：前端把画布 CSS 尺寸算成
// FRAME_W*scale × FRAME_H*scale（192×208 × scale），窗口内还要留出气泡空间，
// 故这里按同一比例放大基础尺寸。scale 由 config.PetScaleValue() 夹取后再传入。
func petWindowSize(scale float64) (width, height int) {
	if math.IsNaN(scale) || scale <= 0 {
		scale = config.DefaultPetScale
	}
	width = int(math.Round(petBaseWidth * scale))
	height = int(math.Round(petBaseHeight * scale))
	if width <= 0 || height <= 0 {
		return petBaseWidth, petBaseHeight
	}
	return width, height
}

// petWindowOptions 组装宠物窗口的创建参数。
//
// 【字段选择说明】（对着 wails v3.0.0-beta.23 的 WebviewWindowOptions 逐条核对）：
//   - Frameless：无边框。与主窗不同，这里恒为 true——宠物不需要标题栏，
//     也不需要 macOS 交通灯（主窗必须 false 才能留住交通灯，见 main.go）。
//   - AlwaysOnTop：宠物要浮在所有窗口之上，否则切到别的应用就"消失"了。
//   - DisableResize：用户不能拖边缘改尺寸；尺寸只随缩放设置变化。
//     它同时挡住了「双击宠物触发 macOS 标题栏双击动作」的路径：wails 侧收到双击
//     后会走它自己的 handleTitlebarDoubleClick → 缺省 ToggleMaximise → zoom:，
//     而这条路径只有在拖拽修复之后才变得可达（修复前宠物本体根本没被识别成拖拽区，
//     双击根本到不了那一步）。DisableResize 会摘掉 NSWindowStyleMaskResizable，
//     预期使 zoom: 成为 no-op；这是刻意保留的连带保护，不要为了"能改尺寸"删掉它。
//   - Hidden：配置里关掉时启动即不显示（不是创建后再 Hide——那会闪一下）。
//   - BackgroundType + BackgroundColour：全透明。两者必须一起给：
//     只给 BackgroundType 时某些平台仍会先画一层不透明底色，表现为宠物周围一块黑边。
//     BackgroundColour 用零值字面量（RGBA{0,0,0,0}），不引任何构造器。
//   - InitialPosition + X/Y：直接落在上次记录的位置上，避免"先居中再跳过去"。
//   - Mac.DisableShadow：去掉系统阴影。带阴影的透明窗口在 macOS 上会有一圈灰边，
//     像素宠物看起来像贴在一张灰纸上。
//   - Windows.Theme：跟随系统明暗，与主窗一致（对透明窗口影响的是非客户区的绘制）。
func petWindowOptions(cfg *config.Config) application.WebviewWindowOptions {
	scale := cfg.PetScaleValue()
	width, height := petWindowSize(scale)

	return application.WebviewWindowOptions{
		Name:   petWindowName,
		Title:  appName,
		Width:  width,
		Height: height,
		// hash 路由：与主窗共用同一份前端产物，路由的 meta.bare 让 App.vue
		// 跳过 AppShell（否则宠物窗口里会出现侧栏 + 标题栏）。
		URL:              "/#/pet",
		Frameless:        true,
		AlwaysOnTop:      true,
		DisableResize:    true,
		Hidden:           !cfg.PetVisible,
		BackgroundType:   application.BackgroundTypeTransparent,
		BackgroundColour: application.RGBA{},
		// 宠物始终排除出屏幕捕获：共享屏幕/录屏时看不到它，用户自己仍能看到。
		// Wails 在 macOS/Windows 有实现，Linux 为 no-op（设置页会显示不支持）。
		ContentProtectionEnabled: true,
		// 【刻意不设 InitialPosition / X / Y】WindowXY 的 X/Y 是"相对某块屏幕
		// WorkArea 原点"，而 Position()/SetPosition() 用的是【全局逻辑坐标】。
		// 两者混用正是「多屏时宠物被拽回主屏」的成因：保存下来的是全局坐标，
		// 恢复时却被当作相对坐标解释，于是永远落在主屏。
		// 故位置统一在窗口创建后用 SetPosition 设置，见 main.go 与 petRestorePosition。
		Mac: application.MacWindow{
			DisableShadow: true,
			// 【macOS 的透明要两个条件同时成立，缺一个就是一块长方体底色】
			//   1. Backdrop = MacBackdropTransparent：只设 BackgroundType 时，Wails
			//      只把【原生窗口】配成透明，WKWebView 自身仍是不透明的——表现就是
			//      宠物周围糊着一块底色；
			//   2. 构建时带 -tags private_mac_apis：Backdrop 的透明依赖 WebKit 的私有
			//      drawsBackground，不加 tag 时该选项退化为「配置了但不生效」。
			//      由 scripts/build.sh 在 darwin 构建时默认注入 EXTRA_TAGS。
			// 官方文档原话：仅靠透明的 HTML/CSS 背景，无法让不透明的原生 WKWebView
			// 变透明。故这一行不是可选的装饰，删掉它 CSS 写得再对也白搭。
			Backdrop: application.MacBackdropTransparent,
		},
		Windows: application.WindowsWindow{Theme: application.SystemDefault},
	}
}

// 位置回落用的常量。
const (
	// petPositionMinVisible 判定"这个位置还算可见"的最小露出尺寸。
	//
	// 只需要露出一个能抓住的角：窗口有一部分在屏幕内，用户就能拖回来。
	// 要求整窗可见会把"跨两块屏摆放"（一半在左屏一半在右屏）误判成不可见。
	petPositionMinVisible = 48
	// petPositionMargin 回落位置距工作区右下角的边距，避免压在 Dock 上。
	petPositionMargin = 24
)

// petRestorePosition 计算宠物窗口应落的【全局】坐标。
//
// 【为什么不在建窗时用 X/Y】见 petWindowOptions 里的说明：那套坐标是相对屏幕的，
// 与 Position()/SetPosition() 的全局坐标混用，就是多屏下宠物被拽回主屏的成因。
//
// 两步：
//  1. 配置里的位置仍落在某块屏幕的工作区内 → 原样使用；
//  2. 否则（首次启动、拔掉了副屏、改了分辨率）→ 回落到主屏右下角。
//     不做这一步的症状是：宠物出现在一块已不存在的显示器坐标上，用户只看到
//     「宠物没了」，而且没有任何界面能把它找回来。
func petRestorePosition(app *application.App, cfg *config.Config) (int, int) {
	width, height := petWindowSize(cfg.PetScaleValue())
	x, y := cfg.PetPosition()
	if app == nil {
		return x, y
	}

	if petPositionVisible(app, x, y, width, height) {
		return x, y
	}
	if fx, fy, ok := petPrimaryBottomRight(app, width, height); ok {
		return fx, fy
	}
	return x, y
}

// petPositionVisible 报告窗口矩形是否仍有至少 petPositionMinVisible 露出在任一屏幕工作区。
//
// 【为什么只要求露出一个角】跨两块屏摆放（一半在左屏一半在右屏）是正常用法；
// 要求整窗可见会把它误判成不可见，然后强行拖回主屏。
func petPositionVisible(app *application.App, x, y, width, height int) bool {
	if app == nil {
		return false
	}
	for _, screen := range app.Screen.GetAll() {
		wa := screen.WorkArea
		if x+width > wa.X+petPositionMinVisible &&
			x < wa.X+wa.Width-petPositionMinVisible &&
			y+height > wa.Y+petPositionMinVisible &&
			y < wa.Y+wa.Height-petPositionMinVisible {
			return true
		}
	}
	return false
}

// petPrimaryBottomRight 返回主屏右下角的回落坐标；没有主屏时 ok=false。
func petPrimaryBottomRight(app *application.App, width, height int) (x, y int, ok bool) {
	if app == nil {
		return 0, 0, false
	}
	primary := app.Screen.GetPrimary()
	if primary == nil || primary.WorkArea.Width <= 0 || primary.WorkArea.Height <= 0 {
		return 0, 0, false
	}
	wa := primary.WorkArea
	return wa.X + wa.Width - width - petPositionMargin,
		wa.Y + wa.Height - height - petPositionMargin,
		true
}

// ensurePetOnScreenLocked 在显示前把窗口拉回主屏右下角（若当前位置已不可见）。
//
// 只在「显示」路径调用：启动建窗走 petRestorePosition，运行中的显隐/缩放走这里。
// 调用方必须已持有 petMu。
func (a *App) ensurePetOnScreenLocked(w application.Window) {
	if a.app == nil || a.cfg == nil || w == nil {
		return
	}
	x, y := w.Position()
	width, height := petWindowSize(a.cfg.PetScaleValue())
	if petPositionVisible(a.app, x, y, width, height) {
		return
	}
	if fx, fy, ok := petPrimaryBottomRight(a.app, width, height); ok {
		w.SetPosition(fx, fy)
	}
}
