<script lang="ts" setup>
// 桌面宠物窗口的渲染层（路由 /pet，meta.bare = true，故不套 AppShell）。
//
// 它是「提醒的化身」，不是装饰：
//   到点提醒 → 切到「等待你处理」并冒气泡说话；打卡完成（待处理清空）→ 庆祝。
//
// 两种渲染模式，统一在同一个容器里：
//   1. builtin：程序化 SVG（呼吸 + 眨眼），不依赖任何素材文件——新用户开箱就有宠物；
//   2. sprite ：<canvas> 画图集（单帧 192×208，8 列 × 9 行），素材来自 codex/导入。
//
// 状态与事件：只订阅【既有】事件 reminder:fired / reminder:changed（见 lib/reminder），
// 不为宠物新增事件常量——事件契约里没有宠物域，多一个域就多一处镜像要维护。
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { Window } from '@wailsio/runtime'
import {
  AckSitBubble,
  CollapsePet,
  CompleteReminder,
  DismissPresentationSuggestion,
  ExpandPet,
  GetDeferralState,
  GetPauseState,
  GetPetGrowth,
  GetPetSettings,
  GetPetSprite,
  GetRestModeSnapshot,
  GetSitSnapshot,
  ListPendingFires,
  ListPetPacks,
  MuteSitToday,
  OpenMainWindow,
  SetPetBubbleHeight,
  SetPetBubbleVisible,
  SetPetSettings,
  SetPresentationMode,
  SetSitTone,
  SnoozeReminder,
  SnoozeSitNudge,
  TakeActiveBreak,
} from '../../bindings/morrow/app.js'
import { invoke } from '../lib/invoke'
import { useEvent } from '../composables/useEvent'
import { ReminderEvent, type ReminderFiredPayload } from '../lib/reminder'
import { State as RestModeState, type Snapshot as RestModeSnapshot } from '../../bindings/morrow/internal/restmode/models.js'
import { logInfo, logWarn } from '../lib/log'
import { SitAction, SitActivity, sitActionLabel, sitMoodClass } from '../lib/sit'
import {
  ActionID,
  BubbleKind as SitBubbleKindEnum,
  State as SitStateEnum,
  Tone as SitToneEnum,
} from '../../bindings/morrow/internal/sit/models.js'
import {
  FRAME_H,
  FRAME_W,
  PET_ANIM_ROW,
  PET_AUTO_COLLAPSE_MS,
  PET_BUBBLE_FIT_WAIT_MS,
  PET_CELEBRATE_MS,
  PET_CLICK_MAX_TRAVEL_PX,
  PET_COLS,
  PET_DOZE_AFTER_MS,
  PET_DOZE_BREATH_MS,
  PET_DRAG_TRAVEL_PX,
  PET_EXPAND_GRACE_MS,
  PET_FPS,
  PET_IDLE_FPS,
  PET_JS_DRAG_FALLBACK,
  PET_MICRO_ACTION_MAX_MS,
  PET_MICRO_ACTION_MIN_MS,
  PET_MICRO_ACTION_MS,
  PET_MICRO_POOL,
  PET_MENU_ITEMS,
  PET_MENU_PACK_EMPTY_HINT,
  PET_MENU_PACK_LOAD_FAILED_HINT,
  PET_MENU_PADDING,
  PET_MENU_SCALE_STEPS,
  PET_MENU_WIDTH,
  PET_MIRROR_INITIAL,
  PET_MIRROR_POSE_BY_CATEGORY,
  PET_MIRROR_ROW,
  PET_MIRROR_SHAKE_MS,
  PET_PRESS_STALE_MS,
  PET_SETTINGS_POLL_MS,
  PET_SLEEP_FPS,
  PET_WAKE_MS,
  PET_WAVE_MS,
  PetAnim,
  PetEvent,
  PetMicro,
  PetMirrorPose,
  advanceMirror,
  growthStageClass,
  petMirrorClass,
  type GrowthStage,
  type PetAnimValue,
  type PetDockedPayload,
  type PetDockSide,
  type PetMenuItem,
  type PetMicroValue,
  type PetMirrorPoseValue,
  type PetMirrorState,
  type PetTodayMood,
} from '../lib/pet'

/** 待处理提醒（用绑定返回类型反推，不手写镜像——Go 侧一改这里立刻跟着变）。 */
type PendingFire = NonNullable<Awaited<ReturnType<typeof ListPendingFires>>>[number]

/** 素材包（同上：从绑定返回值反推类型）。 */
type PetPack = NonNullable<Awaited<ReturnType<typeof ListPetPacks>>>[number]

/** 久坐快照/气泡同样用绑定返回类型反推，避免前端手写第二份快照。 */
type SitSnapshotData = Awaited<ReturnType<typeof GetSitSnapshot>>
type SitBubbleData = NonNullable<SitSnapshotData['bubble']>

// ---- 渲染状态 ----

/** 精灵图缩放倍数（0.5–2.0，由设置页控制）。 */
const scale = ref(1)
/** 当前选中的素材包 ID（'builtin' = 内置 SVG 形象）。 */
const packID = ref('')
/** 渲染模式：sprite 模式才需要 canvas 与动画循环。 */
const mode = ref<'builtin' | 'sprite'>('builtin')
/** 当前动画（决定画图集的哪一行）。 */
const anim = ref<PetAnimValue>(PetAnim.Idle)

/**
 * 芽的**形态**档位（0–4），决定芽的大小与叶展开角。
 *
 * 来源是 `GetPetGrowth().stage`，它在 Go 侧已经是 `max(长期档, 今日档)`，
 * 故这里拿到的是"只可能持平或变大"的长期形态——跨天不会缩回去。
 */
const growth = ref<GrowthStage>(0)

/** 是否已完成首次生长同步；首次同步不播「长高」，否则每次启动都会跳一下。 */
let growthSynced = false

/**
 * 今日**神态**档（full / part / thin），只改高光与轻垂，不改尺寸。
 *
 * 【为什么不与 growth 合成一个】形态是"养了多久"，神态是"今天怎么样"。
 * 合成一个就又回到"每天早上缩小一次"的老问题。
 *
 * 初值取 part（中性）而不是 thin：挂载后第一次 syncGrowth 之前会先渲染一帧，
 * 用 thin 的话新用户打开宠物看到的是"一株蔫芽"，等 4 秒轮询回来才转正常。
 */
const todayMood = ref<PetTodayMood>('part')

/**
 * 当前正在播放的待机微动作；空串表示没有。
 *
 * 它是**叠加层**：不写 `anim`、不改 `PetAnim`，因此不参与气泡优先级判断。
 */
const microAction = ref<PetMicroValue | ''>('')

/**
 * 当前镜像姿态；空串表示不镜像。
 *
 * 【它是待机的替身，不是状态】镜像永远不写 `anim`——待处理、庆祝、挥手、惊醒、打盹
 * 都改的是 `anim` 或压制门控，天然优先。它只回答"待机时该画哪一行 / 挂哪个姿态 class"。
 */
const mirrorPose = ref<PetMirrorPoseValue | ''>('')

/**
 * 镜像姿态对应的待机绘制行（仅 sprite 模式有意义）。
 *
 * 单独存一份而不是每次从 PET_MIRROR_ROW 现查：绘制循环每秒跑 12 次，
 * 这里的原因是"绘制层不该做业务查表"——查表发生在轮询里一次。
 */
const mirrorRow = ref<PetAnimValue>(PetAnim.Idle)

/** 是否在打盹（空闲/锁屏持续 PET_DOZE_AFTER_MS 后进入）。 */
const dozing = ref(false)

/** 是否正在播「惊醒」反馈（醒来后的 300ms）。 */
const waking = ref(false)

/**
 * 系统是否要求减弱动态效果。
 *
 * 用 matchMedia 而不是只靠 CSS 媒体查询兜：微动作由 JS 调度，
 * 若只靠 CSS 停动画，class 仍会被挂上、计时器仍在跑，白费一次调度。
 */
const prefersReducedMotion = ref(false)

/**
 * 根节点的 CSS 变量。
 *
 * 时长与间距的唯一真相在 lib/pet.ts；CSS 不复制数字，只消费这里注入的变量。
 * 否则改 TS 常量时动画时长会静默留在旧值，测试也看不出来。
 */
const petRootStyle: Record<string, string> = {
  '--pet-micro-ms': `${PET_MICRO_ACTION_MS}ms`,
  '--pet-doze-breath-ms': `${PET_DOZE_BREATH_MS}ms`,
  '--pet-menu-padding': `${PET_MENU_PADDING}px`,
  '--pet-mirror-shake-ms': `${PET_MIRROR_SHAKE_MS}ms`,
}

// ---- 暂停状态 ----

/** 是否处于暂停状态（智能免打扰）。 */
const paused = ref(false)
/** 暂停原因（idle/lock/meeting/fullscreen/focus/calendar；空串表示未暂停）。 */
const pauseReason = ref('')
/** 是否显示「进入演示模式」建议（后端已过滤设置/触发/会话）。 */
const suggestionVisible = ref(false)
/** 建议气泡动作的忙碌态，避免重复点击。 */
const presentationBusy = ref(false)
/** 是否有提醒正在自然停顿顺延（宠物显示「等你停下」）。 */
const deferring = ref(false)

// ---- 休息模式状态 ----

/**
 * 休息模式快照。
 *
 * 三种可见状态在宠物上分别对应：
 *   - warning：气泡显示预警文案（Go 侧按真实剩余分三档算好放进 cue_text，US-9）
 *   - countdown：宠物切到「陪做」姿态 + 气泡「陪你休息」+ 剩余秒数（US-27）
 *   - 其他（idle/done/skipped）：宠物不介入，走原有 pending 通道
 */
const restSnap = ref<RestModeSnapshot | null>(null)

// ---- 久坐感知状态 ----

/** 宠物窗口 4 秒轮询的久坐快照；null 表示尚未拉到。 */
const sitSnapshot = ref<SitSnapshotData | null>(null)
/**
 * 已确认展示的气泡本地钉住到 TTL 结束。
 *
 * 后端 Ack 语义是“已展示”，不是“已处理”；Ack 后 PendingSitSpeech 不再返回它。
 * 若只依赖轮询，气泡会在 4 秒后被下一轮空快照抹掉，用户来不及点「更多」。
 */
const pinnedSitBubble = ref<SitBubbleData | null>(null)
/** 当前待展示的久坐/欢迎气泡：优先服务端新气泡，否则保留本地钉住的一条。 */
const sitBubble = computed<SitBubbleData | null>(() => sitSnapshot.value?.bubble ?? pinnedSitBubble.value)
/** 主按钮（去掉 more；more 单独渲染成展开入口）。 */
const sitMainActions = computed(() =>
  (sitBubble.value?.actions ?? []).filter((action) => action !== ActionID.ActionMore),
)
/** 当前气泡是否带「更多」入口。 */
const sitHasMore = computed(() => (sitBubble.value?.actions ?? []).includes(ActionID.ActionMore))
/** 温和语气下不再提供「别这样说」。 */
const sitCanGentle = computed(() => (sitSnapshot.value?.tone ?? SitToneEnum.ToneSassy) !== SitToneEnum.ToneGentle)
/** 「更多」第二行是否展开。 */
const sitMoreOpen = ref(false)
/** 久坐动作在途，防重复点击。 */
const sitBusy = ref(false)
/** 已确认展示的久坐气泡 ID；避免 4 秒轮询重复 Ack。 */
let ackedSitBubbleID = ''

// ---- 气泡状态 ----

/** 待处理提醒数量；>0 时气泡可见。 */
const pendingCount = ref(0)
/** 气泡标题（提醒标题）。 */
const bubbleTitle = ref('')
/** 气泡正文（提醒内容，可为空）。 */
const bubbleText = ref('')
/**
 * 气泡顶部的场景名（回答"这是哪个计划"）。
 *
 * 【只有 ListPendingFires 能给】reminder:fired 的载荷里只有 scenario_id、没有场景名，
 * 故它由 applyPending 从 service.PendingFire.scenario_name 填充，
 * 事件到达时先留空（那一瞬气泡只有标题），异步刷新后补齐。
 */
const bubbleScenario = ref('')

/** 是否正在提交打卡动作：防止一次点击被重复提交（后端会拒绝重复打卡，但请求不该成串发出）。 */
const busy = ref(false)

/**
 * 气泡内的失败提示；空串表示不显示。
 *
 * 【为什么要有它】动作失败时只写日志，用户看到的是「点了没反应」；这条文案
 * 把失败变成可见事实。已被别处处理的场景走自愈路径，不显示这条。
 */
const bubbleError = ref('')

// ---- 右键菜单状态 ----

/** 菜单是否可见。 */
const menuVisible = ref(false)
/** 菜单左上角坐标（相对宠物窗口，已夹到窗口内）。 */
const menuPos = ref({ x: 0, y: 0 })
/** 菜单 DOM 引用：判"这一下点在菜单外面"要用。 */
const menuRef = ref<HTMLElement | null>(null)
/** 菜单动作在途，防重复触发。 */
const menuBusy = ref(false)
/**
 * 打开菜单时预取的素材包列表。
 *
 * 【为什么要预取】「换形象」能不能点，取决于当前有几个包。第一版是点下去才知道
 * （`ListPetPacks` 只有一个包时静默 return），用户看到的是"菜单关了、宠物没变"。
 * 现在打开菜单就先取一次，把不可用状态前置成置灰 + 说明。
 */
const menuPacks = ref<PetPack[]>([])

/** 素材包列表是否读取失败；用于区分「没导入」和「读不到」两种置灰原因。 */
const menuPacksFailed = ref(false)

/** 是否有第二个形象可切（只有内置形象时「换形象」没有意义）。 */
const canSwitchPack = computed(() => menuPacks.value.length > 1)

// ---- 气泡扩窗的运行时句柄 ----

/** 当前可见气泡的 DOM 引用（四个互斥分支共用；同一时刻只有一个挂载）。 */
const bubbleRef = ref<HTMLElement | null>(null)
/** 宠物本体：测量气泡需求时要用它的实际高度。 */
const petArtRef = ref<HTMLElement | null>(null)
/** 上次上报给 Go 的气泡总高度；相同值不重复调用。 */
let lastBubbleNeed = -1
/** 气泡尺寸观察器；气泡切换/卸载时重建。 */
let bubbleObserver: ResizeObserver | undefined
/** 适配是否正在执行；并发触发只排队一次，避免旧的测量结果最后落地。 */
let bubbleFitRunning = false
let bubbleFitQueued = false

/**
 * 把气泡的绝对需求换算成窗口总高度。
 *
 * 用 DOM 实测而不是复制 CSS 常量：内边距、间距、字号一旦调整，这里自动跟随。
 * 气泡被窗口顶部裁掉时 offsetHeight 仍是完整高度，故不会越量越矮。
 */
function measureBubbleNeed(bubble: HTMLElement): number {
  const art = petArtRef.value
  const petEl = bubble.parentElement
  if (!art || !petEl) return 0
  const styles = getComputedStyle(petEl)
  const padTop = parseFloat(styles.paddingTop) || 0
  const padBottom = parseFloat(styles.paddingBottom) || 0
  // 间距用两块的实际几何差，而不是读 row-gap：跨引擎的 flex gap 取值更稳，
  // 且气泡被顶部裁掉时 getBoundingClientRect 仍是完整盒子。
  const gap = Math.max(0, art.getBoundingClientRect().top - bubble.getBoundingClientRect().bottom)
  return Math.ceil(padTop + bubble.offsetHeight + gap + art.offsetHeight + padBottom)
}

/**
 * 让窗口适配当前气泡：先加宽，下一帧按实际宽度测量，再设置高度。
 *
 * 【为什么先加宽再测量】气泡的换行行数取决于窗口宽度。先按窄窗量会得到一个
 * 过高的窗口，再被宽度变化纠正，用户会看到一次多余的跳变。
 */
