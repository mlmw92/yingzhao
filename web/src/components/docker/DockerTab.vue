<template>
  <div class="mw-tab">
    <div class="mw-head">
      <RefreshBar :loading="loading" @refresh="load" />
      <!-- 本页只做**指标展示**（由 Agent 采集、进时序库的聚合计数，粒度止于主机）；
           容器级明细与操作（列表 / 详情 / 日志 / 启停删除）都在「容器与工作负载」那一页。
           这是 2026-10-10 重规划刻意切开的两个信息面。 -->
      <el-button size="small" @click="openConsole">进入容器与工作负载</el-button>
    </div>

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
        :loading="loading && !hostCards.length"
      >
        <div class="host-grid">
          <MwHostCard
            v-for="h in hostCards"
            :key="h.node"
            :name="h.nodeIp || h.ip || h.node"
            :sub="h.node && h.node !== (h.nodeIp || h.ip) ? h.node : ''"
            :status="h.up ? 'normal' : 'abnormal'"
            :count-text="`${h.containersTotal || 0} 容器`"
            :dim="!h.up"
          />
        </div>
      </SectionCard>

      <!-- 容器明细**刻意不在这里**：它含对象标识（容器名 / 镜像 / 端口 / 资源占用），
           属于「容器与工作负载」那个信息面（本页只留按主机的聚合计数）。
           这里说明明细去哪了——否则用户会以为"功能没了"。 -->
      <div class="mw-moved">
        容器明细（名称 / 镜像 / 端口 / 资源占用）与启停、删除等操作已移到
        <el-button link type="primary" size="small" @click="openConsole">容器与工作负载</el-button>
        页的 Docker 平台。
      </div>
    </template>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, h } from 'vue'
import { useRouter } from 'vue-router'
import http from '../../api/http'
import RefreshBar from '../RefreshBar.vue'
import KpiCard from '../KpiCard.vue'
import SectionCard from '../common/SectionCard.vue'
import EmptyState from '../common/EmptyState.vue'
import MwHostCard from '../mw/MwHostCard.vue'
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

// 主机卡片只呈现**聚合计数**（容器数 / 镜像数 / 在线）：容器级明细在管理面，
// 因此这里不再把 data.containers 归并进主机（那是容器列表才需要的东西）。
const hostCards = computed(() => stats.value.hosts || [])

async function load() {
  loading.value = true
  error.value = ''
  try {
    const data = await http.get('/api/v1/middleware/docker/containers')
    const rawHosts = data.hosts || []
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

const router = useRouter()
// openConsole 跳到管理面：本页只做**指标展示**，容器级查看与操作都在那边
// （见 docs/superpowers/specs/2026-10-10-container-workload-management-design.md §一）。
// 带 platform=docker 让它直接落在 Docker 平台（容器页支持该深链）。
function openConsole() {
  router.push({ path: '/container', query: { platform: 'docker' } })
}

onMounted(load)
</script>

<style scoped>
.mw-tab { padding: 4px 0; }
.mw-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 12px; }
.mw-err { margin-bottom: 16px; }
.mw-err-text { margin-right: 8px; }
.mw-moved { font-size: var(--fs-base); color: var(--t3); margin-bottom: 16px; }
.mw-sec { margin-bottom: 16px; }
.kpi-row { display: grid; grid-template-columns: repeat(auto-fit, minmax(160px, 1fr)); gap: 12px; margin-bottom: 16px; }
.host-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(280px, 1fr)); gap: 12px; }
</style>
