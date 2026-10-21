// Package dialog 的 permission.go 负责 macOS 敏感权限申请的应用内说明文案。
//
// 这一层的存在只为了两件事：
//  1. 在用户开启对应功能时，把「为什么要这个权限、拿到之后做什么、不做什么」讲清楚；
//  2. 记住用户的选择（拒绝过就不再弹）——避免 macOS 每次调用都跳系统设置页。
//
// 具体申请动作在 pause.RequestScreenCapture / pause.RequestFocusAccess /
// pause.RequestCalendarAccess 里（那边直接调对应系统 API）。
// 本文件只关心「要不要弹提示、弹什么内容」，不碰 API 本身。
package dialog

import (
	"github.com/wailsapp/wails/v3/pkg/application"
)

// RequestSensitiveAccessConsent 在真正调 macOS 系统 API 之前弹一层应用自己的
// 说明对话框，把「只读什么、不读什么、拒绝后怎么办」讲清楚。
//
// 【为什么不能省这一步】macOS 的原生授权弹窗文案是系统写死的（「允许 XX 控制这台电脑」
// 或「允许 XX 访问日历」），用户看不到「我们只读窗口矩形」或「我们只读忙碌布尔」
// 这类【产品自己的隐私承诺】。ADR 0004 / ADR 0005 的承诺必须在这一层落地；
// 否则用户会按默认「拒绝」，功能就废了。
//
// 返回值：true = 用户点了「继续授权」，调用方可以接着触发系统弹窗；
// false = 用户点了「暂不开启」或关掉对话框，调用方【不能】再触系统弹窗，
// 并把对应的 AskedBefore 记为 true 以避免下次再问。
func RequestSensitiveAccessConsent(app *application.App, title, message string) (bool, error) {
	dlg := app.Dialog.Question().
		SetTitle(title).
		SetMessage(message)

	answer := make(chan bool, 1)
	dlg.AddButton("继续授权").SetAsDefault().OnClick(func() { answer <- true })
	dlg.AddButton("暂不开启").SetAsCancel().OnClick(func() { answer <- false })

	dlg.Show()
	return <-answer, nil
}

// RequestScreenCaptureConsent 是屏幕录制权限的说明窗（快捷方式）。
//
// 与 RequestSensitiveAccessConsent 共用同一份对话框形状，只是把文案固定下来；
// 三个权限（屏幕录制 / 专注 / 日历）都用同一个函数入口，避免三份近似拷贝。
func RequestScreenCaptureConsent(app *application.App) (bool, error) {
	return RequestSensitiveAccessConsent(app, "开启全屏检测",
		"需要你授权【屏幕录制】权限，Morrow 只会读取前台窗口的【位置与尺寸】用来判断是否全屏。\n\n"+
			"不会截图、不会录屏、不会读取窗口标题或内容、不会把任何数据落盘或上传。\n\n"+
			"如果不方便授权，可以只保留【空闲】和【会议应用】两个信号，功能仍然可用。")
}

// RequestP1AccessConsent 是 P1 敏感权限（专注 / 日历）的说明窗。
//
// 保留旧名字：调用点（app.go RequestFocusAccess / RequestCalendarAccess）已经用了；
// 内部委托到 RequestSensitiveAccessConsent，语义完全一致。
func RequestP1AccessConsent(app *application.App, title, message string) (bool, error) {
	return RequestSensitiveAccessConsent(app, title, message)
}
