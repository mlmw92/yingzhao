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
      title="尚未配置 Nginx 监控"
      :hints="[
        '当前没有已采集的 Nginx 实例，请在运行 Agent 的节点上启用 Nginx stub_status 模块',
        '配置完成后约 15-30 秒数据将出现在此页面',
      ]"
    />

    <template v-if="instances.length > 0">
      <div class="kpi-row">
        <KpiCard label="实例总数" :value="stats.total" tone="total">
          <template #icon><ServerIcon /></template>
        </KpiCard>
        <KpiCard label="在线实例" :value="stats.up" tone="up">
          <template #icon><CheckCircleIcon /></template>
        </KpiCard>
        <KpiCard label="离线实例" :value="stats.down" :tone="stats.down > 0 ? 'down' : 'total'">
          <template #icon><XCircleIcon /></template>
        </KpiCard>
        <KpiCard label="总活动连接" :value="stats.totalActive" tone="conn">
          <template #icon><ConnectionIcon /></template>
        </KpiCard>
        <KpiCard label="总请求" :value="formatNum(stats.totalRequests)" tone="ops">
          <template #icon><ActivityIcon /></template>
        </KpiCard>
        <KpiCard label="总等待连接" :value="stats.totalWaiting" tone="mem">
          <template #icon><ClockIcon /></template>
        </KpiCard>
      </div>

      <SectionCard title="实例拓扑" class="mw-block">
        <div v-for="g in topoGroups" :key="g.name" class="topo-group">
          <div class="topo-group-header">
            <span class="topo-group-title"><ServerIcon /><strong>{{ g.name }}</strong></span>
            <span class="dim">共 {{ g.list.length }} 个</span>
          </div>
          <div class="topo-grid">
            <div
              v-for="i in g.list"
              :key="i.instance"
              class="rel-node rel-standalone"
              :class="{ 'is-down': !i.up }"
              @click="openDetail(i)"
            >
              <div class="rel-node-name" :title="i.name || i.instance">{{ i.name || i.instance }}</div>
              <div class="rel-node-meta">
                <MwStatusDot :status="i.up ? 'normal' : 'abnormal'" :label="i.up ? '在线' : '离线'" />
                <span class="dim">·</span>
                <span class="mono">{{ i.nodeIp || nginxDisplayAddr(i) }}</span>
              </div>
            </div>
          </div>
        </div>
      </SectionCard>

      <SectionCard title="实例列表" dense class="mw-block">
        <el-table :data="pagedInstances" style="width: 100%" @row-click="openDetail" :row-class-name="rowClass" size="small" stripe @sort-change="onSortChange">
          <el-table-column label="实例地址" min-width="140" show-overflow-tooltip>
            <template #default="{ row }"><span class="mono">{{ row.nodeIp || nginxDisplayAddr(row) }}</span></template>
          </el-table-column>
          <el-table-column prop="name" label="名称" min-width="90" show-overflow-tooltip />
          <el-table-column label="状态" width="80">
            <template #default="{ row }"><MwStatusDot :status="row.up ? 'normal' : 'abnormal'" :label="row.up ? '正常' : '离线'" /></template>
          </el-table-column>
          <el-table-column prop="activeConnections" label="活动连接" min-width="85" sortable />
          <el-table-column label="已接收" min-width="85" sortable :sort-by="'accepts'">
            <template #default="{ row }"><span class="mono">{{ formatNum(row.accepts) }}</span></template>
          </el-table-column>
          <el-table-column label="已处理" min-width="85" sortable :sort-by="'handled'">
            <template #default="{ row }"><span class="mono">{{ formatNum(row.handled) }}</span></template>
          </el-table-column>
          <el-table-column label="总请求" min-width="85" sortable :sort-by="'requests'">
            <template #default="{ row }"><span class="mono">{{ formatNum(row.requests) }}</span></template>
          </el-table-column>
          <el-table-column label="读取" width="65" sortable :sort-by="'reading'">
            <template #default="{ row }"><span class="mono">{{ row.reading }}</span></template>
          </el-table-column>
          <el-table-column label="写入" width="65" sortable :sort-by="'writing'">
            <template #default="{ row }"><span class="mono">{{ row.writing }}</span></template>
          </el-table-column>
          <el-table-column label="等待" width="65" sortable :sort-by="'waiting'">
            <template #default="{ row }"><span class="mono">{{ row.waiting }}</span></template>
          </el-table-column>
        </el-table>
        <div class="pager">
          <el-pagination background layout="total, sizes, prev, pager, next, jumper" :total="sortedInstances.length" :page-size="pageSize" :current-page="currentPage" :page-sizes="[10,20,50,100]" @current-change="v => currentPage = v" @size-change="v => { pageSize = v; currentPage = 1 }" />
        </div>
      </SectionCard>
    </template>

    <el-drawer v-model="drawerVisible" title="Nginx 实例详情" size="50%" :destroy-on-close="true">
      <div v-if="selected" class="detail-content">
        <MwDetailHeader
          :title="selected.name || selected.instance"
          :subtitle="selected.nodeIp || nginxDisplayAddr(selected)"
          :status="selected.up ? 'normal' : 'abnormal'"
          :status-label="selected.up ? '在线' : '离线'"
          :fields="detailFields"
          v-model:range="range"
        />
        <div class="metric-grid">
          <div class="metric-cell"><div class="mc-label">活动连接</div><div class="mc-value">{{ selected.activeConnections }}</div></div>
          <div class="metric-cell"><div class="mc-label">已接收</div><div class="mc-value">{{ selected.accepts }}</div></div>
          <div class="metric-cell"><div class="mc-label">等待连接</div><div class="mc-value">{{ selected.waiting }}</div></div>
          <div class="metric-cell"><div class="mc-label">读取中</div><div class="mc-value">{{ selected.reading }}</div></div>
          <div class="metric-cell"><div class="mc-label">写入中</div><div class="mc-value">{{ selected.writing }}</div></div>
          <div class="metric-cell"><div class="mc-label">已处理</div><div class="mc-value">{{ selected.handled }}</div></div>
          <div class="metric-cell"><div class="mc-label">总请求</div><div class="mc-value">{{ selected.requests }}</div></div>
          <div class="metric-cell"><div class="mc-label">运行时长</div><div class="mc-value">{{ formatUptime(selected.uptime) }}</div></div>
        </div>
        <div v-for="t in trendData" :key="t.metric" class="trend-block">
          <div class="trend-title">{{ t.name }}（{{ rangeLabel }}）</div>
          <TrendChart :option="t.option" :loading="t.loading" :error="t.error" :height="220" @retry="loadTrends" />
        </div>
      </div>
    </el-drawer>
  </div>
