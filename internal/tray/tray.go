// Package tray 封装系统托盘。
//
// 与 v2 的差异（这是一次净收益）：
//
//   - v2 时代 macOS 【没有托盘】：Wails v2 的 darwin frontend 自己占着 NSApplication
//     的 delegate，而所有 systray 库（getlantern 及其 fork energye/systray）都要
//     setDelegate 才能创建 NSStatusBar，两者互斥——链接期符号冲突、运行时崩溃。
//     项目为此写了 start_darwin.go 的 no-op，并多了一个 fork 依赖。
//
//   - v3 的 SystemTray 由框架在正确的 delegate 上实现，冲突从根上消失。于是
//     internal/tray 从「两个平台文件 + 一个 fork 依赖 + 一段解释为什么 macOS 不行」
//     缩成一层薄封装，deps.yaml 也少一个直接依赖。
//
// 图标注意事项：
//
//	SetIcon 收 PNG 字节，三个平台通用。macOS 菜单栏的惯例是"模板图"（单色、系统
//	按 alpha 通道上色），对应 API 是 SetTemplateIcon。本包默认用 SetIcon，因为脚手架
//	自带的是彩色应用图标；如果你想让 macOS 菜单栏图标与系统主题一致，换成一张单色
//	描边 PNG 并把下面那行改成 SetTemplateIcon 即可。
package tray

import "github.com/wailsapp/wails/v3/pkg/application"

// Handlers 是托盘交互回调，由调用方注入。
type Handlers struct {
	// OnShow 双击托盘图标或点"显示"时触发（恢复窗口）。
	OnShow func()
	// OnQuit 点"退出"时触发（优雅退出应用）。
	OnQuit func()
}

// Options 是托盘启动参数。
type Options struct {
	// Icon 是托盘图标二进制（PNG）。为空时用内置的占位图兜底——
	// 【这不是过度防御】：macOS 传空图标会在 systemtray_darwin.go 里
	// index out of range 直接 panic（不是返回 error），已实测。
	Icon []byte
	// Tooltip 悬停提示。
	Tooltip string
}

// Start 启动系统托盘，返回停止函数（优雅关闭时调用）。
//
// v3 里托盘挂在应用对象上（app.SystemTray.New），不再需要 v2 那套
// 「外部事件循环」的协商，也不依赖 Wails 何时启动完毕。
func Start(app *application.App, opts Options, h Handlers) (stop func(), err error) {
	// 用 len 而非 == nil：空切片（[]byte{}）同样会让 macOS 崩溃，
	// 而它并不等于 nil——v2 版本的这个判断漏掉了这种情况。
	if len(opts.Icon) == 0 {
		opts.Icon = placeholderIcon()
	}

	systray := app.SystemTray.New()
	systray.SetIcon(opts.Icon)
	systray.SetTooltip(opts.Tooltip)

	menu := app.NewMenu()
	menu.Add("显示").OnClick(func(*application.Context) {
		if h.OnShow != nil {
			h.OnShow()
		}
	})
	menu.AddSeparator()
	menu.Add("退出").OnClick(func(*application.Context) {
		if h.OnQuit != nil {
			h.OnQuit()
		}
	})
	systray.SetMenu(menu)

	// 双击图标恢复窗口（菜单里已有"显示"，这里照顾习惯）
	systray.OnDoubleClick(func() {
		if h.OnShow != nil {
			h.OnShow()
		}
	})

	return func() { systray.Destroy() }, nil
}

// placeholderIcon 返回 1x1 透明 PNG，仅作无图标时的兜底。
func placeholderIcon() []byte {
	return []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
		0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89, 0x00, 0x00, 0x00,
		0x0d, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x62, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00, 0x00, 0x00, 0x49,
		0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
	}
}
