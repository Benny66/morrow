// 宠物渲染的单一真相：图集布局、动画行序、帧率、气泡适配的等待窗口。
//
// 为什么集中在这里：Pet.vue（渲染）与设置页（文案提示）都要用同一组数值，
// 而图集布局是【外部约定】——它由 codexpet 生态的素材决定，改了就要连素材一起换。
// 写两遍的结果是某天只改了一处，宠物要么只画半只、要么错行。
//
// 图集规格（v1，codexpet 的 pet.json 里 spriteVersionNumber = 1）：
//   单帧 192×208，8 列 × 9 行 = 整图 1536×1872，每行一套动作、每列一帧循环。

import { eventName, EventAction } from './event'

/** 精灵图单帧宽度（像素）。 */
export const FRAME_W = 192
/** 精灵图单帧高度（像素）。 */
export const FRAME_H = 208

/** 图集列数（每行的帧数）。 */
export const PET_COLS = 8

/** 图集行数（动作数，见 PET_ANIM_ROW）。 */
export const PET_ROWS = 9

/** 整图宽度（= FRAME_W * PET_COLS）。 */
export const PET_SHEET_W = FRAME_W * PET_COLS

/** 整图高度（= FRAME_H * PET_ROWS）。 */
export const PET_SHEET_H = FRAME_H * PET_ROWS

/** 正常播放帧率（12fps 是这批素材设计的节奏）。 */
export const PET_FPS = 12

/**
 * 可见但失焦时的帧率。
 *
 * 【为什么是 8 而不是更低】宠物窗口是 Frameless + AlwaysOnTop 的小窗，启动后
 * 通常拿不到键盘焦点——document.hasFocus() 一开始就是 false，即"失焦"是它的
 * 【常态】而不是例外。若按"失焦 = 可以慢"给到 4fps，宠物从第一眼起就是一顿一顿的，
 * 用户会以为功能卡住了。
 *
 * 真正省电的那一半浏览器已经给了：窗口不可见时 rAF 自动暂停。降帧只需要兜住
 * "可见但失焦"这一种情况，8fps（满帧 12 的 2/3）既省电又保持动作连续。
 * 不要把它调回 4——那不是省电，是让宠物看起来坏了。
 */
export const PET_IDLE_FPS = 8

/**
 * 动画枚举。
 *
 * 取值即语义名（不用数字），因为它在日志、状态机与模板里都要可读；
 * 行号映射见 PET_ANIM_ROW（单一真相）。
 */
export const PetAnim = {
  /** 默认循环：待机呼吸。 */
  Idle: 'idle',
  /** 挥手：点击宠物时的即时反馈。 */
  Waving: 'waving',
  /** 等待你处理：有待处理提醒时（reminder:fired 触发）。 */
  Waiting: 'waiting',
  /** 原地跑。 */
  Running: 'running',
  /** 向右走。 */
  RunningRight: 'running-right',
  /** 向左走。 */
  RunningLeft: 'running-left',
  /** 跳跃：打卡完成 / 待处理清空时的庆祝。 */
  Jumping: 'jumping',
  /** 审阅中：AI 正在生成计划。 */
  Review: 'review',
  /** 失败：AI 失败或提醒被跳过。 */
  Failed: 'failed',
} as const

/** 动画取值类型。 */
export type PetAnimValue = (typeof PetAnim)[keyof typeof PetAnim]

/**
 * 动画 → 图集行号。
 *
 * 【行序是外部约定，不要按"看起来更像"去调换】它来自 codexpet 素材的预览命名顺序
 * （idle / waving / waiting / running / running-right / running-left / jumping /
 * review / failed），换了顺序，宠物就会在"等待处理"时原地跑。
 */
export const PET_ANIM_ROW: Record<PetAnimValue, number> = {
  [PetAnim.Idle]: 0,
  [PetAnim.Waving]: 1,
  [PetAnim.Waiting]: 2,
  [PetAnim.Running]: 3,
  [PetAnim.RunningRight]: 4,
  [PetAnim.RunningLeft]: 5,
  [PetAnim.Jumping]: 6,
  [PetAnim.Review]: 7,
  [PetAnim.Failed]: 8,
}

