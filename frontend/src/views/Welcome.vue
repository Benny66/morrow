<script lang="ts" setup>
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import {
  ApplyScenarioTemplate,
  CompleteOnboarding,
  ListScenarioTemplates,
  PreviewScenarioTemplate,
  SetSitProfileEnabled,
} from '../../bindings/morrow/app.js'
import { invoke, type AppError } from '../lib/invoke'
import { kindMeta } from '../lib/reminder'
import { formatNextFireAt, reminderScheduleText, templateKindLabels } from '../lib/template'
import { logInfo } from '../lib/log'

// 首次引导：工作时段 → 恰好两个关注目标 → 选模板并预览 → 自动启用的计划 → 完成页。
// 全部个性化（过滤/裁切/目标夹取/下一次触发）都由 Go 侧计算，这里只收集输入与渲染。
// 不读取 AI 设置、不调用 AI；没有 Key 也能完成。
type TemplateCatalog = Awaited<ReturnType<typeof ListScenarioTemplates>>
type TemplateRow = NonNullable<TemplateCatalog['templates']>[number]
type PreviewRow = NonNullable<Awaited<ReturnType<typeof PreviewScenarioTemplate>>>
type ApplyResult = NonNullable<Awaited<ReturnType<typeof ApplyScenarioTemplate>>>
type Step = 1 | 2 | 3 | 4 | 'done'

const DEFAULT_WORK_START = '09:00'
const DEFAULT_WORK_END = '18:00'
const MAX_GOALS = 2

const router = useRouter()
const step = ref<Step>(1)
const workStart = ref(DEFAULT_WORK_START)
const workEnd = ref(DEFAULT_WORK_END)
const goals = ref<string[]>([])
const selectedGoals = ref<string[]>([])
const templates = ref<TemplateRow[]>([])
const selectedTemplateKey = ref('')
const preview = ref<PreviewRow | null>(null)
const previewLoading = ref(false)
const templatesLoading = ref(false)
const applying = ref(false)
const skipping = ref(false)
const error = ref<AppError | null>(null)
const appliedResult = ref<ApplyResult | null>(null)
const completionWarning = ref('')

const progressText = computed(() =>
  step.value === 'done' ? '首次设置 · 完成' : `首次设置 · 第 ${step.value} / 4 步`,
)
const title = computed(() => {
  switch (step.value) {
    case 1:
      return '你的一天大概从几点到几点？'
    case 2:
      return '最想先解决哪两件事？'
    case 3:
      return '选一个最接近你一天的模板'
    case 4:
      return '让小满知道你在用什么'
    default:
      return '计划已经就绪'
  }
})
const subtitle = computed(() => {
  switch (step.value) {
    case 1:
      return '工作时段只用于裁切这次生成的提醒窗口，不会改成全局设置。'
    case 2:
      return '恰好选两个；模板会只保留这两类提醒。'
    case 3:
      return '模板是确定性内容，不调用 AI；生成后仍可在计划页逐条修改。'
    case 4:
      return '选择小满能知道多少。应用画像只存本机、可随时关闭和清空。'
    default:
      return '已自动启用，下一次提醒会按下面的时间到来。'
  }
})

/** 表单级时间校验；真正的合法性由 Go 的 Personalize 再验证一次。 */
function clockMinutes(value: string): number | null {
  const match = /^(\d{2}):(\d{2})$/.exec(value)
  if (!match) return null
  const hour = Number(match[1])
  const minute = Number(match[2])
  if (hour > 23 || minute > 59) return null
  return hour * 60 + minute
}

const workWindowValid = computed(() => {
  const start = clockMinutes(workStart.value)
  const end = clockMinutes(workEnd.value)
  return start !== null && end !== null && start < end
})
const goalsFull = computed(() => selectedGoals.value.length >= MAX_GOALS)
const canLeaveGoals = computed(() => selectedGoals.value.length === MAX_GOALS)
const selectedTemplate = computed(
  () => templates.value.find((tpl) => tpl.key === selectedTemplateKey.value) ?? null,
)
const canFinish = computed(
  () => !!preview.value?.valid && !previewLoading.value && !applying.value,
)

function goalLabel(kind: string): string {
  return kindMeta(kind).label
}
function goalIcon(kind: string): string {
  return kindMeta(kind).icon
}
function goalLabels(kinds: string[]): string {
  return kinds.map((kind) => goalLabel(kind)).join('、')
}
function isGoalSelected(kind: string): boolean {
  return selectedGoals.value.includes(kind)
}

