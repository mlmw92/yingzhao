<template>
  <div class="mw-tab">
    <RefreshBar :loading="loading" @refresh="load" />

    <el-alert v-if="error" type="error" :closable="false" show-icon class="mw-err">
      <template #title>
        <span class="mw-err-text">{{ error }}</span>
        <el-button size="small" link type="primary" @click="load">重试</el-button>
      </template>
    </el-alert>

    <EmptyState
      v-if="!loading && !error && instances.length === 0"
      :icon="EmptyIcon"
      :title="spec.emptyTitle || `尚未采集到 ${spec.label} 实例`"
      :hints="spec.emptyHints || [
        `在运行 Agent 的节点上配置 ${spec.label} 地址（agent.yaml 对应实例段），并确认服务可达`,
        '配置完成后约 15-30 秒数据将出现在此页面',
      ]"
    />

    <template v-if="instances.length > 0">
      <!-- KPI 卡片行 -->
      <div class="kpi-row">
        <KpiCard label="实例总数" :value="stats.total" tone="total">
          <template #icon><el-icon :size="20"><Grid /></el-icon></template>
        </KpiCard>
        <KpiCard label="在线实例" :value="stats.up" tone="up">
          <template #icon><el-icon :size="20"><CircleCheck /></el-icon></template>
        </KpiCard>
        <KpiCard label="离线实例" :value="stats.down" :tone="stats.down > 0 ? 'down' : 'total'">
          <template #icon><el-icon :size="20"><CircleClose /></el-icon></template>
        </KpiCard>
        <KpiCard v-for="k in spec.kpis" :key="k.metric + k.agg" :label="k.label" :value="formatKpi(kpiValue(k), k.format)" :tone="k.tone || 'ops'">
          <template #icon><el-icon :size="20"><DataLine /></el-icon></template>
        </KpiCard>
      </div>

      <!-- 实例拓扑（按 group 聚合；未分组的归入「独立实例」） -->
      <SectionCard v-if="spec.showTopology && topoGroups.length" title="实例拓扑" class="mw-block">
        <div v-for="g in topoGroups" :key="g.name" class="topo-group">
          <div class="topo-group-header">
            <span class="topo-group-title"><strong>{{ g.name }}</strong></span>
            <span class="dim">共 {{ g.list.length }} 个</span>
          </div>
          <div class="topo-grid">
            <div
              v-for="i in g.list"
              :key="i.instance"
              class="rel-node"
              :class="[roleClass(i), { 'is-down': !i.up }]"
              @click="openDetail(i)"
            >
              <div class="rel-node-name" :title="i.instance">{{ i.name || i.instance }}</div>
              <div class="rel-node-meta">
                <MwStatusDot :status="i.up ? 'normal' : 'abnormal'" :label="i.up ? '在线' : '离线'" />
                <span class="dim">·</span>
                <span class="mono">{{ displayAddr(i) }}</span>
              </div>
              <div v-if="i.role" class="rel-node-meta"><span>角色：{{ i.role }}</span></div>
            </div>
          </div>
        </div>
      </SectionCard>

      <!-- 实例列表 -->
      <SectionCard title="实例列表" dense>
        <el-table :data="pagedInstances" size="small" stripe @row-click="openDetail" :row-class-name="rowClass">
          <el-table-column label="实例" min-width="170" show-overflow-tooltip>
            <template #default="{ row }">
              <MwStatusDot :status="row.up ? 'normal' : 'abnormal'" dot-only class="cell-dot" />
              <span class="mono">{{ displayAddr(row) }}</span>
            </template>
          </el-table-column>
          <el-table-column label="状态" min-width="90">
            <template #default="{ row }">
              <MwStatusDot :status="row.up ? 'normal' : 'abnormal'" :label="row.up ? '在线' : '离线'" />
            </template>
          </el-table-column>
          <el-table-column prop="name" label="名称" min-width="110" show-overflow-tooltip />
          <el-table-column v-for="m in metaColumns" :key="m" :label="metaLabel(m)" min-width="90" show-overflow-tooltip>
            <template #default="{ row }">
              <MwRoleTag v-if="m === 'role'" :role="row.role" />
              <span v-else>{{ row[m] || '-' }}</span>
            </template>
          </el-table-column>
          <el-table-column prop="node" label="节点" min-width="140" show-overflow-tooltip />
          <el-table-column
            v-for="col in spec.columns"
            :key="col.key"
            :label="col.label"
            min-width="110"
            sortable
            :sort-method="(a, b) => sortByMetric(a, b, col.key)"
          >
            <template #default="{ row }">{{ metricValue(row, col.key, col.format) }}</template>
          </el-table-column>
          <!-- 类型专属列（如某中间件特有的状态标签/操作按钮）从这里扩展 -->
          <slot name="extra-columns" />
        </el-table>
        <div class="pager">
          <el-pagination background layout="total, sizes, prev, pager, next, jumper" :total="instances.length" :page-size="pageSize" :current-page="currentPage" :page-sizes="[10, 20, 50, 100]" @current-change="v => currentPage = v" @size-change="v => { pageSize = v; currentPage = 1 }" />
        </div>
      </SectionCard>
    </template>

    <!-- 详情抽屉 -->
    <el-drawer v-model="drawer" :title="`实例 ${selected?.instance || ''}`" size="45%" :destroy-on-close="true">
      <div v-if="selected" class="detail-content">
        <MwDetailHeader
          :title="selected.name || selected.instance"
          :subtitle="selected.instance"
          :status="selected.up ? 'normal' : 'abnormal'"
          :status-label="selected.up ? '在线' : '离线'"
          :fields="detailFields"
          v-model:range="range"
        />
        <div class="metric-grid">
          <div v-for="m in selected.metrics" :key="m.key" class="metric-cell" :class="{ 'metric-bad': isBad(m) }">
            <div class="mc-label">{{ m.label }}</div>
            <div class="mc-value">{{ m.value }}<small v-if="m.unit"> {{ m.unit }}</small></div>
          </div>
          <div v-if="selected.uptime" class="metric-cell">
            <div class="mc-label">运行时长</div>
            <div class="mc-value">{{ formatUptime(selected.uptime) }}</div>
          </div>
        </div>
        <!-- 类型专属详情区（如专属图表/子表）从这里扩展 -->
        <slot name="detail-extra" :instance="selected" />
        <div v-for="(t, i) in trendData" :key="t.name + '-' + i" class="trend-block">
          <div class="trend-title">{{ t.name }}（{{ rangeLabel }}）</div>
          <TrendChart
            :option="t.option"
            :loading="t.loading"
            :error="t.error"
            :height="180"
            @retry="loadTrends"
          />
        </div>
      </div>
    </el-drawer>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted, h } from 'vue'
