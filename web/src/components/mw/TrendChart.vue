<template>
  <div class="trend-chart" :style="{ height: cssHeight }">
    <!-- 画布常驻 DOM：ECharts 需要在真实布局中测量尺寸，
         加载/错误/空态只是覆盖在上面，切换时不必重建实例。 -->
    <div ref="el" class="tc-canvas"></div>

    <div v-if="error" class="tc-overlay tc-error">
      <el-icon class="tc-err-icon"><WarningFilled /></el-icon>
      <span class="tc-err-msg">{{ error }}</span>
      <el-button v-if="retryable" size="small" @click="$emit('retry')">重试</el-button>
    </div>

    <div v-else-if="loading" class="tc-overlay tc-loading" aria-busy="true" aria-label="加载中">
      <div class="sk sk-pulse tc-sk"></div>
    </div>

    <div v-else-if="isEmpty" class="tc-overlay tc-empty">
      <span class="tc-empty-text">{{ emptyText }}</span>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { WarningFilled } from '@element-plus/icons-vue'
import { initChart } from '../../charts/echarts'

// 趋势图工厂：中间件各 Tab 的抽屉/区块图表统一走这里。
//
// 它负责三件过去每处各写一遍的事：
//   1. ECharts 实例生命周期（init / setOption / dispose）——不再有"关闭抽屉不销毁、
//      反复开关累积实例与 ResizeObserver"的泄漏（ResizeObserver 由 initChart 内部挂载并在
//      dispose 时断开）；
//   2. 空 / 加载 / 错误 三态——过去查询失败只在 console 里留一行，界面上是一块空白，
//      用户无从判断"是真的没数据"还是"接口挂了"；
//   3. 尺寸自适应（交给 initChart 的 ResizeObserver）。
// option 由调用方用 charts/echarts.js 的 mwTrendOption / monitorOption 等工厂生成，
// 保证坐标轴 / 提示框 / 分割线都取令牌色。
const props = defineProps({
  // ECharts option（调用方用 mwTrendOption 等工厂生成）
  option: { type: Object, default: null },
  // 高度：数字按 px，字符串原样（如 '100%'）
  height: { type: [Number, String], default: 300 },
  // 加载中（首次加载 / 切页）
  loading: { type: Boolean, default: false },
  // 错误信息（非空即展示错误态，优先于空态）
  error: { type: String, default: '' },
  // 是否展示"重试"按钮
  retryable: { type: Boolean, default: true },
  // 是否无数据。null = 依据 option.series 自动判断
  empty: { type: Boolean, default: null },
  emptyText: { type: String, default: '暂无数据' },
})
defineEmits(['retry'])

const el = ref(null)
let chart = null

const cssHeight = computed(() => (typeof props.height === 'number' ? `${props.height}px` : props.height))

const isEmpty = computed(() => {
  if (props.empty !== null) return props.empty
  const series = props.option && props.option.series
  if (!Array.isArray(series) || !series.length) return true
  return series.every((s) => !Array.isArray(s.data) || s.data.length === 0)
})

// 三态覆盖时画布仍保留在布局中（否则 initChart 测得 0 尺寸无法自愈）
function render() {
  if (!el.value || !props.option) return
  if (!chart) chart = initChart(el.value)
  if (chart.isDisposed()) return
  chart.setOption(props.option, true)
  nextTick(() => {
    if (chart && !chart.isDisposed() && el.value && el.value.clientWidth > 0) chart.resize()
  })
}

onMounted(render)
watch(() => props.option, render, { deep: true })

onBeforeUnmount(() => {
  if (chart) {
    chart.dispose()
    chart = null
  }
})

// 允许调用方在特殊场景下直接拿实例（如手动 dispatchAction）
defineExpose({ getChart: () => chart })
</script>

<style scoped>
.trend-chart {
  position: relative;
  width: 100%;
  min-height: 80px;
}
.tc-canvas {
  width: 100%;
  height: 100%;
}
.tc-overlay {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  border-radius: var(--r-md);
  background: var(--fill-1);
  font-size: var(--fs-sm);
  color: var(--t3);
}
.tc-error {
  flex-wrap: wrap;
  border: 1px solid var(--danger-bd);
  background: var(--danger-dim);
  color: var(--t1);
}
.tc-err-icon {
  color: var(--danger);
  flex-shrink: 0;
}
.tc-err-msg {
  min-width: 0;
  overflow-wrap: break-word;
}
.tc-sk {
  width: 68%;
  height: 60%;
  border-radius: var(--r-md);
}
.tc-empty-text {
  color: var(--t3);
}
</style>
