<template>
  <div class="empty-state" :class="size">
    <div v-if="icon" class="es-icon">
      <img v-if="typeof icon === 'string'" :src="icon" alt="" />
      <el-icon v-else :size="iconSize"><component :is="icon" /></el-icon>
    </div>
    <div v-if="title" class="es-title">{{ title }}</div>
    <div v-if="hints && hints.length" class="es-hints">
      <p v-for="(h, i) in hints" :key="i" class="es-hint">{{ h }}</p>
    </div>
    <div v-if="actionText || secondaryActionText" class="es-actions">
      <el-button v-if="actionText" size="small" @click="$emit('action')">
        {{ actionText }}
      </el-button>
      <el-button
        v-if="secondaryActionText"
        size="small"
        link
        type="primary"
        @click="$emit('secondary-action')"
      >
        {{ secondaryActionText }}
      </el-button>
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue'

// 全站统一空状态。
// 设计要点：空状态不能只写"暂无数据"，必须告诉用户下一步做什么，
// 所以 hints 是排查建议（"检查 X / 前往 Y"），而不是同义反复。
// size：block = 独立成块的空态（默认）；table-inline = 嵌在 el-table #empty 槽里的紧凑版。
const props = defineProps({
  // 图标：字符串 = 图片 URL；组件/渲染函数（如 h('svg', {...})） = 内联图形。
  // 必须放行 Function：各中间件 Tab 都用 `() => h('svg', {...})` 传内联 SVG，
  // 只写 [Object, String] 会在 dev 下对每个 Tab 报一次 prop 类型告警。
  icon: { type: [Object, String, Function], default: null },
  title: { type: String, default: '' },
  hints: { type: Array, default: () => [] },
  actionText: { type: String, default: '' },
  secondaryActionText: { type: String, default: '' },
  size: { type: String, default: 'block' }, // block | table-inline
})
defineEmits(['action', 'secondary-action'])

const iconSize = computed(() => (props.size === 'table-inline' ? 24 : 32))
</script>

<style scoped>
.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 32px 16px;
  text-align: center;
}
/* 嵌在表格空态里：表格本身已有内边距，这里再给 32px 会把表体撑得很高 */
.empty-state.table-inline {
  padding: 20px 16px;
  gap: 4px;
}
.es-icon {
  color: var(--t3);
  margin-bottom: 4px;
  display: flex;
  align-items: center;
  justify-content: center;
}
.es-icon img {
  width: 32px;
  height: 32px;
  object-fit: contain;
  opacity: 0.6;
}
.empty-state.table-inline .es-icon img {
  width: 24px;
  height: 24px;
}
.es-title {
  font-size: var(--fs-base);
  font-weight: 600;
  color: var(--t2);
}
.es-hints {
  display: flex;
  flex-direction: column;
  gap: 2px;
  max-width: 420px;
}
.es-hint {
  font-size: var(--fs-sm);
  color: var(--t3);
  line-height: 1.6;
}
.es-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 8px;
}
</style>
