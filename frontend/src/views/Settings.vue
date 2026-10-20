<script lang="ts" setup>
import { onMounted, ref } from 'vue'
import { storeToRefs } from 'pinia'
import { useAppStore } from '../stores/app'
// v3 的绑定路径见 stores/app.ts 的说明。
import {
  Confirm,
  ExportDatabase,
  GetAISettings,
  GetAppInfo,
  GetAutostartEnabled,
  GetClipboardText,
  GetDataDir,
  GetMCPToken,
  GetPetSettings,
  GetReminderSettings,
  GetToggleWindowShortcut,
  ImportDatabase,
  ImportPetPack,
  ListPetPacks,
  OpenDataDir,
  RequestNotificationPermission,
  RotateMCPToken,
  SelectFile,
  SelectSaveFile,
  SendNotification,
  SetAISettings,
  SetAutostart,
  SetClipboardText,
  SetPetSettings,
  SetReminderSettings,
  SetToggleWindowShortcut,
  TestAIConnection,
} from '../../bindings/morrow/app.js'
import { invoke, type AppError } from '../lib/invoke'
import { logError, logInfo } from '../lib/log'

// 设置页：主题三档 + 数据目录/备份恢复 + 通知/自启/快捷键/MCP token。
// 演示三件事：响应式状态落盘、原生目录能力、绑定方法的组合调用（选路径 → 导出）。
const store = useAppStore()
// 用 storeToRefs 建立真响应式，避免 ref(store.theme) 的断链快照。
const { theme, themeMode } = storeToRefs(store)

function onThemeChange(mode: '' | 'dark' | 'light') {
  store.setTheme(mode)
}

// 应用信息：版本/平台/路径。类型直接取自绑定返回值，避免手写镜像漂移。
type AppInfo = Awaited<ReturnType<typeof GetAppInfo>>
const info = ref<AppInfo | null>(null)
const actionError = ref<AppError | null>(null)
const exportHint = ref('')
const copyHint = ref('')
const notifyHint = ref('')
const autostartState = ref<'unknown' | 'enabled' | 'disabled'>('unknown')
const shortcutInput = ref('')
const shortcutHint = ref('')
const mcpTokenSet = ref(false)
const mcpHint = ref('')
const importHint = ref('')

// ---- AI 连接配置 ----
// 密钥走「写只写、读脱敏」：读取路径永远拿不到明文（见 ai.PublicSettings），
// 故输入框只用于"重新填写"，留空即保持原值不变。
type AIPublic = Awaited<ReturnType<typeof GetAISettings>>
const aiPublic = ref<AIPublic | null>(null)
const aiBaseURL = ref('')
const aiModel = ref('')
const aiAPIKey = ref('')
const aiHint = ref('')
const aiSaving = ref(false)
const aiTesting = ref(false)

// ---- 提醒行为 ----
// 用四个独立 ref 而不是一个可空对象：模板里 v-model 绑定到 `x.y` 时，
// x 为 null 会让 vue-tsc 直接报"可能为 null"，拆开就没有这个问题。
const quietStart = ref('22:00')
const quietEnd = ref('08:00')
const snoozeMinutes = ref(5)
const notifyEnabled = ref(true)
// 全屏强提醒：属于【提醒设置】（到点怎么提醒），不是宠物设置。
// 之所以不用 @change 立即保存：它与上面的静默时段/推迟分钟同属一组，
// 一起点「保存提醒设置」提交，避免"改一半就已经生效"。
const strongAlertEnabled = ref(true)
const reminderHint = ref('')
const reminderSaving = ref(false)

// ---- 桌面宠物 ----
// 宠物包列表用绑定返回类型反推，不手写镜像（Go 侧字段一改这里立刻跟着变）。
type PetPack = NonNullable<Awaited<ReturnType<typeof ListPetPacks>>>[number]
const petPacks = ref<PetPack[]>([])
const petVisible = ref(true)
const petScale = ref(1)
const petPackID = ref('builtin')
const petHint = ref('')
const petImporting = ref(false)

/** 默认导出文件名：带日期，避免用户多次导出互相覆盖。 */
function defaultBackupName(): string {
  const d = new Date()
  const pad = (n: number) => String(n).padStart(2, '0')
  return `backup-${d.getFullYear()}${pad(d.getMonth() + 1)}${pad(d.getDate())}.db`
}

