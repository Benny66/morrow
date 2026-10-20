import { defineStore } from 'pinia'
import { ref } from 'vue'
// v3 的绑定落在 frontend/bindings/<go module>/**，且文件带 .js 后缀（见 stores/app.ts 的说明）。
import {
  CompleteReminder,
  GetTodayStats,
  ListPendingFires,
  SkipReminder,
  SnoozeReminder,
} from '../../bindings/morrow/app.js'
import { invoke, type AppError } from '../lib/invoke'
import { CheckInAction } from '../lib/reminder'
import { logInfo } from '../lib/log'

/**
 * 今日统计的类型。用绑定的返回类型反推，而不是手写一份镜像：
 * Go 侧字段一改，这里立刻跟着变（手写镜像只会在运行期撞见 undefined）。
 */
export type TodayStats = Awaited<ReturnType<typeof GetTodayStats>>
/** 单条提醒的今日进度。 */
export type ReminderProgress = NonNullable<NonNullable<TodayStats>['reminders']>[number]
/** 待处理提醒（首页卡片）。
 *
 * 用 NonNullable 包一层：Go 的切片在生成的类型里可能是 `T[] | null`（见 lib/page.ts
 * 对同一现象的解释），这里如实兼容两种形状，不替它做更强的断言。
 */
export type PendingFire = NonNullable<Awaited<ReturnType<typeof ListPendingFires>>>[number]

/**
 * 仪表盘的数据仓库。
 *
 * 【为什么用 Pinia 而不是页面内 ref】仪表盘的数据有两个来源：页面的主动加载，
 * 以及后端推来的 reminder:fired / reminder:changed 事件。放在 store 里，
 * 事件回调与页面组件共享同一份状态，"两处各自刷各自的"那种不一致就不会出现。
 */
export const useReminderStore = defineStore('reminder', () => {
  // stats 今日聚合数据；null 表示还没加载过。
  const stats = ref<TodayStats | null>(null)
  // pending 待处理提醒；卡片区的数据源。
  const pending = ref<PendingFire[]>([])
  // loading 是否正在加载（用于骨架屏）。
  const loading = ref(false)
  // acting 正在处理的 fireID（0 表示空闲），用于给对应卡片上 loading。
  const acting = ref(0)
  // error 最近一次操作的错误（归一化后的 AppError）。
  const error = ref<AppError | null>(null)

  /** 重新拉取全部仪表盘数据（统计与待处理并行请求）。 */
  async function loadAll(): Promise<void> {
    loading.value = true
    error.value = null
    try {
      const [nextStats, nextPending] = await Promise.all([
        invoke(() => GetTodayStats(), 'GetTodayStats'),
        invoke(() => ListPendingFires(), 'ListPendingFires'),
      ])
      stats.value = nextStats
      pending.value = nextPending ?? []
    } catch (e) {
      error.value = e as AppError
    } finally {
      loading.value = false
    }
  }

  /**
   * 处理一条提醒。
   *
   * 顺序是「先本地移除 → 再整体刷新」：动作成功的反馈不该等一次往返，
   * 否则点「完成」后卡片要闪 200ms 才消失，用户会以为没点上而再点一次。
   */
  async function act(fireID: number, action: string): Promise<void> {
    acting.value = fireID
    error.value = null
    try {
      if (action === CheckInAction.Done) {
        await invoke(() => CompleteReminder(fireID), 'CompleteReminder')
      } else if (action === CheckInAction.Snooze) {
        await invoke(() => SnoozeReminder(fireID), 'SnoozeReminder')
      } else {
        await invoke(() => SkipReminder(fireID), 'SkipReminder')
      }
      pending.value = pending.value.filter((item) => item.fire_id !== fireID)
      await loadAll()
      logInfo('提醒已处理', { fire_id: fireID, action })
    } catch (e) {
      error.value = e as AppError
    } finally {
      acting.value = 0
    }
  }

  /** 清空错误提示（用户看过之后手动关掉）。 */
  function clearError(): void {
    error.value = null
  }

  return { stats, pending, loading, acting, error, loadAll, act, clearError }
})
