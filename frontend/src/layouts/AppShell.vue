<script lang="ts" setup>
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import TitleBar from '../components/TitleBar.vue'
import { useAppStore } from '../stores/app'

const store = useAppStore()
const route = useRoute()

// 侧栏菜单项（占位，后续模块可扩展）
const menuItems = [
  { path: '/', label: '仪表盘', icon: '◈' },
  { path: '/settings', label: '设置', icon: '⚙' },
  // gen:menu
]

const collapsed = computed(() => store.sidebarCollapsed)
</script>

<template>
  <div class="shell" :class="{ 'shell--collapsed': collapsed }">
    <TitleBar />

    <div class="shell__body">
      <aside class="sidebar">
        <nav class="sidebar__nav">
          <router-link
            v-for="item in menuItems"
            :key="item.path"
            :to="item.path"
            class="sidebar__item"
            :class="{ 'sidebar__item--active': route.path === item.path }"
            :title="item.label"
          >
            <span class="sidebar__icon">{{ item.icon }}</span>
            <span v-if="!collapsed" class="sidebar__label">{{ item.label }}</span>
          </router-link>
        </nav>

        <button class="sidebar__toggle" @click="store.toggleSidebar()">
          {{ collapsed ? '»' : '«' }}
        </button>
      </aside>

      <main class="content">
        <transition name="fade" mode="out-in">
          <router-view :key="route.path" />
        </transition>
      </main>
    </div>
  </div>
</template>

<style scoped>
.shell {
  display: flex;
  flex-direction: column;
  height: 100vh;
  background: var(--bg-base);
  color: var(--text-1);
}

.shell__body {
  flex: 1;
  display: flex;
  min-height: 0;
}

/* 侧栏 */
.sidebar {
  display: flex;
  flex-direction: column;
  width: 200px;
  flex: none;
  background: var(--bg-raised);
  border-right: 1px solid var(--border-subtle);
  transition: width var(--motion-normal) var(--ease-standard);
}

.shell--collapsed .sidebar {
  width: 56px;
}

.sidebar__nav {
  flex: 1;
  padding: var(--space-2);
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
}

.sidebar__item {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-2) var(--space-3);
  border-radius: var(--radius-md);
  color: var(--text-2);
  white-space: nowrap;
  overflow: hidden;
  transition: background-color var(--motion-fast) var(--ease-standard),
    color var(--motion-fast) var(--ease-standard);
}

.sidebar__item:hover {
  background: var(--bg-hover);
  color: var(--text-1);
}

.sidebar__item--active {
  background: var(--color-primary-soft);
  color: var(--color-primary);
}

.sidebar__icon {
  flex: none;
  width: 20px;
  text-align: center;
}

.sidebar__toggle {
  border: none;
  background: transparent;
  color: var(--text-3);
  padding: var(--space-3);
  cursor: pointer;
  border-top: 1px solid var(--border-subtle);
  transition: color var(--motion-fast) var(--ease-standard);
}

.sidebar__toggle:hover {
  color: var(--text-1);
}

/* 内容区 */
.content {
  flex: 1;
  min-width: 0;
  overflow-y: auto;
  padding: var(--space-6);
}

/* 路由切换过渡 */
.fade-enter-active,
.fade-leave-active {
  transition: opacity var(--motion-fast) var(--ease-standard),
    transform var(--motion-fast) var(--ease-standard);
}
.fade-enter-from {
  opacity: 0;
  transform: translateY(4px);
}
.fade-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}
</style>
