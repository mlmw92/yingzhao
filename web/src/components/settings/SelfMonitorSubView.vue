<template>
  <el-card class="settings-card" shadow="never">
    <template #header>
      <div class="card-head">
        <div>
          <span class="title">系统自监控</span>
          <span class="sub">Server 自身运行时、HTTP、时序库、通知与告警管道的健康快照</span>
        </div>
        <div class="head-actions">
          <el-switch v-model="autoRefresh" size="small" active-text="自动刷新" />
          <el-button size="small" :loading="loading" @click="load">刷新</el-button>
        </div>
      </div>
    </template>

    <p class="lead-hint">
      自监控指标以 <code v-pre>self_*</code> 前缀写入时序库，可直接用「指标浏览」查询、
      用告警规则监控 Server 自身；探针 <code v-pre>/healthz</code>、<code v-pre>/readyz</code> 免登录。
    </p>

    <!-- 健康探针：存活与就绪是两个独立信号，各自一张小卡 -->
    <div class="probe-grid">
      <div class="probe-card">
        <div class="pc-head">
          <span class="pc-label">存活 /healthz</span>
          <el-tag :type="health.ok ? 'success' : 'danger'" size="small" effect="dark">
            {{ health.ok ? '正常' : '异常' }}
          </el-tag>
        </div>
        <div class="pc-body">版本 {{ health.body.version || '—' }} · 运行 {{ uptimeText }}</div>
      </div>
      <div class="probe-card">
        <div class="pc-head">
          <span class="pc-label">就绪 /readyz</span>
          <el-tag :type="ready.ok ? 'success' : 'danger'" size="small" effect="dark">
            {{ ready.ok ? '就绪' : '降级' }}
          </el-tag>
        </div>
        <div class="pc-body">
          <template v-if="Object.keys(ready.body.checks || {}).length">
            <span v-for="(c, name) in ready.body.checks" :key="name" class="pc-check">
              <el-tag :type="c.ok ? 'info' : 'danger'" size="small" effect="plain">{{ name }}</el-tag>
              <span v-if="!c.ok" class="bad-detail">{{ c.detail }}</span>
            </span>
          </template>
          <span v-else class="muted">无子检查项</span>
        </div>
      </div>
    </div>

    <!-- 探针失败与快照失败是两件独立的事：拆成两条，避免一条挂了另一条被掩盖 -->
    <el-alert
      v-if="probeError"
      :title="probeError"
      type="warning"
      :closable="false"
      show-icon
      class="err"
    />
    <el-alert
      v-if="snapshotError"
      :title="snapshotError"
      type="warning"
      :closable="false"
      show-icon
      class="err"
    />

    <!-- 关键指标：按 运行时 / HTTP / TSDB / 通知 / 告警 / 节点 分组成卡片网格。
         此前每组一张整宽 el-descriptions，只有 1-2 项的组会拖出一整行空白，页面又长又空。 -->
    <div class="metric-grid">
      <div v-for="g in metricGroups" :key="g.title" class="metric-card">
        <div class="mc-title">{{ g.title }}</div>
        <div class="mc-rows">
          <div v-for="it in g.items" :key="it.label" class="mc-row">
            <span class="mc-label">{{ it.label }}</span>
            <span class="mc-val" :class="{ bad: it.bad }">{{ it.value }}</span>
          </div>
        </div>
      </div>
    </div>

    <!-- 按渠道通知 -->
    <div v-if="channels.length" class="channels">
      <h3 class="section-title">按渠道通知</h3>
      <el-table :data="channels" size="small" stripe>
        <el-table-column prop="channel" label="渠道" min-width="120" />
        <el-table-column prop="sent" label="成功" width="100" />
        <el-table-column label="失败" width="100">
          <template #default="{ row }">
            <span :class="{ bad: row.failed > 0 }">{{ row.failed }}</span>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <!-- 趋势 -->
    <div class="trend">
      <h3 class="section-title">
        趋势（近 1 小时）
        <span class="field-hint inline">
          数据来自时序库 <code v-pre>self_goroutines</code> / <code v-pre>self_heap_alloc_bytes</code>
        </span>
      </h3>
      <!-- 此前 nodeLabel 为空时 renderTrend 直接 return，260px 的图表区永久空白，
           既没有空态也没有任何说明（权限不足、时序库不可达时尤其明显）。 -->
      <el-alert v-if="trendError" :title="trendError" type="warning" :closable="false" show-icon class="err" />
      <div v-if="nodeLabel" ref="chartRef" class="chart" />
      <div v-else class="chart chart-empty">未获取到本节点的标识，无法查询趋势数据</div>
    </div>
  </el-card>
