<template>
  <div class="mw-tab">
    <RefreshBar :loading="loading" @refresh="load" />

    <!-- 刷新失败：保留既有数据的同时在顶部显式提示（不再只 console.error 后静默留白） -->
    <el-alert v-if="error" type="error" :closable="false" show-icon class="mw-err">
      <template #title>
        <span class="mw-err-text">{{ error }}</span>
        <el-button size="small" link type="primary" @click="load">重试</el-button>
      </template>
    </el-alert>

    <EmptyState
      v-if="!loading && !error && hostCards.length === 0"
      :icon="dockerLogo"
      title="尚未配置 Docker 监控"
      :hints="[
        '在运行 Agent 的节点上启用 Docker 守护进程',
        '配置完成后约 15-30 秒数据将出现在此页面',
      ]"
    />

    <template v-if="hostCards.length > 0">
      <div class="kpi-row">
        <KpiCard label="主机总数" :value="stats.hosts.length" tone="total">
          <template #icon><ServerIcon /></template>
        </KpiCard>
        <KpiCard label="在线主机" :value="stats.upHosts" tone="up">
          <template #icon><CheckCircleIcon /></template>
        </KpiCard>
        <KpiCard label="容器总数" :value="stats.total" tone="cluster">
          <template #icon><BoxIcon /></template>
        </KpiCard>
        <KpiCard label="运行中" :value="stats.running" tone="ok">
          <template #icon><PlayIcon /></template>
        </KpiCard>
        <KpiCard label="已停止" :value="stats.stopped" :tone="stats.stopped > 0 ? 'down' : 'total'">
          <template #icon><StopIcon /></template>
        </KpiCard>
        <KpiCard label="总镜像数" :value="stats.totalImages" tone="mem">
          <template #icon><LayersIcon /></template>
        </KpiCard>
      </div>

      <SectionCard
        title="实例拓扑"
        subtitle="部署 Docker 的主机"
        class="mw-sec"
        :loading="loading && !stats.hosts.length"
      >
        <div class="host-grid">
          <MwHostCard
            v-for="h in hostCards"
            :key="h.node"
            :name="h.nodeIp || h.ip || h.node"
            :sub="h.node && h.node !== (h.nodeIp || h.ip) ? h.node : ''"
            :status="h.up ? 'normal' : 'abnormal'"
            :count-text="`${h.items.length} 容器`"
            :dim="!h.up"
          />
        </div>
      </SectionCard>

      <SectionCard title="容器列表" class="mw-sec" dense>
        <el-table
          class="docker-table"
          :data="pagedContainers"
          style="width: 100%"
          size="small"
          stripe
          @row-click="openDetail"
          :row-class-name="rowClass"
          @sort-change="onSortChange"
        >
          <el-table-column prop="name" label="名称" min-width="150" show-overflow-tooltip />
          <el-table-column prop="image" label="镜像" min-width="200" show-overflow-tooltip />
          <el-table-column label="所在主机" min-width="160">
            <template #default="{ row }">
              <div class="host-cell">
                <span class="host-cell-name">{{ row.hostIp || row.host }}</span>
                <span v-if="row.hostIp && row.hostIp !== row.host" class="host-cell-ip">{{ row.host }}</span>
              </div>
            </template>
          </el-table-column>
          <el-table-column label="状态" width="110">
            <template #default="{ row }">
              <MwStatusDot :status="containerStatus(row)" :label="dockerStatusText(row.status || row.state)" />
            </template>
          </el-table-column>
          <el-table-column prop="cpuPercent" label="CPU%" width="90" sortable />
          <el-table-column prop="memUsage" label="内存" width="100" sortable>
            <template #default="{ row }">{{ formatBytes(row.memUsage) }}</template>
          </el-table-column>
          <el-table-column prop="memPercent" label="内存%" width="90" sortable />
        </el-table>

        <EmptyState
          v-if="containers.length === 0"
          size="table-inline"
          title="当前主机下暂无容器"
          :hints="['确认 Agent 已配置 Docker 采集，且 Docker daemon 上有运行中的容器']"
        />

        <div v-if="containers.length > 0" class="pager">
          <el-pagination
            background
            layout="total, sizes, prev, pager, next, jumper"
            :total="sortedContainers.length"
            :page-size="pageSize"
            :current-page="currentPage"
            :page-sizes="[10, 20, 50, 100]"
            @current-change="(v) => (currentPage = v)"
            @size-change="(v) => { pageSize = v; currentPage = 1 }"
          />
        </div>
      </SectionCard>
    </template>

    <el-drawer v-model="drawerVisible" title="容器详情" size="50%" :destroy-on-close="true" @opened="loadTrendChart">
      <div v-if="selected" class="detail-content">
        <MwDetailHeader
          :title="selected.name || selected.instance"
          :subtitle="selected.instance"
          :status="containerStatus(selected)"
          :status-label="dockerStatusText(selected.status || selected.state)"
          :fields="detailFields"
          v-model:range="range"
        />

        <div class="metric-grid">
          <div class="metric-cell"><div class="mc-label">CPU</div><div class="mc-value">{{ (selected.cpuPercent ?? 0).toFixed(1) }}%</div></div>
          <div class="metric-cell"><div class="mc-label">内存</div><div class="mc-value">{{ formatBytes(selected.memUsage) }}</div></div>
          <div class="metric-cell"><div class="mc-label">内存%</div><div class="mc-value">{{ (selected.memPercent ?? 0).toFixed(1) }}%</div></div>
          <div class="metric-cell"><div class="mc-label">PID</div><div class="mc-value">{{ selected.pidsCurrent ?? '-' }}</div></div>
          <div class="metric-cell"><div class="mc-label">网络RX</div><div class="mc-value">{{ formatBytes(selected.netRx) }}</div></div>
          <div class="metric-cell"><div class="mc-label">网络TX</div><div class="mc-value">{{ formatBytes(selected.netTx) }}</div></div>
          <div class="metric-cell"><div class="mc-label">磁盘读</div><div class="mc-value">{{ formatBytes(selected.diskRead) }}</div></div>
          <div class="metric-cell"><div class="mc-label">磁盘写</div><div class="mc-value">{{ formatBytes(selected.diskWrite) }}</div></div>
        </div>

        <div class="trend-block">
          <div class="trend-title">CPU 使用率</div>
          <TrendChart
            :option="trendOption"
            :loading="trendLoading"
            :error="trendError"
            :height="300"
            @retry="loadTrendChart"
          />
        </div>
      </div>
    </el-drawer>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted, h } from 'vue'
