package main

import (
	"errors"
	"fmt"
	"testing"

	"morrow/internal/apperr"
)

// TestRestCompletionSettled 锁定「休息模式自动完成」的错误分类。
//
// 语义：validation（该提醒已处理）与 not_found（记录不存在）说明这条已被
// 宠物/首页/通知提前处理，会话已经没有未结算的事实，可以照常 Reset；
// 内部错误必须保持 Done 等待重试，不能把 CheckIn 丢掉。
func TestRestCompletionSettled(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"已处理（validation）", apperr.Validation("该提醒已处理"), true},
		{"记录不存在（not_found）", apperr.NotFound("该提醒已失效"), true},
		{"包装后的已处理", apperr.Wrap(apperr.Validation("该提醒已处理")), true},
		{"内部错误", apperr.Wrap(fmt.Errorf("数据库不可用")), false},
		{"普通错误", errors.New("boom"), false},
		{"无错误不参与分类", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := restCompletionSettled(tc.err); got != tc.want {
				t.Fatalf("restCompletionSettled(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
