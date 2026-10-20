<script lang="ts" setup>
import { onMounted, ref } from 'vue'
import { Window } from '@wailsio/runtime'

// 标题栏：
//  - 拖拽用 Wails 自定义属性 --wails-draggable: drag（非 Electron 的 -webkit-app-region）。
//  - CSS 自定义属性会继承，故按钮/控件必须显式 no-drag 覆盖，否则点击会误触发拖拽。
//  - Windows 自绘三按钮（最小化/最大化/关闭），通过 wails 运行时控制窗口。
//  - macOS 保留原生交通灯，不自绘按钮（内容顶到顶）。
//
// v3 的窗口控制与 v2 有两处实质差别（见下），这是本文件唯一的改动原因：
//   1. 入口从 `wailsjs/runtime/runtime` 的自由函数（WindowMinimise…）变成
//      `@wailsio/runtime` 的 **Window 单例对象**（方法名去掉 Window 前缀）。
//   2. 【调用是异步的】：每个方法返回 Promise，失败是 reject 而非同步抛错。
//      故这里每个调用都挂 .catch —— 老代码那套 try/catch 接不住 Promise。
//
// 为什么不用动态 import：v2 那样做是为了在纯 vite dev 下「import 就炸」时留条命。
// v3 的 Window 模块导入本身是安全的（只是拿到一个对象），真正的失败发生在调用时，
// 而那已经由 .catch 兜住了。多一层动态 import 只会让类型标注变复杂。

// 是否 macOS：决定是否隐藏自绘三按钮
const isMac = navigator.userAgent.includes('Macintosh')

// 是否最大化：用于切换最大化/还原图标
const maximised = ref(false)

// 非 wails 环境（纯 vite dev）下这些调用会 reject，静默忽略即可——
// 标题栏按钮在那时本来就没有窗口可控制。
function minimise() {
  void Window.Minimise().catch(() => {})
}

function toggleMaximise() {
  void Window.ToggleMaximise().catch(() => {})
  // 乐观翻转：v3 没有同步返回最大化状态，而再查一次是额外一次往返。
  // 与系统实际状态短暂不一致无害（图标而已），下次 onMounted 会纠正。
  maximised.value = !maximised.value
}

function close() {
  // 走 Window.Close() 而不是 Window.Hide()：Close() 会发出 WindowClosing 事件、进入
  // main.go 注册的关窗钩子，由 Go 侧【统一决策】——托盘在则隐藏、托盘缺失则真正退出。
  // 若直接 Hide() 则绕开该决策：托盘起失败时会留下一个没有窗口、也没有托盘的残留进程。
  // 这样自绘关闭按钮与 macOS 原生交通灯走的是同一条决策路径。
  void Window.Close().catch(() => {})
}

onMounted(async () => {
  if (isMac) return
  try {
    // 读取初始最大化状态（尽力而为，失败不影响）
    maximised.value = await Window.IsMaximised()
  } catch {
    /* 忽略 */
  }
})
</script>

<template>
  <header class="titlebar" :class="{ 'titlebar--mac': isMac }">
    <div v-if="isMac" class="titlebar__traffic" />
    <div class="titlebar__drag" />

    <div v-if="!isMac" class="titlebar__controls">
      <button class="titlebar__btn" title="最小化" @click="minimise">—</button>
      <button class="titlebar__btn" :title="maximised ? '还原' : '最大化'" @click="toggleMaximise">
        {{ maximised ? '▣' : '□' }}
      </button>
      <button class="titlebar__btn titlebar__btn--close" title="关闭" @click="close">✕</button>
    </div>
  </header>
</template>

<style scoped>
.titlebar {
  position: relative;
  height: 40px;
  flex: none;
  display: flex;
  align-items: center;
  /* 整条标题栏作为拖拽区（Wails 自定义属性，会继承给子元素） */
  --wails-draggable: drag;
  user-select: none;
}

.titlebar__drag {
  flex: 1;
  height: 100%;
}

/* macOS 交通灯安全区：左上角留空，且不可拖拽（让交通灯可点击） */
.titlebar__traffic {
  flex: none;
  width: 72px;
  height: 100%;
  --wails-draggable: no-drag;
}

.titlebar__controls {
  display: flex;
  align-items: center;
  height: 100%;
  /* 按钮区排除拖拽（覆盖继承来的 drag） */
  --wails-draggable: no-drag;
}

.titlebar__btn {
  width: 46px;
  height: 100%;
  border: none;
  background: transparent;
  color: var(--text-2);
  font-size: 14px;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: background-color var(--motion-fast) var(--ease-standard);
}

.titlebar__btn:hover {
  background: var(--bg-hover);
  color: var(--text-1);
}

.titlebar__btn--close:hover {
  background: var(--color-danger);
  color: var(--color-on-danger);
}
</style>
