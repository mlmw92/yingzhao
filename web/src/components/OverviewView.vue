<template>
  <div class="overview">
    <PageHeader title="系统概览" desc="主机、中间件与告警的实时健康总览">
      <template #actions>
        <el-button v-if="hiddenBlocks.length && !editing" size="small" @click="restoreHidden">
          已隐藏 {{ hiddenBlocks.length }} 个 · 恢复
        </el-button>
        <el-button v-if="editing" size="small" @click="reset">重置布局</el-button>
        <el-button size="small" :type="editing ? 'primary' : 'default'" @click="setEditing(!editing)">
          {{ editing ? '完成编辑' : '自定义布局' }}
        </el-button>
        <el-button size="small" :icon="RefreshRight" @click="refresh" :loading="loading">刷新</el-button>
      </template>
    </PageHeader>

    <!-- 页级错误：此前 get() 把失败吞成 {}，后端全挂与"真的没数据"长得一模一样 -->
    <el-alert v-if="loadError" type="error" :closable="false" show-icon class="ov-load-error">
      <template #title>{{ loadError }}
        <el-button link type="primary" @click="refresh">重试</el-button>
      </template>
    </el-alert>

    <!-- 研判条：有活跃告警才渲染，直接回答"最该处理什么" -->
    <TriageBar :alerts="alertsAll" :nodes="nodes" :latest-map="latestMap" />

    <div v-if="editing" class="ov-edit-hint">
      编辑模式：使用各区块右上角的 ↑ / ↓ 调整顺序，点击「隐藏」可关闭该区块；「智能运维」固定置顶，不可移动或隐藏（但可折叠）。配置自动保存到本地。
    </div>

    <div class="ov-grid">
      <section
        v-for="b in visibleBlocks"
        :key="b.key"
        class="ov-block"
        :data-span="b.span"
        :style="{ gridColumn: 'span ' + b.span }"
      >
        <SectionCard :title="b.title" class="ov-card" :loading="firstLoad && b.key !== 'intelligence'">
          <template #actions>
            <!-- 智能运维区块是 260px 重区块：可折叠（默认收起，有 critical 时自动展开）。
                 注意这是**折叠**，不是隐藏——PINNED_KEYS 与"不可隐藏"的定位保持不变。 -->
            <button
              v-if="b.key === 'intelligence'"
              class="ov-collapse"
              type="button"
              :aria-expanded="String(!intelCollapsed)"
              @click="toggleIntel"
            >{{ intelCollapsed ? '展开' : '收起' }}</button>
            <div v-if="editing" class="ov-edit-tools">
              <template v-if="pinnedKeys.includes(b.key)">
                <span class="ov-pinned" title="该区块固定置顶">固定置顶</span>
              </template>
              <template v-else>
                <button class="ov-tool" title="上移" @click="moveUp(b.key)">↑</button>
                <button class="ov-tool" title="下移" @click="moveDown(b.key)">↓</button>
                <button class="ov-tool" title="隐藏" @click="toggleVisible(b.key)">隐藏</button>
              </template>
            </div>
          </template>
          <div v-show="b.key !== 'intelligence' || !intelCollapsed" class="ov-card-body">
            <component :is="compMap[b.key]" v-bind="blockProps(b.key)" @signal="onIntelSignal" />
          </div>
        </SectionCard>
      </section>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onBeforeUnmount, markRaw } from 'vue'
import { RefreshRight, Monitor, Bell, Connection, FirstAidKit } from '@element-plus/icons-vue'
import http from '../api/http'
import { middlewareTypes } from './overview/middlewareConfig'
// 模板派生类型的通用图标（内置类型用各自的品牌图标）
import templateIcon from '../assets/img/template.svg'
import { formatMetric } from './overview/format'
import { useOverviewLayout } from './overview/useOverviewLayout'
import { dedupeRecentAlerts } from './overview/alertDedupe'
import { calculateSystemHealth } from '../composables/healthScore'

import PageHeader from './common/PageHeader.vue'
import SectionCard from './common/SectionCard.vue'
import TriageBar from './overview/TriageBar.vue'
import HealthBlock from './overview/HealthBlock.vue'
import KpiBlock from './overview/KpiBlock.vue'
import AlertList from './overview/AlertList.vue'
import IntelligenceBlock from './overview/IntelligenceBlock.vue'
import HostOverview from './overview/HostOverview.vue'
import MiddlewareOverview from './overview/MiddlewareOverview.vue'

