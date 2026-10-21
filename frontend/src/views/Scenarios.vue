<script lang="ts" setup>
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import Skeleton from '../components/Skeleton.vue'
import EmptyState from '../components/EmptyState.vue'
import {
  ApplyScenarioTemplate,
  Confirm,
  CreateScenario,
  DeleteScenario,
  GenerateScenarioPlan,
  ListScenarioTemplates,
  ListScenarios,
  SetScenarioEnabled,
} from '../../bindings/morrow/app.js'
import { invoke, type AppError } from '../lib/invoke'
import { ReminderEvent, kindMeta } from '../lib/reminder'
import { reminderScheduleText, templateKindLabels } from '../lib/template'
import { useEvent } from '../composables/useEvent'
import { logInfo } from '../lib/log'

// 场景页：从内置模板一键起步，或用一段自然语言描述处境 → 让 AI 一次性产出整套提醒。
// AI 仍是运行期唯一的生成路径；模板是确定性内容，不调用 AI。
type ScenarioRow = NonNullable<Awaited<ReturnType<typeof ListScenarios>>>[number]
type TemplateCatalog = Awaited<ReturnType<typeof ListScenarioTemplates>>
type TemplateRow = NonNullable<TemplateCatalog['templates']>[number]

const route = useRoute()
const scenarios = ref<ScenarioRow[]>([])
const loading = ref(false)
const error = ref<AppError | null>(null)
const hint = ref('')
// 正在生成计划的场景 ID（0 表示空闲）：同一条卡片上的按钮据此显示 loading。
const generatingID = ref(0)
const saving = ref(false)
// 模板库状态：展开的模板 key、正在应用的模板 key、加载中。
const templates = ref<TemplateRow[]>([])
const templatesLoading = ref(false)
const expandedTemplate = ref('')
const applyingTemplateKey = ref('')

const form = ref({ name: '', description: '' })

const hasScenarios = computed(() => scenarios.value.length > 0)
const canSubmit = computed(
  () => form.value.name.trim().length > 0 && form.value.description.trim().length > 0 && !saving.value,
)

/** 加载场景列表。 */
async function load(): Promise<void> {
  loading.value = true
  try {
    scenarios.value = (await invoke(() => ListScenarios(), 'ListScenarios')) ?? []
  } catch (e) {
    error.value = e as AppError
  } finally {
    loading.value = false
  }
}

/** 显式刷新场景与模板：清掉旧错误，让失败后能原样重试。 */
async function refresh(): Promise<void> {
  error.value = null
  hint.value = ''
  await Promise.all([load(), loadTemplates()])
}

/** 加载内置模板目录。模板来自 Go 侧唯一真相，前端不写死内容。 */
async function loadTemplates(): Promise<void> {
  templatesLoading.value = true
  try {
    const catalog = await invoke(() => ListScenarioTemplates(), 'ListScenarioTemplates')
    templates.value = catalog?.templates ?? []
  } catch (e) {
    error.value = e as AppError
  } finally {
    templatesLoading.value = false
  }
}

/** 展开/收起某个模板的完整提醒清单（一次只展开一个）。 */
function toggleTemplatePreview(key: string): void {
  expandedTemplate.value = expandedTemplate.value === key ? '' : key
}

/**
 * 用模板原样创建一套副本。
 *
 * 这里固定 enable=false：场景页可能已有启用中的计划，自动启用会静默替换它；
 * 只有首次引导（此刻必然没有场景）才自动启用。
 */
async function applyTemplate(tpl: TemplateRow): Promise<void> {
  applyingTemplateKey.value = tpl.key
  error.value = null
  hint.value = ''
  try {
    const result = await invoke(
      () => ApplyScenarioTemplate(tpl.key, '', '', [], false),
      'ApplyScenarioTemplate',
    )
    const name = result?.scenario?.name ?? tpl.name
    const count = result?.reminders?.length ?? 0
    hint.value = `已创建「${name}」（${count} 条提醒，默认停用）。点下方场景卡片的「启用」开始提醒。`
    logInfo('模板副本已创建', { key: tpl.key, scenario_id: result?.scenario?.id, count })
    await load()
  } catch (e) {
    error.value = e as AppError
  } finally {
    applyingTemplateKey.value = ''
  }
}

