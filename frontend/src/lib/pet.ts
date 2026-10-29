// 宠物渲染的单一真相：图集布局、动画行序、帧率、气泡适配的等待窗口。
//
// 为什么集中在这里：Pet.vue（渲染）与设置页（文案提示）都要用同一组数值，
// 而图集布局是【外部约定】——它由 codexpet 生态的素材决定，改了就要连素材一起换。
// 写两遍的结果是某天只改了一处，宠物要么只画半只、要么错行。
//
// 图集规格（v1，codexpet 的 pet.json 里 spriteVersionNumber = 1）：
//   单帧 192×208，8 列 × 9 行 = 整图 1536×1872，每行一套动作、每列一帧循环。

import { eventName, EventAction } from './event'
import { SitCategory, type SitCategoryValue } from './sit'

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
 * 打盹时的帧率。
 *
 * 【为什么这里可以降到 4，而上面明确禁止把 idle 降到 4】那条禁令针对的是**常驻待机**：
 * 宠物窗口拿不到键盘焦点，"失焦"是常态，按失焦降到 4fps 会让宠物从第一眼起就一顿一顿。
 * 打盹不一样——动画本身就是 4 秒一个起伏的慢循环，4fps 采样完全够；
 * 而且按定义此时用户不在看。
 *
 * 【边界】只在 dozing 状态生效；醒来必须在同一次渲染循环内恢复到 12 / 8fps，
 * 不允许"渐入"，否则醒来的第一眼是卡的。
 */
export const PET_SLEEP_FPS = 4

/**
 * 待机微动作的间隔区间（毫秒）。
 *
 * 【为什么不是固定间隔】固定节奏会形成"可预期的机械感"，比不动更容易让人烦躁。
 * 40–90 秒的依据：短于 40 秒过于频繁（一个 24 小时挂着的小窗），
 * 长于 90 秒则用户在一次抬头里很可能什么也没看到。
 */
export const PET_MICRO_ACTION_MIN_MS = 40000
export const PET_MICRO_ACTION_MAX_MS = 90000

/**
 * 单次微动作的时长上限（毫秒）。
 *
 * 微动作是"余光里的一下"，不是一段表演：超过 1.2 秒就会开始争夺注意力。
 * 这也是为什么它不需要气泡、不需要声音。
 */
export const PET_MICRO_ACTION_MS = 1200

/**
 * 进入打盹前需要连续空闲多久（毫秒）。
 *
 * 8 分钟的依据：一次起身倒水、一个短会、一段专注编码都不会超过它；
 * 而如果太短，"抬头发现宠物睡着了"会变成噪声。判据不是"多久没点宠物"，
 * 而是暂停原因为 idle / lock 的持续时长（见 Pet.vue 的 doze 判定）。
 */
export const PET_DOZE_AFTER_MS = 8 * 60 * 1000

/**
 * 惊醒反馈的时长（毫秒）。
 *
 * 【为什么短】它是"你回来了"的即时回执，不是一段动画：300ms 刚好能被余光捕捉到，
 * 又不至于让用户等它演完才能用。与挥手（900ms）分开取值——挥手是回应点击，
 * 惊醒是回应"你不在的时候它睡着了"，语义不同，节奏也该不同。
 */
export const PET_WAKE_MS = 300

/**
 * 打盹时内置形象的呼吸周期（毫秒）。
 *
 * 与 sprite 的 PET_SLEEP_FPS 是同一件事的两条实现：内置形象没有 rAF 循环，
 * 只能把 CSS 呼吸动画拉长。6.4 秒约等于常速的 1.6 倍，看起来是"睡沉了"而不是"卡住了"。
 */
export const PET_DOZE_BREATH_MS = 6400

/**
 * 待机微动作的类型。
 *
 * 【为什么不并进 PetAnim】`PetAnim` 的取值来自 codexpet 图集的行序，它是**状态语义**
 * （等待处理 / 庆祝 / 生成中），参与气泡优先级判断。而"张望""伸懒腰"不是状态，
 * 是叠加在待机之上的一次性装饰——并进去会污染 `setAnim` 的保持时长与优先级链。
 *
 * 也不要用数字：它在 class 名、日志与调试里都要可读。
 */
export const PetMicro = {
  /** 张望：两眼同步左右移，各一次。 */
  Look: 'look',
  /** 伸懒腰：整体纵向拉长再回。 */
  Stretch: 'stretch',
  /** 理芽：一片叶轻抖。 */
  Groom: 'groom',
  /** 打哈欠：张嘴 + 闭眼。 */
  Yawn: 'yawn',
} as const

/** 微动作取值类型。 */
export type PetMicroValue = (typeof PetMicro)[keyof typeof PetMicro]

