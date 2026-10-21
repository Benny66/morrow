//go:build !darwin && !windows

package pause

import "time"

// GetIdleDuration 在非目标平台返回 0（Linux 不在本产品的支持范围，见 spec）。
//
// 【为什么留 stub 而不是删包】`go test ./internal/pause` 在 Linux 开发机上也会跑；
// 缺 stub 会报 undefined: GetIdleDuration，让人以为是实现有 bug。
// 返回 0 = 「永不空闲」，让空闲暂停信号在 Linux 上静默缺席，主流程照跑。
func GetIdleDuration() time.Duration { return 0 }
