package main

import "runtime"

// captureProtectionSupported 返回当前平台是否支持把窗口排除出屏幕捕获。
//
// Wails v3 在 macOS / Windows 有实现；Linux 等平台是 no-op，UI 必须如实降级
// （见 ADR 0006 的 spike 结论）。不在启动时探测：能力是编译期平台决定的，
// 探测只会多一条可能失败的路径。
func captureProtectionSupported() bool {
	return runtime.GOOS == "darwin" || runtime.GOOS == "windows"
}
