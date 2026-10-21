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
  CollapsePet,
  CompleteReminder,
  ExpandPet,
  GetPauseState,
  GetPetGrowth,
  GetPetSettings,
  GetPetSprite,
  GetRestModeSnapshot,
  ListPendingFires,
  OpenMainWindow,
  SetPetBubbleHeight,
  SetPetBubbleVisible,
  SnoozeReminder,
} from '../../bindings/morrow/app.js'
import { invoke } from '../lib/invoke'
import { useEvent } from '../composables/useEvent'
import { ReminderEvent, type ReminderFiredPayload } from '../lib/reminder'
import { State as RestModeState, type Snapshot as RestModeSnapshot } from '../../bindings/morrow/internal/restmode/models.js'
import { logInfo, logWarn } from '../lib/log'
import {
  FRAME_H,
  FRAME_W,
  PET_ANIM_ROW,
  PET_AUTO_COLLAPSE_MS,
  PET_BUBBLE_FIT_WAIT_MS,
  PET_CELEBRATE_MS,
  PET_CLICK_MAX_TRAVEL_PX,
  PET_COLS,
  PET_DRAG_TRAVEL_PX,
  PET_EXPAND_GRACE_MS,
  PET_FPS,
  PET_IDLE_FPS,
  PET_JS_DRAG_FALLBACK,
  PET_PRESS_STALE_MS,
  PET_SETTINGS_POLL_MS,
  PET_WAVE_MS,
  PetAnim,
  PetEvent,
  growthStageClass,
  type GrowthStage,
  type PetAnimValue,
  type PetDockedPayload,
  type PetDockSide,
} from '../lib/pet'

/** 待处理提醒（用绑定返回类型反推，不手写镜像——Go 侧一改这里立刻跟着变）。 */
type PendingFire = NonNullable<Awaited<ReturnType<typeof ListPendingFires>>>[number]

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
 * 芽生长档位（0–4，后端 GetPetGrowth 返回的 stage）。
 *
 * 初值 0：首启尚未拉到数据时显示健康小苗；读取失败时保持上一次值，不闪回。
 */
const growth = ref<GrowthStage>(0)

// ---- 暂停状态 ----

/** 是否处于暂停状态（智能免打扰）。 */
const paused = ref(false)
/** 暂停原因（idle/lock/meeting/fullscreen/focus/calendar；空串表示未暂停）。 */
const pauseReason = ref('')

// ---- 休息模式状态 ----

/**
 * 休息模式快照。
 *
 * 三种可见状态在宠物上分别对应：
 *   - warning：气泡「1 分钟后该起身拉伸了」（US-9）
 *   - countdown：宠物切到「陪做」姿态 + 气泡「陪你休息」+ 剩余秒数（US-27）
 *   - 其他（idle/done/skipped）：宠物不介入，走原有 pending 通道
 */
const restSnap = ref<RestModeSnapshot | null>(null)

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
  const row = PET_ANIM_ROW[anim.value]
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
  const fps = focused && !document.hidden ? PET_FPS : PET_IDLE_FPS
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
 * 拉取并应用芽生长档位（挂载 / reminder:changed 事件 / 4 秒轮询都会走到这里）。
 *
 * 档位计算在 Go 侧完成，前端只把返回的 stage 映射到 CSS class（见 growthStageClass）；
 * 失败时保持上一次值，不闪回、不报错。
 */
async function syncGrowth(): Promise<void> {
  try {
    const result = await invoke(() => GetPetGrowth(), 'GetPetGrowth')
    growth.value = (result?.stage ?? 0) as GrowthStage
  } catch (e) {
    // 读不到时保持上一次形态：宠物窗口没有 DevTools，静默降级优于报错。
    logWarn('读取芽生长档位失败', { err: String(e) })
  }
}

/**
 * 拉取并应用暂停状态（4 秒轮询）。
 *
 * 暂停时显示「我先不打扰」气泡；恢复后收起气泡。
 * 失败时保持上一次值，不闪回、不报错。
 */
