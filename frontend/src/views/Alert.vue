<script lang="ts" setup>
// 全屏强提醒窗口的渲染层（路由 /alert，meta.bare = true，故不套 AppShell）。
//
// 【它为什么存在】系统通知在开了勿扰、被通知中心折叠、或用户正盯着别的窗口时
// 很容易被忽略。本窗口铺满主屏工作区，用半透明遮罩把整块屏幕压住，中间放一张
// 必须处理的卡片——这是"强提醒"的全部含义。
//
// ---- 状态机（显示 / 隐藏的唯一驱动）----
// 窗口由后端创建时就是 Hidden，可见性完全由这里决定；不新增任何事件，
// 只用既有契约 reminder:fired / reminder:changed：
//
//   onMounted        → refresh → 有待处理 && 强提醒开关开 → ShowAlert
//   reminder:fired   → refresh → 有待处理 && 开关开       → ShowAlert
//   reminder:changed → refresh → 无待处理                → HideAlert
//   完成/推迟/跳过   → refresh → 无待处理                → HideAlert
//   开关为 false     → 不显示（并且如果正显示着，一律隐藏）
//
// 【为什么这些判断在前端而不是后端】后端只有数据库与配置，没有"这一屏现在该不该
// 打断用户"的上下文；而"屏幕上有什么"只有渲染层知道。
//
// ---- 与其他窗口的关系 ----
// 它是独立顶层窗口，与主窗、宠物窗口互不影响。宠物气泡在强提醒出现时是否还冒，
// 见 Pet.vue —— 本仓不为它新增跨窗口事件，故那里不做隐藏（理由写在该文件的注释里）。
import { computed, onMounted, onUnmounted, ref } from 'vue'
import {
  CompleteReminder,
  GetReminderSettings,
  HideAlert,
  ListPendingFires,
  ShowAlert,
  SkipReminder,
  SnoozeReminder,
} from '../../bindings/morrow/app.js'
import { invoke } from '../lib/invoke'
import { useEvent } from '../composables/useEvent'
import { ReminderEvent } from '../lib/reminder'
import { logInfo, logWarn } from '../lib/log'

/** 待处理提醒（用绑定返回类型反推，不手写镜像——Go 侧一改这里立刻跟着变）。 */
type PendingFire = NonNullable<Awaited<ReturnType<typeof ListPendingFires>>>[number]

/**
 * 倒计时的推进间隔（毫秒）。
 *
 * 1 秒是"秒数会走"的最小代价：本窗口是常驻的（只隐藏不销毁），间隔再小只是
 * 白烧电，再大则秒数会跳格。
 */
const ALERT_TICK_MS = 1000

// ---- 数据状态 ----

/** 待处理提醒；列表由后端按 fire_at ASC 排序，[0] 就是最早那条。 */
const pending = ref<PendingFire[]>([])
/** 强提醒开关（属于【提醒设置】，不是宠物设置）。 */
const strongAlertEnabled = ref(true)
/** 当前时间（毫秒），供倒计时每秒重算。 */
const nowMs = ref(Date.now())
/** 是否正在执行打卡动作：防止一次点击被重复提交。 */
const busy = ref(false)

// ---- 计时器句柄（普通变量：不参与渲染）----

/** setInterval 句柄；undefined 表示计时器未运行。 */
let tick: number | undefined

// ---- 派生视图 ----

/** 当前这条（最早的一条）。 */
const current = computed<PendingFire | null>(() => pending.value[0] ?? null)

/** 还剩几条待处理（不含当前这条）。 */
const extraCount = computed(() => Math.max(0, pending.value.length - 1))

/** 场景名：告诉用户"这是哪套计划"，空串时不渲染那一行。 */
const scenario = computed(() => (current.value?.scenario_name ?? '').trim())

/** 大标题：提醒标题，空标题时给一句兜底文案（不显示空气泡）。 */
const heading = computed(() => (current.value?.title ?? '').trim() || '该起来动一下了')