// 导出数据库：先经系统「保存」对话框取路径（用户在那里确认覆盖），再交给后端导出。
// 两个绑定各自单一职责，前端按流程组合——比做一个「一步到位」的绑定更灵活。
async function exportDatabase() {
  actionError.value = null
  exportHint.value = ''
  try {
    const target = await invoke(() => SelectSaveFile('导出数据库', defaultBackupName()))
    if (!target) return // 用户取消
    await invoke(() => ExportDatabase(target))
    exportHint.value = `已导出到 ${target}`
    logInfo('数据库已导出', { target })
  } catch (e) {
    actionError.value = e as AppError
  }
}

// 恢复数据库：选备份文件 → 系统确认框二次确认 → 导入（失败后端自动回滚）。
// 恢复成功后页面状态仍是旧的，提示用户重启应用（见 design.md D6 的已知限制）。
async function importDatabase() {
  actionError.value = null
  importHint.value = ''
  try {
    const source = await invoke(() => SelectFile('选择备份文件'))
    if (!source) return // 用户取消
    const ok = await invoke(() => Confirm('恢复数据库', '将用所选备份替换当前数据库，确定继续吗？'))
    if (!ok) return
    await invoke(() => ImportDatabase(source))
    importHint.value = '已恢复，建议重启应用以加载新数据'
    logInfo('数据库已恢复', { source })
  } catch (e) {
    actionError.value = e as AppError
  }
}

// 剪贴板示范：把数据目录路径写进剪贴板（报障时让用户贴给别人）。
async function copyDataDir() {
  actionError.value = null
  copyHint.value = ''
  try {
    const dir = info.value?.data_dir ?? (await invoke(() => GetDataDir()))
    await invoke(() => SetClipboardText(dir))
    copyHint.value = '已复制'
    // 读回演练：剪贴板能力是成对的（写/读），读回失败说明平台侧异常。
    const back = await invoke(() => GetClipboardText())
    if (back !== dir) logError('剪贴板读回不一致', { expected: dir, got: back })
  } catch (e) {
    actionError.value = e as AppError
  }
}

// 系统通知：先请求授权（首次会弹系统授权框），再发测试通知。
async function requestNotifyPermission() {
  actionError.value = null
  notifyHint.value = ''
  try {
    const granted = await invoke(() => RequestNotificationPermission())
    notifyHint.value = granted ? '已授权' : '未授权'
  } catch (e) {
    actionError.value = e as AppError
  }
}

async function sendTestNotification() {
  actionError.value = null
  notifyHint.value = ''
  try {
    await invoke(() => SendNotification('测试通知', `来自 ${new Date().toLocaleTimeString()}`))
    notifyHint.value = '已发送'
  } catch (e) {
    actionError.value = e as AppError
  }
}

// 开机自启：状态查系统真实注册（macOS dev 态会报平台错误，如实展示）。
async function toggleAutostart() {
  actionError.value = null
  try {
    const enable = autostartState.value !== 'enabled'
    await invoke(() => SetAutostart(enable))
    await refreshAutostart()
  } catch (e) {
    actionError.value = e as AppError
  }
}

async function refreshAutostart() {
  try {
    autostartState.value = (await invoke(() => GetAutostartEnabled())) ? 'enabled' : 'disabled'
  } catch (e) {
    autostartState.value = 'unknown'
    actionError.value = e as AppError
  }
}

// 全局快捷键：输入 v3 accelerator 语法（如 CmdOrCtrl+Shift+Space），空 = 关闭。
// 注册失败（被其他应用占用）后端返回错误且保持原快捷键。
async function saveShortcut() {
  actionError.value = null
  shortcutHint.value = ''
  try {
    await invoke(() => SetToggleWindowShortcut(shortcutInput.value.trim()))
    shortcutHint.value = shortcutInput.value.trim() ? '已保存并生效' : '已关闭'
    logInfo('全局快捷键已更新', { accelerator: shortcutInput.value.trim() })
  } catch (e) {
    actionError.value = e as AppError
  }
}

// MCP token：只生成/落盘，运行中的 MCP 服务在重启后启用鉴权（dev-mcp 场景）。
async function rotateMcpToken() {
  actionError.value = null
  mcpHint.value = ''
  try {
    await invoke(() => RotateMCPToken())
    mcpTokenSet.value = true
    mcpHint.value = '已生成，重启后生效'
  } catch (e) {
    actionError.value = e as AppError
  }
}

