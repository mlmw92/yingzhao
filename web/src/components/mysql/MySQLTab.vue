<template>
  <div class="mw-tab">
    <RefreshBar :loading="loading" @refresh="load" />

    <el-alert v-if="error" type="error" :closable="false" show-icon class="mw-err">
      <template #title>
        <span class="mw-err-text">{{ error }}</span>
        <el-button size="small" link type="primary" @click="load">重试</el-button>
      </template>
    </el-alert>

    <!-- 空状态 -->
    <EmptyState
      v-if="!loading && !error && instances.length === 0"
      :icon="EmptyIcon"
      title="尚未配置 MySQL 监控"
      :hints="[
        '当前没有已采集的 MySQL 实例，请在运行 Agent 的节点上配置 MySQL 实例连接信息',
        '配置完成后约 15-30 秒数据将出现在此页面',
      ]"
    />

    <template v-if="instances.length > 0">
      <!-- KPI 概览卡片：与 Redis 统一使用 KpiCard 组件 -->
      <div class="kpi-row">
        <KpiCard :value="stats.total" label="实例总数" tone="total">
          <template #icon><el-icon :size="20"><Grid /></el-icon></template>
        </KpiCard>
        <KpiCard :value="stats.up" label="在线实例" tone="up">
          <template #icon><el-icon :size="20"><CircleCheck /></el-icon></template>
        </KpiCard>
        <KpiCard :value="stats.down" label="离线实例" :tone="stats.down > 0 ? 'down' : 'total'">
          <template #icon><el-icon :size="20"><CircleClose /></el-icon></template>
        </KpiCard>
        <KpiCard :value="formatNum(stats.totalConnections)" label="总连接数" tone="conn">
          <template #icon><el-icon :size="20"><Connection /></el-icon></template>
        </KpiCard>
        <KpiCard :value="formatNum(stats.totalQPS)" label="总 QPS" tone="ops">
          <template #icon><el-icon :size="20"><DataLine /></el-icon></template>
        </KpiCard>
        <KpiCard :value="stats.totalSlowQueries" label="慢查询累计" tone="total">
          <template #icon><el-icon :size="20"><Bell /></el-icon></template>
        </KpiCard>
      </div>

      <!-- 实例拓扑：与 Redis 对齐的主从/集群关系视图 -->
      <SectionCard title="实例拓扑" class="mw-block">
        <!-- 集群组（Group Replication / InnoDB Cluster，多节点多主） -->
        <template v-if="topologyGroups.clusters.length">
          <div v-for="grp in topologyGroups.clusters" :key="'c-'+grp.name" class="topo-group">
            <div class="topo-group-header">
              <span class="topo-group-title">
                <el-icon :size="18"><Connection /></el-icon>
                <strong>集群 {{ grp.name || '未命名' }}</strong>
              </span>
              <span class="topo-meta">
                <span class="badge" :class="clusterHealthClass(grp)" :title="clusterFaultDetail(grp)">{{ clusterHealthText(grp) }}</span>
                <span v-if="clusterModeText(grp)" class="dim">{{ clusterModeText(grp) }}</span>
                <span class="dim">节点 {{ (grp.masters.length + grp.slaves.length) || grp.nodes.length }}</span>
              </span>
            </div>
            <div class="topo-grid">
              <div v-for="i in grp.nodes" :key="'cn-'+i.instance"
                   class="rel-node rel-standalone" :class="{ 'is-down': !i.up }" @click="openDetail(i)">
                <div class="rel-node-name" :title="i.instance">
                  <span class="role-badge" :class="isPrimary(i) ? 'role-badge-m' : 'role-badge-s'">{{ isPrimary(i) ? 'P' : 'S' }}</span>
                  <span class="mono">{{ i.instance }}</span>
                </div>
                <div class="rel-node-meta">
                  <MwStatusDot :status="i.up ? 'normal' : 'abnormal'" :label="i.up ? '在线' : '离线'" />
                  <span class="dim">·</span>
                  <span>{{ nodeRoleText(i) }}</span>
                </div>
                <div v-if="i.up" class="rel-node-meta">
                  <span>{{ formatNum(i.threadsConnected) }} 连接</span>
                  <span class="dim">·</span>
                  <span>{{ formatNum(i.queriesPerSec) }} QPS</span>
                </div>
              </div>
            </div>
            <div class="topo-legend">
              <span class="topo-legend-item">Group Replication：数据在组内多节点间同步，PRIMARY 故障时由组自动选举新主</span>
            </div>
          </div>
        </template>

        <!-- 主从组 -->
        <template v-if="topologyGroups.replications.length">
          <div v-for="grp in topologyGroups.replications" :key="'r-'+grp.name" class="topo-group">
            <div class="topo-group-header">
              <span class="topo-group-title">
                <el-icon :size="18"><Connection /></el-icon>
                <strong>主从 {{ grp.name }}</strong>
              </span>
              <span class="topo-meta">
                <span class="badge" :class="clusterHealthClass(grp)">{{ clusterHealthText(grp) }}</span>
                <span class="dim">主库: {{ grp.masters.length }}</span>
                <span class="dim">· 从库: {{ grp.slaves.length }}</span>
              </span>
            </div>
            <div class="ms-tree">
              <div v-for="(m, idx) in grp.masters" :key="'rm-'+idx" class="ms-unit">
                <div class="rel-node rel-master ms-master" :class="{ 'is-down': !m.up }" @click="openDetail(m)">
                  <div class="rel-node-name" :title="m.instance">
                    <span class="role-badge role-badge-m">M</span>
                    {{ m.name || m.instance }}
                  </div>
                  <div class="rel-node-meta">
                    <MwStatusDot :status="m.up ? 'normal' : 'abnormal'" :label="m.up ? '在线' : '离线'" />
                    <span class="dim">·</span>
                    <span>{{ formatNum(m.threadsConnected) }} 连接</span>
                    <span class="dim">·</span>
                    <span>{{ formatNum(m.queriesPerSec) }} QPS</span>
                  </div>
                </div>
                <div v-if="grp.slavesByMaster[m.instance].length" class="ms-branch">
                  <div class="ms-branch-rail">
                    <span class="ms-rail-repl" title="数据复制方向：主库将 binlog 同步给从库">复制 ↓</span>
                    <span class="ms-rail-fo" title="主库宕机时对应从库提升为新主库">故障转移 ↑</span>
                  </div>
                  <div class="ms-slaves">
                    <div v-for="s in grp.slavesByMaster[m.instance]" :key="'rs-'+s.instance"
                         class="rel-node rel-slave ms-slave-card" :class="{ 'is-down': !s.up }" @click.stop="openDetail(s)">
                      <div class="ms-slave-head">
                        <span class="role-badge role-badge-s">S</span>
                        <span class="mono" :title="s.instance">{{ s.name || s.instance }}</span>
                      </div>
                      <div class="ms-slave-meta">
                        <MwStatusDot :status="s.up ? 'normal' : 'abnormal'" :label="s.up ? '在线' : '离线'" />
                        <span class="dim">·</span>
                        <span>{{ s.up ? formatNum(s.queriesPerSec) + ' QPS' : '离线' }}</span>
                        <span v-if="s.up && s.secondsBehindMaster != null" class="dim">· 延迟 {{ s.secondsBehindMaster }}s</span>
                      </div>
                    </div>
                  </div>
                </div>
              </div>
            </div>
            <div class="topo-legend">
              <span class="topo-legend-item"><span class="legend-line legend-solid"></span>数据复制（master → slave）</span>
              <span class="topo-legend-item"><span class="legend-line legend-dash"></span>故障转移（slave 升主，slave → master）</span>
            </div>
            <!-- 未关联主节点的从库（replicaOf 为空，常见于 agent 未升级二进制） -->
            <div v-if="grp.unlinkedSlaves.length" class="unlinked-block">
              <div class="unlinked-label">
                <span class="role-badge role-badge-s">S</span>
                未关联主节点的从库（{{ grp.unlinkedSlaves.length }} 个）— agent 升级后将自动关联
              </div>
              <div class="unlinked-list">
                <div v-for="s in grp.unlinkedSlaves" :key="'ul-'+s.instance"
                     class="rel-node rel-slave ms-slave-card" :class="{ 'is-down': !s.up }" @click.stop="openDetail(s)">
                  <div class="ms-slave-head">
                    <span class="role-badge role-badge-s">S</span>
                    <span class="mono" :title="s.instance">{{ s.name || s.instance }}</span>
                  </div>
                  <div class="ms-slave-meta">
                    <MwStatusDot :status="s.up ? 'normal' : 'abnormal'" :label="s.up ? '在线' : '离线'" />
                  </div>
                </div>
              </div>
            </div>
          </div>
        </template>

        <!-- 独立实例 -->
        <template v-if="topologyGroups.standalones.length">
          <div class="topo-group">
            <div class="topo-group-header">
              <span class="topo-group-title">
                <el-icon :size="18"><Grid /></el-icon>
                <strong>独立实例</strong>
              </span>
              <span class="dim">共 {{ topologyGroups.standalones.length }} 个</span>
            </div>
            <div class="topo-grid">
              <div v-for="i in topologyGroups.standalones" :key="'sa-'+i.instance"
                   class="rel-node rel-standalone" :class="{ 'is-down': !i.up }" @click="openDetail(i)">
                <div class="rel-node-name" :title="i.instance">{{ i.name || i.instance }}</div>
                <div class="rel-node-meta">
                  <MwStatusDot :status="i.up ? 'normal' : 'abnormal'" :label="i.up ? '在线' : '离线'" />
                  <span class="dim">·</span>
                  <span>{{ i.instance }}</span>
                </div>
                <div class="rel-node-meta">
                  <span>{{ formatNum(i.threadsConnected) }} 连接</span>
                  <span class="dim">·</span>
                  <span>{{ formatNum(i.queriesPerSec) }} QPS</span>
                </div>
              </div>
            </div>
          </div>
        </template>
      </SectionCard>

      <!-- 实例列表 -->
      <SectionCard title="实例列表" dense class="mw-block">
        <el-table :data="pagedInstances" class="mysql-table" style="width: 100%" @row-click="openDetail" :row-class-name="rowClass" size="small" stripe @sort-change="onSortChange">
          <el-table-column prop="instance" label="实例地址" min-width="180" show-overflow-tooltip />
          <el-table-column prop="name" label="名称" min-width="130" show-overflow-tooltip>
            <template #header>
              <el-tooltip content="Agent 配置中指定的实例别名/名称；未配置时为空" placement="top">
                <span>名称 <el-icon :size="12" style="vertical-align: middle; margin-left: 2px;"><QuestionFilled /></el-icon></span>
              </el-tooltip>
            </template>
            <template #default="{ row }">
              <span class="text-muted">{{ row.name || '-' }}</span>
            </template>
          </el-table-column>
          <el-table-column prop="role" label="角色" min-width="90">
            <template #default="{ row }">
              <MwRoleTag :role="row.role" />
            </template>
          </el-table-column>
          <el-table-column prop="version" label="版本" min-width="140" show-overflow-tooltip />
          <el-table-column label="状态" min-width="100">
            <template #default="{ row }">
              <MwStatusDot :status="row.up ? 'normal' : 'abnormal'" :label="row.up ? '正常' : '离线'" />
            </template>
          </el-table-column>
          <el-table-column prop="threadsConnected" label="连接数" min-width="110" sortable />
          <el-table-column prop="queriesPerSec" label="QPS" min-width="100" sortable />
          <el-table-column prop="bufferPoolHitRate" label="缓冲命中率" min-width="150" sortable>
            <template #default="{ row }">
              <span :class="hitRateClass(row.bufferPoolHitRate)">{{ row.bufferPoolHitRate ? row.bufferPoolHitRate.toFixed(1) + '%' : '-' }}</span>
            </template>
          </el-table-column>
          <el-table-column prop="secondsBehindMaster" label="复制延迟(s)" min-width="150" sortable />
          <el-table-column prop="uptime" label="运行时长" min-width="120">
            <template #default="{ row }">{{ formatUptime(row.uptime) }}</template>
          </el-table-column>
        </el-table>
        <div class="pager">
          <el-pagination background layout="total, sizes, prev, pager, next, jumper" :total="sortedInstances.length" :page-size="pageSize" :current-page="currentPage" :page-sizes="[10,20,50,100]" @current-change="v => currentPage = v" @size-change="v => { pageSize = v; currentPage = 1 }" />
        </div>
      </SectionCard>
    </template>

    <!-- 详情抽屉 -->
    <el-drawer v-model="drawerVisible" title="MySQL 实例详情" size="50%" :destroy-on-close="true">
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
          <div class="metric-cell"><div class="mc-label">连接数</div><div class="mc-value">{{ selected.threadsConnected }}</div></div>
          <div class="metric-cell"><div class="mc-label">活跃连接</div><div class="mc-value">{{ selected.threadsRunning }}</div></div>
          <div class="metric-cell"><div class="mc-label">最大连接</div><div class="mc-value">{{ selected.maxConnections }}</div></div>
          <div class="metric-cell"><div class="mc-label">QPS</div><div class="mc-value">{{ formatNum(selected.queriesPerSec) }}</div></div>
          <div class="metric-cell"><div class="mc-label">慢查询</div><div class="mc-value">{{ selected.slowQueries }}</div></div>
          <div class="metric-cell"><div class="mc-label">缓冲命中率</div><div class="mc-value">{{ selected.bufferPoolHitRate ? selected.bufferPoolHitRate.toFixed(1) + '%' : '-' }}</div></div>
          <div class="metric-cell"><div class="mc-label">行锁等待</div><div class="mc-value">{{ selected.rowLockWaits }}</div></div>
          <div class="metric-cell"><div class="mc-label">死锁</div><div class="mc-value">{{ selected.deadlocks }}</div></div>
          <div class="metric-cell"><div class="mc-label">复制延迟</div><div class="mc-value">{{ selected.secondsBehindMaster }}s</div></div>
          <div class="metric-cell"><div class="mc-label">Commit</div><div class="mc-value">{{ formatNum(selected.comCommit) }}</div></div>
          <div class="metric-cell"><div class="mc-label">Rollback</div><div class="mc-value">{{ formatNum(selected.comRollback) }}</div></div>
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
import {
  Grid,
  CircleCheck,
  CircleClose,
  Connection,
  DataLine,
  Bell,
  QuestionFilled,
} from '@element-plus/icons-vue'
import { mwTrendOption } from '../../charts/echarts'
import { rangeToWindow, rangeToStep, MW_RANGE_OPTIONS } from '../mw/mwRange'