/** 正文。 */
const text = computed(() => (current.value?.content ?? '').trim())

/**
 * 已等待时长文案（MM:SS，超过一小时则 H:MM:SS）。
 *
 * 从 fire_at（Unix 秒）算到当前时刻，而不是从"窗口显示时刻"算：窗口可能被隐藏、
 * 被隐藏期间这条提醒一直在等，用显示时刻算会把等待时间抹掉。
 */
const waited = computed(() => {
  const fireAt = current.value?.fire_at ?? 0
  if (fireAt <= 0) return '00:00'
  return formatWait(nowMs.value - fireAt * 1000)
})

/** 把毫秒差格式化成 MM:SS / H:MM:SS，负数（时钟回拨）按 0 处理。 */
function formatWait(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000))
  const hours = Math.floor(total / 3600)
  const minutes = Math.floor((total % 3600) / 60)
  const seconds = total % 60
  const pad = (value: number) => String(value).padStart(2, '0')
  return hours > 0 ? `${hours}:${pad(minutes)}:${pad(seconds)}` : `${pad(minutes)}:${pad(seconds)}`
}

// ---- 倒计时的计时器生命周期 ----
//
// 【为什么不能只挂一个 setInterval 了事】本窗口是常驻的：应用启动时就加载了前端，
// 但绝大多数时间它处于隐藏态。挂一个空转的 1 秒计时器意味着应用活着就每秒醒一次，
// 只为了算一个没人看见的数字。
//
// 规则：只在「窗口可见 且 自己有待处理」时跑。
//   - 启动：applyPending → syncTicker
//   - 页面可见性变化：visibilitychange → syncTicker（隐藏即停）
//   - 隐藏窗口：hideWindow → stopTicker（不等 visibilitychange，立刻停）
//   - 卸载：onUnmounted → stopTicker
// startTicker 对重复调用安全（已存在就直接返回），故不会叠加出多个计时器。

/** 启动倒计时（重复调用安全）。 */
function startTicker(): void {
  if (tick !== undefined) return
  nowMs.value = Date.now()
  tick = window.setInterval(() => {
    nowMs.value = Date.now()
  }, ALERT_TICK_MS)
}

/** 停止倒计时（重复调用安全）。 */
function stopTicker(): void {
  if (tick === undefined) return
  window.clearInterval(tick)
  tick = undefined
}

/** 按「可见 && 有待处理」决定计时器是否该跑。 */
function syncTicker(): void {
  if (pending.value.length > 0 && !document.hidden) {
    startTicker()
    return
  }
  stopTicker()
}

// ---- 设置 / 数据读取 ----

/**
 * 读取强提醒开关。
 *
 * 【每次要显示前都读一次】设置页改动不会广播到本窗口（不为它新增事件），
 * 而这是一次纯内存读取（后端读的是 config 快照），代价可以忽略；
 * 换来的是"关掉开关后下一条提醒就不再占屏"，不需要重启应用。
 */
async function loadStrongAlertSetting(): Promise<void> {
  try {
    const settings = await invoke(() => GetReminderSettings(), 'GetReminderSettings')
    strongAlertEnabled.value = settings.strong_alert_enabled
  } catch (e) {
    // 读不到设置时按"开"处理：强提醒是本窗口存在的理由，
    // 因为一次配置读取失败就静默不显示，等于功能失效。
    strongAlertEnabled.value = true
    logWarn('读取提醒设置失败，强提醒按开启处理', { err: String(e) })
  }
}

/**
 * 重新拉取待处理列表，返回条数。
 *
 * 【为什么不用事件载荷里的文案】reminder:fired 的载荷只有 scenario_id，没有场景名；
 * 而卡片第一行就要用场景名。故一律以 ListPendingFires 的结果为准（它已经 JOIN 了
 * scenarios 并把 scenario_name 填好）。
 *
 * 读取失败时【不改状态】并返回当前条数：宁可维持上一帧的显示，也不要突然收起
 * 一张用户正在看的提醒。
 */
