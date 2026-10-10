<!--
  资产台账 · 主表格。

  状态表达统一到 StatusPill（上报状态 / 来源），删掉此前的 `.dot + .st-*`、`.src-*`
  两套自绘 + 硬编码色。「彻底删除」不再放在行内「更多」下拉里（唯一不可逆动作与
  「忽略」同处一个菜单，只靠一条分隔线区分）——它移到详情抽屉的 `.danger-zone`。
-->
<template>
  <el-table
    ref="tableRef"
    :data="items"
    v-loading="loading"
    row-key="id"
    :row-class-name="rowClass"
    style="width: 100%"
    @row-click="onRowClick"
    @selection-change="$emit('selection-change', $event)"
  >
    <template #empty>
      <EmptyState :icon="Files" :title="emptyTitle" :hints="emptyHints" />
    </template>
    <!-- 多选列：批量维护的入口。点复选框不应打开详情（那是在选行，不是在"看这一条"）。 -->
    <el-table-column type="selection" width="42" />
    <el-table-column label="资产名称" min-width="220">
      <template #default="{ row }">
        <div class="name">
          {{ row.name || row.naturalKey }}
          <!-- 已忽略的行只在「含已忽略」视图里出现，必须显式标记，否则会被当成正常台账资产 -->
          <span
            v-if="row.ignored"
            class="tag-ignored"
            :title="`已从台账隐藏${row.ignoredBy ? '（' + row.ignoredBy + '）' : ''}${row.ignoreReason ? '：' + row.ignoreReason : ''}`"
          >已忽略</span>
        </div>
        <div class="sub">{{ subtitle(row) }}</div>
        <div v-if="labelList(row).length" class="labels">
          <span v-for="lb in labelList(row)" :key="lb.key" class="label-chip">
            {{ lb.key }}<template v-if="lb.value">:{{ lb.value }}</template>
          </span>
        </div>
      </template>
    </el-table-column>
    <el-table-column label="类型" width="110">
      <template #default="{ row }">
        <span class="type-tag" :class="row.typeKey === 'host' ? 'host' : 'mw'">{{ typeLabel(row) }}</span>
      </template>
    </el-table-column>
    <el-table-column label="归属节点" width="150" show-overflow-tooltip>
      <template #default="{ row }">{{ row.node || '—' }}</template>
    </el-table-column>
    <el-table-column label="上报状态" width="116">
      <template #default="{ row }">
        <!-- 列名与措辞刻意与「实例是否可达」区分开：两者是不同维度，同用"在线"会让人以为自相矛盾 -->
        <StatusPill
          :tone="STATUS_TONE[row.status] || 'muted'"
          dot
          :text="statusLabel(row.status)"
          :title="`上报正常 = 最近一次采集上报在 30 分钟内。它只表示 Agent 还在上报这条资产，不表示实例/容器本身可用（后者见名称下的副标题）。最近上报 ${row.lastSeenAt ? relTime(row.lastSeenAt) : '从未'}`"
        />
      </template>
    </el-table-column>
    <el-table-column label="最近上报" width="110">
      <template #default="{ row }">
        <span v-if="row.lastSeenAt">{{ relTime(row.lastSeenAt) }}</span>
        <span v-else class="muted">从未上报</span>
      </template>
    </el-table-column>
    <el-table-column label="来源" width="84">
      <template #default="{ row }">
        <StatusPill :tone="SOURCE_TONE[row.source] || 'muted'" :text="sourceLabel(row.source)" />
      </template>
    </el-table-column>
    <el-table-column label="责任人" width="96">
      <template #default="{ row }">
        <span v-if="row.owner">{{ row.owner }}</span>
        <span v-else class="muted">未指派</span>
      </template>
    </el-table-column>
    <el-table-column label="操作" width="188" fixed="right" class-name="op-col">
      <template #default="{ row }">
        <el-button link type="primary" @click.stop="$emit('detail', row)">详情</el-button>
        <el-button v-if="canWrite" link type="primary" @click.stop="$emit('edit', row)">编辑</el-button>
        <!-- 低频动作收进「更多」：避免误点，也让行内保持清爽。
             「彻底删除」不在这里——它移到详情抽屉的危险区，与「忽略」分开。 -->
        <el-dropdown v-if="canWrite" trigger="click" @command="(cmd) => $emit('command', cmd, row)">
          <el-button link type="primary" @click.stop>
            更多<el-icon :size="12"><ArrowDown /></el-icon>
          </el-button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item v-if="!row.ignored" command="ignore">忽略（从台账隐藏）</el-dropdown-item>
              <el-dropdown-item v-else command="restore">恢复（重新纳入台账）</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </template>
    </el-table-column>
  </el-table>
