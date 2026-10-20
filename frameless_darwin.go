//go:build darwin

package main

// frameless 返回是否无边框。
// macOS 必须 false：Frameless:true 会让 Wails 跳过 NSWindowStyleMaskTitled，
// 交通灯（红黄绿）消失。macOS 用 TitleBarHiddenInset 达到"透明贴边 + 保留交通灯"。
func frameless() bool {
	return false
}