async function applyBubbleFit(): Promise<void> {
  if (bubbleFitRunning) {
    bubbleFitQueued = true
    return
  }
  bubbleFitRunning = true
  try {
    do {
      bubbleFitQueued = false
      await fitBubbleOnce()
    } while (bubbleFitQueued)
  } finally {
    bubbleFitRunning = false
  }
}

/** 单轮适配。串行化由 applyBubbleFit 负责。 */
async function fitBubbleOnce(): Promise<void> {
  // 拖拽手势中不改窗口几何：宠物正跟着鼠标走，扩窗会与之抢位置。
  if (petPressed) return
  // 菜单打开期间不改窗口几何：菜单用 CSS 让气泡让位，气泡测量会变成 0，
  // 若在这里缩回基础尺寸，已经夹好的菜单会越过新窗口底边被裁掉，
  // 窗口位置也会变——规格要求这两件事都不发生。关闭菜单后由 closeMenu 补一次 fit。
  if (menuVisible.value) return
  const bubble = bubbleRef.value
  if (!bubble || bubble.offsetWidth === 0 || bubble.offsetHeight === 0) {
    lastBubbleNeed = -1
    try {
      await invoke(() => SetPetBubbleVisible(false), 'SetPetBubbleVisible')
    } catch (e) {
      logWarn('恢复宠物窗口尺寸失败', { err: String(e) })
    }
    return
  }

  try {
    await invoke(() => SetPetBubbleVisible(true), 'SetPetBubbleVisible')
  } catch (e) {
    logWarn('切换宠物气泡形态失败', { err: String(e) })
    return
  }
  await nextTick()
  // WebKit 会把「可见但非前台」的窗口也报成 hidden，此时 rAF 会被暂停；
  // 只等 rAF 会让适配永远挂起。用超时兜底，保证测量一定会发生。
  await new Promise<void>((resolve) => {
    const timer = window.setTimeout(resolve, PET_BUBBLE_FIT_WAIT_MS)
    window.requestAnimationFrame(() => {
      window.clearTimeout(timer)
      resolve()
    })
  })

  // 等待期间菜单可能被打开：此时气泡已被 CSS 让位，不能再按 0 高度去缩窗口。
  if (menuVisible.value) return
  const need = measureBubbleNeed(bubble)
  if (need <= 0) return
  if (need === lastBubbleNeed) return
  try {
    await invoke(() => SetPetBubbleHeight(need), 'SetPetBubbleHeight')
    lastBubbleNeed = need
  } catch (e) {
    logWarn('调整宠物窗口高度失败', { err: String(e) })
  }
}

/** 重建气泡尺寸观察器：切到另一个气泡元素时旧观察器必须断开。 */
function observeBubble(el: HTMLElement | null): void {
  bubbleObserver?.disconnect()
  bubbleObserver = undefined
  if (!el) return
  bubbleObserver = new ResizeObserver(() => {
    void applyBubbleFit()
  })
  bubbleObserver.observe(el)
}

/** 手势结束后补一次适配：拖动期间被主动跳过，松手后要追平当前文案。 */
function refitBubbleAfterGesture(): void {
  if (bubbleRef.value) void applyBubbleFit()
}

// 气泡出现/消失/切换分支：重建观察器并立即适配。
watch(
  bubbleRef,
  (el) => {
    observeBubble(el)
    void applyBubbleFit()
  },
  { flush: 'post' },
)

// 缩放在设置页变化时，宠物高度变了但气泡本身未必变——观察器抓不到，主动重测。
watch(
  scale,
  () => {
    void applyBubbleFit()
  },
  { flush: 'post' },
)

// ---- 画布 / 动画循环的运行时句柄（不是响应式数据）----

/** canvas 元素引用（仅 sprite 模式存在）。 */
const canvasRef = ref<HTMLCanvasElement | null>(null)
/** 已解码的图集。null 表示尚未就绪或加载失败（此时用内置形象）。 */
let sprite: HTMLImageElement | null = null
/** requestAnimationFrame 句柄；0 表示循环未运行。 */
let rafID = 0
/** 上一次推进帧的时间戳（毫秒），用于按固定帧率节流。 */
let lastPaintAt = 0
/** 当前帧序号（对 PET_COLS 取模得到列号）。 */
let frameIndex = 0
/** 窗口是否获得焦点：失焦时降到 PET_IDLE_FPS，避免常驻小窗满帧烧电。 */
let focused = true
/** 动画保持计时器（挥手/庆祝这类"临时动作"的回落）。 */
let holdTimer: number | undefined
/** 设置轮询计时器（主窗换素材后宠物窗口要跟着换）。 */
let pollTimer: number | undefined
/** 微动作结束计时器（一次性，不是循环）。 */
let microTimer: number | undefined
/** 惊醒反馈结束计时器（一次性）。 */
let wakeTimer: number | undefined
/** 距离下一次微动作还剩多少毫秒；由 4 秒轮询递减，不单独开计时器。 */
let nextMicroActionIn = 0
/** 暂停原因为 idle/lock 的起始时间戳（毫秒）；0 表示当前不满足打盹条件。 */
let dozeSince = 0
/**
 * 镜像状态机的全部状态（候选/确认计数/生效时间）。
 *
 * 这里只存"状态"，**决策在 `advanceMirror()` 纯函数里**（lib/pet.ts）：
 * 时序规则放 .vue 就只能靠看代码保证，而这份逻辑有三条容易写错的时序约束。
 */
let mirrorState: PetMirrorState = { ...PET_MIRROR_INITIAL }
/** 减弱动态效果的媒体查询句柄；卸载时要摘监听。 */
let motionQuery: MediaQueryList | undefined

/**
 * 贴边方向；空串表示已展开。
 *
 * 由后端的 pet:dock 事件驱动：窗口是后端移动的，"宠物该不该平移到露出的窄边里"
 * 只有前端能决定，所以必须靠这个状态配合，否则收起后视觉上是宠物凭空消失。
 */
const dockSide = ref<PetDockSide>('')
/** 鼠标是否停在宠物窗口内（普通变量：不参与渲染）。 */
let hovering = false
/** 收起计时器；hover 期间取消。 */
let collapseTimer: number | undefined
/** 展开后的宽限期截止时间戳；期间忽略 mouseleave。 */
let expandGuardUntil = 0

// ---- 按下 / 拖拽手势的运行时状态（普通变量：不参与渲染）----

/**
 * 左键当前是否按在宠物本体上。
 *
 * 用它把「mousedown 在宠物上」与「mouseup 在窗口别处」配成一次手势——
 * 鼠标可能在拖动中离开窗口，那时 mouseup 落在哪个元素上是不确定的。
 */
let petPressed = false
/** 按下时的光标【屏幕】坐标：判「点击」还是「拖动」的基准。 */
let pressOriginX = 0
let pressOriginY = 0
/** 按下的时刻（毫秒）：用来判定这次按压是不是已经过期（见 PET_PRESS_STALE_MS）。 */
let pressAt = 0
/**
 * 本轮手势是否已经越过点击阈值（= 这是一次拖动）。
 *
 * 【为什么不用 jsDragMoving 代替】那个只在兜底分支里赋值，默认模式下恒为 false。
 * 而原生拖拽会吞掉 mouseup，pressAt 过期的兜底只能挡住"很久以前的那次"，挡不住
 * "刚刚按下去、走了一段、mouseup 侥幸送达"这一种；在 mousemove 里预先记下位移才是
 * 唯一可靠的判据（原生拖拽启动前一定会先来几条 mousemove）。
 */
let petDragged = false

/** 方案 B：本轮手势是否已经越过启动阈值（进入真正搬窗口的阶段）。 */
let jsDragMoving = false
/** 方案 B：窗口基准位置是否已就绪（Position() 是异步的，没就绪就动会把窗口甩到 0,0）。 */
let jsDragBaseReady = false
/** 方案 B：mousedown 时的窗口位置（全局坐标）。 */
let jsDragBaseX = 0
let jsDragBaseY = 0
/** 方案 B：本次要移到的目标位置（由 mousemove 持续覆盖）。 */
let jsDragWantX = 0
let jsDragWantY = 0
/** 方案 B：待执行的 rAF 句柄（0 表示没有排队）。 */
let jsDragRaf = 0

/** 宠物画布的 CSS 尺寸：图集单帧乘以缩放倍数。 */
const artStyle = computed(() => ({
  width: `${Math.round(FRAME_W * scale.value)}px`,
  height: `${Math.round(FRAME_H * scale.value)}px`,
}))

/**
 * 宠物本体的实际内联样式：尺寸 + 拖拽属性。
 *
 * 【为什么拖拽属性要内联，而不是只写在 CSS 里】
 * 兜底模式（PET_JS_DRAG_FALLBACK）必须把原生拖拽关掉：原生拖拽一旦启动就进系统
 * 模态循环，会把兜底要用的 mousemove 掐断——两条通道同时开着，兜底等于没跑，
 * 个别平台还会逐帧互相覆盖成抖动。
 * 内联样式的优先级高于 CSS 规则，所以关掉只发生在兜底模式，默认模式仍由
 * .pet__art 的那条 CSS 声明兜底（CSS 里那条保留，作为默认值与文档）。
 */
const artStyleWithDrag = computed(() => ({
  ...artStyle.value,
  '--wails-draggable': PET_JS_DRAG_FALLBACK ? 'no-drag' : 'drag',
}))

/** 是否显示气泡：有待处理提醒时才说话。 */
const bubbleVisible = computed(() => pendingCount.value > 0)
/**
 * 当前气泡对应的待处理提醒 ID（0 表示没有）。
 *
 * 气泡里的「完成 / 推迟」按钮直接作用于它——这就是"不用打开主窗口也能处理"的实现。
 * 取列表里的第一条：用户看得见的那一条，就该是按钮作用的那一条。
 */
const currentFireID = ref(0)

/** 气泡标题的兜底文案：提醒本身没标题时不至于出现一个空气泡。 */
const bubbleHeading = computed(() => bubbleTitle.value || '该起来动一下了')

/** 是否显示暂停气泡：暂停时显示「我先不打扰」。 */
const pauseBubbleVisible = computed(() => paused.value)

/**
 * 是否显示演示建议：后端已过滤触发时长/设置/会话；这里只加 UI 优先级——
 * 有待处理提醒时让位，避免建议盖住真正的提醒。
 */
const presentationSuggestionVisible = computed(
  () => suggestionVisible.value && pendingCount.value === 0,
)

/**
 * 顺延气泡：用户正在输入时显示「等你停下」。
 * 有待处理提醒时让位——待处理是用户真正需要处理的东西。
 */
const deferringBubbleVisible = computed(
  () => deferring.value && pendingCount.value === 0,
)

/** 是否显示休息模式预警气泡（Warning 状态优先于普通待处理气泡）。 */
const restWarningVisible = computed(() => restSnap.value?.state === RestModeState.StateWarning)
const restWarningText = computed(() => restSnap.value?.cue_text ?? '')

/** 是否处于休息模式陪做状态（Countdown）。 */
const restAccompanying = computed(() => restSnap.value?.state === RestModeState.StateCountdown)
const restAccompanyingText = computed(() => {
  const s = restSnap.value
  if (!s) return ''
  const m = Math.floor(s.remaining_sec / 60)
  const sec = s.remaining_sec % 60
  const pad = (n: number) => String(n).padStart(2, '0')
  return `陪你休息 ${pad(m)}:${pad(sec)}`
})

/**
 * 久坐气泡是否可见：排在所有既有气泡之后，且贴边收起/静音当天不显示。
 *
 * 它是「正式提醒永远优先」的最后一道保险：即使后端已经生成了待展示气泡，
 * 只要有更高优先级内容，前端就不渲染、也不 Ack（避免没看见却消耗预算）。
 */
const sitBubbleVisible = computed(
  () =>
    sitBubble.value !== null &&
    !restAccompanying.value &&
    !restWarningVisible.value &&
    !deferringBubbleVisible.value &&
    !presentationSuggestionVisible.value &&
    !pauseBubbleVisible.value &&
    !bubbleVisible.value &&
    dockSide.value === '' &&
    // 「今天先这样」只压软提醒/记忆；动作确认与欢迎回来仍可见。
    (!sitSnapshot.value?.muted_today ||
      sitBubble.value?.kind === SitBubbleKindEnum.BubbleGreeting),
)

/** 情绪只在待机时露出来；等待/庆祝/休息陪做优先。 */
const sitMoodVisible = computed(
  () =>
    anim.value === PetAnim.Idle &&
    sitSnapshot.value?.state === SitStateEnum.StateTracking &&
    !sitSnapshot.value?.muted_today &&
    !restAccompanying.value &&
    !restWarningVisible.value,
)

const sitMoodClassValue = computed(() =>
  sitMoodVisible.value ? sitMoodClass(sitSnapshot.value?.mood) : '',
)

/**
 * 镜像姿态的根节点 class。
 *
 * 【何时挂】builtin 模式且宠物处于待机**或挥手**时。
 *   - sprite 模式的姿态由 `mirrorRow` 换图集行表达，再挂 class 会重复表达同一件事；
 *   - 等待处理/庆祝/惊醒期间不挂：那几种状态下宠物在表达别的东西（并且庆祝/等待
 *     会动整个身体，叠一个蜷着的底态只会让它变形）。
 *
 * 【为什么挥手要挂】不挂的话，蜷着的宠物点一下会先"站起来"挥手再蹲回去——
 * 一次 900ms 的姿态回弹，看着像坏了。挂上之后挥手只动手臂，身体姿态不动。
 * 代价是必须让镜像的手臂规则在 `.pet--waving` 下让位（见样式里那两个 `:not`），
 * 否则后写的镜像规则会盖掉 `.pet--waving .pet__arm--right`，变成"点了不挥手"且不报错。
 */
const mirrorClassValue = computed(() => {
  const visible = anim.value === PetAnim.Idle || anim.value === PetAnim.Waving
  return mode.value === 'builtin' && visible ? petMirrorClass(mirrorPose.value) : ''
})

/** 暂停气泡的文案：根据暂停原因显示不同的提示。 */
const pauseBubbleText = computed(() => {
  if (!paused.value) return ''
  const reasonMap: Record<string, string> = {
    idle: '你好像离开了，我先不打扰',
    lock: '屏幕已锁定，我先不打扰',
    meeting: '检测到会议应用，我先不打扰',
    fullscreen: '检测到全屏应用，我先不打扰',
    focus: '系统专注模式已开启，我先不打扰',
    calendar: '日历显示你正忙，我先不打扰',
    presentation: '演示模式中，我先隐身',
  }
  return reasonMap[pauseReason.value] || '我先不打扰'
})

// ---- 图集绘制 ----

/**
 * 把当前动画的当前帧画到 canvas 上。
 *
 * 每次都从图集【裁剪一帧】再缩放绘制：canvas 内部按 devicePixelRatio 放大，
 * 故在 Retina 上不会糊（CSS 尺寸由 scale 决定，与内部分辨率解耦）。
 */
function paintCurrentFrame(): void {
  const canvas = canvasRef.value
  if (!canvas || !sprite) return
  const ctx = canvas.getContext('2d')
  if (!ctx) return
  // 镜像只替换【待机】那一行：镜像姿态是待机的替身，不是新的 anim 值。
  // 这样待处理/庆祝/挥手/惊醒天然优先，且 applyPending 的 setAnim(Idle) 不会把姿态打回。
  const row = PET_ANIM_ROW[anim.value === PetAnim.Idle ? mirrorRow.value : anim.value]
  const col = frameIndex % PET_COLS
  ctx.clearRect(0, 0, canvas.width, canvas.height)
  ctx.drawImage(
    sprite,
    col * FRAME_W,
    row * FRAME_H,
    FRAME_W,
    FRAME_H,
    0,
    0,
    canvas.width,
    canvas.height,
  )
}

/**
 * 动画循环。
 *
 * 用 requestAnimationFrame + 时间戳累加控制帧率，而不是每帧都重画或叠 setInterval：
 *   - rAF 在窗口不可见时会被浏览器自动暂停，无需自己判断"要不要停"；
 *   - 时间戳节流保证 12fps 是"最多 12 次/秒"，掉帧时不会补画一串中间帧（那会看起来在抽搐）。
 */
