<script lang="ts" setup>
import { computed, onMounted, onUnmounted, ref } from 'vue'
import Skeleton from '../components/Skeleton.vue'
import EmptyState from '../components/EmptyState.vue'
import { useReminderStore } from '../stores/reminder'
import { useEvent } from '../composables/useEvent'
import {
  Confirm,
  CreateReminder,
  CreateReminders,
  DeleteReminder,
  GenerateQuickAddDrafts,
  GetSitSettings,
  ListMissedToday,
  ListOneOffs,
  ListScenarios,
  SelectFile,
  SetSitProfileEnabled,
  UpdateReminder,
} from '../../bindings/morrow/app.js'
import { invoke, type AppError } from '../lib/invoke'
import { EventAction, eventName } from '../lib/event'
import { logInfo, logWarn } from '../lib/log'
import {
  MAX_ATTACHMENTS,
  MAX_IMAGE_BYTES,
  attachmentKind,
  fileName,
  isImageMime,
  mimeFromFileName,
} from '../lib/attachment'
import {
  CheckInAction,
  ReminderEvent,
  kindMeta,
  type ReminderFiredPayload,
} from '../lib/reminder'

// 仪表盘：当前场景 + 待处理卡片 + 今日进度 + 下一条倒计时 + 一次性提醒。
// 数据全部来自一次 GetTodayStats 聚合调用（见 service.TodayStats 的注释）；
// 一次性列表是独立查询（不受当前场景影响）。
const store = useReminderStore()

// ---- 升级用户的久坐画像同意卡 ----
// 干净安装走 Welcome 第 4 步；已完成引导的升级用户没有问过，就在首页补一次。
const sitConsentVisible = ref(false)
const sitConsentBusy = ref(false)
const sitConsentError = ref<AppError | null>(null)

async function loadSitConsent(): Promise<void> {
  try {
    const settings = await invoke(() => GetSitSettings(), 'GetSitSettings')
    sitConsentVisible.value = !settings.profile_asked
  } catch (e) {
    logWarn('读取久坐画像同意状态失败', { err: String(e) })
  }
}

async function resolveSitConsent(enabled: boolean): Promise<void> {
  if (sitConsentBusy.value) return
  sitConsentBusy.value = true
  sitConsentError.value = null
  try {
    await invoke(() => SetSitProfileEnabled(enabled, false), 'SetSitProfileEnabled')
    sitConsentVisible.value = false
    logInfo('久坐应用画像同意已记录', { enabled })
  } catch (e) {
    sitConsentError.value = e as AppError
  } finally {
    sitConsentBusy.value = false
  }
}

/** 一次性提醒行（用绑定返回类型反推，不手写镜像）。 */
type OneOffRow = NonNullable<Awaited<ReturnType<typeof ListOneOffs>>>[number]
type ReminderInput = Parameters<typeof CreateReminder>[0]
type QuickAddPlan = NonNullable<Awaited<ReturnType<typeof GenerateQuickAddDrafts>>>
type QuickAddDraft = NonNullable<QuickAddPlan['drafts']>[number]
type ScenarioRow = NonNullable<Awaited<ReturnType<typeof ListScenarios>>>[number]

/**
 * 今日已错过（US-6）。
 *
 * 【为什么单独查询而不是塞进 GetTodayStats】stats 每 4 秒轮询一次驱动倒计时；
 * 已错过列表只在页面加载与 reminder:changed 事件时刷新即可，把它并入 stats 会让
 * 一个几乎不变的列表跟着高频查询跑。
 */
type MissedRow = NonNullable<Awaited<ReturnType<typeof ListMissedToday>>>[number]
const missedToday = ref<MissedRow[]>([])

/** 加载今日已错过（首页加载 / reminder:changed 事件都会走到这里）。 */
async function loadMissedToday(): Promise<void> {
  try {
    missedToday.value = (await invoke(() => ListMissedToday(), 'ListMissedToday')) ?? []
  } catch {
    // 与 pending 的加载失败一样保持静默：错过项缺席不影响主流程，用户仍能看到待处理。
    missedToday.value = []
  }
}

