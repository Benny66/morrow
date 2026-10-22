// 久坐感知的前端契约镜像：只复制枚举、类型与展示标签，不复制业务逻辑。
//
// 单一真相在 internal/sit/contract.go；internal/guard/parity_test.go 双向校验。
// 镜像里出现 Go 侧没有的常量、或取值不一致，make test 会变红。

/** 久坐运行状态。 */
export const SitState = {
  Idle: 'idle',
  Tracking: 'tracking',
  BreakWait: 'break_wait',
  Muted: 'muted',
  Unsupported: 'unsupported',
} as const

/** 宠物情绪层。 */
export const SitMood = {
  Energetic: 'energetic',
  Concerned: 'concerned',
  Droopy: 'droopy',
} as const

/** 语气档位；默认毒舌。 */
export const SitTone = {
  Sassy: 'sassy',
  Lively: 'lively',
  Gentle: 'gentle',
} as const

/** 行为类别。 */
export const SitCategory = {
  Coding: 'coding',
  Design: 'design',
  Browsing: 'browsing',
  Communication: 'communication',
  Media: 'media',
  Other: 'other',
} as const

/** 宠物气泡类型。 */
export const BubbleKind = {
  Nudge: 'nudge',
  Memory: 'memory',
  Greeting: 'greeting',
} as const

/** 气泡动作 ID；前端只认这些 ID。 */
export const SitAction = {
  StartBreak: 'start_break',
  Snooze10: 'snooze_10',
  More: 'more',
  Dismiss: 'dismiss',
  OpenApp: 'open_app',
} as const

/** 应用/久坐会话结束原因。 */
export const SitEndReason = {
  AppSwitch: 'app_switch',
  Idle: 'idle',
  Lock: 'lock',
  ActiveBreak: 'active_break',
  LockBreak: 'lock_break',
  IdleBreak: 'idle_break',
  FeatureOff: 'feature_off',
  Abandoned: 'abandoned',
  Unknown: 'unknown',
} as const

/** 休息类型。 */
export const SitBreakKind = {
  Active: 'active',
  Lock: 'lock',
  Idle: 'idle',
} as const

/** 应用画像读取状态。 */
export const ProfileStatus = {
  OK: 'ok',
  Unsupported: 'unsupported',
  ReadError: 'read_error',
  NoData: 'no_data',
} as const

export type SitStateValue = (typeof SitState)[keyof typeof SitState]
export type SitMoodValue = (typeof SitMood)[keyof typeof SitMood]
export type SitToneValue = (typeof SitTone)[keyof typeof SitTone]
export type SitCategoryValue = (typeof SitCategory)[keyof typeof SitCategory]
export type BubbleKindValue = (typeof BubbleKind)[keyof typeof BubbleKind]
export type SitActionValue = (typeof SitAction)[keyof typeof SitAction]
export type ProfileStatusValue = (typeof ProfileStatus)[keyof typeof ProfileStatus]

/** 语气的中文标签（设置页/宠物文案元数据）。 */
export const SIT_TONE_LABEL: Record<SitToneValue, string> = {
  [SitTone.Sassy]: '毒舌',
  [SitTone.Lively]: '活泼',
  [SitTone.Gentle]: '温和',
}

/** 行为类别的中文标签；宠物气泡只说这里的标签，不出现应用名。 */
export const SIT_CATEGORY_LABEL: Record<SitCategoryValue, string> = {
  [SitCategory.Coding]: '写代码',
  [SitCategory.Design]: '做设计',
  [SitCategory.Browsing]: '浏览',
  [SitCategory.Communication]: '沟通',
  [SitCategory.Media]: '影音',
  [SitCategory.Other]: '其他',
}

/** 动作的展示文案；按钮行为由 Pet.vue 按 ID 分发。 */
export const SIT_ACTION_LABEL: Record<SitActionValue, string> = {
  [SitAction.StartBreak]: '起来走走',
  [SitAction.Snooze10]: '再坐 10 分钟',
  [SitAction.More]: '更多',
  [SitAction.Dismiss]: '知道了',
  [SitAction.OpenApp]: '打开客户端',
}

/** 状态/情绪/画像状态的展示文案（久坐页与设置页用）。 */
export const SIT_STATE_LABEL: Record<SitStateValue, string> = {
  [SitState.Idle]: '未开始',
  [SitState.Tracking]: '记录中',
  [SitState.BreakWait]: '休息中',
  [SitState.Muted]: '今天不提醒',
  [SitState.Unsupported]: '当前平台不支持',
}

export const SIT_MOOD_LABEL: Record<SitMoodValue, string> = {
  [SitMood.Energetic]: '精神',
  [SitMood.Concerned]: '有点担心',
  [SitMood.Droopy]: '芽耷拉了',
}

export const SIT_PROFILE_STATUS_LABEL: Record<ProfileStatusValue, string> = {
  [ProfileStatus.OK]: '正常',
  [ProfileStatus.Unsupported]: '当前平台不支持',
  [ProfileStatus.ReadError]: '读取前台应用失败',
  [ProfileStatus.NoData]: '当前未记录',
}

/** 状态 ID → 中文标签；绑定返回的枚举在运行期是字符串。 */
export function sitStateLabel(state: string): string {
  return (SIT_STATE_LABEL as Record<string, string>)[state] ?? state
}

/** 行为类别 ID → 中文标签。 */
export function sitCategoryLabel(category: string): string {
  return (SIT_CATEGORY_LABEL as Record<string, string>)[category] ?? category
}

/** 画像状态 ID → 中文标签。 */
export function sitProfileStatusLabel(status: string): string {
  return (SIT_PROFILE_STATUS_LABEL as Record<string, string>)[status] ?? status
}

/** 情绪 → 宠物根节点 CSS class；空串表示不改变外观。 */
export function sitMoodClass(mood?: string | null): string {
  if (mood === SitMood.Concerned) return 'pet--sit-concerned'
  if (mood === SitMood.Droopy) return 'pet--sit-droopy'
  return ''
}

/** 动作 ID → 中文按钮文案；未知值回退为原 ID，避免按钮空白。 */
export function sitActionLabel(action: string): string {
  return (SIT_ACTION_LABEL as Record<string, string>)[action] ?? action
}

/** 与 Go config.SitSettings 对齐的可绑定视图。 */
export interface SitSettings {
  enabled: boolean
  profile_enabled: boolean
  profile_asked: boolean
  threshold_minutes: number
  tone: SitToneValue
  platform_supported: boolean
  profile_status: ProfileStatusValue | ''
  muted_today: boolean
}

/** 当前专注会话：只含类别与分钟，不含应用名。 */
export interface SitFocus {
  category: SitCategoryValue
  minutes: number
}

/** 宠物待展示气泡。 */
export interface SitBubble {
  id: string
  kind: BubbleKindValue
  title: string
  text: string
  actions: SitActionValue[]
  expires_at: number
}

/** 与 Go sit.Snapshot 对齐；明确不含应用名/应用标识。 */
export interface SitSnapshot {
  state: SitStateValue
  mood: SitMoodValue
  continuous_minutes: number
  today_sitting_minutes: number
  longest_session_minutes: number
  profile_enabled: boolean
  profile_supported: boolean
  platform_supported: boolean
  muted_today: boolean
  budget_remaining: number
  focus?: SitFocus
  bubble?: SitBubble
}

/** 气泡动作/设置动作的可见回执。 */
export interface SitActionResult {
  ok: boolean
  message: string
  credited: boolean
  credited_title: string
}
