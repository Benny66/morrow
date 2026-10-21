//go:build !darwin

package systemprefs

// Open 在非 macOS 平台返回 ErrUnsupported。
//
// Windows / Linux 没有这类「按应用授权」的隐私面板；UI 层已经用
// pause.RequiresScreenCapture / FocusSupported / CalendarSupported 隐藏了
// 相关入口，这里只是兜底，避免误调用时静默成功。
func Open(p Pane) error { return ErrUnsupported }

// Supported 在非 macOS 平台返回 false。
func Supported() bool { return false }