function loop(now: number): void {
  rafID = window.requestAnimationFrame(loop)
  // 打盹时降到 4fps（见 PET_SLEEP_FPS 的注释）。这个循环只在 sprite 模式运行；
  // builtin 的慢节奏由 CSS 动画时长负责（见样式里的 .pet--dozing）。
  const fps = dozing.value ? PET_SLEEP_FPS : focused && !document.hidden ? PET_FPS : PET_IDLE_FPS
  if (now - lastPaintAt < 1000 / fps) return
  lastPaintAt = now
  frameIndex += 1
  paintCurrentFrame()
}

/** 启动动画循环（重复调用安全）。 */
function startLoop(): void {
  if (rafID !== 0) return
  lastPaintAt = 0
  rafID = window.requestAnimationFrame(loop)
}

/** 停止动画循环（重复调用安全）。 */
function stopLoop(): void {
  if (rafID === 0) return
  window.cancelAnimationFrame(rafID)
  rafID = 0
}

/**
 * 按 devicePixelRatio 设置 canvas 的内部分辨率，并画第一帧。
 *
 * 【必须等 nextTick】canvas 元素是 v-if 渲染的（切到 sprite 模式才出现），
 * setup 里拿 ref 时它是 null。
 */
async function prepareCanvas(): Promise<void> {
  await nextTick()
  const canvas = canvasRef.value
  if (!canvas) return
  const dpr = window.devicePixelRatio || 1
  canvas.width = Math.round(FRAME_W * dpr)
  canvas.height = Math.round(FRAME_H * dpr)
  const ctx = canvas.getContext('2d')
  if (ctx) {
    // 像素风素材放大时必须关掉平滑，否则边缘发糊。
    ctx.imageSmoothingEnabled = false
  }
  paintCurrentFrame()
}

/**
 * 加载指定素材包的精灵图。
 *
 * 兜底：素材拿不到 / 解码失败 → 退回内置 SVG 形象并记一条 warn，
 * 绝不能白屏（宠物窗口白屏时用户只会以为"功能坏了"，且没有 DevTools 可查）。
 */
async function loadSprite(id: string): Promise<void> {
  if (id === '' || id === 'builtin') {
    sprite = null
    mode.value = 'builtin'
    stopLoop()
    return
  }

  try {
    const dataURI = await invoke(() => GetPetSprite(id), 'GetPetSprite')
    if (!dataURI) {
      throw new Error('精灵图为空')
    }
    const image = new Image()
    image.onload = () => {
      sprite = image
      mode.value = 'sprite'
      void prepareCanvas().then(startLoop)
    }
    image.onerror = () => {
      sprite = null
      mode.value = 'builtin'
      stopLoop()
      logWarn('宠物精灵图解码失败，已退回内置形象', { pack: id })
    }
    image.src = dataURI
  } catch (e) {
    sprite = null
    mode.value = 'builtin'
    stopLoop()
    logWarn('宠物素材加载失败，已退回内置形象', { pack: id, err: String(e) })
  }
}

// ---- 动画状态机 ----

/**
 * 切换动画。
 *
 * holdMs > 0 表示这是一个"临时动作"（挥手/庆祝）：到时后重新按待处理数量对齐状态，
 * 而不是硬写回 idle——期间可能又来了新提醒。
 */
function setAnim(next: PetAnimValue, holdMs = 0): void {
  if (anim.value !== next) {
    anim.value = next
    frameIndex = 0
  }
  if (holdTimer !== undefined) {
    window.clearTimeout(holdTimer)
    holdTimer = undefined
  }
  if (holdMs > 0) {
    holdTimer = window.setTimeout(() => {
      holdTimer = undefined
      void refreshPending()
    }, holdMs)
  }
}

/**
 * 把待处理列表映射成「宠物状态 + 气泡文案」。
 *
 * 这是状态机的唯一收口：待处理非空 → waiting + 气泡；空 → idle 且收起气泡。
 */
function applyPending(list: PendingFire[]): void {
  // 待处理数据一变，上一次的失败提示就过期了；真失败会在 catch 里重新设置。
  bubbleError.value = ''
  pendingCount.value = list.length
  const first = list[0]
  if (!first) {
    bubbleScenario.value = ''
    bubbleTitle.value = ''
    bubbleText.value = ''
    currentFireID.value = 0
    setAnim(PetAnim.Idle)
    return
  }
  // 场景名必须在标题之上：用户看到"喝水"却不知道属于哪套计划时，
  // 这条提醒就只是一句没头没尾的唠叨。
  bubbleScenario.value = first.scenario_name
  bubbleTitle.value = first.title
  bubbleText.value = first.content
  currentFireID.value = first.fire_id
  // 提醒优先于一切表演：掐掉微动作，打盹中则立刻醒来（不看下一轮轮询）。
  clearMicroAction()
  wakeUp()
  setAnim(PetAnim.Waiting)
}

/** 重新拉取待处理提醒（事件到达、临时动作结束、窗口启动时都会走到这里）。 */
async function refreshPending(): Promise<void> {
  try {
    const list = (await invoke(() => ListPendingFires(), 'ListPendingFires')) ?? []
    applyPending(list)
  } catch (e) {
    // 拉取失败不改状态：宁可维持上一帧的显示，也不要突然收起正在说话的气泡。
    logWarn('读取待处理提醒失败', { err: String(e) })
  }
}

/**
 * 拉取并应用芽的生长档位（挂载 / reminder:changed 事件 / 4 秒轮询都会走到这里）。
 *
 * 两个档位分开用（判定全在 Go 侧，前端只做"整数 → class"）：
 *   - `stage` 是**形态**（max(高水位, 长期)），决定芽的大小，跨天不回退；
 *   - `today_stage` 是**今日神态**，只改高光与轻垂。
 *
 * 失败时保持上一次值，不闪回、不报错。
 */
async function syncGrowth(): Promise<void> {
  const previous = growth.value
  const firstSync = !growthSynced
  try {
    const result = await invoke(() => GetPetGrowth(), 'GetPetGrowth')
    growth.value = (result?.stage ?? 0) as GrowthStage
    growthSynced = true
    // 跨档时播一次长高作为回执；提醒/打盹/菜单优先，不抢它们的画面。
    if (
      !firstSync &&
      growth.value > previous &&
      anim.value === PetAnim.Idle &&
      !dozing.value &&
      !waking.value &&
      !menuVisible.value
    ) {
      setAnim(PetAnim.Jumping, PET_CELEBRATE_MS)
    }
    const today = result?.today_stage ?? 0
    const hasTarget = result?.has_target ?? false
    // 没安排目标时「0%」不是失败，用中性神态；否则新用户/停用场景会一直蔫着。
    todayMood.value = hasTarget ? (today >= 4 ? 'full' : today >= 1 ? 'part' : 'thin') : 'part'
  } catch (e) {
    // 读不到时保持上一次形态：宠物窗口没有 DevTools，静默降级优于报错。
    logWarn('读取芽生长档位失败', { err: String(e) })
  }
}

/**
 * 拉取并应用暂停、演示建议与顺延状态（4 秒轮询）。
 *
 * 两个调用独立失败：一个读不到时保留它上一次的值，不拖累另一个。
 */
async function syncInterruptionState(): Promise<void> {
  try {
    const result = await invoke(() => GetPauseState(), 'GetPauseState')
    paused.value = result?.paused ?? false
    pauseReason.value = result?.reason ?? ''
    suggestionVisible.value = result?.suggestion_visible ?? false
  } catch (e) {
    // 读不到时保持上一次状态：静默降级优于报错。
    logWarn('读取暂停/建议状态失败', { err: String(e) })
  }
  try {
    const deferral = await invoke(() => GetDeferralState(), 'GetDeferralState')
    deferring.value = deferral?.active ?? false
  } catch (e) {
    logWarn('读取顺延状态失败', { err: String(e) })
  }
}

// ---- 待机行为：微动作 / 打盹 / 惊醒 ----
//
// 宠物在「没有提醒」的时间里必须有自己的节奏，否则它只是一张会呼吸的贴图。
// 三条原则（改这里之前先读）：
//   1. 只在 builtin 模式做——图集只有 9 行固定动作，硬塞会错行；
//   2. 一切压制条件优先于表演（待处理 / 暂停 / 演示 / 气泡 / 贴边收起 / 减弱动态）；
//   3. 调度挂在既有 4 秒轮询上，不新增 setInterval——否则"谁在什么时候动"没法推理。

/** 取一个 [MIN, MAX] 之间的随机间隔（毫秒）。 */
function randomMicroDelay(): number {
  const span = PET_MICRO_ACTION_MAX_MS - PET_MICRO_ACTION_MIN_MS
  return PET_MICRO_ACTION_MIN_MS + Math.round(Math.random() * span)
}

/** 清掉正在播放的微动作（被打断时用）。 */
function clearMicroAction(): void {
  if (microTimer !== undefined) {
    window.clearTimeout(microTimer)
    microTimer = undefined
  }
  microAction.value = ''
}

/**
 * 能不能演一次微动作。
 *
 * 这是唯一的门控收口：新增任何"别演"的条件都加在这里，不要散到调用点，
 * 否则下一次加条件一定漏掉某条路径（比如只在 click 里判、却没在轮询里判）。
 */
function canPlayMicro(): boolean {
  if (mode.value !== 'builtin') return false
  if (prefersReducedMotion.value) return false
  if (dozing.value || waking.value) return false
  if (pendingCount.value > 0) return false
  if (paused.value) return false
  if (suggestionVisible.value) return false
  if (restAccompanying.value || restWarningVisible.value) return false
  if (deferringBubbleVisible.value || pauseBubbleVisible.value) return false
  if (bubbleVisible.value || sitBubbleVisible.value) return false
  if (dockSide.value !== '') return false
  if (menuVisible.value) return false
  if (anim.value !== PetAnim.Idle) return false
  return true
}

/**
 * 挑一个本次可以演的微动作。
 *
 * 【为什么不是"镜像生效就禁演"】镜像在白天几乎一直生效，禁演等于把 pet-autonomy 的
 * 微动作整天关掉，宠物反而更"死"。冲突只发生在**抢同一个元素**的动作上，
 * 故只把它们从池子里剔掉：
 *   - `stretch` 动 `.pet__body-group`（镜像的坐低/缩放也在这一层）；
 *   - `look` 动 `.pet__eyes`（`watching` 也放大这一层）。
 * 为什么必须剔：CSS animation 会**整段覆盖**同一元素的静态 transform，
 * 硬叠的结果是"演微动作那一秒突然变回正常大小，然后弹回去"。
 */
function pickMicroAction(): PetMicroValue {
  const pose = mirrorPose.value
  const pool = PET_MICRO_POOL.filter((item) => {
    if (item === PetMicro.Stretch) return pose === ''
    if (item === PetMicro.Look) return pose !== PetMirrorPose.Watching
    return true
  })
  const index = Math.floor(Math.random() * pool.length)
  return pool[index] ?? PetMicro.Groom
}

/** 播一次微动作：随机取池中一项，到时自动清除。 */
function playMicro(): void {
  microAction.value = pickMicroAction()
  nextMicroActionIn = randomMicroDelay()
  if (microTimer !== undefined) window.clearTimeout(microTimer)
  microTimer = window.setTimeout(() => {
    microTimer = undefined
    microAction.value = ''
  }, PET_MICRO_ACTION_MS)
}

/** 打盹条件的唯一判定：暂停原因为空闲/锁屏、且没有更需要露脸的事。 */
function shouldDozeNow(): boolean {
  if (!paused.value) return false
  if (pauseReason.value !== 'idle' && pauseReason.value !== 'lock') return false
  if (pendingCount.value > 0) return false
  if (dockSide.value !== '') return false
  return true
}

/** 推进打盹状态（由 4 秒轮询驱动）。 */
function tickDoze(nowMs: number): void {
  if (!shouldDozeNow()) {
    dozeSince = 0
    if (dozing.value) wakeUp()
    return
  }
  if (dozing.value) return
  if (dozeSince === 0) {
    dozeSince = nowMs
    return
  }
  // 用真实时间差而不是数 tick：锁屏/休眠期间计时器会被节流，
  // 数 tick 会让"离开 10 分钟"变成"等 10 分钟 × 节流倍数"。
  if (nowMs - dozeSince >= PET_DOZE_AFTER_MS) enterDoze()
}

/** 进入打盹：先掐掉微动作，否则会看到"边睡边张望"。 */
function enterDoze(): void {
  clearMicroAction()
  dozing.value = true
}

/**
 * 醒来：幂等。
 *
 * 三个入口（轮询发现暂停解除 / 鼠标进入 / 出现待处理）都可能同时触发；
 * 不在打盹时直接返回，避免同一次醒来播两遍惊醒。
 */
function wakeUp(): void {
  dozeSince = 0
  if (!dozing.value) return
  dozing.value = false
  waking.value = true
  // builtin 的惊醒由 .pet--waking 的 CSS 负责；sprite 没有眼睛/芽的 DOM，
  // 用既有 jumping 行播一次短回执，否则精灵模式醒来只有帧率变化、看不到反馈。
  if (mode.value === 'sprite') setAnim(PetAnim.Jumping, PET_WAKE_MS)
  if (wakeTimer !== undefined) window.clearTimeout(wakeTimer)
  wakeTimer = window.setTimeout(() => {
    wakeTimer = undefined
    waking.value = false
  }, PET_WAKE_MS)
}

/** 推进微动作调度（由 4 秒轮询驱动；精度 ±4 秒对 40–90 秒的间隔无感）。 */
function tickMicro(elapsedMs: number): void {
  if (microAction.value !== '') return
  if (!canPlayMicro()) {
    // 不能演时把计数重置成"下一轮的随机值"：否则条件一解除就会立刻补演一次，
    // 用户看到的是"刚开完会，宠物猛地动一下"。
    nextMicroActionIn = randomMicroDelay()
    return
  }
  // 0 表示"还没排过"（窗口刚打开的第一轮）：先排一次随机间隔。
  // 少了这一步，宠物会在窗口出现 4 秒后就动一下，像是被启动动作惊到。
  if (nextMicroActionIn <= 0) {
    nextMicroActionIn = randomMicroDelay()
    return
  }
  nextMicroActionIn -= elapsedMs
  if (nextMicroActionIn > 0) return
  playMicro()
}

// ---- 待机行为：镜像 ----
//
// 宠物跟着「你在做什么」改变体态（openspec/changes/pet-activity-mirror）。
// 三条原则（改这里之前先读）：
//   1. 镜像永不写 `anim`——它是待机的替身，见 paintCurrentFrame 与 design.md 第 4 节；
//   2. 进入慢、退出快：进入要 PET_MIRROR_CONFIRM_TICKS 次确认 + 最小停留，退出立即；
//   3. 调度挂在既有 4 秒轮询上，不新增 setInterval。

/**
 * 能不能应用镜像姿态。
 *
 * 【为什么这里不判 prefersReducedMotion】减弱动态约束的是"动"，不是"样子"：
 * 姿态本身是静态倾斜（动画部分由 CSS 媒体查询关掉），保留它才是对用户有用的信息。
 * 微动作不同——它整个就是"动一下"，故在 canPlayMicro 里直接挡掉。
 */
function canApplyMirror(): boolean {
  const activity = sitSnapshot.value?.activity
  if (!activity) return false
  if (activity.level === SitActivity.Away) return false
  if (pendingCount.value > 0) return false
  if (paused.value) return false
  if (suggestionVisible.value) return false
  if (dockSide.value !== '') return false
  if (dozing.value || waking.value) return false
  if (menuVisible.value) return false
  return true
}

/** 立即清空镜像（压制条件成立 / 卸载时用；不受最小停留约束）。 */
function clearMirror(): void {
  mirrorState = { ...PET_MIRROR_INITIAL }
  if (mirrorPose.value === '') return
  mirrorPose.value = ''
  mirrorRow.value = PetAnim.Idle
}

