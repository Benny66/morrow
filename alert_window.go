package main

// 全屏强提醒窗口的建窗参数与几何换算。
//
// 【它解决什么问题】系统通知在用户开了「勿扰」、通知被折叠、或用户正盯着别的窗口时
// 很容易被忽略——而健康提醒的价值恰恰在于"到点就被看见"。故加一个铺满主屏工作区的
// 透明窗口，到点时把整块屏幕压上一层半透明遮罩，中间放一张卡片等用户处理。
//
// 【与宠物窗口的关系】两者是并列的独立顶层窗口（都在 main.go 里创建）：
//   - 宠物是"轻量存在感"（一直待在角落），强提醒是"强打断"（到点才出现）；
//   - 两者的显隐互不依赖，也不共享任何状态。宠物气泡要不要让位给强提醒，
//     见 Pet.vue / Alert.vue 的说明（本仓不为它新增事件域）。
//
// 【透明是三层 + 一个构建标签的共同结果，与 pet_window.go 完全同理】
// 原生窗口透明（BackgroundType/BackgroundColour + Mac.Backdrop）、CSS 三层背景透明、
// 以及 darwin 上带 -tags private_mac_apis 构建——少任何一层，遮罩都会变成一块死色。
//
// 建窗本身仍发生在 main.go 的组合根里，本文件只提供选项与几何计算。