/**
 * 微动作的权重表（同一数组顺序即"随机池"）。
 *
 * 张望出现得最多：它最不打扰、也最像"活的"。打哈欠最少——它带着"困"的含义，
 * 与打盹状态容易混淆，多了会让人以为宠物一直在犯困。
 */
export const PET_MICRO_POOL: PetMicroValue[] = [
  PetMicro.Look,
  PetMicro.Look,
  PetMicro.Stretch,
  PetMicro.Groom,
  PetMicro.Yawn,
]

// ---------------------------------------------------------------------------
// 镜像：宠物跟着「你在做什么」改变体态（openspec/changes/pet-activity-mirror）
//
// 【它不是状态，是待机的替身】镜像永远不写 `anim`——待处理提醒、庆祝、挥手、惊醒、
// 打盹都改的是 `anim` 或压制门控，天然优先。镜像只在 `anim === idle` 时决定
// 「待机该画哪一行 / 该挂哪个静态姿态 class」。这样它就不可能推迟任何一条健康提醒。
// ---------------------------------------------------------------------------

/** 镜像姿态。三类就够：再细分，用户在宠物身上分辨不出来。 */
export const PetMirrorPose = {
  /** 一起埋头干活（coding / design）。 */
  Working: 'working',
  /** 你说的我听着（communication）。 */
  Listening: 'listening',
  /** 一起看（browsing / media）。 */
  Watching: 'watching',
} as const

/** 镜像姿态取值类型。 */
export type PetMirrorPoseValue = (typeof PetMirrorPose)[keyof typeof PetMirrorPose]

/**
 * 行为类别 → 镜像姿态（唯一真相）。
 *
 * 【必须恰好覆盖 sit.AllCategories() 的六项】缺一项的后果是"某类活动下宠物毫无反应"，
 * 不报错、不崩溃——正是那种只有护栏能发现的问题。覆盖性由
 * `internal/guard/pet_behavior_test.go` 的 `TestPetMirrorCoversAllCategories` 双向校验。
 *
 * 【为什么值写字符串字面量而不是 `PetMirrorPose.Working`】护栏用与
 * `SIT_CATEGORY_LABEL` 同一套行级正则解析这张表（`[SitCategory.X]: 'pose'`），
 * 写成成员访问会让它解析到 0 条并 Fatal——那种"护栏瞎了"比漂移更难查。
 *
 * `other` 映射为空串：不知道你在做什么时，就不假装知道。
 */
export const PET_MIRROR_POSE_BY_CATEGORY: Record<SitCategoryValue, PetMirrorPoseValue | ''> = {
  [SitCategory.Coding]: 'working',
  [SitCategory.Design]: 'working',
  [SitCategory.Browsing]: 'watching',
  [SitCategory.Communication]: 'listening',
  [SitCategory.Media]: 'watching',
  [SitCategory.Other]: '',
}

/**
 * 进入一个姿态前需要的连续轮询次数。
 *
 * 3 次 × 4 秒 ≈ 12 秒：切到聊天工具回一句话不该让它换姿态；切过去干活则十几秒内跟上。
 * 轮询间隔见 `PET_SETTINGS_POLL_MS`，这里只数次数——不另开计时器。
 */
export const PET_MIRROR_CONFIRM_TICKS = 3

/**
 * 换过姿态之后的最短停留时长（毫秒）。
 *
 * 【为什么要有它，而且只在"进入"方向生效】用户在两块屏/两个窗口之间来回切时，
 * 类别每 4 秒就可能变一次。没有最小停留，宠物的身体会跟着抽搐。
 *
 * 【为什么"退出"不受它约束】压制条件（暂停/收起/打盹/无活动）表达的是"它此刻不该演"，
 * 不是"换个姿态演"。让退出也等 20 秒，症状是"我按了暂停，它还在埋头 20 秒"——
 * 一个小 bug。故：**进入慢、退出快**。
 */
export const PET_MIRROR_MIN_HOLD_MS = 20000

/**
 * `working` 姿态下手臂摆动的周期（毫秒）。
 *
 * 只是"手在动"的暗示，不是表演：1.6 秒一个来回约等于打字的节奏感，
 * 再快会像在抖，再慢就看不出在敲。CSS 通过 `--pet-mirror-shake-ms` 消费它，
 * 不在样式表里另写一份数字。
 */
export const PET_MIRROR_SHAKE_MS = 1600

/** 姿态 → 根节点 class；空姿态返回空串（不挂 class）。 */
export function petMirrorClass(pose: PetMirrorPoseValue | ''): string {
  return pose === '' ? '' : `pet--mirror-${pose}`
}

