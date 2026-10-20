// Package appinfo 汇总应用的版本与运行环境信息，供日志与「关于」界面使用。
//
// 版本来源分三层回退（见 Resolve）：
//  1. 构建期注入：-ldflags "-X <module>/internal/appinfo.InjectedVersion=0.1.0"
//  2. Go build info 的 VCS 提交短哈希（仅当用裸 go build 且未传 -buildvcs=false 时可得）
//  3. "dev"
//
// 注意：wails3 也会传 -buildvcs=false，第 2 层在实际打包时【拿不到】。
// 所以产物必须有第 1 层的注入，否则版本恒为 "dev"——而链接器的 -X
// 对不存在的符号是静默忽略的，写错路径不会有任何提示（故由
// internal/guard/wiring_test.go 核对注入目标）。
//
// 版本号与提交号分开注入是刻意的：版本号受 NSIS 的 VIProductVersion 限制必须是
// 数字点分格式（0.1.0），没有位置放提交信息；而排查时提交号比版本更精确。
//
// 注入链（v3 与 v2 不同，改前先看）：
//   - v2：`wails build -ldflags`，注入点唯一 = scripts/build.sh。
//   - v3：`wails3 build` 【不转发 -ldflags】（已实测），故注入点在 Taskfile：
//     scripts/build.sh（读 build/config.yml 的 info.version 作为单一真相）
//     → `wails3 build VERSION=x.y.z COMMIT=abc`
//     → 根 Taskfile.yml 的 VERSION_LDFLAGS 拼出 -X
//     → 各平台 Taskfile 把它拼进 BUILD_FLAGS
//
// 本包的 InjectedVersion / InjectedCommit 两个变量名即注入目标，护栏会核对它们存在。
//
// 为何要有这个包：此前版本号只存在于产物文件名里，二进制完全不知道自己是哪个版本，
// app.log 里也没有版本上下文——用户报障说「我用的是 0.3」，无从核对。
package appinfo

import (
	"fmt"
	"runtime"
	"runtime/debug"

	"my-notice/internal/appdir"
)

// InjectedVersion 是构建期注入的版本号（数字点分，如 0.1.0），默认空串。
// 命名不用 Version 是为了给同名函数 Version() 让路，避免同名变量与函数混淆。
var InjectedVersion string

// InjectedCommit 是构建期注入的 VCS 提交短哈希，默认空串。
// 与版本号分开注入：版本号必须是数字点分格式（NSIS 限制），放不下提交信息。
var InjectedCommit string

// Resolve 返回版本号：注入值 > VCS 提交短哈希 > "dev"。
func Resolve() string {
	if InjectedVersion != "" {
		return InjectedVersion
	}
	if c := commit(); c != "" {
		return c
	}
	return "dev"
}

// Commit 返回提交短哈希：注入值优先，回退 build info，都没有则空串。
func Commit() string {
	if InjectedCommit != "" {
		return InjectedCommit
	}
	return commit()
}

// commit 返回 VCS 提交短哈希；非 git 构建或信息缺失时返回空串。
func commit() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" && len(s.Value) >= 7 {
			return s.Value[:7]
		}
	}
	return ""
}

// Platform 返回 "darwin/arm64" 形式的平台标识。
// 与 runtime.GOOS 分开暴露，避免调用方各自拼字符串。
func Platform() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}

// Info 是可下发给前端的应用信息（JSON tag 用 snake_case）。
type Info struct {
	// Version 版本号（见 Resolve）。
	Version string `json:"version"`
	// Commit VCS 提交短哈希，无则空串。
	Commit string `json:"commit"`
	// Platform 形如 darwin/arm64。
	Platform string `json:"platform"`
	// DataDir 应用数据目录（数据库/配置/日志同处）。
	DataDir string `json:"data_dir"`
	// LogFile 当前日志文件的绝对路径。
	LogFile string `json:"log_file"`
}

// Current 组装应用信息；数据目录与日志路径取自 appdir（路径的唯一真相）。
func Current(appName string) (Info, error) {
	dir, err := appdir.Dir(appName)
	if err != nil {
		return Info{}, err
	}
	logFile, err := appdir.File(appName, "app.log")
	if err != nil {
		return Info{}, err
	}
	return Info{
		Version:  Resolve(),
		Commit:   Commit(),
		Platform: Platform(),
		DataDir:  dir,
		LogFile:  logFile,
	}, nil
}

// String 返回单行摘要，用于日志首行。
func (i Info) String() string {
	return fmt.Sprintf("version=%s commit=%s platform=%s", i.Version, i.Commit, i.Platform)
}