import (
	"strconv"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// alertWindowName 是主屏强提醒窗口在 WindowManager 里的注册名。
// app.go 的 allAlertWindows() 按这个名字回查窗口（与 mainWindow/petWindow 同构）。
const alertWindowName = "alert"

// alertSecondaryNamePrefix 是副屏强提醒窗口的名字前缀，形如 "alert-2" / "alert-3"。
// 【为什么从 2 开始】主屏叫 "alert"（无后缀）；副屏从 2 编号，视觉上「alert-1」
// 与「alert」难分。前端根据 label 是否等于 alertWindowName 判 primary。
const alertSecondaryNamePrefix = "alert-"

// 主屏不可用时的兜底尺寸。
//
// 【为什么需要一个兜底】GetPrimary() 在极少数情况下会返回 nil（平台初始化未完成、
// 无显示器的 headless 会话）。这时如果把 0×0 交给建窗参数，窗口会缩成一个点，
// 用户此后每次到点都"什么都没发生"，且没有任何界面能解释为什么。
// 给一个常见笔记本分辨率至少能让提醒看得见——错位的窗口远比不出现的强提醒好。
const (
	alertFallbackWidth  = 1280
	alertFallbackHeight = 800
)

// ScreenRect 是一块屏幕的 WorkArea 全局矩形。
type ScreenRect struct {
	X, Y, Width, Height int
	IsPrimary           bool
}

// alertGeometries 返回【所有屏幕】的强提醒窗口矩形（US-18 多屏覆盖）。
//
// 每块屏一个 WorkArea（已扣掉菜单栏/Dock/任务栏）——盖住 Dock 与菜单栏既没意义，
// 也会让用户没法点它们。返回顺序里 primary 一定是第一位，让 main.go 建窗时
// 第一个窗口能拿到 "alert"（无后缀）的名字。
//
// 【为什么不能只覆盖主屏】用户投双屏时，副屏上正放着的资料/演示完全看不到提醒；
// 竞品 Stretchly / Time Out 早就把「所有显示器同时被压住」当成基本能力。
// 只覆盖主屏的旧行为在 ticket 009 前是刻意保守（怕副屏闪烁），现在补上。
func alertGeometries(app *application.App) []ScreenRect {
	if app == nil {
		return []ScreenRect{{X: 0, Y: 0, Width: alertFallbackWidth, Height: alertFallbackHeight, IsPrimary: true}}
	}
	screens := app.Screen.GetAll()
	out := make([]ScreenRect, 0, len(screens))
	primary := app.Screen.GetPrimary()
	for _, s := range screens {
		if s == nil {
			continue
		}
		wa := s.WorkArea
		if wa.Width <= 0 || wa.Height <= 0 {
			continue
		}
		out = append(out, ScreenRect{
			X: wa.X, Y: wa.Y, Width: wa.Width, Height: wa.Height,
			IsPrimary: primary != nil && s.ID == primary.ID,
		})
	}
	if len(out) == 0 {
		return []ScreenRect{{X: 0, Y: 0, Width: alertFallbackWidth, Height: alertFallbackHeight, IsPrimary: true}}
	}
	// primary 顶到第一位：main.go 建窗顺序 = 名字分配顺序。
	for i := range out {
		if out[i].IsPrimary && i != 0 {
			out[0], out[i] = out[i], out[0]
			break
		}
	}
	return out
}

// alertGeometry 计算主屏强提醒窗口应覆盖的矩形。
//
// 【保留为独立函数】main.go 现在用 alertGeometries 拿全屏列表；本函数给单测
// 与将来「只要主屏」的调用点留一个稳定入口，内部复用同一套几何规则。
func alertGeometry(app *application.App) (x, y, width, height int) {
	rects := alertGeometries(app)
	if len(rects) == 0 {
		return 0, 0, alertFallbackWidth, alertFallbackHeight
	}
	r := rects[0]
	return r.X, r.Y, r.Width, r.Height
}

// alertWindowOptions 组装强提醒窗口的创建参数。
//
// 尺寸由调用方传入（见 alertGeometry）：建窗时就给定正确尺寸，避免"先小后大"跳一下。
//
// 【字段选择说明】（对着 wails v3.0.0-beta.23 的 WebviewWindowOptions 逐条核对，
// 与 petWindowOptions 的取舍保持一致，改动时请两边一起看）：
//   - Frameless：无边框。全屏窗不需要标题栏，也不需要 macOS 交通灯。
//   - AlwaysOnTop：必须浮在所有窗口之上——它存在的唯一目的就是抢注意力。
//   - DisableResize：用户拖不到边缘；尺寸只由 alertGeometry 决定（换屏幕后重启生效）。
//   - Hidden：**必须**为 true。可见性由前端按"有没有待处理提醒"决定（见 Alert.vue），
//     建窗即显示会让它在每个启动瞬间闪一下全屏遮罩。
//   - BackgroundType + BackgroundColour：原生层全透明，两者必须一起给
//     （只给前者时某些平台仍先画一层不透明底色，遮罩就变成一整块死色）。
//   - Mac.DisableShadow：全屏窗的阴影没有意义，且在透明窗口上会留下一圈灰边。
//   - Mac.Backdrop：同宠物窗口——WKWebView 自身也必须被配成透明，
//     且依赖构建标签 private_mac_apis（见 pet_window.go 的长注释）。
//   - Windows.Theme：跟随系统明暗，与主窗一致。
//
// 【刻意不设 X/Y，也刻意不给任何元素 --wails-draggable】全屏窗不该被拖动：
// 位置由 alertGeometry 决定，用户把它拖歪之后没有任何入口能拖回来。
func alertWindowOptions(name string, width, height int) application.WebviewWindowOptions {
	if width <= 0 || height <= 0 {
		width, height = alertFallbackWidth, alertFallbackHeight
	}
	if name == "" {
		name = alertWindowName
	}

	return application.WebviewWindowOptions{
		Name:   name,
		Title:  appName,
		Width:  width,
		Height: height,
		// hash 路由 + meta.bare：与主窗共用同一份前端产物，该路由跳过 AppShell
		// （否则全屏窗里会画出侧栏与标题栏，遮罩也就不透明了）。
		// 【?p=1 的用途】Alert.vue 通过 URL query 判自己是 primary 还是副屏：
		//   primary 显示完整卡片 + 完成/推迟/跳过三按钮（US-19）；
		//   副屏显示简化卡片（标题 + 完成），保证用户在任一副屏都能立刻处理。
		// 【用 query 而不是 window label】label 是 Go 侧的内部标识，暴露给前端会
		// 让"命名规则"变成第二处真相；query 是一次性的、语义明确的字符串。
		URL:              "/#/alert?p=" + alertPrimaryFlag(name),
		Frameless:        true,
		AlwaysOnTop:      true,
		DisableResize:    true,
		Hidden:           true,
		BackgroundType:   application.BackgroundTypeTransparent,
		BackgroundColour: application.RGBA{},
		Mac: application.MacWindow{
			DisableShadow: true,
			Backdrop:      application.MacBackdropTransparent,
		},
		Windows: application.WindowsWindow{Theme: application.SystemDefault},
	}
}

// alertPrimaryFlag 由窗口名派生 URL 上的 primary 标记：主屏 = "1"，副屏 = "0"。
func alertPrimaryFlag(name string) string {
	if name == alertWindowName {
		return "1"
	}
	return "0"
}

// AlertSecondaryName 供 main.go 拼副屏窗口名（"alert-2" / "alert-3" ...）。
func AlertSecondaryName(idx int) string {
	return alertSecondaryNamePrefix + strconv.Itoa(idx)
}
