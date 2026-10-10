<template>
  <div class="host-overview">
    <div v-if="!groupedHosts || groupedHosts.length === 0" class="host-empty">暂无主机数据</div>
    <div v-for="g in groupedHosts" :key="g.group" class="host-group">
      <div class="group-head">
        <span class="group-name">{{ g.group }}</span>
        <span class="group-count">{{ g.hosts.length }}</span>
        <span class="group-meta">
          在线 {{ countByStatus(g.hosts, 'online') }} · 离线 {{ countByStatus(g.hosts, 'offline') }}
        </span>
      </div>
      <div class="host-grid">
        <el-popover
          v-for="h in g.hosts"
          :key="h.hostname"
          placement="top"
          :width="280"
          trigger="hover"
          :show-after="80"
          :persistent="false"
          popper-class="host-popover"
        >
          <template #reference>
            <!-- 方块是按钮：键盘可达（Tab + Enter），不再是一个纯 <div @click> -->
            <button
              type="button"
              class="host-cell"
              :class="cellCls(h)"
              :title="displayName(h) + ' · ' + (h.ip || h.hostname)"
              @click="goNode(h.hostname)"
            >
              <span class="hc-dot"></span>
              <span class="hc-label">{{ shortName(h) }}</span>
              <!-- 离线用文字角标（非颜色编码）：色盲/黑白打印下也能分辨 -->
              <span v-if="h.status !== 'online'" class="hc-off">离线</span>
            </button>
          </template>
          <div class="hp-host">
            <div class="hp-title">
              <OsIcon :os="h.os" />
              <span class="hp-name">{{ displayName(h) }}</span>
              <span class="hp-status" :class="h.status === 'online' ? 'up' : 'down'">
                {{ h.status === 'online' ? '在线' : '离线' }}
              </span>
            </div>
            <div class="hp-ip">{{ h.ip || h.hostname }}</div>
            <div class="hp-metrics">
              <div class="hp-m">
                <span class="hp-m-label">CPU</span>
                <div class="hp-bar"><div class="hp-bar-fill" :style="barStyle(h.metrics.cpu, 'cpu')"></div></div>
                <span class="hp-m-val">{{ pct(h.metrics.cpu) }}</span>
              </div>
              <div class="hp-m">
                <span class="hp-m-label">内存</span>
                <div class="hp-bar"><div class="hp-bar-fill" :style="barStyle(h.metrics.mem, 'mem')"></div></div>
                <span class="hp-m-val">{{ pct(h.metrics.mem) }}</span>
              </div>
              <div class="hp-m">
                <span class="hp-m-label">磁盘</span>
                <div class="hp-bar"><div class="hp-bar-fill" :style="barStyle(h.metrics.disk, 'disk')"></div></div>
                <span class="hp-m-val">{{ pct(h.metrics.disk) }}</span>
              </div>
            </div>
            <div class="hp-foot">点击主机查看详情 →</div>
          </div>
        </el-popover>
      </div>
    </div>
  </div>
</template>

<script setup>
import { useRouter } from 'vue-router'
import OsIcon from '../OsIcon.vue'

const props = defineProps({
  groupedHosts: { type: Array, default: () => [] },
})
const router = useRouter()

// 阈值单一来源：方块配色与 popover 进度条此前各写一套（85/95/100 vs 70/80/85），
// 同一台机器两处可能给出不同颜色。现在只在这里定义一次：warn 档 = 阈值，bad 档 = 阈值 + 15。
const TH = { cpu: 70, mem: 80, disk: 85 }

function displayName(h) {
  return h.displayName && h.displayName.trim() ? h.displayName : h.hostname
}
// 用于方块内的短名：取 displayName / hostname 前 8 个字符（6 字符会把 web-01 与 web-011 显示成同一个）
function shortName(h) {
  const n = displayName(h) || h.hostname || '?'
  return n.length > 8 ? n.slice(0, 8) : n
}
function pct(v) {
  return v == null || isNaN(v) ? '-' : Math.round(v) + '%'
}
function barStyle(v, key) {
  const n = v == null || isNaN(v) ? 0 : Math.max(0, Math.min(100, v))
  const base = TH[key] || 85
  let color = 'var(--ok)'
  if (n >= base + 15) color = 'var(--danger)'
  else if (n >= base) color = 'var(--warn)'
  return { width: n + '%', background: color }
}
// 根据负载+在线状态决定方块颜色：离线红、任意指标偏高橙、否则绿
function cellCls(h) {
  if (h.status !== 'online') return 'cell-down'
  const m = h.metrics || {}
  if ((m.cpu ?? 0) >= TH.cpu + 15 || (m.mem ?? 0) >= TH.mem + 15 || (m.disk ?? 0) >= TH.disk + 15) return 'cell-bad'
  if ((m.cpu ?? 0) >= TH.cpu || (m.mem ?? 0) >= TH.mem || (m.disk ?? 0) >= TH.disk) return 'cell-warn'
  return 'cell-up'
}
function countByStatus(hosts, st) {
  return hosts.filter((h) => (st === 'online' ? h.status === 'online' : h.status !== 'online')).length
}
function goNode(name) {
  if (name) router.push('/node/' + name)
}
</script>