const { blocks, editing, pinnedKeys, moveUp, moveDown, toggleVisible, reset, setEditing, persist } = useOverviewLayout()

const compMap = {
  health: HealthBlock,
  kpi: KpiBlock,
  criticalAlerts: AlertList,
  intelligence: IntelligenceBlock,
  hostOverview: HostOverview,
  middleware: MiddlewareOverview,
  recentAlerts: AlertList,
}

const loading = ref(false)
// 首屏骨架只在第一次加载出现：30s 轮询刷新时不应让整页闪骨架
const firstLoad = ref(true)
const loadError = ref('')
const nodes = ref([])
const groups = ref([])
const alertsAll = ref([])
const latest = ref({ metrics: {} })
const mwData = ref({}) // key -> instances[]
// 模板派生类型的卡片：数据来自 /middleware/overview（total/up/down/summary 已由后端算好）
const dynamicMw = ref([])

async function loadAll() {
  loading.value = true
  // get() 不再把失败吞成 {}：收集失败项，页级提示"哪几块没拿到数据"
  const failed = []
  const get = (url, label) =>
    http
      .get(url)
      .then((r) => r || {})
      .catch(() => {
        failed.push(label)
        return {}
      })
  try {
    const [nRes, gRes, aRes, lRes, ...mw] = await Promise.all([
      get('/api/v1/nodes', '主机列表'),
      get('/api/v1/groups', '主机分组'),
      get('/api/v1/alerts?state=active', '活跃告警'),
      get('/api/v1/nodes/latest', '最新指标'),
      ...middlewareTypes.map((t) => get(t.endpoint, t.label)),
    ])
    nodes.value = nRes.nodes || []
    groups.value = gRes.groups || []
    alertsAll.value = aRes.alerts || []
    latest.value = lRes || { metrics: {} }
    // 模板派生类型的卡片数据取自总览接口（内置类型走各自的实例端点，结构不统一）
    try {
      const ov = await http.get('/api/v1/middleware/overview')
      dynamicMw.value = (ov.types || []).filter((t) => t.kind === 'template')
    } catch (e) {
      dynamicMw.value = []
      failed.push('中间件总览')
    }
    const map = {}
    middlewareTypes.forEach((t, i) => {
      const r = mw[i] || {}
      // 不同端点返回结构不统一，做兼容：
      //   - 大多数：{ instances: [...] }
      //   - Docker：  { containers: [...], hosts: [...] }
      //   - K8s：    { clusters: [...], nodes: [...], pods: [...] }
      let arr = []
      if (Array.isArray(r.instances)) arr = r.instances
      else if (Array.isArray(r.containers)) arr = r.containers
      else if (Array.isArray(r.clusters)) arr = r.clusters
      else if (Array.isArray(r)) arr = r
      map[t.key] = arr
    })
    mwData.value = map
    loadError.value = failed.length ? `${failed.join('、')} 加载失败` : ''
  } finally {
    loading.value = false
    firstLoad.value = false
  }
}

function refresh() {
  loadAll()
}

// ---------- 智能运维区块折叠 ----------
// 默认收起；一旦分析结论里出现 critical，自动展开（不让用户漏掉最该看的东西）。
const intelCollapsed = ref(true)
const intelHasCritical = ref(false)
function toggleIntel() {
  intelCollapsed.value = !intelCollapsed.value
}
function onIntelSignal(payload) {
  intelHasCritical.value = !!(payload && payload.critical)
  if (intelHasCritical.value) intelCollapsed.value = false
}

// ---------- 隐藏区块的恢复入口（非编辑态可见） ----------
const hiddenBlocks = computed(() => blocks.value.filter((b) => !b.visible))
function restoreHidden() {
  blocks.value.forEach((b) => {
    b.visible = true
  })
  persist()
}

// ---------- 派生数据 ----------
const latestMap = computed(() => latest.value.metrics || {})

const sortedNodes = computed(() =>
  [...nodes.value].sort((a, b) => {
    const g = (a.group || '').localeCompare(b.group || '')
    if (g !== 0) return g
    const na = a.displayName || a.hostname
    const nb = b.displayName || b.hostname
    return na.localeCompare(nb)
  })
)