function toggleGoal(kind: string): void {
  if (isGoalSelected(kind)) {
    selectedGoals.value = selectedGoals.value.filter((item) => item !== kind)
    return
  }
  if (goalsFull.value) return
  selectedGoals.value = [...selectedGoals.value, kind]
}

async function loadTemplates(): Promise<void> {
  templatesLoading.value = true
  try {
    const catalog = await invoke(() => ListScenarioTemplates(), 'ListScenarioTemplates')
    templates.value = catalog?.templates ?? []
    goals.value = catalog?.goals ?? []
  } catch (e) {
    error.value = e as AppError
  } finally {
    templatesLoading.value = false
  }
}

// 预览请求序号：用户快速改目标/换模板时，丢弃过期响应，避免旧结果覆盖新结果。
let previewSeq = 0
async function loadPreview(): Promise<void> {
  if (!selectedTemplateKey.value) {
    preview.value = null
    return
  }
  const seq = ++previewSeq
  previewLoading.value = true
  error.value = null
  try {
    const result = await invoke(
      () =>
        PreviewScenarioTemplate(
          selectedTemplateKey.value,
          workStart.value,
          workEnd.value,
          selectedGoals.value,
        ),
      'PreviewScenarioTemplate',
    )
    if (seq !== previewSeq) return
    preview.value = result
  } catch (e) {
    if (seq !== previewSeq) return
    preview.value = null
    error.value = e as AppError
  } finally {
    if (seq === previewSeq) previewLoading.value = false
  }
}

function selectTemplate(tpl: TemplateRow): void {
  selectedTemplateKey.value = tpl.key
  void loadPreview()
}

function next(): void {
  error.value = null
  if (step.value === 1) {
    if (!workWindowValid.value) return
    step.value = 2
    return
  }
  if (step.value === 2) {
    if (!canLeaveGoals.value) return
    step.value = 3
    if (selectedTemplateKey.value) void loadPreview()
    return
  }
  if (step.value === 3) {
    step.value = 4
  }
}

function back(): void {
  error.value = null
  if (step.value === 2) step.value = 1
  else if (step.value === 3) step.value = 2
  else if (step.value === 4) step.value = 3
}

/**
 * 完成引导：先建计划，再写完成标记。
 *
 * 若计划已创建但标记写入失败，不重复建计划——保留 appliedResult，
 * 允许在完成页只重试写标记；下次启动也会因已有场景自动补齐。
 */
async function finish(profileEnabled: boolean): Promise<void> {
  if (!canFinish.value || !selectedTemplateKey.value) return
  applying.value = true
  error.value = null
  try {
    await invoke(
      () => SetSitProfileEnabled(profileEnabled, false),
      'SetSitProfileEnabled',
    )
    let applied = appliedResult.value
    if (!applied) {
      const result = await invoke(
        () =>
          ApplyScenarioTemplate(
            selectedTemplateKey.value,
            workStart.value,
            workEnd.value,
            selectedGoals.value,
            true,
          ),
        'ApplyScenarioTemplate',
      )
      if (!result) throw new Error('模板应用返回空结果')
      applied = result
      appliedResult.value = result
    }
    try {
      await invoke(() => CompleteOnboarding(), 'CompleteOnboarding')
      completionWarning.value = ''
    } catch (e) {
      completionWarning.value =
        '计划已创建并启用，但完成状态写入失败；可重试，或下次启动自动补齐。'
      error.value = e as AppError
    }
    logInfo('首次引导计划已创建', {
      scenario_id: applied.scenario?.id,
      reminders: applied.reminders?.length ?? 0,
      onboarding_saved: completionWarning.value === '',
    })
    step.value = 'done'
  } catch (e) {
    error.value = e as AppError
  } finally {
    applying.value = false
  }
}

async function retryComplete(): Promise<void> {
  try {
    await invoke(() => CompleteOnboarding(), 'CompleteOnboarding')
    completionWarning.value = ''
    error.value = null
  } catch (e) {
    error.value = e as AppError
  }
}

async function skip(): Promise<void> {
  skipping.value = true
  error.value = null
  try {
    await invoke(() => CompleteOnboarding(), 'CompleteOnboarding')
    logInfo('首次引导已跳过')
    await router.replace('/')
  } catch (e) {
    error.value = e as AppError
  } finally {
    skipping.value = false
  }
}

