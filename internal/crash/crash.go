// Package crash 负责全局崩溃捕获与崩溃日志落盘。
//
// Wails 默认开启 panic recovery（DisablePanicRecovery=false），但只打日志。
// 这里额外包一层 recover，把崩溃堆栈写入独立的 crash-<时间戳>.log，
// 便于事后排查，且不污染常规日志。
package crash

import (
	"fmt"
	"os"
	"runtime"
	"time"

	"morrow/internal/appdir"
)

// Wrap 在独立 goroutine 运行 fn，捕获 panic 并落盘崩溃日志。
// 用于包裹启动期或长驻的业务逻辑。
func Wrap(appName string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				writeCrashLog(appName, r)
			}
		}()
		fn()
	}()
}

// writeCrashLog 将崩溃信息写入独立日志文件（与常规日志同目录，见 internal/appdir）。
func writeCrashLog(appName string, r interface{}) {
	// 时间戳文件名，避免覆盖历史崩溃
	ts := time.Now().Format("20060102-150405")
	path, err := appdir.File(appName, fmt.Sprintf("crash-%s.log", ts))
	if err != nil {
		return
	}

	buf := make([]byte, 64*1024)
	n := runtime.Stack(buf, true)
	content := fmt.Sprintf("panic: %v\n\n%s", r, string(buf[:n]))
	_ = os.WriteFile(path, []byte(content), 0o644)
}
