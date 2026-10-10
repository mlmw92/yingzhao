<!--
  资产台账 · 详情抽屉（原型的三 Tab：属性对比 / 变更历史 / 关联关系）。

  三个 Tab 各自持有 loading / 错误 / 重试：此前四路数据走 `Promise.all`，任一失败整体抛错，
  表现为"历史接口挂了 → 整个资产详情打不开"，而用户最需要的「属性对比」其实早就回来了。
  现在并行取、各自降级——属性对比永远可用。

  「彻底删除」放在这里的危险区（不再是行内「更多」下拉里的一项）：它是唯一不可逆的动作，
  必须与「忽略」在视觉与位置上分开，并写清影响范围。
-->
<template>
  <el-drawer
    v-model="visible"
    size="min(780px, 94vw)"
    :title="detail ? detail.name || detail.naturalKey : '资产详情'"
  >
    <div v-if="detail" class="detail">
      <div class="d-tags">
        <span class="type-tag" :class="detail.typeKey === 'host' ? 'host' : 'mw'">{{ typeLabel(detail) }}</span>
        <StatusPill :tone="STATUS_TONE[detail.status] || 'muted'" dot :text="statusLabel(detail.status)" />
        <span class="chip">{{ detail.node || '无归属节点' }}</span>
        <span v-if="detail.owner" class="chip">{{ detail.owner }}</span>
      </div>
      <div class="d-meta">
        自然键 <code>{{ detail.naturalKey }}</code> · 资产 ID <code>{{ detail.displayId || detail.id }}</code> ·
        首次发现 {{ fmtTime(detail.createdAt) }} ·
        最近上报 {{ detail.lastSeenAt ? relTime(detail.lastSeenAt) : '从未上报' }}
      </div>

      <!-- 已忽略：必须显式说明"它还在、只是被隐藏了"，否则用户会以为资产被删了 -->
      <el-alert
        v-if="detail.ignored"
        type="warning"
        :closable="false"
        show-icon
        class="alert-gap"
        :title="`该资产已从台账隐藏（忽略）${detail.ignoredBy ? ' · ' + detail.ignoredBy : ''}${detail.ignoredAt ? ' · ' + fmtTime(detail.ignoredAt) : ''}`"
        :description="`${detail.ignoreReason ? '理由：' + detail.ignoreReason + '；' : ''}采集仍在继续，恢复后会看到最新状态。列表与统计默认不包含它。`"
      />
      <div v-if="detail.ignored && canWrite" class="drawer-actions">
        <el-button type="primary" @click="$emit('restore', detail)">恢复（重新纳入台账）</el-button>
      </div>

      <!-- 标签：与属性分开的分类维度 -->
      <div class="d-labels">
        <span class="d-labels-title">标签</span>
        <template v-if="labelList(detail).length">
          <span v-for="lb in labelList(detail)" :key="lb.key" class="label-chip">
            {{ lb.key }}<template v-if="lb.value">:{{ lb.value }}</template>
          </span>
        </template>
        <span v-else class="muted">未打标签（用于分类与筛选，如 env:prod、system:order）</span>
        <el-button v-if="canWrite" link type="primary" class="d-labels-edit" @click="$emit('edit', detail)">编辑标签</el-button>
      </div>

      <el-tabs :model-value="tab" class="detail-tabs" @update:model-value="$emit('update:tab', $event)">
        <el-tab-pane :label="`属性对比 ${(detail.attrs || []).length}`" name="attr">
          <SectionCard :loading="attrLoading" :error="attrError" error-retry @retry="$emit('retry-attr')">
            <el-alert
              v-if="detail.conflictCount > 0"
              type="warning"
              :closable="false"
              show-icon
              :title="`该资产有 ${detail.conflictCount} 个字段的人工值与采集值不一致，生效值取人工值`"
              class="alert-gap"
            />
            <div class="sec">
              <span>技术属性</span>
              <span class="muted">由 Agent 采集写入；人工值不覆盖采集值</span>
            </div>
            <el-table :data="techRows" style="width: 100%">
              <template #empty>
                <EmptyState
                  :icon="Files"
                  title="暂无采集属性"
                  :hints="['该资产还没有采集到的技术属性', '等下一轮上报后再看；仍未出现时确认对应采集项已开启']"
                />
              </template>
              <el-table-column label="字段" width="150">
                <template #default="{ row }"><span class="mono">{{ row.key }}</span></template>
              </el-table-column>
              <el-table-column label="采集值" min-width="150">
                <template #default="{ row }">
                  <span v-if="row.discovery !== undefined" :class="{ struck: row.conflict }">{{ attrText(row.key, row.discovery) }}</span>
                  <span v-else class="muted">—</span>
                </template>
              </el-table-column>
              <el-table-column label="人工值" min-width="150">
                <template #default="{ row }">
                  <span v-if="row.manual !== undefined">{{ attrText(row.key, row.manual) }}</span>
                  <span v-else class="muted">—</span>
                </template>
              </el-table-column>
              <el-table-column label="生效值" min-width="200">
                <template #default="{ row }">
                  <span>{{ attrText(row.key, row.effective) }}</span>
                  <StatusPill
                    class="src-pill"
                    :tone="row.manual !== undefined ? 'info' : 'muted'"
                    :text="row.manual !== undefined ? '人工' : '自动'"
                  />
                  <el-button
                    v-if="row.conflict && canWrite"
                    link
                    type="warning"
                    @click="$emit('restore-attr', row.key)"
                  >恢复采集值</el-button>
                </template>
              </el-table-column>
            </el-table>

            <div class="sec">
              <span>管理属性</span>
              <span class="muted">仅人工维护，采集不写入（责任人固定用 owner 键）</span>
            </div>
            <el-table :data="manualRows" style="width: 100%">
              <template #empty>
                <EmptyState
                  :icon="Files"
                  title="暂无人工属性"
                  :hints="['管理属性只由人工维护，采集不会写入', '在详情里补充责任人（owner 键）等字段后这里就有内容']"
                />
              </template>
              <el-table-column label="字段" width="150">
                <template #default="{ row }"><span class="mono">{{ row.key }}</span></template>
              </el-table-column>
              <el-table-column label="人工值" min-width="200">
                <template #default="{ row }">{{ attrText(row.key, row.manual) }}</template>
              </el-table-column>
              <el-table-column label="维护人 / 时间" min-width="200">
                <template #default="{ row }">
                  <span class="muted">{{ row.manualBy || '—' }} · {{ fmtTime(row.manualAt) }}</span>
                </template>
              </el-table-column>
            </el-table>

            <div class="sec">
              <span>归属节点</span>
              <span class="muted">资源范围锚点，接口与界面均不可变更</span>
            </div>
            <el-descriptions :column="1" border>
              <el-descriptions-item label="归属节点">{{ detail.node || '无' }}</el-descriptions-item>
            </el-descriptions>

            <!-- 期望值（标杆）：说清"当前是哪台"，而不只是"本资产是不是"——
                 只回答后者的话，用户想知道"那到底是哪台"只能再去巡检页翻。 -->
            <div v-if="canWrite" class="drawer-actions">
              <el-button type="primary" @click="$emit('edit', detail)">编辑资产</el-button>
              <template v-if="baselineIsSelf">
                <el-button type="danger" plain @click="$emit('drop-baseline')">清除期望值</el-button>
              </template>
              <el-button v-else title="把本资产的当前配置设为该类型的期望值（配置巡检的合规偏差以它为基准）" @click="$emit('make-baseline')">
                设为期望值（标杆）
              </el-button>
              <el-button link type="primary" @click="$emit('goto-inspect')">查看配置巡检</el-button>
            </div>
            <p class="muted baseline-line">
              <template v-if="baselineIsSelf">
                本资产就是「{{ typeLabel(detail) }}」类型的期望值：配置巡检的合规偏差以它的当前配置为基准。
              </template>
              <template v-else-if="currentBaseline">
                本类型当前的期望值（标杆）是 <code>{{ currentBaseline.assetKey }}</code><template v-if="currentBaseline.setBy">（{{ currentBaseline.setBy }} 设置）</template>；
                设为期望值会把它替换掉（每个类型只保留一个）。
              </template>
              <template v-else>本类型尚未设置期望值：配置巡检只做快照前后比对，不产出合规偏差。</template>
            </p>

            <!-- 容器类资产（Pod / 工作负载）到容器页看日志与事件：只读跳转，
                 因此不受写权限门控——只读账号同样需要这条排障路径 -->
            <div v-if="detail.container" class="drawer-actions">
              <el-button
                :title="detail.typeKey === 'pod' ? '到容器页查看该 Pod 的日志与事件' : '到容器页查看该集群/命名空间的工作负载'"
                @click="$emit('goto-container', detail)"
              >
                {{ detail.typeKey === 'pod' ? '查看容器日志' : '查看容器工作负载' }}
              </el-button>
              <span class="muted">集群 {{ detail.container.cluster }} · {{ detail.container.namespace }}</span>
            </div>

            <!-- 危险区：唯一不可逆的动作，与「忽略」分开，并写清影响范围 -->
            <SectionCard class="danger-zone" title="危险操作" subtitle="不可恢复，请确认影响范围">
              <template v-if="!detail.hasDiscovery">
                <p class="muted">
                  彻底删除会连同该资产的人工值、变更历史与配置快照一并删除，<b>不可恢复</b>。
                  若之后 Agent 再次上报同一自然键，会重新建档，但历史与人工值不会回来。
                </p>
                <el-button type="danger" @click="$emit('purge', detail)">彻底删除该资产</el-button>
              </template>
              <p v-else class="muted">
                该资产由采集发现（有自动上报）：删除后会被下一轮上报重建，因此不提供彻底删除。
                要让它从台账消失请用「忽略」（可随时恢复）。
              </p>
            </SectionCard>
          </SectionCard>
        </el-tab-pane>

        <el-tab-pane :label="`变更历史 ${history.length}`" name="history">
          <SectionCard
            title="变更历史"
            subtitle="字段级 diff，仅在值真正变化时记录"
            :loading="historyLoading"
            :error="historyError"
            error-retry
            @retry="$emit('retry-history')"
          >
            <el-timeline v-if="history.length">
              <el-timeline-item
                v-for="rec in history"
                :key="rec.at + rec.field + (rec.new || '')"
                :timestamp="fmtTime(rec.at)"
                :type="rec.kind === 'initial' ? 'success' : rec.source === 'manual' ? 'primary' : 'info'"
                placement="top"
              >
                <div class="tl-title">
                  <template v-if="rec.kind === 'initial'">资产建档</template>
                  <template v-else-if="rec.new">更新 <span class="mono">{{ rec.field }}</span></template>
                  <template v-else>恢复采集值 <span class="mono">{{ rec.field }}</span></template>
                </div>
                <div v-if="rec.kind !== 'initial'" class="tl-diff">
                  <span class="struck">{{ rec.old || '（空）' }}</span> → <b>{{ rec.new || '（已清除）' }}</b>
                </div>
                <div class="muted">
                  来源 {{ rec.source === 'manual' ? '人工' : '采集' }}
                  <template v-if="rec.actor"> · 操作人 {{ rec.actor }}</template>
                </div>
                <!-- 正向入口：这条变更属于哪一次操作。没有关联 id 的记录（采集侧写的、
                     以及本次升级之前的历史）不给入口——按钮点了查不到东西比没有更糟。
                     读审计需要独立权限点，没有就整块不显示。 -->
                <div v-if="rec.requestId && canReadAudit" class="tl-audit">
                  <el-button link type="primary" size="small" @click="$emit('goto-audit', rec.requestId)">
                    查看对应审计
                  </el-button>
                </div>
              </el-timeline-item>
            </el-timeline>
            <EmptyState
              v-else
              :icon="DataLine"
              title="暂无变更记录"
              :hints="['字段发生变化（采集值差异或人工值修改）时才记录一条', '运行态字段（如 up / status）刻意不参与比对']"
            />
          </SectionCard>
        </el-tab-pane>

        <el-tab-pane :label="`关联关系 ${links.length}`" name="links">
          <SectionCard
            title="关联关系"
            :loading="linksLoading"
            :error="linksError"
            error-retry
            @retry="$emit('retry-links')"
          >
            <template #actions>
              <el-button size="small" @click="$emit('open-topology')">关系图</el-button>
              <el-button v-if="canWrite" size="small" @click="$emit('add-link')">添加关系</el-button>
            </template>
            <p class="muted">
              人工维护的关系优先于采集自动发现：被人工认领过的边不会被采集覆盖。
              自动建立的有两类：中间件实例 / K8s 容器 runs_on 宿主主机，以及 K8s 容器 member_of 所属工作负载。
            </p>
            <el-table :data="links" style="width: 100%">
              <template #empty>
                <EmptyState
                  :icon="Files"
                  title="暂无关联关系"
                  :hints="['runs_on（实例/容器 → 主机）与 member_of（容器 → 工作负载）由 Agent 上报后自动建立', '也可以用上方「添加关系」人工建立']"
                />
              </template>
              <el-table-column label="方向" width="105">
                <template #default="{ row }">
                  <span :class="'rel-' + row.direction">{{ row.direction === 'out' ? '本资产 →' : '← 指向本资产' }}</span>
                </template>
              </el-table-column>
              <el-table-column label="关系" width="130">
                <template #default="{ row }"><span class="tag">{{ kindShort(row.kind) }}</span></template>
              </el-table-column>
              <el-table-column label="对端资产" min-width="200">
                <template #default="{ row }">
                  <span class="mono">{{ row.peerKey }}</span>
                  <span class="muted">（{{ peerTypeLabel(row.peerType) }}）</span>
                </template>
              </el-table-column>
              <el-table-column label="来源" width="80">
                <template #default="{ row }">
                  <StatusPill :tone="row.source === 'manual' ? 'info' : 'muted'" :text="row.source === 'manual' ? '人工' : '采集'" />
                </template>
              </el-table-column>
              <el-table-column v-if="canWrite" label="操作" width="70" align="center">
                <template #default="{ row }">
                  <el-button type="danger" link size="small" @click="$emit('remove-link', row)">解除</el-button>
                </template>
              </el-table-column>
            </el-table>

            <!-- 被人工隐藏的关系（逻辑删除的结果）。必须能列出来并能恢复：
                 看不见的删除等于不可逆，用户会以为删错了就再也回不来。 -->
            <template v-if="suppressed.length">
              <div class="sec"><span>已人工隐藏 {{ suppressed.length }}</span></div>
              <el-table :data="suppressed" size="small" style="width: 100%">
                <el-table-column label="关系" width="130">
                  <template #default="{ row }"><span class="tag">{{ kindShort(row.kind) }}</span></template>
                </el-table-column>
                <el-table-column label="对端资产" min-width="200">
                  <template #default="{ row }"><span class="mono">{{ row.peerKey }}</span></template>
                </el-table-column>
                <el-table-column label="隐藏者" width="110">
                  <template #default="{ row }">{{ row.createdBy || '—' }}</template>
                </el-table-column>
                <el-table-column v-if="canWrite" label="操作" width="70" align="center">
                  <template #default="{ row }">
                    <el-button link size="small" @click="$emit('restore-link', row)">恢复</el-button>
                  </template>
                </el-table-column>
              </el-table>
              <p class="muted muted-base">
                隐藏是逻辑删除：采集不会再自动建立这些关系。「恢复」把它们交还给采集 ——
                若采集仍在上报，下一次上报就会重新出现。
              </p>
            </template>
            <p class="muted muted-base">
              这里只列直接关系，范围外的对端不返回。要看 N 跳邻域（影响面）用上方「关系图」：
              点节点即以它为中心重新展开。
            </p>
          </SectionCard>
        </el-tab-pane>
      </el-tabs>

      <!-- AI 诊断：把台账信息、变更与关系交给模型，给下一步排查建议（只给建议，不改数据）。
           入口不按写权限门控——只读账号同样需要这条排障路径（与容器日志入口同一取向）。 -->
      <div class="drawer-actions">
        <AiDiagnosePanel kind="asset" :id="detail.id" />
      </div>
    </div>
  </el-drawer>