import { Grid, CircleCheck, CircleClose, DataLine } from '@element-plus/icons-vue'
import http from '../../api/http'
import RefreshBar from '../RefreshBar.vue'
import KpiCard from '../KpiCard.vue'
import SectionCard from '../common/SectionCard.vue'
import EmptyState from '../common/EmptyState.vue'
import MwStatusDot from './MwStatusDot.vue'
import MwRoleTag from './MwRoleTag.vue'
import MwDetailHeader from './MwDetailHeader.vue'
import TrendChart from './TrendChart.vue'
import { mwTrendOption } from '../../charts/echarts'
import { rangeToWindow, rangeToStep, MW_RANGE_OPTIONS } from './mwRange'
import './mw.css'

// spec 结构：
//   label        类型展示名（空状态文案用）
//   emptyTitle/emptyHints  空状态文案（可选，缺省按 label 生成）
//   kpis         跨实例聚合的 KPI 卡：[{ label, metric, agg: sum|max|avg, format?, tone? }]
//   metaColumns  实例元信息列（role / group），渲染在「名称」之后
//   columns      实例表指标列：[{ key, label, format? }]（key 为实例 metrics 里的指标名）
//   trends       详情抽屉趋势图：[{ name, unit?, metric }] 或 [{ name, unit?, metrics: [{ name, metric, scale? }] }]
const props = defineProps({
  spec: { type: Object, required: true },
})

