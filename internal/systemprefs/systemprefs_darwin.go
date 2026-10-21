//go:build darwin

package systemprefs

import (
	"fmt"
	"os/exec"
)

// Open 用 `open` 命令打开 pane 对应的系统隐私设置面板（非阻塞）。
//
// 【为什么用命令而不是 NSWorkspace.openURL】与 internal/reveal 同一取舍：
// URL 作为独立参数传给 `open`，不经 shell，也不可能被路径里的特殊字符干扰；
// cgo 方案要多一层 Objective-C 桥，收益为零。
//
// 【失败判据】`open` 只在「无法解析 URL 或找不到处理程序」时返回非零；
// anchor 不存在时系统会打开「隐私与安全」根页并以 0 退出——所以这里把
// 「已打开但可能停在根页」当作成功，前端只在 err != nil 时给文字兜底。
func Open(p Pane) error {
	u := URLFor(p)
	if u == "" {
		return errUnknownPane(p)
	}
	cmd := exec.Command("open", u)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("打开系统设置失败: %w", err)
	}
	// 不 Wait 就返回：同步 Wait 会阻塞到用户关闭设置页。异步回收避免僵尸进程。
	go func() { _ = cmd.Wait() }()
	return nil
}

// Supported 报告当前平台是否支持跳转（macOS 恒 true）。
func Supported() bool { return true }
