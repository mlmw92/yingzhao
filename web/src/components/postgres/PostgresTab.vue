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
      title="尚未配置 PostgreSQL 监控"
      :hints="[
        '当前没有已采集的 PostgreSQL 实例，请在运行 Agent 的节点上配置实例连接信息（agent.yaml 对应实例段）',
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
        <KpiCard label="总连接数" :value="formatNum(stats.totalConnections)" tone="conn">
          <template #icon><el-icon :size="20"><Connection /></el-icon></template>
        </KpiCard>
        <KpiCard label="总事务提交" :value="formatNum(stats.totalCommits)" tone="ops">
          <template #icon><el-icon :size="20"><DataLine /></el-icon></template>
        </KpiCard>
        <KpiCard label="总死锁数" :value="stats.totalDeadlocks" tone="total">
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
              class="rel-node"
              :class="[roleClass(i), { 'is-down': !i.up }]"
              @click="openDetail(i)"
            >
              <div class="rel-node-name" :title="i.name || i.instance">{{ i.name || i.instance }}</div>
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
        <el-table class="postgres-table" :data="pagedInstances" style="width: 100%" @row-click="openDetail" :row-class-name="rowClass" size="small" stripe @sort-change="onSortChange">
          <el-table-column prop="instance" label="实例地址" min-width="150" show-overflow-tooltip />
          <el-table-column prop="name" label="名称" min-width="110" show-overflow-tooltip />
          <el-table-column prop="role" label="角色" min-width="75">
            <template #default="{ row }"><MwRoleTag :role="row.role" /></template>
          </el-table-column>
          <el-table-column prop="version" label="版本" min-width="135" show-overflow-tooltip />
          <el-table-column label="状态" min-width="80">
            <template #default="{ row }"><MwStatusDot :status="row.up ? 'normal' : 'abnormal'" :label="row.up ? '正常' : '离线'" /></template>
          </el-table-column>
          <el-table-column prop="numbackends" label="连接数" min-width="85" sortable />
          <el-table-column prop="cacheHitRatio" label="缓存命中率" min-width="110" sortable>
            <template #default="{ row }"><span :class="hitRateClass(row.cacheHitRatio)">{{ row.cacheHitRatio ? row.cacheHitRatio.toFixed(1) + '%' : '-' }}</span></template>
          </el-table-column>
          <el-table-column prop="deadlocks" label="死锁" min-width="70" sortable />
          <el-table-column prop="replicationLag" label="复制延迟(bytes)" min-width="135" sortable />
          <el-table-column label="数据库大小" min-width="100">
            <template #default="{ row }">{{ formatBytes(row.databaseSize) }}</template>
          </el-table-column>
        </el-table>
        <div class="pager">
          <el-pagination background layout="total, sizes, prev, pager, next, jumper" :total="sortedInstances.length" :page-size="pageSize" :current-page="currentPage" :page-sizes="[10,20,50,100]" @current-change="v => currentPage = v" @size-change="v => { pageSize = v; currentPage = 1 }" />
        </div>
      </SectionCard>
    </template>

    <el-drawer v-model="drawerVisible" title="PostgreSQL 实例详情" size="50%" :destroy-on-close="true">
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
          <div class="metric-cell"><div class="mc-label">连接数</div><div class="mc-value">{{ selected.numbackends }}</div></div>
          <div class="metric-cell"><div class="mc-label">最大连接</div><div class="mc-value">{{ selected.maxConnections }}</div></div>
          <div class="metric-cell"><div class="mc-label">事务提交</div><div class="mc-value">{{ formatNum(selected.xactCommit) }}</div></div>
          <div class="metric-cell"><div class="mc-label">事务回滚</div><div class="mc-value">{{ formatNum(selected.xactRollback) }}</div></div>
          <div class="metric-cell"><div class="mc-label">缓存命中率</div><div class="mc-value">{{ selected.cacheHitRatio ? selected.cacheHitRatio.toFixed(1) + '%' : '-' }}</div></div>
          <div class="metric-cell"><div class="mc-label">死锁</div><div class="mc-value">{{ selected.deadlocks }}</div></div>
          <div class="metric-cell"><div class="mc-label">复制延迟</div><div class="mc-value">{{ formatBytes(selected.replicationLag) }}</div></div>
          <div class="metric-cell"><div class="mc-label">数据库大小</div><div class="mc-value">{{ formatBytes(selected.databaseSize) }}</div></div>
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
import { Grid, CircleCheck, CircleClose, Connection, DataLine, Bell } from '@element-plus/icons-vue'
import { mwTrendOption } from '../../charts/echarts'
import { rangeToWindow, rangeToStep, MW_RANGE_OPTIONS } from '../mw/mwRange'