async function refreshPending(): Promise<number> {
  try {
    const list = (await invoke(() => ListPendingFires(), 'ListPendingFires')) ?? []
    pending.value = list
    syncTicker()
    return list.length
  } catch (e) {
    logWarn('读取待处理提醒失败', { err: String(e) })
    return pending.value.length
  }
}

// ---- 窗口显隐 ----

/** 让后端显示并置前本窗口。 */
async function showWindow(): Promise<void> {
  try {
    await invoke(() => ShowAlert(), 'ShowAlert')
  } catch (e) {
    logWarn('显示强提醒窗口失败', { err: String(e) })
  }
}

/** 隐藏本窗口（并立刻停掉倒计时，不等可见性变化）。 */
async function hideWindow(): Promise<void> {
  stopTicker()
  try {
    await invoke(() => HideAlert(), 'HideAlert')
  } catch (e) {
    logWarn('隐藏强提醒窗口失败', { err: String(e) })
  }
}

/**
 * 显示/隐藏的唯一收口：拉数据 → 判断 → 动作。
 *
 * 所有入口（挂载、两个事件、三个按钮、窗口重新可见）都走这里，
 * 才不会出现"A 路径显示、B 路径该隐藏却忘了"这类不对称。
 */
async function syncVisibility(): Promise<void> {
  const count = await refreshPending()
  if (count === 0) {
    await hideWindow()
    return
  }
  await loadStrongAlertSetting()
  if (!strongAlertEnabled.value) {
    await hideWindow()
    return
  }
  await showWindow()
}

// ---- 事件订阅（既有事件契约，不新增常量）----

// fired：到点了。载荷里没有场景名，故这里【不读载荷】，一律让 syncVisibility
// 拉到完整数据再渲染——少一次"先渲染半条信息再改"的闪烁。
useEvent(ReminderEvent.Fired, () => {
  void syncVisibility()
})

// changed：别处（主窗卡片、通知按钮、设置页）也可能把待处理清空，
// 这时必须收起遮罩，否则用户会面对一张点不掉的空卡片。
useEvent(ReminderEvent.Changed, () => {
  void syncVisibility()
})

// ---- 交互 ----

/** 三种打卡动作（完成 / 推迟 / 跳过）。 */
type AlertAction = 'done' | 'snooze' | 'skip'

/**
 * 处理当前这条提醒。
 *
 * 【推迟不传分钟数】由后端配置（设置页 → 推迟分钟数）决定。两处各存一份默认值
 * 早晚会漂移成"设置页说 5 分钟、实际 10 分钟"。
 *
 * 失败只记日志、不改窗口状态：卡片上的这条仍然是待处理（后端也不会把它标成已处理），
 * 用户可以直接再点一次。
 */
async function act(kind: AlertAction): Promise<void> {
  const fire = current.value
  if (!fire || busy.value) return
  busy.value = true
  try {
    if (kind === 'done') {
      await invoke(() => CompleteReminder(fire.fire_id), 'CompleteReminder')
    } else if (kind === 'snooze') {
      await invoke(() => SnoozeReminder(fire.fire_id), 'SnoozeReminder')
    } else {
      await invoke(() => SkipReminder(fire.fire_id), 'SkipReminder')
    }
    await syncVisibility()
  } catch (e) {
    logWarn('处理提醒失败', { kind, fire_id: fire.fire_id, err: String(e) })
  } finally {
    busy.value = false
  }
}

/** 模板里的按钮回调：包一层，避免模板表达式里产生未处理的 Promise。 */
function onDone(): void {
  void act('done')
}

/** 同上：推迟。 */
function onSnooze(): void {
  void act('snooze')
}

/** 同上：跳过。 */
function onSkip(): void {
  void act('skip')
}

