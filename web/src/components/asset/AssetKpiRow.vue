<!--
  资产台账 · 健康度卡片行。

  可下钻的卡片用**真正的 <button>** 而不是 `div + @click`：键盘可达、有焦点环、
  读屏能读出"按钮"。此前是 div，触屏与键盘都到不了，下钻成了鼠标专属功能。
-->
<template>
  <div class="kpi-row">
    <button type="button" class="kpi-click" title="清空筛选，查看全部资产" @click="$emit('drill-all')">
      <KpiCard :value="summary.total" label="资产总数" hint="点击下钻" tone="total">
        <template #icon><el-icon :size="20"><Files /></el-icon></template>
      </KpiCard>
    </button>
    <button type="button" class="kpi-click" title="只看超过 30 分钟未上报的资产" @click="$emit('drill-status', 'missing')">
      <KpiCard :value="summary.missing" label="失联" hint="超 30 分钟未上报" :tone="summary.missing > 0 ? 'down' : 'total'">
        <template #icon><el-icon :size="20"><WarningFilled /></el-icon></template>
      </KpiCard>
    </button>
    <button type="button" class="kpi-click" title="只看人工未指派责任人的资产" @click="$emit('drill-no-owner')">
      <KpiCard :value="summary.noOwner" label="无责任人" hint="人工值未指派" tone="ops">
        <template #icon><el-icon :size="20"><UserFilled /></el-icon></template>
      </KpiCard>
    </button>
    <button type="button" class="kpi-click" title="只看同一字段人工值与采集值并存的资产" @click="$emit('drill-conflict')">
      <KpiCard :value="summary.conflict" label="人工 / 采集冲突" hint="同字段双值不一致" tone="mem">
        <template #icon><el-icon :size="20"><Switch /></el-icon></template>
      </KpiCard>
    </button>
    <button type="button" class="kpi-click" title="只看已从台账隐藏（忽略）的资产；采集仍在继续，可逐个恢复" @click="$emit('drill-ignored')">
      <KpiCard :value="summary.ignored" label="已忽略" hint="已从台账隐藏" tone="conn">
        <template #icon><el-icon :size="20"><Hide /></el-icon></template>
      </KpiCard>
    </button>
    <!-- 「近 7 天变更」不可下钻：保留卡片样式但不给手型，也不是按钮（避免出现一个点了没反应的按钮） -->
    <div class="kpi-click kpi-static">
      <KpiCard :value="summary.changes" label="近 7 天变更" hint="含采集与人工" tone="conn">
        <template #icon><el-icon :size="20"><DataLine /></el-icon></template>
      </KpiCard>
    </div>
  </div>
</template>

<script setup>
import KpiCard from '../KpiCard.vue'
import { Files, WarningFilled, UserFilled, Switch, Hide, DataLine } from '@element-plus/icons-vue'

defineProps({
  summary: { type: Object, default: () => ({}) },
})
defineEmits(['drill-all', 'drill-status', 'drill-no-owner', 'drill-conflict', 'drill-ignored'])
</script>

<style scoped>
/* 健康度卡片行：复用全局 KpiCard，与中间件 / 容器等页面同一视觉语言 */
.kpi-row {
  display: grid;
  /* 150px 下限保证 1080 宽的窗口下 5 张卡也在同一行，不会落单一张 */
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
  gap: 12px;
  margin-bottom: 12px;
}
.kpi-click {
  /* 用按钮承载下钻：清掉浏览器默认外观，让它看起来仍是一张卡 */
  display: flex;
  width: 100%;
  padding: 0;
  border: none;
  background: none;
  font: inherit;
  color: inherit;
  text-align: left;
  cursor: pointer;
}
.kpi-click:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
  border-radius: var(--r-lg);
}
/* 卡片为子组件根节点：需要穿透一层才能撑满网格单元 */
.kpi-click :deep(.kpi-card) {
  width: 100%;
}
/* 「近 7 天变更」不可下钻，保留卡片样式但不给手型 */
.kpi-static {
  cursor: default;
}
</style>
