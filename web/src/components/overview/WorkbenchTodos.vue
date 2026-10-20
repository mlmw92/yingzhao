<template>
  <div class="wb-todos">
    <div v-for="t in todos" :key="t.kind" class="wb-row">
      <span class="wb-label">{{ labelOf(t.kind) }}</span>
      <span v-if="!t.available" class="wb-muted">无权限</span>
      <span v-else-if="t.count === 0" class="wb-muted">无</span>
      <router-link v-else :to="linkOf(t.kind)" class="wb-count">{{ t.count }} 条 →</router-link>
    </div>
    <div v-if="allClear" class="wb-empty">当前没有需要你处理的事项</div>
    <div v-if="errors.length" class="wb-errors">
      <span v-for="e in errors" :key="e.kind">{{ e.message }}</span>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import http from '../../api/http'

const todos = ref([])
const errors = ref([])
const loaded = ref(false)

// 空态只在「所有段都可用且都为 0」时出现：有段不可用或有错误时，
// 说「没有待办」是在撒谎
const allClear = computed(
  () => loaded.value && errors.value.length === 0 &&
    todos.value.every((t) => !t.available || t.count === 0),
)

function labelOf(kind) {
  return kind === 'alert' ? '待确认告警' : '待我审批'
}
function linkOf(kind) {
  return kind === 'alert' ? '/alerts' : '/approvals'
}

onMounted(async () => {
  try {
    const data = await http.get('/api/v1/workbench/summary')
    todos.value = data.todos || []
    errors.value = data.errors || []
  } catch (e) {
    errors.value = [{ kind: 'summary', message: '待办加载失败' }]
  } finally {
    loaded.value = true
  }
})
</script>

<style scoped>
.wb-row { display: flex; align-items: center; gap: 12px; padding: 6px 0; }
.wb-label { color: var(--t2); }
.wb-count { color: var(--accent); font-weight: 600; }
.wb-muted { color: var(--t3); }
.wb-empty { color: var(--t3); padding: 6px 0; }
.wb-errors { color: var(--danger); font-size: var(--fs-sm); }
</style>