// 空状态图标：保留原有"数据库圆柱"造型（内联 SVG，避免为一个空态引入图片资源）
const EmptyIcon = () =>
  h('svg', {
    viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', 'stroke-width': 1.5,
    innerHTML:
      '<ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M3 5v6c0 1.66 4 3 9 3s9-1.34 9-3V5"/><path d="M3 11v6c0 1.66 4 3 9 3s9-1.34 9-3v-6"/>',
  })

// 详情抽屉趋势图：连接数与缓存命中率两条。多指标并行拉取（此前只有一条且串行）。
const TRENDS = [
  { name: '连接数', metric: 'postgres_numbackends', unit: '个' },
  { name: '缓存命中率', metric: 'postgres_cache_hit_ratio', unit: '%', yMax: 100 },
]

const loading = ref(true)
const instances = ref([])
const error = ref('')
const drawerVisible = ref(false)
const selected = ref(null)

const stats = computed(() => {
  const s = { total: 0, up: 0, down: 0, totalConnections: 0, totalCommits: 0, totalDeadlocks: 0 }
  for (const i of instances.value) {
    s.total++
    if (i.up) s.up++; else s.down++
    s.totalConnections += i.numbackends || 0
    s.totalCommits += i.xactCommit || 0
    s.totalDeadlocks += i.deadlocks || 0
  }
  return s
})

// 拓扑分组：按后端 group 字段真实聚合，未分组归入「独立实例」。
// 此前所有实例被硬编码塞进同一个「独立实例」组，多集群/多分组场景下分组信息完全丢失。
const topoGroups = computed(() => {
  const map = new Map()
  for (const i of instances.value) {
    const g = String(i.group || '').trim() || '独立实例'
    if (!map.has(g)) map.set(g, [])
    map.get(g).push(i)
  }
  return [...map.entries()].map(([name, list]) => ({ name, list }))
})

// 节点左边框按角色着色：主库橙、备库绿、独立蓝（与列表角色标签同一套语义）。
function roleClass(i) {
  const r = String(i.role || '').toLowerCase()
  if (r.includes('master') || r.includes('primary') || r.includes('leader')) return 'rel-master'
  if (r.includes('replica') || r.includes('standby') || r.includes('slave') || r.includes('secondary')) return 'rel-replica'
  return 'rel-standalone'
}

const detailFields = computed(() => {
  const s = selected.value
  if (!s) return []
  const f = [
    { label: '实例', value: s.instance, mono: true },
    { label: '节点', value: s.node, mono: true },
  ]
  if (s.role) f.push({ label: '角色', value: s.role })
  if (s.group) f.push({ label: '分组', value: s.group })
  if (s.version) f.push({ label: '版本', value: s.version })
  if (s.database) f.push({ label: '数据库', value: s.database })
  return f
})

async function load() {
  loading.value = true
  error.value = ''
  try {
    const data = await http.get('/api/v1/middleware/postgres/instances')
    instances.value = data.instances || []
  } catch (e) {
    console.error('加载 PostgreSQL 实例失败', e)
    error.value = 'PostgreSQL 实例数据加载失败'
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
        trendData.value[i].option = mwTrendOption(series, { unit: t.unit, yMax: t.yMax })
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
watch(range, () => { if (drawerVisible.value) loadTrends() })

function formatNum(n) { return n != null ? Number(n).toLocaleString() : '-' }
function formatBytes(b) { if (!b) return '-'; const u = ['B','KB','MB','GB','TB']; let i = 0; while (b >= 1024 && i < u.length-1) { b /= 1024; i++ } return b.toFixed(1) + ' ' + u[i] }
function formatUptime(s) { if (!s) return '-'; const d = Math.floor(s / 86400); const h = Math.floor((s % 86400) / 3600); return d > 0 ? `${d}天${h}小时` : `${h}小时` }
function hitRateClass(v) { if (!v) return ''; if (v >= 99) return 'metric-good'; if (v >= 90) return 'metric-warn'; return 'metric-bad' }
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
.metric-good { color: var(--chart-green); }
.metric-warn { color: var(--warn); }
.metric-bad { color: var(--danger); }
.pager { display: flex; justify-content: flex-end; margin-top: 12px; }
.postgres-table :deep(th) { white-space: nowrap; }
.postgres-table :deep(td) { white-space: nowrap; }
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
.rel-master { border-left: 4px solid var(--chart-orange); }
.rel-replica { border-left: 4px solid var(--chart-green); }

/* 详情抽屉 */
.detail-content { padding: 0 4px; }
.metric-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(140px, 1fr)); gap: 12px; margin-bottom: 20px; }
.metric-cell { padding: 12px; background: var(--fill-1); border-radius: var(--r-md); text-align: center; }
.mc-label { font-size: var(--fs-xs); color: var(--t3); margin-bottom: 4px; }
.mc-value { font-size: var(--fs-lg); font-weight: 600; }
.trend-block { margin-bottom: 18px; }
.trend-title { font-size: var(--fs-sm); color: var(--t3); margin-bottom: 6px; }
</style>
