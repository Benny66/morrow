<script lang="ts" setup>
// 「久坐」页：把连续用机、休息与抵扣变成可核对的事实。
// 这一版只展示时间/休息/抵扣与站立走动目标；应用画像与按日应用明细在 006/007。
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import {
  Confirm,
  ClearSitProfile,
  GetSitDay,
  GetSitSettings,
  GetSitSnapshot,
  GetSitTrend,
  GetTodayStats,
  SetAppCategory,
} from '../../bindings/morrow/app.js'
import { invoke, type AppError } from '../lib/invoke'
import { useEvent } from '../composables/useEvent'
import { ReminderEvent } from '../lib/reminder'
import { SitCategory, sitCategoryLabel, sitProfileStatusLabel, sitStateLabel } from '../lib/sit'
import { logInfo, logWarn } from '../lib/log'

type SitSnapshot = Awaited<ReturnType<typeof GetSitSnapshot>>
type SitDay = Awaited<ReturnType<typeof GetSitDay>>
type SitTrend = Awaited<ReturnType<typeof GetSitTrend>>
type TodayStats = Awaited<ReturnType<typeof GetTodayStats>>
type SitSettings = Awaited<ReturnType<typeof GetSitSettings>>
type AppUsage = NonNullable<SitDay['apps']>[number]

const snapshot = ref<SitSnapshot | null>(null)
const day = ref<SitDay | null>(null)
const selectedDay = ref<SitDay | null>(null)
const selectedDate = ref(localDateKey())
const trend = ref<NonNullable<SitTrend>>([])
const stats = ref<TodayStats | null>(null)
const sitSettings = ref<SitSettings | null>(null)
const loading = ref(true)
const busy = ref(false)
const error = ref<AppError | null>(null)
const hint = ref('')
const categorySavingKey = ref('')

