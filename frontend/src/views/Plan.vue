<script lang="ts" setup>
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import Skeleton from '../components/Skeleton.vue'
import EmptyState from '../components/EmptyState.vue'
import {
  CreateReminder,
  DeleteReminder,
  GeneratePlanFromInput,
  ListReminders,
  ListScenarios,
  SelectFile,
  SetReminderEnabled,
  UpdateReminder,
} from '../../bindings/morrow/app.js'
import { ErrorCode, invoke, type AppError } from '../lib/invoke'
import { MAX_ATTACHMENTS, attachmentKind, fileName } from '../lib/attachment'
import { KIND_OPTIONS, ReminderEvent, ReminderKind, kindMeta } from '../lib/reminder'
import { useEvent } from '../composables/useEvent'
import { logInfo } from '../lib/log'

// 计划页：手工维护某个场景下的提醒项。
// AI 生成的计划落在这里，用户随后可以逐条微调（间隔/时段/目标），
// 这也是「AI 只生成一次、之后完全本地可控」的另一半含义。
//
// 本页同时也是 AI 多模态生成入口：文字 + 图片/文件 → 草稿 → 用户确认后落库。
// 草稿不直接写库（见 Go 侧 GeneratePlanFromInput 的注释），避免一次试错替换掉现有计划。
type ScenarioRow = NonNullable<Awaited<ReturnType<typeof ListScenarios>>>[number]
type ReminderRow = NonNullable<Awaited<ReturnType<typeof ListReminders>>>[number]
type PlanDraft = NonNullable<Awaited<ReturnType<typeof GeneratePlanFromInput>>>
// wails3 生成的绑定把 Go 的 []ReminderDraft 映射成 ReminderDraft[] | null，
// 直接对 PlanDraft['reminders'] 取 [number] 会在 null 上找不到索引签名。
// 运行时第 343 行已用 plan?.reminders ?? [] 兜底，这里用 NonNullable 对齐类型即可。
type DraftRow = NonNullable<PlanDraft['reminders']>[number]

/** 编辑表单。字段名与 Go 的 service.ReminderInput 的 JSON tag 一一对应。 */
interface ReminderForm {
  id: number
  scenario_id: number
  title: string
  content: string
  kind: string
  interval_min: number
  window_start: string
  window_end: string
  daily_target: number
  enabled: boolean
  once_at: number
  sort_order: number
}

/** 已选附件（只在前端用于展示；Go 侧按路径重新读取）。 */
interface AttachmentRow {
  /** 磁盘路径，提交时原样传给后端。 */
  path: string
  /** 文件名（仅展示）。 */
  name: string
  /** 类型：图片走多模态，文本拼进 prompt。 */
  kind: 'text' | 'image'
}

const route = useRoute()

const scenarios = ref<ScenarioRow[]>([])
const reminders = ref<ReminderRow[]>([])
const selectedID = ref(0)
const loading = ref(false)
const saving = ref(false)
const error = ref<AppError | null>(null)
const hint = ref('')

const dialogVisible = ref(false)
// editing 区分"编辑现有条目"与"新增"：决定提交时调 Update 还是 Create。
const editing = ref(false)
const form = ref<ReminderForm>(blankForm(0))

// ---- AI 生成相关状态 ----
const aiVisible = ref(false)
const aiDescription = ref('')
const aiAttachments = ref<AttachmentRow[]>([])
const aiDrafts = ref<DraftRow[]>([])
const aiSummary = ref('')
const aiGenerating = ref(false)
const aiSaving = ref(false)
const aiError = ref<AppError | null>(null)
const aiHint = ref('')

const selectedScenario = computed(() => scenarios.value.find((item) => item.id === selectedID.value) ?? null)
const hasReminders = computed(() => reminders.value.length > 0)
const aiReady = computed(
  () => !aiGenerating.value && (aiDescription.value.trim().length > 0 || aiAttachments.value.length > 0),
)

/** 空表单（默认值取最常见的办公时段，减少用户输入）。 */
function blankForm(scenarioID: number): ReminderForm {
  return {
    id: 0,
    scenario_id: scenarioID,
    title: '',
    content: '',
    kind: ReminderKind.Custom,
    interval_min: 60,
    window_start: '09:00',
    window_end: '18:00',
    daily_target: 0,
    enabled: true,
    once_at: 0,
    sort_order: 0,
  }
}

