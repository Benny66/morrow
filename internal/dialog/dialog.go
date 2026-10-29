// Package dialog 封装 Wails v3 的原生对话框，暴露为前端可调用的形式。
//
// 与 v2 的差异（迁移时最容易踩的地方）：
//
//   - 能力长在【应用对象】上（app.Dialog.*），不再有 runtime.* 那套「传 ctx」的函数。
//     故这些函数接收 *application.App 而非 context.Context。
//
//   - 【对话框不再同步返回结果】。v2 的 runtime.MessageDialog 直接返回被点按钮的文本；
//     v3 的 MessageDialog 只有 Show()，结果经按钮回调送出。要保住"弹框→拿到用户选择"
//     这种同步调用，只能自己搭一条 channel 桥。
//
//     这不是权宜之计：v3 框架自己在 messageprocessor_dialog.go 里实现"前端发起的对话框
//     请求"时，用的就是同一套写法（建 chan → OnClick 写入 → Show() → 阻塞读）。
//     本包只是把同一形状用在 Go 侧调用上。
package dialog

import (
	"github.com/wailsapp/wails/v3/pkg/application"
)

// SelectFile 打开"选择文件"对话框，返回所选路径（取消返回空串）。
func SelectFile(app *application.App, title string) (string, error) {
	return app.Dialog.OpenFile().
		SetTitle(title).
		PromptForSingleSelection()
}

// SelectDirectory 打开"选择目录"对话框，返回所选路径（取消返回空串）。
//
// v3 没有独立的"选目录"入口：OpenFile 用 CanChooseDirectories / CanChooseFiles
// 两个开关切换形态，故这里显式关掉"可选文件"。
func SelectDirectory(app *application.App, title string) (string, error) {
	return app.Dialog.OpenFile().
		CanChooseFiles(false).
		CanChooseDirectories(true).
		SetTitle(title).
		PromptForSingleSelection()
}

// SelectSaveFile 打开"保存文件"对话框，返回目标路径（取消返回空串）。
//
// 这里用 SetOptions 而不是链式 SetTitle，是 v3.0.0 的一处框架缺口：
// SaveFileDialogOptions 结构体【有】Title 字段，Windows 实现也确实消费它
// （dialogs_windows.go 拿它做对话框标题），但 SaveFileDialogStruct【没暴露 SetTitle】
// ——OpenFileDialogStruct 与 MessageDialog 都有，只有保存对话框漏了。故只能整体赋值。
//
// 【SetOptions 是整体赋值，不是增量】：它把 Title/Message/Directory/Filename/ButtonText/
// Filters/Window 全部按传入值覆盖。新增字段时若仍走这条路，必须一并带上，否则会被静默清空。
func SelectSaveFile(app *application.App, title, defaultFilename string) (string, error) {
	dlg := app.Dialog.SaveFile()
	dlg.SetOptions(&application.SaveFileDialogOptions{
		Title:    title,
		Filename: defaultFilename,
	})
	return dlg.PromptForSingleSelection()
}

// Confirm 弹出确认框，返回用户是否确认。
//
// 两个必须注意的点：
//
//  1. AddButton 返回的是 *Button 而不是对话框本身，【不能串在链式调用中间】。
//     所以先建好对话框，再逐个把按钮加上。
//  2. 必须有一个按钮 SetAsCancel——用户按 Esc 或直接关掉对话框时，macOS 会把它
//     当作"取消按钮"的响应回传。少了它，取消这条路径就没有回调，<-answer 会一直等下去。
func Confirm(app *application.App, title, message string) (bool, error) {
	dlg := app.Dialog.Question().
		SetTitle(title).
		SetMessage(message)

	// 缓冲 1：按钮回调与这里的读取之间存在竞态，无缓冲通道会把结果丢掉
	answer := make(chan bool, 1)

	dlg.AddButton("是").SetAsDefault().OnClick(func() { answer <- true })
	dlg.AddButton("否").SetAsCancel().OnClick(func() { answer <- false })

	dlg.Show()
	return <-answer, nil
}
