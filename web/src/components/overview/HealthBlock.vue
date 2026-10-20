<template>
  <div class="health-block">
    <div class="hb-top">
      <div class="ring" :class="'score-' + rank(health.score)" :style="{ '--p': clamp(health.score) }">
        <div class="ring-inner">
          <span class="score">{{ Math.round(health.score) }}</span>
        </div>
      </div>
      <div class="hb-meta">
        <div class="hb-status" :class="'c-' + rank(health.score)">{{ health.statusText }}</div>
        <div class="hb-sub">
          在线 {{ health.online }} / 共 {{ health.total }}
          <template v-if="health.offline"> · <span class="off">离线 {{ health.offline }}</span></template>
        </div>
      </div>
    </div>

    <!-- 四个构成项：按行列出各自扣了多少分。
         与「紧急告警」同处一行时这张卡会被拉高；明细行用 space-evenly 铺开，
         既填满高度，也解释「分数为什么是 0」。 -->
    <div class="hb-breakdown">
      <div v-for="f in facts" :key="f.key" class="hb-row" :data-key="f.key">
        <i class="hb-dot" :class="'dot-' + f.tone"></i>
        <span class="hb-label">{{ f.label }}</span>
        <span class="hb-value" :title="f.value">{{ f.value }}</span>
        <span class="hb-deduct" :class="f.deduct > 0 ? 'c-' + f.tone : 'c-muted'">
          {{ f.deduct > 0 ? '−' + f.deduct : '0' }}
        </span>
      </div>
    </div>
    <p class="hb-formula">健康度 = 100 − 四项扣分（最低 0）</p>
  </div>
</template>

<script setup>
import { computed } from 'vue'

const props = defineProps({
  health: { type: Object, required: true },
})

const RANK = { muted: 0, ok: 1, warn: 2, danger: 3 }
const worse = (a, b) => (RANK[a] >= RANK[b] ? a : b)

// 四个构成项与 healthScore.js 的扣分口径一致：
//   可用性＝在线率；性能＝CPU/内存均值是否超阈值；容量＝磁盘均值；安全＝告警级别分布
const facts = computed(() => {
  const h = props.health || {}
  const total = h.total || 0
  const offline = h.offline || 0
  const avail = total ? ((total - offline) / total) * 100 : 0
  const pressure = h.pressure || []
  const byKey = (k) => pressure.find((p) => p.key === k) || null
  const cpu = byKey('cpu')
  const mem = byKey('mem')
  const disk = byKey('disk')

  const toneOf = (p) =>
    !p || !p.count ? 'muted' : p.badCount > 0 ? 'danger' : p.warnCount > 0 ? 'warn' : 'ok'
  const pct = (p) => (p && p.count ? Math.round(p.rate) + '%' : '—')

  const crit = h.criticalAlerts || 0
  const warn = h.warningAlerts || 0

  // 扣分口径与 healthScore.js 一致：
  //   可用性扣 60×离线率；性能/容量按超阈值幅度×权重；安全按 critical×5 + warning×2。
  // 这里单独算一份是为了把「分数从哪来」显示出来，不改变总分口径。
  const deductOf = (p) => (p && p.count ? Math.round(((p.avgOver || 0) / 100) * (p.weight || 0)) : 0)
  const availDeduct = total ? Math.round((offline / total) * 60) : 0
  const secDeduct = Math.min(100, crit * 5 + warn * 2)

  return [
    {
      key: 'avail',
      label: '可用性',
      value: total ? avail.toFixed(1) + '%' : '—',
      tone: !total ? 'muted' : offline === 0 ? 'ok' : offline / total >= 0.3 ? 'danger' : 'warn',
      deduct: availDeduct,
    },
    {
      key: 'perf',
      label: '性能',
      value: `CPU ${pct(cpu)} · 内存 ${pct(mem)}`,
      tone: worse(toneOf(cpu), toneOf(mem)),
      deduct: deductOf(cpu) + deductOf(mem),
    },
    {
      key: 'capacity',
      label: '容量',
      value: `磁盘 ${pct(disk)}`,
      tone: toneOf(disk),
      deduct: deductOf(disk),
    },
    {
      key: 'security',
      label: '安全',
      value: crit > 0 ? `${crit} 条紧急` : warn > 0 ? `${warn} 条警告` : '无告警',
      tone: crit > 0 ? 'danger' : warn > 0 ? 'warn' : 'ok',
      deduct: secDeduct,
    },
  ]
})