/** 加载场景列表，并确定当前选中的场景。 */
async function loadScenarios(): Promise<void> {
  try {
    scenarios.value = (await invoke(() => ListScenarios(), 'ListScenarios')) ?? []
  } catch (e) {
    error.value = e as AppError
    return
  }
  if (selectedID.value) return
  // 选中优先级：URL 参数（从场景页跳过来）> 当前启用场景 > 第一个场景。
  const raw = route.query.scenario
  const fromQuery = typeof raw === 'string' ? Number(raw) : 0
  const active = scenarios.value.find((item) => item.enabled)
  selectedID.value = fromQuery || active?.id || scenarios.value[0]?.id || 0
}

/** 加载所选场景的提醒项。 */
async function loadReminders(): Promise<void> {
  if (!selectedID.value) {
    reminders.value = []
    return
  }
  loading.value = true
  try {
    reminders.value = (await invoke(() => ListReminders(selectedID.value), 'ListReminders')) ?? []
  } catch (e) {
    error.value = e as AppError
  } finally {
    loading.value = false
  }
}

/** 打开"新增"对话框。 */
function openCreate(): void {
  error.value = null
  hint.value = ''
  editing.value = false
  form.value = blankForm(selectedID.value)
  form.value.sort_order = reminders.value.length
  dialogVisible.value = true
}

/** 打开"编辑"对话框。 */
function openEdit(row: ReminderRow): void {
  error.value = null
  hint.value = ''
  editing.value = true
  form.value = {
    id: row.id,
    scenario_id: row.scenario_id ?? selectedID.value,
    title: row.title,
    content: row.content ?? '',
    kind: row.kind,
    interval_min: row.interval_min,
    window_start: row.window_start,
    window_end: row.window_end,
    daily_target: row.daily_target,
    enabled: row.enabled,
    once_at: row.once_at,
    sort_order: row.sort_order,
  }
  dialogVisible.value = true
}

/** 提交表单（新增或编辑）。校验全部在后端：错误信息更权威，也不会两处漂移。 */
async function submit(): Promise<void> {
  saving.value = true
  error.value = null
  try {
    const payload = { ...form.value }
    if (editing.value) {
      await invoke(() => UpdateReminder(payload), 'UpdateReminder')
    } else {
      await invoke(() => CreateReminder(payload), 'CreateReminder')
    }
    dialogVisible.value = false
    hint.value = editing.value ? '已保存修改' : '已新增提醒'
    logInfo('提醒项已保存', { scenario_id: payload.scenario_id, id: payload.id })
    await loadReminders()
  } catch (e) {
    error.value = e as AppError
  } finally {
    saving.value = false
  }
}

/** 启用/停用单条提醒。 */
async function toggleEnabled(row: ReminderRow): Promise<void> {
  error.value = null
  try {
    await invoke(() => SetReminderEnabled(row.id, !row.enabled), 'SetReminderEnabled')
    await loadReminders()
  } catch (e) {
    error.value = e as AppError
  }
}

/** 删除单条提醒。 */
async function removeReminder(row: ReminderRow): Promise<void> {
  error.value = null
  try {
    await invoke(() => DeleteReminder(row.id), 'DeleteReminder')
    hint.value = `已删除「${row.title}」`
    await loadReminders()
  } catch (e) {
    error.value = e as AppError
  }
}

/** 间隔的可读文案（0 = 每天在起点只提醒一次）。 */
function intervalText(minutes: number): string {
  if (minutes <= 0) return '每天一次'
  if (minutes % 60 === 0) return `每 ${minutes / 60} 小时`
  return `每 ${minutes} 分钟`
}

/** 列表里的一行元信息：每日单点只显示起点时刻，不显示时段区间。 */
function metaText(row: ReminderRow): string {
  if (row.interval_min <= 0) return `${row.window_start} · 每天一次`
  const target = row.daily_target ? ` · 目标 ${row.daily_target} ${kindMeta(row.kind).unit}` : ''
  return `${row.window_start}–${row.window_end} · ${intervalText(row.interval_min)}${target}`
}

/** 打开 AI 生成面板。 */
function openAI(): void {
  aiError.value = null
  aiHint.value = ''
  aiDrafts.value = []
  aiSummary.value = ''
  aiVisible.value = true
}

/**
 * 添加一个附件。
 *
 * 复用系统的单文件选择框、可重复点击添加：v3 的多选能力在各平台实现不一，
 * 「一次一个」虽然多几次点击，但行为在每个平台上都一样。
 */
async function addAttachment(): Promise<void> {
  aiError.value = null
  aiHint.value = ''
  try {
    const path = await invoke(() => SelectFile('选择图片或文件'), 'SelectFile')
    if (!path) return // 用户取消
    if (aiAttachments.value.some((item) => item.path === path)) {
      aiHint.value = '这个文件已经添加过了'
      return
    }
    if (aiAttachments.value.length >= MAX_ATTACHMENTS) {
      aiHint.value = `一次最多添加 ${MAX_ATTACHMENTS} 个附件`
      return
    }
    aiAttachments.value.push({ path, name: fileName(path), kind: attachmentKind(path) })
  } catch (e) {
    aiError.value = e as AppError
  }
}

