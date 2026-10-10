<!--
  资产关系图（N 跳邻域）。力导向布局，点节点即以它为中心重新展开。

  抽成独立组件的原因：这张图有三个入口——台账详情（看某个资产的关系）、
  告警影响面（看这条告警会影响谁）、全库关系视图（看符合筛选的那批边）。
  三处必须**完全同一套**范围裁剪、节点着色与交互，否则同一个资产在不同入口
  会画出不同的图，而这种不一致没有报错、只在现场被当成"数据不对"。

  两种工作模式（由 reCenter 区分）：
  - **邻域模式**（默认）：组件自己按 assetId 取数，点节点换中心；
  - **数据模式**（传了 data）：数据由调用方给（例如全库筛选子图），没有"中心"这个概念，
    因此不显示跳数、点节点改为向外抛 node-click（由调用方决定去哪里）。

  「图四件套」（图例 / 缩放 / 全屏 / 导出）放在组件内，三个入口自动获得同一套工具——
  放在页面层会让三处各写一遍，迟早不一致。
-->
<template>
  <div class="topo" :class="{ fullscreen }">
    <div class="topo-bar">
      <!-- 跳数切换必须显式忽略事件参数：el-radio-group 的 change 会把新值当第一个实参传进来，
           而 load 的第一个参数是资产 id——直接绑 load 会把"3 跳"当成"资产 3"。 -->
      <el-radio-group v-if="reCenter" v-model="depth" size="small" @change="() => load()">
        <el-radio-button :value="1">1 跳</el-radio-button>
        <el-radio-button :value="2">2 跳</el-radio-button>
        <el-radio-button :value="3">3 跳</el-radio-button>
      </el-radio-group>
      <span v-if="reCenter" class="muted">点节点可以「以它为中心」重新展开</span>
      <span v-else class="muted">点节点打开该资产的详情</span>
      <span class="spacer"></span>
      <div class="topo-tools">
        <el-button size="small" :icon="ZoomIn" title="放大" aria-label="放大" @click="zoomBy(1.2)" />
        <el-button size="small" :icon="ZoomOut" title="缩小" aria-label="缩小" @click="zoomBy(1 / 1.2)" />
        <el-button size="small" title="复位缩放与视角" @click="resetZoom">复位</el-button>
        <el-button size="small" :icon="FullScreen" :title="fullscreen ? '退出全屏' : '全屏查看'" @click="toggleFullscreen">
          {{ fullscreen ? '退出全屏' : '全屏' }}
        </el-button>
        <el-button size="small" :icon="Download" title="导出为 PNG 图片" @click="exportImage">导出</el-button>
      </div>
    </div>

    <!-- 可点击图例：按类型过滤。纯文字图例只能"看"，不能"问"（"只想看主机"时无处下手）。 -->
    <div v-if="legendItems.length" class="topo-legend-bar">
      <button
        v-for="it in legendItems"
        :key="it.key"
        type="button"
        class="lg-item"
        :class="{ off: hidden.has(it.key) }"
        :aria-pressed="!hidden.has(it.key)"
        :title="hidden.has(it.key) ? `显示「${it.label}」` : `隐藏「${it.label}」`"
        @click="toggleType(it.key)"
      >
        <span class="lg-dot" :style="{ background: it.color }"></span>{{ it.label }}
      </button>
      <span class="muted lg-line">实线 = 采集发现 · 虚线 = 人工维护 · 红圈 = 失联</span>
    </div>

    <el-alert
      v-if="truncated"
      type="warning"
      :closable="false"
      show-icon
      :title="truncatedTitle"
      :description="truncatedHint"
      class="alert-gap"
    />
    <el-alert v-if="error" type="error" :closable="false" show-icon :title="error" class="alert-gap" />
    <div
      v-show="!error"
      ref="chartRef"
      v-loading="loading"
      class="topo-chart"
      :style="chartStyle"
    ></div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onBeforeUnmount, nextTick, watch } from 'vue'
