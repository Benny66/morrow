// Package systemprefs 负责跳转到操作系统的隐私设置面板。
//
// 存在的理由：macOS 上「屏幕录制 / 专注 / 日历」三个权限一旦被拒绝，应用内
// 的任何说明窗都不会再弹（系统只弹一次）；用户必须自己去
// 「系统设置 → 隐私与安全 → XXX」里手动勾选。没有直达链接时，这一步对
// 非技术用户几乎是死路——所以提供一个「去系统设置」按钮。
//
// 【为什么不复用 internal/reveal】那个包只负责「在文件管理器里打开一个目录」，
// 语义是定位文件；这里是「打开系统设置并定位到某个权限面板」。两者的平台命令
// 不同、失败处理不同，混在一起会让两个调用点都读不懂。
package systemprefs

import (
	"errors"
	"fmt"
)

// Pane 是可以直达的隐私设置面板。
type Pane string

const (
	// PaneScreenRecording 屏幕录制（全屏检测用）。
	PaneScreenRecording Pane = "screen_recording"
	// PaneFocus 专注模式（系统专注联动用）。仅 macOS 13+ 的隐私面板里有这一项。
	PaneFocus Pane = "focus"
	// PaneCalendar 日历（日历忙闲感知用）。
	PaneCalendar Pane = "calendar"
)

// ErrUnsupported 表示当前平台没有可直达的隐私面板。
var ErrUnsupported = errors.New("当前平台不支持跳转系统隐私设置")

// URLFor 返回 pane 对应的 macOS 深链；未知 pane 返回空串。
//
// 【anchor 的稳定性】`x-apple.systempreferences:com.apple.preference.security?Privacy_XXX`
// 是 macOS 13+ 通用的深链形式：
//   - Privacy_ScreenCapture / Privacy_Calendars 长期稳定；
//   - Privacy_Focus 在部分版本上可能不存在——此时系统会退回「隐私与安全」根页，
//     用户仍能自己找到专注项，比打不开好。
//
// 未知 anchor 时 open 命令也不会报错（系统打开根页），所以「跳转失败」只能
// 靠 Pane 本身非法来判；见 Open 的文档。
func URLFor(p Pane) string {
	switch p {
	case PaneScreenRecording:
		return "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture"
	case PaneFocus:
		return "x-apple.systempreferences:com.apple.preference.security?Privacy_Focus"
	case PaneCalendar:
		return "x-apple.systempreferences:com.apple.preference.security?Privacy_Calendars"
	}
	return ""
}

// Valid 判断 pane 是否已知。
func Valid(p Pane) bool {
	return URLFor(p) != ""
}

// errUnknownPane 是非法 pane 的错误（内部辅助，保持文案一致）。
func errUnknownPane(p Pane) error {
	return fmt.Errorf("未知的隐私面板: %q", string(p))
}
