// Package apperr 定义管道契约中的「错误协议」。
//
// 契约：绑定方法（app.go 的 App 方法）返回的业务错误 MUST 为 *Error，
// 携带机器可读的 code 与面向用户的中文 message。前端据 code 分流，不解析文案。
//
// 序列化约定（关键，勿改）：
//   - main.go 把 apperr.MarshalError 注入 application.Options.MarshalError。
//   - v3 的契约是「返回合法 JSON 字节，或返回 nil 表示交回框架默认处理」，
//     由框架负责把这串字节送到前端；前端的 Error.message 就是这段 JSON 文本。
//   - 前端 frontend/src/lib/invoke.ts 收到后 JSON.parse 还原为 AppError。
//
// 【为什么返回字节而不是对象】（v2 → v3 的理由变了，结论没变）：
//
//	v2 的 ErrorFormatter 签名是 func(error) any，但项目被迫返回【字符串】——
//	因为 v2 的前端把 payload 包进 new Error(payload)，传对象会被强转成
//	"[object Object]"，结构化字段全部丢失（v2.15.0 实测）。
//	v3 换成了 MarshalError func(error) []byte，序列化这一步由框架完成，
//	不再经过 new Error(payload) 那条会丢结构的路径。
//
//	结论依然是「前端必须能 JSON.parse 出 {code,message}」，这一点由
//	frontend/src/lib/invoke.ts 的 normalizeError 与 internal/guard/parity_test.go 共同守住。
package apperr

import (
	"encoding/json"
	"errors"
)

// 错误码常量。新增错误码在此登记（单一真相），前端据其分流。
const (
	// CodeNotFound 记录不存在。
	CodeNotFound = "not_found"
	// CodeValidation 入参校验失败。
	CodeValidation = "validation"
	// CodeConflict 状态冲突（如重复创建）。
	CodeConflict = "conflict"
	// CodeInternal 未归类的系统错误（兜底）。
	CodeInternal = "internal"
)

// Error 是业务错误的统一形态。
// Code 供前端分流，Message 面向用户（中文，可直接展示），Detail 仅排查用不展示。
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
}

// Error 实现 error 接口，返回面向用户的中文消息。
func (e *Error) Error() string { return e.Message }

// New 构造业务错误。
func New(code, message string) *Error {
	return &Error{Code: code, Message: message}
}

// NotFound 记录不存在的业务错误。
func NotFound(message string) *Error {
	return &Error{Code: CodeNotFound, Message: message}
}

// Validation 入参校验失败的业务错误。
func Validation(message string) *Error {
	return &Error{Code: CodeValidation, Message: message}
}

// Conflict 状态冲突的业务错误。
func Conflict(message string) *Error {
	return &Error{Code: CodeConflict, Message: message}
}

// Wrap 把任意错误归一化为 *Error：已是 *Error 原样返回，
// 否则兜底为 internal（Message 为中文兜底文案，Detail 保留原始错误供排查）。
func Wrap(err error) *Error {
	if err == nil {
		return nil
	}
	var ae *Error
	if errors.As(err, &ae) {
		return ae
	}
	return &Error{Code: CodeInternal, Message: "系统错误", Detail: err.Error()}
}

// MarshalError 是 application.Options.MarshalError 的实现：把 error 序列化为 JSON 字节。
//
// 返回 nil 表示「交回框架的默认处理」——只在序列化失败时这么做（理论上不会发生，
// 因为 Error 全是可导出的基本类型字段）。不要把 nil 用作「没有错误」的表达：
// 本函数只在确实有错误时被调用。
func MarshalError(err error) []byte {
	ae := Wrap(err)
	if ae == nil {
		return nil
	}
	b, mErr := json.Marshal(ae)
	if mErr != nil {
		return nil
	}
	return b
}