/**
 * 创建场景。
 *
 * withAI=true 时紧接着跑一次生成：用户在这个页面上的意图几乎总是「描述完就生成」，
 * 把两件事拆成两步操作只会多一次点击。
 */
async function createScenario(withAI: boolean): Promise<void> {
  if (!canSubmit.value) return
  saving.value = true
  error.value = null
  hint.value = ''
  try {
    const created = await invoke(
      () => CreateScenario(form.value.name.trim(), form.value.description.trim()),
      'CreateScenario',
    )
    if (!created) return
    logInfo('场景已创建', { id: created.id, name: created.name })
    form.value.name = ''
    form.value.description = ''
    await load()
    if (withAI) {
      await runGenerate(created.id)
    } else {
      hint.value = '已保存。点「AI 生成计划」让模型把这套描述变成提醒。'
    }
  } catch (e) {
    error.value = e as AppError
  } finally {
    saving.value = false
  }
}

/** 生成/重新生成计划。失败时把错误交给页面展示，用户可以原样重试。 */
async function runGenerate(scenarioID: number): Promise<void> {
  generatingID.value = scenarioID
  error.value = null
  hint.value = ''
  try {
    const result = await invoke(
      () => GenerateScenarioPlan(scenarioID),
      'GenerateScenarioPlan',
    )
    const count = result?.reminders?.length ?? 0
    hint.value = `已生成 ${count} 条提醒`
    logInfo('AI 计划已生成', { scenario_id: scenarioID, count })
    await load()
  } catch (e) {
    error.value = e as AppError
  } finally {
    generatingID.value = 0
  }
}

/** 启用/停用场景（启用会自动停用其他场景，这是后端的单选语义）。 */
async function toggleEnabled(row: ScenarioRow): Promise<void> {
  error.value = null
  try {
    await invoke(() => SetScenarioEnabled(row.id, !row.enabled), 'SetScenarioEnabled')
    await load()
  } catch (e) {
    error.value = e as AppError
  }
}

/** 删除场景（级联清理提醒、触发与打卡记录，故必须二次确认）。 */
async function removeScenario(row: ScenarioRow): Promise<void> {
  error.value = null
  try {
    const confirmed = await invoke(
      () => Confirm('删除场景', `将删除「${row.name}」及其全部提醒与打卡记录，确定吗？`),
      'Confirm',
    )
    if (!confirmed) return
    await invoke(() => DeleteScenario(row.id), 'DeleteScenario')
    hint.value = '场景已删除'
    await load()
  } catch (e) {
    error.value = e as AppError
  }
}