function dateKeyOf(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

function localDateKey(): string {
  return dateKeyOf(new Date())
}

/** 未来日期不可选；服务端也会返回空聚合，双保险。 */
function isFutureDate(d: Date): boolean {
  return dateKeyOf(d) > localDateKey()
}

function appKey(app: AppUsage): string {
  return `${app.platform}\u0000${app.app_id}`
}

function selectTrendDate(dateKey: string): void {
  selectedDate.value = dateKey
}

let selectedSeq = 0

async function loadSelectedDay(): Promise<void> {
  const seq = ++selectedSeq
  try {
    const selected = await invoke(() => GetSitDay(selectedDate.value), 'GetSitDay')
    if (seq === selectedSeq) selectedDay.value = selected
  } catch (e) {
    if (seq === selectedSeq) {
      error.value = e as AppError
      logWarn('读取按日应用明细失败', { err: String(e) })
    }
  }
}

/** 只保留站立/走动：久坐感知只和这两类目标发生关系。 */
const goalReminders = computed(() =>
  (stats.value?.reminders ?? []).filter((r) => r.kind === 'stand' || r.kind === 'move'),
)
const goalDone = computed(() => goalReminders.value.reduce((sum, r) => sum + r.done_count, 0))
const goalTarget = computed(() => goalReminders.value.reduce((sum, r) => sum + r.daily_target, 0))
const goalPercent = computed(() => {
  if (goalTarget.value <= 0) return 0
  return Math.min(100, Math.round((goalDone.value / goalTarget.value) * 100))
})
const breakTotal = computed(() => {
  const d = day.value
  if (!d) return 0
  return d.active_breaks + d.lock_breaks + d.idle_breaks
})
const platformSupported = computed(() => snapshot.value?.platform_supported ?? true)
const noStandMove = computed(() => !stats.value?.scenario || goalReminders.value.length === 0)
const maxTrendMinutes = computed(() => Math.max(1, ...trend.value.map((d) => d.sitting_minutes)))

const profileEnabled = computed(() => snapshot.value?.profile_enabled ?? false)
const profileStatus = computed(() => sitSettings.value?.profile_status ?? '')
const focus = computed(() => snapshot.value?.focus ?? null)
const appUsage = computed(() => selectedDay.value?.apps ?? [])
const categories = computed(() => selectedDay.value?.categories ?? [])
const maxCategoryMinutes = computed(() => Math.max(1, ...categories.value.map((c) => c.minutes)))
const isSelectedToday = computed(() => selectedDate.value === localDateKey())
const selectedBreakTotal = computed(() => {
  const d = selectedDay.value
  if (!d) return 0
  return d.active_breaks + d.lock_breaks + d.idle_breaks
})
const profileStatusText = computed(() => {
  if (!profileEnabled.value) return '未开启'
  if (!isSelectedToday.value) return '历史日期'
  return sitProfileStatusLabel(profileStatus.value || 'no_data')
})
const categoryOptions = Object.values(SitCategory).map((value) => ({
  value,
  label: sitCategoryLabel(value),
}))
const profileEmptyText = computed(() => {
  if (!profileEnabled.value) {
    return '应用画像未开启；基础模式不读取应用名称。开启后可以按日查看应用明细与类别。'
  }
  if (!isSelectedToday.value) {
    return '这一天没有应用使用记录。'
  }
  switch (profileStatus.value) {
    case 'read_error':
      return '应用画像已开启，但最近读取前台应用失败；请检查系统权限或稍后重试。'
    case 'unsupported':
      return '当前平台不支持应用画像。'
    case 'no_data':
    default:
      return '应用画像已开启，今天还没有应用使用记录。'
  }
})

function categoryPercent(minutes: number): string {
  if (minutes <= 0) return '2px'
  return `${Math.max(4, Math.round((minutes / maxCategoryMinutes.value) * 100))}%`
}

function trendLabel(dateKey: string): string {
  return dateKey.length >= 10 ? dateKey.slice(5) : dateKey
}

function barHeight(minutes: number): string {
  if (minutes <= 0) return '2px'
  return `${Math.max(4, Math.round((minutes / maxTrendMinutes.value) * 100))}%`
}

async function refresh(): Promise<void> {
  error.value = null
  try {
    const today = localDateKey()
    const [snap, sitDay, sitTrend, todayStats, settings] = await Promise.all([
      invoke(() => GetSitSnapshot(), 'GetSitSnapshot'),
      invoke(() => GetSitDay(today), 'GetSitDay'),
      invoke(() => GetSitTrend(7), 'GetSitTrend'),
      invoke(() => GetTodayStats(), 'GetTodayStats'),
      invoke(() => GetSitSettings(), 'GetSitSettings'),
    ])
    snapshot.value = snap
    day.value = sitDay
    trend.value = sitTrend ?? []
    stats.value = todayStats
    sitSettings.value = settings
    if (!selectedDate.value || selectedDate.value > today) {
      selectedDate.value = today
    }
    if (selectedDate.value === today) {
      selectedDay.value = sitDay
    } else {
      await loadSelectedDay()
    }
  } catch (e) {
    error.value = e as AppError
    logWarn('读取久坐页失败', { err: String(e) })
  } finally {
    loading.value = false
  }
}

async function onCategoryChange(app: AppUsage, category: string): Promise<void> {
  if (categorySavingKey.value) return
  categorySavingKey.value = appKey(app)
  error.value = null
  hint.value = ''
  try {
    await invoke(
      () => SetAppCategory(app.platform, app.app_id, category),
      'SetAppCategory',
    )
    hint.value = '已应用，历史视图同步'
    logInfo('应用类别已纠正')
    await refresh()
  } catch (e) {
    error.value = e as AppError
  } finally {
    categorySavingKey.value = ''
  }
}

function onCategorySelect(app: AppUsage, value: unknown): void {
  void onCategoryChange(app, String(value))
}

watch(selectedDate, (value) => {
  const today = localDateKey()
  if (!value || value > today) {
    selectedDate.value = today
    return
  }
  void loadSelectedDay()
})

async function clearProfileData(): Promise<void> {
  if (busy.value) return
  busy.value = true
  error.value = null
  hint.value = ''
  try {
    const ok = await invoke(
      () =>
        Confirm(
          '清空应用与久坐数据',
          '会删除应用会话、类别纠正、久坐会话、休息与宠物发言；已计入目标的打卡记录与连续天数保留。确定吗？',
        ),
      'Confirm',
    )
    if (!ok) return
    await invoke(() => ClearSitProfile(), 'ClearSitProfile')
    selectedDate.value = localDateKey()
    hint.value = '已清空应用与久坐数据；画像开启时会继续记录新数据，打卡记录保留'
    logInfo('应用与久坐数据已清空')
    await refresh()
  } catch (e) {
    error.value = e as AppError
  } finally {
    busy.value = false
  }
}

useEvent(ReminderEvent.Changed, () => {
  void refresh()
})

let refreshTimer: number | undefined

onMounted(async () => {
  await refresh()
  refreshTimer = window.setInterval(() => {
    void refresh()
  }, 30000)
})

onUnmounted(() => {
  if (refreshTimer !== undefined) {
    window.clearInterval(refreshTimer)
    refreshTimer = undefined
  }
})
</script>

<template>
  <section class="page">
    <h1 class="page__title">久坐</h1>
    <p class="sit-lede">
      基于键鼠活跃度估算的「连续用机」时间，不代表真实坐姿；没有摄像头，也不会读取屏幕内容。
    </p>

    <p v-if="error" class="sit-error">[{{ error.code }}] {{ error.message }}</p>

    <div v-if="loading" class="sit-muted">正在读取…</div>

    <div v-else-if="!platformSupported" class="sit-state">
      <h2>当前平台不支持久坐感知</h2>
      <p>正式提醒仍然可用；久坐软提醒与数据统计在当前平台不启动。</p>
    </div>

    <div v-else-if="noStandMove" class="sit-state">
      <h2>还没有站立/走动计划</h2>
      <p>
        久坐感知只在当前场景包含站立或走动提醒时启用。先去
        <router-link to="/plan">计划页</router-link>
        添加一条，或到
        <router-link to="/scenarios">场景页</router-link>
        选一个带站立的模板。
      </p>
    </div>

    <template v-else>
      <div class="sit-metrics">
        <div class="sit-metric">
          <span class="sit-metric__label">当前连续用机</span>
          <strong class="sit-metric__value">{{ snapshot?.continuous_minutes ?? 0 }}<small>分钟</small></strong>
          <span class="sit-metric__hint">{{ sitStateLabel(snapshot?.state ?? 'idle') }}</span>
        </div>
        <div class="sit-metric">
          <span class="sit-metric__label">当前专注</span>
          <strong class="sit-metric__value">
            {{ focus ? sitCategoryLabel(focus.category) : '—' }}
            <small v-if="focus">{{ focus.minutes }} 分钟</small>
          </strong>
          <span class="sit-metric__hint">
            {{ profileEnabled ? '同一应用连续使用满 10 分钟' : '应用画像开启后可用' }}
          </span>
        </div>
        <div class="sit-metric">
          <span class="sit-metric__label">今日久坐总时长</span>
          <strong class="sit-metric__value">{{ snapshot?.today_sitting_minutes ?? 0 }}<small>分钟</small></strong>
          <span class="sit-metric__hint">已收尾会话 + 当前会话</span>
        </div>
        <div class="sit-metric">
          <span class="sit-metric__label">最长连续久坐</span>
          <strong class="sit-metric__value">{{ snapshot?.longest_session_minutes ?? 0 }}<small>分钟</small></strong>
          <span class="sit-metric__hint">今日单段最长</span>
        </div>
        <div class="sit-metric">
          <span class="sit-metric__label">真实休息</span>
          <strong class="sit-metric__value">{{ breakTotal }}<small>次</small></strong>
          <span class="sit-metric__hint">
            主动 {{ day?.active_breaks ?? 0 }} · 锁屏 {{ day?.lock_breaks ?? 0 }} · 空闲
            {{ day?.idle_breaks ?? 0 }}
          </span>
        </div>
        <div class="sit-metric">
          <span class="sit-metric__label">已抵扣站立/走动</span>
          <strong class="sit-metric__value">{{ day?.credited_count ?? 0 }}<small>次</small></strong>
          <span class="sit-metric__hint">主动休息或锁屏满足条件时抵扣</span>
        </div>
        <div class="sit-metric">
          <span class="sit-metric__label">今日站立/走动目标</span>
          <strong class="sit-metric__value">{{ goalDone }}<small>/ {{ goalTarget }}</small></strong>
          <span class="sit-metric__hint">复用提醒计划的每日目标</span>
        </div>
      </div>

      <div class="sit-card">
        <div class="sit-card__head">
          <h2 class="sit-card__title">今日目标进度</h2>
          <span class="sit-muted">{{ goalPercent }}%</span>
        </div>
        <div class="sit-progress" role="progressbar" :aria-valuenow="goalPercent" aria-valuemin="0" aria-valuemax="100">
          <div class="sit-progress__bar" :style="{ width: `${goalPercent}%` }" />
        </div>
        <div v-if="goalReminders.length" class="sit-goals">
          <div v-for="item in goalReminders" :key="item.reminder_id" class="sit-goal">
            <span>{{ item.title }}</span>
            <span class="sit-muted">{{ item.done_count }}/{{ item.daily_target }}</span>
          </div>
        </div>
      </div>

      <div class="sit-card">
        <div class="sit-card__head">
          <h2 class="sit-card__title">最近 7 天</h2>
          <span class="sit-muted">点柱子查看那天的应用明细</span>
        </div>
        <div class="sit-bars">
          <button
            v-for="item in trend"
            :key="item.date_key"
            type="button"
            class="sit-bars__item"
            :class="{ 'is-selected': item.date_key === selectedDate }"
            :aria-label="`查看 ${item.date_key} 的久坐与应用明细`"
            @click="selectTrendDate(item.date_key)"
          >
            <span class="sit-bars__value">{{ item.sitting_minutes }}</span>
            <span class="sit-bars__track">
              <span class="sit-bars__bar" :style="{ height: barHeight(item.sitting_minutes) }" />
            </span>
            <span class="sit-bars__date">{{ trendLabel(item.date_key) }}</span>
            <span class="sit-bars__break">起 {{ item.active_breaks }} · 休 {{ item.lock_breaks + item.idle_breaks }}</span>
          </button>
        </div>
      </div>

      <div class="sit-card">
        <div class="sit-card__head">
          <h2 class="sit-card__title">应用使用</h2>
          <div class="sit-day-picker">
            <el-date-picker
              v-model="selectedDate"
              type="date"
              value-format="YYYY-MM-DD"
              format="YYYY-MM-DD"
              size="small"
              :clearable="false"
              :disabled-date="isFutureDate"
              aria-label="选择要查看的日期"
            />
            <el-tag size="small" type="info">{{ profileStatusText }}</el-tag>
          </div>
        </div>
        <div class="sit-day-stats">
          <span>久坐 {{ selectedDay?.sitting_minutes ?? 0 }} 分钟</span>
          <span>最长 {{ selectedDay?.longest_session_minutes ?? 0 }} 分钟</span>
          <span>真实休息 {{ selectedBreakTotal }} 次</span>
          <span class="sit-muted">
            主动 {{ selectedDay?.active_breaks ?? 0 }} · 锁屏 {{ selectedDay?.lock_breaks ?? 0 }} · 空闲
            {{ selectedDay?.idle_breaks ?? 0 }}
          </span>
          <span>已抵扣 {{ selectedDay?.credited_count ?? 0 }} 次</span>
        </div>

        <div v-if="appUsage.length" class="sit-apps">
          <div v-for="app in appUsage" :key="appKey(app)" class="sit-app">
            <div class="sit-app__main">
              <span class="sit-app__name" :title="app.display_name">{{ app.display_name }}</span>
              <el-select
                :model-value="app.category"
                class="sit-app__select"
                size="small"
                :aria-label="`调整 ${app.display_name} 的行为类别`"
                :disabled="categorySavingKey === appKey(app)"
                @change="onCategorySelect(app, $event)"
              >
                <el-option
                  v-for="option in categoryOptions"
                  :key="option.value"
                  :label="option.label"
                  :value="option.value"
                />
              </el-select>
            </div>
            <div class="sit-app__meta">{{ app.minutes }} 分钟 · {{ app.sessions }} 次</div>
          </div>
        </div>
        <p v-else class="sit-muted">{{ profileEmptyText }}</p>
        <p v-if="!profileEnabled && appUsage.length" class="sit-muted">
          应用画像已关闭，以下是保留的历史记录。
        </p>
        <router-link v-if="!profileEnabled" to="/settings" class="sit-link">
          去设置开启应用画像
        </router-link>

        <div v-if="categories.length" class="sit-cats">
          <div v-for="cat in categories" :key="cat.category" class="sit-cat">
            <span class="sit-cat__label">{{ sitCategoryLabel(cat.category) }}</span>
            <div class="sit-cat__track">
              <div class="sit-cat__bar" :style="{ width: categoryPercent(cat.minutes) }" />
            </div>
            <span class="sit-cat__value">{{ cat.minutes }} 分钟</span>
          </div>
        </div>
      </div>

      <div class="sit-card sit-card--plain">
        <div class="sit-card__head">
          <h2 class="sit-card__title">数据与模式</h2>
          <el-tag size="small" type="info">
            {{ profileEnabled ? '应用画像' : '基础模式' }}
          </el-tag>
        </div>
        <p class="sit-muted">
          <template v-if="profileEnabled">
            应用画像记录应用名、前台时长与行为类别；不读窗口标题、屏幕内容或键鼠输入内容。数据只存本机，不上传。
            类别纠正也只存本机，会同步到历史视图。
          </template>
          <template v-else>
            基础模式只记录连续用机与休息，不读取应用名称。数据只存本机，不上传；清空应用与久坐数据不会删除已计入目标的打卡记录。
          </template>
        </p>
        <div class="sit-actions">
          <button class="sit-btn" :disabled="busy" @click="clearProfileData">
            {{ busy ? '处理中…' : '清空应用与久坐数据…' }}
          </button>
          <span v-if="hint" class="sit-muted">{{ hint }}</span>
        </div>
      </div>
    </template>
  </section>
</template>

<style scoped>
.sit-lede {
  max-width: 720px;
  margin: -8px 0 20px;
  color: var(--text-2);
  font-size: var(--font-size-sm);
  line-height: 1.6;
}

.sit-muted {
  color: var(--text-2);
  font-size: var(--font-size-xs);
}

.sit-error {
  color: var(--color-danger);
  font-size: var(--font-size-sm);
}

.sit-state {
  max-width: 620px;
  padding: 24px;
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-lg);
  background: var(--bg-raised);
}