/** 动画 → 中文标签（仅用于日志与调试，不进界面文案）。 */
export const PET_ANIM_LABEL: Record<PetAnimValue, string> = {
  [PetAnim.Idle]: '待机',
  [PetAnim.Waving]: '挥手',
  [PetAnim.Waiting]: '等待处理',
  [PetAnim.Running]: '原地跑',
  [PetAnim.RunningRight]: '向右走',
  [PetAnim.RunningLeft]: '向左走',
  [PetAnim.Jumping]: '庆祝',
  [PetAnim.Review]: '生成中',
  [PetAnim.Failed]: '失败',
}

/**
 * 芽生长档位（镜像 Go 侧 service.GrowthStage* 数值）。
 *
 * 只镜像数值与类型，**不镜像 growthStage 的判定逻辑**（铁律 1）——
 * 档位由后端计算好返回，前端只做「整数 → CSS class」的纯展示映射。
 */
export type GrowthStage = 0 | 1 | 2 | 3 | 4

/**
 * 把后端返回的档位夹取到 0..4 并生成 CSS class 名。
 *
 * 不含任何完成率阈值判定；夹取只是为了在异常输入下仍能稳定映射到一个合法 class。
 */
export function growthStageClass(stage: number): string {
  const n = Math.min(4, Math.max(0, Math.round(stage)))
  return `pet--growth-${n}`
}

/**
 * 庆祝动画的保持时长（毫秒）。
 *
 * 取值依据：跳跃一行 8 帧 @12fps ≈ 670ms 一轮，播放两轮半足够"看清发生了什么"，
 * 又不会让宠物一直跳着打扰人。
 */
export const PET_CELEBRATE_MS = 1600

/** 挥手动画的保持时长（毫秒）：一轮不到，点到即止。 */
export const PET_WAVE_MS = 900

/**
 * 设置轮询间隔（毫秒）。
 *
 * 宠物窗口与主窗是两个 webview，用户在设置页换宠物形象/缩放时，宠物窗口收不到
 * 通知（本仓【不为宠物新增事件常量】——事件契约里没有宠物域）。
 * 故用一次极廉价的本地绑定调用（读内存里的配置）轮询对齐，4 秒内的延迟
 * 对"换个形象"这种低频操作完全可以接受。
 */
export const PET_SETTINGS_POLL_MS = 4000

/**
 * 气泡适配等待窗口宽度生效的兜底（毫秒）。
 *
 * 隐藏页面的 rAF 会被 WebKit 暂停；只等 rAF 会让适配永远挂起。
 * 80ms 覆盖一次原生 resize + 一帧布局，正常路径下 rAF 会先到。
 */
export const PET_BUBBLE_FIT_WAIT_MS = 80

/**
 * 鼠标离开后多久把宠物收回边缘（毫秒）。
 *
 * 1200ms 是一个"确实走开了"与"只是划过"的分界：太快会在用户把手移向气泡按钮时
 * 就把宠物收走；太慢则贴边隐藏失去意义。
 */
export const PET_AUTO_COLLAPSE_MS = 1200

/**
 * 展开后无条件停留的时间（毫秒）。
 *
 * 窗口从屏幕外移回原位时，鼠标可能已经不在窗口里了（它停在屏幕边缘）。
 * 此时会立刻收到一次 mouseleave，若不设宽限期，宠物会在"展开→收起"之间抖动。
 */
export const PET_EXPAND_GRACE_MS = 2500

/**
 * 判定「一次点击」而非「一次拖动」的最大光标位移（屏幕像素）。
 *
 * 【为什么不用原生 click 事件】宠物本体整块是窗口的拖拽区。wails 的拖拽逻辑
 * （drag.ts）对 click / contextmenu / dblclick 挂了捕获阶段的 suppressEvent：
 * 只要 dragging 为真就 stopImmediatePropagation + preventDefault。
 * 而 mouseup 会先把 dragging 置回 false（primaryUp）——于是「拖完之后的那次 click
 * 到底传不传得到页面」取决于 mouseup 与原生拖拽会话的时序，两边都不可靠。
 *
 * 【位移判定能做什么、不能做什么——不要高估它】
 * 它只在 mouseup 真的被投递到页面时才有机会执行。而原生拖拽只要 1px 位移就会启动
 * （drag.ts 的 onMouseMove 里 canDrag 一成立就 invoke('wails:drag')），随后进入系统
 * 模态循环，把后续 mousemove / mouseup 一起吞掉。
 * 所以实际效果是：**「点一下挥手」只在零位移点击上触发**；手抖出 1px，这一轮既不会
 * 拖也不会挥手。这是 wails 这条原生通道的固有限制，不是这个阈值选得不好，改大改小
 * 都变不出第二次机会。
 *
 * 【单位必须是屏幕坐标（screenX/screenY）】原生拖拽期间窗口跟着光标走，
 * clientX/clientY 几乎不变；只有屏幕坐标能反映光标真实走过的距离。
 */
