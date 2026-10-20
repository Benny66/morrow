import { createRouter, createWebHashHistory } from 'vue-router'
import type { RouteRecordRaw } from 'vue-router'

// 路由骨架：首页/设置为占位，具体内容由 example-module 填充。
const routes: RouteRecordRaw[] = [
  {
    path: '/',
    name: 'home',
    component: () => import('../views/Home.vue'),
    meta: { title: '仪表盘' },
  },
  {
    path: '/settings',
    name: 'settings',
    component: () => import('../views/Settings.vue'),
    meta: { title: '设置' },
  },
// gen:route
]

const router = createRouter({
  // Wails 打包后是 file:// 协议，用 hash 历史避免路由刷新 404。
  history: createWebHashHistory(),
  routes,
})

export default router
