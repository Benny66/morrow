package apperr

import (
	"encoding/json"
	"errors"
	"testing"
)

// TestMarshalErrorReturnsJSONBytes 是本包最关键的契约：
// MarshalError MUST 产出【合法 JSON 字节】，前端才能 JSON.parse 回 {code,message}。
//
// v2 → v3 这里变过一次：v2 的 ErrorFormatter 必须返回字符串（因为前端会走
// new Error(payload)，对象会被强转成 "[object Object]"）；v3 由框架序列化，
// 契约变成 []byte。结论不变，形态变了。
func TestMarshalErrorReturnsJSONBytes(t *testing.T) {
	out := MarshalError(NotFound("记录不存在"))

	if out == nil {
		t.Fatal("MarshalError MUST 返回非 nil 字节——返回 nil 会被框架当作「无错误」交回默认处理")
	}

	var got Error
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("MarshalError 返回的字节不是合法 JSON: %v（原值 %q）", err, out)
	}
	if got.Code != CodeNotFound {
		t.Errorf("期望 code=%q，实际 %q", CodeNotFound, got.Code)
	}
	if got.Message != "记录不存在" {
		t.Errorf("期望 message=记录不存在，实际 %q", got.Message)
	}
}

// TestWrapUnknownError 未知错误 MUST 兜底为 internal，且不把英文堆栈当 message 展示。
func TestWrapUnknownError(t *testing.T) {
	raw := errors.New("sqlite: no such table")
	got := Wrap(raw)

	if got.Code != CodeInternal {
		t.Errorf("未知错误期望 code=%q，实际 %q", CodeInternal, got.Code)
	}
	if got.Message != "系统错误" {
		t.Errorf("未知错误 message 应为中文兜底，实际 %q", got.Message)
	}
	if got.Detail != "sqlite: no such table" {
		t.Errorf("原始错误应保留在 Detail，实际 %q", got.Detail)
	}
}

// TestWrapPreservesBusinessError 已是 *Error 的错误 MUST 原样返回，不被兜底覆盖。
func TestWrapPreservesBusinessError(t *testing.T) {
	orig := NotFound("资产不存在")
	got := Wrap(orig)

	if got != orig {
		t.Errorf("业务错误应原样返回，实际被替换为 %+v", got)
	}
}

// TestWrapWrappedError 用 %w 包裹的业务错误 MUST 经 errors.As 解出。
func TestWrapWrappedError(t *testing.T) {
	orig := NotFound("资产不存在")
	got := Wrap(errors.Join(errors.New("上下文"), orig))

	if got.Code != CodeNotFound {
		t.Errorf("包裹的业务错误应被解出，期望 code=%q，实际 %q", CodeNotFound, got.Code)
	}
}

// TestWrapNil nil 输入返回 nil（不 panic）。
func TestWrapNil(t *testing.T) {
	if got := Wrap(nil); got != nil {
		t.Errorf("Wrap(nil) 应返回 nil，实际 %+v", got)
	}
}