</template>

<script setup>
import { ref, computed, onMounted, onActivated, onDeactivated, onUnmounted, nextTick } from 'vue'
import http from '../../api/http'
import { initChart, tokenColor } from '../../charts/echarts'
const AXIS_COLOR = tokenColor('t2', '#9fb3c8')

const loading = ref(false)
const probeError = ref('')
const snapshotError = ref('')
const trendError = ref('')
const autoRefresh = ref(true)
const m = ref({})
const nodeLabel = ref('')
const health = ref({ ok: false, body: {} })
const ready = ref({ ok: false, body: {} })
const chartRef = ref(null)
let chartInstance = null
let snapshotTimer = null
let trendTimer = null
let activated = true

const channels = computed(() => {
  const by = m.value.notify?.byChannel || {}
  return Object.entries(by).map(([channel, c]) => ({ channel, sent: c.sent, failed: c.failed }))
})

const uptimeText = computed(() => formatDuration(health.value.body.uptimeSeconds ?? m.value.uptimeSeconds))
const evalAgeText = computed(() => {
  const age = m.value.alert?.evalAgeSeconds
  if (age === undefined || age === null) return '—'
  if (age < 0) return '尚未评估'
  return age + ' 秒前'
})
// 评估周期 3 倍仍未评估即视为停摆（与 /readyz 同口径）
const evalStale = computed(() => {
  const age = m.value.alert?.evalAgeSeconds
  const iv = m.value.alert?.evalIntervalSeconds || 0
  if (!iv || age === undefined || age === null) return false
  return age < 0 || age > iv * 3 + 5
})

// 16 项指标按 运行时 / HTTP / TSDB / 通知 / 告警 / 节点 分组——
// 平铺 4 列时 3 个"坏值"分散在不同行，扫读时很难一眼定位问题域。
const metricGroups = computed(() => [
  {
    title: '运行时',
    items: [
      { label: 'goroutine', value: m.value.goroutines ?? '—' },
      { label: '堆内存', value: mb(m.value.heapAllocBytes) },
      { label: 'GC 次数', value: m.value.gcCount ?? '—' },
      { label: 'WebSocket 连接', value: m.value.wsConnections ?? '—' },
    ],
  },
  {
    title: 'HTTP',
    items: [
      { label: '请求', value: m.value.http?.requests ?? '—' },
      { label: '错误', value: m.value.http?.errors ?? '—', bad: (m.value.http?.errors || 0) > 0 },
    ],
  },
  {
    title: 'TSDB',
    items: [
      { label: '写入', value: m.value.tsdb?.writeTotal ?? '—' },
      { label: '写入失败', value: m.value.tsdb?.writeErrors ?? '—', bad: (m.value.tsdb?.writeErrors || 0) > 0 },
      { label: '查询', value: m.value.tsdb?.queryTotal ?? '—' },
      { label: '查询失败', value: m.value.tsdb?.queryErrors ?? '—', bad: (m.value.tsdb?.queryErrors || 0) > 0 },
    ],
  },
  {
    title: '通知',
    items: [
      { label: '成功', value: m.value.notify?.sent ?? '—' },
      { label: '失败', value: m.value.notify?.failed ?? '—', bad: (m.value.notify?.failed || 0) > 0 },
    ],
  },
  {
    title: '告警',
    items: [
      { label: '评估轮次', value: m.value.alert?.evalTotal ?? '—' },
      { label: '评估节拍', value: evalAgeText.value, bad: evalStale.value },
      { label: '活跃告警', value: `${m.value.alert?.firing ?? '—'}（抑制 ${m.value.alert?.suppressed ?? 0}）` },
    ],
  },
  {
    title: '节点',
    items: [{ label: '在线', value: `${m.value.nodes?.online ?? '—'} / ${m.value.nodes?.total ?? '—'}` }],
  },
])

