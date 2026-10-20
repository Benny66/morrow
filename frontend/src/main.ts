import { createApp } from 'vue'
import { createPinia } from 'pinia'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import App from './App.vue'
import router from './router'
import { useAppStore } from './stores/app'
import { createVueErrorHandler, installGlobalErrorHandler } from './lib/error-report'
import { ipcStats } from './lib/invoke'
import { logInfo } from './lib/log'
import './styles/tokens.css'
import './styles/theme.css'
import './styles/element.css'
import './styles/index.css'

const app = createApp(App)

// 全局错误兜底必须在挂载【之前】装好，否则启动期的异常捕获不到。
// 生产态没有 DevTools，不装的话未捕获异常会彻底静默（既不显示也不留痕）。
installGlobalErrorHandler()
// Vue 组件树内的异常（渲染/生命周期）不在 window 事件覆盖范围内，单独挂。
app.config.errorHandler = createVueErrorHandler()

const pinia = createPinia()
app.use(pinia)
app.use(router)
app.use(ElementPlus)

// 挂载前同步初始主题到 <html>（await 确保 config 读取完成后再挂载，避免首屏闪烁）。
const themeStart = performance.now()
await useAppStore(pinia).initTheme()
const themeMs = performance.now() - themeStart

const mountStart = performance.now()
app.mount('#app')
const mountMs = performance.now() - mountStart

// ---- 启动分段（时间线的前端半段，Go 半段见 main.go 的「启动阶段」）----
//
// 关键点：Element Plus 等静态依赖是在【本文件执行之前】就被解析执行的，
// performance.now() 取在本文件顶部已经错过了那一段。故「到脚本就绪」只能取自
// 浏览环境的导航计时——这正是回答「1.4MB 前端产物让启动慢了多少」的唯一来源。
const nav = performance.getEntriesByType('navigation')[0] as PerformanceNavigationTiming | undefined
const toJsMs = Math.round(nav?.domContentLoadedEventEnd ?? 0)

logInfo('前端启动', {
  to_js_ms: toJsMs, // webview 启动 + 资源加载 + 全部静态依赖的解析执行
  theme_ms: Math.round(themeMs), // 挂载前的那次后端往返（首帧被它挡着）
  mount_ms: Math.round(mountMs),
  total_ms: Math.round(performance.now()), // 自导航开始
  ...ipcStats(), // 启动期绑定调用聚合：次数 / 最大耗时 / 最慢者
})
