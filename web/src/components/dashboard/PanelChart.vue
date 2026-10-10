<template>
  <SectionCard
    class="panel-chart"
    dense
    :title="panel.title || panel.metric"
    :subtitle="subtitle"
  >
    <template #actions>
      <el-button
        v-if="editing"
        size="small"
        text
        bg
        :title="panel.size === 'full' ? '切为半宽' : '切为全宽'"
        @click="$emit('toggle-size')"
      >{{ panel.size === 'full' ? '半宽' : '全宽' }}</el-button>
      <el-button v-if="editing" size="small" text bg @click="$emit('edit')">编辑</el-button>
      <el-button size="small" text bg type="primary" @click="onExport">导出CSV</el-button>
      <!-- 删除面板与删除看板保持同一套确认标准（原先删除面板无二次确认） -->
      <el-popconfirm v-if="editing" title="确认删除该面板？" @confirm="$emit('remove')">
        <template #reference>
          <el-button size="small" text bg type="danger">删除</el-button>
        </template>
      </el-popconfirm>
    </template>

    <div class="chart-wrap">
      <!-- 图表节点常驻 DOM：容器被 v-if 摘掉会让 echarts 初始化拿到 null，
           三态改用覆盖层表达，而不是替换掉 .chart -->
      <div ref="el" class="chart"></div>
      <div v-if="loading" class="panel-state">
        <span class="panel-spin"></span>
        <span>加载中…</span>
      </div>
      <div v-else-if="error" class="panel-state error">
        <span class="panel-err-msg">{{ error }}</span>
        <el-button size="small" @click="load">重试</el-button>
      </div>
      <div v-else-if="empty" class="panel-state empty">
        <span>该时间范围内没有数据</span>
      </div>
    </div>
  </SectionCard>
</template>

<script setup>
import { ref, computed, onMounted, onBeforeUnmount, watch, nextTick } from 'vue'
import { initChart, monitorOption, COLORS } from '../../charts/echarts'
import http from '../../api/http'
import SectionCard from '../common/SectionCard.vue'

const props = defineProps({
  panel: { type: Object, required: true },
  editing: { type: Boolean, default: false },
  // 指标单位（来自指标目录），用于坐标轴/提示框格式化；缺失时回退裸数值
  unit: { type: String, default: '' },
})
const emit = defineEmits(['edit', 'remove', 'toggle-size'])

const el = ref(null)
let chart = null
const error = ref('')
const loading = ref(false)
const empty = ref(false)

// 序列配色轮转：原先所有序列固定 COLORS.cyan，多序列（按 instance 拆分）无法区分。
const PALETTE = ['cyan', 'blue', 'purple', 'amber', 'green', 'red']
function colorAt(i) {
  return COLORS[PALETTE[i % PALETTE.length]] || COLORS.cyan
}

const CHART_LABEL = { line: '折线', area: '面积', bar: '柱状', gauge: '仪表', pie: '饼图' }
const subtitle = computed(() => {
  const t = CHART_LABEL[props.panel.chartType] || '折线'
  const scope = props.panel.node ? ` · ${props.panel.node}` : ''
  return `${t} · ${props.panel.range || '1h'}${scope}`
})

// 时间范围 → 查询窗口。step 必须与窗口匹配：7d 用 60s 会拉回上万点。
// 导出与查询共用同一份边界，避免「7d 导出采样错误」（原导出恒 60000）。
function rangeBounds(r) {
  const now = Date.now()
  switch (r || '1h') {
    case '6h': return { start: now - 6 * 3600000, end: now, step: 60000 }
    case '24h': return { start: now - 24 * 3600000, end: now, step: 300000 }
    case '7d': return { start: now - 7 * 86400000, end: now, step: 1800000 }
    case '1h':
    default: return { start: now - 3600000, end: now, step: 60000 }
  }
}

async function load() {
  if (!chart) return
  error.value = ''
  loading.value = true
  const { start, end, step } = rangeBounds(props.panel.range)
  const params = new URLSearchParams({
    metric: props.panel.metric,
    start,
    end,
    step,
  })
  if (props.panel.node) params.set('node', props.panel.node)
  if (props.panel.instance) params.set('instance', props.panel.instance)
  for (const k in (props.panel.labels || {})) params.set('labels', `${k}=${props.panel.labels[k]}`)
  try {
    const d = await http.get('/api/v1/query/range?' + params.toString())
    const series = d.series || d.data || []
    // 按指标名归类到单序列（面板只选一个指标）；多实例时按 instance 拆成多条曲线
    const data = {}
    ;(series || []).forEach((s) => {
      const label = s.labels && s.labels.instance ? `${props.panel.metric}·${s.labels.instance}` : props.panel.metric
      data[label] = (s.points || []).map((p) => [p.timestamp, p.value])
    })
    const names = Object.keys(data)
    empty.value = !names.some((n) => (data[n] || []).length)
    chart.setOption(
      monitorOption({
        chartType: props.panel.chartType,
        unit: props.unit,
        series: names.map((n, i) => ({ name: n, color: colorAt(i), data: data[n] || [] })),
      }),
      true
    )
  } catch (e) {
    empty.value = false
    error.value = '加载失败：' + (e.message || e)
  } finally {
    loading.value = false
  }
}

function onExport() {
  const { start, end, step } = rangeBounds(props.panel.range)
  http.exportMetricCSV({
    metric: props.panel.metric,
    node: props.panel.node,
    instance: props.panel.instance,
    start,
    end,
    // 复用查询同一份 step，修正 7d 导出采样错误
    step,
    labels: Object.entries(props.panel.labels || {}).map(([k, v]) => `${k}=${v}`).join(','),
    filename: `metric_${props.panel.metric}.csv`,
  }).catch((e) => {
    error.value = '导出失败：' + (e.message || e)
  })
}

onMounted(async () => {
  await nextTick()
  chart = initChart(el.value)
  load()
})

onBeforeUnmount(() => {
  chart && chart.dispose()
})

// 只在这些字段真正变化时重新查询。
// 原实现 watch(props.panel, {deep:true})：编辑任一面板会克隆整块 draft，
// 触发所有面板的 load（N 个面板 N 次全量重查）。
const watchKey = computed(() =>
  [
    props.panel.metric,
    props.panel.range,
    props.panel.node,
    props.panel.instance,
    props.panel.chartType,
    JSON.stringify(props.panel.labels || {}),
  ].join('\u0001')
)
watch(watchKey, () => load())
</script>

<style scoped>
.panel-chart {
  height: 100%;
}
.chart-wrap {
  position: relative;
  height: 220px;
}
.chart {
  width: 100%;
  height: 100%;
}
.panel-state {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  font-size: var(--fs-sm);
  color: var(--t3);
  background: var(--fill-1);
  border-radius: var(--r-md);
}
.panel-state.error {
  color: var(--danger);
}
.panel-err-msg {
  max-width: 70%;
  overflow-wrap: break-word;
}
.panel-spin {
  width: 14px;
  height: 14px;
  border: 2px solid var(--bd);
  border-top-color: var(--accent);
  border-radius: 50%;
  animation: panel-spin 0.7s linear infinite;
}
@keyframes panel-spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