// 数据目录：数据库/配置/日志同目录。出问题时用户能自己把日志捞出来。
async function openDataDir() {
  actionError.value = null
  try {
    await invoke(() => OpenDataDir())
  } catch (e) {
    actionError.value = e as AppError
  }
}

// ---- AI 连接配置 ----
// 密钥是「写只写、读脱敏」：读取路径永远拿不到明文（见 ai.PublicSettings），
// 故输入框只用于重新填写，留空即保持原值不变（见 config.SaveAISettings）。

/** 读取 AI 配置并回显（密钥回显为脱敏串，输入框留空）。 */
async function refreshAISettings() {
  try {
    aiPublic.value = await invoke(() => GetAISettings(), 'GetAISettings')
    aiBaseURL.value = aiPublic.value?.base_url ?? ''
    aiModel.value = aiPublic.value?.model ?? ''
    aiAPIKey.value = ''
  } catch (e) {
    actionError.value = e as AppError
  }
}

/** 保存 AI 配置。 */
async function saveAISettings() {
  actionError.value = null
  aiHint.value = ''
  aiSaving.value = true
  try {
    await invoke(
      () => SetAISettings(aiBaseURL.value.trim(), aiModel.value.trim(), aiAPIKey.value.trim()),
      'SetAISettings',
    )
    aiHint.value = '已保存'
    logInfo('AI 配置已保存', { model: aiModel.value.trim() })
    await refreshAISettings()
  } catch (e) {
    actionError.value = e as AppError
  } finally {
    aiSaving.value = false
  }
}

/** 测试连接：先落盘再试连，避免"测的是旧配置"这种误导。 */
async function testAIConnection() {
  actionError.value = null
  aiHint.value = ''
  aiTesting.value = true
  try {
    await invoke(
      () => SetAISettings(aiBaseURL.value.trim(), aiModel.value.trim(), aiAPIKey.value.trim()),
      'SetAISettings',
    )
    aiHint.value = await invoke(() => TestAIConnection(), 'TestAIConnection')
    await refreshAISettings()
  } catch (e) {
    actionError.value = e as AppError
  } finally {
    aiTesting.value = false
  }
}

// ---- 提醒行为 ----

/** 读取提醒行为设置。 */
async function refreshReminderSettings() {
  try {
    const current = await invoke(() => GetReminderSettings(), 'GetReminderSettings')
    quietStart.value = current.quiet_start
    quietEnd.value = current.quiet_end
    snoozeMinutes.value = current.snooze_minutes
    notifyEnabled.value = current.notify_enabled
    strongAlertEnabled.value = current.strong_alert_enabled
  } catch (e) {
    actionError.value = e as AppError
  }
}

/** 保存提醒行为设置（静默时段格式由后端校验）。 */
async function saveReminderSettings() {
  actionError.value = null
  reminderHint.value = ''
  reminderSaving.value = true
  try {
    await invoke(
      () =>
        SetReminderSettings({
          quiet_start: quietStart.value.trim(),
          quiet_end: quietEnd.value.trim(),
          snooze_minutes: snoozeMinutes.value,
          notify_enabled: notifyEnabled.value,
          strong_alert_enabled: strongAlertEnabled.value,
        }),
      'SetReminderSettings',
    )
    reminderHint.value = '已保存并生效'
    await refreshReminderSettings()
  } catch (e) {
    actionError.value = e as AppError
  } finally {
    reminderSaving.value = false
  }
}

// ---- 桌面宠物 ----

/**
 * 读取宠物设置与可用素材包。
 *
 * 两个调用分开写：素材包列表可能因为用户刚删了目录而失败，但那不该让
 * 开关/缩放也回显不出来（设置页的半边表格比整页空白有用）。
 */
async function refreshPet() {
  try {
    const current = await invoke(() => GetPetSettings(), 'GetPetSettings')
    petVisible.value = current.pet_visible
    petScale.value = current.pet_scale
    petPackID.value = current.pet_pack_id
  } catch (e) {
    actionError.value = e as AppError
  }
  try {
    petPacks.value = (await invoke(() => ListPetPacks(), 'ListPetPacks')) ?? []
  } catch (e) {
    petPacks.value = []
    actionError.value = e as AppError
  }
}

