// Package appdir 是应用数据目录的【唯一真相】。
//
// 应用的全部落盘文件（数据库 / 配置 / 日志 / 崩溃日志）都在同一个目录下：
//
//	<用户配置目录>/<appName>/
//
// 其中「用户配置目录」取自 os.UserConfigDir()（Windows: %AppData%，
// macOS: ~/Library/Application Support，Linux: ~/.config）。
//
// 为何单独成包：此前 config / database / logging / crash 各自写了一遍这段路径算法，
// 是「单一真相」铁律（AGENTS.md 铁律 1）的正面违反——改一处就分叉，且没人会发现。
// 现在四处共用本包，改路径只需改一处。
//
// 刻意不做成包级单例：appName 由组合根（main.go）传入，包级状态会让
// 「同一进程按不同 appName 跑两套配置」的场景变脆。
package appdir

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Dir 返回应用数据目录的绝对路径，并确保目录存在。
func Dir(appName string) (string, error) {
	if err := validateAppName(appName); err != nil {
		return "", err
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("获取用户配置目录失败: %w", err)
	}
	dir := filepath.Join(base, appName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("创建数据目录失败: %w", err)
	}
	return dir, nil
}

// File 返回数据目录下的文件绝对路径（只拼路径，不创建文件）。
// name 为目录内的文件名，如 "config.json"、"app.log"。
func File(appName, name string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("文件名不能为空")
	}
	if strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		return "", fmt.Errorf("文件名不得含路径分隔符或指向上级: %q", name)
	}
	dir, err := Dir(appName)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

// validateAppName 拒绝空名与含路径分隔符的名——否则 appName 可上跳目录，
// 把数据写到应用目录之外（如 appName = "../../.ssh"）。
func validateAppName(appName string) error {
	if strings.TrimSpace(appName) == "" {
		return fmt.Errorf("应用名不能为空")
	}
	if strings.ContainsAny(appName, `/\`) || appName == "." || appName == ".." {
		return fmt.Errorf("应用名不得含路径分隔符: %q", appName)
	}
	return nil
}
