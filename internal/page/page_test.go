package page

import "testing"

// TestNormalizedBounds 归一化 MUST 夹取页码下界与页大小上下界（护栏断言的正是这条逻辑）。
func TestNormalizedBounds(t *testing.T) {
	cases := []struct {
		name         string
		in           Request
		wantPage     int
		wantPageSize int
	}{
		{"零值取默认", Request{}, 1, DefaultPageSize},
		{"页码为负夹到1", Request{Page: -5, PageSize: 10}, 1, 10},
		{"页大小为负取默认", Request{Page: 2, PageSize: -1}, 2, DefaultPageSize},
		{"页大小超上限夹取", Request{Page: 1, PageSize: 99999}, 1, MaxPageSize},
		{"合法值原样", Request{Page: 3, PageSize: 50}, 3, 50},
		{"恰好等于上限", Request{Page: 1, PageSize: MaxPageSize}, 1, MaxPageSize},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := c.in.Normalized()
			if got.Page != c.wantPage {
				t.Errorf("page: 期望 %d，实际 %d", c.wantPage, got.Page)
			}
			if got.PageSize != c.wantPageSize {
				t.Errorf("page_size: 期望 %d，实际 %d", c.wantPageSize, got.PageSize)
			}
		})
	}
}

// TestOffsetUsesNormalized 未归一化的请求算 offset 也不得产生负数。
func TestOffsetUsesNormalized(t *testing.T) {
	if got := (Request{Page: -3, PageSize: 10}).Offset(); got != 0 {
		t.Errorf("页码为负时 offset 应为 0，实际 %d", got)
	}
	if got := (Request{Page: 3, PageSize: 20}).Offset(); got != 40 {
		t.Errorf("第 3 页 20 条 offset 应为 40，实际 %d", got)
	}
}

// TestNewResultFillsEcho 结果 MUST 回填归一化后的 page/page_size，供前端回显一致值。
func TestNewResultFillsEcho(t *testing.T) {
	got := NewResult(Request{Page: 0, PageSize: 0}, []string{"a"}, 1)

	if got.Page != 1 || got.PageSize != DefaultPageSize {
		t.Errorf("应回填归一化值 (1, %d)，实际 (%d, %d)", DefaultPageSize, got.Page, got.PageSize)
	}
	if got.Total != 1 || len(got.List) != 1 {
		t.Errorf("total/list 应原样保留，实际 total=%d len=%d", got.Total, len(got.List))
	}
}

// TestNewResultNilListBecomesEmpty nil 列表 MUST 序列化为 []，避免前端拿到 null。
func TestNewResultNilListBecomesEmpty(t *testing.T) {
	got := NewResult[string](Request{Page: 1, PageSize: 10}, nil, 0)

	if got.List == nil {
		t.Error("nil 列表应归一化为空切片（保证 JSON 是 [] 而非 null）")
	}
	if len(got.List) != 0 {
		t.Errorf("空列表长度应为 0，实际 %d", len(got.List))
	}
}
