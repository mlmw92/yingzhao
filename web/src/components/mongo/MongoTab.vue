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
      title="尚未配置 MongoDB 监控"
      :hints="[
        '当前没有已采集的 MongoDB 实例，请在运行 Agent 的节点上配置实例连接信息（agent.yaml 对应实例段）',
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
        <KpiCard label="当前总连接数" :value="formatNum(stats.totalConnections)" tone="conn">
          <template #icon><el-icon :size="20"><Connection /></el-icon></template>
        </KpiCard>
        <KpiCard label="文档总数" :value="formatNum(stats.totalObjects)" tone="ops">
          <template #icon><el-icon :size="20"><Coin /></el-icon></template>
        </KpiCard>
        <KpiCard label="库数据总量" :value="formatMB(stats.totalDataSize)" tone="total">
          <template #icon><el-icon :size="20"><DataLine /></el-icon></template>
        </KpiCard>
      </div>

      <SectionCard title="实例列表" dense class="mw-block">
        <el-table class="mongo-table" :data="pagedInstances" style="width: 100%" @row-click="openDetail" :row-class-name="rowClass" size="small" stripe @sort-change="onSortChange">
          <el-table-column prop="instance" label="实例地址" min-width="150" show-overflow-tooltip />
          <el-table-column prop="name" label="名称" min-width="110" show-overflow-tooltip />
          <el-table-column prop="role" label="角色" min-width="85">
            <template #default="{ row }"><MwRoleTag :role="row.role" /></template>
          </el-table-column>
          <el-table-column prop="topology" label="拓扑" min-width="90" show-overflow-tooltip />
          <el-table-column prop="version" label="版本" min-width="110" show-overflow-tooltip />
          <el-table-column label="状态" min-width="80">
            <template #default="{ row }"><MwStatusDot :status="row.up ? 'normal' : 'abnormal'" :label="row.up ? '正常' : '离线'" /></template>
          </el-table-column>
          <el-table-column prop="connectionsCurrent" label="连接数" min-width="85" sortable />
          <el-table-column label="常驻内存" min-width="110">
            <template #default="{ row }">{{ row.memResidentMB ? row.memResidentMB.toFixed(0) + ' MB' : '-' }}</template>
          </el-table-column>
          <el-table-column label="数据大小" min-width="100">
            <template #default="{ row }">{{ row.dbDataSizeMB ? row.dbDataSizeMB.toFixed(1) + ' MB' : '-' }}</template>
          </el-table-column>
          <el-table-column prop="replLag" label="复制延迟(s)" min-width="115" sortable />
        </el-table>
        <div class="pager">
          <el-pagination background layout="total, sizes, prev, pager, next, jumper" :total="sortedInstances.length" :page-size="pageSize" :current-page="currentPage" :page-sizes="[10,20,50,100]" @current-change="v => currentPage = v" @size-change="v => { pageSize = v; currentPage = 1 }" />
        </div>
      </SectionCard>
    </template>

    <el-drawer v-model="drawerVisible" title="MongoDB 实例详情" size="50%" :destroy-on-close="true">
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
          <div class="metric-cell"><div class="mc-label">当前连接</div><div class="mc-value">{{ formatNum(selected.connectionsCurrent) }}</div></div>
          <div class="metric-cell"><div class="mc-label">可用连接</div><div class="mc-value">{{ formatNum(selected.connectionsAvailable) }}</div></div>
          <div class="metric-cell"><div class="mc-label">常驻内存</div><div class="mc-value">{{ selected.memResidentMB ? selected.memResidentMB.toFixed(0) + ' MB' : '-' }}</div></div>
          <div class="metric-cell"><div class="mc-label">虚拟内存</div><div class="mc-value">{{ selected.memVirtualMB ? selected.memVirtualMB.toFixed(0) + ' MB' : '-' }}</div></div>
          <div class="metric-cell"><div class="mc-label">文档数</div><div class="mc-value">{{ formatNum(selected.dbObjects) }}</div></div>
          <div class="metric-cell"><div class="mc-label">索引数</div><div class="mc-value">{{ formatNum(selected.dbIndexes) }}</div></div>
          <div class="metric-cell"><div class="mc-label">复制延迟</div><div class="mc-value">{{ selected.replLag ? selected.replLag.toFixed(1) + ' s' : '-' }}</div></div>
          <div class="metric-cell"><div class="mc-label">运行时长</div><div class="mc-value">{{ formatUptime(selected.uptime) }}</div></div>
        </div>
        <div v-for="t in trendData" :key="t.title" class="trend-block">
          <div class="trend-title">{{ t.title }}（{{ rangeLabel }}）</div>
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
import { Grid, CircleCheck, CircleClose, Connection, Coin, DataLine } from '@element-plus/icons-vue'
import { mwTrendOption } from '../../charts/echarts'
import { rangeToWindow, rangeToStep, MW_RANGE_OPTIONS } from '../mw/mwRange'