</template>

<script setup>
import { ref } from 'vue'
import { ArrowDown, Files } from '@element-plus/icons-vue'
import EmptyState from '../common/EmptyState.vue'
import StatusPill from '../common/StatusPill.vue'
import { STATUS_TONE, SOURCE_TONE, statusLabel, sourceLabel, subtitle, relTime, typeLabel as fmtTypeLabel, labelList, rowClass } from './assetFormat'
import { useAssetTypes } from '../../composables/useAssetTypes'

defineProps({
  items: { type: Array, default: () => [] },
  loading: { type: Boolean, default: false },
  canWrite: { type: Boolean, default: false },
  emptyTitle: { type: String, default: '' },
  emptyHints: { type: Array, default: () => [] },
})
const emit = defineEmits(['row-click', 'selection-change', 'detail', 'edit', 'command'])

const { labelOf } = useAssetTypes()
const typeLabel = (row) => fmtTypeLabel(row, labelOf)

const tableRef = ref(null)

// 点复选框列只应改变选中态，不该顺带打开详情抽屉（用户是在选行，不是在"看这一条"）
function onRowClick(row, column) {
  if (column && column.type === 'selection') return
  emit('row-click', row, column)
}

function clearSelection() {
  tableRef.value?.clearSelection?.()
}
defineExpose({ clearSelection })
</script>

<style scoped>
.name {
  color: var(--accent);
  font-weight: 500;
}
.sub {
  color: var(--t3);
  font-size: var(--fs-xs);
  margin-top: 2px;
}
/* 类型是**分类**不是状态：不用 StatusPill（它表达 ok/warn/danger），用中性标签但走令牌 */
.type-tag {
  display: inline-block;
  padding: 1px 6px;
  border-radius: var(--r-xs);
  font-size: var(--fs-xs);
}
.type-tag.host {
  background: var(--accent-dim);
  color: var(--accent);
}
.type-tag.mw {
  background: color-mix(in srgb, var(--violet) 16%, transparent);
  color: var(--violet);
}
.labels {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  margin-top: 4px;
}
.label-chip {
  display: inline-block;
  padding: 0 6px;
  border-radius: var(--r-xs);
  background: var(--fill-2);
  color: var(--t3);
  font-size: var(--fs-xs);
  line-height: 17px;
  font-family: var(--mono);
}
/* 已忽略标记：只在「含已忽略」视图里出现，必须显眼 */
.tag-ignored {
  display: inline-block;
  margin-left: 6px;
  padding: 0 6px;
  border: 1px solid var(--bd-strong);
  border-radius: var(--r-xs);
  color: var(--t3);
  font-size: var(--fs-xs);
  line-height: 16px;
  vertical-align: 1px;
}
/* 行高亮需穿透到 Element Plus 生成的 tr 上（scoped 样式默认作用不到组件内部）。
   底色由状态令牌派生，避免写死 rgba 后在换主题时与页面其它元素割裂。 */
:deep(.row-conflict) {
  background: color-mix(in srgb, var(--warn) 8%, transparent);
}
:deep(.row-missing) {
  background: color-mix(in srgb, var(--danger) 8%, transparent);
}
/* 操作列：链接按钮靠右一行排开。用 flex + gap 而不是依赖 .el-button + .el-button
   的 12px 外边距——「更多」是 el-dropdown（inline-flex 包裹），相邻选择器对它不生效，
   于是三件套（详情 / 编辑 / 更多）会被挤到第二行。 */
:deep(.op-col .cell) {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 4px;
  white-space: nowrap;
}
:deep(.op-col .cell .el-button + .el-button) { margin-left: 0; }
</style>
