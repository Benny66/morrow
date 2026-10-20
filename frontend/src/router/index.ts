import { createRouter, createWebHashHistory } from 'vue-router'
import type { RouteRecordRaw } from 'vue-router'

// 路由 meta 的类型扩展（声明合并）。
//
// 【为什么必须扩展而不是 as any】meta 的类型是 RouteMeta，默认不带业务字段。
// 直接 `route.meta.bare` 在 vue-tsc（strict）下报「属性不存在」；用 as any 糊过去
// 等于让这个判据在类型系统里隐形——写错成 meta.brar 也照样编译通过，
// 而症状是"宠物窗口里出现了整个应用壳"。
declare module 'vue-router' {
  interface RouteMeta {
    /** 页面标题（侧栏/关于界面用）。 */
    title?: string
    /**
     * 是否为「裸页面」：true 时不套 AppShell。
     *
     * 只有桌面宠物窗口用它（同一份前端产物被两个窗口共用，
     * 宠物窗口需要一片纯透明的空白画布）。见 App.vue 的说明。
     */
    bare?: boolean
  }
}

// 路由骨架：首页为仪表盘，其余为提醒应用的三个功能页，外加宠物窗口的裸页面。
const routes: RouteRecordRaw[] = [
  {
    path: '/',
    name: 'home',
    component: () => import('../views/Home.vue'),
    meta: { title: '仪表盘' },
  },
  {
    path: '/scenarios',
    name: 'scenarios',
    component: () => import('../views/Scenarios.vue'),
    meta: { title: '场景' },
  },
  {
    path: '/plan',
    name: 'plan',
    component: () => import('../views/Plan.vue'),
    meta: { title: '计划' },
  },
  {
    path: '/settings',
    name: 'settings',
    component: () => import('../views/Settings.vue'),
    meta: { title: '设置' },
  },
  {
    // 宠物窗口专用路由：宠物窗口创建时 URL 为 /#/pet（见 pet_window.go）。
    // meta.bare 让它跳过 AppShell——否则透明小窗里会画出侧栏与标题栏。
    path: '/pet',
    name: 'pet',
    component: () => import('../views/Pet.vue'),
    meta: { title: '桌面宠物', bare: true },
  },
  {
    // 全屏强提醒窗口专用路由：该窗口创建时 URL 为 /#/alert（见 alert_window.go）。
    // meta.bare 同样必需——否则这个铺满屏幕的窗口里会画出侧栏与标题栏，
    // 半透明遮罩也就不可能成立。
    path: '/alert',
    name: 'alert',
    component: () => import('../views/Alert.vue'),
    meta: { title: '强提醒', bare: true },
  },
// gen:route
]

const router = createRouter({
  // Wails 打包后是 file:// 协议，用 hash 历史避免路由刷新 404。
  history: createWebHashHistory(),
  routes,
})

export default router