/**
 * 把姿态落到渲染层并留痕。
 *
 * 【为什么打日志】姿态是"看得见但说不清"的东西：用户报"没反应"时，
 * 唯一能区分「链路没通」与「幅度太小」的证据就是这一行。
 */
function setMirrorPose(pose: PetMirrorPoseValue | '', nowMs: number, category: string): void {
  mirrorPose.value = pose
  mirrorRow.value = pose === '' ? PetAnim.Idle : PET_MIRROR_ROW[pose]
  logInfo('镜像姿态已切换', {
    pose: pose || '(无)',
    category: category || '(无)',
    at: nowMs,
  })
}

/**
 * 推进镜像姿态（由 4 秒轮询驱动）。
 *
 * 决策全在 `advanceMirror()`（纯函数）：进入要 3 次确认、换过之后 20 秒内不再换、
 * 压制条件立即退出。这里只做三件事——判门控、取候选、把结果落到 ref 上。
 */
function tickMirror(nowMs: number): void {
  const allowed = canApplyMirror()
  const category = sitSnapshot.value?.activity?.category ?? ''
  const want: PetMirrorPoseValue | '' =
    allowed && category ? (PET_MIRROR_POSE_BY_CATEGORY[category] ?? '') : ''

  const next = advanceMirror(mirrorState, { allowed, want, nowMs })
  if (next.pose !== mirrorState.pose) {
    setMirrorPose(next.pose, nowMs, category)
  }
  mirrorState = next
}

/** 从宠物建议进入演示模式（source=suggested）。 */
async function enterPresentationFromSuggestion(): Promise<void> {
  if (presentationBusy.value) return
  presentationBusy.value = true
  try {
    await invoke(() => SetPresentationMode(true, 'suggested'), 'SetPresentationMode')
    await syncInterruptionState()
  } catch (e) {
    logWarn('进入演示模式失败', { err: String(e) })
  } finally {
    presentationBusy.value = false
  }
}

/** 忽略本次建议；同一会议会话内不再建议。 */
async function dismissPresentationSuggestion(): Promise<void> {
  if (presentationBusy.value) return
  presentationBusy.value = true
  try {
    await invoke(() => DismissPresentationSuggestion(), 'DismissPresentationSuggestion')
    suggestionVisible.value = false
  } catch (e) {
    logWarn('忽略演示建议失败', { err: String(e) })
  } finally {
    presentationBusy.value = false
  }
}

/**
 * 拉取并应用休息模式快照（4 秒轮询）。
 *
 * Warning → 气泡显示预警文案；Countdown → 宠物切「陪做」姿态并显示 mm:ss；
 * 其他状态一律收起休息气泡，走原有 pending/pause 通道。
 */
async function syncRestModeState(): Promise<void> {
  try {
    restSnap.value = (await invoke(() => GetRestModeSnapshot(), 'GetRestModeSnapshot')) ?? null
  } catch (e) {
    logWarn('读取休息模式快照失败', { err: String(e) })
  }
}

/** 拉取久坐快照；失败保持上一轮，避免气泡突然消失。 */
async function syncSitState(): Promise<void> {
  try {
    sitSnapshot.value = await invoke(() => GetSitSnapshot(), 'GetSitSnapshot')
    const bubble = sitSnapshot.value?.bubble
    if (bubble) {
      pinnedSitBubble.value = bubble
    } else if (pinnedSitBubble.value && pinnedSitBubble.value.expires_at * 1000 <= Date.now()) {
      pinnedSitBubble.value = null
    }
  } catch (e) {
    logWarn('读取久坐状态失败', { err: String(e) })
  }
}

/** 本地收起当前久坐气泡，等下一次轮询以服务端为准。 */
function clearSitBubble(): void {
  pinnedSitBubble.value = null
  if (sitSnapshot.value?.bubble) {
    sitSnapshot.value = { ...sitSnapshot.value, bubble: undefined }
  }
  sitMoreOpen.value = false
}

/** 立即确认当前气泡已被展示；幂等，失败只记日志。 */
async function ackSitBubbleNow(id: string): Promise<void> {
  if (!id || id === ackedSitBubbleID) return
  ackedSitBubbleID = id
  try {
    await invoke(() => AckSitBubble(id), 'AckSitBubble')
  } catch (e) {
    logWarn('确认久坐气泡失败', { err: String(e) })
  }
}

/** 久坐气泡动作分发：只按后端给的 action ID 走，不在前端猜业务。 */
async function onSitAction(action: string): Promise<void> {
  const bubble = sitBubble.value
  if (!bubble || sitBusy.value) return
  if (action === SitAction.More) {
    sitMoreOpen.value = !sitMoreOpen.value
    return
  }
  if (action === SitAction.OpenApp) {
    openMainWindow()
    return
  }
  if (action === SitAction.Dismiss) {
    await ackSitBubbleNow(bubble.id)
    clearSitBubble()
    return
  }

  sitBusy.value = true
  try {
    if (action === SitAction.StartBreak) {
      await invoke(() => TakeActiveBreak(bubble.id), 'TakeActiveBreak')
    } else if (action === SitAction.Snooze10) {
      await invoke(() => SnoozeSitNudge(bubble.id), 'SnoozeSitNudge')
    }
    clearSitBubble()
    await syncSitState()
  } catch (e) {
    logWarn('处理久坐气泡失败', { action, err: String(e) })
    await syncSitState()
  } finally {
    sitBusy.value = false
  }
}

/** 「今天先这样」：只看今天，正式提醒不受影响。 */
async function onSitMuteToday(): Promise<void> {
  if (sitBusy.value) return
  sitBusy.value = true
  try {
    await invoke(() => MuteSitToday(), 'MuteSitToday')
    // 后端会紧接着创建静音确认气泡；不要先清本地，避免轮询先 Ack 后把它抹掉。
    await syncSitState()
  } catch (e) {
    logWarn('静音今日久坐失败', { err: String(e) })
  } finally {
    sitBusy.value = false
  }
}

/** 「别这样说」：切到温和语气并持久化。 */
async function onSitGentle(): Promise<void> {
  if (sitBusy.value) return
  sitBusy.value = true
  try {
    await invoke(() => SetSitTone(String(SitToneEnum.ToneGentle)), 'SetSitTone')
    // 后端会创建温和确认气泡；交给 sync 替换，避免清掉刚渲染的确认。
    await syncSitState()
  } catch (e) {
    logWarn('切换宠物语气失败', { err: String(e) })
  } finally {
    sitBusy.value = false
  }
}

// 久坐气泡真正出现时才 Ack；贴边/被更高优先级气泡挡住时不消耗预算。
watch(
  [sitBubbleVisible, () => sitBubble.value?.id ?? ''],
  ([visible, id]) => {
    if (!visible || !id || id === ackedSitBubbleID) return
    void ackSitBubbleNow(id)
  },
  { flush: 'post' },
)

// 换了一条气泡就收起「更多」第二行，避免把上一条的菜单带到下一条。
watch(
  () => sitBubble.value?.id ?? '',
  () => {
    sitMoreOpen.value = false
  },
)

/** reminder:changed 的处理：只有"刚才有、现在没有"才庆祝。 */
async function handleChanged(): Promise<void> {
  const before = pendingCount.value
  await Promise.all([refreshPending(), syncGrowth()])
  if (before > 0 && pendingCount.value === 0) {
    setAnim(PetAnim.Jumping, PET_CELEBRATE_MS)
  }
}

// ---- 事件订阅（既有事件契约，不新增常量）----

useEvent<ReminderFiredPayload>(ReminderEvent.Fired, (payload) => {
  // 载荷已带展示文案：先让气泡立刻说话，再异步对齐总数（少一次往返的等待）。
  // 唯独场景名拿不到（载荷里只有 scenario_id），所以下面填的只能算"半条信息"——
  // 紧接着的 refreshPending 会以 ListPendingFires 的结果为【准】覆盖全部文案
  // （含 bubbleScenario），不要把这里当成最终真相。
  //
  // 【必须先清掉上一条的残留】否则 A 场景的提醒刚处理完、B 场景的提醒到点，
  // 这一瞬会把「A 的场景名」配在「B 的标题」上；而万一 refreshPending 失败
  // （那个函数的约定是"失败不改状态"），张冠李戴就会一直挂着。
  // 宁可这一瞬只有标题，也不要把场景名配错——场景名是"这是哪个计划"的唯一线索。
  bubbleScenario.value = ''
  bubbleError.value = ''
  currentFireID.value = 0
  bubbleTitle.value = payload.title
  bubbleText.value = payload.content
  pendingCount.value = Math.max(1, pendingCount.value)
  setAnim(PetAnim.Waiting)
  void refreshPending()
  // 顺延中的提醒被投递后，尽快让「等你停下」气泡收起。
  void syncInterruptionState()
})

useEvent(ReminderEvent.Changed, () => {
  void handleChanged()
})

// 贴边状态：后端把窗口移出/移回屏幕边缘后广播。
// 前端只做视觉配合（把宠物平移到露出的那条窄边里），窗口位置由后端负责。
useEvent<PetDockedPayload>(PetEvent.Docked, (payload) => {
  dockSide.value = payload.side
  // 收起时必须关菜单：菜单是窗口内的 fixed 层，窗口被移到屏幕外之后它跟着跑出可视区，
  // 等用户下次划过窄边展开时，菜单会以一个"过期"的位置突然出现。
  if (payload.side !== '') closeMenu()
})

// ---- 交互 ----

/**
 * 点宠物：挥手。
 *
 * 【注意】宠物本体整块是拖拽区（--wails-draggable: drag，窗口没有标题栏，
 * 拖它就是唯一的移动方式）。所以「点击」不能靠原生 click 事件判（它会和拖拽
 * 抢同一串 mousedown/mouseup，见 lib/pet.ts 的 PET_CLICK_MAX_TRAVEL_PX），
 * 由 onWindowPointerUp 按位移判出来再调这里；打开主窗口的入口在气泡上。
 */
function onPetClick(): void {
  if (anim.value === PetAnim.Jumping) return
  setAnim(PetAnim.Waving, PET_WAVE_MS)
}

/** 左键按在宠物本体上：记录基准，供「点击 vs 拖动」判定使用。 */
function onPetPointerDown(event: MouseEvent): void {
  if (event.button !== 0) return
  petPressed = true
  petDragged = false
  pressAt = performance.now()
  pressOriginX = event.screenX
  pressOriginY = event.screenY
  if (!PET_JS_DRAG_FALLBACK) return
  // 方案 B 需要窗口当前的真实位置做基准：这里只管发起，不阻塞手势——
  // Position() 是异步的，若它返回得比第一次 mousemove 慢，jsDragBaseReady
  // 仍是 false，onWindowPointerMove 会跳过本轮而不是拿 0,0 去搬窗口。
  jsDragMoving = false
  jsDragBaseReady = false
  Window.Position()
    .then((pos) => {
      if (!petPressed) return
      jsDragBaseX = pos.x
      jsDragBaseY = pos.y
      jsDragBaseReady = true
    })
    .catch((e: unknown) => {
      logWarn('读取宠物窗口位置失败，本次手势不拖拽', { err: String(e) })
    })
}

/**
 * 光标移动：**无条件挂载**（两种模式都要用），但只有兜底模式才真的搬窗口。
 *
 * 无条件挂载的理由是前半段：原生拖拽启动前一定会先到几条 mousemove，用它们把
 * 「这一轮是拖动」记下来——一旦原生模态循环吞掉 mouseup，就再没有第二次机会判定。
 */
function onWindowPointerMove(event: MouseEvent): void {
  if (!petPressed) return
  const dx = event.screenX - pressOriginX
  const dy = event.screenY - pressOriginY
  if (!petDragged && Math.hypot(dx, dy) >= PET_CLICK_MAX_TRAVEL_PX) {
    petDragged = true
  }

  // 以下是方案 B 的搬窗口逻辑，默认模式下不参与。
  if (!PET_JS_DRAG_FALLBACK) return
  if (!jsDragMoving) {
    if (Math.hypot(dx, dy) < PET_DRAG_TRAVEL_PX) return
    if (!jsDragBaseReady) return
    jsDragMoving = true
  }
  jsDragWantX = jsDragBaseX + Math.round(dx)
  jsDragWantY = jsDragBaseY + Math.round(dy)
  scheduleJsDrag()
}

/**
 * 用 rAF 节流 SetPosition。
 *
 * 每一帧最多一次 IPC：mousemove 在 Retina 上可以一秒几百条，逐条搬窗口
 * 既费往返又会让窗口在两次刷新之间跳好几个位置，看起来是抖的。
 */
function scheduleJsDrag(): void {
  if (jsDragRaf !== 0) return
  jsDragRaf = window.requestAnimationFrame(() => {
    jsDragRaf = 0
    if (!jsDragMoving) return
    Window.SetPosition(jsDragWantX, jsDragWantY).catch((e: unknown) => {
      logWarn('拖拽移动宠物窗口失败', { err: String(e) })
    })
  })
}

/**
 * 松手：结束手势；零位移才算一次点击（挥手）。
 *
 * 【两道闸门缺一不可】
 *   1. 过期检查：原生拖拽吞掉 mouseup，这个基准可能已经放了几分钟（拖动自己的窗口
 *      并不会失焦，blur 兜不住）。过期的按压只清理状态，绝不拿它判一次点击。
 *   2. 拖动预判：petDragged 在 mousemove 里就已置位，所以即使 mouseup 侥幸送达、
 *      且窗口跟着光标走导致位移看起来很小，也不会把一次拖动误判成点击。
 */
function onWindowPointerUp(event: MouseEvent): void {
  if (!petPressed) return
  if (performance.now() - pressAt > PET_PRESS_STALE_MS) {
    cancelPetPress()
    refitBubbleAfterGesture()
    return
  }
  const dragged = petDragged
  cancelPetPress()
  refitBubbleAfterGesture()
  if (event.button !== 0) return
  if (dragged) return
  const dx = event.screenX - pressOriginX
  const dy = event.screenY - pressOriginY
  if (Math.hypot(dx, dy) > PET_CLICK_MAX_TRAVEL_PX) return
  onPetClick()
}

/**
 * 丢弃未完成的手势（窗口失焦 / 组件卸载 / 按压过期）。
 *
 * 鼠标可能在窗口外松开、也可能压根收不到 mouseup：不清掉的话，下一次 mouseup
 * 会拿着一个早已过期的基准坐标去判位移。
 */
function cancelPetPress(): void {
  petPressed = false
  petDragged = false
  jsDragMoving = false
  jsDragBaseReady = false
  if (jsDragRaf !== 0) {
    window.cancelAnimationFrame(jsDragRaf)
    jsDragRaf = 0
  }
}

/** 点气泡：把主窗口拉到前面（复用 Go 侧 restoreWindow）。 */
function openMainWindow(): void {
  invoke(() => OpenMainWindow(), 'OpenMainWindow').catch((e) => {
    logWarn('打开主窗口失败', e)
  })
}

// ---- 右键菜单 ----

/** 宠物设置的可绑定视图（从绑定返回值反推，不手写第二份类型）。 */
type PetSettings = Awaited<ReturnType<typeof GetPetSettings>>
/** 只允许改这三项；其余字段一律沿用当前快照。 */
type PetSettingsPatch = Partial<Pick<PetSettings, 'pet_visible' | 'pet_scale' | 'pet_pack_id'>>

/**
 * 把坐标夹进「视口减去菜单尺寸」的范围内。
 *
 * 单独成函数是因为要用两次：先用常量粗放一次（避免首帧闪到窗口外），
 * 渲染后再用实测尺寸纠正一次。
 */
function clampMenuOffset(cursor: number, size: number, viewport: number): number {
  const max = Math.max(PET_MENU_PADDING, viewport - size - PET_MENU_PADDING)
  return Math.min(Math.max(PET_MENU_PADDING, cursor), max)
}