const metaColumns = computed(() => props.spec.metaColumns || [])
const META_LABELS = { role: '角色', group: '分组', version: '版本', topology: '拓扑', database: '数据库' }
function metaLabel(key) {
  return META_LABELS[key] || key
}

// 实例地址的展示口径：默认展示 instance；Nginx 这类用 nodeIp 更可读的
// 类型在 spec 里声明 instanceDisplay: 'nodeIp'。
function displayAddr(i) {
  if (!i) return '-'
  return props.spec.instanceDisplay === 'nodeIp' ? i.nodeIp || i.instance || '-' : i.instance || '-'
}

// 拓扑按 group 聚合，未分组归入「独立实例」。
const topoGroups = computed(() => {
  const map = new Map()
  for (const i of instances.value) {
    const g = String(i.group || '').trim() || '独立实例'
    if (!map.has(g)) map.set(g, [])
    map.get(g).push(i)
  }
  return [...map.entries()].map(([name, list]) => ({ name, list }))
})

// 超过阈值的指标在详情里标红（如 RocketMQ 的消息积压）
function isBad(m) {
  return (props.spec.badMetrics || []).includes(m.key) && m.value > 0
}

// 拓扑节点按角色着色：主库/从库一眼可分。角色文案各中间件不同
// （master/primary/leader vs replica/standby/slave/secondary），统一按关键字判断。
function roleClass(i) {
  const r = String(i.role || '').toLowerCase()
  if (r.includes('master') || r.includes('primary') || r.includes('leader')) return 'rel-master'
  if (r.includes('replica') || r.includes('standby') || r.includes('slave') || r.includes('secondary')) return 'rel-replica'
  return 'rel-standalone'
}

// 空状态图标：保留原有"扫描靶心"造型（内联 SVG，避免为一个空态引入图片资源）
const EmptyIcon = () =>
  h('svg', {
    viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', 'stroke-width': 1.5,
    width: 32, height: 32, innerHTML: '<circle cx="12" cy="12" r="10"/><path d="M12 2v20M2 12h20"/>',
  })

const instances = ref([])
// B9：loading 初值必须为 true——否则首屏会先闪一下"尚未采集到实例"空态再出数据。
const loading = ref(true)
const error = ref('')
const drawer = ref(false)
const selected = ref(null)
const currentPage = ref(1)
const pageSize = ref(20)

const stats = computed(() => {
  const up = instances.value.filter((i) => i.up).length
  return { total: instances.value.length, up, down: instances.value.length - up }
})

const pagedInstances = computed(() => {
  const s = (currentPage.value - 1) * pageSize.value
  return instances.value.slice(s, s + pageSize.value)
})

const detailFields = computed(() => {
  const s = selected.value
  if (!s) return []
  const f = [{ label: '节点', value: s.node, mono: true }]
  if (s.role) f.push({ label: '角色', value: s.role })
  if (s.version) f.push({ label: '版本', value: s.version })
  return f
})

