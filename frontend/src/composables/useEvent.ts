import { onUnmounted } from 'vue'
import { onEvent } from '../lib/event'

/**
 * 在组件里订阅后端事件，组件卸载时【自动】解除订阅。
 *
 * 为何需要它：onEvent 只是把取消函数递到手上，漏不漏靠人的纪律；而漏解绑的后果
 * （路由切回后同一事件触发多次）在开发态极难察觉——开发时通常只切一次页面。
 * 挂到组件生命周期上，才让「正确用法」成为「最省事的用法」。
 *
 * 用法：
 *   useEvent<EventProgress>(eventName('asset', EventAction.Progress), (p) => {
 *     percent.value = p.total > 0 ? Math.round((p.done / p.total) * 100) : 0
 *   })
 *
 * 必须在组件 setup 中调用（onUnmounted 的注册要求有活跃组件实例）。
 *
 * @returns 取消订阅函数（通常在组件外提前取消时才需要用到）
 */
export function useEvent<T = unknown>(name: string, handler: (payload: T) => void): () => void {
  const off = onEvent<T>(name, handler)
  onUnmounted(off)
  return off
}