const groupedHosts = computed(() => {
  const map = {}
  const order = []
  sortedNodes.value.forEach((n) => {
    const g = n.group && n.group.trim() ? n.group : '未分组'
    if (!map[g]) {
      map[g] = []
      order.push(g)
    }
    map[g].push({ ...n, metrics: latestMap.value[n.hostname] || {} })
  })
  order.sort((a, b) => {
    if (a === '未分组') return 1
    if (b === '未分组') return -1
    return a.localeCompare(b)
  })
  return order.map((g) => ({ group: g, hosts: map[g] }))
})

const health = computed(() => calculateSystemHealth(nodes.value, latestMap.value, alertsAll.value))

const kpis = computed(() => {
  const ns = sortedNodes.value
  const total = ns.length
  const offline = ns.filter((n) => n.status !== 'online').length
  const online = total - offline
  const a = alertsAll.value
  const crit = a.filter((x) => (x.severity || '').toLowerCase() === 'critical').length
  const warn = a.filter((x) => (x.severity || '').toLowerCase() === 'warning').length
  return [
    {
      label: '主机总数',
      value: total,
      foot: `在线 ${online} · 离线 ${offline}`,
      tone: offline > 0 ? 'warn' : 'good',
      icon: markRaw(Monitor),
    },
    {
      label: '活跃告警',
      value: a.length,
      foot: `紧急 ${crit} · 警告 ${warn}`,
      tone: crit > 0 ? 'bad' : warn > 0 ? 'warn' : 'good',
      icon: markRaw(Bell),
    },
    {
      label: '中间件实例',
      value: Object.values(mwData.value).reduce((s, arr) => s + (arr ? arr.length : 0), 0),
      foot: `${middlewareTypes.length} 种类型`,
      tone: 'good',
      icon: markRaw(Connection),
    },
    {
      label: '系统健康度',
      value: Math.round(health.value.score),
      foot: health.value.statusText,
      tone: health.value.rank === 'bad' ? 'bad' : health.value.rank === 'warn' ? 'warn' : 'good',
      icon: markRaw(FirstAidKit),
    },
  ]
})

const criticalAlerts = computed(() =>
  alertsAll.value
    .filter((a) => (a.severity || '').toLowerCase() === 'critical')
    .slice(0, 8)
)

// 「紧急告警」区块可见时才去重：区块被用户隐藏后，最近告警要还原完整列表，否则等于丢信息（§33 #7）
const criticalBlockVisible = computed(
  () => (blocks.value.find((b) => b.key === 'criticalAlerts') || {}).visible !== false
)

// 去重规则抽在 alertDedupe.js（纯函数，可单测），这里只负责把结果拆给模板
const recentBlock = computed(() =>
  dedupeRecentAlerts({
    alerts: alertsAll.value,
    critical: criticalAlerts.value,
    criticalVisible: criticalBlockVisible.value,
    limit: 12,
  })
)
const recentAlerts = computed(() => recentBlock.value.list)
// 因与「紧急告警」重复而被剔除的条数：空状态不能再说"暂无告警"，要说"已在上方显示"
const recentDeduped = computed(() => recentBlock.value.deduped)

const mwSummaries = computed(() => {
  const builtin = middlewareTypes.map((t) => {
    const inst = mwData.value[t.key] || []
    const total = inst.length
    const online = inst.filter((i) => i.up).length
    const offline = total - online
    const up = inst.filter((i) => i.up)
    const top = [...up]
      .sort((a, b) => (Number(b[t.topKey]) || 0) - (Number(a[t.topKey]) || 0))
      .slice(0, 3)
      .map((i) => ({
        label: i.name || i.instance || i.node,
        valueText: formatMetric(t.fmt, i[t.topKey]),
        subText: t.subKey ? formatMetric(t.subFmt, i[t.subKey]) : '',
      }))
    return { ...t, total, online, offline, topN: top }
  })

  // 模板派生类型：主指标取模板 rules.metrics 声明的首项；空状态要给出与内置类型不同的排查方向
  const dynamic = dynamicMw.value.map((t) => ({
    key: t.type,
    label: t.label,
    icon: templateIcon,
    tab: t.type,
    total: t.total,
    online: t.up,
    offline: t.down,
    topN: (t.summary || []).slice(0, 3).map((s) => ({
      label: s.label,
      valueText: `${s.value}${s.unit ? ' ' + s.unit : ''}`,
      subText: '',
    })),
    emptyTitle: '已配置但无数据',
    emptySub: '检查生效分组与目标可达性，或点开「采集项模板」查看',
  }))
  return builtin.concat(dynamic)
})

