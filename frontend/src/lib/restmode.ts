// 休息模式领域的前端镜像：状态常量 + 格式化助手。
//
// 与 Go 侧成对：
//   - 状态常量的单一真相在 internal/restmode.State，此处是镜像，由
//     guard/parity_test.go 双向对账；
//   - Snapshot 的类型直接用生成绑定里的 models.ts（不手写第二份镜像，
//     否则每次字段变更都要同步两处）。
//
// 【只镜像常量与类型，不镜像逻辑】倒计时到 0 自动完成的实现只在 Go 的
// App.tickRestMode 里；前端只查询状态并渲染，不做二次判定。见 AGENTS.md 铁律 1。

/**
 * 休息模式状态枚举（镜像 Go 的 restmode.State）。
 *
 * - idle：未在休息；Alert.vue 收到 idle 应收起覆盖层。
 * - warning：到点前 60 秒的预警；Pet.vue 显示「1 分钟后...」气泡，菜单栏同步。
 * - countdown：倒计时进行中；显示引导动作与剩余秒数。
 * - done：倒计时自然到点（服务端 tick 会立刻落 CheckIn 并 Reset）；
 *   这一状态在快照里存在时间极短（≤1 秒），前端一般看不到。
 * - skipped：用户「提前结束 → 不算」；同样会被立刻 Reset。
 * - early_completed：用户「提前结束 → 算完成」；同上。
 */
export const RestModeState = {
  Idle: 'idle',
  Warning: 'warning',
  Countdown: 'countdown',
  Done: 'done',
  Skipped: 'skipped',
  EarlyCompleted: 'early_completed',
} as const

export type RestModeStateValue = (typeof RestModeState)[keyof typeof RestModeState]

/** 把秒数渲染成 mm:ss。超过 60 分不会截断——最长时长是 180s（move 类），够用。 */
export function formatRemaining(sec: number): string {
  const clamped = Math.max(0, Math.floor(sec))
  const m = Math.floor(clamped / 60)
  const s = clamped % 60
  return `${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
}