.sit-state h2 {
  margin: 0 0 8px;
  font-size: var(--font-size-md);
}

.sit-state p {
  margin: 0;
  color: var(--text-2);
  font-size: var(--font-size-sm);
  line-height: 1.7;
}

.sit-state a {
  color: var(--color-primary);
}

.sit-metrics {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
  gap: 12px;
  margin-bottom: 20px;
}

.sit-metric {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 14px 16px;
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-lg);
  background: var(--bg-raised);
}

.sit-metric__label {
  color: var(--text-2);
  font-size: var(--font-size-xs);
}

.sit-metric__value {
  color: var(--text-1);
  font-size: 26px;
  font-weight: 650;
  font-variant-numeric: tabular-nums;
  line-height: 1.1;
}

.sit-metric__value small {
  margin-left: 4px;
  color: var(--text-2);
  font-size: var(--font-size-xs);
  font-weight: 400;
}

.sit-metric__hint {
  color: var(--text-2);
  font-size: 11px;
  line-height: 1.4;
}

.sit-card {
  margin-bottom: 16px;
  padding: 18px;
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-lg);
  background: var(--bg-raised);
}

.sit-card--plain {
  background: var(--bg-subtle);
}

.sit-card__head {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 12px;
}

.sit-card__title {
  margin: 0;
  font-size: var(--font-size-md);
}

