<template>
  <!-- 单根元素：根前放注释会让组件变成多根，外部透传的属性/类会失配 -->
  <div
    v-if="triage"
    class="triage"
    role="alert"
    :class="'sev-' + (triage.severity || 'warn')"
  >
    <!-- role="alert"：这条是"最该处理的一件事"，出现/变化时读屏应立即播报，
         而不是等用户逐个区块扫过去才发现。 -->
    <span class="triage-badge">研判</span>
    <div class="triage-text">
      <b>{{ triage.headline }}</b>
      <span>{{ triage.reason }}</span>
      <span v-if="triage.suggestion">建议：{{ triage.suggestion }}</span>
    </div>
    <el-button class="triage-btn" size="small" @click="goAlerts">查看详情</el-button>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { buildTriage } from '../../composables/useTriage'

const props = defineProps({
  alerts: { type: Array, default: () => [] },
  nodes: { type: Array, default: () => [] },
  latestMap: { type: Object, default: () => ({}) },
})

const router = useRouter()

// 无告警时返回 null，整条不渲染
const triage = computed(() => buildTriage(props.alerts, props.nodes, props.latestMap))

// 带着这条研判告警的 node / rule 跳转：告警中心据此直接过滤，
// 用户不用在几百条里再自己找一遍。
function goAlerts() {
  const a = (triage.value && triage.value.alert) || {}
  const query = {}
  if (a.node) query.node = String(a.node)
  if (a.ruleName) query.rule = String(a.ruleName)
  router.push({ path: '/alerts', query })
}
</script>


<style scoped>
.triage {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 14px;
  margin-bottom: 16px;
  border-radius: var(--r-md);
  background: var(--danger-dim);
  border: 1px solid var(--danger-bd);
  border-left: 3px solid var(--danger);
}
.triage.sev-warning {
  background: var(--warn-dim);
  border-color: var(--warn-bd);
  border-left-color: var(--warn);
}
.triage.sev-info {
  background: var(--info-dim);
  border-color: var(--info-bd);
  border-left-color: var(--info);
}
.triage-badge {
  flex-shrink: 0;
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0.06em;
  padding: 2px 6px;
  border-radius: var(--r-xs);
  background: var(--danger);
  color: var(--s0);
}
.triage.sev-warning .triage-badge {
  background: var(--warn);
}
.triage.sev-info .triage-badge {
  background: var(--info);
}
.triage-text {
  flex: 1;
  min-width: 0;
  font-size: var(--fs-sm);
  color: var(--t2);
  line-height: 1.6;
}
.triage-text b {
  color: var(--t1);
  font-weight: 600;
  margin-right: 6px;
}
.triage-text span + span {
  margin-left: 6px;
}
.triage-btn {
  flex-shrink: 0;
}
</style>
