package main

import (
	"time"

	"morrow/internal/pause"
)

// systemActivity 把平台空闲时长适配成引擎需要的 ActivityProvider。
//
// 不支持的平台（Linux 等）返回 supported=false，引擎据此关闭顺延；
// 不能把 GetIdleDuration 的 0 当成「一直在输入」。
type systemActivity struct{}

func (systemActivity) IdleDuration() (time.Duration, bool) {
	return pause.GetIdleDuration(), pause.IdleSupported()
}
