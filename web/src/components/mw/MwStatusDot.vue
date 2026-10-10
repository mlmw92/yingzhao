<template>
  <span class="mw-status" :class="[type, { lg: large, 'dot-only': dotOnly }]" :title="title || undefined">
    <span class="mw-status-dot"></span>
    <span v-if="!dotOnly" class="mw-status-label">{{ label ?? defaultLabel }}</span>
  </span>
</template>

<script setup>
import { computed } from 'vue'

// 中间件状态点：三态语义色。
//
// 两态（normal/abnormal）会把 paused/restarting/dead 全压成一档，
// 严重度被系统性低估——"重启中"和"已挂掉"看起来一模一样。
// 现在统一为三态：
//   normal   正常/在线/运行中         → 绿
//   warning  降级/暂停/重启中/待就绪   → 琥珀
//   abnormal 离线/失败/已停止/崩溃     → 红
// 传入其它取值时按别名归一，未知值兜底为 abnormal（宁可高估不可漏报）。
const props = defineProps({
  // 'normal' | 'warning' | 'abnormal'（也接受 online/up/running、paused/restarting、offline/down/failed 等别名）
  status: { type: String, default: 'normal' },
  // 自定义文案（默认按三态取 正常/警告/异常）
  label: { type: String, default: null },
  // 大号（用于详情头部）
  large: { type: Boolean, default: false },
  // 只画圆点、不显示文案（用于主机卡/列表首列等紧凑场景）
  dotOnly: { type: Boolean, default: false },
  // 悬浮提示（默认取当前文案）
  tooltip: { type: String, default: '' },
})

const NORMAL = ['normal', 'ok', 'up', 'online', 'running', 'healthy', 'active', 'ready', 'success']
const WARNING = ['warning', 'warn', 'degraded', 'paused', 'restarting', 'pending', 'starting', 'created', 'syncing', 'unstable']
const ABNORMAL = ['abnormal', 'offline', 'down', 'failed', 'error', 'dead', 'exited', 'crashloopbackoff', 'unhealthy', 'stopped', 'missing']

const type = computed(() => {
  const s = String(props.status || '').trim().toLowerCase()
  if (NORMAL.includes(s)) return 'normal'
  if (WARNING.includes(s)) return 'warning'
  if (ABNORMAL.includes(s)) return 'abnormal'
  // 未知取值：既不当作正常也不当作警告，直接按异常展示（避免把故障显示成绿灯）
  return 'abnormal'
})

const LABELS = { normal: '正常', warning: '警告', abnormal: '异常' }
const defaultLabel = computed(() => LABELS[type.value])
const title = computed(() => props.tooltip || props.label || defaultLabel.value)
</script>

<style scoped>
.mw-status {
  display: inline-flex;
  align-items: center;
  font-size: var(--fs-sm);
  font-weight: 500;
  line-height: 1;
}
.mw-status-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  margin-right: 6px;
  display: inline-block;
  flex-shrink: 0;
}
.mw-status.dot-only .mw-status-dot {
  margin-right: 0;
}
.mw-status-label {
  white-space: nowrap;
}
/* 正常：绿灯 + 绿色字体 */
.mw-status.normal {
  color: var(--ok);
}
.mw-status.normal .mw-status-dot {
  background: var(--ok);
  box-shadow: 0 0 6px var(--ok-bd);
}
/* 警告：琥珀 + 琥珀字体（暂停/重启中/降级） */
.mw-status.warning {
  color: var(--warn);
}
.mw-status.warning .mw-status-dot {
  background: var(--warn);
  box-shadow: 0 0 6px var(--warn-bd);
}
/* 异常：红灯 + 红色字体（离线/失败/崩溃）——不再用黄色，避免与警告混淆 */
.mw-status.abnormal {
  color: var(--danger);
}
.mw-status.abnormal .mw-status-dot {
  background: var(--danger);
  box-shadow: 0 0 6px var(--danger-bd);
}
/* 大号 */
.mw-status.lg {
  font-size: var(--fs-md);
}
.mw-status.lg .mw-status-dot {
  width: 12px;
  height: 12px;
}
</style>
