<!--
  资产台账 · 筛选区。

  两件事：
  ① **折叠**：6 个下拉/输入 + 1 个复选框平铺太挤，默认只留「类型 / 状态 / 关键词」，
     其余收进「更多筛选」；
  ② **摘要条**：把当前生效的筛选（含点 KPI 下钻产生的条件）汇总成可逐条撤销的 chips ——
     此前 drillStatus/drillIgnored 没有可见出口，"列表为什么变少"没人说得清。
-->
<template>
  <div class="filter-wrap">
    <div class="filter-bar">
      <div class="field">
        <span class="field-label">类型</span>
        <el-select v-model="filter.type" placeholder="全部类型" clearable size="small" class="w-s">
          <!-- 类型清单来自 GET /api/v1/asset-types（配置项模型），加类型不必再改这里；
               容器与工作负载是 K8s 清单上报的产物，**默认不计入上方数字**，选中才出现在列表里。 -->
          <el-option v-for="t in assetTypes" :key="t.key" :label="t.title" :value="t.key" />
        </el-select>
      </div>
      <div class="field">
        <span class="field-label">状态</span>
        <el-select v-model="filter.status" placeholder="全部上报状态" clearable size="small" class="w-s">
          <el-option label="上报正常" value="online" />
          <el-option label="失联" value="missing" />
          <el-option label="归档" value="archived" />
        </el-select>
      </div>
      <el-input
        v-model="filter.keyword"
        clearable
        size="small"
        placeholder="搜索资产名 / 自然键 / 属性值"
        class="w-l"
        @keyup.enter="$emit('reload')"
      >
        <template #prefix><el-icon><Search /></el-icon></template>
      </el-input>
      <el-button type="primary" size="small" :loading="loading" @click="$emit('reload')">查询</el-button>
      <el-button size="small" @click="$emit('reset')">重置</el-button>
      <el-button link size="small" @click="expanded = !expanded">
        {{ expanded ? '收起筛选' : '更多筛选' }}
        <el-icon :size="12"><component :is="expanded ? ArrowUp : ArrowDown" /></el-icon>
      </el-button>

      <div v-show="expanded" class="more">
        <div class="field">
          <span class="field-label">来源</span>
          <el-select v-model="filter.source" placeholder="全部来源" clearable size="small" class="w-s">
            <el-option label="自动" value="auto" />
            <el-option label="人工" value="manual" />
            <el-option label="混合" value="mixed" />
          </el-select>
        </div>
        <div class="field">
          <span class="field-label">归属节点</span>
          <el-input
            v-model="filter.node"
            clearable
            size="small"
            placeholder="如 web-01"
            class="w-s"
            @keyup.enter="$emit('reload')"
          />
        </div>
        <div class="field">
          <span class="field-label">标签</span>
          <el-input
            v-model="filter.label"
            clearable
            size="small"
            placeholder="如 env:prod 或 env"
            class="w-s"
            title="按标签筛选：key:value 精确匹配值；只填 key 表示「存在该标签」"
            @keyup.enter="$emit('reload')"
          />
        </div>
        <el-checkbox
          :model-value="showIgnored"
          title="连已忽略的资产一起显示（它们带「已忽略」标记，可在这里恢复）"
          @update:model-value="$emit('update:showIgnored', $event)"
        >
          含已忽略
        </el-checkbox>
      </div>
    </div>

    <FilterChips
      v-if="chips.length"
      class="filter-chips-row"
      :chips="chips"
      @remove="$emit('remove-chip', $event)"
      @clear="$emit('reset')"
    />

    <!-- 把"数字里为什么没有容器"说清楚：否则用户看到集群里明明有几十个 Pod、
         顶部却只显示 12 个资产，只会怀疑数字算错。 -->
    <p class="muted">
      容器与工作负载不计入上方数字：它们是滚动更新、扩缩容产生的短命对象，
      计入后「失联」会被替换掉的旧 Pod 填满、失去意义。把「类型」选成容器（Pod）或工作负载即可查看。
    </p>
  </div>
</template>

<script setup>
import { computed, ref } from 'vue'
import { Search, ArrowDown, ArrowUp } from '@element-plus/icons-vue'
import FilterChips from '../common/FilterChips.vue'
import { statusLabel, sourceLabel } from './assetFormat'

const props = defineProps({
  filter: { type: Object, required: true },
  drill: { type: Object, required: true },
  assetTypes: { type: Array, default: () => [] },
  showIgnored: { type: Boolean, default: false },
  loading: { type: Boolean, default: false },
})
defineEmits(['reload', 'reset', 'clear-drill', 'update:showIgnored', 'remove-chip'])

const expanded = ref(false)

const typeTitle = (key) => {
  const hit = props.assetTypes.find((t) => t.key === key)
  return hit ? hit.title : key
}

// chips 的 key 带上维度前缀：filter.* 与 drill.* 分开，父组件按 key 精确清除，
// 不必再猜"这个 'ignored' 到底是筛选里的还是下钻里的"。
const chips = computed(() => {
  const f = props.filter
  const d = props.drill
  const out = []
  if (f.type) out.push({ key: 'filter.type', label: '类型', value: typeTitle(f.type) })
  if (f.status) out.push({ key: 'filter.status', label: '状态', value: statusLabel(f.status) })
  if (f.source) out.push({ key: 'filter.source', label: '来源', value: sourceLabel(f.source) })
  if (f.node) out.push({ key: 'filter.node', label: '归属节点', value: f.node })
  if (f.keyword) out.push({ key: 'filter.keyword', label: '关键词', value: f.keyword })
  if (f.label) out.push({ key: 'filter.label', label: '标签', value: f.label })
  if (f.ignored === 'with') out.push({ key: 'filter.ignored', label: '范围', value: '含已忽略' })
  if (d.ownerMissing) out.push({ key: 'drill.ownerMissing', label: '下钻', value: '无责任人' })
  if (d.conflict) out.push({ key: 'drill.conflict', label: '下钻', value: '人工/采集冲突' })
  if (d.ignored) out.push({ key: 'drill.ignored', label: '下钻', value: '已忽略' })
  return out
})
</script>

<style scoped>
.field {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.field-label {
  font-size: var(--fs-sm);
  color: var(--t3);
  white-space: nowrap;
}
/* 折叠区：独占一行，避免与上面三个控件挤在一起换行错乱 */
.more {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  flex-basis: 100%;
}
.filter-chips-row {
  margin-bottom: var(--desc-base);
}
/* 说明段：筛选条与下方工具栏之间的过渡说明 */
p.muted {
  margin-bottom: var(--desc-base);
}
</style>
