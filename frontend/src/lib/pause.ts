// 暂停领域的前端镜像：PauseReason 枚举。
//
// 与 Go 侧成对：常量的单一真相在 internal/pause.PauseReason，此处为镜像，
// 由 internal/guard/parity_test.go 双向对账（漂移即失败）。
//
// 【为什么单独一个 pause.ts 而不是塞进 lib/reminder.ts】reminder 域管的是
// 「一条提醒怎么被处理」；pause 域管的是「为什么这条提醒暂时没投递」。
// 两者共享一份 kind 枚举（都取 model.Kind*）但事件与原因语义独立，
// 混在一个文件里会让下一次加暂停信号时改到 reminder 常量。

/**
 * 智能免打扰的暂停原因枚举。
 *
 * - idle：系统连续 N 分钟无键鼠输入
 * - lock：屏幕锁定或系统休眠
 * - meeting：前台为 Zoom / Teams / 飞书 / 钉钉 等会议应用
 * - fullscreen：前台窗口铺满当前屏幕
 * - focus：系统专注模式开启（macOS 12+）
 * - calendar：日历显示当前忙碌（macOS EventKit）
 *
 * 【顺序不是随便的】Go 侧 CompositeProvider 里 `reasonPriority` map 决定
 * 同时触发时展示哪个（lock > focus > calendar > meeting > fullscreen > idle）。
 * 这里的字符串取值是【契约】——Go 端改了名字（例如把 'meeting' 改成 'in_meeting'）
 * 会破坏 parity guard；优先级顺序调整不影响 TS 常量。
 */
export const PauseReason = {
  /** 未暂停；与 Go 的 ReasonNone 一致（"none" 而不是空串——见 provider.go 注释）。 */
  None: 'none',
  Idle: 'idle',
  Lock: 'lock',
  Meeting: 'meeting',
  Fullscreen: 'fullscreen',
  Focus: 'focus',
  Calendar: 'calendar',
} as const

export type PauseReasonValue = (typeof PauseReason)[keyof typeof PauseReason]