/**
 * 保存宠物设置并把结果回显。
 *
 * 后端在保存后【立即】作用到窗口（开关决定显隐、缩放决定窗口尺寸），
 * 故这里不需要再调一次 ShowPet/HidePet。
 */
async function savePetSettings() {
  actionError.value = null
  petHint.value = ''
  try {
    const message = await invoke(
      () =>
        SetPetSettings({
          pet_visible: petVisible.value,
          pet_scale: petScale.value,
          pet_pack_id: petPackID.value,
        }),
      'SetPetSettings',
    )
    petHint.value = message || '已保存'
    logInfo('宠物设置已保存', {
      visible: petVisible.value,
      scale: petScale.value,
      pack: petPackID.value,
    })
  } catch (e) {
    actionError.value = e as AppError
  }
}

/**
 * 导入宠物包：选文件 → 后端复制进数据目录 → 刷新列表并切到新包。
 *
 * 返回 null 表示用户在选择框里取消了，此时安静返回（不是错误）。
 */
async function importPetPack() {
  actionError.value = null
  petHint.value = ''
  petImporting.value = true
  try {
    const pack = await invoke(() => ImportPetPack(), 'ImportPetPack')
    if (!pack) return
    petPacks.value = (await invoke(() => ListPetPacks(), 'ListPetPacks')) ?? []
    petPackID.value = pack.id
    await savePetSettings()
    petHint.value = `已导入：${pack.name}`
  } catch (e) {
    actionError.value = e as AppError
  } finally {
    petImporting.value = false
  }
}

// 双保险：AppInfo 已含数据目录，若它整条拿不到（极端情况），退回单独查。
// 顺带拉起各设置项的初始状态。
onMounted(async () => {
  try {
    info.value = await invoke(() => GetAppInfo())
  } catch (e) {
    actionError.value = e as AppError
    try {
      info.value = { data_dir: await invoke(() => GetDataDir()) } as AppInfo
    } catch {
      // 两次都失败：错误已记录，界面展示占位即可
    }
  }
  try {
    shortcutInput.value = await invoke(() => GetToggleWindowShortcut())
  } catch {
    // 快捷键状态拿不到不影响页面其余部分
  }
  try {
    mcpTokenSet.value = (await invoke(() => GetMCPToken())) !== ''
  } catch {
    // 同上
  }
  await refreshAutostart()
  await refreshAISettings()
  await refreshReminderSettings()
  await refreshPet()
})
</script>

