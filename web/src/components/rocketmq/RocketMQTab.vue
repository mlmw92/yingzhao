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
      title="尚未配置 RocketMQ 监控"
      :hints="[
        '当前没有已采集的 RocketMQ 实例，请在运行 Agent 的节点上配置 Broker 地址',
        '配置完成后约 15-30 秒数据将出现在此页面',
      ]"
    />

    <template v-if="instances.length > 0">
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
        <KpiCard label="总 Broker 数" :value="stats.totalBrokers" tone="cluster">
          <template #icon><el-icon :size="20"><Monitor /></el-icon></template>
        </KpiCard>
        <KpiCard label="总 Topic 数" :value="stats.totalTopics" tone="ops">
          <template #icon><el-icon :size="20"><Document /></el-icon></template>
        </KpiCard>
        <KpiCard label="总消息积压" :value="formatNum(stats.totalBacklog)" tone="total">
          <template #icon><el-icon :size="20"><Bell /></el-icon></template>
        </KpiCard>
      </div>

      <SectionCard title="实例拓扑" class="mw-block">
        <div v-for="g in topoGroups" :key="g.name" class="topo-group">
          <div class="topo-group-header">
            <span class="topo-group-title">
              <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.6"><rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/></svg>
              <strong>{{ g.name }}</strong>
            </span>
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
              <div class="rel-node-name" :title="i.instance">{{ i.name || i.instance }}</div>
              <div class="rel-node-meta">
                <MwStatusDot :status="i.up ? 'normal' : 'abnormal'" :label="i.up ? '在线' : '离线'" />
                <span class="dim">·</span>
                <span class="mono">{{ i.instance }}</span>
              </div>
              <div v-if="i.role" class="rel-node-meta"><span>角色：{{ i.role }}</span></div>
            </div>
          </div>
        </div>
      </SectionCard>

      <SectionCard title="实例列表" dense class="mw-block">
        <el-table class="rocketmq-table" :data="pagedInstances" style="width: 100%" @row-click="openDetail" :row-class-name="rowClass" size="small" stripe @sort-change="onSortChange">
          <el-table-column prop="group" label="分组" min-width="95" />
          <el-table-column prop="name" label="名称" min-width="110" show-overflow-tooltip />
          <el-table-column prop="instance" label="实例地址" min-width="150" show-overflow-tooltip />
          <el-table-column prop="role" label="角色" min-width="118">
            <template #default="{ row }"><MwRoleTag :role="row.role" /></template>
          </el-table-column>
          <el-table-column prop="version" label="版本" min-width="110" />
          <el-table-column label="状态" min-width="85">
            <template #default="{ row }"><MwStatusDot :status="row.up ? 'normal' : 'abnormal'" :label="row.up ? '正常' : '离线'" /></template>
          </el-table-column>
          <el-table-column prop="brokerCount" label="Broker 数" min-width="95" sortable />
          <el-table-column prop="topicCount" label="Topic 数" min-width="95" sortable />
          <el-table-column prop="consumerGroupCount" label="消费组" min-width="95" sortable />
          <el-table-column prop="consumerLag" label="消费延迟" min-width="100" sortable />
          <el-table-column prop="producerTps" label="生产 TPS" min-width="100" sortable />
          <el-table-column prop="consumerTps" label="消费 TPS" min-width="100" sortable />
        </el-table>
        <div class="pager">
          <el-pagination background layout="total, sizes, prev, pager, next, jumper" :total="sortedInstances.length" :page-size="pageSize" :current-page="currentPage" :page-sizes="[10,20,50,100]" @current-change="v => currentPage = v" @size-change="v => { pageSize = v; currentPage = 1 }" />
        </div>
      </SectionCard>
    </template>

    <el-drawer v-model="drawerVisible" title="RocketMQ 实例详情" size="50%" :destroy-on-close="true">
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
          <div class="metric-cell"><div class="mc-label">Broker 数</div><div class="mc-value">{{ selected.brokerCount }}</div></div>
          <div class="metric-cell"><div class="mc-label">Topic 数</div><div class="mc-value">{{ selected.topicCount }}</div></div>
          <div class="metric-cell"><div class="mc-label">消费组</div><div class="mc-value">{{ selected.consumerGroupCount }}</div></div>
          <div class="metric-cell"><div class="mc-label">消费延迟</div><div class="mc-value">{{ formatNum(selected.consumerLag) }}</div></div>
          <div class="metric-cell"><div class="mc-label">生产 TPS</div><div class="mc-value">{{ formatNum(selected.producerTps) }}</div></div>
          <div class="metric-cell"><div class="mc-label">消费 TPS</div><div class="mc-value">{{ formatNum(selected.consumerTps) }}</div></div>
          <div class="metric-cell"><div class="mc-label">消息积压</div><div class="mc-value" :class="{'metric-bad': selected.messageAccumulation > 0}">{{ formatNum(selected.messageAccumulation) }}</div></div>
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
import MwRoleTag from '../mw/MwRoleTag.vue'
import MwDetailHeader from '../mw/MwDetailHeader.vue'
import TrendChart from '../mw/TrendChart.vue'
import { Grid, CircleCheck, CircleClose, Monitor, Document, Bell } from '@element-plus/icons-vue'
import { mwTrendOption } from '../../charts/echarts'
import { rangeToWindow, rangeToStep, MW_RANGE_OPTIONS } from '../mw/mwRange'