// 空状态图标：保留原有"数据库圆柱"造型
const EmptyIcon = () =>
  h('svg', {
    viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', 'stroke-width': 1.5,
    innerHTML:
      '<ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M3 5v6c0 1.66 4 3 9 3s9-1.34 9-3V5"/><path d="M3 11v6c0 1.66 4 3 9 3s9-1.34 9-3v-6"/>',
  })

// 详情抽屉趋势图：QPS 与连接数量纲不同，拆成两张图（避免单轴量纲混叠）。
const TRENDS = [
  { title: 'QPS', unit: 'QPS', metrics: [{ name: 'QPS', metric: 'mysql_queries_per_sec' }] },
  { title: '连接数', unit: '个', metrics: [{ name: '连接数', metric: 'mysql_threads_connected' }] },
]

const loading = ref(true)
const instances = ref([])
// 集群健康结论来自后端（/api/v1/middleware/mysql/instances 的 clusters 字段）：
// 与告警引擎共用 alert.ClassifyClusterFault，页面不再自行判定，避免同实例两种结论。
// 形如 { 'dev-mysql-gr': { mode: 'single'|'multi'|'unknown', fault: '', members: 3 } }
const clusterInfo = ref({})
const error = ref('')
const drawerVisible = ref(false)
const selected = ref(null)