</template>

<script setup>
import { ref, onMounted, computed, watch, h } from 'vue'
import http from '../../api/http'
import RefreshBar from '../RefreshBar.vue'
import KpiCard from '../KpiCard.vue'
import SectionCard from '../common/SectionCard.vue'
import EmptyState from '../common/EmptyState.vue'
import MwStatusDot from '../mw/MwStatusDot.vue'
import MwDetailHeader from '../mw/MwDetailHeader.vue'
import TrendChart from '../mw/TrendChart.vue'
import { mwTrendOption } from '../../charts/echarts'
import { rangeToWindow, rangeToStep, MW_RANGE_OPTIONS } from '../mw/mwRange'

// ---- 图标组件（内联 SVG 渲染函数） ----
function svgIcon(s) {
  const inner = s.replace(/^<svg[^>]*>/, '').replace(/<\/svg>\s*$/, '')
  return () => h('svg', { viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', 'stroke-width': 2, innerHTML: inner })
}
const ServerIcon = svgIcon('<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="2" y="3" width="20" height="8" rx="2"/><rect x="2" y="13" width="20" height="8" rx="2"/><line x1="6" y1="7" x2="6.01" y2="7"/><line x1="6" y1="17" x2="6.01" y2="17"/></svg>')
const CheckCircleIcon = svgIcon('<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><polyline points="9 12 12 15 16 10"/></svg>')
const XCircleIcon = svgIcon('<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><line x1="15" y1="9" x2="9" y2="15"/><line x1="9" y1="9" x2="15" y2="15"/></svg>')
const ConnectionIcon = svgIcon('<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M13.144 10.144a4 4 0 1 0-5.742 0"/><path d="M11 14.48V17"/><circle cx="11" cy="19" r="2"/><path d="M16 9a5 5 0 0 1 4.516 2.861"/><path d="M19.922 12.633a5 5 0 0 1-.39 4.155"/><circle cx="21" cy="18" r="2"/></svg>')
const ActivityIcon = svgIcon('<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="22 12 18 12 15 21 9 3 6 12 2 12"/></svg>')
const ClockIcon = svgIcon('<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg>')

// 空状态图标：保留原有"靶心/十字"造型
const EmptyIcon = () =>
  h('svg', {
    viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', 'stroke-width': 1.5,
    innerHTML: '<circle cx="12" cy="12" r="10"/><path d="M12 2v20M2 12h20"/>',
  })

// 详情抽屉趋势图：活动连接 + 等待连接（均为瞬时量，适合做时间序列）
const TRENDS = [
  { name: '活动连接', metric: 'nginx_active_connections', unit: '个' },
  { name: '等待连接', metric: 'nginx_waiting', unit: '个' },
]

const loading = ref(true)
const instances = ref([])
const error = ref('')
const drawerVisible = ref(false)
const selected = ref(null)

const stats = computed(() => {
  const s = { total: 0, up: 0, down: 0, totalActive: 0, totalRequests: 0, totalWaiting: 0 }
  for (const i of instances.value) {
    s.total++
    if (i.up) s.up++; else s.down++
    s.totalActive += i.activeConnections || 0
    s.totalRequests += i.requests || 0
    s.totalWaiting += i.waiting || 0
  }
  return s
})

// 拓扑分组：按后端 group 字段真实聚合，未分组归入「独立实例」。
const topoGroups = computed(() => {
  const map = new Map()
  for (const i of instances.value) {
    const g = String(i.group || '').trim() || '独立实例'
    if (!map.has(g)) map.set(g, [])
    map.get(g).push(i)
  }
  return [...map.entries()].map(([name, list]) => ({ name, list }))
})

const detailFields = computed(() => {
  const s = selected.value
  if (!s) return []
  const f = [
    { label: '节点', value: s.node, mono: true },
    { label: '实例地址', value: s.nodeIp || nginxDisplayAddr(s), mono: true },
  ]
  if (s.version) f.push({ label: '版本', value: s.version })
  if (s.group) f.push({ label: '分组', value: s.group })
  return f
})

async function load() {
  loading.value = true
  error.value = ''
  try {
    const data = await http.get('/api/v1/middleware/nginx/instances')
    instances.value = data.instances || []
  } catch (e) {
    console.error('加载 Nginx 实例失败', e)
    error.value = 'Nginx 实例数据加载失败'
  } finally {
    loading.value = false
  }
}

function openDetail(row) {
  selected.value = row
  drawerVisible.value = true
  loadTrends()
}

// ---- 详情抽屉趋势图 ----
const range = ref('1h')
const rangeLabel = computed(() => (MW_RANGE_OPTIONS.find((r) => r.value === range.value) || {}).label || '')
const trendData = ref([])

async function loadTrends() {
  const row = selected.value
  if (!row) return
  trendData.value = TRENDS.map((t) => ({ metric: t.metric, name: t.name, option: null, loading: true, error: '' }))
  const { start, end } = rangeToWindow(range.value)
  const step = rangeToStep(range.value)
  await Promise.all(
    TRENDS.map(async (t, i) => {
      try {
        const data = await http.get(
          `/api/v1/query/range?node=${encodeURIComponent(row.node)}&metric=${encodeURIComponent(t.metric)}&start=${start}&end=${end}&step=${step}`
        )
        const series = []
        for (const s of data.series || []) {
          if (s.labels?.instance !== row.instance) continue
          series.push({ name: t.name, data: (s.points || []).map((p) => [p.timestamp, p.value]) })
        }
        trendData.value[i].option = mwTrendOption(series, { unit: t.unit })
      } catch (e) {
        console.error('趋势查询失败', e)
        trendData.value[i].error = '趋势数据加载失败'
      } finally {
        trendData.value[i].loading = false
      }
    })
  )
}

watch(range, () => { if (drawerVisible.value) loadTrends() })

function formatUptime(s) { if (!s) return '-'; const d = Math.floor(s / 86400); const h = Math.floor((s % 86400) / 3600); return d > 0 ? `${d}天${h}小时` : `${h}小时` }
function formatNum(n) { if (!n) return '0'; if (n >= 1000000) return (n / 1000000).toFixed(1) + 'M'; if (n >= 1000) return (n / 1000).toFixed(1) + 'K'; return n.toFixed(0) }
function nginxDisplayAddr(i) { if (!i) return '-'; return i.nodeIp || i.instance || '-' }
function rowClass({ row }) { return row.up ? '' : 'row-down' }

const currentPage = ref(1)
const pageSize = ref(10)
const sortState = ref({ prop: '', order: '' })
function onSortChange({ prop, order }) { sortState.value = { prop, order }; currentPage.value = 1 }
const sortedInstances = computed(() => {
  const arr = [...instances.value]
  const { prop, order } = sortState.value
  if (prop && order) {
    arr.sort((a, b) => {
      let av = a[prop], bv = b[prop]
      if (typeof av === 'string') return order === 'ascending' ? av.localeCompare(bv) : bv.localeCompare(av)
      av = av ?? 0; bv = bv ?? 0
      return order === 'ascending' ? av - bv : bv - av
    })
  }
  return arr
})
const pagedInstances = computed(() => sortedInstances.value.slice((currentPage.value - 1) * pageSize.value, currentPage.value * pageSize.value))
watch(instances, () => { currentPage.value = 1 })

onMounted(load)
</script>

<style scoped>
.mw-tab { padding: 4px 0; }
.mw-err { margin-bottom: 16px; }
.mw-err-text { margin-right: 8px; }
.kpi-row { display: grid; grid-template-columns: repeat(auto-fit, minmax(160px, 1fr)); gap: 12px; margin-bottom: 16px; }
.mw-block { margin-bottom: 16px; }
.dim { color: var(--t3); font-size: var(--fs-sm); }
.mono { font-family: var(--mono); }
.pager { display: flex; justify-content: flex-end; margin-top: 12px; }
:deep(.row-down) { opacity: 0.6; }

/* 实例拓扑 */
.topo-group { margin-bottom: 16px; }
.topo-group:last-child { margin-bottom: 0; }
.topo-group + .topo-group { padding-top: 16px; border-top: 1px dashed var(--bd); }
.topo-group-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px; flex-wrap: wrap; gap: 8px; }
.topo-group-title { display: inline-flex; align-items: center; gap: 8px; font-size: var(--fs-base); color: var(--t1); }
.topo-group-title svg { width: 18px; height: 18px; color: var(--accent); flex-shrink: 0; }
.topo-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); gap: 12px; }
.rel-node { padding: 12px 14px; border-radius: var(--r-lg); cursor: pointer; border: 1px solid var(--bd); background: var(--s1); transition: transform 0.15s, box-shadow 0.15s; }
.rel-node:hover { transform: translateY(-2px); box-shadow: 0 4px 16px rgba(0,0,0,0.3); }
.rel-node.is-down { opacity: 0.6; }
.rel-node-name { font-size: var(--fs-base); font-weight: 600; color: var(--t1); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.rel-node-meta { display: flex; align-items: center; gap: 6px; font-size: var(--fs-sm); color: var(--t2); margin-top: 6px; }
.rel-standalone { border-left: 4px solid var(--chart-blue); }

/* 详情抽屉 */
.detail-content { padding: 0 4px; }
.metric-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(140px, 1fr)); gap: 12px; margin-bottom: 20px; }
.metric-cell { padding: 12px; background: var(--fill-1); border-radius: var(--r-md); text-align: center; }
.mc-label { font-size: var(--fs-xs); color: var(--t3); margin-bottom: 4px; }
.mc-value { font-size: var(--fs-lg); font-weight: 600; }
.trend-block { margin-bottom: 18px; }
.trend-title { font-size: var(--fs-sm); color: var(--t3); margin-bottom: 6px; }
</style>
