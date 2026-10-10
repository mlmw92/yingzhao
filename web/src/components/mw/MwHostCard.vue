<template>
  <div class="mw-host" :class="{ 'is-dim': dim, 'is-selected': selected }" @click="$emit('select')">
    <div class="mwh-head">
      <MwStatusDot :status="status" dot-only />
      <div class="mwh-id">
        <span class="mwh-name" :title="name">{{ name }}</span>
        <span v-if="sub" class="mwh-sub" :title="sub">{{ sub }}</span>
      </div>
      <span v-if="countText" class="mwh-count">{{ countText }}</span>
      <div v-if="$slots.actions" class="mwh-actions" @click.stop>
        <slot name="actions" />
      </div>
    </div>
    <div v-if="$slots.default" class="mwh-body">
      <slot />
    </div>
    <div v-else-if="emptyText" class="mwh-body mwh-empty">{{ emptyText }}</div>
  </div>
</template>

<script setup>
import MwStatusDot from './MwStatusDot.vue'

// 中间件主机卡：统一 Docker / K8s / Redis 三套各自手写的主机块。
// 此前三处各写一遍"状态点 + 名称 + 副名 + 计数"，状态点还各用各的颜色
// （--ok / --accent / --danger 混用），现在统一走 MwStatusDot 的语义色。
defineProps({
  // 主标题：IP 或节点名
  name: { type: String, default: '' },
  // 副标题：节点名 / 实例标识
  sub: { type: String, default: '' },
  // 状态：直接透传 MwStatusDot（normal/warning/abnormal 及其别名）
  status: { type: String, default: 'normal' },
  // 右侧计数文案（如「3 容器」「12 Pod」），为空则不显示
  countText: { type: String, default: '' },
  // 置灰（如主机离线）
  dim: { type: Boolean, default: false },
  // 选中态
  selected: { type: Boolean, default: false },
  // 无默认插槽时的占位文案
  emptyText: { type: String, default: '' },
})
defineEmits(['select'])
</script>

<style scoped>
.mw-host {
  border: 1px solid var(--bd);
  border-radius: var(--r-lg);
  overflow: hidden;
  background: var(--s1);
  transition: border-color 0.2s, box-shadow 0.2s;
}
.mw-host.is-selected {
  border-color: var(--accent);
  box-shadow: 0 0 0 1px var(--accent-dim);
}
.mw-host.is-dim {
  opacity: 0.6;
}
.mwh-head {
  display: flex;
  align-items: center;
  gap: var(--sp-2);
  padding: 10px 12px;
  font-weight: 600;
}
.mwh-body + .mwh-head,
.mwh-head:not(:last-child) {
  border-bottom: 1px solid var(--bd);
}
.mwh-id {
  display: flex;
  flex-direction: column;
  line-height: 1.3;
  min-width: 0;
}
.mwh-name {
  font-size: var(--fs-sm);
  font-family: var(--mono);
  color: var(--t1);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.mwh-sub {
  font-size: var(--fs-xs);
  color: var(--t3);
  font-family: var(--mono);
  font-weight: 400;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.mwh-count {
  margin-left: auto;
  font-size: var(--fs-sm);
  color: var(--t3);
  font-weight: 400;
  white-space: nowrap;
  flex-shrink: 0;
}
.mwh-actions {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: 6px;
}
.mwh-actions ~ .mwh-count,
.mwh-count + .mwh-actions {
  margin-left: var(--sp-2);
}
.mwh-body {
  padding: 10px 12px;
  display: flex;
  flex-wrap: wrap;
  gap: var(--sp-2);
}
.mwh-empty {
  font-size: var(--fs-sm);
  color: var(--t3);
  padding: 4px 0;
}
</style>
