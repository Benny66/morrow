import { ref, type Ref } from 'vue'
import { invoke, type AppError } from '../lib/invoke'
import { newPageRequest, PageSize, type PageRequest, type PageResult } from '../lib/page'

/** usePagedList 的初始值配置。 */
export interface UsePagedListOptions {
  /** 初始页码，默认 1。 */
  page?: number
  /** 初始页大小，默认 PageSize.Default。 */
  pageSize?: number
  /**
   * 绑定调用标签，用于慢调用告警里定位（如 'ListAssets'）。
   *
   * 由调用方提供而非本模块猜测：本模块内部是 `invoke(() => fetcher(req))`，
   * 猜出来只会是 `fetcher` 这种误导性名字。
   */
  label?: string
}

/** usePagedList 的返回值。 */
export interface UsePagedListReturn<T> {
  /** 当前页数据。 */
  list: Ref<T[]>
  /** 满足条件的总条数（用于渲染分页控件）。 */
  total: Ref<number>
  /** 当前页码（加载成功后以 Go 回显值为准）。 */
  page: Ref<number>
  /** 当前页大小（加载成功后以 Go 回显值为准）。 */
  pageSize: Ref<number>
  /** 是否加载中。 */
  loading: Ref<boolean>
  /** 最近一次加载的错误（已归一化为 AppError），成功时为 null。 */
  error: Ref<AppError | null>
  /** 加载一页。传入 next 可同时改页码/页大小（如搜索后回第 1 页）。 */
  load: (next?: Partial<PageRequest>) => Promise<void>
}

/**
 * 列表页的加载与状态收敛：把「页码/页大小/总数/加载中/错误」这套逐页复制的样板
 * 收进一处。只管状态与加载，【不管渲染】——表格/分页控件仍由下游自行实现。
 *
 * 用法：
 *   const { list, total, page, pageSize, loading, error, load } = usePagedList(
 *     (req) => ListExamples(req),
 *     { label: 'ListExamples' },   // 可选：慢调用告警里用它定位
 *   )
 *   onMounted(load)
 *
 * fetcher 采用【注入】而非内联绑定调用：组合式函数因此保持零业务耦合，
 * 换实现（加缓存、加轮询）只改调用方一行，也便于后续注入 fake 做测试。
 *
 * 契约细节：加载成功后 page/pageSize 以 Go 回显的归一化值为准——请求值可能被
 * 夹取（页大小越界、页码下界），手写页面最容易漏掉这一条。
 */
export function usePagedList<T>(
  fetcher: (req: PageRequest) => Promise<PageResult<T>>,
  options: UsePagedListOptions = {},
): UsePagedListReturn<T> {
  const page = ref(options.page ?? 1)
  const pageSize = ref(options.pageSize ?? PageSize.Default)
  // 泛型下 ref([]) 推导为 never[]，此处按契约显式收窄。
  const list = ref([]) as Ref<T[]>
  const total = ref(0)
  const loading = ref(false)
  const error = ref<AppError | null>(null)

  async function load(next: Partial<PageRequest> = {}): Promise<void> {
    if (next.page !== undefined) page.value = next.page
    if (next.page_size !== undefined) pageSize.value = next.page_size

    loading.value = true
    error.value = null
    try {
      const res = await invoke(
        () => fetcher(newPageRequest(page.value, pageSize.value)),
        options.label ?? '分页列表',
      )
      list.value = res.list ?? []
      total.value = res.total
      // 以 Go 回显的归一化值为准（契约，见文件头）
      page.value = res.page
      pageSize.value = res.page_size
    } catch (e) {
      error.value = e as AppError
      // 失败时一并清空数据：留着上一页的旧行会让「加载失败」看起来像「加载成功」。
      list.value = []
      total.value = 0
    } finally {
      loading.value = false
    }
  }

  return { list, total, page, pageSize, loading, error, load }
}