/** 把 Unix 秒格式化成「YYYY-MM-DD HH:MM」，0 表示从未生成。 */
function formatTime(unixSeconds: number): string {
  if (!unixSeconds) return '从未生成'
  return new Date(unixSeconds * 1000).toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

// 别处（计划页/首页打卡）改了数据时同步刷新，避免两个页面的场景状态不一致。
useEvent(ReminderEvent.Changed, () => {
  void load()
})

onMounted(() => {
  // 从首页快速添加跳过来时，把当时的描述预填进场景表单，省一次复制。
  const description = route.query.description
  if (typeof description === 'string' && description.trim() !== '') {
    form.value.description = description
  }
  void refresh()
})
</script>

<template>
  <section class="page">
    <header class="page__head">
      <div>
        <h1 class="page__title">场景</h1>
        <p class="page__desc">
          先选一个模板，或用一句话描述你的处境（例如「我在备孕，不能久坐，每小时喝一杯水」），AI 会生成整套提醒。
        </p>
      </div>
      <el-button size="small" :loading="loading || templatesLoading" @click="refresh()">刷新</el-button>
    </header>

    <p v-if="error" class="page__error">[{{ error.code }}] {{ error.message }}</p>
    <p v-if="hint" class="page__hint">{{ hint }}</p>

    <section class="templates">
      <div class="templates__head">
        <div>
          <h2 class="templates__title">从模板开始</h2>
          <p class="templates__desc">
            不需要 AI Key，选一个最接近你一天的模板；生成后可在计划页逐条修改。
          </p>
        </div>
        <span v-if="templatesLoading" class="templates__loading">加载模板…</span>
      </div>

      <div v-if="templates.length" class="template-grid">
        <article v-for="tpl in templates" :key="tpl.key" class="template-card">
          <div class="template-card__head">
            <h3 class="template-card__name">{{ tpl.name }}</h3>
            <span class="template-card__count">{{ tpl.reminders?.length ?? 0 }} 条提醒</span>
          </div>
          <p class="template-card__summary">{{ tpl.summary }}</p>
          <p class="template-card__kinds">{{ templateKindLabels(tpl) }}</p>
          <div class="template-card__actions">
            <el-button size="small" text @click="toggleTemplatePreview(tpl.key)">
              {{ expandedTemplate === tpl.key ? '收起提醒' : '查看提醒' }}
            </el-button>
            <el-button
              size="small"
              type="primary"
              :loading="applyingTemplateKey === tpl.key"
              :disabled="applyingTemplateKey !== '' && applyingTemplateKey !== tpl.key"
              @click="applyTemplate(tpl)"
            >
              使用模板
            </el-button>
          </div>
          <ul v-if="expandedTemplate === tpl.key" class="template-card__preview">
            <li
              v-for="(item, index) in tpl.reminders ?? []"
              :key="index"
              class="template-card__reminder"
            >
              <span class="template-card__kind">{{ kindMeta(item.kind).label }}</span>
              <span class="template-card__title">{{ item.title }}</span>
              <span class="template-card__schedule">{{ reminderScheduleText(item) }}</span>
            </li>
          </ul>
        </article>
      </div>
    </section>

    <el-card class="create" shadow="never">
      <h2 class="create__title">新建场景</h2>
      <el-input
        v-model="form.name"
        class="create__input"
        placeholder="场景名，如「备孕办公日」"
        maxlength="50"
        show-word-limit
      />
      <el-input
        v-model="form.description"
        class="create__input"
        type="textarea"
        :rows="4"
        maxlength="2000"
        show-word-limit
        placeholder="描述你的一天：什么时候不能久坐、多久喝一次水、什么时候想走动或上厕所……"
      />
      <div class="create__actions">
        <el-button type="primary" :loading="saving" :disabled="!canSubmit" @click="createScenario(true)">
          AI 生成计划
        </el-button>
        <el-button :loading="saving" :disabled="!canSubmit" @click="createScenario(false)">
          仅保存场景
        </el-button>
        <span class="create__note">生成计划需要先在「设置」页填好 AI 接口与 API Key。</span>
      </div>
    </el-card>

    <Skeleton v-if="loading && !hasScenarios" :rows="3" />

    <EmptyState v-else-if="!hasScenarios" description="还没有场景。可以从上方模板开始，或填下面的表单描述一次你的一天。" />

    <ul v-else class="list">
      <li v-for="row in scenarios" :key="row.id" class="scenario">
        <div class="scenario__head">
          <div class="scenario__intro">
            <h3 class="scenario__name">
              {{ row.name }}
              <el-tag v-if="row.enabled" size="small" type="success">已启用</el-tag>
            </h3>
            <p class="scenario__summary">{{ row.summary || '尚未生成计划' }}</p>
          </div>
          <span class="scenario__time">{{ formatTime(row.generated_at) }}</span>
        </div>

        <p class="scenario__desc">{{ row.description }}</p>

        <div class="scenario__actions">
          <el-button
            size="small"
            type="primary"
            :loading="generatingID === row.id"
            @click="runGenerate(row.id)"
          >
            {{ row.generated_at ? '重新生成' : 'AI 生成计划' }}
          </el-button>
          <el-button size="small" @click="toggleEnabled(row)">
            {{ row.enabled ? '停用' : '启用' }}
          </el-button>
          <router-link class="scenario__link" :to="{ path: '/plan', query: { scenario: String(row.id) } }">
            查看计划
          </router-link>
          <el-button size="small" text type="danger" @click="removeScenario(row)">删除</el-button>
        </div>
      </li>
    </ul>
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
  max-width: 620px;
  color: var(--text-2);
  font-size: var(--font-size-sm);
  line-height: 1.6;
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

.templates {
  margin-bottom: var(--space-5);
}

.templates__head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--space-4);
  margin-bottom: var(--space-3);
}

