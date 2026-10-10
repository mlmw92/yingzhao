<template>
  <section class="intelligence-block">
    <!-- 标题由外层统一渲染（首页概览为每个区块画标题），这里只放工具行：
         此前组件自带 eyebrow + 「智能运维」标题，于是同一个标题在卡片里出现两次，
         组件又自带 padding/边框/圆角嵌在外层卡片内，形成双层边框。 -->
    <div class="block-header">
      <span class="update-time">{{ updatedAt }}</span>
      <div class="header-actions">
        <el-button text type="primary" @click="refresh">刷新分析</el-button>
        <el-button text type="primary" @click="$router.push('/intelligence')">查看详情</el-button>
      </div>
    </div>
    <div v-loading="loading" class="intelligence-body">
      <div v-if="error" class="intel-error">
        <el-icon><WarningFilled /></el-icon>
        <span>{{ error }}</span>
        <el-button size="small" @click="refresh">重试</el-button>
      </div>
      <template v-else-if="summary">
        <div class="signal-grid">
          <div class="signal-card risk"><span>需关注资源</span><strong>{{ summary.riskCount }}</strong><small>按综合风险排序</small></div>
          <div class="signal-card anomaly"><span>持续异常</span><strong>{{ summary.anomalyCount }}</strong><small>偏离动态基线</small></div>
          <div class="signal-card capacity"><span>容量紧迫</span><strong>{{ summary.urgentCapacityCount }}</strong><small>预计 7 天内耗尽</small></div>
          <div class="signal-card coverage"><span>数据已就绪</span><strong>{{ summary.readyNodeCount || 0 }}</strong><small>共 {{ summary.nodeCount || 0 }} 个节点</small></div>
        </div>
        <div class="readiness" :class="{ ready: summary.readyNodeCount === summary.nodeCount && summary.nodeCount > 0 }"><span>{{ summary.readyNodeCount === summary.nodeCount && summary.nodeCount > 0 ? '数据已就绪' : '仍在采集' }}</span><p>{{ readinessText }}</p></div>
        <div v-if="summary.hosts?.length" class="top-risk">
          <div class="risk-label"><span class="pulse"></span>最高优先级</div>
          <div class="risk-main"><strong>{{ topHost.node }}<em v-if="topHost.ip" class="host-ip">{{ topHost.ip }}</em></strong><span :class="['severity', topHost.severity]">{{ severityText(topHost.severity) }}</span></div>
          <p>{{ topHost.evidence?.[0]?.title || '历史指标处于稳定范围内' }}</p>
        </div>
      </template>
      <el-empty v-else-if="!loading" description="暂无可用于智能分析的历史指标" :image-size="62" />
    </div>
  </section>
</template>

<script setup>
import { computed, onMounted, ref, watch } from 'vue'
import { WarningFilled } from '@element-plus/icons-vue'
import http from '../../api/http'

// 向外层（首页概览）通报"是否出现 critical"：区块默认收起，一旦有 critical 自动展开
const emit = defineEmits(['signal'])

const summary = ref(null)
const loading = ref(false)
const error = ref('')
const updatedAt = computed(() => summary.value?.generatedAt ? new Date(summary.value.generatedAt).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' }) + ' 更新' : '')
const readinessText = computed(() => {
  const total = summary.value?.nodeCount || 0
  const ready = summary.value?.readyNodeCount || 0
  if (!total) return '等待主机历史指标进入分析窗口'
  return ready === total ? '关键指标历史样本充足，分析结论可用' : `${total - ready} 个节点仍需积累关键指标历史样本`
})
function severityText(value) { return ({ critical: '紧急', warning: '关注', info: '稳定' })[value] || '未知' }
const topHost = computed(() => summary.value?.hosts?.[0] || null)

watch(summary, (s) => {
  const critical = !!(s && (s.hosts || []).some((h) => h.severity === 'critical'))
  emit('signal', { critical })
})

async function refresh() {
  loading.value = true
  try {
    summary.value = await http.analysisSummary(168, true)
    error.value = ''
  } catch (e) {
    // 失败必须说出来：此前只有 console.error，界面停在上一份（或空）数据上，看起来"分析没结论"
    error.value = (e && e.message) || '智能分析摘要加载失败'
  } finally {
    loading.value = false
  }
}
onMounted(refresh)
</script>

<style scoped>
/* 不再自带卡片外观：这里嵌在外层 .ov-card 内，容器（底色/描边/圆角/内边距）由外层提供 */
.intelligence-block {
  min-height: 260px;
}
.block-header,
.header-actions,
.risk-main {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.update-time {
  color: var(--t3);
  font-size: var(--fs-xs);
}
.intelligence-body {
  min-height: 174px;
}
.intel-error {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 12px;
  border: 1px solid var(--danger-bd);
  background: var(--danger-dim);
  border-radius: var(--r-md);
  font-size: var(--fs-sm);
  color: var(--t1);
}
.intel-error .el-icon {
  color: var(--danger);
  flex-shrink: 0;
}
.signal-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 10px;
  margin-top: 20px;
}
.signal-card {
  padding: 13px;
  border-radius: 12px;
  background: var(--fill-1);
  border: 1px solid var(--bd);
}
.signal-card span,
.signal-card small {
  display: block;
  color: var(--t3);
  font-size: var(--fs-xs);
}
.signal-card strong {
  display: block;
  margin: 6px 0 4px;
  color: var(--t1);
  font-size: 25px;
  line-height: 1;
}
.risk strong {
  color: var(--warn);
}
.anomaly strong {
  color: var(--warn);
}
.capacity strong {
  color: var(--danger);
}
.coverage strong {
  color: var(--ok);
}
.readiness {
  display: flex;
  gap: 8px;
  align-items: center;
  margin-top: 12px;
  padding: 8px 10px;
  border-radius: 8px;
  background: var(--warn-dim);
  color: var(--warn);
  font-size: var(--fs-xs);
}
.readiness.ready {
  background: var(--ok-dim);
  color: var(--ok);
}
.readiness p {
  margin: 0;
  color: var(--t3);
}
.top-risk {
  margin-top: 14px;
  padding: 12px 14px;
  border-radius: 10px;
  background: var(--fill-2);
}
.risk-label {
  color: var(--t3);
  font-size: var(--fs-xs);
}
.risk-main {
  justify-content: flex-start;
  margin-top: 4px;
  color: var(--t1);
}
.top-risk p {
  margin: 4px 0 0;
  color: var(--t3);
  font-size: var(--fs-sm);
}
.severity {
  padding: 2px 7px;
  border-radius: var(--r-pill);
  font-size: var(--fs-xs);
}
.severity.critical {
  color: var(--danger);
  background: var(--danger-dim);
}
.severity.warning {
  color: var(--warn);
  background: var(--warn-dim);
}
.severity.info {
  color: var(--ok);
  background: var(--ok-dim);
}
.host-ip {
  font-style: normal;
  font-size: var(--fs-xs);
  font-weight: 400;
  color: var(--t3);
  margin-left: 6px;
}
.pulse {
  display: inline-block;
  width: 7px;
  height: 7px;
  margin-right: 5px;
  border-radius: 50%;
  background: var(--accent);
  box-shadow: 0 0 10px var(--accent);
  animation: pulse 1.8s infinite;
}
@keyframes pulse {
  50% {
    opacity: 0.35;
    transform: scale(0.75);
  }
}
@media (max-width: 768px) {
  .signal-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
</style>