function clamp(v) {
  const n = Number(v)
  if (isNaN(n)) return 0
  return Math.max(0, Math.min(100, n))
}
function rank(score) {
  const s = Number(score)
  if (isNaN(s)) return 'unknown'
  if (s >= 90) return 'good'
  if (s >= 70) return 'warn'
  return 'bad'
}
</script>

<style scoped>
.health-block {
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  gap: 14px;
  height: 100%;
  min-height: 120px;
}
.hb-top {
  display: flex;
  align-items: center;
  gap: 16px;
}
.ring {
  --p: 0;
  width: 88px;
  height: 88px;
  border-radius: 50%;
  background: conic-gradient(var(--ring-color) calc(var(--p) * 1%), var(--s2) 0);
  display: flex;
  align-items: center;
  justify-content: center;
  position: relative;
  flex-shrink: 0;
}
/* 内圆 9px 收缩 = 9px 环宽；底色必须与卡片一致（s1），否则会看出一个"洞" */
.ring::before {
  content: '';
  position: absolute;
  inset: 9px;
  background: var(--s1);
  border-radius: 50%;
}
.ring-inner {
  position: relative;
  display: flex;
  align-items: baseline;
  gap: 2px;
}
.score {
  font-size: var(--fs-3xl);
  font-weight: 800;
  line-height: 1;
  font-family: var(--mono);
  letter-spacing: -0.02em;
}
.ring.score-good {
  --ring-color: var(--ok);
}
.ring.score-warn {
  --ring-color: var(--warn);
}
.ring.score-bad {
  --ring-color: var(--danger);
}
.ring.score-unknown {
  --ring-color: var(--t3);
}
.hb-meta {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
}
.hb-status {
  font-size: var(--fs-lg);
  font-weight: 700;
}
.hb-status.c-good {
  color: var(--ok);
}
.hb-status.c-warn {
  color: var(--warn);
}
.hb-status.c-bad {
  color: var(--danger);
}
.hb-status.c-unknown {
  color: var(--t3);
}
.hb-sub {
  font-size: var(--fs-sm);
  color: var(--t2);
}
.hb-sub .off {
  color: var(--danger);
}
.hb-breakdown {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  justify-content: space-evenly;
}
.hb-row {
  display: grid;
  grid-template-columns: 8px 48px minmax(0, 1fr) auto;
  align-items: center;
  gap: 8px;
  padding: 6px 0;
  border-top: 1px solid var(--bd);
}
.hb-row:first-child {
  border-top: none;
}
.hb-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--t3);
}
.dot-ok {
  background: var(--ok);
}
.dot-warn {
  background: var(--warn);
}
.dot-danger {
  background: var(--danger);
}
.dot-muted {
  background: var(--t3);
}
.hb-label {
  font-size: var(--fs-sm);
  color: var(--t2);
}
.hb-value {
  font-size: var(--fs-sm);
  color: var(--t1);
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.hb-deduct {
  font-family: var(--mono);
  font-size: var(--fs-sm);
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}
.c-ok {
  color: var(--ok);
}
.c-warn {
  color: var(--warn);
}
.c-danger {
  color: var(--danger);
}
.c-muted {
  color: var(--t3);
}
.hb-formula {
  margin: 0;
  font-size: var(--fs-xs);
  color: var(--t3);
}
</style>