/** 已错过条目的时间文案（HH:MM）。 */
function missedTime(fireAt: number): string {
  const d = new Date(fireAt * 1000)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${pad(d.getHours())}:${pad(d.getMinutes())}`
}

/** 手动一次性提醒表单。when 使用本地时间字符串，提交时转 Unix 秒。 */
interface OneOffForm {
  id: number
  title: string
  content: string
  when: string
}

/** AI 草稿的可编辑行。 */
interface AiDraftRow {
  selected: boolean
  scheduleType: 'once' | 'daily'
  title: string
  content: string
  when: string
  kind: string
  intervalMin: number
  windowStart: string
  windowEnd: string
  dailyTarget: number
  scenarioId: number
}

/** 快速添加的附件：路径文件，或粘贴/拖拽进来的内联图片。 */
interface AiAttachmentRow {
  id: string
  name: string
  kind: 'image' | 'text'
  path?: string
  mime?: string
  dataBase64?: string
}

const oneOffs = ref<OneOffRow[]>([])
const oneOffsLoading = ref(false)
const oneOffsError = ref<AppError | null>(null)
const oneOffHint = ref('')
const oneOffSaving = ref(false)
const oneOffForm = ref<OneOffForm>(blankOneOffForm())
const oneOffEditForm = ref<OneOffForm>(blankOneOffForm())
const oneOffDialogVisible = ref(false)

// ---- AI 快速添加 ----
const aiDescription = ref('')
const aiGenerating = ref(false)
const aiSaving = ref(false)
const aiError = ref<AppError | null>(null)
const aiPreviewVisible = ref(false)
const aiSummary = ref('')
const aiOmitted = ref(0)
const aiRows = ref<AiDraftRow[]>([])
const aiAttachments = ref<AiAttachmentRow[]>([])
const scenarios = ref<ScenarioRow[]>([])
/** 内联附件序号：仅用于 Vue key，避免同一毫秒粘贴多张时 key 冲突。 */
let inlineAttachmentSeq = 0
const defaultScenarioID = computed(
  () => scenarios.value.find((item) => item.enabled)?.id ?? scenarios.value[0]?.id ?? 0,
)

// 倒计时用的「当前时间」：每秒推进一次，文案自己走动，不必为每条提醒起定时器。
const nowMs = ref(Date.now())
let ticker: number | undefined

onMounted(() => {
  void store.loadAll()
  void loadOneOffs()
  void loadMissedToday()
  void loadScenarios()
  void loadSitConsent()
  ticker = window.setInterval(() => {
    nowMs.value = Date.now()
  }, 1000)
})

onUnmounted(() => {
  if (ticker !== undefined) {
    window.clearInterval(ticker)
  }
})

// 后端推来的事件：fired 让新卡片立刻出现，changed 让别处（或通知按钮）的操作
// 也能反映到这一屏。两个都只是"重新拉一次"，不试图在前端拼状态。
useEvent<ReminderFiredPayload>(ReminderEvent.Fired, () => {
  void store.loadAll()
  void loadOneOffs()
  void loadMissedToday()
})
useEvent(ReminderEvent.Changed, () => {
  void store.loadAll()
  void loadOneOffs()
  void loadMissedToday()
  void loadScenarios()
})

/** 待处理分组的 DOM 引用；菜单栏「查看待处理」会把这一块滚进视野（US-16）。 */
const pendingSectionRef = ref<HTMLElement | null>(null)

// app:show_pending 是菜单栏「查看待处理」入口的广播（后端 internal/event）。
// 与 reminder:changed 区分：changed 是「数据变了」，show_pending 是「用户想看」。
useEvent(eventName('app', EventAction.ShowPending), () => {
  void store.loadAll()
  void loadMissedToday()
  // nextTick 不引：pendingSectionRef 是稳定 ref（v-if 挂载后一直存在），
  // scrollIntoView 在数据尚未到达时滚到空区块也没副作用。
  pendingSectionRef.value?.scrollIntoView({ behavior: 'smooth', block: 'start' })
})

const scenario = computed(() => store.stats?.scenario ?? null)
const progress = computed(() => store.stats?.reminders ?? [])
const totalDone = computed(() => store.stats?.done_total ?? 0)
const totalTarget = computed(() => store.stats?.target_total ?? 0)
const pendingCount = computed(() => store.stats?.pending_count ?? 0)

const overallPercent = computed(() =>
  totalTarget.value > 0 ? Math.min(100, Math.round((totalDone.value / totalTarget.value) * 100)) : 0,
)

const todayText = computed(() => {
  const now = new Date()
  return `${now.getMonth() + 1} 月 ${now.getDate()} 日`
})

// 倒计时用「目标时间 - 当前时间」每秒重算，而不是 setTimeout 到点：
// 目标是后端算出来的，用户中途推迟或改计划时，重算能立刻跟上。
const countdown = computed(() => {
  const target = store.stats?.next_fire_at ?? 0
  if (target <= 0) return '今天已无提醒'
  const diff = target * 1000 - nowMs.value
  if (diff <= 0) return '即将提醒'
  return formatDuration(diff)
})

const nextTitle = computed(() => store.stats?.next_title ?? '')

/** 把毫秒差格式化成 m:ss 或 h:mm:ss。 */
function formatDuration(ms: number): string {
  const total = Math.floor(ms / 1000)
  const hours = Math.floor(total / 3600)
  const minutes = Math.floor((total % 3600) / 60)
  const seconds = total % 60
  const pad = (value: number) => String(value).padStart(2, '0')
  return hours > 0 ? `${hours}:${pad(minutes)}:${pad(seconds)}` : `${minutes}:${pad(seconds)}`
}

/** 把 Unix 秒格式化成 HH:MM（本地时区）。 */
function formatClock(unixSeconds: number): string {
  if (!unixSeconds) return '—'
  return new Date(unixSeconds * 1000).toLocaleTimeString('zh-CN', {
    hour: '2-digit',
    minute: '2-digit',
  })
}

/** 两位补零。 */
function pad2(value: number): string {
  return String(value).padStart(2, '0')
}

/** 把 Unix 秒格式化成「YYYY-MM-DD HH:mm」（本地时区）。 */
function formatDateTime(unixSeconds: number): string {
  if (!unixSeconds) return '—'
  const date = new Date(unixSeconds * 1000)
  return `${date.getFullYear()}-${pad2(date.getMonth() + 1)}-${pad2(date.getDate())} ${pad2(date.getHours())}:${pad2(date.getMinutes())}`
}

/** 把 Unix 秒转成日期选择器使用的本地字符串。 */
function formatDateTimeInput(unixSeconds: number): string {
  return formatDateTime(unixSeconds)
}

/** 把本地「YYYY-MM-DD HH:mm」解析为 Unix 秒；非法时返回 0。 */
function parseLocalDateTime(text: string): number {
  const match = /^(\d{4})-(\d{2})-(\d{2})[ T](\d{2}):(\d{2})$/.exec(text.trim())
  if (!match) return 0
  const date = new Date(
    Number(match[1]),
    Number(match[2]) - 1,
    Number(match[3]),
    Number(match[4]),
    Number(match[5]),
    0,
    0,
  )
  return Math.floor(date.getTime() / 1000)
}

/** 空表单默认落在下一小时附近（按 5 分钟取整），减少一次输入。 */
function blankOneOffForm(): OneOffForm {
  const next = new Date(Date.now() + 60 * 60 * 1000)
  next.setSeconds(0, 0)
  next.setMinutes(Math.ceil(next.getMinutes() / 5) * 5)
  return {
    id: 0,
    title: '',
    content: '',
    when: formatDateTimeInput(Math.floor(next.getTime() / 1000)),
  }
}

/** 把表单转成 CreateReminder / UpdateReminder 的入参。 */
function oneOffPayload(form: OneOffForm): ReminderInput {
  return {
    id: form.id,
    scenario_id: 0,
    title: form.title.trim(),
    content: form.content.trim(),
    kind: 'custom',
    interval_min: 0,
    window_start: '',
    window_end: '',
    daily_target: 0,
    enabled: true,
    once_at: parseLocalDateTime(form.when),
    sort_order: 0,
  }
}

/** 校验表单并返回可读错误；合法时返回 null。 */
function validateOneOff(payload: ReminderInput): string | null {
  if (!payload.title) return '先写一句要提醒什么'
  if (payload.once_at <= Math.floor(Date.now() / 1000)) {
    return '提醒时间已过去，请选择将来的时间'
  }
  return null
}

/** 加载一次性提醒列表。 */
async function loadOneOffs(): Promise<void> {
  oneOffsLoading.value = true
  oneOffsError.value = null
  try {
    oneOffs.value = (await invoke(() => ListOneOffs(), 'ListOneOffs')) ?? []
  } catch (e) {
    oneOffsError.value = e as AppError
  } finally {
    oneOffsLoading.value = false
  }
}

/** 加载场景列表，供周期草稿选择目标场景。 */
async function loadScenarios(): Promise<void> {
  try {
    scenarios.value = (await invoke(() => ListScenarios(), 'ListScenarios')) ?? []
  } catch (e) {
    aiError.value = e as AppError
  }
}

/** 手动创建一条一次性提醒。 */
async function createOneOff(): Promise<void> {
  oneOffErrorClear()
  const payload = oneOffPayload(oneOffForm.value)
  const invalid = validateOneOff(payload)
  if (invalid) {
    oneOffsError.value = { code: 'validation', message: invalid }
    return
  }
  oneOffSaving.value = true
  try {
    await invoke(() => CreateReminder(payload), 'CreateReminder')
    oneOffHint.value = `已添加「${payload.title}」`
    oneOffForm.value = blankOneOffForm()
    await Promise.all([store.loadAll(), loadOneOffs()])
  } catch (e) {
    oneOffsError.value = e as AppError
  } finally {
    oneOffSaving.value = false
  }
}

/** 打开编辑一次性提醒对话框。 */
function openEditOneOff(row: OneOffRow): void {
  oneOffErrorClear()
  oneOffEditForm.value = {
    id: row.id,
    title: row.title,
    content: row.content ?? '',
    when: formatDateTimeInput(row.once_at),
  }
  oneOffDialogVisible.value = true
}

/** 保存编辑后的一次性提醒。 */
async function submitOneOffEdit(): Promise<void> {
  oneOffErrorClear()
  const payload = oneOffPayload(oneOffEditForm.value)
  const invalid = validateOneOff(payload)
  if (invalid) {
    oneOffsError.value = { code: 'validation', message: invalid }
    return
  }
  oneOffSaving.value = true
  try {
    await invoke(() => UpdateReminder(payload), 'UpdateReminder')
    oneOffDialogVisible.value = false
    oneOffHint.value = '已保存修改'
    await Promise.all([store.loadAll(), loadOneOffs()])
  } catch (e) {
    oneOffsError.value = e as AppError
  } finally {
    oneOffSaving.value = false
  }
}

/** 删除一条一次性提醒（含已错过项）。 */
async function deleteOneOff(row: OneOffRow): Promise<void> {
  oneOffErrorClear()
  try {
    const confirmed = await invoke(
      () => Confirm('删除一次性提醒', `确定删除「${row.title}」吗？`),
      'Confirm',
    )
    if (!confirmed) return
    await invoke(() => DeleteReminder(row.id), 'DeleteReminder')
    oneOffHint.value = `已删除「${row.title}」`
    await Promise.all([store.loadAll(), loadOneOffs()])
  } catch (e) {
    oneOffsError.value = e as AppError
  }
}

/** 清掉一次性表单的错误与提示。 */
function oneOffErrorClear(): void {
  oneOffsError.value = null
  oneOffHint.value = ''
}

/** 待处理卡片的时间/归属文案；一次性项没有场景。 */
function pendingMeta(fire: (typeof store.pending)[number]): string {
  if (fire.is_one_off) return `一次性 · ${formatDateTime(fire.fire_at)}`
  return `${fire.scenario_name} · ${formatClock(fire.fire_at)}`
}

/** 把 AI 草稿转成可编辑行。 */
function draftToRow(draft: QuickAddDraft): AiDraftRow {
  const daily = draft.schedule_type === 'daily'
  return {
    selected: true,
    scheduleType: daily ? 'daily' : 'once',
    title: draft.title,
    content: draft.content,
    when: draft.once_at > 0 ? formatDateTimeInput(draft.once_at) : blankOneOffForm().when,
    kind: draft.kind,
    intervalMin: daily ? draft.interval_min : 0,
    windowStart: daily ? draft.window_start || '09:00' : '',
    windowEnd: daily ? draft.window_end || '18:00' : '',
    dailyTarget: daily ? draft.daily_target : 0,
    scenarioId: daily ? defaultScenarioID.value : 0,
  }
}

/** 追加一个来自系统文件选择器的附件（图片或文本，走路径）。 */
async function addAttachmentFile(): Promise<void> {
  aiError.value = null
  try {
    const path = await invoke(() => SelectFile('选择图片或文件'), 'SelectFile')
    if (!path) return
    if (aiAttachments.value.some((item) => item.path === path)) {
      aiError.value = { code: 'validation', message: '这个文件已经添加过了' }
      return
    }
    if (aiAttachments.value.length >= MAX_ATTACHMENTS) {
      aiError.value = {
        code: 'validation',
        message: `一次最多添加 ${MAX_ATTACHMENTS} 个附件`,
      }
      return
    }
    aiAttachments.value.push({
      id: `path:${path}`,
      name: fileName(path),
      kind: attachmentKind(path),
      path,
    })
  } catch (e) {
    aiError.value = e as AppError
  }
}

/** 把 File 读成不含 data URI 前缀的 base64。 */
function readFileAsBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => {
      const result = String(reader.result ?? '')
      const comma = result.indexOf(',')
      resolve(comma >= 0 ? result.slice(comma + 1) : result)
    }
    reader.onerror = () => reject(reader.error ?? new Error('读取图片失败'))
    reader.readAsDataURL(file)
  })
}

/** 追加一张粘贴/拖拽进来的图片（不落盘，只存在内存与请求体）。 */
async function addInlineImage(file: File): Promise<void> {
  const rawMime = (file.type || mimeFromFileName(file.name) || 'image/png').toLowerCase()
  const mime = rawMime === 'image/jpg' ? 'image/jpeg' : rawMime
  if (!isImageMime(mime)) {
    aiError.value = { code: 'validation', message: '仅支持 png / jpg / webp / gif 图片' }
    return
  }
  if (file.size > MAX_IMAGE_BYTES) {
    aiError.value = { code: 'validation', message: '图片超过 5MB，请压缩后再试' }
    return
  }
  if (aiAttachments.value.length >= MAX_ATTACHMENTS) {
    aiError.value = {
      code: 'validation',
      message: `一次最多添加 ${MAX_ATTACHMENTS} 个附件`,
    }
    return
  }
  try {
    const dataBase64 = await readFileAsBase64(file)
    aiAttachments.value.push({
      id: `inline:${inlineAttachmentSeq++}`,
      name: file.name || '粘贴的图片.png',
      kind: 'image',
      mime,
      dataBase64,
    })
  } catch (e) {
    aiError.value = e as AppError
  }
}

/** 处理粘贴：只拦截图片，文本粘贴仍然落到输入框。 */
async function onAiPaste(event: ClipboardEvent): Promise<void> {
  const items = event.clipboardData?.items
  if (!items) return
  const images: File[] = []
  for (const item of Array.from(items)) {
    if (item.kind === 'file' && item.type.startsWith('image/')) {
      const file = item.getAsFile()
      if (file) images.push(file)
    }
  }
  if (images.length === 0) return
  event.preventDefault()
  aiError.value = null
  for (const file of images) {
    await addInlineImage(file)
  }
}

/** 处理拖拽：图片走内联通道，其他文件提示改用文件选择器。 */
async function onAiDrop(event: DragEvent): Promise<void> {
  const files = Array.from(event.dataTransfer?.files ?? [])
  if (files.length === 0) return
  const images = files.filter((file) => file.type.startsWith('image/'))
  if (images.length === 0) {
    aiError.value = {
      code: 'validation',
      message: '拖拽目前只支持图片；文本/PDF 请用「添加图片/文件」选择',
    }
    return
  }
  aiError.value = null
  for (const file of images) {
    await addInlineImage(file)
  }
}

/** 移除一个附件。 */
function removeAiAttachment(id: string): void {
  aiAttachments.value = aiAttachments.value.filter((item) => item.id !== id)
}

/** 调 AI 把一句话解析成草稿；成功才打开预览。 */
async function generateQuickAdd(): Promise<void> {
  aiError.value = null
  const description = aiDescription.value.trim()
  if (!description && aiAttachments.value.length === 0) {
    aiError.value = { code: 'validation', message: '先写一句提醒内容，或添加一张图片' }
    return
  }
  const filePaths = aiAttachments.value
    .filter((item) => item.path)
    .map((item) => item.path as string)
  const inlineImages = aiAttachments.value
    .filter((item) => item.dataBase64)
    .map((item) => ({
      name: item.name,
      mime: item.mime ?? 'image/png',
      data_base64: item.dataBase64 as string,
    }))
  aiGenerating.value = true
  try {
    const plan = await invoke(
      () => GenerateQuickAddDrafts(description, filePaths, inlineImages),
      'GenerateQuickAddDrafts',
    )
    const drafts = plan?.drafts ?? []
    if (drafts.length === 0) {
      aiError.value = { code: 'validation', message: 'AI 没有给出可用的提醒，换个说法再试一次' }
      return
    }
    aiSummary.value = plan?.summary ?? ''
    aiOmitted.value = plan?.omitted ?? 0
    aiRows.value = drafts.map(draftToRow)
    aiPreviewVisible.value = true
  } catch (e) {
    aiError.value = e as AppError
  } finally {
    aiGenerating.value = false
  }
}

/** 把选中的 AI 草稿转成批量写入入参。 */
function aiRowPayload(row: AiDraftRow): ReminderInput {
  if (row.scheduleType === 'daily') {
    return {
      id: 0,
      scenario_id: row.scenarioId,
      title: row.title.trim(),
      content: row.content.trim(),
      kind: row.kind || 'custom',
      interval_min: row.intervalMin,
      window_start: row.windowStart.trim(),
      window_end: row.windowEnd.trim(),
      daily_target: row.dailyTarget,
      enabled: true,
      once_at: 0,
      sort_order: 0,
    }
  }
  return {
    id: 0,
    scenario_id: 0,
    title: row.title.trim(),
    content: row.content.trim(),
    kind: row.kind || 'custom',
    interval_min: 0,
    window_start: '',
    window_end: '',
    daily_target: 0,
    enabled: true,
    once_at: parseLocalDateTime(row.when),
    sort_order: 0,
  }
}

/** 用户确认后原子写入选中的 AI 草稿。 */
async function saveAiDrafts(): Promise<void> {
  aiError.value = null
  const selected = aiRows.value.filter((row) => row.selected)
  if (selected.length === 0) {
    aiError.value = { code: 'validation', message: '至少选择一条提醒' }
    return
  }
  const payloads: ReminderInput[] = []
  for (const row of selected) {
    const payload = aiRowPayload(row)
    if (!payload.title) {
      aiError.value = { code: 'validation', message: '每条草稿都需要标题' }
      return
    }
    if (row.scheduleType === 'daily') {
      if (payload.scenario_id <= 0) {
        aiError.value = {
          code: 'validation',
          message: `「${payload.title}」需要选择目标场景`,
        }
        return
      }
    } else if (payload.once_at <= Math.floor(Date.now() / 1000)) {
      aiError.value = { code: 'validation', message: `「${payload.title}」的时间已过去` }
      return
    }
    payloads.push(payload)
  }

  aiSaving.value = true
  try {
    await invoke(() => CreateReminders(payloads), 'CreateReminders')
    aiPreviewVisible.value = false
    aiRows.value = []
    aiDescription.value = ''
    aiAttachments.value = []
    oneOffHint.value = `已新增 ${payloads.length} 条提醒`
    await Promise.all([store.loadAll(), loadOneOffs()])
  } catch (e) {
    aiError.value = e as AppError
  } finally {
    aiSaving.value = false
  }
}

/** 进度百分比（无目标时返回 0，不显示虚假的满格）。 */
function progressPercent(done: number, target: number): number {
  if (target <= 0) return 0
  return Math.min(100, Math.round((done / target) * 100))
}
</script>

<template>
  <section class="page">
    <header class="page__head">
      <div>
        <h1 class="page__title">仪表盘</h1>
        <p class="page__desc">{{ todayText }} · {{ scenario ? scenario.name : '未启用场景' }}</p>
      </div>
      <el-button size="small" :loading="store.loading" @click="store.loadAll()">刷新</el-button>
    </header>

    <p v-if="store.error" class="page__error" @click="store.clearError()">
      [{{ store.error.code }}] {{ store.error.message }}
    </p>

    <el-card v-if="sitConsentVisible" class="sit-consent" shadow="never">
      <div class="sit-consent__copy">
        <h2 class="sit-consent__title">让小满知道你在用什么？</h2>
        <p class="sit-consent__desc">
          开启后会记录前台应用名、起止时间、使用时长与行为类别；只存本机，不上传，
          不读窗口标题、屏幕内容或键鼠输入内容。可以随时在设置里关闭、查看或清空。
        </p>
        <p class="sit-consent__note">
          小满默认说话比较直（毒舌），可随时切换；选择基础检测也能继续久坐提醒，只是不读应用名。
        </p>
      </div>
      <div class="sit-consent__actions">
        <el-button :loading="sitConsentBusy" @click="resolveSitConsent(false)">
          只使用基础检测
        </el-button>
        <el-button type="primary" :loading="sitConsentBusy" @click="resolveSitConsent(true)">
          开启应用画像
        </el-button>
      </div>
      <p v-if="sitConsentError" class="page__error" @click="sitConsentError = null">
        [{{ sitConsentError.code }}] {{ sitConsentError.message }}
      </p>
    </el-card>

    <el-card class="quick" shadow="never">
      <div class="quick__drop" @paste="onAiPaste" @dragover.prevent @drop.prevent="onAiDrop">
        <div class="quick__head">
          <h2 class="quick__title">快速添加</h2>
          <span class="quick__note">一次性 / 周期提醒 · 可粘贴或拖拽截图</span>
        </div>
        <el-input
          v-model="aiDescription"
          type="textarea"
          :rows="2"
          maxlength="2000"
          show-word-limit
          placeholder="用一句话描述，例如「明天下午 4 点吃一个苹果」"
        />
        <div class="quick__row">
          <el-button
            :loading="aiGenerating"
            :disabled="!aiDescription.trim() && aiAttachments.length === 0"
            @click="generateQuickAdd"
          >
            AI 生成草稿
          </el-button>
          <el-button
            size="small"
            :disabled="aiAttachments.length >= MAX_ATTACHMENTS"
            @click="addAttachmentFile"
          >
            添加图片/文件
          </el-button>
          <span class="quick__note">也可以直接在下面手动填写</span>
        </div>
        <ul v-if="aiAttachments.length" class="ai-files">
          <li v-for="item in aiAttachments" :key="item.id" class="ai-file">
            <img
              v-if="item.dataBase64"
              class="ai-file__thumb"
              :src="`data:${item.mime ?? 'image/png'};base64,${item.dataBase64}`"
              alt=""
            />
            <span v-else class="ai-file__icon">{{ item.kind === 'image' ? '图' : '文' }}</span>
            <span class="ai-file__name" :title="item.name">{{ item.name }}</span>
            <el-button size="small" text type="danger" @click="removeAiAttachment(item.id)">
              移除
            </el-button>
          </li>
        </ul>
        <p v-if="aiError && !aiPreviewVisible" class="page__error" @click="aiError = null">
          [{{ aiError.code }}] {{ aiError.message }}
        </p>

        <p class="quick__divider">或手动添加</p>
      <div class="quick__row">
        <el-input
          v-model="oneOffForm.title"
          maxlength="50"
          placeholder="提醒我…（如「吃一个苹果」）"
          @keyup.enter="createOneOff"
        />
        <el-date-picker
          v-model="oneOffForm.when"
          class="quick__time"
          type="datetime"
          value-format="YYYY-MM-DD HH:mm"
          format="YYYY-MM-DD HH:mm"
          placeholder="选择日期时间"
        />
        <el-button type="primary" :loading="oneOffSaving" @click="createOneOff">添加</el-button>
      </div>
      <el-input
        v-model="oneOffForm.content"
        maxlength="200"
        placeholder="补充说明（可选）"
        @keyup.enter="createOneOff"
      />
      <p v-if="oneOffsError && !oneOffDialogVisible" class="page__error" @click="oneOffsError = null">
        [{{ oneOffsError.code }}] {{ oneOffsError.message }}
      </p>
      <p v-if="oneOffHint" class="page__hint">{{ oneOffHint }}</p>
      <p v-if="oneOffsLoading" class="quick__note">正在加载一次性提醒…</p>
      </div>
    </el-card>

    <section v-if="oneOffs.length" class="block">
      <h2 class="block__title">
        即将到来
        <el-tag size="small" type="warning">{{ oneOffs.length }}</el-tag>
      </h2>
      <ul class="oneoffs">
        <li
          v-for="item in oneOffs"
          :key="item.id"
          class="oneoff"
          :class="{ 'oneoff--missed': item.missed }"
        >
          <span class="oneoff__time">{{ formatDateTime(item.once_at) }}</span>
          <div class="oneoff__body">
            <p class="oneoff__title">
              {{ item.title }}
              <el-tag v-if="item.missed" size="small" type="info">已错过</el-tag>
            </p>
            <p v-if="item.content" class="oneoff__content">{{ item.content }}</p>
          </div>
          <div class="oneoff__actions">
            <el-button v-if="!item.missed" size="small" @click="openEditOneOff(item)">编辑</el-button>
            <el-button size="small" text type="danger" @click="deleteOneOff(item)">删除</el-button>
          </div>
        </li>
      </ul>
    </section>

    <Skeleton v-if="store.loading && !store.stats" :rows="4" />

    <template v-else>
      <el-card v-if="scenario" class="card" shadow="never">
        <div class="card__head">
          <div class="card__intro">
            <h2 class="card__title">{{ scenario.name }}</h2>
            <p class="card__summary">{{ scenario.summary || '这个场景还没有生成计划。' }}</p>
          </div>
          <router-link class="card__link" to="/plan">查看计划</router-link>
        </div>

        <div class="metrics">
          <div class="metric">
            <span class="metric__label">今日完成</span>
            <span class="metric__value">{{ totalDone }} / {{ totalTarget || '—' }}</span>
          </div>
          <div class="metric">
            <span class="metric__label">待处理</span>
            <span class="metric__value">{{ pendingCount }}</span>
          </div>
          <div class="metric">
            <span class="metric__label">下一条 · {{ nextTitle || '—' }}</span>
            <span class="metric__value metric__value--mono">{{ countdown }}</span>
          </div>
        </div>

        <el-progress :percentage="overallPercent" :stroke-width="6" :show-text="false" />
      </el-card>

      <div v-else class="card card--empty">
        <EmptyState description="还没有启用的场景。到「场景」页描述你的处境，让 AI 生成一套提醒计划。" />
        <router-link class="card__link" to="/scenarios">去创建场景</router-link>
      </div>

      <section ref="pendingSectionRef" v-if="store.pending.length" class="block">
        <h2 class="block__title">
          待处理
          <el-tag size="small" type="warning">{{ store.pending.length }}</el-tag>
        </h2>
        <ul class="fires">
          <li v-for="fire in store.pending" :key="fire.fire_id" class="fire">
            <span class="fire__icon">{{ kindMeta(fire.kind).icon }}</span>
            <div class="fire__body">
              <p class="fire__title">
                {{ fire.title }}
                <el-tag v-if="fire.is_one_off" size="small" type="warning">一次性</el-tag>
              </p>
              <p v-if="fire.content" class="fire__content">{{ fire.content }}</p>
              <p class="fire__meta">{{ pendingMeta(fire) }}</p>
            </div>
            <div class="fire__actions">
              <el-button
                size="small"
                type="primary"
                :loading="store.acting === fire.fire_id"
                @click="store.act(fire.fire_id, CheckInAction.Done)"
              >
                完成
              </el-button>
              <el-button
                size="small"
                :loading="store.acting === fire.fire_id"
                @click="store.act(fire.fire_id, CheckInAction.Snooze)"
              >
                推迟
              </el-button>
              <el-button
                size="small"
                text
                :loading="store.acting === fire.fire_id"
                @click="store.act(fire.fire_id, CheckInAction.Skip)"
              >
                跳过
              </el-button>
            </div>
          </li>
        </ul>
      </section>

      <!-- 已错过（US-6）：智能免打扰期间跳过的每日单点与一次性提醒。
           不补弹、不参与打卡——只是保留「今天有过这条安排」的可见回执。 -->
      <section v-if="missedToday.length" class="block block--missed">
        <h2 class="block__title">
          已错过
          <el-tag size="small" type="info">{{ missedToday.length }}</el-tag>
          <span class="block__subtitle">免打扰期间跳过，不补弹</span>
        </h2>
        <ul class="missed">
          <li v-for="item in missedToday" :key="item.fire_id" class="missed__row">
            <span class="missed__time">{{ missedTime(item.fire_at) }}</span>
            <span class="missed__icon">{{ kindMeta(item.kind).icon }}</span>
            <span class="missed__title">{{ item.title }}</span>
            <el-tag v-if="item.is_one_off" size="small" type="warning">一次性</el-tag>
          </li>
        </ul>
      </section>

      <section v-if="progress.length" class="block">
        <h2 class="block__title">今日进度</h2>
        <ul class="rows">
          <li v-for="item in progress" :key="item.reminder_id" class="row">
            <span class="row__icon">{{ kindMeta(item.kind).icon }}</span>
            <div class="row__body">
              <div class="row__head">
                <span class="row__title">{{ item.title }}</span>
                <span class="row__count">
                  {{ item.done_count }} / {{ item.daily_target || '—' }}
                  <template v-if="item.daily_target">{{ kindMeta(item.kind).unit }}</template>
                  <template v-if="item.streak > 1"> · 连续 {{ item.streak }} 天</template>
                </span>
              </div>
              <el-progress
                :percentage="progressPercent(item.done_count, item.daily_target)"
                :stroke-width="4"
                :show-text="false"
              />
            </div>
            <span class="row__next" :title="'下一个触发时间'">{{ formatClock(item.next_fire_at) }}</span>
          </li>
        </ul>
      </section>
    </template>

    <el-dialog v-model="aiPreviewVisible" title="AI 草稿" width="640px">
      <div class="ai-preview">
        <p v-if="aiSummary" class="quick__note">{{ aiSummary }}</p>
        <p v-if="aiOmitted > 0" class="page__error">
          有 {{ aiOmitted }} 条被省略，请拆分输入后重试
        </p>
        <ul class="ai-drafts">
          <li v-for="(row, index) in aiRows" :key="index" class="ai-draft">
            <el-checkbox v-model="row.selected" />
            <div class="ai-draft__body">
              <div class="ai-draft__headline">
                <el-tag size="small" :type="row.scheduleType === 'daily' ? 'success' : 'warning'">
                  {{ row.scheduleType === 'daily' ? '周期' : '一次性' }}
                </el-tag>
                <el-input v-model="row.title" maxlength="50" placeholder="标题" />
              </div>
              <el-input v-model="row.content" maxlength="200" placeholder="正文（可选）" />
              <el-date-picker
                v-if="row.scheduleType === 'once'"
                v-model="row.when"
                class="quick__time"
                type="datetime"
                value-format="YYYY-MM-DD HH:mm"
                format="YYYY-MM-DD HH:mm"
                placeholder="选择日期时间"
              />
              <template v-else>
                <div class="ai-draft__daily">
                  <el-input-number
                    v-model="row.intervalMin"
                    :min="0"
                    :max="720"
                    :step="5"
                    controls-position="right"
                  />
                  <span class="quick__note">分钟</span>
                  <el-input v-model="row.windowStart" class="ai-draft__clock" placeholder="09:00" />
                  <span class="quick__note">至</span>
                  <el-input v-model="row.windowEnd" class="ai-draft__clock" placeholder="18:00" />
                  <el-input-number
                    v-model="row.dailyTarget"
                    :min="0"
                    :max="99"
                    controls-position="right"
                  />
                  <span class="quick__note">次/日</span>
                </div>
                <div class="ai-draft__scenario">
                  <el-select v-model="row.scenarioId" size="small" placeholder="目标场景">
                    <el-option
                      v-for="item in scenarios"
                      :key="item.id"
                      :label="item.name"
                      :value="item.id"
                    />
                  </el-select>
                  <router-link
                    v-if="scenarios.length === 0"
                    class="card__link"
                    :to="{ path: '/scenarios', query: { description: aiDescription } }"
                  >
                    先创建场景
                  </router-link>
                </div>
              </template>
            </div>
            <el-button size="small" text type="danger" @click="aiRows.splice(index, 1)">移除</el-button>
          </li>
        </ul>
        <p v-if="aiError" class="page__error" @click="aiError = null">
          [{{ aiError.code }}] {{ aiError.message }}
        </p>
      </div>
      <template #footer>
        <el-button :disabled="aiSaving" @click="aiPreviewVisible = false">取消</el-button>
        <el-button
          type="primary"
          :loading="aiSaving"
          :disabled="!aiRows.some((row) => row.selected)"
          @click="saveAiDrafts"
        >
          写入提醒
        </el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="oneOffDialogVisible" title="编辑一次性提醒" width="460px">
      <div class="quick quick--dialog">
        <el-input v-model="oneOffEditForm.title" maxlength="50" placeholder="提醒我…" />
        <el-input v-model="oneOffEditForm.content" maxlength="200" placeholder="补充说明（可选）" />
        <el-date-picker
          v-model="oneOffEditForm.when"
          class="quick__time"
          type="datetime"
          value-format="YYYY-MM-DD HH:mm"
          format="YYYY-MM-DD HH:mm"
          placeholder="选择日期时间"
        />
        <p v-if="oneOffsError" class="page__error" @click="oneOffsError = null">
          [{{ oneOffsError.code }}] {{ oneOffsError.message }}
        </p>
      </div>
      <template #footer>
        <el-button :disabled="oneOffSaving" @click="oneOffDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="oneOffSaving" @click="submitOneOffEdit">保存</el-button>
      </template>
    </el-dialog>
  </section>
</template>

<style scoped>
.page__head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--space-4);
  margin-bottom: var(--space-4);
}

.page__title {
  font-size: var(--font-size-xl);
  margin: 0 0 var(--space-1);
}

.page__desc {
  margin: 0;
  color: var(--text-2);
  font-size: var(--font-size-sm);
}

.page__error {
  margin: 0 0 var(--space-4);
  color: var(--color-danger);
  cursor: pointer;
}

.card {
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-lg);
  background: var(--bg-raised);
  margin-bottom: var(--space-5);
}

.card--empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--space-2);
  padding-bottom: var(--space-5);
}

.card__head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--space-4);
  margin-bottom: var(--space-4);
}

.card__intro {
  min-width: 0;
}

.card__title {
  font-size: var(--font-size-lg);
  margin: 0 0 var(--space-1);
}

.card__summary {
  margin: 0;
  color: var(--text-2);
  font-size: var(--font-size-sm);
  line-height: 1.6;
}

.card__link {
  flex: none;
  color: var(--color-primary);
  font-size: var(--font-size-sm);
  white-space: nowrap;
}

.metrics {
  display: flex;
  gap: var(--space-6);
  margin-bottom: var(--space-4);
}

.metric {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
}

.metric__label {
  color: var(--text-3);
  font-size: var(--font-size-xs);
}

.metric__value {
  font-size: var(--font-size-lg);
  color: var(--text-1);
}

.metric__value--mono {
  font-family: var(--font-mono);
}

.block {
  margin-bottom: var(--space-5);
}

.block__title {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--font-size-md);
  color: var(--text-2);
  margin: 0 0 var(--space-3);
}

/* ---- 已错过（US-6） ---- */
/* 视觉上刻意比【待处理】低一档：灰色、无边框、无 hover。
   错过项没有可操作性（不能"完成"过去时刻的提醒），高对比会让人误点。 */
.block--missed .block__subtitle {
  font-size: var(--font-size-xs);
  color: var(--text-3);
  font-weight: 400;
  margin-left: var(--space-1);
}

.missed {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
}

.missed__row {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-2);
  border-radius: var(--radius-sm);
  color: var(--text-3);
  font-size: var(--font-size-sm);
  opacity: 0.75;
}

.missed__time {
  font-variant-numeric: tabular-nums;
  color: var(--text-3);
  min-width: 4.5em;
}

.missed__icon {
  font-size: var(--font-size-md);
}

.missed__title {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* ---- 快速添加（一次性提醒） ---- */
.sit-consent {
  margin-bottom: var(--space-4);
  border-color: var(--color-primary-300);
  background: var(--color-primary-soft);
}

.sit-consent__title {
  margin: 0 0 var(--space-2);
  font-size: var(--font-size-md);
}

.sit-consent__desc {
  margin: 0;
  color: var(--text-2);
  font-size: var(--font-size-sm);
  line-height: 1.7;
}

.sit-consent__note {
  margin: var(--space-2) 0 0;
  color: var(--text-3);
  font-size: var(--font-size-xs);
  line-height: 1.7;
}

.sit-consent__actions {
  display: flex;
  justify-content: flex-end;
  gap: var(--space-2);
  margin-top: var(--space-3);
}

.quick {
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-lg);
  background: var(--bg-raised);
  margin-bottom: var(--space-5);
}

.quick__head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: var(--space-3);
  margin-bottom: var(--space-3);
}

.quick__title {
  font-size: var(--font-size-md);
  color: var(--text-2);
  margin: 0;
}

.quick__note {
  color: var(--text-3);
  font-size: var(--font-size-xs);
}

.quick__row {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  margin-bottom: var(--space-2);
}

.quick__row :deep(.el-input) {
  flex: 1;
}

.quick__time {
  flex: none;
  width: 200px;
}

.quick__divider {
  margin: var(--space-3) 0 var(--space-2);
  color: var(--text-3);
  font-size: var(--font-size-xs);
}

.quick__drop {
  min-width: 0;
}

.ai-files {
  list-style: none;
  margin: var(--space-2) 0 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
}

.ai-file {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-1) var(--space-2);
  background: var(--bg-hover);
  border-radius: var(--radius-sm);
}

.ai-file__icon {
  flex: none;
  width: 20px;
  height: 20px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: var(--radius-sm);
  background: var(--bg-active);
  color: var(--text-2);
  font-size: var(--font-size-xs);
}

.ai-file__thumb {
  flex: none;
  width: 28px;
  height: 28px;
  object-fit: cover;
  border-radius: var(--radius-sm);
  border: 1px solid var(--border-subtle);
}

.ai-file__name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: var(--font-size-sm);
  color: var(--text-1);
}

.ai-preview {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}

.ai-drafts {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  max-height: 52vh;
  overflow: auto;
}

.ai-draft {
  display: flex;
  align-items: flex-start;
  gap: var(--space-2);
  padding: var(--space-3);
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-md);
  background: var(--bg-raised);
}

.ai-draft :deep(.el-checkbox) {
  margin-top: var(--space-1);
}

.ai-draft__body {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.ai-draft .quick__time {
  width: 100%;
}

.ai-draft__headline {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.ai-draft__headline :deep(.el-input) {
  flex: 1;
}

.ai-draft__daily {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
}

.ai-draft__daily :deep(.el-input-number) {
  width: 130px;
}

.ai-draft__clock {
  width: 88px;
  flex: none;
}

.ai-draft__scenario {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.ai-draft__scenario :deep(.el-select) {
  width: 200px;
}

.quick--dialog {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  border: 0;
  background: transparent;
  margin: 0;
}

.quick--dialog .quick__time {
  width: 100%;
}

/* ---- 即将到来（一次性提醒） ---- */
.oneoffs {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.oneoff {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-3) var(--space-4);
  background: var(--bg-raised);
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-md);
}

.oneoff--missed {
  opacity: 0.65;
  border-style: dashed;
}

.oneoff__time {
  flex: none;
  width: 150px;
  color: var(--text-3);
  font-size: var(--font-size-xs);
  font-family: var(--font-mono);
}

.oneoff__body {
  flex: 1;
  min-width: 0;
}

.oneoff__title {
  margin: 0;
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--font-size-md);
}

.oneoff__content {
  margin: var(--space-1) 0 0;
  color: var(--text-2);
  font-size: var(--font-size-sm);
}

.oneoff__actions {
  flex: none;
  display: flex;
  gap: var(--space-2);
}

.fires,
.rows {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.fire {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-3) var(--space-4);
  background: var(--bg-raised);
  border: 1px solid var(--color-warning);
  border-radius: var(--radius-md);
}

.fire__icon,
.row__icon {
  flex: none;
  width: 28px;
  height: 28px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: var(--radius-sm);
  background: var(--bg-hover);
  color: var(--text-2);
  font-size: var(--font-size-sm);
}

.fire__body {
  flex: 1;
  min-width: 0;
}

.fire__title {
  margin: 0;
  font-size: var(--font-size-md);
}

.fire__content {
  margin: var(--space-1) 0 0;
  color: var(--text-2);
  font-size: var(--font-size-sm);
}

.fire__meta {
  margin: var(--space-1) 0 0;
  color: var(--text-3);
  font-size: var(--font-size-xs);
}

.fire__actions {
  flex: none;
  display: flex;
  gap: var(--space-2);
}

.row {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-3) var(--space-4);
  background: var(--bg-raised);
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-md);
}

.row__body {
  flex: 1;
  min-width: 0;
}

.row__head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: var(--space-3);
  margin-bottom: var(--space-2);
}

.row__title {
  font-size: var(--font-size-md);
}

.row__count {
  color: var(--text-2);
  font-size: var(--font-size-sm);
}

.row__next {
  flex: none;
  color: var(--text-3);
  font-family: var(--font-mono);
  font-size: var(--font-size-xs);
}
</style>
