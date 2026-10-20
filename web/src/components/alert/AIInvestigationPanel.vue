<template>
  <div class="ai-inv">
    <div v-if="loading" class="muted">加载中…</div>
    <template v-else-if="inv">
      <div class="inv-state">
        <el-tag size="small" :type="tagType">{{ stateLabel }}</el-tag>
        <span v-if="inv.protocol" class="muted">协议：{{ inv.protocol }}</span>
        <span class="muted">工具调用 {{ (inv.steps || []).length }} 次</span>
        <span v-if="inv.rounds" class="muted">{{ inv.rounds }} 轮</span>
        <span v-if="inv.tokens" class="muted">{{ formatInt(inv.tokens) }} tokens</span>
        <span v-if="elapsed" class="muted">耗时 {{ elapsed }}</span>
      </div>
      <!-- 停止原因只在非正常结束时出现：正常结束写一行「停止原因：正常结束」纯属噪音 -->
      <p v-if="stopLabel" class="inv-stop">停止原因：{{ stopLabel }}</p>
      <!-- 重跑入口只对已结束的调查出现：运行中再点一次只会被服务端拒绝 -->
      <div v-if="canRetry" class="inv-actions">
        <el-button size="small" :loading="retrying" @click="retry">重新调查</el-button>
      </div>
      <p v-if="inv.answer" class="inv-answer">{{ inv.answer }}</p>
      <p v-if="inv.error" class="inv-error">{{ inv.error }}</p>

      <div v-if="(inv.suggestions || []).length" class="inv-suggestions">
        <div class="sug-title">建议处置</div>
        <p class="sug-hint">AI 只给建议，不会自动执行；提交后需另一位同事批准</p>
        <div v-for="s in inv.suggestions" :key="s.id" class="sug-item">
          <div class="sug-head">
            <strong>{{ s.title }}</strong>
            <span class="muted">@ {{ s.node }}</span>
          </div>
          <div v-if="s.reason" class="sug-reason">{{ s.reason }}</div>
          <div class="sug-params">{{ paramsText(s) }}</div>
          <div class="sug-actions">
            <el-button
              size="small"
              type="primary"
              :loading="submittingId === s.id"
              :disabled="!!submitted[submissionKey(s)]"
              @click="submitSuggestion(s)"
            >
              {{ submitted[submissionKey(s)] ? '已提交审批' : '提交审批' }}
            </el-button>
            <span v-if="submitted[submissionKey(s)]" class="muted">审批单 {{ submitted[submissionKey(s)] }}</span>
          </div>
        </div>
      </div>

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
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { ElMessage } from 'element-plus'
import http from '../../api/http'

const props = defineProps({ alertId: { type: String, required: true } })
const inv = ref(null)
const loading = ref(true)

// stopped 的取值来自服务端 ai.Outcome.Stopped；done 表示模型自己给出了结论。
const STOP_LABELS = {
  done: '正常结束',
  tools: '工具调用次数用尽',
  tokens: 'token 预算用尽',
  rounds: '对话轮次用尽',
  timeout: '超时',
  model_error: '模型调用失败',
}

// state=done 只代表「这次运行没有报错」，不代表调查得出了结论：
// 预算/超时收尾时也是 done，但 answer 里只有一句「调查未完成」。
const incomplete = computed(() => {
  const v = inv.value
  return !!v && v.state === 'done' && !!v.stopped && v.stopped !== 'done'
})

const stateLabel = computed(() => {
  if (incomplete.value) return '未完成'
  return {
    running: '调查中', done: '已完成', failed: '失败', skipped: '已跳过',
  }[inv.value?.state] || inv.value?.state || ''
})
const tagType = computed(() => {
  if (incomplete.value) return 'warning'
  return {
    running: 'warning', done: 'success', failed: 'danger', skipped: 'info',
  }[inv.value?.state] || 'info'
})
const stopLabel = computed(() => {
  const s = inv.value?.stopped
  if (!s || s === 'done') return ''
  return STOP_LABELS[s] || s
})

// 千分位手写而不用 toLocaleString：不同运行环境的 locale 数据不一致，
// 界面上的数字不该随浏览器语言变化。
function formatInt(n) {
  return String(n).replace(/\B(?=(\d{3})+(?!\d))/g, ',')
}