const stats = computed(() => {
  const s = { total: 0, up: 0, down: 0, totalConnections: 0, totalQPS: 0, totalSlowQueries: 0 }
  for (const i of instances.value) {
    s.total++
    if (i.up) s.up++; else s.down++
    s.totalConnections += i.threadsConnected || 0
    s.totalQPS += i.queriesPerSec || 0
    s.totalSlowQueries += i.slowQueries || 0
  }
  return s
})

// 按拓扑分组，对齐 Redis 的"实例拓扑"展示
const topologyGroups = computed(() => {
  const reps = instances.value.filter((i) => (i.topology || '').toLowerCase() === 'replication')
  const clusters = instances.value.filter((i) => (i.topology || '').toLowerCase() === 'cluster')
  const standalone = instances.value.filter((i) => {
    const t = (i.topology || '').toLowerCase()
    return t !== 'replication' && t !== 'cluster'
  })

  const groupBy = (list) => {
    const map = {}
    list.forEach((i) => {
      const g = i.group || i.name || i.instance
      ;(map[g] = map[g] || []).push(i)
    })
    return Object.keys(map).map((name) => {
      const items = map[name]
      const masters = items.filter((i) => (i.role || '').toLowerCase() === 'master')
      const slaves = items.filter((i) => (i.role || '').toLowerCase() === 'slave')
      const slavesByMaster = {}
      masters.forEach((m) => {
        slavesByMaster[m.instance] = slaves.filter((s) => s.replicaOf === m.instance)
      })
      const unlinkedSlaves = slaves.filter((s) => !s.replicaOf || !masters.some((m) => m.instance === s.replicaOf))
      return { name, masters, slaves, slavesByMaster, unlinkedSlaves, nodes: items }
    })
  }

  return {
    replications: groupBy(reps),
    clusters: groupBy(clusters),
    standalones: standalone,
  }
})