<template>
  <section class="page">
    <h1 class="page__title">设置</h1>

    <div class="setting">
      <span class="setting__label">主题</span>
      <button
        class="setting__btn"
        :class="{ 'setting__btn--active': themeMode === '' }"
        @click="onThemeChange('')"
      >
        跟随系统
      </button>
      <button
        class="setting__btn"
        :class="{ 'setting__btn--active': theme === 'light' && themeMode !== '' }"
        @click="onThemeChange('light')"
      >
        亮色
      </button>
      <button
        class="setting__btn"
        :class="{ 'setting__btn--active': theme === 'dark' && themeMode !== '' }"
        @click="onThemeChange('dark')"
      >
        暗色
      </button>
    </div>

    <div class="setting">
      <span class="setting__label">数据目录</span>
      <span class="setting__path" :title="info?.data_dir">{{ info?.data_dir || '—' }}</span>
      <button class="setting__btn" @click="openDataDir">打开</button>
      <button class="setting__btn" @click="copyDataDir">复制</button>
      <span v-if="copyHint" class="setting__muted">{{ copyHint }}</span>
    </div>

    <div class="setting">
      <span class="setting__label">版本</span>
      <span class="setting__path">{{ info?.version || '—' }}</span>
      <span class="setting__muted">{{ info?.platform || '' }}</span>
    </div>

    <div class="setting">
      <span class="setting__label">日志文件</span>
      <span class="setting__path" :title="info?.log_file">{{ info?.log_file || '—' }}</span>
    </div>

    <div class="setting">
      <span class="setting__label">数据备份</span>
      <button class="setting__btn" @click="exportDatabase">导出数据库…</button>
      <button class="setting__btn" @click="importDatabase">恢复数据库…</button>
      <span v-if="exportHint" class="setting__muted" :title="exportHint">已导出</span>
      <span v-if="importHint" class="setting__muted">{{ importHint }}</span>
    </div>

    <div class="setting">
      <span class="setting__label">系统通知</span>
      <button class="setting__btn" @click="requestNotifyPermission">请求授权</button>
      <button class="setting__btn" @click="sendTestNotification">发送测试通知</button>
      <span v-if="notifyHint" class="setting__muted">{{ notifyHint }}</span>
    </div>

    <div class="setting">
      <span class="setting__label">开机自启</span>
      <button class="setting__btn" @click="toggleAutostart">
        {{ autostartState === 'enabled' ? '停用' : '启用' }}
      </button>
      <span class="setting__muted">
        {{ autostartState === 'enabled' ? '已启用' : autostartState === 'disabled' ? '未启用' : '状态未知（打包后可用）' }}
      </span>
    </div>

    <div class="setting">
      <span class="setting__label">全局快捷键</span>
      <input
        v-model="shortcutInput"
        class="setting__input"
        placeholder="如 CmdOrCtrl+Shift+Space，留空关闭"
        spellcheck="false"
      />
      <button class="setting__btn" @click="saveShortcut">保存</button>
      <span v-if="shortcutHint" class="setting__muted">{{ shortcutHint }}</span>
    </div>

    <div class="setting">
      <span class="setting__label">MCP Token</span>
      <span class="setting__muted">{{ mcpTokenSet ? '已设置' : '未设置' }}</span>
      <button class="setting__btn" @click="rotateMcpToken">生成新 Token</button>
      <span v-if="mcpHint" class="setting__muted">{{ mcpHint }}</span>
    </div>
    <p class="setting__note">仅 dev-mcp 构建生效：设置后 MCP 请求需携带 Bearer Token，重启应用后生效。</p>

    <h2 class="setting__section">AI 生成计划</h2>

    <div class="setting">
      <span class="setting__label">接口地址</span>
      <input
        v-model="aiBaseURL"
        class="setting__input setting__input--wide"
        placeholder="https://api.deepseek.com/v1"
        spellcheck="false"
      />
    </div>

    <div class="setting">
      <span class="setting__label">模型</span>
      <input
        v-model="aiModel"
        class="setting__input"
        placeholder="deepseek-v4.1-flash"
        spellcheck="false"
      />
    </div>

    <div class="setting">
      <span class="setting__label">API Key</span>
      <input
        v-model="aiAPIKey"
        class="setting__input setting__input--wide"
        type="password"
        :placeholder="aiPublic?.api_key_masked || '尚未设置'"
        spellcheck="false"
      />
      <span class="setting__muted">
        {{ aiPublic?.api_key_set ? '已保存，留空则不修改' : '未设置' }}
      </span>
    </div>

    <div class="setting">
      <span class="setting__label">连接</span>
      <button class="setting__btn" :disabled="aiSaving" @click="saveAISettings">
        {{ aiSaving ? '保存中…' : '保存' }}
      </button>
      <button class="setting__btn" :disabled="aiTesting" @click="testAIConnection">
        {{ aiTesting ? '测试中…' : '测试连接' }}
      </button>
      <span v-if="aiHint" class="setting__muted">{{ aiHint }}</span>
    </div>

    <p class="setting__note">
      AI 只在「生成计划」时调用一次；之后所有提醒都由本地定时器触发，断网也能用。
    </p>

    <h2 class="setting__section">提醒行为</h2>

    <div class="setting">
      <span class="setting__label">静默时段</span>
      <input
        v-model="quietStart"
        class="setting__input setting__input--narrow"
        placeholder="22:00"
        spellcheck="false"
      />
      <span class="setting__muted">至</span>
      <input
        v-model="quietEnd"
        class="setting__input setting__input--narrow"
        placeholder="08:00"
        spellcheck="false"
      />
      <span class="setting__muted">这段时间不弹通知，但仍会记录</span>
    </div>

    <div class="setting">
      <span class="setting__label">推迟分钟</span>
      <el-input-number
        v-model="snoozeMinutes"
        :min="1"
        :max="120"
        :step="1"
        size="small"
        controls-position="right"
      />
      <span class="setting__muted">点「推迟」后多久再提醒一次</span>
    </div>

    <div class="setting">
      <span class="setting__label">系统通知</span>
      <el-switch v-model="notifyEnabled" />
      <span class="setting__muted">关闭后提醒仍会记录，并显示在首页待处理卡片</span>
    </div>

    <div class="setting">
      <span class="setting__label">全屏提醒</span>
      <el-switch v-model="strongAlertEnabled" />
      <span class="setting__muted">到点时全屏提醒，不容易漏掉</span>
    </div>

    <div class="setting">
      <span class="setting__label"></span>
      <button class="setting__btn" :disabled="reminderSaving" @click="saveReminderSettings">
        {{ reminderSaving ? '保存中…' : '保存提醒设置' }}
      </button>
      <span v-if="reminderHint" class="setting__muted">{{ reminderHint }}</span>
    </div>

    <h2 class="setting__section">桌面宠物</h2>

    <div class="setting">
      <span class="setting__label">显示宠物</span>
      <el-switch v-model="petVisible" @change="savePetSettings" />
      <span class="setting__muted">关掉只是隐藏窗口，提醒仍会记录并进首页的待处理卡片</span>
    </div>

    <div class="setting">
      <span class="setting__label">形象</span>
      <el-select
        v-model="petPackID"
        class="setting__select"
        size="small"
        @change="savePetSettings"
      >
        <el-option v-for="pack in petPacks" :key="pack.id" :label="pack.name" :value="pack.id" />
      </el-select>
      <el-button size="small" :loading="petImporting" @click="importPetPack">
        导入宠物包…
      </el-button>
      <span v-if="petHint" class="setting__muted">{{ petHint }}</span>
    </div>

    <div class="setting">
      <span class="setting__label">缩放</span>
      <el-input-number
        v-model="petScale"
        :min="0.5"
        :max="2"
        :step="0.1"
        :precision="1"
        size="small"
        controls-position="right"
        @change="savePetSettings"
      />
      <span class="setting__muted">窗口会随缩放一起变大，保证宠物不被裁掉</span>
    </div>

    <p class="setting__note">
      素材可从 codexpet.top 下载（pet.json + spritesheet.webp）后点「导入宠物包」；
      若你已在 Codex 客户端装过宠物，这里会自动列出 ~/.codex/pets 下的素材，无需导入。
    </p>

    <p v-if="actionError" class="setting__error">[{{ actionError.code }}] {{ actionError.message }}</p>
  </section>
