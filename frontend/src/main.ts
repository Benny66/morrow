import { createApp } from 'vue'
import { createPinia } from 'pinia'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import App from './App.vue'
import router from './router'
import { useAppStore } from './stores/app'
import { createVueErrorHandler, installGlobalErrorHandler } from './lib/error-report'
import { invoke, ipcStats } from './lib/invoke'
import { logError, logInfo } from './lib/log'
import { ShouldShowOnboarding } from '../bindings/morrow/app.js'
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

// 启动逻辑必须收进异步函数，禁止在模块顶层 await。
//
// 【为什么】生产构建会把共用模块并进入口 chunk，路由 chunk 于是静态 import 入口；
// 入口若在顶层 await router.isReady()，就形成「入口等路由、路由等入口」的循环，
// 表现为打包版白屏且无任何报错（wails3 dev 各模块独立加载，看不出来）。
// 护栏见 frontend/scripts/check-entry-tla.mjs（挂在 npm run build 之后）。
async function bootstrap(): Promise<void> {
  // 挂载前同步初始主题到 <html>（await 确保 config 读取完成后再挂载，避免首屏闪烁）。
  const themeStart = performance.now()
  await useAppStore(pinia).initTheme()
  const themeMs = performance.now() - themeStart

  // 等首次导航解析完再挂载。
  //
  // 【为什么必须等】挂载那一刻 route 还是 START_LOCATION（path='/', meta={}），
  // 而宠物窗口的 URL 是 /#/pet、靠 meta.bare 跳过 AppShell（见 App.vue）。
  // 不等的话首帧按 '/' 渲染——一个 240×360 的透明小窗里会先闪出一整屏深色 AppShell，
  // 再切到宠物页。主窗看不出差别，宠物窗口上却是一块明显的黑方块。
  await router.isReady()

  // 首次引导门禁：必须在挂载前决定，否则首页会先闪一帧再跳走。
  // 宠物 / 强提醒窗口（meta.bare）跳过——它们与主界面的引导状态无关，
  // 若在这里重定向，宠物窗口会被换成一张向导页。
  if (router.currentRoute.value.meta.bare !== true) {
    try {
      const shouldShow = await invoke(() => ShouldShowOnboarding(), 'ShouldShowOnboarding')
      const onWelcome = router.currentRoute.value.path === '/welcome'
      if (shouldShow && !onWelcome) {
        await router.replace('/welcome')
      } else if (!shouldShow && onWelcome) {
        await router.replace('/')
      }
    } catch (err) {
      // 绑定失败（纯 vite dev / Go 侧异常）不阻塞启动；代价只是这次不显示引导。
      logError('首次引导状态检查失败', err)
    }
  }

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
}

// 不 await：入口模块必须立即结束求值。bootstrap 内的异常由全局 unhandledrejection 兜底。
void bootstrap()
