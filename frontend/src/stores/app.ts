import { defineStore } from 'pinia'
import { ref } from 'vue'
// v2 是 '../../wailsjs/go/main/App'；v3 的绑定落在 frontend/bindings/<go module 名>/ 下，
// 且文件带 .js 后缀（生成器自己内部也是这么 import 的，见 bindings/**/app.ts）。
import { GetTheme, SetTheme } from '../../bindings/my-notice/app.js'
import { invoke } from '../lib/invoke'
import { logError } from '../lib/log'

/** 用户选择的主题模式：'' = 跟随系统（与 Go 侧 config 的 theme 字段同形）。 */
type ThemeMode = '' | 'dark' | 'light'
/** 实际生效的主题（解析后的具体档位，写 data-theme 用的永远是它）。 */
type Theme = 'dark' | 'light'

// 检测系统偏好（含空值兜底：webview 无 matchMedia 时默认暗色）。
function systemPrefersDark(): boolean {
  try {
    return window.matchMedia?.('(prefers-color-scheme: dark)').matches ?? true
  } catch {
    return true
  }
}

// 应用全局状态：主题（三档）+ 侧栏折叠。
//
// 「模式」与「生效值」分开是跟随系统档的关键：config 存的是模式（''=跟随系统），
// 而 data-theme 需要一个具体值——跟随系统时由系统偏好解析而来，且要随系统
// 偏好实时联动（matchMedia change 监听），切到显式档位时摘掉监听。
export const useAppStore = defineStore('app', () => {
  // 初始值先取系统偏好占位，真正的"config 优先"在 initTheme 里完成。
  const theme = ref<Theme>(systemPrefersDark() ? 'dark' : 'light')
  const themeMode = ref<ThemeMode>('')
  const sidebarCollapsed = ref(false)

  // matchMedia 监听句柄：跟随系统档挂着，显式档位摘掉。lazy 建立以复用同一实例。
  let media: MediaQueryList | null = null
  const onMediaChange = () => {
    if (themeMode.value === '') applyResolved(systemPrefersDark() ? 'dark' : 'light')
  }

  function applyResolved(next: Theme) {
    theme.value = next
    document.documentElement.setAttribute('data-theme', next)
  }

  function watchSystem() {
    if (media) return
    try {
      media = window.matchMedia?.('(prefers-color-scheme: dark)') ?? null
      media?.addEventListener('change', onMediaChange)
    } catch {
      media = null
    }
  }

  function unwatchSystem() {
    media?.removeEventListener('change', onMediaChange)
    media = null
  }

  function setTheme(mode: ThemeMode) {
    themeMode.value = mode
    if (mode === '') {
      watchSystem()
      applyResolved(systemPrefersDark() ? 'dark' : 'light')
    } else {
      unwatchSystem()
      applyResolved(mode)
    }
    // 异步落盘到 config.json（Go 侧），失败不阻断切换。
    // 经 invoke 包装：错误归一化为 AppError 并写进 app.log —— 原来只 console.error，
    // 而打包后的应用没有 DevTools，那条错误等于没记。
    invoke(() => SetTheme(mode), 'SetTheme').catch((err) => {
      logError('主题持久化失败', err)
    })
  }

  // 启动时同步主题：config 优先，空串（或非 dark/light 的任何值）= 跟随系统。
  // 这是"前端调 Go"的活范例：GetTheme 读 config。
  async function initTheme() {
    let mode: ThemeMode
    try {
      // 经 invoke：这一步在挂载之前，是启动时间线上唯一的一次后端往返，
      // 故必须进 IPC 统计（见 main.ts 的启动分段）
      const saved = await invoke(() => GetTheme(), 'GetTheme')
      mode = saved === 'dark' || saved === 'light' ? saved : ''
    } catch {
      // 非 wails 环境（纯 vite dev）或调用失败，fallback 跟随系统
      mode = ''
    }
    setTheme(mode)
  }

  function toggleSidebar() {
    sidebarCollapsed.value = !sidebarCollapsed.value
  }

  return { theme, themeMode, sidebarCollapsed, setTheme, initTheme, toggleSidebar }
})
