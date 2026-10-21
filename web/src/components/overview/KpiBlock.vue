<template>
  <div class="kpi-grid">
    <KpiCard
      v-for="k in kpis"
      :key="k.label"
      :value="k.value"
      :label="k.label"
      :hint="k.foot"
      :tone="k.tone"
      :icon="k.icon"
    >
      <template #spark>
        <VChart
          v-if="series(k.label).length > 1"
          class="kpi-spark"
          :option="sparkOption(series(k.label))"
          autoresize
        />
        <!-- 采样点不足 2 个时曲线画不出来。此前只是不渲染，于是同一行里有的卡有迷你图、
             有的没有，两行行高不同；等攒够 2 个点后卡片又突然长高约 44px。
             这里用同高的占位块把高度钉住。 -->
        <div v-else class="kpi-spark kpi-spark-pending"></div>
      </template>
    </KpiCard>
    <div v-if="!kpis || kpis.length === 0" class="kpi-empty">
      <EmptyState
        :icon="DataLine"
        title="暂无指标数据"
        :hints="[
          '确认 Agent 已上报、且主机处于在线状态',
          '刚添加的主机，第一个采样点通常需要 1 分钟',
        ]"
      />
    </div>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import VChart from 'vue-echarts'
import { DataLine } from '@element-plus/icons-vue'
import EmptyState from '../common/EmptyState.vue'
// 视觉与全站 KPI 卡统一：本组件只负责「数据 + sparkline」，样式不再自持一份
import KpiCard from '../KpiCard.vue'

const props = defineProps({
  kpis: { type: Array, default: () => [] },
})

/* =========================================================
 * 迷你趋势（sparkline）
 * 数据来源：每次首页刷新拿到的真实 KPI 值，滚动保存最近 N 个采样点。
 * 刻意不做插值、不伪造历史——所以刚打开页面时曲线是空的，跑几分钟后才成形。
 * 也不新增接口：/api/v1/nodes/latest 只给最新一个点，无法反推历史序列。
 * ========================================================= */
const HISTORY_KEY = 'yingzhao-kpi-history'
const MAX_POINTS = 24
const HISTORY_TTL = 24 * 60 * 60 * 1000

function loadHistory() {
  try {
    const raw = localStorage.getItem(HISTORY_KEY)
    const d = raw ? JSON.parse(raw) : null
    return d && typeof d === 'object' ? d : {}
  } catch (e) {
    return {}
  }
}

const history = ref(loadHistory())

watch(
  () => props.kpis,
  (list) => {
    if (!list || !list.length) return
    const now = Date.now()
    const next = { ...history.value }
    let changed = false
    for (const k of list) {
      const arr = (next[k.label] || []).filter((p) => p && now - p.t < HISTORY_TTL)
      const v = Number(k.value)
      if (Number.isNaN(v)) continue
      const last = arr[arr.length - 1]
      // 同一轮刷新（含 keep-alive 唤醒）不重复记点
      if (last && last.v === v && now - last.t < 5000) continue
      arr.push({ t: now, v })
      next[k.label] = arr.slice(-MAX_POINTS)
      changed = true
    }
    if (!changed) return
    history.value = next
    try {
      localStorage.setItem(HISTORY_KEY, JSON.stringify(next))
    } catch (e) {
      /* 容量错误忽略 */
    }
  },
  { deep: true, immediate: true }
)

function series(label) {
  const arr = history.value[label] || []
  return arr.map((p) => p.v)
}

/* ECharts 取不到 CSS 变量，只能读计算后的值；换肤后要重新读，否则颜色不跟随 */
const themeTick = ref(0)
function onThemeChanged() {
  themeTick.value += 1
}
const accent = computed(() => {
  themeTick.value // 依赖：换肤时重新取色
  if (typeof window === 'undefined' || !document.body) return '#4a9df0'
  const v = getComputedStyle(document.body).getPropertyValue('--accent').trim()
  return v || '#4a9df0'
})

onMounted(() => window.addEventListener('yingzhao:theme-changed', onThemeChanged))
onBeforeUnmount(() => window.removeEventListener('yingzhao:theme-changed', onThemeChanged))

function sparkOption(data) {
  const color = accent.value
  return {
    animation: false,
    grid: { left: 0, right: 0, top: 2, bottom: 0 },
    xAxis: { type: 'category', show: false, boundaryGap: false, data: data.map((_, i) => i) },
    yAxis: { type: 'value', show: false, scale: true },
    series: [
      {
        type: 'line',
        data,
        showSymbol: false,
        smooth: true,
        lineStyle: { width: 1.5, color },
        itemStyle: { color },
        areaStyle: { color, opacity: 0.12 },
      },
    ],
  }
}
</script>

<style scoped>
.kpi-grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 12px;
  height: 100%;
  /* 卡片嵌在「关键指标」区块卡内部（外层已是 --s1），用 s2 比外层亮一级表达层级 */
  --kpi-bg: var(--s2);
}
.kpi-spark {
  display: block;
  width: 100%;
  height: 36px;
  margin-top: 8px;
}
/* 曲线还没成形时的占位：与真实曲线同高，只淡到底色 */
.kpi-spark-pending {
  height: 36px;
  margin-top: 8px;
  border-radius: var(--r-xs);
  background: var(--fill-1);
}
.kpi-empty {
  grid-column: 1 / -1;
}
</style>