.sit-progress {
  height: 8px;
  overflow: hidden;
  border-radius: 999px;
  background: var(--bg-base);
}

.sit-progress__bar {
  height: 100%;
  border-radius: inherit;
  background: var(--color-primary);
  transition: width 300ms var(--ease-in-out);
}

.sit-goals {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 20px;
  margin-top: 12px;
}

.sit-goal {
  display: flex;
  gap: 8px;
  font-size: var(--font-size-xs);
}

.sit-bars {
  display: flex;
  align-items: flex-end;
  gap: 10px;
  min-height: 150px;
  padding-top: 8px;
}

.sit-bars__item {
  display: flex;
  flex: 1;
  flex-direction: column;
  align-items: center;
  gap: 4px;
  min-width: 0;
  padding: 0;
  border: 0;
  background: none;
  color: inherit;
  font: inherit;
  cursor: pointer;
  border-radius: var(--radius-sm);
}

.sit-bars__item:focus-visible {
  outline: 2px solid var(--color-primary);
  outline-offset: 4px;
}

.sit-bars__item.is-selected .sit-bars__track {
  outline: 2px solid var(--color-primary);
  outline-offset: 2px;
}

.sit-bars__item.is-selected .sit-bars__value,
.sit-bars__item.is-selected .sit-bars__date {
  color: var(--color-primary);
  font-weight: 600;
}