</template>

<style scoped>
.page__title {
  font-size: var(--font-size-xl);
  margin: 0 0 var(--space-4);
}
.setting {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  margin-bottom: var(--space-3);
}
.setting__label {
  flex: none;
  width: 72px;
  color: var(--text-2);
}
.setting__path {
  max-width: 420px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text-3);
  font-size: var(--font-size-sm);
}
.setting__muted {
  color: var(--text-3);
  font-size: var(--font-size-sm);
}
.setting__note {
  margin: calc(-1 * var(--space-2)) 0 var(--space-3) 84px;
  color: var(--text-3);
  font-size: var(--font-size-xs);
}
.setting__error {
  color: var(--color-danger);
}
.setting__btn {
  flex: none;
  border: 1px solid var(--border-subtle);
  background: var(--bg-raised);
  color: var(--text-2);
  padding: var(--space-2) var(--space-4);
  border-radius: var(--radius-md);
  cursor: pointer;
  transition: all var(--motion-fast) var(--ease-standard);
}
.setting__btn--active {
  border-color: var(--color-primary);
  color: var(--color-primary);
  background: var(--color-primary-soft);
}
.setting__input {
  width: 260px;
  border: 1px solid var(--border-subtle);
  background: var(--bg-base);
  color: var(--text-1);
  padding: var(--space-2) var(--space-3);
  border-radius: var(--radius-md);
  font-size: var(--font-size-sm);
}
.setting__input:focus {
  outline: none;
  border-color: var(--color-primary);
}
/* 分组标题：把「AI」「提醒行为」与上面的通用设置分开，避免一长串同级行 */
.setting__section {
  font-size: var(--font-size-md);
  color: var(--text-2);
  margin: var(--space-6) 0 var(--space-3);
  padding-top: var(--space-4);
  border-top: 1px solid var(--border-subtle);
}
/* 地址类输入框要放得下完整 URL */
.setting__input--wide {
  width: 360px;
}
/* 钟点输入框只放 HH:MM */
.setting__input--narrow {
  width: 88px;
}
/* 宠物包下拉：名字可能较长（"内置小宠 / 像素小狗"），给足宽度但别撑破一行 */
.setting__select {
  width: 200px;
}
</style>