// 空状态图标：保留原有"分层堆叠"造型
const EmptyIcon = () =>
  h('svg', {
    viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', 'stroke-width': 1.5,
    innerHTML: '<path d="M3 7l9-4 9 4-9 4-9-4z"/><path d="M3 12l9 4 9-4"/><path d="M3 17l9 4 9-4"/>',
  })

// 详情抽屉趋势图：消息积压 + 消费延迟
const TRENDS = [
  { name: '消息积压', metric: 'rocketmq_message_accumulation', unit: '条' },
  { name: '消费延迟', metric: 'rocketmq_consumer_lag', unit: '条' },
]

const loading = ref(true)
const instances = ref([])
const error = ref('')
const drawerVisible = ref(false)
const selected = ref(null)

const stats = computed(() => {
  const s = { total: 0, up: 0, down: 0, totalBrokers: 0, totalTopics: 0, totalBacklog: 0 }
  for (const i of instances.value) {
    s.total++
    if (i.up) s.up++; else s.down++
    s.totalBrokers += i.brokerCount || 0
    s.totalTopics += i.topicCount || 0
    // 「总消息积压」取各实例的积压总量（messageAccumulation）；
    // consumerLag 是单个消费组的最大延迟，语义不同，不能拿来当积压总量。
    s.totalBacklog += i.messageAccumulation || 0
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
    { label: '实例', value: s.instance, mono: true },
    { label: '节点', value: s.node, mono: true },
  ]
  if (s.group) f.push({ label: '分组', value: s.group })
  if (s.role) f.push({ label: '角色', value: s.role })
  if (s.version) f.push({ label: '版本', value: s.version })
  return f
})

async function load() {
  loading.value = true
  error.value = ''
  try {
    const data = await http.get('/api/v1/middleware/rocketmq/instances')
    instances.value = data.instances || []
  } catch (e) {
    console.error('加载 RocketMQ 实例失败', e)
    error.value = 'RocketMQ 实例数据加载失败'
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

function formatNum(n) { return n != null ? Number(n).toLocaleString() : '-' }
function formatUptime(s) { if (!s) return '-'; const d = Math.floor(s / 86400); const h = Math.floor((s % 86400) / 3600); return d > 0 ? `${d}天${h}小时` : `${h}小时` }
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
.metric-bad { color: var(--danger); }
.pager { display: flex; justify-content: flex-end; margin-top: 12px; }
.rocketmq-table :deep(th) { white-space: nowrap; }
.rocketmq-table :deep(td) { white-space: nowrap; }
:deep(.row-down) { opacity: 0.6; }

/* 实例拓扑 */
.topo-group { margin-bottom: 16px; }
.topo-group:last-child { margin-bottom: 0; }
.topo-group + .topo-group { padding-top: 16px; border-top: 1px dashed var(--bd); }
.topo-group-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px; flex-wrap: wrap; gap: 8px; }
.topo-group-title { display: inline-flex; align-items: center; gap: 6px; font-size: var(--fs-base); color: var(--t1); }
.topo-group-title svg { color: var(--accent); flex-shrink: 0; }
.topo-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); gap: 12px; }
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
