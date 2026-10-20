<template>
  <div class="ai-inv">
    <div v-if="loading" class="muted">加载中…</div>
    <template v-else-if="inv">
      <div class="inv-state">
        <el-tag size="small" :type="tagType">{{ stateLabel }}</el-tag>
        <span v-if="inv.protocol" class="muted">协议：{{ inv.protocol }}</span>
        <span class="muted">工具调用 {{ (inv.steps || []).length }} 次</span>
      </div>
      <p v-if="inv.answer" class="inv-answer">{{ inv.answer }}</p>
      <p v-if="inv.error" class="inv-error">{{ inv.error }}</p>
      <details v-if="(inv.steps || []).length">
        <summary>调查步骤</summary>
        <div v-for="(s, i) in inv.steps" :key="i" class="inv-step">
          <strong>{{ s.tool }}</strong>
          <span v-if="s.err" class="inv-error">{{ s.err }}</span>
          <pre v-else>{{ s.result }}</pre>
        </div>
      </details>
    </template>
    <div v-else class="muted">暂无 AI 调查（自动调查未开启或该告警未触发调查）</div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import http from '../../api/http'

const props = defineProps({ alertId: { type: String, required: true } })
const inv = ref(null)
const loading = ref(true)

const stateLabel = computed(() => ({
  running: '调查中', done: '已完成', failed: '失败', skipped: '已跳过',
}[inv.value?.state] || inv.value?.state || ''))
const tagType = computed(() => ({
  running: 'warning', done: 'success', failed: 'danger', skipped: 'info',
}[inv.value?.state] || 'info'))

onMounted(async () => {
  try {
    const data = await http.get('/api/v1/ai/investigations?alertId=' + encodeURIComponent(props.alertId))
    inv.value = data.investigation || null
  } catch {
    inv.value = null
  } finally {
    loading.value = false
  }
})
</script>

<style scoped>
.ai-inv { padding: 4px 0; }
.inv-state { display: flex; gap: 12px; align-items: center; margin-bottom: 8px; }
.inv-answer { white-space: pre-wrap; line-height: 1.6; }
.inv-error { color: var(--danger); }
.inv-step { margin: 8px 0; }
.inv-step pre { white-space: pre-wrap; font-size: var(--fs-sm); background: var(--fill-1); padding: 8px; border-radius: 6px; }
.muted { color: var(--t3); }
</style>
