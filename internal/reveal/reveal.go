// Package reveal 在系统文件管理器中打开一个目录。
//
// 用途：把应用数据目录（数据库/配置/日志所在处，见 internal/appdir）暴露给用户——
// 出问题时用户能自己把日志捞出来，而不是靠开发者口头教三套平台的路径。
//
// 为何不用 Wails 的 runtime.BrowserOpenURL：它要求 URL 形态，路径里的空格
// （macOS 的 "Application Support"、Windows 带空格的用户名）必须 URL 编码，
// 漏编码即静默失败——典型的「在别人机器上才炸」。这里走系统命令，路径作为
// 独立参数传入，不经 shell，空格与特殊字符天然安全。
package reveal

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

// Open 在系统文件管理器中打开 dir（非阻塞：不等文件管理器关闭）。
// dir 必须已存在；不存在时返回明确错误，而非把平台命令的报错原样抛出。
func Open(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("目录不存在: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("不是目录: %s", dir)
	}

	name, err := opener()
	if err != nil {
		return err
	}
	// 命令名与路径分开传参：不经 shell，路径中的空格/引号不会被解释。
	cmd := exec.Command(name, dir)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("调用 %s 打开目录失败: %w", name, err)
	}
	// 不 Wait 就返回：Start 过的子进程没人回收会变成僵尸进程，一直挂到应用退出。
	// 也不能同步 Wait——那会阻塞到文件管理器关闭；故异步回收，退出码不关心
	// （Windows 的 explorer 成功时也可能返回 1）。
	go func() { _ = cmd.Wait() }()
	return nil
}

// opener 返回当前平台用于「在文件管理器中打开」的命令名。
// 未知平台返回明确错误（不静默降级成 no-op）。
func opener() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		return "open", nil
	case "windows":
		return "explorer", nil
	case "linux":
		return "xdg-open", nil
	default:
		return "", fmt.Errorf("不支持的平台 %s，无法打开文件管理器", runtime.GOOS)
	}
}