</template>

<script setup>
import { computed } from 'vue'
import { Files, DataLine } from '@element-plus/icons-vue'
import SectionCard from '../common/SectionCard.vue'
import EmptyState from '../common/EmptyState.vue'
import StatusPill from '../common/StatusPill.vue'
import AiDiagnosePanel from '../ai/AiDiagnosePanel.vue'
import { useAssetTypes } from '../../composables/useAssetTypes'
import {
  STATUS_TONE, statusLabel, fmtTime, relTime, attrText, labelList, kindShort, peerTypeLabel,
  typeLabel as fmtTypeLabel,
} from './assetFormat'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  detail: { type: Object, default: null },
  history: { type: Array, default: () => [] },
  links: { type: Array, default: () => [] },
  suppressed: { type: Array, default: () => [] },
  baselines: { type: Array, default: () => [] },
  canWrite: { type: Boolean, default: false },
  canReadAudit: { type: Boolean, default: false },
  tab: { type: String, default: 'attr' },
  attrLoading: { type: Boolean, default: false },
  attrError: { type: String, default: '' },
  historyLoading: { type: Boolean, default: false },
  historyError: { type: String, default: '' },
  linksLoading: { type: Boolean, default: false },
  linksError: { type: String, default: '' },
})
const emit = defineEmits([
  'update:modelValue', 'update:tab', 'retry-attr', 'retry-history', 'retry-links',
  'edit', 'restore', 'make-baseline', 'drop-baseline', 'restore-attr', 'purge',
  'goto-container', 'goto-audit', 'goto-inspect', 'open-topology', 'add-link', 'remove-link', 'restore-link',
])

