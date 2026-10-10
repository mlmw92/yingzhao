<template>
  <div class="kpi-card" :class="`tone-${tone}`">
    <div v-if="icon || $slots.icon" class="kpi-icon">
      <slot name="icon">
        <el-icon :size="18"><component :is="icon" /></el-icon>
      </slot>
    </div>
    <div class="kpi-body">
      <div class="kpi-num">{{ value }}</div>
      <div class="kpi-text">{{ label }}</div>
      <!-- hint 可选：口径说明 / 次级统计（如「在线 3 · 离线 1」） -->
      <div v-if="hint" class="kpi-hint" :title="hint">{{ hint }}</div>
      <!-- 迷你趋势等附加内容由调用方注入（首页概览用它画 sparkline） -->
      <slot name="spark" />
    </div>
  </div>
</template>

<script setup>
/* 全站唯一的 KPI 卡片组件。
 * 首页概览此前另有一份 KpiBlock 内联实现（84px 高 / 26px 数值 / --r-md），
 * 与这里（70px / 20px / --r-lg）是两套视觉，同一屏里数字大小对不上。
 * 现已收敛到本组件：概览通过 icon 属性 + #spark 插槽复用，不再各写一份。
 * 需要卡内嵌套（外层已是卡片）时，调用方用 --kpi-bg 覆盖底色。 */
defineProps({
  value: { type: [String, Number], default: '' },
  label: { type: String, default: '' },
  tone: { type: String, default: 'total' },
  hint: { type: String, default: '' },
  // 图标组件（可选）；与 #icon 插槽二选一，插槽优先
  icon: { type: [Object, Function], default: null },
})
</script>

<style scoped>
.kpi-card {
  border-radius: var(--r-lg);
  padding: 15px 15px;
  min-height: 70px;
  display: flex;
  align-items: center;
  gap: 11px;
  border: 1px solid var(--bd);
  background: var(--kpi-bg, var(--s1));
  position: relative;
  overflow: hidden;
  transition: transform 0.2s, box-shadow 0.2s;
}
.kpi-card:hover {
  transform: translateY(-1px);
  box-shadow: var(--sh-2);
}
.kpi-card::before {
  content: '';
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  height: 1px;
  background: var(--bd);
}
.kpi-icon {
  width: 36px;
  height: 36px;
  border-radius: var(--r-md);
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  /* 默认中性：颜色只留给真实异常，不用来给 KPI 分类 */
  background: var(--fill-2);
  color: var(--t2);
}
.kpi-icon :deep(svg) {
  width: 19px;
  height: 19px;
}
.kpi-body {
  display: flex;
  flex-direction: column;
  min-width: 0;
  flex: 1;
}
.kpi-num {
  font-size: var(--fs-xl);
  font-weight: 700;
  font-family: var(--mono);
  letter-spacing: -0.02em;
  line-height: 1.15;
}
.kpi-text {
  font-size: var(--fs-sm);
  color: var(--t2);
  margin-top: 1px;
  white-space: nowrap;
}
.kpi-hint {
  font-size: var(--fs-xs);
  color: var(--t3);
  margin-top: 2px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* =========================================================
 * 色调：只区分「异常」与「正常」两档，不给 KPI 做分类配色。
 * 原来 8 个 tone 各配一种颜色（青/绿/红/紫/蓝/橙/靛），是典型的
 * 彩色图标网格——颜色变成了装饰，真正的异常反而不显眼。
 * 现在：默认中性；只有 down / alert / bad（真实故障）上红色。
 * 其余 tone（total/up/ok/mem/conn/ops/cluster/host）保留为语义别名，
 * 一律落到中性样式，避免调用方传值报错、也避免颜色回潮。
 * ========================================================= */
.tone-down .kpi-icon,
.tone-alert .kpi-icon,
.tone-bad .kpi-icon {
  background: var(--danger-dim);
  color: var(--danger);
}
.tone-down::before,
.tone-alert::before,
.tone-bad::before {
  height: 2px;
  background: var(--danger);
}
.tone-down .kpi-num,
.tone-alert .kpi-num,
.tone-bad .kpi-num {
  color: var(--danger);
}
/* warn 级：琥珀，用于"接近阈值"这类需要看一眼但不是故障的指标 */
.tone-warn .kpi-icon {
  background: var(--warn-dim);
  color: var(--warn);
}
.tone-warn::before {
  height: 2px;
  background: var(--warn);
}
</style>
