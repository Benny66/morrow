//go:build !darwin

package main

// frameless 返回是否无边框。
// Windows/Linux 用 Frameless:true + 自绘三按钮（TitleBar.vue 渲染）。
func frameless() bool {
	return true
}
