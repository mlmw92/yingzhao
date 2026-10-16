<template>
  <div class="mw-tab">
    <div class="mw-head">
      <RefreshBar :loading="loading" @refresh="load" />
      <!-- 本页只做**指标展示**（由 Agent 采集、进时序库的聚合计数与用量，粒度止于集群/节点）；
           对象级明细与操作（工作负载 / Pod / 事件 / 详情 / 日志，以及排障写操作）都在
           「容器与工作负载」那一页。这是 2026-10-10 重规划刻意切开的两个信息面。 -->
      <el-button size="small" @click="openConsole">进入容器与工作负载</el-button>
    </div>

    <el-alert v-if="error" type="error" :closable="false" show-icon class="mw-err">
      <template #title>
        <span class="mw-err-text">{{ error }}</span>
        <el-button size="small" link type="primary" @click="load">重试</el-button>
      </template>
    </el-alert>

    <!-- 未配置：全平台没有任何 K8s 集群上报 -->
    <EmptyState
      v-if="!loading && !error && clusters.length === 0"
      :icon="EmptyIcon"
      title="尚未配置 Kubernetes 监控"
      :hints="[
        '请在能访问 apiserver 的节点 agent.yaml 中开启 collectors.k8s 并配置 k8sInstances（kubeconfig 或 apiServer+token）',
        '配置完成后约 15-30 秒，本页将出现集群卡片',
      ]"
    />

    <template v-if="clusters.length > 0">
      <!-- KPI -->
      <div class="kpi-row">
        <KpiCard :value="stats.clusters" label="集群数" tone="cluster">
          <template #icon><ClusterIcon /></template>
        </KpiCard>
        <KpiCard :value="stats.nodesReady + '/' + stats.nodesTotal" label="节点就绪/总数" tone="host">
          <template #icon><ServerIcon /></template>
        </KpiCard>
        <KpiCard :value="stats.podsRunning" label="运行 Pod" tone="up">
          <template #icon><PodIcon /></template>
        </KpiCard>
        <KpiCard :value="stats.podsTotal" label="Pod 总数" tone="total">
          <template #icon><BoxIcon /></template>
        </KpiCard>
        <!-- 「异常 Pod」计数留在这里（它是指标），明细去管理面看——因此这张卡可点。
             计数与明细分属两个信息面：本页回答"有多少"，管理面回答"是哪个"。 -->
        <div
          class="kpi-clickable"
          :title="'查看异常 Pod 明细：跳到「容器与工作负载」（按容器级有效状态判定的明细在那里）'"
          @click="openConsole"
        >
          <KpiCard :value="stats.podsAbnormal" label="异常 Pod" :tone="stats.podsAbnormal > 0 ? 'down' : 'total'">
            <template #icon><AlertIcon /></template>
          </KpiCard>
        </div>
        <KpiCard :value="stats.workloadsUnhealthy" label="不健康工作负载" :tone="stats.workloadsUnhealthy > 0 ? 'alert' : 'total'">
          <template #icon><ActivityIcon /></template>
        </KpiCard>
      </div>

      <!-- 集群卡片 -->
      <SectionCard title="集群" class="mw-block">
        <div class="host-grid">
          <MwHostCard
            v-for="c in clusters"
            :key="c.node + '|' + c.instance"
            :name="c.name || c.instance"
            :sub="c.instance"
            :status="c.up ? 'normal' : 'abnormal'"
            :count-text="c.version || ''"
            :dim="!c.up"
          >
            <div class="host-stats">
              <div class="stat-item">
                <span class="stat-label">节点</span>
                <span class="stat-val" :class="c.nodesReady < c.nodesTotal ? 'warn' : 'ok'">{{ c.nodesReady }}/{{ c.nodesTotal }}</span>
              </div>
              <div class="stat-item">
                <span class="stat-label">Pod</span>
                <span class="stat-val">{{ c.podsRunning }}/{{ c.podsTotal }}</span>
              </div>
              <!-- 用 podsAbnormal（按容器级有效状态算）而不是 pending+failed：
                   phase=Running 但容器在 CrashLoopBackOff 的 Pod 也必须算进异常。
                   服务端已对旧 Agent 做过口径回退，这里恒有值。 -->
              <div class="stat-item" v-if="c.podsAbnormal > 0">
                <span class="stat-label">异常</span>
                <span class="stat-val warn">{{ c.podsAbnormal }}</span>
              </div>
              <div class="stat-item" v-if="workloadUnhealthy(c) > 0">
                <span class="stat-label">负载异常</span>
                <span class="stat-val warn">{{ workloadUnhealthy(c) }}</span>
              </div>
            </div>
          </MwHostCard>
        </div>
      </SectionCard>

      <!-- 节点列表 -->
      <SectionCard title="节点列表" dense class="mw-block">
        <el-table :data="nodes" style="width: 100%" empty-text="暂无节点数据">
          <el-table-column prop="nodeName" label="节点名" min-width="180" show-overflow-tooltip />
          <el-table-column prop="ip" label="IP" min-width="140" show-overflow-tooltip />
          <el-table-column prop="cluster" label="集群" width="140" />
          <!-- 150 而不是 120：'Control-Plane' 标签连内边距约需 116px，再加单元格左右内边距，
               120px 的列里会被裁掉一截——这就是"角色显示不全"。 -->
          <el-table-column label="角色" width="150">
            <template #default="{ row }"><MwRoleTag :role="row.role" /></template>
          </el-table-column>
          <el-table-column label="状态" width="100">
            <template #default="{ row }"><MwStatusDot :status="row.ready ? 'normal' : 'abnormal'" :label="row.ready ? '就绪' : '未就绪'" /></template>
          </el-table-column>
          <el-table-column label="CPU 用量" width="110" sortable :sort-by="'cpuCores'">
            <template #default="{ row }">{{ row.cpuCores ? row.cpuCores.toFixed(2) + ' 核' : '-' }}</template>
          </el-table-column>
          <el-table-column label="内存用量" width="110" sortable :sort-by="'memBytes'">
            <template #default="{ row }">{{ formatBytes(row.memBytes) }}</template>
          </el-table-column>
        </el-table>
      </SectionCard>

      <!-- 异常 Pod 明细**刻意不在这里**：它含对象标识（Pod 名 / 命名空间 / 原因），
           属于「容器与工作负载」那个信息面（本页只留计数，见上面的 KPI 卡）。
           这里说明明细去哪了——否则用户会以为"功能没了"。 -->
      <div v-if="pods.length > 0" class="mw-moved">
        有 {{ pods.length }} 个异常 Pod。明细（Pod 名 / 命名空间 / 状态）已移到
        <el-button link type="primary" size="small" @click="openConsole">容器与工作负载</el-button>
        页——那里可以逐条查看、看日志，并由超管执行删除等排障操作。
      </div>
    </template>
  </div>