// 空状态图标：保留原有"叶片/校验"造型
const EmptyIcon = () =>
  h('svg', {
    viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', 'stroke-width': 1.5,
    innerHTML: '<path d="M12 2c4 3 6 6 6 10 0 3-2 5-6 5s-6-2-6-5c0-4 2-7 6-10z"/><path d="M9 13l2 2 4-4"/>',
  })

// 详情抽屉趋势图：连接数与常驻内存单位不同，拆成两张图（避免单轴量纲混叠）。
const TRENDS = [
  { title: '连接数', unit: '个', metrics: [{ name: '当前连接', metric: 'mongodb_connections_current' }] },
  { title: '常驻内存', unit: 'MB', metrics: [{ name: '常驻内存', metric: 'mongodb_mem_resident_bytes', scale: 1 / 1048576 }] },
]

const loading = ref(true)
const instances = ref([])
const error = ref('')
const drawerVisible = ref(false)
const selected = ref(null)

const stats = computed(() => {
  const s = { total: 0, up: 0, down: 0, totalConnections: 0, totalObjects: 0, totalDataSize: 0 }
  for (const i of instances.value) {
    s.total++
    if (i.up) s.up++; else s.down++
    s.totalConnections += i.connectionsCurrent || 0
    s.totalObjects += i.dbObjects || 0
    s.totalDataSize += i.dbDataSizeMB || 0
  }
  return s
})

const detailFields = computed(() => {
  const s = selected.value
  if (!s) return []
  const f = [
    { label: '实例', value: s.instance, mono: true },
    { label: '节点', value: s.node, mono: true },
  ]
  if (s.role) f.push({ label: '角色', value: s.role })
  if (s.topology) f.push({ label: '拓扑', value: s.topology })
  if (s.version) f.push({ label: '版本', value: s.version })
  return f
})

async function load() {
  loading.value = true
  error.value = ''
  try {
    const data = await http.get('/api/v1/middleware/mongodb/instances')
    instances.value = data.instances || []
  } catch (e) {
    console.error('加载 MongoDB 实例失败', e)
    error.value = 'MongoDB 实例数据加载失败'
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
  trendData.value = TRENDS.map((t) => ({ title: t.title, option: null, loading: true, error: '' }))
  const { start, end } = rangeToWindow(range.value)
  const step = rangeToStep(range.value)
  await Promise.all(
    TRENDS.map(async (t, i) => {
      try {
        const parts = await Promise.all(
          t.metrics.map(async (m) => {
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

watch(range, () => { if (drawerVisible.value) loadTrends() })

function formatNum(n) { return n != null ? Number(n).toLocaleString() : '-' }
function formatMB(n) { if (!n && n !== 0) return '-'; if (n < 1024) return n.toFixed(1) + ' MB'; return (n / 1024).toFixed(1) + ' GB' }
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
.mono { font-family: var(--mono); }
.pager { display: flex; justify-content: flex-end; margin-top: 12px; }
.mongo-table :deep(th) { white-space: nowrap; }
.mongo-table :deep(td) { white-space: nowrap; }
:deep(.row-down) { opacity: 0.6; }

/* 详情抽屉 */
.detail-content { padding: 0 4px; }
.metric-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(140px, 1fr)); gap: 12px; margin-bottom: 20px; }
.metric-cell { padding: 12px; background: var(--fill-1); border-radius: var(--r-md); text-align: center; }
.mc-label { font-size: var(--fs-xs); color: var(--t3); margin-bottom: 4px; }
.mc-value { font-size: var(--fs-lg); font-weight: 600; }
.trend-block { margin-bottom: 18px; }
.trend-title { font-size: var(--fs-sm); color: var(--t3); margin-bottom: 6px; }
</style>
