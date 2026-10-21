// 提醒领域的前端镜像：事件名 / 载荷 / 类型枚举 / 展示元数据。
//
// 与 Go 侧成对：
//   - 事件名与动作常量的单一真相在 internal/event + internal/reminder，此处是镜像，
//     由 internal/guard/parity_test.go 校验（漂移即失败）；
//   - 载荷类型是 Go 结构体的镜像，字段名与 JSON tag 一致（Go 的 json tag 就是契约定；
//     生成器导出的字段名也用 json tag，两边不会打架）。
//
// 【只镜像常量与类型，不镜像逻辑】类型归一化（非法 kind → custom）的唯一实现在
// Go 的 model.NormalizeKind；前端不再抄一份。见 AGENTS.md 铁律 1。

import { eventName, EventAction } from './event'

/** 事件 domain，镜像 Go 的 reminder.Domain。 */
export const REMINDER_DOMAIN = 'reminder'

/**
 * 提醒领域的事件名。
 * 订阅时只能用这两个常量：手写字符串拼错了不会有任何提示（订阅失败是静默的）。
 */
export const ReminderEvent = {
  /** 提醒已送达：首页据此立刻刷出待处理卡片。 */
  Fired: eventName(REMINDER_DOMAIN, EventAction.Fired),
  /** 数据变更（打卡 / 推迟 / 计划或场景改动）：各页面据此重新拉取。 */
  Changed: eventName(REMINDER_DOMAIN, EventAction.Changed),
} as const

/** reminder:fired 的载荷（镜像 Go 的 reminder.FiredPayload）。 */
export interface ReminderFiredPayload {
  /** 触发记录 ID，打卡时回传它。 */
  fire_id: number
  /** 提醒项 ID。 */
  reminder_id: number
  /** 场景 ID。 */
  scenario_id: number
  /** 提醒标题。 */
  title: string
  /** 提醒正文。 */
  content: string
  /** 提醒类型（见 ReminderKind）。 */
  kind: string
  /** 是否为一次性提醒（无场景、只响这一回）。 */
  is_one_off: boolean
  /** 计划触发时间（Unix 秒）。 */
  fire_at: number
  /** 是否为顺延到上限后的安静投递（不弹全屏强提醒，也不进休息模式）。 */
  quiet_delivery: boolean
}

/** reminder:changed 的载荷（镜像 Go 的 reminder.ChangedPayload）。 */
export interface ReminderChangedPayload {
  /** 变更原因，仅用于日志定位。 */
  reason: string
}

/** 提醒类型枚举（镜像 Go 的 model.Kind*）。 */
export const ReminderKind = {
  /** 喝水。 */
  Water: 'water',
  /** 站立。 */
  Stand: 'stand',
  /** 走动。 */
  Move: 'move',
  /** 如厕。 */
  Toilet: 'toilet',
  /** 护眼 / 放松。 */
  Rest: 'rest',
  /** 其他（兜底）。 */
  Custom: 'custom',
} as const

/** 提醒类型取值。 */
export type ReminderKindValue = (typeof ReminderKind)[keyof typeof ReminderKind]

/** 类型的展示元数据。 */
export interface KindMeta {
  /** 中文名（下拉选项与卡片标签）。 */
  label: string
  /** 单字图标（沿用项目侧栏的字符图标风格，不引入图标库）。 */
  icon: string
  /** 进度的量词：喝水按「杯」，其余按「次」。 */
  unit: string
}

/**
 * 类型 → 展示元数据。
 *
 * 【为什么只在前端有这张表】它只影响呈现（图标/量词/中文名），不影响任何判定：
 * 后端只认 kind 字符串。把它放在 Go 侧再传下来，反而会让"改个量词"变成一次后端改动。
 */
export const KIND_META: Record<string, KindMeta> = {
  [ReminderKind.Water]: { label: '喝水', icon: '水', unit: '杯' },
  [ReminderKind.Stand]: { label: '站立', icon: '立', unit: '次' },
  [ReminderKind.Move]: { label: '走动', icon: '走', unit: '次' },
  [ReminderKind.Toilet]: { label: '如厕', icon: '厕', unit: '次' },
  [ReminderKind.Rest]: { label: '放松', icon: '憩', unit: '次' },
  [ReminderKind.Custom]: { label: '提醒', icon: '·', unit: '次' },
}

/** 未知 kind 的兜底元数据（AI 加了新类型而前端还没跟上时不至于空白）。 */
const FALLBACK_META: KindMeta = { label: '提醒', icon: '·', unit: '次' }

/** 取某 kind 的展示元数据，未知类型走兜底。 */
export function kindMeta(kind: string): KindMeta {
  return KIND_META[kind] ?? FALLBACK_META
}

/**
 * 类型下拉选项（从 KIND_META 派生，不另写一份清单）。
 * 顺序即 KIND_META 的键顺序，与 Go 侧 model.AllKinds() 的语义顺序一致。
 */
export const KIND_OPTIONS = Object.entries(KIND_META).map(([value, meta]) => ({
  value,
  label: meta.label,
}))

/** 打卡动作（镜像 Go 的 model.Action*），仅用于前端日志与按钮分流。 */
export const CheckInAction = {
  /** 完成。 */
  Done: 'done',
  /** 推迟。 */
  Snooze: 'snooze',
  /** 跳过。 */
  Skip: 'skip',
} as const
