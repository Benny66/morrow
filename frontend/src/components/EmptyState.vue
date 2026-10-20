<script lang="ts" setup>
// 空状态：占位 + 描述，避免用户面对空白。
defineProps<{ description?: string }>()
</script>

<template>
  <div class="empty">
    <div class="empty__icon">◎</div>
    <p class="empty__desc">{{ description ?? '暂无数据' }}</p>
  </div>
</template>

<style scoped>
.empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-10);
  color: var(--text-3);
}
.empty__icon {
  font-size: 32px;
  opacity: 0.6;
}
.empty__desc {
  font-size: var(--font-size-sm);
}
</style>