function openPlan(): void {
  const id = appliedResult.value?.scenario?.id
  if (!id) {
    void router.replace('/')
    return
  }
  void router.replace({ path: '/plan', query: { scenario: String(id) } })
}

function goHome(): void {
  void router.replace('/')
}

// 目标/工作时段在向导内变化时，只有停在模板步才重新预览；进入第 3 步时 next() 会主动拉一次。
watch([workStart, workEnd, selectedGoals], () => {
  if (step.value === 3 && selectedTemplateKey.value) void loadPreview()
})

onMounted(() => {
  void loadTemplates()
})
</script>

<template>
  <section class="wizard-card">
    <header class="wizard__head">
      <div>
        <p class="wizard__eyebrow">{{ progressText }}</p>
        <h1 class="wizard__title">{{ title }}</h1>
        <p class="wizard__subtitle">{{ subtitle }}</p>
      </div>
      <el-button v-if="step !== 'done'" text :loading="skipping" :disabled="applying" @click="skip()">
        跳过
      </el-button>
    </header>

    <p v-if="error" class="wizard__error">[{{ error.code }}] {{ error.message }}</p>

    <div v-if="step === 1" class="step">
      <div class="time-row">
        <label class="time-field">
          <span class="time-field__label">开始</span>
          <el-time-select
            v-model="workStart"
            start="06:00"
            end="23:00"
            step="00:30"
            format="HH:mm"
          />
        </label>
        <span class="time-sep">—</span>
        <label class="time-field">
          <span class="time-field__label">结束</span>
          <el-time-select
            v-model="workEnd"
            start="06:00"
            end="23:00"
            step="00:30"
            format="HH:mm"
          />
        </label>
      </div>
      <p v-if="!workWindowValid" class="field-error">结束时间需要晚于开始时间。</p>
      <p class="step__note">默认 09:00–18:00；只影响这一次生成的计划。</p>
      <div class="step__actions">
        <el-button type="primary" :disabled="!workWindowValid" @click="next()">下一步</el-button>
      </div>
    </div>

    <div v-else-if="step === 2" class="step">
      <p v-if="templatesLoading" class="step__note">正在加载目标…</p>
      <div v-else class="goal-grid">
        <button
          v-for="kind in goals"
          :key="kind"
          type="button"
          class="goal"
          :class="{
            'goal--selected': isGoalSelected(kind),
            'goal--disabled': goalsFull && !isGoalSelected(kind),
          }"
          :disabled="goalsFull && !isGoalSelected(kind)"
          @click="toggleGoal(kind)"
        >
          <span class="goal__icon">{{ goalIcon(kind) }}</span>
          <span class="goal__label">{{ goalLabel(kind) }}</span>
          <span class="goal__check">{{ isGoalSelected(kind) ? '✓' : '' }}</span>
        </button>
      </div>
      <p class="step__note">已选 {{ selectedGoals.length }} / {{ MAX_GOALS }}。再点一次可取消。</p>
      <div class="step__actions">
        <el-button @click="back()">上一步</el-button>
        <el-button type="primary" :disabled="!canLeaveGoals" @click="next()">下一步</el-button>
      </div>
    </div>

    <div v-else-if="step === 3" class="step">
      <div class="template-grid">
        <button
          v-for="tpl in templates"
          :key="tpl.key"
          type="button"
          class="template-choice"
          :class="{ 'template-choice--selected': selectedTemplateKey === tpl.key }"
          @click="selectTemplate(tpl)"
        >
          <span class="template-choice__name">{{ tpl.name }}</span>
          <span class="template-choice__summary">{{ tpl.summary }}</span>
          <span class="template-choice__meta">
            {{ templateKindLabels(tpl) }} · {{ tpl.reminders?.length ?? 0 }} 条
          </span>
        </button>
      </div>

      <p v-if="templatesLoading" class="step__note">正在加载模板…</p>
      <p v-else-if="!selectedTemplate" class="step__note">选一个模板，查看保留的提醒。</p>
      <div v-else class="preview">
        <p v-if="previewLoading" class="step__note">正在计算保留的提醒…</p>
        <template v-else-if="preview">
          <p class="preview__summary">
            将保留 <strong>{{ preview.reminders?.length ?? 0 }}</strong> 条提醒
            <span class="preview__time">{{ workStart }}–{{ workEnd }}</span>
          </p>
          <p v-if="preview.missing_goals?.length" class="preview__missing">
            这个时段里没有「{{ goalLabels(preview.missing_goals) }}」的提醒，请回到上一步调整目标或时段。
          </p>
          <p v-else-if="preview.next_fire_at" class="preview__next">
            下一次计划提醒：{{ formatNextFireAt(preview.next_fire_at) }}
          </p>
          <ul class="preview__list">
            <li
              v-for="(item, index) in preview.reminders ?? []"
              :key="index"
              class="preview__item"
            >
              <span class="preview__kind">{{ kindMeta(item.kind).label }}</span>
              <span class="preview__title">{{ item.title }}</span>
              <span class="preview__schedule">{{ reminderScheduleText(item) }}</span>
            </li>
          </ul>
        </template>
      </div>

      <div class="step__actions">
        <el-button @click="back()">上一步</el-button>
        <el-button type="primary" :disabled="!canFinish" @click="next()">下一步</el-button>
      </div>
    </div>

    <div v-else-if="step === 4" class="step">
      <div class="consent">
        <p class="consent__lead">
          开启后，小满会记录你这个时段在用的应用名、起止时间、使用时长与行为类别。
        </p>
        <ul class="consent__list">
          <li>只存在本机，不上传，不读窗口标题、屏幕内容或键鼠输入内容。</li>
          <li>可以在「设置 → 久坐感知」或「久坐」页查看、关闭、清空。</li>
          <li>小满默认说话比较直（毒舌），可随时在设置或气泡里切换。</li>
        </ul>
        <p class="step__note">只使用基础检测：仍会记录连续用机与休息，但不读取应用名称。</p>
      </div>
      <div class="step__actions">
        <el-button @click="back()">上一步</el-button>
        <el-button :disabled="!canFinish || applying" @click="finish(false)">
          只使用基础检测
        </el-button>
        <el-button type="primary" :loading="applying" :disabled="!canFinish" @click="finish(true)">
          开启应用画像并开始
        </el-button>
      </div>
    </div>

    <div v-else class="done">
      <div class="done__badge">已自动启用</div>
      <h2 class="done__name">{{ appliedResult?.scenario?.name ?? '你的计划' }}</h2>
      <p class="done__count">共 {{ appliedResult?.reminders?.length ?? 0 }} 条提醒</p>
      <p v-if="appliedResult?.next_fire_at" class="done__next">
        下一次计划提醒：{{ formatNextFireAt(appliedResult.next_fire_at) }}
      </p>
      <p v-if="completionWarning" class="done__warning">
        {{ completionWarning }}
        <el-button text size="small" @click="retryComplete()">重试保存</el-button>
      </p>
      <div class="done__actions">
        <el-button @click="openPlan()">查看计划</el-button>
        <el-button type="primary" @click="goHome()">开始使用</el-button>
      </div>
    </div>
  </section>
