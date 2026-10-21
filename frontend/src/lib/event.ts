// 管道契约：Go↔TS 的事件协议前端侧。
//
// 与 Go 侧 internal/event 成对：
//   - Go 用 event.Name(domain, action) 拼事件名，本模块的 eventName() 是它的镜像；
//   - 动作常量（progress/done/error）的单一真相在 Go，此处为镜像，
//     由 internal/guard/parity_test.go 断言二者一致（漂移即失败）。
//
// 为何必须有本模块：没有它，下游只能手写 Events.On('asset:progress', ...)——
// 那是第二处真相，拼错了不会有任何提示（订阅失败是静默的）。
//
// 【v3 的载荷形状变了，这是迁移时最容易漏的一处】：
//   - v2：`EventsOn(name, (...data) => ...)` —— 回调收到的是**摊平的参数**，
//     故老代码要判断「一个参数还是多个」再来决定取 data[0] 还是整个数组。
//   - v3：`Events.On(name, (ev) => ...)` —— 回调收到的是一个 **WailsEvent 对象**
//     `{name, data, sender}`，真正的载荷在 `ev.data` 上。
//   Go 侧 emit 的形状（单参数 vs 多参数）不再影响前端取值方式，那段判断随之删除。
//
// 另一个变化：v3 的 `Events.On` 是**纯本地注册**——它不抛错，也不向 Go 发任何请求，
// 只是往一张表里塞一个回调（Go 侧通过 window._wails.dispatchWailsEvent 推过来）。
// 故 v2 那个 `try/catch + console.warn('事件订阅失败')` 在 v3 下【永远不会触发】：
// 非 wails 环境下订阅是**静默失效**的，本模块没有可靠的本地判据去发现它。
// 保留一个假的兜底只会让人以为它在工作，故删掉，把这件事写在注释里。
//
// 可抄的最小范例（长任务推进度）：
//
//	Go 侧（service 层跑长任务，经 App 绑定方法拿到 app 实例）：
//	  app.Event.Emit(event.Name("asset", event.ActionProgress),
//	      event.Progress{Done: done, Total: total})
//	  app.Event.Emit(event.Name("asset", event.ActionDone),
//	      event.Result{Ok: true})
//
//	前端侧（组件里，卸载自动解绑）：
//	  const percent = ref(0)
//	  useEvent<EventProgress>(eventName('asset', EventAction.Progress), (p) => {
//	    percent.value = p.total > 0 ? Math.round((p.done / p.total) * 100) : 0
//	  })

import { Events } from '@wailsio/runtime'

/** 事件动作常量。单一真相在 Go 的 internal/event（Action*），此处为镜像。 */
export const EventAction = {
  /** 进度：长任务推进中。 */
  Progress: 'progress',
  /** 成功结束。 */
  Done: 'done',
  /** 失败结束。 */
  Error: 'error',
  /** 提醒已送达（domain 为 reminder）。 */
  Fired: 'fired',
  /** 数据变更，前端应重新拉取。 */
  Changed: 'changed',
  /** 桌面宠物贴边收起 / 展开（domain 为 pet）。 */
  Dock: 'dock',
  /** 菜单栏「查看待处理」入口触发（domain 为 app，US-16）。 */
  ShowPending: 'show_pending',
} as const

/** 事件动作取值。 */
export type EventActionValue = (typeof EventAction)[keyof typeof EventAction]

/**
 * 拼装符合约定的事件名（`<domain>:<action>`）。
 * domain 与业务模块名一致（如 'asset'），镜像 Go 的 event.Name。
 *
 * action 写成 `EventActionValue | string` 是有意的：事件协议是**约定式**的，
 * 下游可以有自定义动作。联合类型让编辑器给出三个内置动作的补全提示，
 * 同时不禁止其他取值（并非冗余——删掉 `| string` 会把约定变成硬约束）。
 */
export function eventName(domain: string, action: EventActionValue | string): string {
  return `${domain}:${action}`
}

/** 进度载荷（对应 Go 的 event.Progress）。Total 为 0 表示总量未知，应按「不确定进度」处理。 */
export interface EventProgress {
  done: number
  total: number
}

/** 结束载荷（对应 Go 的 event.Result）。失败时 ok 为 false，message 为中文提示。 */
export interface EventResult {
  ok: boolean
  message?: string
}

/**
 * 订阅一个后端事件，返回取消订阅函数。
 *
 * 【务必解除订阅】：订阅是累积的，漏解绑会让同一事件在路由来回切换后
 * 触发多次（重复处理 + 内存泄漏）。组件里请改用 composables/useEvent.ts 自动解绑。
 *
 * 载荷约定：取 `ev.data`（v3 的回调参数是 WailsEvent 对象，不是摊平的参数，
 * 见文件头说明）。非 wails 环境下本函数**静默失效**——订阅能注册成功，但永远
 * 不会有事件到达，而这里没有可靠的本地判据可以发现它。
 */
export function onEvent<T = unknown>(name: string, handler: (payload: T) => void): () => void {
  return Events.On(name, (ev) => {
    handler(ev.data as T)
  })
}
