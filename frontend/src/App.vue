<script lang="ts" setup>
// 根组件：决定这一屏是「完整应用」还是「裸页面」。
//
// 【为什么需要这个判断】桌面宠物是【同一个前端产物】里的另一个窗口（URL 为 /#/pet）。
// 若这里无条件渲染 AppShell，宠物窗口里会画出侧栏 + 标题栏 + 内容区——
// 一个 240×360 的透明小窗里塞进整个应用壳，既不透明也无法使用。
//
// 判据是路由 meta.bare（类型扩展见 router/index.ts 的 RouteMeta 声明合并）：
//   - bare === true → 只渲染 router-view（宠物窗口）；
//   - 其余          → 照旧走 AppShell（主窗口）。
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import AppShell from './layouts/AppShell.vue'

const route = useRoute()

// 用 computed 而不是直接读 route.meta.bare：路由切换时 meta 会变，
// 模板里的表达式需要跟着重算（直接读只在首次渲染求值）。
const bare = computed(() => route.meta.bare === true)
</script>

<template>
  <router-view v-if="bare" />
  <AppShell v-else />
</template>