async function syncPauseState(): Promise<void> {
  try {
    const result = await invoke(() => GetPauseState(), 'GetPauseState')
    paused.value = result?.paused ?? false
    pauseReason.value = result?.reason ?? ''
  } catch (e) {
    // 读不到时保持上一次状态：静默降级优于报错。
    logWarn('读取暂停状态失败', { err: String(e) })
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
})

useEvent(ReminderEvent.Changed, () => {
  void handleChanged()
})

// 贴边状态：后端把窗口移出/移回屏幕边缘后广播。
// 前端只做视觉配合（把宠物平移到露出的那条窄边里），窗口位置由后端负责。
useEvent<PetDockedPayload>(PetEvent.Docked, (payload) => {
  dockSide.value = payload.side
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

/** 鼠标进入：若正处于贴边收起态，展开回原位置。 */
function onPetEnter(): void {
  hovering = true
  if (collapseTimer !== undefined) {
    window.clearTimeout(collapseTimer)
    collapseTimer = undefined
  }
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
  await syncPauseState()
  await syncRestModeState()
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
  focused = document.hasFocus()

  try {
    await syncSettings()
  } catch (e) {
    // 读不到设置不是致命错误：用内置形象 + 默认缩放先把宠物显示出来。
    mode.value = 'builtin'
    logWarn('读取宠物设置失败，使用内置形象', { err: String(e) })
  }

  await refreshPending()
  await syncGrowth()
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
  cancelPetPress()
  stopLoop()
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
    :class="[`pet--${anim}`, growthStageClass(growth), dockSide ? `pet--dock-${dockSide}` : '']"
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

    <!-- 暂停气泡：智能免打扰期间显示「我先不打扰」。 -->
    <div v-else-if="pauseBubbleVisible" ref="bubbleRef" class="bubble bubble--pause">
      <p class="bubble__title">{{ pauseBubbleText }}</p>
    </div>

    <!-- 待处理气泡：只有没有更高优先级气泡时才出现。按钮直接作用于第一条提醒，
         让用户不必打开主窗口就能处理。
         【必须是这条链的最后一支】陪做 > 预警 > 暂停 > 待处理：同一时刻只显示一个
         气泡。倒计时期间用户正在「做」，再叠一个完成/推迟只会抢注意力，也会让
         「从宠物提前完成」绕过休息模式的会话收尾。 -->
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

    <!-- 宠物本体：整块是拖拽区（窗口无边框，拖它就是移动窗口的方式）。
         用 mousedown 而不是 click 起手势：click 与拖拽共用同一串鼠标事件，
         谁先谁后由 wails 的拖拽逻辑决定，靠它判「点击」是不可靠的。
         拖拽属性走内联样式，兜底模式下会被切成 no-drag（见 artStyleWithDrag）。 -->
    <div class="pet__art" ref="petArtRef" :style="artStyleWithDrag" @mousedown="onPetPointerDown">
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
              <path class="pet__stem" d="M96 46V20" />
              <path class="pet__leaf pet__leaf--left" d="M96 30C87 29 74 22 72 12 84 11 94 19 96 30Z" />
              <path class="pet__leaf pet__leaf--right" d="M96 24C105 23 118 16 120 8 108 7 98 14 96 24Z" />
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

/* ---- 芽随今日完成率生长：五档 ----
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
  transform: rotate(6deg);
}
.pet--growth-0 .pet__leaf--right {
  transform: rotate(-6deg);
}
.pet--growth-1 .pet__leaf--left {
  transform: rotate(0deg);
}
.pet--growth-1 .pet__leaf--right {
  transform: rotate(0deg);
}
.pet--growth-2 .pet__leaf--left {
  transform: rotate(-8deg);
}
.pet--growth-2 .pet__leaf--right {
  transform: rotate(8deg);
}
.pet--growth-3 .pet__leaf--left {
  transform: rotate(-14deg);
}
.pet--growth-3 .pet__leaf--right {
  transform: rotate(14deg);
}
.pet--growth-4 .pet__leaf--left {
  transform: rotate(-20deg);
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
</style>