</template>

<style scoped>
.wizard-card {
  width: min(820px, 100%);
  padding: var(--space-6);
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-lg);
  background: var(--bg-raised);
}

.wizard__head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--space-4);
  margin-bottom: var(--space-5);
}

.wizard__eyebrow {
  margin: 0 0 var(--space-2);
  color: var(--color-primary);
  font-size: var(--font-size-xs);
  letter-spacing: 0.08em;
}

.wizard__title {
  margin: 0 0 var(--space-2);
  font-size: 26px;
  line-height: 1.3;
}

.wizard__subtitle {
  margin: 0;
  color: var(--text-2);
  font-size: var(--font-size-sm);
  line-height: 1.7;
}

.wizard__error {
  margin: 0 0 var(--space-4);
  padding: var(--space-2) var(--space-3);
  border: 1px solid var(--color-danger);
  border-radius: var(--radius-md);
  color: var(--color-danger);
  font-size: var(--font-size-sm);
}

.step__note {
  margin: var(--space-3) 0 0;
  color: var(--text-3);
  font-size: var(--font-size-xs);
}

.field-error {
  margin: var(--space-2) 0 0;
  color: var(--color-danger);
  font-size: var(--font-size-sm);
}

.step__actions {
  display: flex;
  justify-content: flex-end;
  gap: var(--space-3);
  margin-top: var(--space-5);
}

/* 第 1 步：工作时段 */
.time-row {
  display: flex;
  align-items: flex-end;
  gap: var(--space-3);
}

