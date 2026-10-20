// Package event 定义管道契约中的「事件协议」。
//
// 契约（约定式，非结构强制）：Go 侧向前端推送事件时——
//   - 事件名 MUST 遵循 `<domain>:<action>`，domain 与业务模块名一致；
//   - 进度类 payload MUST 含 done / total；
//   - 结束类 MUST 含 ok，失败时附 message。
//
// 为何是约定式而非结构强制：不同业务的 payload 差异大，硬套结构会变成教条，
// 违背「管道只定契约、不定产品面」的定位。本包提供事件名与 payload 的
// 最小构造辅助，让「正确用法」成为「最省事的用法」，但不禁止自定义 payload。
package event

import "fmt"

// 动作常量（单一真相）。事件名 = domain + ":" + action。
const (
	// ActionProgress 进度（长任务推进中）。
	ActionProgress = "progress"
	// ActionDone 成功结束。
	ActionDone = "done"
	// ActionError 失败结束。
	ActionError = "error"
)

// Name 拼装符合约定的事件名（`<domain>:<action>`）。
func Name(domain, action string) string {
	return fmt.Sprintf("%s:%s", domain, action)
}

// Progress 是进度事件的载荷约定。
type Progress struct {
	// Done 已完成数量。
	Done int64 `json:"done"`
	// Total 总数。未知时可传 0，前端应做「不确定进度」处理。
	Total int64 `json:"total"`
}

// Result 是结束事件的载荷约定。
type Result struct {
	// Ok 是否成功。
	Ok bool `json:"ok"`
	// Message 失败时的中文提示（成功可为空）。
	Message string `json:"message,omitempty"`
}