.templates__title {
  font-size: var(--font-size-md);
  margin: 0 0 var(--space-1);
  color: var(--text-1);
}

.templates__desc {
  margin: 0;
  color: var(--text-2);
  font-size: var(--font-size-sm);
  line-height: 1.6;
}

.templates__loading {
  flex: none;
  color: var(--text-3);
  font-size: var(--font-size-xs);
}

.template-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
  gap: var(--space-3);
}

.template-card {
  padding: var(--space-4);
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-lg);
  background: var(--bg-raised);
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.template-card__head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: var(--space-2);
}

.template-card__name {
  font-size: var(--font-size-lg);
  margin: 0;
}

.template-card__count {
  flex: none;
  color: var(--text-3);
  font-size: var(--font-size-xs);
  font-family: var(--font-mono);
}

.template-card__summary {
  margin: 0;
  color: var(--text-2);
  font-size: var(--font-size-sm);
  line-height: 1.6;
}

.template-card__kinds {
  margin: 0;
  color: var(--text-3);
  font-size: var(--font-size-xs);
}

.template-card__actions {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-2);
  margin-top: var(--space-1);
}

.template-card__preview {
  list-style: none;
  margin: var(--space-1) 0 0;
  padding: var(--space-2) 0 0;
  border-top: 1px solid var(--border-subtle);
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
}

.template-card__reminder {
  display: grid;
  grid-template-columns: auto 1fr;
  gap: 0 var(--space-2);
  font-size: var(--font-size-xs);
  color: var(--text-2);
}

.template-card__kind {
  color: var(--color-primary);
}

.template-card__title {
  font-weight: 500;
}

.template-card__schedule {
  grid-column: 2;
  color: var(--text-3);
  font-family: var(--font-mono);
}

.create {
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-lg);
  background: var(--bg-raised);
  margin-bottom: var(--space-5);
}

.create__title {
  font-size: var(--font-size-md);
  color: var(--text-2);
  margin: 0 0 var(--space-3);
}

.create__input {
  margin-bottom: var(--space-3);
}

.create__actions {
  display: flex;
  align-items: center;
  gap: var(--space-3);
}

.create__note {
  color: var(--text-3);
  font-size: var(--font-size-xs);
}

.list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}

.scenario {
  padding: var(--space-4);
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-lg);
  background: var(--bg-raised);
}

.scenario__head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--space-4);
}

.scenario__intro {
  min-width: 0;
}

.scenario__name {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--font-size-lg);
  margin: 0 0 var(--space-1);
}

.scenario__summary {
  margin: 0;
  color: var(--text-2);
  font-size: var(--font-size-sm);
}

.scenario__time {
  flex: none;
  color: var(--text-3);
  font-size: var(--font-size-xs);
  font-family: var(--font-mono);
}

.scenario__desc {
  margin: var(--space-3) 0;
  color: var(--text-3);
  font-size: var(--font-size-sm);
  line-height: 1.6;
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.scenario__actions {
  display: flex;
  align-items: center;
  gap: var(--space-3);
}

.scenario__link {
  color: var(--color-primary);
  font-size: var(--font-size-sm);
}
</style>