// ---------------------------------------------------------------------------
// 镜像状态机（纯函数）
//
// 【为什么单独抽出来，而不是写在 Pet.vue 里】这段逻辑有三条容易写错的时序规则：
// 进入要 3 次确认、换过之后 20 秒内不再换、压制条件立即退出。它没有任何 DOM 依赖，
// 放进 .vue 就只能靠"看代码"验证——本仓没有 JS 测试运行器（加一个要动 deps.yaml）。
// 抽成纯函数后，`scripts/e2e/mirror_logic.py` 可以在真实页面里 `import()` 这个模块
// 直接断言时序，既不用造测试框架，也不会让它"定义了没人验"。
// ---------------------------------------------------------------------------

/** 镜像状态机的全部状态（可序列化，便于断言时直接比较）。 */
export interface PetMirrorState {
  /** 当前已生效的姿态；空串表示没有。 */
  pose: PetMirrorPoseValue | ''
  /** 正在攒确认次数的候选姿态。 */
  candidate: PetMirrorPoseValue | ''
  /** 候选已连续命中的轮询次数。 */
  ticks: number
  /** 当前姿态的生效时间戳（毫秒）；空姿态时为 0。 */
  since: number
}

/** 初始状态：没有姿态、没有候选。 */
export const PET_MIRROR_INITIAL: PetMirrorState = { pose: '', candidate: '', ticks: 0, since: 0 }

/** 一次 tick 的输入。 */
export interface PetMirrorInput {
  /** 门控是否允许（由调用方判完：暂停/待处理/贴边/打盹/菜单…）。 */
  allowed: boolean
  /** 期望的姿态；空串表示"此刻不该演"。 */
  want: PetMirrorPoseValue | ''
  /** 本次 tick 的时间戳（毫秒）。 */
  nowMs: number
}

/**
 * 推进镜像状态机，返回**新**状态（不修改入参）。
 *
 * 三条规则按顺序生效：
 *   1. 不允许 → 立刻清空（退出快：压制条件表达的是"它不该演"，不是"换个姿态演"）；
 *   2. 候选变了 → 确认计数归 1，本轮不切换（进入慢：切过去回句话不该换姿态）；
 *   3. 候选稳定够久 → 切换，但当前姿态没到最小停留就先等着（防两块屏之间来回抽搐）。
 */
export function advanceMirror(state: PetMirrorState, input: PetMirrorInput): PetMirrorState {
  if (!input.allowed) {
    if (state.pose === '' && state.candidate === '' && state.ticks === 0) return state
    return { ...PET_MIRROR_INITIAL }
  }

  const want = input.want
  if (want !== state.candidate) {
    return { ...state, candidate: want, ticks: 1 }
  }

  const ticks = state.ticks < PET_MIRROR_CONFIRM_TICKS ? state.ticks + 1 : state.ticks
  if (want === state.pose) return ticks === state.ticks ? state : { ...state, ticks }
  if (state.pose !== '' && input.nowMs - state.since < PET_MIRROR_MIN_HOLD_MS) {
    return { ...state, ticks }
  }
  if (ticks < PET_MIRROR_CONFIRM_TICKS) return { ...state, ticks }
  return { pose: want, candidate: want, ticks, since: input.nowMs }
}

/**
 * 今日神态的三档 class 后缀。
 *
 * 与生长档位（0..4）无关：档位决定**形态**（芽的大小，长期不回退），
 * 神态只决定"今天看起来有没有精神"。两者分开是这次改动的核心。
 */
export type PetTodayMood = 'full' | 'part' | 'thin'

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

/**
 * 姿态 → 待机时改用哪一行图集。
 *
 * 【为什么排在 PET_ANIM_ROW 之后】本表在模块求值时就会读取 `PetAnim` 的成员，
 * 写在 `PetAnim` 声明之前会触发 TDZ（Cannot access 'PetAnim' before initialization），
 * 宠物页直接白屏——这类顺序问题的症状看起来像"整个前端挂了"，极难定位到一行常量。
 *
 * 【没有素材的事不做假动作】图集 9 行里没有任何一行能表达"听"：
 * 借 `waiting` 会让"在听你说话"和"等你处理提醒"长得一样，借 `running-*` 是走动。
 * 故 `listening` / `watching` 在 sprite 模式保持待机行，只有 builtin 才有姿态。
 * `working` 借 `review`（行 7，此前从未被触达）：埋头与「生成中」的身体语言一致，
 * 且"生成中"走的是 `anim`，与镜像这条通道天然不冲突。
 *
 * 【观感未定】若真机目视后 `review` 读起来不像在敲键盘，把这里改成 `PetAnim.Idle`
 * 即可退化为 builtin-only——映射表与门控都不必动。
 */