function metricOf(row, key) {
  const hit = (row.metrics || []).find((m) => m.key === key)
  return hit ? hit.value : null
}
function metricValue(row, key, format) {
  const v = metricOf(row, key)
  return v === null ? '-' : formatValue(v, format)
}
// formatValue 只做展示口径转换：服务端给的是基础单位（如 MB），
// 这里按人读的单位换档（MB/GB/TB）。未知 format 原样返回，避免静默改数。
function formatValue(v, format) {
  if (format === 'mb') return formatMB(v)
  if (format === 'compact') return formatCompact(v)
  if (format === 'bytes') return formatBytes(v)
  if (format === 'pct') return formatPercent(v)
  return v
}
// formatBytes 把原始字节换算成 B/KB/MB/GB/TB。
// 与 'mb' 的区别：'mb' 用于服务端已换算成 MB 的指标，'bytes' 用于仍以字节返回的指标。
function formatBytes(b) {
  if (!b) return '-'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  let n = Number(b)
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024
    i++
  }
  return n.toFixed(1) + ' ' + units[i]
}
function formatPercent(v) {
  if (v === null || v === undefined || v === '') return '-'
  return Number(v).toFixed(1) + '%'
}
// formatCompact 用于请求数这类大整数：1.5K / 2.3M，避免表格被长数字撑开
function formatCompact(n) {
  if (!n) return '0'
  if (n >= 1000000) return (n / 1000000).toFixed(1) + 'M'
  if (n >= 1000) return (n / 1000).toFixed(1) + 'K'
  return n.toFixed(0)
}
// formatUptime 把秒数转成「X天Y小时」；不足 1 小时按小时显示
function formatUptime(s) {
  if (!s) return '-'
  const d = Math.floor(s / 86400)
  const h = Math.floor((s % 86400) / 3600)
  return d > 0 ? `${d}天${h}小时` : `${h}小时`
}
function formatMB(n) {
  if (!n && n !== 0) return '-'
  if (n < 1024) return n.toFixed(1) + ' MB'
  if (n < 1024 * 1024) return (n / 1024).toFixed(1) + ' GB'
  return (n / 1024 / 1024).toFixed(2) + ' TB'
}
function sortByMetric(a, b, key) {
  const va = metricOf(a, key), vb = metricOf(b, key)
  return (va ?? 0) - (vb ?? 0)
}
function formatNum(v) {
  if (v === null || v === undefined) return '-'
  if (typeof v !== 'number') return v
  return v.toLocaleString('zh-CN', { maximumFractionDigits: 2 })
}
function formatKpi(v, format) {
  if (typeof v !== 'number') return v
  return format ? formatValue(v, format) : formatNum(v)
}
function kpiValue(k) {
  const vals = instances.value.map((i) => metricOf(i, k.metric)).filter((v) => v !== null)
  if (!vals.length) return '-'
  if (k.agg === 'max') return Math.max(...vals)
  if (k.agg === 'avg') return vals.reduce((s, v) => s + v, 0) / vals.length
  return vals.reduce((s, v) => s + v, 0)
}
function rowClass({ row }) {
  return row.up ? '' : 'row-down'
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const data = await http.get(`/api/v1/middleware/${encodeURIComponent(props.spec.type)}/instances`)
    instances.value = data.instances || []
  } catch (e) {
    console.error('加载实例失败', e)
    error.value = `${props.spec.label} 实例数据加载失败`
  } finally {
    loading.value = false
  }
}

// ---- 详情抽屉趋势图 ----
// B3 修复：删除组件内自带的 setInterval(load, 30000)——
// 它与 RefreshBar 的自动刷新构成双定时器，同一份数据被拉两遍。
// 自动刷新统一交给 RefreshBar（它还带页面隐藏暂停）。
const range = ref('1h')
const rangeLabel = computed(() => (MW_RANGE_OPTIONS.find((r) => r.value === range.value) || {}).label || '')
const trendData = ref([])

function openDetail(row) {
  selected.value = row
  drawer.value = true
  loadTrends()
}

// 一张趋势图可以画一条或多条曲线：单指标的旧写法 {name, metric} 仍可用，
// 多指标写法 {name, metrics:[{name, metric, scale}]} 用于同量纲对比（如总空间 vs 已用）。
function trendMetrics(t) {
  return t.metrics || [{ name: t.name, metric: t.metric }]
}