.time-field {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.time-field__label {
  color: var(--text-3);
  font-size: var(--font-size-xs);
}

.time-sep {
  padding-bottom: var(--space-2);
  color: var(--text-3);
}

/* 第 2 步：关注目标 */
.goal-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
  gap: var(--space-3);
}

.goal {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-3);
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-md);
  background: var(--bg-base);
  color: var(--text-1);
  font: inherit;
  text-align: left;
  cursor: pointer;
  transition: border-color var(--motion-fast) var(--ease-standard),
    background-color var(--motion-fast) var(--ease-standard);
}

.goal:hover {
  border-color: var(--color-primary);
}

.goal--selected {
  border-color: var(--color-primary);
  background: var(--color-primary-soft);
}

.goal--disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

.goal__icon {
  width: 24px;
  flex: none;
  text-align: center;
  color: var(--color-primary);
}

.goal__label {
  font-size: var(--font-size-sm);
}

.goal__check {
  margin-left: auto;
  color: var(--color-primary);
  font-size: var(--font-size-sm);
}

/* 第 3 步：模板与预览 */
.template-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: var(--space-3);
}

.template-choice {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  padding: var(--space-3);
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-md);
  background: var(--bg-base);
  color: var(--text-1);
  font: inherit;
  text-align: left;
  cursor: pointer;
  transition: border-color var(--motion-fast) var(--ease-standard),
    background-color var(--motion-fast) var(--ease-standard);
}

.template-choice:hover {
  border-color: var(--color-primary);
}

.template-choice--selected {
  border-color: var(--color-primary);
  background: var(--color-primary-soft);
}

.template-choice__name {
  font-size: var(--font-size-md);
  font-weight: 600;
}

.template-choice__summary {
  color: var(--text-2);
  font-size: var(--font-size-xs);
  line-height: 1.5;
}

.template-choice__meta {
  color: var(--text-3);
  font-size: var(--font-size-xs);
}

.preview {
  margin-top: var(--space-4);
  padding: var(--space-4);
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-md);
  background: var(--bg-base);
}

.preview__summary {
  margin: 0;
  color: var(--text-1);
  font-size: var(--font-size-sm);
}

.preview__summary strong {
  color: var(--color-primary);
}

.preview__time {
  margin-left: var(--space-2);
  color: var(--text-3);
  font-family: var(--font-mono);
  font-size: var(--font-size-xs);
}

.preview__missing {
  margin: var(--space-2) 0 0;
  color: var(--color-danger);
  font-size: var(--font-size-sm);
  line-height: 1.6;
}

.preview__next {
  margin: var(--space-2) 0 0;
  color: var(--text-2);
  font-size: var(--font-size-sm);
}

.preview__list {
  list-style: none;
  margin: var(--space-3) 0 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.preview__item {
  display: grid;
  grid-template-columns: auto 1fr;
  gap: 0 var(--space-2);
  font-size: var(--font-size-xs);
  color: var(--text-2);
}

.preview__kind {
  color: var(--color-primary);
}

.preview__title {
  font-weight: 500;
}

.preview__schedule {
  grid-column: 2;
  color: var(--text-3);
  font-family: var(--font-mono);
}

/* 第 4 步：应用画像同意 */
.consent {
  padding: var(--space-4);
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-lg);
  background: var(--bg-subtle);
}

.consent__lead {
  margin: 0 0 var(--space-2);
  color: var(--text-1);
  font-size: var(--font-size-sm);
  line-height: 1.7;
}

.consent__list {
  margin: 0;
  padding-left: 18px;
  color: var(--text-2);
  font-size: var(--font-size-xs);
  line-height: 1.9;
}

/* 完成页 */
.done {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: var(--space-2);
}

.done__badge {
  padding: 2px var(--space-2);
  border: 1px solid var(--color-success);
  border-radius: var(--radius-full);
  color: var(--color-success);
  font-size: var(--font-size-xs);
}

.done__name {
  margin: var(--space-1) 0 0;
  font-size: var(--font-size-xl);
}

.done__count,
.done__next {
  margin: 0;
  color: var(--text-2);
  font-size: var(--font-size-sm);
}

.done__warning {
  margin: var(--space-2) 0 0;
  color: var(--color-warning);
  font-size: var(--font-size-sm);
  line-height: 1.6;
}

.done__actions {
  display: flex;
  gap: var(--space-3);
  margin-top: var(--space-4);
}
</style>