export const PET_MIRROR_ROW: Record<PetMirrorPoseValue, PetAnimValue> = {
  [PetMirrorPose.Working]: PetAnim.Review,
  [PetMirrorPose.Listening]: PetAnim.Idle,
  [PetMirrorPose.Watching]: PetAnim.Idle,
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

// ---------------------------------------------------------------------------
// 宠物右键菜单
//
// 【为什么菜单项写在这里而不是直接写在 Pet.vue 的模板里】标题、顺序、动作名是
// 三处都要用（菜单渲染、动作分发、护栏校验）。放一处，改文案不会出现"菜单上有、
// 分发里没有"的悬空项——护栏 `pet_menu_test.go` 就是照这张表逐个校验分发函数存在的。
// ---------------------------------------------------------------------------

/** 菜单宽度（逻辑像素）。窗口只有 240 宽，留两侧各 36 的余量。 */
export const PET_MENU_WIDTH = 168

/**
 * 菜单容器的内边距（逻辑像素），同时用作"距窗口边缘的安全距离"。
 *
 * 【为什么没有"菜单项高度"常量】条目高度不是常数：有内边距，而且「隐藏宠物」
 * 比别的项多一行说明。按常数估算会和实际差 10px 量级，而窗口是 `overflow: hidden`，
 * 差出来的几像素正好把最后一项裁掉且不报错。故高度一律以渲染后实测为准
 * （见 `Pet.vue` 的 `onPetContextMenu`），这里只留宽度与安全距离。
 */
export const PET_MENU_PADDING = 4

/**
 * 缩放的可循环档位。
 *
 * 只是"菜单里能快速切到的四个常用值"，**不是合法范围**——上下界仍以
 * `config.MinPetScale` / `config.MaxPetScale`（0.5–2.0）为准。真要把这些值写进配置，
 * 后端 `PetScaleValue()` 会再夹一次；这里不放 0.5 / 2.0 这两个极端值，
 * 是因为菜单的价值是"少走一步"，不是"穷举所有可能"。
 */
export const PET_MENU_SCALE_STEPS: number[] = [0.75, 1, 1.25, 1.5]

/**
 * 只有一个素材包时，「换形象」的说明文案。
 *
 * 【为什么必须有这句】内置形象之外没有任何包时，切换在语义上是空操作。
 * 第一版直接 `return`，用户点了之后菜单关闭、宠物没变、日志里也没有一行——
 * 看起来就是"这个功能坏了"。能点却什么都不发生，比置灰更糟：
 * 置灰至少说明"现在不能"，并且告诉了用户下一步该去哪。
 */
export const PET_MENU_PACK_EMPTY_HINT = '先在设置里导入素材包'

/**
 * 素材包列表读取失败时的说明。
 *
 * 必须和「没有导入包」分开：两者都会让「换形象」置灰，但用户可以采取的动作不同。
 * 把读取失败说成"去导入素材包"，会把用户引到一个解决不了问题的页面。
 */
export const PET_MENU_PACK_LOAD_FAILED_HINT = '读取素材包失败，请重试'

/**
 * 菜单项的动作名。
 *
 * 取值必须与 `Pet.vue` 里的处理函数**同名**——护栏按它去查 `function <名字>(` 是否存在。
 * 这样"加了菜单项但忘了写分发"会在测试阶段变红，而不是等用户点了没反应。
 */
export type PetMenuAction =
  | 'cyclePetPack'
  | 'cyclePetScale'
  | 'muteTodayFromMenu'
  | 'openMainWindow'
  | 'hidePetFromMenu'

/** 一个菜单项。 */
export interface PetMenuItem {
  /** 稳定标识，用于 v-for 的 key 与日志。 */
  id: string
  /** 菜单上显示的文字。 */
  label: string
  /** 补充说明（渲染成更小的第二行）；可选。 */
  hint?: string
  /** 点击后执行的动作（见 PetMenuAction）。 */
  action: PetMenuAction
}

/**
 * 菜单项（唯一真相）。
 *
 * 顺序即显示顺序。「隐藏宠物」必须有说明文字：宠物藏起来之后它自己就没有入口了，
 * 不说清楚"去哪找回来"，用户会以为宠物被删了。
 */
export const PET_MENU_ITEMS: readonly PetMenuItem[] = [
  { id: 'cycle-pack', label: '换形象', action: 'cyclePetPack' },
  { id: 'cycle-scale', label: '缩放', action: 'cyclePetScale' },
  { id: 'mute-today', label: '今天先这样', action: 'muteTodayFromMenu' },
  { id: 'open-main', label: '打开主窗', action: 'openMainWindow' },
  {
    id: 'hide-pet',
    label: '隐藏宠物',
    hint: '可在设置里重新打开',
    action: 'hidePetFromMenu',
  },
]

/** pet:dock 的载荷（镜像 Go 的 PetDockPayload）。 */
export interface PetDockedPayload {
  /** 贴在哪个边；空串表示已展开。 */
  side: PetDockSide
}