/**
 * 键盘支持：Enter = 完成，Escape = 推迟。
 *
 * 【为什么 Esc 不绑「跳过」】跳过是不可逆的（这条提醒从此不再出现），而 Esc 是
 * 误触率最高的键；推迟是可逆的，代价小得多。
 *
 * 【为什么还要判 event.target】本页当前没有输入框，但这一层判断只花两行：
 * 将来（或某个插件的）输入框出现在这里时，打字里的 Enter/Esc 不会被吞掉。
 */
function onKeydown(event: KeyboardEvent): void {
  if (busy.value) return
  const target = event.target
  if (target instanceof HTMLElement) {
    const tag = target.tagName.toLowerCase()
    if (tag === 'input' || tag === 'textarea' || target.isContentEditable) return
  }
  if (event.key === 'Enter') {
    event.preventDefault()
    onDone()
    return
  }
  if (event.key === 'Escape') {
    event.preventDefault()
    onSnooze()
  }
}

/** 页面可见性变化：隐藏时停表，重新可见时对齐一次状态（期间可能已有新提醒）。 */
function onVisibilityChange(): void {
  if (document.hidden) {
    stopTicker()
    return
  }
  void syncVisibility()
}

// ---- 生命周期 ----

onMounted(async () => {
  document.addEventListener('visibilitychange', onVisibilityChange)
  // 键盘快捷键挂在 window 上：焦点可能在 body 或某个按钮上，两种都要能按。
  window.addEventListener('keydown', onKeydown)
  await loadStrongAlertSetting()
  await syncVisibility()
  logInfo('强提醒窗口已就绪', {
    pending: pending.value.length,
    strong_alert: strongAlertEnabled.value,
  })
})

onUnmounted(() => {
  document.removeEventListener('visibilitychange', onVisibilityChange)
  window.removeEventListener('keydown', onKeydown)
  stopTicker()
})
</script>

<template>
  <!-- 遮罩铺满整屏。刻意【不挂点击关闭】：强提醒的意义就是"必须处理才能消失"，
       点空白处就能划掉的话，它退化成了一条不好关的通知。
       关闭只有两个入口：下面三个按钮，或在主窗口里处理掉全部待处理。 -->
  <div class="alert">
    <section v-if="current" class="alert__card" role="alertdialog" aria-live="assertive">
      <p v-if="scenario" class="alert__scenario">{{ scenario }}</p>
      <p v-else-if="current?.is_one_off" class="alert__scenario">一次性提醒</p>
      <h1 class="alert__title">{{ heading }}</h1>
      <p v-if="text" class="alert__text">{{ text }}</p>

      <div class="alert__wait">
        <span class="alert__wait-dot" aria-hidden="true" />
        <span class="alert__wait-label">已等待</span>
        <span class="alert__wait-value">{{ waited }}</span>
      </div>

      <div class="alert__actions">
        <button class="alert__btn alert__btn--primary" :disabled="busy" @click="onDone">
          完成
        </button>
        <button class="alert__btn" :disabled="busy" @click="onSnooze">推迟</button>
        <button class="alert__btn" :disabled="busy" @click="onSkip">跳过</button>
      </div>

      <p v-if="extraCount > 0" class="alert__extra">还有 {{ extraCount }} 条待处理</p>
    </section>
  </div>
</template>

<style scoped>
/* 窗口必须全透明——三层都要覆盖，缺一层遮罩就变成一块不透明的死色。
   - html：WebKit 的默认底色来自它，只覆盖 body 不够；
   - body：styles/index.css 给它设了 background-color: var(--bg-base)；
   - #app：Vue 挂载点，任一处给它加背景都会让整窗变成实心色块。
   用 :global 是因为这些节点在组件外；!important 是为了不依赖样式插入顺序。
   注意：CSS 只解决"网页这一层"，macOS 上真正的透明还需要
   Mac.Backdrop=MacBackdropTransparent + private_mac_apis 构建标签（见 alert_window.go）。 */
:global(html),
:global(body),
:global(#app) {
  background: transparent !important;
  overflow: hidden;
}