/** 移除一个附件。 */
function removeAttachment(index: number): void {
  aiAttachments.value.splice(index, 1)
}

/** 让后端生成草稿（不落库）。 */
async function generateDrafts(): Promise<void> {
  if (!selectedID.value) return
  const description = aiDescription.value.trim()
  if (!description && aiAttachments.value.length === 0) {
    aiError.value = { code: ErrorCode.Validation, message: '先写一句描述，或添加一张图片/一个文件' }
    return
  }
  aiGenerating.value = true
  aiError.value = null
  aiHint.value = ''
  aiDrafts.value = []
  aiSummary.value = ''
  try {
    const paths = aiAttachments.value.map((item) => item.path)
    const plan = await invoke(() => GeneratePlanFromInput(description, paths), 'GeneratePlanFromInput')
    aiDrafts.value = plan?.reminders ?? []
    aiSummary.value = plan?.summary ?? ''
    if (aiDrafts.value.length === 0) {
      aiHint.value = 'AI 没有给出可用的提醒项，换个说法再试一次'
    }
    logInfo('AI 草稿已生成', { scenario_id: selectedID.value, count: aiDrafts.value.length })
  } catch (e) {
    aiError.value = e as AppError
  } finally {
    aiGenerating.value = false
  }
}

/** 把一条草稿转成 CreateReminder 的入参。 */
function draftToInput(draft: DraftRow, sortOrder: number): ReminderForm {
  return {
    id: 0,
    scenario_id: selectedID.value,
    title: draft.title,
    content: draft.content,
    kind: draft.kind,
    interval_min: draft.interval_min,
    window_start: draft.window_start,
    window_end: draft.window_end,
    daily_target: draft.daily_target,
    enabled: true,
    once_at: 0,
    sort_order: sortOrder,
  }
}

/**
 * 用户确认后逐条落库。
 *
 * 【为什么逐条调 CreateReminder 而不是新增一个批量绑定】批量写要么在 Go 侧
 * 复制一遍校验，要么绕过校验——前者是第二处真相，后者让 AI 产物成为唯一
 * 未经校验的写入路径。逐条走同一条入口，代价只是几次本地 IPC。
 */
async function saveDrafts(): Promise<void> {
  if (!selectedID.value || aiDrafts.value.length === 0) return
  aiSaving.value = true
  aiError.value = null
  const total = aiDrafts.value.length
  try {
    let sortOrder = reminders.value.length
    for (const draft of aiDrafts.value) {
      const payload = draftToInput(draft, sortOrder)
      sortOrder += 1
      await invoke(() => CreateReminder(payload), 'CreateReminder')
    }
    aiVisible.value = false
    aiDrafts.value = []
    aiDescription.value = ''
    aiAttachments.value = []
    hint.value = `已新增 ${total} 条提醒`
    await loadReminders()
  } catch (e) {
    aiError.value = e as AppError
  } finally {
    aiSaving.value = false
  }
}

/** 草稿区里的一条预览文案。 */
function draftMeta(draft: DraftRow): string {
  if (draft.interval_min <= 0) return `${draft.window_start} · 每天一次`
  return `${draft.window_start}–${draft.window_end} · ${intervalText(draft.interval_min)}`
}

watch(selectedID, () => {
  void loadReminders()
})

// 别处改动（AI 重新生成、场景删除）时同步刷新本页。
useEvent(ReminderEvent.Changed, () => {
  void loadScenarios()
  void loadReminders()
})

onMounted(async () => {
  await loadScenarios()
  await loadReminders()
})
</script>