import { ZoomIn, ZoomOut, FullScreen, Download } from '@element-plus/icons-vue'
import { initChart, tokenColor } from '../../charts/echarts'
import { getAssetTopology } from '../../api/asset'

const props = defineProps({
  // 邻域模式必传（以谁为中心）；数据模式下不需要。
  assetId: { type: [Number, String], default: null },
  // 画布高度：抽屉里要矮一些，独立弹窗里可以高一些。
  height: { type: Number, default: 460 },
  // data 非空即进入**数据模式**：不自己取数、没有中心、点节点只抛事件。
  // 形状与邻域载荷一致（nodes/edges/truncated），因此两处画出来的是同一种图。
  data: { type: Object, default: null },
  // 是否允许"点节点换中心"。全库视图里没有中心可言，置 false。
  reCenter: { type: Boolean, default: true },
  // 加载中：切筛选时旧图会停留一瞬，没有反馈会被读成"点了没反应"。
  loading: { type: Boolean, default: false },
})

const emit = defineEmits(['node-click'])

const depth = ref(2)
const error = ref('')
const topo = ref(null)
const chartRef = ref(null)
const fullscreen = ref(false)
// 缩放倍率由组件维护：ECharts 的 graph 没有"缩放动作"可 dispatch，
// 只能改 series.zoom 后 setOption；把倍率存下来才能做按钮与复位。
const zoom = ref(1)
const hidden = ref(new Set())
let chart = null

const truncated = computed(() => !!(topo.value && topo.value.truncated))

// 全屏时让画布撑满，非全屏时用传入高度（内联 height 会盖过 CSS，故按状态切换）
const chartStyle = computed(() => (fullscreen.value ? {} : { height: props.height + 'px' }))

// 截断提示要跟着模式走：邻域模式的边界是**节点数**（能缩跳数），数据模式常常是**边数**
// （只能缩筛选）。说错是哪一种，用户就不知道该往哪调。
const truncatedTitle = computed(() => (props.reCenter
  ? '节点数达到上限，图只展开了部分关系'
  : '命中结果超过上限，图只画出了前一部分'))
const truncatedHint = computed(() => (props.reCenter
  ? '缩小跳数，或直接以目标资产为中心查看。静默省略会让人以为关系就这么多。'
  : '当前筛选命中的关系超过上限，缩小筛选范围可以看到全部。静默省略会让人以为关系就这么多。'))

// 类型 → 颜色令牌。与图例共用同一份定义，避免"图上是蓝的、图例说是绿的"。
// 读的是 CSS 变量而不是写死的十六进制：写死的话切到青绿 / 紫色主题后
// --accent / --ok 全变了，而关系图仍停在极光蓝那一套，与页面其它元素割裂。
const TOPO_TOKENS = {
  host: 'chart-blue',
  'middleware-instance': 'chart-green',
  pod: 'chart-orange',
  workload: 'chart-purple',
}
const TYPE_TITLES = {
  host: '主机',
  'middleware-instance': '中间件实例',
  pod: '容器（Pod）',
  workload: '工作负载',
  service: 'K8s 服务',
  'middleware-cluster': '中间件集群',
}

// 状态标签与台账列表共用一套措辞（"上报正常/失联"而不是"在线/离线"：
// 后者是采集侧探活的结果，两者同词会让人觉得自相矛盾）。
const STATUS_LABELS = { online: '上报正常', missing: '失联', archived: '归档' }
const statusLabel = (s) => STATUS_LABELS[s] || s || '—'

// 图例只列**当前图里真的出现过的类型**：列一堆没出现的类型，点了没变化会被当成坏了。
const legendItems = computed(() => {
  const seen = new Map()
  for (const n of topo.value?.nodes || []) {
    const k = n.typeKey || 'unknown'
    if (seen.has(k)) continue
    seen.set(k, {
      key: k,
      label: n.typeTitle || TYPE_TITLES[k] || k,
      color: tokenColor(TOPO_TOKENS[k] || 't3', '#8a94a6'),
    })
  }
  return [...seen.values()]
})

