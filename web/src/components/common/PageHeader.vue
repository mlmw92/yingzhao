<template>
  <div class="page-header">
    <div class="ph-text">
      <h2 class="ph-title">{{ title }}</h2>
      <p v-if="desc" class="ph-desc">{{ desc }}</p>
    </div>
    <div v-if="$slots.actions" class="ph-actions">
      <slot name="actions" />
    </div>
  </div>
</template>

<script setup>
// 全站统一页头：标题 + 描述 + 右侧操作区。
// 刻意不使用渐变文字——标题只是标题，层级靠字号与字重表达。
defineProps({
  title: { type: String, default: '' },
  desc: { type: String, default: '' },
})
</script>

<style scoped>
.page-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 16px;
  /* 操作区是 flex-shrink:0，一旦总宽超出就把标题压成一条窄竖列。
     允许换行后，窄屏是「标题一行、按钮另起一行」，而不是整页横向溢出。 */
  flex-wrap: wrap;
  margin-bottom: 20px;
}
.ph-text {
  min-width: 0;
}
.ph-title {
  font-size: var(--fs-2xl);
  font-weight: 600;
  line-height: 1.25;
  letter-spacing: -0.01em;
  color: var(--t1);
  /* 长 ID / 长资源名不要把容器撑破 */
  overflow-wrap: break-word;
}
.ph-desc {
  font-size: var(--fs-sm);
  color: var(--t3);
  margin-top: 4px;
}
.ph-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
  flex-wrap: wrap;
}
</style>