import http from '../../api/http'
import RefreshBar from '../RefreshBar.vue'
import KpiCard from '../KpiCard.vue'
import SectionCard from '../common/SectionCard.vue'
import EmptyState from '../common/EmptyState.vue'
import MwStatusDot from '../mw/MwStatusDot.vue'
import MwHostCard from '../mw/MwHostCard.vue'
import MwDetailHeader from '../mw/MwDetailHeader.vue'
import TrendChart from '../mw/TrendChart.vue'
import { mwTrendOption } from '../../charts/echarts'
import { rangeToWindow, rangeToStep } from '../mw/mwRange'
import dockerLogo from '../../assets/img/docker.svg'

// ---- 图标组件（内联 SVG 渲染函数） ----
function svgIcon(s) {
  const inner = s.replace(/^<svg[^>]*>/, '').replace(/<\/svg>\s*$/, '')
  return () => h('svg', { viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', 'stroke-width': 2, innerHTML: inner })
}
const ServerIcon = svgIcon('<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="2" y="3" width="20" height="8" rx="2"/><rect x="2" y="13" width="20" height="8" rx="2"/><line x1="6" y1="7" x2="6.01" y2="7"/><line x1="6" y1="17" x2="6.01" y2="17"/></svg>')
const CheckCircleIcon = svgIcon('<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><polyline points="9 12 12 15 16 10"/></svg>')
const BoxIcon = svgIcon('<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"/><polyline points="3.27 6.96 12 12.01 20.73 6.96"/><line x1="12" y1="22.08" x2="12" y2="12"/></svg>')
const PlayIcon = svgIcon('<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polygon points="5 3 19 12 5 21 5 3"/></svg>')
const StopIcon = svgIcon('<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="6" y="6" width="12" height="12" rx="2"/></svg>')
const LayersIcon = svgIcon('<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polygon points="12 2 2 7 12 12 22 7 12 2"/><polyline points="2 17 12 22 22 17"/><polyline points="2 12 12 17 22 12"/></svg>')

const loading = ref(true)
const error = ref('')
const stats = ref({ hosts: [] })
const drawerVisible = ref(false)
const selected = ref(null)

const statusTextMap = {
  running: '运行中',
  paused: '已暂停',
  exited: '已停止',
  restarting: '重启中',
  dead: '异常',
  created: '已创建',
}
function dockerStatusText(state) { return statusTextMap[state] || state || '-' }

// 容器 6 态 → 三态语义色。此前 `up ? normal : abnormal` 把 paused/restarting/dead
// 全压成一档：'重启中'（可自愈）与'已停止/异常'（需介入）看起来一样，严重度被低估。
const WARN_STATES = ['paused', 'restarting', 'created']
function containerStatus(c) {
  const s = String((c && (c.status || c.state)) || '').trim().toLowerCase()
  if (s === 'running') return 'normal'
  if (WARN_STATES.includes(s)) return 'warning'
  if (s) return 'abnormal'
  return c && c.up ? 'normal' : 'abnormal'
}

// 主机 → 其容器（把 host/hostIp 补进容器行，供抽屉与趋势查询使用）
const hostCards = computed(() =>
  (stats.value.hosts || []).map((h) => ({
    ...h,
    items: (h.containers || []).map((c) => ({ ...c, host: h.node, hostIp: h.ip })),
  }))
)
const containers = computed(() => hostCards.value.flatMap((h) => h.items))

const detailFields = computed(() => {
  const s = selected.value
  if (!s) return []
  return [
    { label: '主机', value: s.hostIp || s.host, mono: true },
    { label: '状态', value: dockerStatusText(s.status || s.state) },
    { label: '镜像', value: s.image },
  ]
})

async function load() {
  loading.value = true
  error.value = ''
  try {
    const data = await http.get('/api/v1/middleware/docker/containers')
    const rawHosts = data.hosts || []
    const rawContainers = data.containers || []
    // 后端 containers 为按 node 的扁平列表，归并到对应主机以渲染容器列表
    const byNode = {}
    for (const h of rawHosts) { h.containers = []; byNode[h.node] = h }
    for (const c of rawContainers) {
      const h = byNode[c.node]
      if (h) h.containers.push(c)
    }
    stats.value = { hosts: rawHosts, total: 0, running: 0, stopped: 0, totalImages: 0, upHosts: 0 }
    let total = 0, running = 0, stopped = 0, images = 0, up = 0
    for (const h of rawHosts) {
      // B11：此前对每台主机无条件 up++，导致"在线主机"永远等于"主机总数"。
      // 后端 dockerHostInfo 有真实 up 字段（daemon 上报 docker_containers_total 才为 true）。
      if (h.up) up++
      images += h.imagesTotal || 0
      total += h.containersTotal || 0
      running += h.containersRunning || 0
      stopped += h.containersStopped || 0
    }
    stats.value.total = total
    stats.value.running = running
    stats.value.stopped = stopped
    stats.value.totalImages = images
    stats.value.upHosts = up
  } catch (e) {
    console.error(e)
    error.value = 'Docker 数据加载失败，请检查 Server 与 Agent 连接'
  } finally {
    loading.value = false
  }
}

function openDetail(row) {
  selected.value = row
  drawerVisible.value = true
}

// ---- 抽屉趋势图（统一走 TrendChart：三态 + 关闭即销毁） ----
const range = ref('1h')
const trendOption = ref(null)
const trendLoading = ref(false)
const trendError = ref('')

async function loadTrendChart() {
  const row = selected.value
  if (!row) return
  trendLoading.value = true
  trendError.value = ''
  const { start, end } = rangeToWindow(range.value)
  const step = rangeToStep(range.value)
  try {
    const data = await http.get(
      `/api/v1/query/range?node=${encodeURIComponent(row.host)}&metric=docker_container_cpu_percent&start=${start}&end=${end}&step=${step}`
    )
    const series = []
    for (const s of data.series || []) {
      if (s.labels?.instance === row.instance || s.labels?.container_name === row.name) {
        series.push({ name: 'CPU%', data: (s.points || []).map((p) => [p.timestamp, p.value]) })
      }
    }
    trendOption.value = mwTrendOption(series, { unit: '%', yMin: 0, yMax: 100 })
  } catch (e) {
    console.error(e)
    trendError.value = '趋势数据加载失败'
    trendOption.value = null
  } finally {
    trendLoading.value = false
  }
}

// 切换时间范围后重查（仅在抽屉打开时）
watch(range, () => { if (drawerVisible.value) loadTrendChart() })

function formatBytes(b) {
  if (!b && b !== 0) return '-'
  const u = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  let v = b
  while (v >= 1024 && i < u.length - 1) { v /= 1024; i++ }
  return v.toFixed(1) + ' ' + u[i]
}
function rowClass({ row }) { return row.up ? '' : 'row-down' }

const currentPage = ref(1)
const pageSize = ref(10)
const sortState = ref({ prop: '', order: '' })
function onSortChange({ prop, order }) { sortState.value = { prop, order }; currentPage.value = 1 }
const sortedContainers = computed(() => {
  const arr = [...containers.value]
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
const pagedContainers = computed(() => sortedContainers.value.slice((currentPage.value - 1) * pageSize.value, currentPage.value * pageSize.value))
watch(containers, () => { currentPage.value = 1 })

onMounted(load)
</script>

<style scoped>
.mw-tab { padding: 4px 0; }
.mw-err { margin-bottom: 16px; }
.mw-err-text { margin-right: 8px; }
.mw-sec { margin-bottom: 16px; }
.kpi-row { display: grid; grid-template-columns: repeat(auto-fit, minmax(160px, 1fr)); gap: 12px; margin-bottom: 16px; }
.host-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(280px, 1fr)); gap: 12px; }
.host-cell { display: flex; flex-direction: column; }
.host-cell-name { font-size: var(--fs-sm); }
.host-cell-ip { font-size: var(--fs-xs); color: var(--t3); font-family: var(--mono); }
.pager { display: flex; justify-content: flex-end; margin-top: 12px; }
.docker-table :deep(.el-table__row) { cursor: pointer; }
:deep(.row-down) { opacity: 0.6; }
.detail-content { padding: 0 20px; }
.metric-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(140px, 1fr)); gap: 12px; margin-bottom: 24px; }
.metric-cell { padding: 12px; background: var(--fill-1); border-radius: var(--r-md); text-align: center; }
.mc-label { font-size: var(--fs-sm); color: var(--t3); margin-bottom: 4px; }
.mc-value { font-size: var(--fs-lg); font-weight: 600; font-family: var(--mono); }
.trend-block { margin-bottom: 18px; }
.trend-title { font-size: var(--fs-sm); color: var(--t3); margin-bottom: 6px; }
</style>
