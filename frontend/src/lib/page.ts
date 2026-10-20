// 管道契约：Go↔TS 的分页协议前端侧。
//
// 与 Go 侧 internal/page 成对：
//   - 数据结构（Request / Result[T]）在此以 TS 类型镜像，字段名与 JSON tag 一致；
//   - 页大小上下界是【常量镜像】，单一真相在 Go，由 internal/guard/parity_test.go
//     断言二者一致（漂移即失败）。
//
// 【只镜像常量，不镜像逻辑】：归一化（页码下界、页大小夹取）的唯一实现在 Go 的
// Request.Normalized()，本模块【不】照抄一份——那是把「一个规则两个实现」，
// 比复制常量更糟。前端发原始值，Go 归一化后经 Result 回填，前端以回显值为准。

/** 页大小上下界。单一真相在 Go 的 internal/page（DefaultPageSize / MaxPageSize），此处为镜像。 */
export const PageSize = {
  /** 请求未指定页大小（0）时 Go 采用的默认值。 */
  Default: 20,
  /** 页大小上限，超出由 Go 夹取。 */
  Max: 200,
} as const

/** 分页请求（对应 Go 的 page.Request）。字段名与 JSON tag 一致。 */
export interface PageRequest {
  /** 页码，1-based。小于 1 由 Go 归一化为 1。 */
  page: number
  /** 每页条数。0 由 Go 归一化为 PageSize.Default，超出上限被夹取。 */
  page_size: number
}

/** 分页结果（对应 Go 的 page.Result[T]）。 */
export interface PageResult<T> {
  /**
   * 当前页数据。
   *
   * 【为什么是可空的】Go 的 `[]T` 本身可 nil，绑定生成器无从知道运行期一定非 nil，
   * 故生成的 TS 类型是 `T[] | null`（见 frontend/bindings/<模块>/internal/page/models.ts
   * 的 `Result<T>`）。本镜像如实照抄该类型，不替它作出更强的断言——
   * 写死成 `T[]` 就是在类型层面撒谎，而这条谎言不会报错，只会在 Go 侧真的返回 nil 时
   * 让下游拿到 null 却以为拿到了数组。
   *
   * 运行期确实非 nil（page.NewResult 保证无数据时是 []），但那是 Go 的不变量，
   * 不该由前端的类型来断言。null 的归一化只做一次，在 usePagedList 的 `res.list ?? []`；
   * 它的返回类型 UsePagedListReturn.list 因此仍是干净的 `T[]`。
   */
  list: T[] | null
  /** 满足条件的总条数。 */
  total: number
  /** Go 回填的归一化页码，前端应以它为准。 */
  page: number
  /** Go 回填的归一化页大小，前端应以它为准。 */
  page_size: number
}

/**
 * 构造分页请求（不归一化——归一化是 Go 的职责，见文件头）。
 * 省略参数时用 PageSize.Default 作为页大小。
 *
 * 参数显式标注 `number`：PageSize 用了 `as const`，默认值会被推断成字面量类型
 * （20 / 200），不标注则调用方传普通 number 时会类型不匹配。
 */
export function newPageRequest(page: number = 1, pageSize: number = PageSize.Default): PageRequest {
  return { page, page_size: pageSize }
}