const visibleBlocks = computed(() => blocks.value.filter((b) => b.visible))

function blockProps(key) {
  switch (key) {
    case 'health':
      return { health: health.value }
    case 'kpi':
      return { kpis: kpis.value }
    case 'criticalAlerts':
      return { alerts: criticalAlerts.value, variant: 'critical' }
    case 'hostOverview':
      return { groupedHosts: groupedHosts.value }
    case 'middleware':
      return { summaries: mwSummaries.value, loading: loading.value }
    case 'recentAlerts':
      return { alerts: recentAlerts.value, dedupedCount: recentDeduped.value, variant: 'recent' }
    default:
      return {}
  }
}

// ---------- 轮询：页面不可见时暂停 ----------
// 与 RefreshBar / HostsView 同一套口径（此前首页 30s 轮询不判断可见性，后台标签页一直在打接口）
let timer = null
function startTimer() {
  stopTimer()
  timer = setInterval(loadAll, 30000)
}
function stopTimer() {
  if (timer) {
    clearInterval(timer)
    timer = null
  }
}
function onVisibilityChange() {
  if (document.hidden) {
    stopTimer()
  } else {
    loadAll()
    startTimer()
  }
}

onMounted(() => {
  loadAll()
  startTimer()
  document.addEventListener('visibilitychange', onVisibilityChange)
})
onBeforeUnmount(() => {
  stopTimer()
  document.removeEventListener('visibilitychange', onVisibilityChange)
})
</script>

<style scoped>
.overview {
  padding: 4px;
}
.ov-load-error {
  margin-bottom: 12px;
}
.ov-load-error :deep(.el-button--primary) { font-size: var(--fs-sm); }
.ov-edit-hint {
  font-size: var(--fs-sm);
  color: var(--accent);
  background: var(--accent-dim);
  border: 1px solid var(--bd);
  border-radius: 8px;
  padding: 8px 12px;
  margin-bottom: 14px;
}
.ov-grid {
  display: grid;
  grid-template-columns: repeat(12, 1fr);
  gap: 16px;
  align-items: stretch;
}
.ov-block {
  min-width: 0;
}
/* 外观来自 SectionCard（统一容器），这里只负责布局 */
.ov-card {
  height: 100%;
}
.ov-edit-tools {
  display: flex;
  gap: 6px;
}
.ov-collapse {
  font-size: var(--fs-sm);
  line-height: 1;
  padding: 4px 10px;
  border-radius: 6px;
  border: 1px solid var(--bd);
  background: var(--s2);
  color: var(--t2);
  cursor: pointer;
  transition: color var(--dur-1) var(--ease), border-color var(--dur-1) var(--ease);
}
.ov-collapse:hover {
  color: var(--accent);
  border-color: var(--accent);
}
.ov-tool {
  font-size: var(--fs-sm);
  line-height: 1;
  padding: 4px 8px;
  border-radius: 6px;
  border: 1px solid var(--bd);
  background: var(--s2);
  color: var(--t2);
  cursor: pointer;
  transition: all 0.15s;
}
.ov-tool:hover {
  color: var(--t1);
  border-color: var(--accent);
}
.ov-pinned {
  font-size: var(--fs-xs);
  line-height: 1;
  padding: 4px 8px;
  border-radius: 6px;
  border: 1px solid var(--bd);
  background: var(--s2);
  color: var(--accent);
  cursor: default;
}
.ov-card-body {
  flex: 1;
  min-width: 0;
}

/* 12 栅格里 span 4 在 1400px 以下只剩约 300px，而「关键指标」卡内部还要再分 2 列，
   每格约 138px——26px 的数字加上「在线 x · 离线 y」几乎必然被截成「在线 …」。
   1400px 起先让 span 4 降到半宽（span 6），1100px 以下再全部拉通。
   data-span 是为了让断点能区分区块：span 是内联样式，CSS 只能靠属性选择器命中。 */
@media (max-width: 1280px) {
  .ov-block[data-span='4'] {
    grid-column: span 6 !important;
  }
}
@media (max-width: 1024px) {
  .ov-block {
    grid-column: span 12 !important;
  }
}
</style>