function toggleType(key) {
  const s = new Set(hidden.value)
  if (s.has(key)) s.delete(key)
  else s.add(key)
  hidden.value = s
  render()
}

// load 取邻域并重绘。id 省略时以当前资产为中心（点节点会显式传入新中心）。
async function load(id) {
  const target = id === undefined ? props.assetId : id
  error.value = ''
  try {
    topo.value = await getAssetTopology(target, { depth: depth.value })
  } catch (e) {
    topo.value = null
    error.value = e.message || '加载关系图失败'
  }
  await nextTick()
  render()
}

// render 把邻域画成力导向图：节点大小按跳数（中心最大）、颜色按类型、
// 红圈表示失联；边用虚线区分人工维护。
function render() {
  const data = topo.value
  if (!data || !chartRef.value) return
  if (!chart) chart = initChart(chartRef.value)

  const hiddenTypes = hidden.value
  const nodes = (data.nodes || [])
    .filter((n) => !hiddenTypes.has(n.typeKey || 'unknown'))
    .map((n) => ({
      id: n.key,
      name: n.name || n.naturalKey,
      symbolSize: n.root ? 52 : n.depth === 1 ? 38 : 28,
      itemStyle: {
        color: tokenColor(TOPO_TOKENS[n.typeKey] || 't3', '#8a94a6'),
        // 已从台账隐藏的资产画成半透明：它仍在关系里，但已不是"要治理的对象"
        opacity: n.ignored ? 0.45 : 1,
        borderColor: n.status === 'online' ? 'transparent' : tokenColor('danger', '#f56c6c'),
        borderWidth: n.status === 'online' ? 0 : 2,
      },
      // 原始节点挂在 data 上：tooltip 与点击回调都要用
      raw: n,
      label: { show: true, fontSize: 11 },
    }))
  const visibleIds = new Set(nodes.map((n) => n.id))
  const links = (data.edges || [])
    // 两端都在图里才画边：只隐藏一端会留下一根连到空气的线
    .filter((e) => visibleIds.has(e.from) && visibleIds.has(e.to))
    .map((e) => ({
      source: e.from,
      target: e.to,
      raw: e,
      lineStyle: {
        type: e.source === 'manual' ? 'dashed' : 'solid',
        width: 1.4,
        color: tokenColor('t3', '#9aa4b2'),
      },
      label: {
        show: true,
        formatter: e.kind,
        fontSize: 10,
        color: tokenColor('t3', '#8a94a6'),
      },
    }))

  chart.setOption(
    {
      tooltip: {
        formatter: (p) => {
          if (p.dataType === 'edge') {
            const e = p.data.raw || {}
            return `${e.kind}<br/>${e.from}<br/>↓<br/>${e.to}<br/>来源：${e.source === 'manual' ? '人工维护' : '采集发现'}`
          }
          const n = (p.data && p.data.raw) || {}
          // 数据模式没有中心，"距中心 N 跳"会是一句假话（那里所有节点的 depth 都是 0）
          const where = props.reCenter ? (n.depth ? `距中心 ${n.depth} 跳` : '（中心）') : ''
          const head = `${n.typeTitle || ''}：${n.name || n.naturalKey}`
          const tail = where ? `<br/>${where}` : ''
          return `${head}<br/>归属节点：${n.node || '—'}<br/>状态：${statusLabel(n.status)}${tail}`
        },
      },
      series: [
        {
          type: 'graph',
          layout: 'force',
          roam: true,
          draggable: true,
          zoom: zoom.value,
          data: nodes,
          links,
          // 力导向参数刻意收敛：关系图节点数不多，斥力太大只会散成一团看不清
          force: { repulsion: 320, edgeLength: 130, gravity: 0.08 },
          emphasis: { focus: 'adjacency' },
          edgeSymbol: ['none', 'arrow'],
          edgeSymbolSize: 7,
        },
      ],
    },
    true
  )
  chart.off('click')
  chart.on('click', (p) => {
    const n = p && p.data && p.data.raw
    if (!n) return
    if (!props.reCenter) {
      // 数据模式没有中心，交给调用方决定去哪里（全库视图拿它打开资产详情）
      emit('node-click', n)
      return
    }
    // 点中心自己不必重查（已经是以它为中心）
    if (n.root) return
    load(n.id)
  })
}