function isPrimary(i) {
  return ['primary', 'master'].includes((i.role || '').toLowerCase())
}
function nodeRoleText(i) {
  const r = (i.role || '').toLowerCase()
  if (['primary', 'master'].includes(r)) return 'PRIMARY'
  if (['secondary', 'slave'].includes(r)) return 'SECONDARY'
  return i.role || '—'
}

function clusterHealth(grp) {
  // 后端已判定集群状态损坏（无主 / 多主脑裂 / 组视图分裂 / 模式不一致）→ 直接判异常。
  // 判定口径与「MySQL 集群状态损坏」告警规则一致，页面与告警不会再出现两种结论。
  if (clusterInfo.value[grp.name]?.fault) return 'fault'
  const items = grp.masters.concat(grp.slaves, grp.nodes || [])
  if (!items.length) return 'unknown'
  if (items.some((i) => !i.up)) return 'bad'
  if (items.some((i) => i.up && i.secondsBehindMaster != null && i.secondsBehindMaster > 30)) return 'warn'
  return 'good'
}
function clusterHealthClass(grp) {
  const h = clusterHealth(grp)
  return h === 'good' ? 'badge-ok' : h === 'warn' ? 'badge-warn' : h === 'bad' || h === 'fault' ? 'badge-down' : 'badge-unknown'
}
function clusterHealthText(grp) {
  const info = clusterInfo.value[grp.name]
  if (info?.fault) return clusterFaultShort(info.fault)
  const h = clusterHealth(grp)
  return h === 'good' ? '运行正常' : h === 'warn' ? '延迟偏高' : h === 'bad' ? '存在离线' : '未知'
}
// 徽标只放短语，完整原因放 title 悬浮展示（后端返回的判定说明较长）
function clusterFaultShort(fault) {
  if (fault.includes('组视图分裂')) return '组视图分裂'
  if (fault.includes('无主')) return '无主'
  if (fault.includes('多主')) return '多主（疑似脑裂）'
  if (fault.includes('模式配置不一致')) return '模式不一致'
  if (fault.includes('角色不一致')) return '角色不一致'
  return '状态损坏'
}
function clusterFaultDetail(grp) {
  return clusterInfo.value[grp.name]?.fault || ''
}
// 集群复制模式：单主（多主即脑裂）/ 多主（全部成员可写，属正常形态）/ 未知（旧版 Agent 未上报）
function clusterModeText(grp) {
  const mode = clusterInfo.value[grp.name]?.mode
  return mode === 'single' ? '单主模式' : mode === 'multi' ? '多主模式' : ''
}