const { labelOf } = useAssetTypes()
const typeLabel = (row) => fmtTypeLabel(row, labelOf)

const visible = computed({
  get: () => props.modelValue,
  set: (v) => emit('update:modelValue', v),
})

// 属性对比的两种分段：技术属性 = 有采集值（含冲突），管理属性 = 仅人工写
const attrMap = computed(() => (props.detail && props.detail.attrs) || [])

const techRows = computed(() => {
  const byKey = new Map()
  for (const attr of attrMap.value) {
    const row = byKey.get(attr.key) || { key: attr.key }
    if (attr.source === 'manual') {
      row.manual = attr.value
      row.manualBy = attr.updatedBy || ''
    } else {
      row.discovery = attr.value
    }
    byKey.set(attr.key, row)
  }
  const out = []
  for (const row of byKey.values()) {
    if (row.discovery === undefined && row.manual === undefined) continue
    row.conflict = row.discovery !== undefined && row.manual !== undefined
    row.effective = row.manual !== undefined ? row.manual : row.discovery
    if (row.discovery !== undefined) out.push(row)
  }
  return out.sort((a, b) => a.key.localeCompare(b.key))
})

const manualRows = computed(() => {
  const byKey = new Map()
  for (const attr of attrMap.value) {
    const row = byKey.get(attr.key) || { key: attr.key }
    if (attr.source === 'manual') {
      row.manual = attr.value
      row.manualBy = attr.updatedBy || ''
      row.manualAt = attr.updatedAt
    } else {
      row.discovery = attr.value
    }
    byKey.set(attr.key, row)
  }
  return [...byKey.values()]
    .filter((row) => row.manual !== undefined && row.discovery === undefined)
    .sort((a, b) => a.key.localeCompare(b.key))
})