</template>

<script setup>
import { ref, onMounted, computed, h } from 'vue'
import { useRouter } from 'vue-router'
import http from '../../api/http'
import RefreshBar from '../RefreshBar.vue'
import KpiCard from '../KpiCard.vue'
import SectionCard from '../common/SectionCard.vue'
import EmptyState from '../common/EmptyState.vue'
import MwStatusDot from '../mw/MwStatusDot.vue'
import MwRoleTag from '../mw/MwRoleTag.vue'
import MwHostCard from '../mw/MwHostCard.vue'

// ---- 图标组件（内联 SVG 渲染函数） ----
function svgIcon(s) {
  const inner = s.replace(/^<svg[^>]*>/, '').replace(/<\/svg>\s*$/, '')
  return () => h('svg', { viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', 'stroke-width': 2, innerHTML: inner })
}
const ClusterIcon = svgIcon('<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="3"/><circle cx="5" cy="5" r="2"/><circle cx="19" cy="5" r="2"/><circle cx="5" cy="19" r="2"/><circle cx="19" cy="19" r="2"/><line x1="6.5" y1="6.5" x2="9.5" y2="9.5"/><line x1="14.5" y1="9.5" x2="17.5" y2="6.5"/><line x1="6.5" y1="17.5" x2="9.5" y2="14.5"/><line x1="14.5" y1="14.5" x2="17.5" y2="17.5"/></svg>')
const ServerIcon = svgIcon('<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="2" y="3" width="20" height="8" rx="2"/><rect x="2" y="13" width="20" height="8" rx="2"/><line x1="6" y1="7" x2="6.01" y2="7"/><line x1="6" y1="17" x2="6.01" y2="17"/></svg>')
const PodIcon = svgIcon('<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 2 3 7v10l9 5 9-5V7z"/><path d="M12 22V12M3 7l9 5 9-5"/></svg>')
const BoxIcon = svgIcon('<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"/><polyline points="3.27 6.96 12 12.01 20.73 6.96"/><line x1="12" y1="22.08" x2="12" y2="12"/></svg>')
const AlertIcon = svgIcon('<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>')
const ActivityIcon = svgIcon('<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="22 12 18 12 15 21 9 3 6 12 2 12"/></svg>')

// 空状态图标：保留原有"六边形/Pod"造型
const EmptyIcon = () =>
  h('svg', {
    viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', 'stroke-width': 1.5,
    innerHTML: '<path d="M12 2 3 7v10l9 5 9-5V7z"/><path d="M12 22V12M3 7l9 5 9-5"/>',
  })

const loading = ref(true)
const clusters = ref([])
const nodes = ref([])
const pods = ref([])
const error = ref('')

const stats = computed(() => {
  const s = { clusters: 0, nodesTotal: 0, nodesReady: 0, podsTotal: 0, podsRunning: 0, podsAbnormal: 0, workloadsUnhealthy: 0 }
  for (const c of clusters.value) {
    s.clusters++
    s.nodesTotal += c.nodesTotal || 0
    s.nodesReady += c.nodesReady || 0
    s.podsTotal += c.podsTotal || 0
    s.podsRunning += c.podsRunning || 0
    // 口径：用服务端算好的 podsAbnormal（按容器级有效状态），不再是 pending+failed。
    // 后者漏掉"phase 是 Running、但容器在 CrashLoopBackOff"的 Pod——那正是最该被看到的。
    // 用 ?? 而不是 ||：0 是合法值；只有字段根本不存在（新旧版本错配）才回退旧口径。
    s.podsAbnormal += c.podsAbnormal ?? ((c.podsPending || 0) + (c.podsFailed || 0))
    s.workloadsUnhealthy += workloadUnhealthy(c)
  }
  return s
})

function workloadUnhealthy(c) {
  return (c.deploymentsUnhealthy || 0) + (c.statefulSetsUnhealthy || 0) + (c.daemonSetsUnhealthy || 0)
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const data = await http.get('/api/v1/middleware/k8s/instances')
    clusters.value = data.clusters || []
    nodes.value = data.nodes || []
    pods.value = data.pods || []
  } catch (e) {
    console.error('加载 Kubernetes 数据失败', e)
    error.value = 'Kubernetes 数据加载失败'
  } finally {
    loading.value = false
  }
}

function formatBytes(n) {
  if (!n) return '-'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let v = n, i = 0
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++ }
  return v.toFixed(1) + ' ' + units[i]
}