export const PET_CLICK_MAX_TRAVEL_PX = 4

/**
 * 一次「按下」最多能挂多久（毫秒）：超过就不再把它当成一次待判定的手势。
 *
 * 【为什么需要一个上限：mouseup 可能永远不来】原生拖拽一旦启动就进入系统模态循环，
 * 后续 mousemove / mouseup 不再投递到页面。于是按压状态会一直挂着——而拖动自己的
 * 窗口并不会让窗口失焦，blur 兜不住这件事。
 * 之后用户去点气泡上的按钮，那次 mouseup 会先被当成「这轮手势松手了」，拿一个几分
 * 钟前的基准坐标去算位移；判出来的结果虽然不是点击，但状态机已经脏了一轮。
 *
 * 1200ms 的选法：正常人从按下到松手远小于它；它只影响「过期手势怎么收尾」，
 * 不影响拖拽本身（拖拽由原生通道负责，与这个计时器无关）。
 */
export const PET_PRESS_STALE_MS = 1200

/**
 * 方案 B（JS 手动拖拽）的启动阈值（屏幕像素）。
 *
 * 只有位移超过它才真正开始移动窗口：否则一次普通的点击会顺手把窗口推歪 1px。
 */
export const PET_DRAG_TRAVEL_PX = 3

/**
 * 是否启用【方案 B：JS 手动拖拽兜底通道】。
 *
 * 默认关闭。主通道是 wails 原生的 `--wails-draggable: drag`（见 Pet.vue 的
 * .pet__art），B 只是它的备用轮胎：用 @wailsio/runtime 的 Window.Position() /
 * Window.SetPosition() 在前端自己搬窗口，不经过 wails 的命中测试。
 *
 * 【什么时候该把它改成 true】原生拖拽仍然拖不动（例如某天 wails 换了命中测试的
 * 实现，或这个透明 + Frameless 的窗口形态在某个平台上不吃 CSS 属性）。
 * 切换方式：只改这一个常量。Pet.vue 会自动挂上 mousemove 通道，并在同一处把
 * .pet__art 的拖拽属性内联成 no-drag——**两条通道必须互斥**：原生拖拽一旦启动就进
 * 系统模态循环，会把兜底要用的 mousemove 掐断，两者同时开着只会互相覆盖。
 *
 * 【类型为什么显式写成 boolean】为了不让 TS 把类型收窄成字面量 false。只有保留
 * boolean 的类型语义，它才是一个「开关」而不是一处恒定假值：否则读代码的人（以及
 * 将来的常量折叠优化）都可以合法地把 `if (PET_JS_DRAG_FALLBACK)` 整段当成死代码，
 * 而它恰恰是这条兜底通道唯一的入口。改成 true 时也不会被 CFA 当成恒假分支处理。
 */
export const PET_JS_DRAG_FALLBACK: boolean = false

/**
 * 贴边收起时露在屏幕内的宽度（逻辑像素）。
 *
 * 与 Go 侧 petDockPeekPx 必须一致：后端按这个值把窗口移出屏幕边缘，
 * 前端按这个值把宠物平移到那条窄边里。两边写成不同数字的症状是
 * 「宠物露出来半张脸」或「只露出空白」。
 */
export const PET_DOCK_PEEK_PX = 20

/** 宠物领域的事件名（domain 与 Go 侧 emit 的 "pet" 一致）。 */
export const PET_DOMAIN = 'pet'

/**
 * 宠物事件。
 *
 * 只订阅这个：贴边收起/展开是后端移动窗口后必须告知前端的状态，
 * 前端据此把宠物平移到露出的窄边里（否则视觉上宠物凭空消失）。
 */
export const PetEvent = {
  Docked: eventName(PET_DOMAIN, EventAction.Dock),
} as const

/** 贴边方向；空串表示已展开。 */
export type PetDockSide = '' | 'left' | 'right' | 'top' | 'bottom'

/** pet:dock 的载荷（镜像 Go 的 PetDockPayload）。 */
export interface PetDockedPayload {
  /** 贴在哪个边；空串表示已展开。 */
  side: PetDockSide
}