/**
 * 打开菜单。
 *
 * 【事件源为什么是 contextmenu——已对源码确认，不是猜的】
 * 读 v3.0.0-beta.23 的 `@wailsio/runtime/dist/drag.js`：
 *   1. 拖拽只在 `pressed & 1`（主键）时才 arm，右键按下只让 `buttons` 记上 bit 2，
 *      既不进 `primaryDown` 也不会置 `dragging`；
 *   2. `suppressEvent` 对 contextmenu 的抑制条件是 `dragging || resizing`；
 *   3. 拖拽进行中的右键 mousedown 会被 `update()` 吞（`dragging && event.button !== 0`）。
 * 故静止右键能正常到达页面；拖拽中右键被吞正好是期望行为（此刻不该弹菜单）。
 *
 * 【贴边收起时不弹】此刻用户是在"划过"那 20px 窄边，不是在操作宠物；
 * 弹出菜单还会把宠物挤出可见区，而他想要的只是展开。
 */
async function onPetContextMenu(event: MouseEvent): Promise<void> {
  if (dockSide.value !== '') return
  // 菜单是用户在操作，别同时演微动作
  clearMicroAction()
  // 先取素材包列表：菜单项要用它决定「换形象」是能点还是置灰。
  // 这一步在显示之前完成，避免"先显示成可点、几十毫秒后突然变灰"的闪动。
  await refreshMenuPacks()
  // 第一次用常量粗放：避免菜单先渲染在窗口外再跳回来。
  menuPos.value = {
    x: clampMenuOffset(event.clientX, PET_MENU_WIDTH, window.innerWidth),
    y: Math.max(PET_MENU_PADDING, event.clientY),
  }
  menuVisible.value = true

  // 第二次按【实测】尺寸纠正。必须有这一步：条目高度不是常数（有内边距、
  // 「隐藏宠物」还多一行说明），按常数估算会差 10px 量级——而 `.pet` 是
  // overflow:hidden，差出来的那几像素正好把最后一项裁掉，且不会有任何报错。
  await nextTick()
  const menu = menuRef.value
  if (!menu) return
  menuPos.value = {
    x: clampMenuOffset(event.clientX, menu.offsetWidth, window.innerWidth),
    y: clampMenuOffset(event.clientY, menu.offsetHeight, window.innerHeight),
  }
}

/** 关闭菜单（幂等）。 */
function closeMenu(): void {
  if (!menuVisible.value) return
  menuVisible.value = false
  // 菜单期间气泡被 CSS 隐藏、fit 被跳过；关掉后等 DOM 恢复可见再重测一次，
  // 否则气泡可能保持 0 高度直到下一次 4 秒轮询。
  void nextTick(() => {
    if (!menuVisible.value && bubbleRef.value) void applyBubbleFit()
  })
}

/** 点在菜单外面关闭。 */
function onWindowPointerDownForMenu(event: MouseEvent): void {
  if (!menuVisible.value) return
  const menu = menuRef.value
  if (menu && event.target instanceof Node && menu.contains(event.target)) return
  closeMenu()
}

/** Escape 关闭：宠物窗口通常没有键盘焦点，故监听挂在 window 上而不是元素上。 */
function onWindowKeydownForMenu(event: KeyboardEvent): void {
  if (!menuVisible.value || event.key !== 'Escape') return
  event.preventDefault()
  closeMenu()
}

/**
 * 提交一次宠物设置改动。
 *
 * 【必须拼完整结构体】`SetPetSettings` 收的是整个 `PetSettings`；只传要改的字段，
 * 另外两个会被写成零值（`pet_scale:0` 回落默认、`pet_pack_id:""` 回落内置形象），
 * 表现为"换个形象顺手把缩放重置了"。本函数是唯一入口，
 * **禁止在菜单项里手写结构体字面量**。
 */
async function withPetSettings(patch: PetSettingsPatch): Promise<void> {
  const settings = await invoke(() => GetPetSettings(), 'GetPetSettings')
  await invoke(() => SetPetSettings({ ...settings, ...patch }), 'SetPetSettings')
}

/** 换形象：按 ListPetPacks 顺序切到下一个，到末尾回卷。 */
async function refreshMenuPacks(): Promise<void> {
  try {
    menuPacks.value = (await invoke(() => ListPetPacks(), 'ListPetPacks')) ?? []
    menuPacksFailed.value = false
  } catch (e) {
    // 取不到就按"没有可选项"处理：宁可置灰并说明，也不要让用户点了一下没反应。
    menuPacks.value = []
    menuPacksFailed.value = true
    logWarn('读取宠物素材包失败', { err: String(e) })
  }
}

/**
 * 切换形象。
 *
 * 【为什么先判 `length <= 1`】只有内置形象时，`(current+1) % 1` 还是它自己，
 * 切了等于没切。第一版在这种情况下静默 return——菜单关了、宠物没变、日志也没有一行，
 * 用户只能得出"这个功能坏了"。现在这条分支在菜单里就是禁用态（见 menuItemDisabled）。
 */
async function cyclePetPack(): Promise<void> {
  if (!canSwitchPack.value) await refreshMenuPacks()
  const packs = menuPacks.value
  if (packs.length <= 1) return
  // findIndex 为 -1 表示当前包已不在列表里（被删了/改了名）：从第一个开始，
  // 而不是"原地不动"——否则用户会以为换形象对他失效了。
  const current = packs.findIndex((pack) => pack.id === packID.value)
  const next = packs[(current + 1) % packs.length]
  if (!next || next.id === packID.value) return
  await withPetSettings({ pet_pack_id: next.id })
  await syncSettings()
}

/** 菜单项的补充说明：静态 hint 优先，其次按当前状态解释"为什么不能点"。 */
function menuItemHint(item: PetMenuItem): string | undefined {
  if (item.action === 'cyclePetPack' && !canSwitchPack.value) {
    return menuPacksFailed.value ? PET_MENU_PACK_LOAD_FAILED_HINT : PET_MENU_PACK_EMPTY_HINT
  }
  return item.hint
}

/** 菜单项是否禁用：动作在途一律禁用；「换形象」在只有一个素材包时禁用。 */
function menuItemDisabled(item: PetMenuItem): boolean {
  if (menuBusy.value) return true
  return item.action === 'cyclePetPack' && !canSwitchPack.value
}

/** 缩放：在 PET_MENU_SCALE_STEPS 里取下一个更大的档，到顶回到底档。 */
async function cyclePetScale(): Promise<void> {
  const next = PET_MENU_SCALE_STEPS.find((step) => step > scale.value + 0.001) ?? PET_MENU_SCALE_STEPS[0]
  if (next === undefined || Math.abs(next - scale.value) < 0.001) return
  await withPetSettings({ pet_scale: next })
  // 窗口尺寸由 Go 侧按新 scale 重算；这里同步一次本地值，免得下一次轮询前看着不匹配。
  await syncSettings()
}

/** 今天先这样：与气泡「更多」里的同一动作同源。 */
async function muteTodayFromMenu(): Promise<void> {
  await invoke(() => MuteSitToday(), 'MuteSitToday')
  await syncSitState()
}

/** 隐藏宠物：只改配置层的显隐，设置页可重新打开（那里会先还原贴边位置）。 */
async function hidePetFromMenu(): Promise<void> {
  await withPetSettings({ pet_visible: false })
}

/** 执行菜单项：分发 + 收尾。失败只记日志——菜单已关，没有地方显示错误。 */
async function runMenuItem(item: PetMenuItem): Promise<void> {
  if (menuItemDisabled(item)) return
  menuBusy.value = true
  try {
    switch (item.action) {
      case 'cyclePetPack':
        await cyclePetPack()
        break
      case 'cyclePetScale':
        await cyclePetScale()
        break
      case 'muteTodayFromMenu':
        await muteTodayFromMenu()
        break
      case 'openMainWindow':
        openMainWindow()
        break
      case 'hidePetFromMenu':
        await hidePetFromMenu()
        break
    }
  } catch (e) {
    logWarn('宠物菜单动作失败', { action: item.action, err: String(e) })
  } finally {
    menuBusy.value = false
    closeMenu()
  }
}

/**
 * 处理当前气泡里的提醒：完成 / 推迟。
 *
 * 推迟的分钟数由后端配置决定（设置页里那个），这里不传——两处各存一份默认值
 * 早晚会漂移成"设置页说 5 分钟、实际 10 分钟"。
 */
async function actCurrent(kind: 'done' | 'snooze'): Promise<void> {
  const fireID = currentFireID.value
  if (fireID === 0 || busy.value) return
  busy.value = true
  bubbleError.value = ''
  const before = pendingCount.value
  try {
    if (kind === 'done') {
      await invoke(() => CompleteReminder(fireID), 'CompleteReminder')
    } else {
      await invoke(() => SnoozeReminder(fireID), 'SnoozeReminder')
    }
    await refreshPending()
    // 完成最后一条时当场庆祝：这条路径是前端主动调的，后端不会再发 changed 事件
    // （changed 由提醒引擎在自身动作后发出），不在这里补一下就少了一次反馈。
    if (kind === 'done' && before > 0 && pendingCount.value === 0) {
      setAnim(PetAnim.Jumping, PET_CELEBRATE_MS)
    }
  } catch (e) {
    logWarn('处理提醒失败', { kind, fire_id: fireID, err: String(e) })
    // 失败先自愈：这条可能已被别处处理，重拉后气泡自然收起。
    await refreshPending()
    // 重拉后仍是同一条 → 是真实失败；给用户可见提示，而不是只写日志。
    if (currentFireID.value === fireID) {
      bubbleError.value = '操作失败，请再试一次'
    }
  } finally {
    busy.value = false
  }
}

/** 模板里的按钮回调：包一层，避免模板表达式里产生未处理的 Promise。 */
function onDone(): void {
  void actCurrent('done')
}

/** 同上：推迟。 */
function onSnooze(): void {
  void actCurrent('snooze')
}

/** 安排一次收起；鼠标仍在宠物上则跳过。 */
function scheduleCollapse(delayMs: number): void {
  if (collapseTimer !== undefined) {
    window.clearTimeout(collapseTimer)
  }
  collapseTimer = window.setTimeout(() => {
    collapseTimer = undefined
    if (hovering) return
    invoke(() => CollapsePet(), 'CollapsePet').catch((e) => {
      logWarn('收起宠物失败', e)
    })
  }, delayMs)
}

/** 鼠标进入：若正处于贴边收起态，展开回原位置；无论哪种情况都顺手唤醒。 */
function onPetEnter(): void {
  hovering = true
  if (collapseTimer !== undefined) {
    window.clearTimeout(collapseTimer)
    collapseTimer = undefined
  }
  // 鼠标靠近就是"人回来了"最直接的信号，比轮询发现暂停解除更早。
  wakeUp()
  if (dockSide.value === '') return
  // 宽限期：窗口移回原位后鼠标可能落在窗口外，会立刻收到一次 mouseleave。
  // 不设宽限期的话，宠物会在"展开→收起"之间快速抖动。
  expandGuardUntil = performance.now() + PET_EXPAND_GRACE_MS
  invoke(() => ExpandPet(), 'ExpandPet').catch((e) => {
    logWarn('展开宠物失败', e)
  })
}

/** 鼠标离开：延时收起（期间回来会取消）。 */
function onPetLeave(): void {
  hovering = false
  if (performance.now() < expandGuardUntil) return
  scheduleCollapse(PET_AUTO_COLLAPSE_MS)
}

// ---- 设置同步 ----

/** 拉取设置并应用；素材包变化时重新加载精灵图。 */
async function syncSettings(): Promise<void> {
  const settings = await invoke(() => GetPetSettings(), 'GetPetSettings')
  scale.value = settings.pet_scale
  if (settings.pet_pack_id !== packID.value) {
    packID.value = settings.pet_pack_id
    await loadSprite(packID.value)
  }
}

/** 轮询设置、生长档位、暂停状态与休息模式（失败只记一条 warn，下一轮再试）。 */
async function pollSettings(): Promise<void> {
  try {
    await syncSettings()
  } catch (e) {
    logWarn('同步宠物设置失败', { err: String(e) })
  }
  // 生长档位、暂停状态、休息模式并入同一次 4 秒轮询：兜住「次日翻篇」这类没有
  // 打卡事件的情况。休息模式的预警与倒计时都靠这个通道刷新。
  await syncGrowth()
  await syncInterruptionState()
  await syncRestModeState()
  await syncSitState()
  // 行为调度放在最后：门控依赖上面刚刷新的暂停/待处理/休息/久坐状态。
  // 两者都只做整数递减与一次分支判断，不额外发绑定调用。
  const nowMs = Date.now()
  tickDoze(nowMs)
  // 镜像先于微动作：镜像决定待机底态，微动作要看到它才能判"该不该叠"。
  tickMirror(nowMs)
  tickMicro(PET_SETTINGS_POLL_MS)
  // 4 秒一次的兜底：原生窗口显隐、引擎差异都可能让观察器漏事件。
  // 有气泡时重报一次；Go 侧对相同尺寸是幂等的，不会反复 resize。
  if (bubbleRef.value) void applyBubbleFit()
}

// ---- 生命周期 ----

function onFocus(): void {
  focused = true
}

function onBlur(): void {
  focused = false
  // 失焦也关菜单：宠物窗口失去焦点通常意味着用户去看别的窗口了，
  // 留着菜单会挡住宠物，而它已经不在用户的注意力里。
  closeMenu()
}

/** 系统「减弱动态效果」偏好在运行期间被切换。 */
function onReducedMotionChange(event: MediaQueryListEvent): void {
  prefersReducedMotion.value = event.matches
  // 中途打开时立刻掐掉正在演的微动作：等它演完再停，用户会以为这个开关没生效。
  if (event.matches) clearMicroAction()
}

onMounted(async () => {
  window.addEventListener('focus', onFocus)
  window.addEventListener('blur', onBlur)
  // 手势监听挂在 window 上而不是元素上：拖动中鼠标常常已经离开这个小窗口，
  // 挂在元素上的 mouseup 就永远等不到，手势状态会一直挂着。
  window.addEventListener('mouseup', onWindowPointerUp)
  window.addEventListener('blur', cancelPetPress)
  // mousemove 无条件挂载：它前半段要判「这一轮是不是拖动」，两种模式都用得上；
  // 搬窗口的后半段由函数内部按 PET_JS_DRAG_FALLBACK 自己挡掉。
  window.addEventListener('mousemove', onWindowPointerMove)
  // 菜单的"点外面关闭"与 Escape 都挂在 window 上：宠物窗口没有标题栏，
  // 焦点通常不在某个元素上，挂在元素上的 keydown 永远等不到。
  window.addEventListener('mousedown', onWindowPointerDownForMenu, true)
  window.addEventListener('keydown', onWindowKeydownForMenu)
  focused = document.hasFocus()

  // 减弱动态效果：既决定 CSS（媒体查询自己会生效），也决定 JS 调度要不要发车。
  motionQuery = window.matchMedia('(prefers-reduced-motion: reduce)')
  prefersReducedMotion.value = motionQuery.matches
  motionQuery.addEventListener('change', onReducedMotionChange)

  try {
    await syncSettings()
  } catch (e) {
    // 读不到设置不是致命错误：用内置形象 + 默认缩放先把宠物显示出来。
    mode.value = 'builtin'
    logWarn('读取宠物设置失败，使用内置形象', { err: String(e) })
  }

  await refreshPending()
  await syncGrowth()
  await syncSitState()
  logInfo('宠物窗口已就绪', { pack: packID.value, mode: mode.value, scale: scale.value })

  pollTimer = window.setInterval(() => {
    void pollSettings()
  }, PET_SETTINGS_POLL_MS)
})