function formatDuration(sec) {
  if (sec === undefined || sec === null) return '—'
  const s = Number(sec)
  if (s < 60) return s + ' 秒'
  if (s < 3600) return Math.floor(s / 60) + ' 分钟'
  const h = Math.floor(s / 3600)
  const mm = Math.floor((s % 3600) / 60)
  return mm ? `${h} 小时 ${mm} 分` : `${h} 小时`
}

function mb(bytes) {
  if (bytes === undefined || bytes === null) return '—'
  const v = Number(bytes)
  if (v < 1024 * 1024) return (v / 1024).toFixed(1) + ' KB'
  return (v / 1024 / 1024).toFixed(1) + ' MB'
}

async function load() {
  loading.value = true
  probeError.value = ''
  snapshotError.value = ''
  try {
    const probe = await http.probeHealth()
    health.value = probe.health
    ready.value = probe.ready
  } catch (e) {
    probeError.value = '探针请求失败：' + (e.message || '请稍后重试')
  }
  try {
    const status = await http.getSelfStatus()
    m.value = status.metrics || {}
    nodeLabel.value = status.nodeLabel || status.metrics?.node || ''
  } catch (e) {
    // 缺 dashboard:read 时只看到探针部分，仍应给出明确原因而不是空白页
    snapshotError.value = '读取自监控快照失败：' + (e.message || '请稍后重试')
  }
  loading.value = false
  await nextTick()
  renderTrend()
}

async function renderTrend() {
  if (!chartRef.value || !nodeLabel.value) return
  const end = Date.now()
  const start = end - 3600 * 1000
  // step 必须 ≥ 1000（毫秒）。后端 /api/v1/query/range 对 step<1000 直接 400
  // —— 此前这里写 step=30，于是趋势查询恒定失败、图表永久空白。
  const query = (metric) =>
    http.get(`/api/v1/query/range?node=${encodeURIComponent(nodeLabel.value)}&metric=${metric}&start=${start}&end=${end}&step=60000`)
  try {
    const [goroutines, heap] = await Promise.all([query('self_goroutines'), query('self_heap_alloc_bytes')])
    const series = []
    for (const s of goroutines.series || []) {
      series.push({ name: 'goroutine', type: 'line', smooth: true, showSymbol: false, data: s.points.map((p) => [p.timestamp, p.value]) })
    }
    for (const s of heap.series || []) {
      series.push({
        name: '堆内存 (MB)', type: 'line', yAxisIndex: 1, smooth: true, showSymbol: false,
        data: s.points.map((p) => [p.timestamp, +(p.value / 1024 / 1024).toFixed(1)]),
      })
    }
    // 增量更新：实例只建一次，后续 setOption 合并数据。
    // 原实现每轮 dispose()+initChart() 全量重建，10s 一次会持续抖动、丢动画。
    if (!chartInstance) chartInstance = initChart(chartRef.value)
    chartInstance.setOption({
      tooltip: { trigger: 'axis' },
      legend: { data: series.map((s) => s.name), textStyle: { color: AXIS_COLOR } },
      grid: { left: 55, right: 60, top: 40, bottom: 30 },
      xAxis: { type: 'time' },
      yAxis: [
        { type: 'value', name: 'goroutine' },
        { type: 'value', name: 'MB' },
      ],
      series,
    })
    trendError.value = ''
  } catch (e) {
    // 趋势失败不再静默：至少让用户知道是查询挂了，而不是图表"本来就这样"
    trendError.value = '趋势查询失败：' + (e.message || '请稍后重试')
  }
}

// —— 轮询：快照 10s / 图表 60s，且页面隐藏或 keep-alive 切走时暂停 ——
function tickSnapshot() {
  if (autoRefresh.value && !document.hidden) load()
}
function tickTrend() {
  if (autoRefresh.value && !document.hidden) renderTrend()
}
function startTimers() {
  if (snapshotTimer || trendTimer) return
  snapshotTimer = setInterval(tickSnapshot, 10000)
  trendTimer = setInterval(tickTrend, 60000)
}
function stopTimers() {
  if (snapshotTimer) { clearInterval(snapshotTimer); snapshotTimer = null }
  if (trendTimer) { clearInterval(trendTimer); trendTimer = null }
}
function onVisibility() {
  if (document.hidden) stopTimers()
  else if (activated) startTimers()
}