.sit-bars__value {
  color: var(--text-2);
  font-size: 11px;
  font-variant-numeric: tabular-nums;
}

.sit-bars__track {
  display: flex;
  align-items: flex-end;
  width: 100%;
  height: 100px;
  border-radius: var(--radius-sm) var(--radius-sm) 0 0;
  background: var(--bg-base);
}

.sit-bars__bar {
  width: 100%;
  min-height: 2px;
  border-radius: var(--radius-sm) var(--radius-sm) 0 0;
  background: var(--color-primary);
}

.sit-bars__date {
  color: var(--text-2);
  font-size: 11px;
}

.sit-bars__break {
  color: var(--text-3);
  font-size: 10px;
}

.sit-apps {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.sit-app {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 8px 10px;
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-sm);
  background: var(--bg-base);
}

.sit-app__main {
  display: flex;
  flex: 1;
  align-items: center;
  gap: 10px;
  min-width: 0;
}

.sit-app__name {
  display: block;
  overflow: hidden;
  min-width: 0;
  color: var(--text-1);
  font-size: var(--font-size-sm);
  text-overflow: ellipsis;
  white-space: nowrap;
}

.sit-app__select {
  flex: none;
  width: 108px;
}

.sit-app__meta {
  flex: none;
  color: var(--text-2);
  font-size: var(--font-size-xs);
  font-variant-numeric: tabular-nums;
}

