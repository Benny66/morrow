<script lang="ts" setup>
import { onMounted, ref } from 'vue'
import { storeToRefs } from 'pinia'
import { useAppStore } from '../stores/app'
// v3 的绑定路径见 stores/app.ts 的说明。
import {
  Confirm,
  ExportDatabase,
  GetAppInfo,
  GetAutostartEnabled,
  GetClipboardText,
  GetDataDir,
  GetMCPToken,
  GetToggleWindowShortcut,
  ImportDatabase,
  OpenDataDir,
  RequestNotificationPermission,
  RotateMCPToken,
  SelectFile,
  SelectSaveFile,
  SendNotification,
  SetAutostart,
  SetClipboardText,
  SetToggleWindowShortcut,
} from '../../bindings/my-notice/app.js'
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
</style>