<style scoped>
.host-overview {
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.group-head {
  display: flex;
  align-items: baseline;
  gap: 10px;
  margin-bottom: 8px;
}
.group-name {
  font-size: var(--fs-sm);
  font-weight: 700;
  color: var(--t1);
}
.group-count {
  font-size: var(--fs-sm);
  color: var(--t2);
  background: var(--s2);
  border-radius: 10px;
  padding: 1px 8px;
}
.group-meta {
  font-size: var(--fs-sm);
  color: var(--t3);
  margin-left: auto;
}
.host-grid {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.host-cell {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 5px 10px;
  background: var(--s2);
  border: 1px solid var(--bd);
  border-radius: 6px;
  cursor: pointer;
  font-size: var(--fs-sm);
  line-height: 1;
  font-family: inherit;
  color: var(--t1);
  user-select: none;
  transition: transform 0.12s, border-color 0.12s, box-shadow 0.12s;
}
.host-cell:hover {
  transform: translateY(-1px);
  border-color: var(--accent);
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.2);
  z-index: 1;
}
.host-cell:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 1px;
}
.hc-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  flex-shrink: 0;
}
.hc-label {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 110px;
  color: var(--t1);
}
.hc-off {
  font-size: var(--fs-xs);
  color: var(--t3);
  border: 1px solid var(--bd);
  border-radius: 4px;
  padding: 0 4px;
  line-height: 1.4;
}
.cell-up {
  border-color: var(--ok-bd);
}
.cell-up .hc-dot {
  background: var(--ok);
  box-shadow: 0 0 6px var(--ok);
}
.cell-warn {
  border-color: var(--warn-bd);
}
.cell-warn .hc-dot {
  background: var(--warn);
  box-shadow: 0 0 6px var(--warn);
}
.cell-bad {
  border-color: var(--danger-bd);
}
.cell-bad .hc-dot {
  background: var(--danger);
  box-shadow: 0 0 6px var(--danger);
}
.cell-down {
  border-color: var(--danger-bd);
  opacity: 0.7;
}
.cell-down .hc-dot {
  background: var(--t3);
  box-shadow: none;
}
.cell-down .hc-label {
  color: var(--t2);
  text-decoration: line-through;
}
.host-empty {
  color: var(--t3);
  text-align: center;
  padding: 30px;
  font-size: var(--fs-sm);
}
</style>

<style>
/* popover 浮层样式（不能 scoped） */
.host-popover {
  padding: 10px 12px !important;
  min-width: 240px;
}
.host-popover .hp-host {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.host-popover .hp-title {
  display: flex;
  align-items: center;
  gap: 8px;
}
.host-popover .hp-name {
  font-size: 14px;
  font-weight: 700;
  flex: 1;
}
.host-popover .hp-status {
  font-size: 13px;
  padding: 1px 7px;
  border-radius: 10px;
  font-weight: 600;
}
.host-popover .hp-status.up {
  color: var(--ok);
  background: var(--ok-dim);
}
.host-popover .hp-status.down {
  color: var(--danger);
  background: var(--danger-dim);
}
.host-popover .hp-ip {
  font-size: 13px;
  color: var(--t2);
  font-family: var(--mono);
}
.host-popover .hp-metrics {
  display: flex;
  flex-direction: column;
  gap: 5px;
  padding-top: 4px;
  border-top: 1px dashed var(--bd);
}
.host-popover .hp-m {
  display: grid;
  grid-template-columns: 32px 1fr 38px;
  align-items: center;
  gap: 6px;
  font-size: 13px;
}
.host-popover .hp-m-label {
  color: var(--t2);
}
.host-popover .hp-bar {
  height: 5px;
  border-radius: 3px;
  background: var(--s2);
  overflow: hidden;
}
.host-popover .hp-bar-fill {
  height: 100%;
  border-radius: 3px;
  transition: width 0.4s ease;
}
.host-popover .hp-m-val {
  text-align: right;
  color: var(--t1);
  font-variant-numeric: tabular-nums;
}
.host-popover .hp-foot {
  font-size: 13px;
  color: var(--accent);
  text-align: right;
  margin-top: 2px;
}
</style>