// 本资产是否是其资产类型的期望值（标杆）；不是的话 currentBaseline 给出"当前是哪台"
const currentBaseline = computed(() => {
  const d = props.detail
  if (!d) return null
  return props.baselines.find((x) => x.typeKey === d.typeKey) || null
})
const baselineIsSelf = computed(() => {
  const d = props.detail
  const b = currentBaseline.value
  return !!(d && b && String(b.assetId) === String(d.id))
})
</script>

<style scoped>
.detail {
  padding-bottom: 8px;
}
.d-tags {
  display: flex;
  gap: 8px;
  align-items: center;
  flex-wrap: wrap;
}
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
.chip {
  display: inline-block;
  padding: 1px 6px;
  border-radius: var(--r-xs);
  background: var(--fill-2);
  color: var(--t3);
  font-size: var(--fs-xs);
}
.d-meta {
  margin: 10px 0 6px;
  font-size: var(--fs-sm);
  color: var(--t3);
}
.d-meta code {
  background: var(--fill-2);
  border-radius: var(--r-xs);
  padding: 1px 5px;
}
.d-labels {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  margin: 10px 0 4px;
}
.d-labels-title {
  font-size: var(--fs-sm);
  color: var(--t3);
  margin-right: 2px;
}
.d-labels-edit {
  margin-left: auto;
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
/* 三个 Tab 的内容量差异很大，给内容区一个下限，切换时不再"塌"一下 */
.detail-tabs :deep(.el-tabs__content) {
  min-height: 320px;
}
.sec {
  display: flex;
  align-items: baseline;
  gap: 8px;
  margin: 16px 0 8px;
  font-size: var(--fs-base);
  font-weight: 600;
}
.sec .muted {
  font-weight: 400;
  font-size: var(--fs-xs);
}
.tag {
  font-size: var(--fs-xs);
  padding: 1px 6px;
  border-radius: var(--r-xs);
  background: var(--fill-2);
  color: var(--t2);
}
.mono {
  font-family: var(--mono);
}
.alert-gap {
  margin-bottom: 12px;
}
.drawer-actions {
  margin-top: 18px;
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.baseline-line {
  margin-top: var(--desc-tight);
  line-height: 20px;
}
.src-pill {
  margin-left: 8px;
}
.struck {
  color: var(--t3);
  text-decoration: line-through;
  margin-right: 4px;
}
.tl-title {
  font-size: var(--fs-base);
}
.tl-diff {
  font-size: var(--fs-sm);
  color: var(--t3);
  margin: 4px 0;
}
.tl-audit {
  margin-top: 4px;
}
.rel-out {
  color: var(--accent);
}
.rel-in {
  color: var(--violet);
}
</style>
