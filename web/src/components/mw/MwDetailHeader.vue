<template>
  <div class="mw-detail-head">
    <div class="mdh-top">
      <div class="mdh-title-wrap">
        <MwStatusDot v-if="status" :status="status" :label="statusLabel || null" large />
        <h3 class="mdh-title" :title="title">{{ title }}</h3>
        <span v-if="subtitle" class="mdh-sub">{{ subtitle }}</span>
      </div>
      <div class="mdh-actions">
        <el-radio-group
          v-if="ranges.length"
          :model-value="range"
          size="small"
          @update:model-value="(v) => $emit('update:range', v)"
        >
          <el-radio-button v-for="r in ranges" :key="r.value" :value="r.value">{{ r.label }}</el-radio-button>
        </el-radio-group>
        <slot name="actions" />
      </div>
    </div>

    <div v-if="fields.length" class="mdh-fields">
      <div v-for="(f, i) in fields" :key="f.label || i" class="mdh-field">
        <span class="mdh-label">{{ f.label }}</span>
        <span class="mdh-value" :class="{ mono: f.mono }">{{ f.value ?? '-' }}</span>
      </div>
    </div>
  </div>
</template>

<script setup>
import MwStatusDot from './MwStatusDot.vue'
import { MW_RANGE_OPTIONS } from './mwRange'

// 中间件详情抽屉的统一对象头 + 时间范围。
//
// 此前只有 RedisTab 的抽屉有"对象头 + 时间范围"，其余 14 个 Tab 要么没有、
// 要么各写一份，且时间范围大多写死"近 1 小时"不可改。这里把两者抽成一个头：
//   title/subtitle → 我是谁；status → 健康度；fields → 关键元信息；
//   ranges/range → 可切换的时间窗口（默认 5 档），趋势图据此重查。
defineProps({
  title: { type: String, default: '' },
  subtitle: { type: String, default: '' },
  // 状态：透传 MwStatusDot（空串则不显示状态点）
  status: { type: String, default: '' },
  statusLabel: { type: String, default: '' },
  // 元信息字段：[{ label, value, mono? }]
  fields: { type: Array, default: () => [] },
  // 时间范围选项：[{ label, value }]，空数组则不显示切换器
  ranges: { type: Array, default: () => MW_RANGE_OPTIONS },
  // 当前选中的范围值（配合 v-model:range）
  range: { type: String, default: '1h' },
})
defineEmits(['update:range'])
</script>

<style scoped>
.mw-detail-head {
  margin-bottom: var(--sp-4);
}
.mdh-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--sp-3);
  flex-wrap: wrap;
}
.mdh-title-wrap {
  display: flex;
  align-items: center;
  gap: var(--sp-2);
  min-width: 0;
}
.mdh-title {
  font-size: var(--fs-lg);
  font-weight: 600;
  color: var(--t1);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.mdh-sub {
  font-size: var(--fs-sm);
  color: var(--t3);
  font-family: var(--mono);
}
.mdh-actions {
  display: flex;
  align-items: center;
  gap: var(--sp-2);
  flex-shrink: 0;
  flex-wrap: wrap;
}
.mdh-fields {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
  gap: var(--sp-2);
  margin-top: var(--sp-3);
}
.mdh-field {
  background: var(--fill-1);
  border-radius: var(--r-md);
  padding: 8px 10px;
  font-size: var(--fs-sm);
  display: flex;
  align-items: baseline;
  gap: 8px;
  min-width: 0;
}
.mdh-label {
  color: var(--t3);
  font-size: var(--fs-xs);
  flex-shrink: 0;
}
.mdh-value {
  color: var(--t1);
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.mdh-value.mono {
  font-family: var(--mono);
}
</style>