onMounted(() => {
  document.addEventListener('visibilitychange', onVisibility)
  load()
  startTimers()
})

// keep-alive 切回来：重新计时并让图表按当前尺寸重排
onActivated(() => {
  activated = true
  startTimers()
  nextTick(() => chartInstance && chartInstance.resize())
})

onDeactivated(() => {
  activated = false
  stopTimers()
})

onUnmounted(() => {
  document.removeEventListener('visibilitychange', onVisibility)
  stopTimers()
  if (chartInstance) { chartInstance.dispose(); chartInstance = null }
})
</script>

<style scoped>
.settings-card {
  border-radius: var(--r-lg);
}
.card-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--sp-3);
  flex-wrap: wrap;
}
.card-head .title {
  font-size: var(--fs-base);
  font-weight: 600;
  margin-right: 10px;
}
.card-head .sub {
  font-size: var(--fs-sm);
  color: var(--t3);
}
.head-actions {
  display: flex;
  align-items: center;
  gap: 12px;
}
.lead-hint {
  margin: 0 0 var(--sp-3);
  font-size: var(--fs-sm);
  color: var(--t3);
  line-height: 1.6;
}
/* 探针：存活与就绪各一张小卡，横向并排、窄屏折行 */
.probe-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: var(--sp-3);
  margin-bottom: var(--sp-3);
}
.probe-card {
  border: 1px solid var(--bd);
  border-radius: var(--r-md);
  background: var(--fill-1);
  padding: 10px 12px;
}
.pc-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--sp-2);
  margin-bottom: 6px;
}
.pc-label {
  font-size: var(--fs-sm);
  font-weight: 600;
}
.pc-body {
  font-size: var(--fs-xs);
  color: var(--t3);
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  line-height: 1.7;
}
.pc-check {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}
.field-hint {
  font-size: var(--fs-xs);
  color: var(--t2);
}
.field-hint.inline {
  margin-left: 4px;
  font-weight: 400;
}
.bad-detail {
  margin-left: 6px;
  font-size: var(--fs-xs);
  color: var(--danger);
}
.bad {
  color: var(--danger);
  font-weight: 600;
}
.err {
  margin-bottom: var(--sp-2);
}
/* 指标卡网格：6 组自适应排布，窄屏折到 2 列 / 1 列 */
.metric-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: var(--sp-3);
}
.metric-card {
  border: 1px solid var(--bd);
  border-radius: var(--r-md);
  background: var(--fill-1);
  padding: 10px 12px;
}
.mc-title {
  font-size: var(--fs-xs);
  font-weight: 600;
  color: var(--t2);
  margin-bottom: 8px;
  letter-spacing: 0.02em;
}
.mc-rows {
  display: flex;
  flex-direction: column;
  gap: 5px;
}
.mc-row {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: var(--sp-2);
  font-size: var(--fs-sm);
}
.mc-label {
  color: var(--t3);
  flex-shrink: 0;
}
.mc-val {
  color: var(--t1);
  font-variant-numeric: tabular-nums;
  text-align: right;
  word-break: break-all;
}
.section-title {
  font-size: var(--fs-sm);
  font-weight: 600;
  margin: 0 0 8px;
}
.channels {
  margin-top: var(--sp-4);
}
.chart {
  width: 100%;
  height: 260px;
}
/* 与 .chart 同高：空态不该让卡片塌下去，也不该变成一整块空白 */
.chart-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  border: 1px dashed var(--bd);
  border-radius: var(--r-md);
  color: var(--t3);
  font-size: var(--fs-sm);
}
.trend {
  margin-top: var(--sp-4);
}
code {
  font-family: var(--mono);
  font-size: var(--fs-xs);
}
@media (max-width: 1024px) {
  .probe-grid {
    grid-template-columns: 1fr;
  }
}
</style>
