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
	// OnShowPending 「查看待处理」入口：把主窗拉到前面并让 Home.vue 滚到待处理区。
	// 与 OnShow 分开是为了让菜单栏里两个入口指向两个不同的心智（US-16）。
	OnShowPending func()
	// OnQuit 点"退出"时触发（优雅退出应用）。
	OnQuit func()
	// StatusLines 返回若干行当前状态文本（US-15 / US-17 / US-9 的菜单栏承载）。
	// 每次 Refresh 都会被调；空数组 = 不显示顶部区域。
	//
	// 【为什么是数组而不是单个字符串】菜单栏顶部要同时显示「今日 X/Y · 待处理 N」
	// 与「下一条 HH:MM · 休息/免打扰状态」两行；用字符串换行 macOS NSMenuItem 不会
	// 自动折行，会看到一行很长的文本。多项更清晰。
	StatusLines func() []string
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

// Instance 是已启动托盘的句柄；Refresh 用于在状态变化时重建菜单。
type Instance struct {
	systray *application.SystemTray
	app     *application.App
	h       Handlers
}

// Start 启动系统托盘，返回实例句柄与停止函数（优雅关闭时调用）。
//
// v3 里托盘挂在应用对象上（app.SystemTray.New），不再需要 v2 那套
// 「外部事件循环」的协商，也不依赖 Wails 何时启动完毕。
func Start(app *application.App, opts Options, h Handlers) (*Instance, func(), error) {
	// 用 len 而非 == nil：空切片（[]byte{}）同样会让 macOS 崩溃，
	// 而它并不等于 nil——v2 版本的这个判断漏掉了这种情况。
	if len(opts.Icon) == 0 {
		opts.Icon = placeholderIcon()
	}

	systray := app.SystemTray.New()
	systray.SetIcon(opts.Icon)
	systray.SetTooltip(opts.Tooltip)

	inst := &Instance{systray: systray, app: app, h: h}
	inst.rebuildMenu()

	// 双击图标恢复窗口（菜单里已有"显示"，这里照顾习惯）
	systray.OnDoubleClick(func() {
		if h.OnShow != nil {
			h.OnShow()
		}
	})

	return inst, func() { systray.Destroy() }, nil
}

// Refresh 重建菜单。由 App 在检测到暂停状态、休息模式预警或倒计时切换时调用。
//
// 【为什么每次全量重建而不是原地改】v3 的 MenuItem 没有公开 SetTitle；改文案
// 只能重新 NewMenu + SetMenu。整棵菜单只有 3–4 项，重建代价可以忽略。
//
// 【为什么不后台定时 Refresh】状态变化是事件驱动的（restModeAutoComplete 每秒 tick
// 已经会检测到，pauseProvider.State 只在变化时触发），让 App 在检测到变化时
// 主动调 Refresh，比让 tray 自己起 goroutine 更省。
func (i *Instance) Refresh() {
	if i == nil {
		return
	}
	i.rebuildMenu()
}

func (i *Instance) rebuildMenu() {
	menu := i.app.NewMenu()

	// 顶部状态区：多行 disabled 项，每行一条状态信息（US-15 三项 + US-17 一项）。
	if i.h.StatusLines != nil {
		lines := i.h.StatusLines()
		if len(lines) > 0 {
			for _, text := range lines {
				if text == "" {
					continue
				}
				item := menu.Add(text)
				item.SetEnabled(false)
			}
			menu.AddSeparator()
		}
	}

	menu.Add("打开主窗").OnClick(func(*application.Context) {
		if i.h.OnShow != nil {
			i.h.OnShow()
		}
	})
	// 「查看待处理」独立入口（US-16）：拉起主窗后由前端滚到待处理区。
	// 与「打开主窗」分开是因为「打开」只恢复窗口，「查看」额外做一次前端跳转。
	if i.h.OnShowPending != nil {
		menu.Add("查看待处理").OnClick(func(*application.Context) {
			i.h.OnShowPending()
		})
	}
	menu.AddSeparator()
	menu.Add("退出").OnClick(func(*application.Context) {
		if i.h.OnQuit != nil {
			i.h.OnQuit()
		}
	})
	i.systray.SetMenu(menu)
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