<template>
  <section class="page">
    <header class="page__head">
      <div>
        <h1 class="page__title">计划</h1>
        <p class="page__desc">
          {{ selectedScenario?.summary || '选择场景后可以逐条调整提醒的间隔、时段与每日目标。' }}
        </p>
      </div>
      <div class="page__tools">
        <el-select v-model="selectedID" class="page__select" size="small" placeholder="选择场景">
          <el-option v-for="item in scenarios" :key="item.id" :label="item.name" :value="item.id" />
        </el-select>
        <el-button size="small" :disabled="!selectedID" @click="openAI">AI 生成</el-button>
        <el-button size="small" type="primary" :disabled="!selectedID" @click="openCreate">
          新增提醒
        </el-button>
      </div>
    </header>

    <p v-if="error" class="page__error">[{{ error.code }}] {{ error.message }}</p>
    <p v-if="hint" class="page__hint">{{ hint }}</p>

    <EmptyState v-if="!selectedID" description="还没有场景。先到「场景」页创建一个。" />

    <Skeleton v-else-if="loading && !hasReminders" :rows="4" />

    <EmptyState v-else-if="!hasReminders" description="这个场景还没有提醒项。可以用 AI 生成，也可以手动新增一条。" />

    <ul v-else class="list">
      <li v-for="row in reminders" :key="row.id" class="item" :class="{ 'item--off': !row.enabled }">
        <span class="item__icon">{{ kindMeta(row.kind).icon }}</span>
        <div class="item__body">
          <div class="item__head">
            <span class="item__title">{{ row.title }}</span>
            <el-tag size="small">{{ kindMeta(row.kind).label }}</el-tag>
            <el-tag v-if="!row.enabled" size="small" type="info">已停用</el-tag>
          </div>
          <p v-if="row.content" class="item__content">{{ row.content }}</p>
          <p class="item__meta">{{ metaText(row) }}</p>
        </div>
        <div class="item__actions">
          <el-button size="small" @click="openEdit(row)">编辑</el-button>
          <el-button size="small" @click="toggleEnabled(row)">
            {{ row.enabled ? '停用' : '启用' }}
          </el-button>
          <el-button size="small" text type="danger" @click="removeReminder(row)">删除</el-button>
        </div>
      </li>
    </ul>

    <el-dialog v-model="dialogVisible" :title="editing ? '编辑提醒' : '新增提醒'" width="520px">
      <div class="form">
        <label class="form__row">
          <span class="form__label">标题</span>
          <el-input v-model="form.title" maxlength="50" placeholder="如「喝水」" />
        </label>
        <label class="form__row">
          <span class="form__label">正文</span>
          <el-input
            v-model="form.content"
            type="textarea"
            :rows="2"
            maxlength="200"
            placeholder="如「起身倒一杯温水，慢慢喝完」"
          />
        </label>
        <label class="form__row">
          <span class="form__label">类型</span>
          <el-select v-model="form.kind" class="form__control">
            <el-option v-for="option in KIND_OPTIONS" :key="option.value" :label="option.label" :value="option.value" />
          </el-select>
        </label>
        <label class="form__row">
          <span class="form__label">间隔</span>
          <el-input-number v-model="form.interval_min" :min="0" :max="720" :step="5" controls-position="right" />
          <span class="form__hint">分钟；填 0 表示每天同一时刻只提醒一次</span>
        </label>
        <label class="form__row">
          <span class="form__label">时段</span>
          <el-input v-model="form.window_start" class="form__time" placeholder="09:00" />
          <span class="form__hint">至</span>
          <el-input v-model="form.window_end" class="form__time" placeholder="18:00" />
        </label>
        <label class="form__row">
          <span class="form__label">每日目标</span>
          <el-input-number v-model="form.daily_target" :min="0" :max="99" controls-position="right" />
          <span class="form__hint">次；填 0 表示不设目标</span>
        </label>
        <label class="form__row">
          <span class="form__label">参与调度</span>
          <el-switch v-model="form.enabled" />
        </label>
      </div>
      <template #footer>
        <el-button :disabled="saving" @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="submit">保存</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="aiVisible" title="用 AI 生成提醒" width="620px">
      <div class="ai">
        <p class="ai__note">
          写一句描述，或添加排班表、体检报告、日程截图之类的图片/文件，AI 会生成一份草稿。
          草稿确认后才会写入计划。
        </p>
        <el-input
          v-model="aiDescription"
          type="textarea"
          :rows="4"
          maxlength="2000"
          show-word-limit
          placeholder="例如「我在备孕，不能久坐，每小时喝一杯水」"
        />
        <div class="ai__actions">
          <el-button size="small" @click="addAttachment">添加图片/文件</el-button>
          <span class="ai__hint">支持 txt / md / csv / json / log 与 png / jpg / webp / gif；不支持 PDF</span>
        </div>

        <ul v-if="aiAttachments.length" class="ai__files">
          <li v-for="(item, index) in aiAttachments" :key="item.path" class="ai__file">
            <span class="ai__file-icon">{{ item.kind === 'image' ? '图' : '文' }}</span>
            <span class="ai__file-name" :title="item.path">{{ item.name }}</span>
            <el-button size="small" text type="danger" @click="removeAttachment(index)">移除</el-button>
          </li>
        </ul>

        <p v-if="aiError" class="page__error">[{{ aiError.code }}] {{ aiError.message }}</p>
        <p v-if="aiError?.detail" class="ai__detail">{{ aiError.detail }}</p>
        <p v-if="aiHint" class="page__hint">{{ aiHint }}</p>

        <div v-if="aiDrafts.length" class="ai__drafts">
          <p class="ai__drafts-title">
            草稿 {{ aiDrafts.length }} 条
            <span v-if="aiSummary" class="ai__summary">· {{ aiSummary }}</span>
          </p>
          <ul class="ai__draft-list">
            <li v-for="(draft, index) in aiDrafts" :key="`${draft.title}-${index}`" class="ai__draft">
              <div class="ai__draft-head">
                <span class="ai__draft-title">{{ draft.title }}</span>
                <el-tag size="small">{{ kindMeta(draft.kind).label }}</el-tag>
              </div>
              <p v-if="draft.content" class="ai__draft-content">{{ draft.content }}</p>
              <p class="ai__draft-meta">{{ draftMeta(draft) }}</p>
            </li>
          </ul>
        </div>
      </div>
      <template #footer>
        <el-button :disabled="aiGenerating || aiSaving" @click="aiVisible = false">关闭</el-button>
        <el-button :loading="aiGenerating" :disabled="!aiReady" @click="generateDrafts">
          {{ aiDrafts.length ? '重新生成' : 'AI 生成' }}
        </el-button>
        <el-button
          type="primary"
          :loading="aiSaving"
          :disabled="aiDrafts.length === 0"
          @click="saveDrafts"
        >
          确认写入计划
        </el-button>
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
  max-width: 560px;
  color: var(--text-2);
  font-size: var(--font-size-sm);
  line-height: 1.6;
}

