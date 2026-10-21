import { kindMeta } from './reminder'

/** 模板提醒的日程文案（间隔 0 = 每天单点）。 */
export function reminderScheduleText(item: {
  kind: string
  interval_min: number
  window_start: string
  window_end: string
  daily_target: number
}): string {
  const unit = kindMeta(item.kind).unit
  const target = item.daily_target > 0 ? ` · 目标 ${item.daily_target} ${unit}` : ''
  if (item.interval_min === 0) return `每天 ${item.window_start}${target}`
  return `每 ${item.interval_min} 分钟 · ${item.window_start}–${item.window_end}${target}`
}

/** 模板覆盖的类型标签（去重，保持模板顺序）。 */
export function templateKindLabels(tpl: { reminders: { kind: string }[] | null }): string {
  const seen = new Set<string>()
  const labels: string[] = []
  for (const item of tpl.reminders ?? []) {
    if (seen.has(item.kind)) continue
    seen.add(item.kind)
    labels.push(kindMeta(item.kind).label)
  }
  return labels.join(' · ')
}

/** 把下一次计划触发时间格式化为「今天 / 明天 HH:MM」；0 表示没有下一次。 */
export function formatNextFireAt(unixSeconds: number): string {
  if (!unixSeconds) return '—'
  const at = new Date(unixSeconds * 1000)
  const now = new Date()
  const tomorrow = new Date(now)
  tomorrow.setDate(now.getDate() + 1)
  const time = at.toLocaleTimeString('zh-CN', {
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  })
  if (at.toDateString() === now.toDateString()) return `今天 ${time}`
  if (at.toDateString() === tomorrow.toDateString()) return `明天 ${time}`
  return at.toLocaleString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  })
}
