// Package page 定义管道契约中的「分页协议」。
//
// 契约：列表类绑定方法 MUST 返回 PageResult[T]，请求参数 MUST 经 PageRequest.Normalized()
// 归一化后再查库（页码下界、页大小上下界）。
//
// 页大小上下界是【单一真相】：常量定义在本包，service 与护栏共同引用，
// 不得在别处另写一份数字。
package page

// 页大小上下界（单一真相）。
const (
	// DefaultPageSize 请求未指定页大小（0）时的默认值。
	DefaultPageSize = 20
	// MaxPageSize 页大小上限，超出则夹取，防止一次拉全表。
	MaxPageSize = 200
)

// Request 是列表查询的分页请求。
type Request struct {
	// Page 页码，1-based。小于 1 归一化为 1。
	Page int `json:"page"`
	// PageSize 每页条数。0 归一化为 DefaultPageSize，超出上限夹取。
	PageSize int `json:"page_size"`
}

// Normalized 返回归一化后的分页请求：页码下界 1、页大小夹取到 [1, MaxPageSize]。
// service 层在查库前 MUST 调用本方法，避免负数 offset 或一次拉全表。
func (r Request) Normalized() Request {
	out := r
	if out.Page < 1 {
		out.Page = 1
	}
	if out.PageSize <= 0 {
		out.PageSize = DefaultPageSize
	}
	if out.PageSize > MaxPageSize {
		out.PageSize = MaxPageSize
	}
	return out
}

// Offset 返回 SQL offset（归一化后计算）。
func (r Request) Offset() int {
	n := r.Normalized()
	return (n.Page - 1) * n.PageSize
}

// Result 是列表查询的分页结果（泛型）。
//
// 注意（Wails 绑定）：泛型实例在生成的 TS 里会被压平成含模块路径的类名
// （如 PageResult_<pkg>_Item_）。类型安全，名字难看，仅在类型标注处可见。
type Result[T any] struct {
	List     []T   `json:"list"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
}

// NewResult 构造分页结果，并回填归一化后的 page/page_size，保证前端拿到一致的回显值。
func NewResult[T any](req Request, list []T, total int64) Result[T] {
	n := req.Normalized()
	if list == nil {
		list = []T{} // 保证 JSON 序列化为 []，而非 null，前端无需额外判空
	}
	return Result[T]{
		List:     list,
		Total:    total,
		Page:     n.Page,
		PageSize: n.PageSize,
	}
}