const router = useRouter()
// openConsole 跳到管理面：本页只做**指标展示**，对象级查看与操作都在那边
// （见 docs/superpowers/specs/2026-10-10-container-workload-management-design.md §一）。
// 带 platform=k8s 让它直接落在 Kubernetes 平台（容器页支持该深链）。
function openConsole() {
  router.push({ path: '/container', query: { platform: 'k8s' } })
}

onMounted(load)
</script>

<style scoped>
.mw-tab { padding: 4px 0; }
.mw-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 12px; }
.kpi-clickable { cursor: pointer; }
.mw-moved { font-size: var(--fs-base); color: var(--t3); margin-bottom: 16px; }
.mw-err { margin-bottom: 16px; }
.mw-err-text { margin-right: 8px; }
.kpi-row { display: grid; grid-template-columns: repeat(auto-fit, minmax(140px, 1fr)); gap: 10px; margin-bottom: 16px; }
.mw-block { margin-bottom: 16px; }
.host-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(280px, 1fr)); gap: 12px; }
.host-stats { display: flex; flex-wrap: wrap; gap: 12px; }
.stat-item { display: flex; flex-direction: column; gap: 2px; }
.stat-label { font-size: var(--fs-xs); color: var(--t3); }
.stat-val { font-size: var(--fs-base); font-weight: 600; font-family: var(--mono); }
.stat-val.ok { color: var(--ok); }
.stat-val.warn { color: var(--warn); }
</style>