// —— 图四件套（缩放 / 全屏 / 导出） ——
function applyZoom() {
  if (chart) chart.setOption({ series: [{ zoom: zoom.value }] })
}

function zoomBy(factor) {
  zoom.value = Math.min(8, Math.max(0.2, zoom.value * factor))
  applyZoom()
}

function resetZoom() {
  zoom.value = 1
  applyZoom()
}

function toggleFullscreen() {
  fullscreen.value = !fullscreen.value
  // 画布尺寸变了要显式 resize 一次（ResizeObserver 在 display 切换时不一定触发）
  nextTick(() => chart && chart.resize())
}

// Esc 退出全屏：全屏后工具栏可能被顶到视口外，键盘出口是必需的
function onKeydown(e) {
  if (e.key === 'Escape' && fullscreen.value) toggleFullscreen()
}

function exportImage() {
  if (!chart || typeof chart.getDataURL !== 'function') return
  const url = chart.getDataURL({
    pixelRatio: 2,
    backgroundColor: tokenColor('s1', '#ffffff'),
  })
  const a = document.createElement('a')
  a.href = url
  a.download = `topology-${Date.now()}.png`
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
}

function dispose() {
  if (chart) {
    chart.dispose()
    chart = null
  }
}

// 换资产（同一个组件被复用时）必须重新拉取并重建图表：
// 否则会画出上一个资产的图，而界面上没有任何提示。
watch(() => props.assetId, () => {
  if (props.data) return
  dispose()
  load()
})

// 数据模式：数据由调用方给（筛选变了就换一份），换数据必须重建图表。
watch(() => props.data, (d) => {
  if (!d) return
  dispose()
  hidden.value = new Set()
  zoom.value = 1
  topo.value = d
  render()
})

onMounted(() => {
  window.addEventListener('keydown', onKeydown)
  if (props.data) {
    topo.value = props.data
    render()
    return
  }
  load()
})
onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKeydown)
  dispose()
})
</script>

<style scoped>
.topo-bar {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
  margin-bottom: 10px;
}
.spacer { flex: 1; }
.topo-tools { display: flex; align-items: center; gap: 6px; }

/* 可点击图例 */
.topo-legend-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  margin-bottom: 10px;
}
.lg-item {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 2px 10px;
  border: 1px solid var(--bd);
  border-radius: var(--r-pill);
  background: var(--fill-1);
  color: var(--t2);
  font-size: var(--fs-xs);
  font-family: inherit;
  cursor: pointer;
}
.lg-item.off { opacity: 0.45; text-decoration: line-through; }
.lg-dot { width: 8px; height: 8px; border-radius: 50%; flex: none; }
.lg-line { margin-left: auto; }

.topo-chart {
  width: 100%;
}

/* 全屏：用固定定位而不是 Fullscreen API——后者在部分嵌入式环境被禁用，
   且会脱离应用主题背景。Esc 退出（见 onKeydown）。 */
.topo.fullscreen {
  position: fixed;
  inset: 0;
  z-index: 1000;
  display: flex;
  flex-direction: column;
  padding: 16px;
  background: var(--s1);
}
.topo.fullscreen .topo-chart {
  flex: 1;
  min-height: 0;
}
</style>