.page__tools {
  flex: none;
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.page__select {
  width: 180px;
}

.page__error {
  margin: 0 0 var(--space-3);
  color: var(--color-danger);
}

.page__hint {
  margin: 0 0 var(--space-3);
  color: var(--color-success);
  font-size: var(--font-size-sm);
}

.list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.item {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-3) var(--space-4);
  background: var(--bg-raised);
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-md);
}

.item--off {
  opacity: 0.6;
}

.item__icon {
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

.item__body {
  flex: 1;
  min-width: 0;
}

.item__head {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.item__title {
  font-size: var(--font-size-md);
}

.item__content {
  margin: var(--space-1) 0 0;
  color: var(--text-2);
  font-size: var(--font-size-sm);
}

.item__meta {
  margin: var(--space-1) 0 0;
  color: var(--text-3);
  font-size: var(--font-size-xs);
  font-family: var(--font-mono);
}

.item__actions {
  flex: none;
  display: flex;
  gap: var(--space-2);
}

.form {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}

.form__row {
  display: flex;
  align-items: center;
  gap: var(--space-3);
}

.form__label {
  flex: none;
  width: 72px;
  color: var(--text-2);
  font-size: var(--font-size-sm);
}

.form__control {
  width: 100%;
}

.form__time {
  width: 96px;
}

.form__hint {
  color: var(--text-3);
  font-size: var(--font-size-xs);
}

.ai {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}

.ai__note {
  margin: 0;
  color: var(--text-2);
  font-size: var(--font-size-sm);
  line-height: 1.6;
}

.ai__actions {
  display: flex;
  align-items: center;
  gap: var(--space-3);
}

.ai__hint {
  color: var(--text-3);
  font-size: var(--font-size-xs);
}

.ai__files {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
}

.ai__file {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-1) var(--space-2);
  background: var(--bg-hover);
  border-radius: var(--radius-sm);
}

.ai__file-icon {
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

.ai__file-name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: var(--font-size-sm);
  color: var(--text-1);
}

.ai__detail {
  margin: 0;
  color: var(--text-3);
  font-size: var(--font-size-xs);
  font-family: var(--font-mono);
  word-break: break-all;
}

.ai__drafts {
  border-top: 1px solid var(--border-subtle);
  padding-top: var(--space-3);
}

.ai__drafts-title {
  margin: 0 0 var(--space-2);
  color: var(--text-2);
  font-size: var(--font-size-sm);
}

.ai__summary {
  color: var(--text-3);
}

.ai__draft-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.ai__draft {
  padding: var(--space-2) var(--space-3);
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-md);
  background: var(--bg-raised);
}

.ai__draft-head {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.ai__draft-title {
  font-size: var(--font-size-md);
}

.ai__draft-content {
  margin: var(--space-1) 0 0;
  color: var(--text-2);
  font-size: var(--font-size-sm);
}

.ai__draft-meta {
  margin: var(--space-1) 0 0;
  color: var(--text-3);
  font-size: var(--font-size-xs);
  font-family: var(--font-mono);
}
</style>
