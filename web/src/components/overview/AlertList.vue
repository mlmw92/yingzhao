<template>
  <div class="alert-list">
    <!-- 行语义化：此前是 <div @click>，键盘完全够不着。role=button + tabindex + Enter/Space
         让「跳到告警中心看这条」对键盘用户同样可用。 -->
    <div
      v-for="(a, i) in alerts"
      :key="a.id || `${a.ruleId || a.ruleName}-${a.startsAt || i}`"
      class="al-item"
      role="button"
      tabindex="0"
      :title="`在告警中心查看：${a.ruleName || '未命名告警'}`"
      @click="open(a)"
      @keydown.enter.prevent="open(a)"
      @keydown.space.prevent="open(a)"
    >
      <span class="al-dot" :class="'sev-' + (a.severity || '').toLowerCase()"></span>
      <div class="al-body">
        <div class="al-title">{{ a.ruleName || '未命名告警' }}</div>
        <div v-if="showMessage && a.message" class="al-message" :title="a.message">{{ a.message }}</div>
        <div class="al-sub">
          {{ a.node }}<template v-if="showIp && a.nodeIp"> ({{ a.nodeIp }})</template> · {{ timeAgo(a.startsAt) }}
        </div>
      </div>
      <StatusPill v-if="showState" :tone="stateTone(a.state)" :text="stateText(a.state)" />
    </div>

    <div v-if="!alerts || alerts.length === 0" class="al-empty">
      <template v-if="variant === 'recent' && dedupedCount > 0">
        <span class="al-ok">↑</span> 最近 {{ dedupedCount }} 条告警已在上方「紧急告警」中显示
      </template>
      <template v-else-if="variant === 'critical'">
        <span class="al-ok">✓</span> 当前无紧急告警
      </template>
      <template v-else>
        <span class="al-ok">✓</span> 暂无告警记录
      </template>
    </div>
    <div v-else-if="variant === 'recent' && dedupedCount > 0" class="al-dedup">
      已在「紧急告警」中显示的 {{ dedupedCount }} 条不重复列出
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import StatusPill from '../common/StatusPill.vue'

/* 首页两个告警区块（紧急告警 / 最近告警）此前是两份几乎逐行相同的实现：
 * timeAgo / goAlerts / .dot.sev-* 三套都各写一遍。这里收敛为一个参数化组件，
 * variant 决定「是否显示 message / 状态药丸 / 空态文案」，行为与视觉保持一份。 */
const props = defineProps({
  alerts: { type: Array, default: () => [] },
  // 'critical' = 紧急告警；'recent' = 最近告警
  variant: { type: String, default: 'recent' },
  // 因与「紧急告警」重复而被剔除的条数（§33 #7 去重）；0 表示未去重
  dedupedCount: { type: Number, default: 0 },
})

const router = useRouter()

const showMessage = computed(() => props.variant === 'recent')
const showState = computed(() => props.variant === 'recent')
const showIp = computed(() => props.variant === 'critical')

function timeAgo(t) {
  if (!t) return ''
  const d = new Date(t)
  if (isNaN(d)) return ''
  const s = Math.floor((Date.now() - d.getTime()) / 1000)
  if (s < 60) return s + ' 秒前'
  if (s < 3600) return Math.floor(s / 60) + ' 分钟前'
  if (s < 86400) return Math.floor(s / 3600) + ' 小时前'
  return Math.floor(s / 86400) + ' 天前'
}

function stateText(s) {
  if (s === 'active' || s === 'firing') return '告警中'
  if (s === 'acked') return '已确认'
  if (s === 'resolved') return '已恢复'
  return s || '-'
}
function stateTone(s) {
  if (s === 'active' || s === 'firing') return 'danger'
  if (s === 'acked') return 'warn'
  if (s === 'resolved') return 'ok'
  return 'muted'
}

// 点某一行直接带着这条告警的 node / rule 跳转，而不是丢到全量列表里让用户自己找
function open(a) {
  const query = {}
  if (a && a.node) query.node = String(a.node)
  if (a && a.ruleName) query.rule = String(a.ruleName)
  router.push({ path: '/alerts', query })
}
</script>

<style scoped>
.alert-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
  height: 100%;
  overflow: auto;
}
.al-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 9px 11px;
  background: var(--s2);
  border-radius: var(--r-md);
  cursor: pointer;
  transition: background 0.2s;
}
.al-item:hover {
  background: var(--fill-3);
}
.al-item:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 1px;
}
.al-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
}
.sev-critical {
  background: var(--danger);
  box-shadow: 0 0 8px var(--danger-dim);
}
.sev-warning {
  background: var(--warn);
}
.sev-info {
  background: var(--info);
}
.al-body {
  flex: 1;
  min-width: 0;
}
.al-title {
  font-size: var(--fs-sm);
  font-weight: 600;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.al-message {
  margin-top: 2px;
  font-size: var(--fs-sm);
  color: var(--t2);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.al-sub {
  font-size: var(--fs-sm);
  color: var(--t2);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.al-empty {
  display: flex;
  align-items: center;
  gap: 8px;
  justify-content: center;
  height: 100%;
  padding: 30px;
  color: var(--ok);
  font-size: var(--fs-sm);
}
.al-ok {
  font-weight: 700;
}
.al-dedup {
  padding: 4px 2px 0;
  font-size: var(--fs-sm);
  color: var(--t3);
  text-align: right;
}
</style>