const elapsed = computed(() => {
  const v = inv.value
  if (!v || !v.startedAt || !v.endedAt) return ''
  const ms = v.endedAt - v.startedAt
  if (ms < 0) return ''
  return (ms / 1000).toFixed(1) + ' 秒'
})

const canRetry = computed(() => !!inv.value && inv.value.state !== 'running')
const retrying = ref(false)

// 建议提交：key → 审批单号。提交成功后按钮变成「已提交审批」，避免重复提交。
//
// key 必须带调查 ID：建议编号每次调查都从 sug-1 重新开始，
// 只用 s.id 会让重跑后的新建议继承上一轮的单号（显示成已提交、点不动）。
const submittingId = ref('')
const submitted = ref({})

function submissionKey(s) {
  return `${inv.value?.id || ''}:${s.id}`
}

function paramsText(s) {
  return Object.entries(s.params || {}).map(([k, v]) => `${k} = ${v}`).join(' · ')
}

async function submitSuggestion(s) {
  submittingId.value = s.id
  try {
    const res = await http.submitAISuggestion(inv.value.id, s.id)
    submitted.value = { ...submitted.value, [submissionKey(s)]: res.approval?.id || '已提交' }
    ElMessage.success('已提交审批，需另一位同事批准')
  } catch (e) {
    ElMessage.error(e.message || '提交失败')
  } finally {
    submittingId.value = ''
  }
}

// 调查在服务端是后台任务，结果不会主动推送：运行中只能轮询。
// 上限 90 次 × 2 秒 = 3 分钟，覆盖单次调查 120 秒的时限。
const POLL_INTERVAL = 2000
const POLL_MAX = 90
let pollTimer = null
let pollCount = 0

function stopPolling() {
  if (pollTimer) {
    clearTimeout(pollTimer)
    pollTimer = null
  }
}

function schedulePoll() {
  if (pollTimer || pollCount >= POLL_MAX) return
  pollTimer = setTimeout(async () => {
    pollTimer = null
    pollCount++
    await load(true)
  }, POLL_INTERVAL)
}

async function load(silent = false) {
  if (!silent) loading.value = true
  try {
    const data = await http.get('/api/v1/ai/investigations?alertId=' + encodeURIComponent(props.alertId))
    inv.value = data.investigation || null
  } catch {
    // 轮询失败保留上一次的结果：一次网络抖动不该把已经看到的结论清空
    if (!silent) inv.value = null
  } finally {
    if (!silent) loading.value = false
  }
  if (inv.value?.state === 'running') schedulePoll()
  else stopPolling()
}

async function retry() {
  retrying.value = true
  stopPolling()
  pollCount = 0
  try {
    await http.retryAIInvestigation(props.alertId)
    // 服务端已同步落下 running 记录，这一步就能读到「调查中」，随后由轮询接手
    await load(true)
  } catch (e) {
    ElMessage.error(e.message || '重新调查失败')
  } finally {
    retrying.value = false
  }
}

onMounted(load)
onUnmounted(stopPolling)
</script>

<style scoped>
.ai-inv { padding: 4px 0; }
.inv-state { display: flex; gap: 12px; align-items: center; margin-bottom: 8px; flex-wrap: wrap; }
.inv-stop { color: var(--warning, #b26a00); font-size: var(--fs-sm); margin: 0 0 8px; }
.inv-actions { margin: 0 0 10px; }
.inv-suggestions { margin: 12px 0; padding: 12px; border: 1px solid var(--bd); border-radius: var(--r-md); background: var(--fill-1); }
.sug-title { font-weight: 600; margin-bottom: 4px; }
.sug-hint { font-size: var(--fs-sm); color: var(--t3); margin: 0 0 10px; }
.sug-item { padding: 8px 0; border-top: 1px solid var(--bd); }
.sug-head { display: flex; gap: 8px; align-items: baseline; }
.sug-reason { font-size: var(--fs-sm); color: var(--t2); margin-top: 4px; }
.sug-params { font-family: var(--mono); font-size: var(--fs-sm); color: var(--t3); margin-top: 4px; }
.sug-actions { display: flex; gap: 10px; align-items: center; margin-top: 8px; }
.inv-answer { white-space: pre-wrap; line-height: 1.6; }
.inv-error { color: var(--danger); }
.inv-step { margin: 8px 0; }
.inv-step pre { white-space: pre-wrap; font-size: var(--fs-sm); background: var(--fill-1); padding: 8px; border-radius: 6px; }
.muted { color: var(--t3); }
</style>