/* 遮罩层：颜色来自令牌（--scrim 由 --bg-base 派生半透明），不写死 rgba。
   换肤时它跟着变；前端护栏（禁硬编码色值）也不会被绕过。 */
.alert {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 100vw;
  height: 100vh;
  padding: var(--space-8);
  background: var(--scrim);
  color: var(--text-1);
  user-select: none;
  /* 刻意不声明 --wails-draggable：全屏窗不该被拖动（拖歪了没有入口能拖回来） */
  animation: alert-fade var(--motion-normal) var(--ease-standard);
}

/* ---- 中央卡片 ---- */
.alert__card {
  width: min(560px, 100%);
  max-height: 100%;
  padding: var(--space-8);
  overflow: hidden;
  background: var(--bg-raised);
  border: 1px solid var(--border-strong);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-lg);
  text-align: center;
  animation: alert-rise var(--motion-slow) var(--ease-standard);
}

/* 场景名：回答"这是哪个计划"。品牌色 + 加粗，与标题拉开层级。 */
.alert__scenario {
  margin: 0 0 var(--space-2);
  color: var(--color-primary);
  font-size: var(--font-size-sm);
  font-weight: 600;
  letter-spacing: 0.02em;
}

.alert__title {
  margin: 0;
  font-size: calc(var(--font-size-xl) * 1.4);
  font-weight: 600;
  line-height: 1.3;
}

.alert__text {
  margin: var(--space-3) 0 0;
  color: var(--text-2);
  font-size: var(--font-size-md);
  line-height: 1.5;
}

/* ---- 已等待倒计时 ---- */
.alert__wait {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: var(--space-2);
  margin-top: var(--space-6);
}

/* 呼吸的小圆点：让"还在等"这件事有动感，不靠文字解释 */
.alert__wait-dot {
  width: var(--space-2);
  height: var(--space-2);
  border-radius: var(--radius-full);
  background: var(--color-warning);
  animation: alert-pulse 1.6s var(--ease-in-out) infinite;
}

.alert__wait-label {
  color: var(--text-3);
  font-size: var(--font-size-sm);
}

.alert__wait-value {
  color: var(--text-1);
  font-family: var(--font-mono);
  font-size: calc(var(--font-size-xl) * 1.2);
  font-variant-numeric: tabular-nums;
}

/* ---- 三个动作按钮 ---- */
.alert__actions {
  display: flex;
  gap: var(--space-3);
  margin-top: var(--space-6);
}

.alert__btn {
  flex: 1;
  padding: var(--space-3) 0;
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-md);
  background: transparent;
  color: var(--text-2);
  font-size: var(--font-size-md);
  cursor: pointer;
  transition: background var(--motion-fast) var(--ease-standard);
}

.alert__btn:hover:not(:disabled) {
  background: var(--bg-hover);
  color: var(--text-1);
}

.alert__btn:disabled {
  opacity: 0.5;
  cursor: default;
}

.alert__btn--primary {
  border-color: var(--color-primary);
  background: var(--color-primary-soft);
  color: var(--color-primary);
  font-weight: 600;
}

.alert__btn--primary:hover:not(:disabled) {
  background: var(--color-primary-50);
}

/* 「还有 N 条待处理」：放在角落，不抢主标题的注意力 */
.alert__extra {
  margin: var(--space-4) 0 0;
  color: var(--text-3);
  font-size: var(--font-size-xs);
}

@keyframes alert-fade {
  from {
    opacity: 0;
  }
  to {
    opacity: 1;
  }
}

@keyframes alert-rise {
  from {
    transform: translateY(8px) scale(0.98);
    opacity: 0;
  }
  to {
    transform: translateY(0) scale(1);
    opacity: 1;
  }
}

@keyframes alert-pulse {
  0%,
  100% {
    opacity: 1;
    transform: scale(1);
  }
  50% {
    opacity: 0.35;
    transform: scale(0.82);
  }
}
</style>
