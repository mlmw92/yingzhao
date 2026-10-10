<template>
  <div v-if="chips.length" class="filter-chips">
    <span class="fc-prefix">筛选</span>
    <span v-for="c in chips" :key="c.key" class="fc-chip">
      <span class="fc-text">
        {{ c.label }}<template v-if="hasValue(c.value)">：{{ c.value }}</template>
      </span>
      <el-icon class="fc-x" role="button" tabindex="0" @click="remove(c.key)" @keydown.enter="remove(c.key)">
        <Close />
      </el-icon>
    </span>
    <el-button v-if="showClear" link size="small" class="fc-clear" @click="$emit('clear')">
      {{ clearText }}
    </el-button>
  </div>
</template>

<script setup>
import { Close } from '@element-plus/icons-vue'

// 筛选条件回显条：把"当前生效但界面上看不到"的条件显式列出来，逐条可撤销 + 一键清空。
// 解决三处同需求：LogsView 只有部分条件可见、AuditView 完全没有回显、AssetListView 下钻态有盲区。
// chips 形如 [{ key, label, value }]：label 是维度名（"节点"/"关键词"），value 是当前值。
defineProps({
  chips: { type: Array, default: () => [] },
  clearText: { type: String, default: '清空筛选' },
  showClear: { type: Boolean, default: true },
})
const emit = defineEmits(['remove', 'clear'])

function hasValue(v) {
  return v !== undefined && v !== null && v !== ''
}
function remove(key) {
  emit('remove', key)
}
</script>

<style scoped>
.filter-chips {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
}
.fc-prefix {
  font-size: var(--fs-xs);
  color: var(--t3);
}
.fc-chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px 4px 2px 8px;
  border-radius: var(--r-pill);
  background: var(--fill-2);
  border: 1px solid var(--bd);
  font-size: var(--fs-xs);
  color: var(--t2);
  line-height: 1.6;
  max-width: 260px;
}
.fc-text {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.fc-x {
  cursor: pointer;
  color: var(--t3);
  border-radius: 50%;
  padding: 1px;
  flex-shrink: 0;
  transition: color var(--dur-1) var(--ease), background var(--dur-1) var(--ease);
}
.fc-x:hover {
  color: var(--t1);
  background: var(--fill-3);
}
.fc-clear {
  margin-left: 2px;
}
</style>