onUnmounted(() => {
  window.removeEventListener('focus', onFocus)
  window.removeEventListener('blur', onBlur)
  window.removeEventListener('mouseup', onWindowPointerUp)
  window.removeEventListener('blur', cancelPetPress)
  window.removeEventListener('mousemove', onWindowPointerMove)
  window.removeEventListener('mousedown', onWindowPointerDownForMenu, true)
  window.removeEventListener('keydown', onWindowKeydownForMenu)
  cancelPetPress()
  stopLoop()
  motionQuery?.removeEventListener('change', onReducedMotionChange)
  motionQuery = undefined
  clearMicroAction()
  clearMirror()
  if (wakeTimer !== undefined) {
    window.clearTimeout(wakeTimer)
    wakeTimer = undefined
  }
  bubbleObserver?.disconnect()
  bubbleObserver = undefined
  sprite = null
  if (holdTimer !== undefined) {
    window.clearTimeout(holdTimer)
    holdTimer = undefined
  }
  if (pollTimer !== undefined) {
    window.clearInterval(pollTimer)
    pollTimer = undefined
  }
  // 收起计时器必须清掉：组件卸载后它还会去调绑定方法，那时窗口已经不存在了。
  if (collapseTimer !== undefined) {
    window.clearTimeout(collapseTimer)
    collapseTimer = undefined
  }
})
</script>

<template>
  <div
    class="pet"
    :class="[
      `pet--${anim}`,
      growthStageClass(growth),
      `pet--today-${todayMood}`,
      sitMoodClassValue,
      dockSide ? `pet--dock-${dockSide}` : '',
      microAction ? `pet--micro pet--micro-${microAction}` : '',
      mirrorClassValue,
      dozing ? 'pet--dozing' : '',
      waking ? 'pet--waking' : '',
      menuVisible ? 'pet--menu' : '',
    ]"
    :style="petRootStyle"
    @mouseenter="onPetEnter"
    @mouseleave="onPetLeave"
  >
    <!-- 休息模式陪做气泡（Countdown）：宠物与 Alert 覆盖层同步的锚点，用户
         不看覆盖层也能余光扫到还剩多久。优先级最高——此刻用户正在【做】这件事。 -->
    <div v-if="restAccompanying" ref="bubbleRef" class="bubble bubble--rest">
      <p class="bubble__title">{{ restAccompanyingText }}</p>
    </div>

    <!-- 休息模式预警气泡（Warning）：到点前 60 秒的软提示，不发系统通知。 -->
    <div v-else-if="restWarningVisible" ref="bubbleRef" class="bubble bubble--warning">
      <p class="bubble__title">{{ restWarningText }}</p>
    </div>

    <!-- 顺延气泡：用户正在输入时显示「等你停下」；有更高优先级气泡时让位。 -->
    <div v-else-if="deferringBubbleVisible" ref="bubbleRef" class="bubble bubble--pause">
      <p class="bubble__title">等你停下</p>
      <p class="bubble__text">你在输入，我稍后再提醒。</p>
    </div>

    <!-- 演示建议：会议/全屏持续 5 秒后出现；有待处理/休息时让位，避免盖住真正的提醒。 -->
    <div
      v-else-if="presentationSuggestionVisible"
      ref="bubbleRef"
      class="bubble bubble--suggestion"
    >
      <p class="bubble__title">在开会或共享屏幕？</p>
      <p class="bubble__text">进入演示模式，暂停提醒并隐藏桌宠。</p>
      <div class="bubble__actions">
        <button
          class="bubble__btn bubble__btn--primary"
          :disabled="presentationBusy"
          @click="enterPresentationFromSuggestion"
        >
          进入
        </button>
        <button
          class="bubble__btn"
          :disabled="presentationBusy"
          @click="dismissPresentationSuggestion"
        >
          不用了
        </button>
      </div>
    </div>

    <!-- 暂停气泡：智能免打扰期间显示「我先不打扰」。 -->
    <div v-else-if="pauseBubbleVisible" ref="bubbleRef" class="bubble bubble--pause">
      <p class="bubble__title">{{ pauseBubbleText }}</p>
    </div>

    <!-- 待处理气泡：只有没有更高优先级气泡时才出现。按钮直接作用于第一条提醒，
         让用户不必打开主窗口就能处理。
         【必须是这条链的最后一支】陪做 > 预警 > 顺延 > 演示建议（仅无待处理） >
         暂停 > 待处理：同一时刻只显示一个气泡。倒计时期间用户正在「做」，再叠一个
         完成/推迟只会抢注意力，也会让「从宠物提前完成」绕过休息模式的会话收尾。 -->
    <div v-else-if="bubbleVisible" ref="bubbleRef" class="bubble">
      <!-- 场景名先行：先回答"这是哪个计划"，再说这条提醒本身 -->
      <p v-if="bubbleScenario" class="bubble__scenario">{{ bubbleScenario }}</p>
      <p class="bubble__title">{{ bubbleHeading }}</p>
      <p v-if="bubbleText" class="bubble__text">{{ bubbleText }}</p>
      <p v-if="bubbleError" class="bubble__error" role="alert">{{ bubbleError }}</p>
      <div class="bubble__actions">
        <button class="bubble__btn bubble__btn--primary" :disabled="busy" @click="onDone">
          完成
        </button>
        <button class="bubble__btn" :disabled="busy" @click="onSnooze">推迟</button>
      </div>
      <span class="bubble__hint" @click="openMainWindow">打开客户端</span>
    </div>

    <!-- 久坐软提醒/欢迎气泡：排在所有既有气泡之后，最低优先级。
         动作只按后端给的 action ID 分发；更多菜单在气泡内展开，避免小窗裁切。 -->
    <div v-else-if="sitBubbleVisible && sitBubble" ref="bubbleRef" class="bubble bubble--sit">
      <p class="bubble__title">{{ sitBubble.title }}</p>
      <p v-if="sitBubble.text" class="bubble__text">{{ sitBubble.text }}</p>
      <div class="bubble__actions">
        <template v-for="action in sitMainActions" :key="action">
          <button
            class="bubble__btn"
            :class="{ 'bubble__btn--primary': action === ActionID.ActionStartBreak }"
            :disabled="sitBusy"
            @click="onSitAction(action)"
          >
            {{ sitActionLabel(action) }}
          </button>
        </template>
        <button
          v-if="sitHasMore"
          class="bubble__btn"
          :disabled="sitBusy"
          @click="onSitAction(ActionID.ActionMore)"
        >
          更多
        </button>
      </div>
      <div v-if="sitMoreOpen" class="bubble__actions bubble__actions--secondary">
        <button class="bubble__btn" :disabled="sitBusy" @click="onSitMuteToday">
          今天先这样
        </button>
        <button v-if="sitCanGentle" class="bubble__btn" :disabled="sitBusy" @click="onSitGentle">
          别这样说
        </button>
        <button class="bubble__btn" :disabled="sitBusy" @click="openMainWindow">
          打开客户端
        </button>
      </div>
    </div>

    <!-- 右键菜单：自绘在窗口内（不新增 Go 侧 API）。位置由光标决定并夹在窗口内，
         故不会被 240×380 的窗口裁掉；打开时由 .pet--menu 让气泡让位（数据不动）。
         --wails-draggable: no-drag 是必须的：--wails-draggable 会从 .pet 继承下来，
         不排除的话点菜单项会先启动窗口拖拽，click 被 suppressEvent 吞掉。 -->
    <div
      v-if="menuVisible"
      ref="menuRef"
      class="pet__menu"
      role="menu"
      :style="{
        left: `${menuPos.x}px`,
        top: `${menuPos.y}px`,
        width: `${PET_MENU_WIDTH}px`,
        maxWidth: `calc(100vw - ${PET_MENU_PADDING * 2}px)`,
        maxHeight: `calc(100vh - ${PET_MENU_PADDING * 2}px)`,
      }"
    >
      <button
        v-for="item in PET_MENU_ITEMS"
        :key="item.id"
        class="pet__menu-item"
        type="button"
        role="menuitem"
        :disabled="menuItemDisabled(item)"
        @click="runMenuItem(item)"
      >
        <span class="pet__menu-label">{{ item.label }}</span>
        <span v-if="menuItemHint(item)" class="pet__menu-hint">{{ menuItemHint(item) }}</span>
      </button>
    </div>

    <!-- 宠物本体：整块是拖拽区（窗口无边框，拖它就是移动窗口的方式）。
         用 mousedown 而不是 click 起手势：click 与拖拽共用同一串鼠标事件，
         谁先谁后由 wails 的拖拽逻辑决定，靠它判「点击」是不可靠的。
         拖拽属性走内联样式，兜底模式下会被切成 no-drag（见 artStyleWithDrag）。
         右键单独走 contextmenu：拖拽只由主键 arm，右键不会启动拖拽（见 onPetContextMenu）。 -->
    <div
      class="pet__art"
      ref="petArtRef"
      :style="artStyleWithDrag"
      @mousedown="onPetPointerDown"
      @contextmenu.prevent="onPetContextMenu"
    >
      <canvas v-if="mode === 'sprite'" ref="canvasRef" class="pet__canvas" />

      <!-- 内置形象「小满」：一滴水 + 头顶一株芽，纯程序化绘制（呼吸/眨眼/摇芽全靠 CSS），
           不依赖任何素材——新用户开箱就有宠物。设定与比例见 docs/pet-character.md。
           画板 192×208 与图集单帧一致，故切到 sprite 模式时尺寸不跳。 -->
      <svg v-else class="pet__svg" viewBox="0 0 192 208" role="img" aria-label="桌面宠物">
        <!-- 落地影：给一个「站在桌面上」的落点，浅色壁纸上不至于浮空 -->
        <ellipse class="pet__shadow" cx="96" cy="194" rx="46" ry="7" />

        <g class="pet__body-group">
          <!-- 手臂画在身体【之前】：关节被身体盖住，只露出袖口那一截。
               于是挥手时看不到接缝，动画里也不必处理关节连接。 -->
          <g class="pet__arm pet__arm--left"><line x1="50" y1="150" x2="36" y2="160" /></g>
          <g class="pet__arm pet__arm--right"><line x1="142" y1="150" x2="156" y2="160" /></g>

          <!-- 身体：水滴。这个轮廓是全图唯一必须在 96px（0.5 倍缩放）下仍然成立的东西 -->
          <path class="pet__body" d="M96 40C118 68 154 98 154 132A58 58 0 0 1 38 132C38 98 74 68 96 40Z" />
          <!-- 高光：水滴的「水感」全靠它，去掉就成了一颗普通的蛋 -->
          <ellipse class="pet__sheen" cx="72" cy="96" rx="11" ry="19" transform="rotate(-20 72 96)" />

          <ellipse class="pet__cheek" cx="58" cy="132" rx="11" ry="6.5" />
          <ellipse class="pet__cheek" cx="134" cy="132" rx="11" ry="6.5" />

          <!-- 两套眼睛：睁眼（负责眨眼）与弯眼（负责笑）。
               切透明度而不是加条件渲染——换形状要动 Vue 层的结构，只为一次眨眼不值得。 -->
          <g class="pet__eyes">
            <g class="pet__eyes-open">
              <ellipse class="pet__eye" cx="74" cy="116" rx="8.5" ry="10.5" />
              <ellipse class="pet__eye" cx="118" cy="116" rx="8.5" ry="10.5" />
            </g>
            <g class="pet__eyes-happy">
              <path d="M63 118q11-13 22 0" />
              <path d="M107 118q11-13 22 0" />
            </g>
          </g>

          <path class="pet__mouth" d="M88 140q8 8 16 0" />

          <!-- 芽：情绪指示器。它在头顶，比脸更早被余光看到——
               「等待」与「庆祝」的差别主要靠它，因为脸的表情要 192px 以上才看得清。 -->
          <g class="pet__sprout">
            <!-- 生长档位的静态形态层：整体 scale 与叶 rotate 作用在这一层，
                 与动态层 .pet__sprout（sway/alert/pop 的 rotate）是不同元素，互不覆盖。 -->
            <g class="pet__sprout-body">
              <!-- 情绪层：久坐担心/耷拉。与生长档位（父层 scale）分开，
                   避免两个 transform 互相覆盖。 -->
              <g class="pet__sprout-mood">
                <path class="pet__stem" d="M96 46V20" />
                <path class="pet__leaf pet__leaf--left" d="M96 30C87 29 74 22 72 12 84 11 94 19 96 30Z" />
                <path class="pet__leaf pet__leaf--right" d="M96 24C105 23 118 16 120 8 108 7 98 14 96 24Z" />
              </g>
            </g>
          </g>
        </g>
      </svg>
    </div>
  </div>
</template>

<style scoped>
/* 宠物窗口必须全透明——三层都要覆盖，缺一层就是一块底色方块。
   - html：WebKit 的 canvas 默认底色来自它，只覆盖 body 不够；
   - body：styles/index.css 给它设了 background-color: var(--bg-base)；
   - #app：Vue 挂载点，将来任一处给它加背景都会让整窗变成卡片。
   用 :global 是因为这些节点在组件外；!important 是为了不依赖样式插入顺序
   （开发态与打包态的顺序并不相同，靠特异性赌顺序是不可靠的）。
   注意：CSS 只解决"网页这一层"，macOS 上真正的透明还需要
   Mac.Backdrop=MacBackdropTransparent + private_mac_apis 构建标签（见 pet_window.go）。 */
:global(html),
:global(body),
:global(#app) {
  background: transparent !important;
  overflow: hidden;
}

.pet {
  position: relative;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: flex-end;
  gap: var(--space-2);
  height: 100vh;
  padding: var(--space-2);
  background: transparent;
  color: var(--text-1);
  overflow: hidden;
  user-select: none;
  /* 窗口没有标题栏，整窗就是拖拽区：这条负责窗口的空白/内边距部分，
     宠物本体由 .pet__art 自己显式声明（原因见那条长注释） */
  --wails-draggable: drag;
}

/* ---- 气泡 ---- */
.bubble {
  position: relative;
  /* 固定气泡的自然高度：默认 flex-shrink 会在窗口不够高时把气泡压扁，
     使 offsetHeight 量不到真实需求。 */
  flex: none;
  max-width: 100%;
  padding: var(--space-2) var(--space-3);
  background: var(--bg-raised);
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-md);
  cursor: pointer;
  /* 气泡是点击区，必须显式排除拖拽：--wails-draggable 会从 .pet 继承下来，
     不覆盖的话点它只会拖动窗口，OpenMainWindow 永远等不到点击。 */
  --wails-draggable: no-drag;
}

/* 暂停气泡：使用次要色调，表达「我不打扰你」的温和感。 */
.bubble--pause {
  background: var(--bg-subtle);
  border-color: var(--border-subtle);
  opacity: 0.85;
}

/* 预警气泡（Warning）：稍亮的边框但不抢主色——是「提醒」而不是「指令」。 */
.bubble--warning {
  background: var(--bg-raised);
  border-color: var(--color-primary-300);
}

/* 演示建议气泡：比暂停气泡更主动，但仍用次要色调，不抢休息/待处理。 */
.bubble--suggestion {
  background: var(--color-primary-soft);
  border-color: var(--color-primary-300);
}

.bubble--suggestion::after {
  background: var(--color-primary-soft);
}

/* 久坐软提醒/欢迎气泡：最低优先级，仍用品牌软色，避免像错误提示。 */
.bubble--sit {
  background: var(--color-primary-soft);
  border-color: var(--color-primary-300);
}

.bubble--sit::after {
  background: var(--color-primary-soft);
  border-color: var(--color-primary-300);
}

/* 「更多」第二行：三个入口在 240px 宽气泡里换行，避免把按钮压成一条线。 */
.bubble__actions--secondary {
  flex-wrap: wrap;
}

.bubble__actions--secondary .bubble__btn {
  flex: 1 1 30%;
}

/* 陪做气泡（Countdown）：与休息模式覆盖层同色系（品牌主色），文字用 tabular-nums
   避免 mm:ss 跳动时视觉位移。 */
.bubble--rest {
  background: var(--color-primary-soft);
  border-color: var(--color-primary);
}

.bubble--rest .bubble__title {
  font-variant-numeric: tabular-nums;
}

