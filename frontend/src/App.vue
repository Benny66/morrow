<script lang="ts" setup>
// 根组件：决定这一屏是「完整应用」「首次引导」还是「裸页面」。
//
// 【为什么需要 bare】桌面宠物是【同一个前端产物】里的另一个窗口（URL 为 /#/pet）。
// 若这里无条件渲染 AppShell，宠物窗口里会画出侧栏 + 标题栏 + 内容区——
// 一个 240×360 的透明小窗里塞进整个应用壳，既不透明也无法使用。
//
// 判据是路由 meta（类型扩展见 router/index.ts 的 RouteMeta 声明合并）：
//   - bare === true   → 只渲染 router-view（宠物 / 强提醒窗口）；
//   - wizard === true → 标题栏 + 居中内容，不渲染侧栏（首次引导）；
//   - 其余            → 照旧走 AppShell（主窗口）。
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import AppShell from './layouts/AppShell.vue'
import TitleBar from './components/TitleBar.vue'

const route = useRoute()

// 用 computed 而不是直接读 route.meta：路由切换时 meta 会变，
// 模板里的表达式需要跟着重算（直接读只在首次渲染求值）。
const bare = computed(() => route.meta.bare === true)
const wizard = computed(() => route.meta.wizard === true)
</script>

<template>
  <router-view v-if="bare" />
  <div v-else-if="wizard" class="wizard">
    <TitleBar />
    <main class="wizard__content">
      <router-view />
    </main>
  </div>
  <AppShell v-else />
</template>

<style scoped>
.wizard {
  display: flex;
  flex-direction: column;
  height: 100vh;
  background: var(--bg-base);
  color: var(--text-1);
}

.wizard__content {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  display: flex;
  justify-content: center;
  align-items: flex-start;
  padding: clamp(24px, 8vh, 96px) var(--space-6) var(--space-6);
}
</style>
