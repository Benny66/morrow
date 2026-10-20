<script lang="ts" setup>
// 骨架屏：加载态占位，避免白屏 spinner。
defineProps<{ rows?: number }>()
</script>

<template>
  <div class="skeleton">
    <div
      v-for="i in rows ?? 5"
      :key="i"
      class="skeleton__row"
      :style="{ animationDelay: `${(i - 1) * 60}ms` }"
    />
  </div>
</template>

<style scoped>
.skeleton {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}
.skeleton__row {
  height: 40px;
  border-radius: var(--radius-md);
  background: var(--bg-hover);
  animation: pulse 1.2s var(--ease-in-out) infinite;
}
@keyframes pulse {
  0%,
  100% {
    opacity: 1;
  }
  50% {
    opacity: 0.5;
  }
}
</style>