/* 气泡尾巴：旋转 45° 的小方块，只留右下两条边，看起来像从气泡长出来的 */
.bubble::after {
  content: '';
  position: absolute;
  left: 50%;
  bottom: -6px;
  width: 10px;
  height: 10px;
  margin-left: -5px;
  background: var(--bg-raised);
  border-right: 1px solid var(--border-subtle);
  border-bottom: 1px solid var(--border-subtle);
  transform: rotate(45deg);
}

/* 场景名：气泡顶部的归属标签，回答"这是哪个计划"。
   比标题小一档并用品牌色，读起来是"标签"而不是第二行标题；
   单行 + 省略号——场景名再长也不该把气泡撑成四行。
   【高度预算】12px 字号 × 1.4 行高 ≈ 17px，加上与标题的 2px 间距 ≈ 20px，
   这正是宠物窗口高度从 360 提到 380 的原因（见 pet_window.go 的 petBaseHeight）。 */
.bubble__scenario {
  margin: 0;
  overflow: hidden;
  color: var(--color-primary);
  font-size: var(--font-size-xs);
  font-weight: 600;
  line-height: 1.4;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* 有场景名时标题下移 2px：这是气泡从 3 行变 4 行的唯一额外间距，
   不要顺手把它加到 .bubble__title 上——没场景名时标题会白白多出一截上边距。 */
.bubble__scenario + .bubble__title {
  margin-top: 2px;
}

.bubble__title {
  margin: 0;
  font-size: var(--font-size-sm);
  font-weight: 600;
  line-height: 1.4;
}

.bubble__text {
  margin: var(--space-1) 0 0;
  max-height: 2.8em;
  overflow: hidden;
  color: var(--text-2);
  font-size: var(--font-size-xs);
  line-height: 1.4;
}

.bubble__hint {
  display: block;
  margin-top: var(--space-1);
  color: var(--color-primary);
  font-size: var(--font-size-xs);
}

/* 失败提示：把「点了没反应」变成可见事实。只在这一条仍在待处理时出现；
   已被别处处理的场景由 applyPending 清掉并收起气泡。 */
.bubble__error {
  margin: var(--space-1) 0 0;
  color: var(--color-danger);
  font-size: var(--font-size-xs);
  line-height: 1.4;
}

/* ---- 气泡里的操作按钮 ----
   让用户不必打开主窗口就能处理当前这条提醒。 */
.bubble__actions {
  display: flex;
  gap: var(--space-1);
  margin-top: var(--space-2);
}

.bubble__btn {
  flex: 1;
  padding: 3px 0;
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-2);
  font-size: var(--font-size-xs);
  cursor: pointer;
  /* 按钮是点击区，必须显式排除拖拽（--wails-draggable 会从 .pet 继承下来），
     否则点「完成」只会拖动窗口。 */
  --wails-draggable: no-drag;
}

.bubble__btn:hover:not(:disabled) {
  background: var(--bg-hover);
  color: var(--text-1);
}

.bubble__btn:disabled {
  opacity: 0.5;
  cursor: default;
}

.bubble__btn--primary {
  border-color: var(--color-primary);
  color: var(--color-primary);
}

.bubble__btn--primary:hover:not(:disabled) {
  background: var(--color-primary-soft);
}

/* ---- 宠物本体 ----
   【为什么这里要显式写 --wails-draggable: drag，而不是靠从 .pet 继承】
   拖拽判定在 wails 的注入脚本里（v3.0.0-beta.23 的
   internal/runtime/desktop/@wailsio/runtime/src/drag.ts），核心是：

     function isDraggableEvent(event) {
       const target = eventTarget(event);              // 见下方 utils.ts
       const style  = window.getComputedStyle(target);
       return style.getPropertyValue('--wails-draggable').trim() === 'drag'
         && event.offsetX >= 0 && event.offsetX < target.clientWidth   // ← 成败在这
         && event.offsetY >= 0 && event.offsetY < target.clientHeight;
     }

     // utils.ts：target 不是 HTMLElement 时【只向上一层】取 parentElement
     function eventTarget(event) {
       if (event.target instanceof HTMLElement) return event.target;
       else if (event.target instanceof Node) return event.target.parentElement ?? document.body;
       return document.body;
     }

   自定义属性会继承，所以「写在 .pet 上让子元素继承」本身没错——值确实传到了。
   真正卡住的是后半段尺寸判断：光标压在宠物身上时，最内层命中的是 SVG 的
   <circle>/<ellipse>/<path>，它们不是 HTMLElement，eventTarget() 只上溯一层，
   拿到的仍是 SVG 的 <g>；而 SVG 的几何元素【没有 CSS 布局盒】，
   clientWidth / clientHeight 恒为 0，于是 `offsetX < 0` 为假，整条判定返回 false。

   「SVG 元素的 clientWidth 恒为 0」是查 WebKit 源码确认的，不是推断——
   Source/WebCore/dom/Element.cpp 的 clientWidth() / clientHeight() 里
   没有任何 SVG 分支，只有：

     if (CheckedPtr renderer = renderBox()) {
         return convertToNonSubpixelValue(...renderer->paddingBoxWidth()...);
     }
     return 0;   // ← <circle>/<g> 渲染成 RenderSVGShape/RenderSVGContainer，
                 //   都不是 RenderBox，renderBox() 为空，直接走到这里

   【顺带说明】另有一种流传的解释是「WebKit 上 SVG 元素读不到继承来的自定义属性」。
   真正卡住的是上面那条尺寸判断，但无论哪种解释成立，修法都一样：让命中点不再是
   SVG 元素。本修法对两种解释同时成立，所以不需要先判定到底是哪一个。

   症状因此非常典型：拖窗口的空白处能动（命中 .pet 这个 div，有尺寸），
   一压到宠物本体就拖不动——与用户报告的现象完全一致。
   （sprite 模式命中 <canvas>，它是 HTMLElement、有尺寸，所以那种模式下本来是能拖的。）

   修法：让 SVG / canvas 不吃指针事件，命中点自然落到 .pet__art 这个有真实宽高的
   div 上，三条判定全部成立。显式声明 drag 是第二重保险：万一将来某个层把继承值
   覆盖成空，这里仍有一份自己的声明。 */
.pet__art {
  position: relative;
  flex: none;
  display: flex;
  align-items: flex-end;
  justify-content: center;
  cursor: grab;
  /* 窗口没有标题栏，宠物本体就是拖动窗口的把手。
     【注意这里不是唯一来源】模板上还有一条内联的 --wails-draggable
     （见 artStyleWithDrag）：兜底模式会把它切成 no-drag。内联样式优先级更高，
     会盖住这条声明。这里保留的意义是「默认值 + 文档」。 */
  --wails-draggable: drag;
}

.pet__art:active {
  cursor: grabbing;
}

.pet__canvas {
  width: 100%;
  height: 100%;
  /* 像素风素材：放大时保持硬边 */
  image-rendering: pixelated;
  /* 不吃命中测试：让拖拽判定落在 .pet__art 上（原因见上方长注释） */
  pointer-events: none;
}

.pet__svg {
  width: 100%;
  height: 100%;
  transform-origin: 50% 100%;
  animation: pet-breathe 3.4s var(--ease-in-out) infinite;
  /* 不吃命中测试：SVG 几何元素没有布局盒，clientWidth 为 0，会让拖拽判定失败
     （原因见上方长注释） */
  pointer-events: none;
}

/* 等待处理时呼吸更快一档：不用额外素材也能表达"我在等你" */
.pet--waiting .pet__svg {
  animation-duration: 1.5s;
}

.pet--waving .pet__svg {
  animation: pet-wave 450ms var(--ease-in-out) 2;
}

.pet--jumping .pet__svg {
  animation: pet-jump 520ms var(--ease-in-out) infinite;
}

/* ---- 内置形象「小满」----
   配色全部走设计令牌，不写死色值（前端护栏会拦硬编码 hex）。
   换肤只改 --color-primary，角色跟着变——这也是它不引入专属色的原因。 */
.pet__shadow {
  fill: var(--color-primary-900);
  opacity: 0.18;
}

.pet__body {
  fill: var(--color-primary);
}

/* 高光：水滴的水感来源。透明度别超过 0.65，再高就像贴了块胶带 */
.pet__sheen {
  fill: var(--color-primary-50);
  opacity: 0.55;
}

.pet__cheek {
  fill: var(--color-primary-300);
  opacity: 0.7;
}

.pet__eye {
  fill: var(--color-primary-900);
}

.pet__mouth {
  fill: none;
  stroke: var(--color-primary-900);
  stroke-width: 3.5;
  stroke-linecap: round;
}

/* 手臂：用圆头粗线段画，关节藏在身体里。
   transform-origin 用百分比配 fill-box，不写 px——px 会依赖 transform-box 的
   初始值（各引擎历史上不一致），而百分比相对元素自身包围盒是确定的。 */
.pet__arm line {
  stroke: var(--color-primary);
  stroke-width: 16;
  stroke-linecap: round;
}

.pet__arm--right {
  transform-box: fill-box;
  transform-origin: 0% 0%;
}

/* 芽：绿色取自语义令牌 --color-success（健康 / 成长），不是新加的颜色。
   将来若要做「久坐太久 → 芽耷拉」，只改这一个类即可。 */
.pet__stem {
  fill: none;
  stroke: var(--color-success);
  stroke-width: 5;
  stroke-linecap: round;
}

.pet__leaf {
  fill: var(--color-success);
  transform-box: fill-box;
  transition: transform 300ms var(--ease-in-out);
}

/* 左叶旋转原点在「叶与茎的接点」(96,30)：fill-box 下即右下角。 */
.pet__leaf--left {
  transform-origin: 100% 100%;
}

/* 右叶旋转原点在「叶与茎的接点」(96,24)：fill-box 下即左下角。 */
.pet__leaf--right {
  transform-origin: 0% 100%;
}

.pet__sprout {
  transform-box: fill-box;
  transform-origin: 50% 100%;
  animation: pet-sprout-sway 3.8s var(--ease-in-out) infinite;
}

/* 生长档位的静态形态层：整体 scale 以茎基 (96,46) 为原点向上生长。
   与 .pet__sprout 的动态动画分属父子两个元素，父子 transform 自然叠加、互不覆盖。 */
.pet__sprout-body {
  transform-box: fill-box;
  transform-origin: 50% 100%;
  transition: transform 300ms var(--ease-in-out);
}

/* 久坐情绪层：在生长档位内部再做旋转/压扁，避免覆盖生长 scale。
   芽是头顶的情绪指示器；担心时轻微偏，耷拉时明显低头。 */
.pet__sprout-mood {
  transform-box: fill-box;
  transform-origin: 50% 100%;
  transition: transform 300ms var(--ease-in-out);
}

.pet--sit-concerned .pet__sprout-mood {
  transform: rotate(7deg) scaleY(0.96);
}

.pet--sit-droopy .pet__sprout-mood {
  transform: rotate(15deg) scaleY(0.84);
}

/* ---- 芽的形态：五档 ----
   这里的 `.pet--growth-N` 由【形态档】驱动，形态档 = max(长期档, 今日档)，
   其中长期档来自「累计完成天数」——**不是**今日完成率。
   今日完成率只驱动 `.pet--today-*` 那组神态类（高光/轻垂），永远不碰 scale。
   （这一行原本写的是"芽随今日完成率生长"，跨天归零的 bug 就是那个口径造成的。）
   「叶展开角」负=内收、正=展开；左右两叶互为镜像：
   左叶正角=内收/负角=展开，右叶相反。整体 scale 见下表。
   | 档位 | scale | 叶展开角 | 左叶 rotate | 右叶 rotate |
   | 0    | 0.55  | -6°      | +6deg       | -6deg       |
   | 1    | 0.70  | 0°       | 0           | 0           |
   | 2    | 0.85  | +8°      | -8deg       | +8deg       |
   | 3    | 1.0   | +14°     | -14deg      | +14deg      |
   | 4    | 1.1   | +20°     | -20deg      | +20deg      |
   颜色继续用 --color-success（禁硬编码 hex）。 */
.pet--growth-0 .pet__sprout-body {
  transform: scale(0.55);
}
.pet--growth-1 .pet__sprout-body {
  transform: scale(0.7);
}
.pet--growth-2 .pet__sprout-body {
  transform: scale(0.85);
}
.pet--growth-3 .pet__sprout-body {
  transform: scale(1);
}
.pet--growth-4 .pet__sprout-body {
  transform: scale(1.1);
}

.pet--growth-0 .pet__leaf--left {
  --leaf-left-angle: 6deg;
  transform: rotate(var(--leaf-left-angle));
}
.pet--growth-0 .pet__leaf--right {
  transform: rotate(-6deg);
}
.pet--growth-1 .pet__leaf--left {
  --leaf-left-angle: 0deg;
  transform: rotate(var(--leaf-left-angle));
}
.pet--growth-1 .pet__leaf--right {
  transform: rotate(0deg);
}
.pet--growth-2 .pet__leaf--left {
  --leaf-left-angle: -8deg;
  transform: rotate(var(--leaf-left-angle));
}
.pet--growth-2 .pet__leaf--right {
  transform: rotate(8deg);
}
.pet--growth-3 .pet__leaf--left {
  --leaf-left-angle: -14deg;
  transform: rotate(var(--leaf-left-angle));
}
.pet--growth-3 .pet__leaf--right {
  transform: rotate(14deg);
}
.pet--growth-4 .pet__leaf--left {
  --leaf-left-angle: -20deg;
  transform: rotate(var(--leaf-left-angle));
}
.pet--growth-4 .pet__leaf--right {
  transform: rotate(20deg);
}

/* 眨眼只作用于睁眼那一套：给弯眼加眨运动会得到一条抽搐的线 */
.pet__eyes-open {
  transform-box: fill-box;
  transform-origin: center;
  animation: pet-blink 4.6s steps(1, end) infinite;
}

.pet__eyes-happy {
  opacity: 0;
}

.pet__eyes-happy path {
  fill: none;
  stroke: var(--color-primary-900);
  stroke-width: 4.5;
  stroke-linecap: round;
}

/* 笑眼在「挥手」与「庆祝」时顶替睁眼。两套都在 DOM 里，切透明度即可 */
.pet--waving .pet__eyes-open,
.pet--jumping .pet__eyes-open {
  opacity: 0;
}

.pet--waving .pet__eyes-happy,
.pet--jumping .pet__eyes-happy {
  opacity: 1;
}

/* 挥手：手臂绕肩部上抬。两轮 450ms = PET_WAVE_MS 的 900ms */
.pet--waving .pet__arm--right {
  animation: pet-arm-wave 450ms var(--ease-in-out) 2;
}

/* 等待处理：芽立起并小幅抖动。它比「呼吸变快」更容易被余光捕捉到 */
.pet--waiting .pet__sprout {
  animation: pet-sprout-alert 800ms var(--ease-in-out) infinite;
}

/* 庆祝：芽跟着整身一起弹 */
.pet--jumping .pet__sprout {
  animation: pet-sprout-pop 520ms var(--ease-in-out) infinite;
}

/* ---- 今日神态：只改「脸色」，不改尺寸 ----
   与形态档（.pet--growth-*）刻意分开：形态是「养了多久」，神态是「今天怎么样」。
   今日档永远不碰 scale——一旦今日影响尺寸，就又会变成「每天早上缩小一次」。
   久坐情绪优先于今日神态，故用 :not 把它让开：不靠书写顺序取胜，改文件顺序也不会错。 */
.pet--today-full .pet__sheen {
  opacity: 0.62;
}

.pet--today-part .pet__sheen {
  opacity: 0.5;
}

.pet--today-thin .pet__sheen {
  opacity: 0.34;
}

/* 优先级阶梯（高 → 低）：久坐情绪 / 打盹 / 惊醒 > 今日神态。
   这里把四个"更高优先"的状态显式 :not 掉，而不是靠书写顺序——
   这条选择器带四个类，权重 (0,4,0)，比 .pet--dozing .pet__sprout-mood (0,2,0) 高，
   不让开的话打盹时芽会保持"今天没做完"的耷拉角度，看起来像没睡着。 */
