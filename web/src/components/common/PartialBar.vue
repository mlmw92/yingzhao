<template>
  <div v-if="normalized.length" class="partial-bar">
    <el-icon class="pb-icon"><WarningFilled /></el-icon>
    <div class="pb-text">
      <span class="pb-title">{{ title }}</span>
      <span class="pb-list">{{ listText }}</span>
    </div>
    <el-button size="small" @click="$emit('retry')">{{ retryText }}</el-button>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { WarningFilled } from '@element-plus/icons-vue'

// 部分失败提示条：替代各页 `catch (e) { /* ignore */ }` 的静默吞错。
// 语义与"整体错误"不同——数据里有一部分成功、一部分失败时用它，避免整块被错误态顶掉。
// failed 可传字符串数组或 [{ label, message }] 数组。
const props = defineProps({
  failed: { type: Array, default: () => [] },
  title: { type: String, default: '部分数据加载失败' },
  retryText: { type: String, default: '重试失败项' },
  maxShow: { type: Number, default: 3 },
})
defineEmits(['retry'])

const normalized = computed(() =>
  props.failed.map((f) => (typeof f === 'string' ? { label: f } : f)).filter((f) => f && f.label)
)
const listText = computed(() => {
  const labels = normalized.value.map((f) => f.label)
  const shown = labels.slice(0, props.maxShow).join('、')
  const rest = labels.length - props.maxShow
  return rest > 0 ? `${shown} 等 ${labels.length} 项` : shown
})
</script>

<style scoped>
.partial-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  border: 1px solid var(--warn-bd);
  background: var(--warn-dim);
  border-radius: var(--r-md);
  font-size: var(--fs-sm);
}
.pb-icon {
  color: var(--warn);
  flex-shrink: 0;
}
.pb-text {
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: baseline;
  gap: 6px;
  flex-wrap: wrap;
}
.pb-title {
  color: var(--t1);
  font-weight: 600;
}
.pb-list {
  color: var(--t2);
  overflow-wrap: break-word;
}
</style>