.sit-day-picker {
  display: flex;
  flex: none;
  flex-wrap: wrap;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
}

.sit-day-picker :deep(.el-date-editor.el-input) {
  width: 132px;
}

.sit-day-stats {
  display: flex;
  flex-wrap: wrap;
  gap: 6px 14px;
  margin: -4px 0 14px;
  color: var(--text-2);
  font-size: var(--font-size-xs);
  font-variant-numeric: tabular-nums;
}

.sit-link {
  display: inline-block;
  margin-top: 8px;
  color: var(--color-primary);
  font-size: var(--font-size-xs);
}

.sit-cats {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-top: 16px;
  padding-top: 12px;
  border-top: 1px solid var(--border-subtle);
}

.sit-cat {
  display: grid;
  grid-template-columns: 64px 1fr auto;
  align-items: center;
  gap: 10px;
  font-size: var(--font-size-xs);
}

.sit-cat__label {
  color: var(--text-2);
}

.sit-cat__track {
  height: 6px;
  overflow: hidden;
  border-radius: 999px;
  background: var(--bg-base);
}

.sit-cat__bar {
  height: 100%;
  border-radius: inherit;
  background: var(--color-primary);
}

.sit-cat__value {
  color: var(--text-2);
  font-variant-numeric: tabular-nums;
}

.sit-actions {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-top: 14px;
}

.sit-btn {
  padding: 6px 12px;
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-sm);
  background: var(--bg-raised);
  color: var(--text-1);
  font-size: var(--font-size-xs);
  cursor: pointer;
}

.sit-btn:hover:not(:disabled) {
  border-color: var(--color-primary);
  color: var(--color-primary);
}

.sit-btn:disabled {
  opacity: 0.5;
  cursor: default;
}
</style>