const detailFields = computed(() => {
  const s = selected.value
  if (!s) return []
  const f = [
    { label: '实例', value: s.instance, mono: true },
    { label: '节点', value: s.node, mono: true },
  ]
  if (s.role) f.push({ label: '角色', value: s.role })
  if (s.version) f.push({ label: '版本', value: s.version })
  if (s.topology) f.push({ label: '拓扑', value: s.topology })
  if (s.replicaOf) f.push({ label: '主库', value: s.replicaOf, mono: true })
  return f
})

async function load() {
  loading.value = true
  error.value = ''
  try {
    const data = await http.get('/api/v1/middleware/mysql/instances')
    instances.value = data.instances || []
    const map = {}
    for (const c of data.clusters || []) map[c.name] = c
    clusterInfo.value = map
  } catch (e) {
    console.error('加载 MySQL 实例失败', e)
    error.value = 'MySQL 实例数据加载失败'
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
.mysql-table :deep(th) { white-space: nowrap; }
.text-muted { color: var(--t3); }
.metric-good { color: var(--ok); }
.metric-warn { color: var(--warn); }
.metric-bad { color: var(--danger); }
:deep(.row-down) { opacity: 0.6; }
.mono { font-family: var(--mono); }
.pager { display: flex; justify-content: flex-end; margin-top: 12px; }

/* ===== 实例拓扑（对齐 Redis 拓扑展示） ===== */
.topo-group { margin-bottom: 22px; }
.topo-group:last-child { margin-bottom: 0; }
.topo-group + .topo-group { padding-top: 20px; border-top: 1px dashed var(--bd); }
.topo-group-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 14px; flex-wrap: wrap; gap: 8px; }
.topo-group-title { display: inline-flex; align-items: center; gap: 6px; font-size: var(--fs-base); color: var(--t1); }
.topo-group-title .el-icon { color: var(--accent); }
.topo-meta { display: inline-flex; align-items: center; gap: 10px; font-size: var(--fs-sm); }
.dim { color: var(--t3); }
.badge { padding: 2px 8px; border-radius: 10px; font-size: var(--fs-xs); font-weight: 600; }
.badge-ok { color: var(--ok); background: var(--ok-dim); }
.badge-warn { color: var(--warn); background: var(--warn-dim); }
.badge-down { color: var(--danger); background: var(--danger-dim); }
.badge-unknown { color: var(--t3); background: var(--fill-1); }

.topo-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); gap: 12px; }
.rel-node { padding: 12px 14px; border-radius: var(--r-lg); cursor: pointer; border: 1px solid var(--bd); background: var(--s1); transition: transform 0.15s, box-shadow 0.15s; }
.rel-node:hover { transform: translateY(-2px); box-shadow: 0 4px 16px rgba(0,0,0,0.3); }
.rel-node.is-down { opacity: 0.6; }
.rel-node-name { font-size: var(--fs-base); font-weight: 600; color: var(--t1); display: flex; align-items: center; gap: 6px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.rel-node-meta { display: flex; align-items: center; gap: 6px; font-size: var(--fs-sm); color: var(--t2); margin-top: 6px; }
.rel-standalone { border-left: 4px solid var(--chart-blue); }

/* 主从树 */
.ms-tree { display: flex; flex-direction: column; gap: 18px; margin-top: 6px; }
.ms-unit { display: flex; gap: 18px; align-items: stretch; flex-wrap: wrap; }
.rel-master { border-left: 4px solid var(--chart-orange); min-width: 220px; }
.ms-master { flex: 0 0 auto; }
.ms-branch { flex: 1; min-width: 240px; display: flex; flex-direction: column; gap: 10px; }
.ms-branch-rail { display: flex; gap: 14px; font-size: var(--fs-sm); color: var(--t3); padding-left: 4px; }
.legend-line { display: inline-block; width: 26px; height: 0; vertical-align: middle; margin-right: 4px; }
.legend-solid { border-top: 2px solid var(--chart-orange); }
.legend-dash { border-top: 2px dashed var(--chart-green); }
.ms-slaves { display: grid; grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); gap: 12px; position: relative; padding-left: 16px; border-left: 2px solid var(--chart-orange); }
.ms-slave-card { border-left: 4px solid var(--chart-green); }
.ms-slave-head { display: flex; align-items: center; gap: 6px; font-size: var(--fs-sm); font-weight: 600; color: var(--t1); overflow: hidden; }
.ms-slave-head .mono { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ms-slave-meta { display: flex; align-items: center; gap: 6px; font-size: var(--fs-sm); color: var(--t2); margin-top: 6px; flex-wrap: wrap; }
.role-badge { display: inline-flex; align-items: center; justify-content: center; width: 18px; height: 18px; border-radius: 4px; font-size: var(--fs-xs); font-weight: 700; color: #fff; flex: 0 0 auto; }
.role-badge-m { background: var(--chart-orange); }
.role-badge-s { background: var(--chart-green); }

.topo-legend { display: flex; gap: 18px; margin-top: 10px; font-size: var(--fs-sm); color: var(--t3); flex-wrap: wrap; }
.unlinked-block { margin-top: 14px; padding: 10px 12px; border: 1px dashed var(--bd); border-radius: var(--r-md); background: var(--warn-dim); }
.unlinked-label { font-size: var(--fs-sm); color: var(--t2); display: flex; align-items: center; gap: 6px; margin-bottom: 10px; }
.unlinked-list { display: grid; grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); gap: 12px; }

/* 详情抽屉 */
.detail-content { padding: 0 4px; }
.metric-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(140px, 1fr)); gap: 12px; margin-bottom: 20px; }
.metric-cell { padding: 12px; background: var(--fill-1); border-radius: var(--r-md); text-align: center; }
.mc-label { font-size: var(--fs-xs); color: var(--t3); margin-bottom: 4px; }
.mc-value { font-size: var(--fs-lg); font-weight: 600; }
.trend-block { margin-bottom: 18px; }
.trend-title { font-size: var(--fs-sm); color: var(--t3); margin-bottom: 6px; }
</style>