.pet--today-thin:not(.pet--sit-concerned):not(.pet--sit-droopy):not(.pet--dozing):not(.pet--waking)
  .pet__sprout-mood {
  transform: rotate(5deg) scaleY(0.94);
}

/* ---- 待机微动作 ----
   三条约定：
     1. 都是 1.2 秒以内的一次性动作，由 class 驱动、到时摘掉，不循环；
     2. 动作期间**暂停**呼吸（animation-play-state），不是删掉——删掉会让呼吸在
        微动作结束时从头开始，看起来像卡了一下；
     3. 只用既有元素的既有坐标系，不新造 transform-origin。 */
.pet--micro .pet__svg {
  animation-play-state: paused;
}

.pet--micro-look .pet__eyes {
  animation: pet-micro-look var(--pet-micro-ms, 1200ms) var(--ease-in-out);
}

.pet--micro-stretch .pet__body-group {
  transform-box: fill-box;
  transform-origin: 50% 100%;
  animation: pet-micro-stretch var(--pet-micro-ms, 1200ms) var(--ease-in-out);
}

.pet--micro-groom .pet__leaf--left {
  animation: pet-micro-groom var(--pet-micro-ms, 1200ms) var(--ease-in-out);
}

.pet--micro-yawn .pet__mouth {
  transform-box: fill-box;
  transform-origin: 50% 50%;
  animation: pet-micro-yawn var(--pet-micro-ms, 1200ms) var(--ease-in-out);
}

/* 打哈欠要闭眼：与眨眼动画互斥，故直接停掉再压扁，不让两个动画抢 transform */
.pet--micro-yawn .pet__eyes-open {
  animation: none;
  transform: scaleY(0.12);
}

/* ---- 镜像姿态 ----
   宠物跟着「你在做什么」改变体态（openspec/changes/pet-activity-mirror）。
   三条约定：
     1. 只改 transform，不引入任何新颜色；
     2. **幅度按「96px 高的宠物」校准**：`pet-character.md` 明确"脸的表情要 192px
        以上才看得清"，反过来说 96px 下只有【轮廓级】变化能读到。第一版用 2–3px 位移与
        1.5° 旋转，实测在 scale=0.5（96px）下等于没做——所以这里的量级是 6–7px 与 3–5°；
     3. 与微动作共存：微动作是 CSS animation，在层叠里赢过这里的静态 transform，
        演完自动回到姿态。不要为了"防打架"去关掉微动作（那等于整天不让宠物动）。
   sprite 模式的姿态由换图集行表达（PET_MIRROR_ROW），不挂这些 class。 */
.pet--mirror-working .pet__body-group,
.pet--mirror-listening .pet__body-group,
.pet--mirror-watching .pet__body-group {
  transform-box: fill-box;
  transform-origin: 50% 100%;
}

/* 一起埋头：整体坐低 10px 并缩到 93%，向左压 4°。
   【为什么是"坐低 + 缩小"而不是"前倾"】画板 192×208 里，芽尖在 y≈7、阴影在 y≈194——
   **上方几乎没有余量**（上抬超过 6px 就裁切），所以"抬头/站高"这类姿态在这套素材上做不出来；
   能读出来的只有向下与缩放。缩小 7% 在 96px 高时约 7px，是真正能被余光捕捉的量级。 */
.pet--mirror-working .pet__body-group {
  transform: translateY(10px) rotate(-4deg) scale(0.93);
}

/* working 的手臂摆动。`:not(.pet--waving)` 是必须的：挥手动的也是 `.pet__arm--right`，
   而镜像规则写在本文件更后面——不让位的话同优先级下镜像赢，点了不挥手且不报错。 */
.pet--mirror-working:not(.pet--waving) .pet__arm--right,
.pet--mirror-working:not(.pet--waving) .pet__arm--left {
  transform-box: fill-box;
  transform-origin: 50% 0%;
  animation: pet-mirror-typing var(--pet-mirror-shake-ms, 1600ms) var(--ease-in-out) infinite;
}

/* 你说的我听着：整体向我这边倾 9° 并侧移 6px + 右手绕肩抬起 45°。
   【为什么角度给到 9°】6° 的倾斜在这张水滴上几乎不改包围盒（实测宽 +1%、高 −1%），
   96px 下等于没做；9° 才会把芽明显甩向一侧，轮廓才变。
   【为什么是绕肩旋转而不是平移手臂】手臂是两段短 line、关节被身体盖住；
   平移会把整条袖子挪进身体里（视觉上"手没了"），绕肩旋转才会把袖子露到肩上方。 */
.pet--mirror-listening .pet__body-group {
  transform: translateX(-6px) rotate(9deg);
}

/* 抬手的右手同样要在挥手时让位（同上：`.pet--waving` 的规则写在前面，同优先级会输）。 */
.pet--mirror-listening:not(.pet--waving) .pet__arm--right {
  transform-box: fill-box;
  transform-origin: 0% 0%;
  transform: rotate(-45deg);
}

/* 一起看：横向放大 10%、纵向压扁 7%——「趴下来一起看」。
   【为什么不整体放大】画板顶部余量只有 5 个单位（芽尖 y≈7）：整体 scale(1.04)
   实测把芽尖顶到距画板边缘 2 个设备像素，再高一点就裁切。
   **宽高反向变化**（变宽 + 变扁）比单纯放大更容易被读到：长宽比从 0.75 变到 0.87，
   是"形状变了"而不是"大小变了"，后者会被误认为缩放设置或呼吸。
   scale 挂在父组 .pet__eyes 上，不碰负责眨眼的 .pet__eyes-open——两个动画抢 transform 会让眨眼失效。 */
.pet--mirror-watching .pet__body-group {
  transform: scaleX(1.1) scaleY(0.93);
}

.pet--mirror-watching .pet__eyes {
  transform-box: fill-box;
  transform-origin: 50% 50%;
  transform: scaleY(1.15);
}

/* ---- 打盹与惊醒 ----
   打盹是「用户不在」的形态：闭眼、芽收拢、呼吸放慢到 6.4 秒一轮。
   惊醒是事件反馈：只放大眼睛 + 芽弹一下，300ms 结束（PET_WAKE_MS）。 */
.pet--dozing .pet__eyes-open {
  animation: none;
  transform: scaleY(0.1);
}

.pet--dozing .pet__sprout {
  animation: none;
}

.pet--dozing .pet__sprout-mood {
  transform: rotate(8deg) scaleY(0.78);
}

.pet--dozing .pet__svg {
  animation-duration: var(--pet-doze-breath-ms, 6.4s);
}

.pet--waking .pet__eyes-open {
  animation: none;
  transform: scaleY(1.5);
  transition: transform 120ms var(--ease-in-out);
}

.pet--waking .pet__sprout-mood {
  transform: translateY(-3px) scaleY(1.1);
  transition: transform 120ms var(--ease-in-out);
}

/* ---- 右键菜单 ----
   自绘而非原生：菜单只有 5 项，窗口上方留白够放，且这样做 Go 侧一行都不用改。
   【为什么是 fixed 而不是 absolute】`.pet` 有内边距与 overflow:hidden：
   absolute 的 left/top 以 padding 盒为基准，会和光标差一个 padding 的距离；
   fixed 直接对齐视口坐标，和 event.clientX/Y 同源。且 fixed 不受祖先 overflow 裁剪
   （本页没有任何祖先带 transform，这个前提成立才成立）。
   位置仍由 JS 夹在窗口内（见 onPetContextMenu）；宽度上限也跟着视口走
   （模板内联 maxWidth），小缩放窗口下不会横向溢出。
   高度随字体与说明行变化，不写死：maxHeight 由模板内联，超出时菜单内部滚动。 */
.pet__menu {
  position: fixed;
  z-index: 10;
  /* 宽度不在这里写：由模板内联成 PET_MENU_WIDTH，保持"一个数字一处定义"。
     高度也不写：它随字体与"是否有第二行说明"变化，靠 JS 渲染后实测（见 onPetContextMenu）。 */
  padding: var(--pet-menu-padding);
  overflow-y: auto;
  overscroll-behavior: contain;
  border-radius: var(--radius-sm);
  border: 1px solid var(--border-subtle);
  background: var(--bg-raised);
  box-shadow: var(--shadow-md);
  /* 菜单是点击区：--wails-draggable 从 .pet 继承下来，不排除就会拖窗口 */
  --wails-draggable: no-drag;
}

.pet__menu-item {
  display: flex;
  flex: none;
  flex-direction: column;
  gap: 1px;
  width: 100%;
  padding: 6px var(--space-2);
  border: 0;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-1);
  font-size: var(--font-size-sm);
  text-align: left;
  cursor: pointer;
  --wails-draggable: no-drag;
}

.pet__menu-item:hover:not(:disabled) {
  background: var(--bg-hover);
}

.pet__menu-item:disabled {
  opacity: 0.6;
  cursor: default;
}

/* 说明行：字号更小、颜色更淡——它是补充，不该和菜单项标题抢注意力 */
.pet__menu-hint {
  color: var(--text-2);
  font-size: var(--font-size-xs);
}

/* 菜单打开时气泡让位：两者都在同一块 240×380 里，叠加会互相遮住。
   数据不动——只在菜单可见期间不渲染气泡，关掉菜单立刻恢复。 */
.pet--menu .bubble {
  display: none;
}

/* 尊重系统「减弱动态效果」。
   只停【常驻循环】，不停【事件触发的瞬时反馈】：
   前者是这类用户真正想关掉的东西（一个 24 小时在呼吸的小窗），
   后者是"我处理完了"的唯一视觉回执，停掉就只剩文字了。
   注意 specificity：下面这几条都低于 .pet--waving / .pet--jumping 的规则，
   所以挥手与庆祝照常播——这是刻意的，不是漏写。 */
@media (prefers-reduced-motion: reduce) {
  .pet__svg,
  .pet__sprout,
  .pet__eyes-open,
  .pet--waiting .pet__sprout {
    animation: none;
  }

  /* 情绪是静态状态，不是事件反馈；减弱动态时直接切换，不做过渡。 */
  .pet__sprout-mood {
    transition: none;
  }

  /* 镜像的姿态本身就是静态信息（前倾/托腮/注视），要保留；
     停掉的只是"手在摆动"这个动画部分。 */
  .pet--mirror-working .pet__arm--right,
  .pet--mirror-working .pet__arm--left {
    animation: none;
  }
}

@keyframes pet-breathe {
  0%,
  100% {
    transform: scale(1, 1);
  }
  50% {
    transform: scale(1.03, 0.97);
  }
}

@keyframes pet-blink {
  0%,
  92%,
  100% {
    transform: scaleY(1);
  }
  95% {
    transform: scaleY(0.12);
  }
}

/* 镜像 working：双手交替起落。4px 在 96px 高的宠物上约 2px——它只是"在动"的暗示，
   真正的姿态信号由 body-group 的坐低承担。 */
@keyframes pet-mirror-typing {
  0%,
  100% {
    transform: translateY(0);
  }
  50% {
    transform: translateY(-4px);
  }
}

@keyframes pet-wave {
  0%,
  100% {
    transform: rotate(0deg);
  }
  25% {
    transform: rotate(-5deg);
  }
  75% {
    transform: rotate(5deg);
  }
}

@keyframes pet-jump {
  0%,
  100% {
    transform: translateY(0);
  }
  40% {
    transform: translateY(-8%);
  }
}

/* 芽：待机时轻微摇摆，作为一个持续存在的「有生命」信号 */
@keyframes pet-sprout-sway {
  0%,
  100% {
    transform: rotate(-2.5deg);
  }
  50% {
    transform: rotate(2.5deg);
  }
}

/* 芽：等待处理——立起来并加快频率，幅度仍小（再大就成了摇头） */
@keyframes pet-sprout-alert {
  0%,
  100% {
    transform: rotate(0deg) scaleY(1.08);
  }
  50% {
    transform: rotate(6deg) scaleY(1.08);
  }
}

/* 芽：庆祝——大幅左右甩，配合整身的跳跃 */
@keyframes pet-sprout-pop {
  0%,
  100% {
    transform: rotate(-10deg) scaleY(1.1);
  }
  50% {
    transform: rotate(10deg) scaleY(1.1);
  }
}

/* 挥手：0 → 抬起 → 回落 → 抬起一半，读起来是"招手两下"而不是原地抖 */
@keyframes pet-arm-wave {
  0%,
  100% {
    transform: rotate(0deg);
  }
  35% {
    transform: rotate(-58deg);
  }
  65% {
    transform: rotate(-24deg);
  }
}

/* ==========================================================================
   贴边收起
   --------------------------------------------------------------------------
   后端把窗口移到屏幕边缘外，只留 20px 可见（Go 的 petDockPeekPx 与前端的
   PET_DOCK_PEEK_PX 必须一致）。

   这里用 flex 对齐把宠物贴到【露出的那一边】，而不是算像素平移：
   窗口尺寸会随缩放设置变化，硬算出来的偏移量迟早会错一截。
   ========================================================================== */

/* 藏在左边 → 露出的是窗口右缘 → 宠物靠右 */
.pet--dock-left {
  align-items: flex-end;
}

/* 藏在右边 → 露出左缘 → 宠物靠左 */
.pet--dock-right {
  align-items: flex-start;
}

/* 藏在上面 → 露出底缘 → 宠物贴底（.pet 默认就是 flex-end，这里只为对称写出来） */
.pet--dock-top {
  justify-content: flex-end;
}

/* 藏在下面 → 露出顶缘 → 宠物贴顶 */
.pet--dock-bottom {
  justify-content: flex-start;
}

/* 收起态去掉内边距：露出的那 20px 里不该再有空白把宠物顶走 */
.pet--dock-left,
.pet--dock-right,
.pet--dock-top,
.pet--dock-bottom {
  padding: 0;
}

/* 收起态不显示气泡：它会把宠物挤出露出的那 20px，用户也点不到 */
.pet--dock-left .bubble,
.pet--dock-right .bubble,
.pet--dock-top .bubble,
.pet--dock-bottom .bubble {
  display: none;
}

/* ==========================================================================
   待机微动作的关键帧
   ==========================================================================
   幅度上限刻意压得很小（位移 ≤4px、缩放 ≤6%）：它们出现在用户余光里，
   幅度一大就从"活的"变成"闹的"。八帧素材的 12fps 节奏与这些关键帧无关——
   前者是图集，后者是 SVG 骨骼。 */

/* 张望：两眼同步左→右→回中。只看眼睛，身体不动 */
@keyframes pet-micro-look {
  0%,
  100% {
    transform: translateX(0);
  }
  25% {
    transform: translateX(3.5px);
  }
  55% {
    transform: translateX(-3.5px);
  }
  80% {
    transform: translateX(0);
  }
}

/* 伸懒腰：整体纵向拉长，再轻微回收（回弹只有 1%，多了会像抽搐） */
@keyframes pet-micro-stretch {
  0%,
  100% {
    transform: scale(1, 1);
  }
  35% {
    transform: scale(1.03, 1.06);
  }
  70% {
    transform: scale(0.995, 0.99);
  }
}

/* 理芽：左叶抖两下。
   关键帧以生长档位的基准角（--leaf-left-angle）为原点叠加，不能从 0deg 起——
   否则 growth-0/2/3/4 下叶子会在动画开始瞬间从基准角跳到 0，再在结束时跳回去。 */
@keyframes pet-micro-groom {
  0%,
  100% {
    transform: rotate(calc(var(--leaf-left-angle, 0deg) + 0deg));
  }
  30% {
    transform: rotate(calc(var(--leaf-left-angle, 0deg) + 10deg));
  }
  60% {
    transform: rotate(calc(var(--leaf-left-angle, 0deg) - 4deg));
  }
}

/* 打哈欠：嘴张开再合上。眼睛的闭合由 .pet--micro-yawn .pet__eyes-open 负责 */
@keyframes pet-micro-yawn {
  0%,
  100% {
    transform: scale(1, 1);
  }
  40%,
  65% {
    transform: scale(1, 2.4);
  }
}
</style>
