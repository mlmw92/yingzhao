<template>
  <div class="mw-grid">
    <!-- 加载中先占位：summaries 在数据到达前每个类型都是 total=0，
         直接渲染会把「尚未配置监控」当成结论显示出来，数据到达后再整块翻转。 -->
    <template v-if="loading">
      <div v-for="i in 4" :key="'sk' + i" class="mw-card mw-card-sk">
        <div class="sk sk-row" style="width: 55%"></div>
        <div class="sk" style="height: 64px; margin-top: 14px"></div>
      </div>
    </template>
    <template v-else>
      <!-- 整卡可点 → 用 <button>：此前是 <div @click>，键盘与读屏都够不着 -->
      <button
        v-for="s in summaries"
        :key="s.key"
        type="button"
        class="mw-card"
        :class="{ disabled: s.total === 0 }"
        :title="`前往 ${s.label} 页面`"
        @click="goTab(s.tab)"
      >
        <div class="mw-head">
          <!-- 模板派生类型若未提供图标则不渲染，避免出现坏图 -->
          <img v-if="s.icon" class="mw-icon" :src="s.icon" :alt="s.label" />
          <span class="mw-name">{{ s.label }}</span>
          <span v-if="s.total > 0" class="mw-total">{{ s.total }}</span>
          <span v-else class="mw-badge">未配置</span>
        </div>

        <div v-if="s.total > 0" class="mw-body">
          <div class="mw-donut" :style="donutStyle(s)">
            <div class="mw-donut-inner">
              <span class="mw-online">{{ s.online }}</span>
              <span class="mw-donut-label">在线</span>
            </div>
          </div>
          <div class="mw-meta">
            <div class="mw-stat">
              <span class="mw-stat-val off" v-if="s.offline">{{ s.offline }}</span>
              <span class="mw-stat-val ok" v-else>0</span>
              <span class="mw-stat-label">离线/异常</span>
            </div>
            <div class="mw-top">
              <div class="mw-top-title">Top 实例</div>
              <div v-for="(t, i) in s.topN" :key="i" class="mw-top-row">
                <span class="mw-top-label" :title="t.label">{{ t.label }}</span>
                <span class="mw-top-val">{{ t.valueText }}</span>
              </div>
            </div>
          </div>
        </div>

        <div v-else class="mw-empty">
          <el-icon :size="20"><InfoFilled /></el-icon>
          <!-- 空状态文案可被覆盖：模板类型「已配置但无数据」与内置类型「尚未配置」的排查方向不同 -->
          <div class="mw-empty-title">{{ s.emptyTitle || '尚未配置监控' }}</div>
          <div class="mw-empty-sub">{{ s.emptySub || `点击前往 ${s.label} 页面了解接入方式` }}</div>
        </div>
      </button>
    </template>
  </div>
</template>

<script setup>
import { InfoFilled } from '@element-plus/icons-vue'
import { useRouter } from 'vue-router'

const props = defineProps({
  summaries: { type: Array, default: () => [] },
  // 数据未到达时为 true：此时 summaries 里每个类型都是 total=0，
  // 不加这个标志就会把「尚未配置监控」当成真实结论先显示一遍。
  loading: { type: Boolean, default: false },
})
const router = useRouter()

function donutStyle(s) {
  const onlinePct = s.total > 0 ? (s.online / s.total) * 100 : 0
  return {
    background: `conic-gradient(var(--ok) 0 ${onlinePct}%, var(--danger) ${onlinePct}% 100%)`,
  }
}
function goTab(tab) {
  router.push({ path: '/middleware', query: { tab } })
}
</script>

<style scoped>
.mw-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 14px;
  /* 卡片嵌在区块卡内部（外层已是 --s1），用 s2 亮一级表达层级 */
  --mw-bg: var(--s2);
}
/* 卡内是 64px 甜甜圈 + 14px gap + 文字列，4 列在 1400px 以下就开始挤：
   Top 实例的名称列会被 max-width 截断到只剩几个字。逐级降列而不是一步到单列。 */
@media (max-width: 1280px) {
  .mw-grid { grid-template-columns: repeat(3, 1fr); }
}
@media (max-width: 1024px) {
  .mw-grid { grid-template-columns: repeat(2, 1fr); }
}
@media (max-width: 768px) {
  .mw-grid { grid-template-columns: 1fr; }
}
.mw-card {
  background: var(--mw-bg);
  border: 1px solid var(--bd);
  border-radius: var(--r-lg);
  padding: 14px;
  cursor: pointer;
  transition: transform 0.15s, border-color 0.15s, box-shadow 0.15s;
  display: flex;
  flex-direction: column;
  /* button 复位：整卡可点但仍要保持卡片的排版与文字对齐 */
  text-align: left;
  font: inherit;
  color: inherit;
  width: 100%;
}
.mw-card:hover {
  transform: translateY(-2px);
  border-color: var(--accent);
  box-shadow: var(--sh-3);
}
.mw-card:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
}
.mw-card.disabled {
  opacity: 0.78;
}
.mw-card.disabled:hover {
  transform: none;
  border-color: var(--bd);
  box-shadow: none;
}
.mw-head {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 12px;
}
.mw-icon {
  width: 20px;
  height: 20px;
  object-fit: contain;
}
.mw-name {
  font-size: var(--fs-base);
  font-weight: 700;
  flex: 1;
}
.mw-total {
  font-size: var(--fs-sm);
  color: var(--t2);
  background: var(--s3);
  border-radius: var(--r-pill);
  padding: 1px 8px;
}
.mw-badge {
  font-size: var(--fs-sm);
  font-weight: 600;
  color: var(--t3);
  background: var(--s3);
  border: 1px dashed var(--bd);
  border-radius: var(--r-pill);
  padding: 1px 7px;
  letter-spacing: 0.04em;
}
.mw-body {
  display: flex;
  align-items: center;
  gap: 14px;
}
.mw-donut {
  width: 64px;
  height: 64px;
  border-radius: 50%;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  position: relative;
}
.mw-donut::before {
  content: '';
  position: absolute;
  inset: 6px;
  background: var(--mw-bg);
  border-radius: 50%;
}
.mw-donut-inner {
  position: relative;
  display: flex;
  flex-direction: column;
  align-items: center;
}
.mw-online {
  font-size: var(--fs-lg);
  font-weight: 800;
  line-height: 1;
}
.mw-donut-label {
  font-size: var(--fs-sm);
  color: var(--t2);
}
.mw-meta {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.mw-stat {
  display: flex;
  flex-direction: column;
}
.mw-stat-val {
  font-size: var(--fs-md);
  font-weight: 700;
}
.mw-stat-val.ok {
  color: var(--ok);
}
.mw-stat-val.off {
  color: var(--danger);
}
.mw-stat-label {
  font-size: var(--fs-sm);
  color: var(--t2);
}
.mw-top-title {
  font-size: var(--fs-sm);
  color: var(--t2);
  margin-bottom: 4px;
}
.mw-top-row {
  display: flex;
  justify-content: space-between;
  font-size: var(--fs-sm);
  padding: 1px 0;
}
.mw-top-label {
  color: var(--t1);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 90px;
}
.mw-top-val {
  color: var(--accent);
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}
.mw-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 18px 0 8px;
  color: var(--t3);
  text-align: center;
}
.mw-empty-title {
  font-size: var(--fs-sm);
  font-weight: 600;
  color: var(--t2);
}
.mw-empty-sub {
  font-size: var(--fs-sm);
  color: var(--t3);
  line-height: 1.5;
}
</style>
