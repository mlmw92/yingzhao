<template>
  <div ref="el" class="spark"></div>
</template>

<script setup>
// 表格内嵌的迷你趋势图（延迟 sparkline）。
// 走 sparkOption（无轴/无网格的最小化配置）——不要复用 areaOption：
// 后者带 y 轴刻度与左侧网格边距，在 28px 高容器里会把曲线压成细缝。
import { ref, onMounted, onBeforeUnmount, watch, nextTick } from 'vue'
import { initChart, sparkOption, COLORS } from '../../charts/echarts'

const props = defineProps({
  // 支持 [[ts, val]] 或 [{timestamp, value}]
  points: { type: Array, default: () => [] },
  color: { type: String, default: '' },
  unit: { type: String, default: '' },
})

const el = ref(null)
let chart = null

function normalize() {
  return (props.points || [])
    .map((p) => (Array.isArray(p) ? Number(p[1]) : Number(p.value)))
    .filter((v) => Number.isFinite(v))
}

function render() {
  if (!chart) return
  const opt = sparkOption(props.color || COLORS.cyan, props.unit)
  opt.series[0].data = normalize()
  chart.setOption(opt, true)
}

onMounted(async () => {
  await nextTick()
  if (!el.value) return
  chart = initChart(el.value)
  render()
})

onBeforeUnmount(() => {
  chart && chart.dispose()
})

watch(() => props.points, render, { deep: true })
</script>

<style scoped>
.spark {
  width: 100%;
  height: 28px;
}
</style>