async function loadTrends() {
  const row = selected.value
  if (!row) return
  const trends = props.spec.trends || []
  trendData.value = trends.map((t) => ({ name: t.name, option: null, loading: true, error: '' }))
  const { start, end } = rangeToWindow(range.value)
  const step = rangeToStep(range.value)
  // 并行拉取（此前是串行 await，8~9 个指标逐个等，抽屉打开明显卡顿）
  await Promise.all(
    trends.map(async (t, i) => {
      try {
        const parts = await Promise.all(
          trendMetrics(t).map(async (m) => {
            const data = await http.get(
              `/api/v1/query/range?node=${encodeURIComponent(row.node)}&metric=${encodeURIComponent(m.metric)}&start=${start}&end=${end}&step=${step}`
            )
            const acc = []
            for (const s of data.series || []) {
              if (s.labels?.instance !== row.instance) continue
              acc.push({
                name: m.name,
                data: (s.points || []).map((p) => [p.timestamp, m.scale ? p.value * m.scale : p.value]),
              })
            }
            return acc
          })
        )
        trendData.value[i].option = mwTrendOption(parts.flat(), { unit: t.unit })
      } catch (e) {
        console.error('趋势查询失败', e)
        trendData.value[i].error = '趋势数据加载失败'
      } finally {
        trendData.value[i].loading = false
      }
    })
  )
}

// 切换时间范围后重查
watch(range, () => { if (drawer.value) loadTrends() })

// 图表实例生命周期交给 TrendChart（抽屉 destroy-on-close，关闭即卸载即 dispose），
// 这里不再维护 charts 数组，也就不会出现"反复开关累积 ECharts 实例"的泄漏。
onMounted(load)
</script>

<style scoped>
.mw-tab { padding: 4px 0; }
.mw-err { margin-bottom: 16px; }
.mw-err-text { margin-right: 8px; }
.kpi-row { display: grid; grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); gap: 12px; margin-bottom: 14px; }
.cell-dot { margin-right: 8px; vertical-align: middle; }
.pager { margin-top: 10px; display: flex; justify-content: flex-end; }
.mono { font-family: var(--mono); }
.dim { color: var(--t3); font-size: var(--fs-sm); }
.mw-block { margin-bottom: 16px; }
.metric-bad .mc-value { color: var(--danger); }

/* 实例拓扑 */
.topo-group { margin-bottom: 16px; }
.topo-group:last-child { margin-bottom: 0; }
.topo-group + .topo-group { padding-top: 16px; border-top: 1px dashed var(--bd); }
.topo-group-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px; flex-wrap: wrap; gap: 8px; }
.topo-group-title { display: inline-flex; align-items: center; gap: 6px; font-size: var(--fs-base); color: var(--t1); }
.topo-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); gap: 12px; }
.rel-node { padding: 12px 14px; border-radius: var(--r-lg); cursor: pointer; border: 1px solid var(--bd); background: var(--s1); transition: transform 0.15s, box-shadow 0.15s; }
.rel-node:hover { transform: translateY(-2px); box-shadow: 0 4px 16px rgba(0,0,0,0.3); }
.rel-node.is-down { opacity: 0.6; }
.rel-node-name { font-size: var(--fs-base); font-weight: 600; color: var(--t1); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.rel-node-meta { display: flex; align-items: center; gap: 6px; font-size: var(--fs-sm); color: var(--t2); margin-top: 6px; }
.rel-standalone { border-left: 4px solid var(--chart-blue); }
.rel-master { border-left: 4px solid var(--chart-orange); }
.rel-replica { border-left: 4px solid var(--chart-green); }
:deep(.row-down) { opacity: 0.55; }
.metric-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(160px, 1fr)); gap: 10px; margin-bottom: 16px; }
.metric-cell { background: var(--fill-1); border-radius: var(--r-md); padding: 10px 12px; }
.mc-label { font-size: var(--fs-xs); color: var(--t3); margin-bottom: 4px; }
.mc-value { font-size: var(--fs-lg); font-weight: 600; font-family: var(--mono); }
.trend-block { margin-bottom: 18px; }
.trend-title { font-size: var(--fs-sm); color: var(--t3); margin-bottom: 6px; }
</style>
