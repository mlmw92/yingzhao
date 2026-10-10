<!--
  资产台账 · 弹窗集合（新建 / 编辑 / 添加关系 / 批量转派 / 批量打标签 / 批量结果）。

  为什么集中在一个文件：这六个弹窗共享同一批表单状态与提交口径（都写人工值、都要二次确认），
  拆成六个文件会让"改一处文案"散落到六处。这里全部做成**受控**组件——
  状态与提交逻辑留在编排层（AssetListView），本组件只负责呈现与转发事件。

  「彻底删除」不在这里：它已移到详情抽屉的危险区（唯一不可逆动作与「忽略」分开）。
-->
<template>
  <!-- 新建：手工建档（人工来源）。表单项与列表列一一对应：
       资产名称→「资产名称」，类型+地址→「类型」，归属节点→「归属节点」，责任人→「责任人」；
       自然键由「实例类型 + 地址」自动拼出，不要求用户理解 <类型>:<地址> 的内部约定。 -->
  <el-dialog
    :model-value="createVisible"
    title="新建资产"
    width="min(640px, 94vw)"
    @update:model-value="$emit('update:createVisible', $event)"
  >
    <el-form label-width="100px">
      <el-form-item label="资产类型">
        <!-- 只提供两种：手工**能**建的只有主机与中间件实例。
             容器/工作负载由清单上报产生，手工建一个会被下一轮上报覆盖或变成孤儿。 -->
        <el-radio-group :model-value="assetKind" @update:model-value="$emit('update:assetKind', $event)">
          <el-radio-button value="host">{{ labelOf('host') }}</el-radio-button>
          <el-radio-button value="instance">{{ labelOf('middleware-instance') }}</el-radio-button>
        </el-radio-group>
      </el-form-item>
      <el-form-item v-if="assetKind === 'host'" label="主机名">
        <el-input v-model="form.naturalKey" placeholder="与 Agent 上报的 hostname 一致，如 web-01" />
      </el-form-item>
      <template v-else>
        <el-form-item label="实例类型">
          <el-select
            :model-value="instanceType"
            style="width: 100%"
            @update:model-value="$emit('update:instanceType', $event)"
          >
            <el-option v-for="(label, key) in INSTANCE_LABELS" :key="key" :label="label" :value="key" />
          </el-select>
        </el-form-item>
        <el-form-item label="实例地址">
          <el-input
            :model-value="instanceAddr"
            placeholder="host:port，如 127.0.0.1:6379"
            @update:model-value="$emit('update:instanceAddr', $event)"
          />
        </el-form-item>
        <el-form-item label="自然键">
          <el-input :model-value="naturalKeyPreview || '填写实例类型与地址后自动生成'" disabled />
        </el-form-item>
      </template>
      <el-form-item label="资产名称">
        <el-input v-model="form.name" placeholder="可留空，默认展示自然键" />
      </el-form-item>
      <el-form-item label="归属节点">
        <el-input
          v-model="form.node"
          :placeholder="assetKind === 'host' ? '留空则默认为主机自身' : '必须是你有权访问的节点；决定资源范围可见性'"
        />
      </el-form-item>
      <el-form-item label="责任人">
        <el-input
          :model-value="ownerInput"
          placeholder="选填；以人工属性 owner 记录，列表按它展示与统计"
          @update:model-value="$emit('update:ownerInput', $event)"
        />
      </el-form-item>
      <el-form-item label="其它属性">
        <AssetAttrEditor :rows="editAttrs" :dict="attrDict" />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="$emit('update:createVisible', false)">取消</el-button>
      <el-button type="primary" :loading="saving" @click="$emit('submit-create')">提交</el-button>
    </template>
  </el-dialog>

  <!-- 编辑：可改资产名称 + 人工属性 + 标签（采集值不受影响）。与列表对齐：名称 / 责任人 / 其它属性；
       责任人清空提交 = 恢复「未指派」（走 resetAttrs 删除人工值，而不是写入空值）。 -->
  <el-dialog
    :model-value="editVisible"
    title="编辑资产"
    width="min(640px, 94vw)"
    @update:model-value="$emit('update:editVisible', $event)"
  >
    <el-alert
      type="info"
      :closable="false"
      show-icon
      title="这里写入的是人工值：Agent 采集的值会原样保留，两者差异在详情里可对比。归属节点与自然键是资源范围锚点，不可修改。"
      class="alert-gap"
    />
    <el-form label-width="100px">
      <el-form-item label="资产">
        <el-input :model-value="editLabel" disabled />
      </el-form-item>
      <el-form-item label="资产名称">
        <el-input v-model="form.name" placeholder="留空或保持原值表示不修改；清空后列表中回落到自然键" />
      </el-form-item>
      <el-form-item label="归属节点">
        <el-input :model-value="form.node || '（无归属节点）'" disabled />
      </el-form-item>
      <el-form-item label="责任人">
        <el-input
          :model-value="ownerInput"
          :placeholder="editHadOwner ? '清空并提交 = 恢复未指派' : '选填'"
          @update:model-value="$emit('update:ownerInput', $event)"
        />
      </el-form-item>
      <el-form-item label="其它属性">
        <AssetAttrEditor :rows="editAttrs" :dict="attrDict" />
      </el-form-item>
      <el-form-item label="标签">
        <div class="attr-editor">
          <div class="muted label-hint">
            分类维度（如 env:prod、system:order），与「其它属性」不同：标签只用于筛选与展示，不参与巡检比对。
            删除某一行即删除该标签；键留空的行会被忽略。
          </div>
          <div v-for="(row, i) in editLabels" :key="i" class="attr-row">
            <el-input v-model="row.key" placeholder="标签键，如 env" style="width: 42%" />
            <el-input v-model="row.value" placeholder="值，如 prod（可留空）" style="width: 42%" />
            <el-button link type="danger" @click="$emit('remove-label-row', i)">删除</el-button>
          </div>
          <el-button link type="primary" @click="$emit('add-label-row')">+ 添加标签</el-button>
        </div>
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="$emit('update:editVisible', false)">取消</el-button>
      <el-button type="primary" :loading="saving" @click="$emit('submit-edit')">提交</el-button>
    </template>
  </el-dialog>

  <!-- 添加关系：方向 × 类型 × 对端。方向做成显式选择而不是让前端猜——
       服务端把 URL 里的资产当作边的基准，direction 决定对端是终点还是起点。 -->
  <el-dialog
    :model-value="linkAddVisible"
    title="添加关系"
    width="min(560px, 94vw)"
    @update:model-value="$emit('update:linkAddVisible', $event)"
  >
    <el-form label-width="90px">
      <el-form-item label="方向">
        <el-radio-group v-model="linkForm.direction">
          <el-radio-button value="out">本资产 → 对端</el-radio-button>
          <el-radio-button value="in">对端 → 本资产</el-radio-button>
        </el-radio-group>
      </el-form-item>
      <el-form-item label="关系类型">
        <el-select v-model="linkForm.kind" style="width: 100%">
          <el-option v-for="k in LINK_KINDS" :key="k.value" :label="k.label" :value="k.value" />
        </el-select>
      </el-form-item>
      <el-form-item label="对端资产">
        <el-select
          v-model="linkForm.peer"
          filterable
          remote
          :remote-method="(q) => $emit('search-peers', q)"
          :loading="peerLoading"
          placeholder="输入名称 / 自然键 / 节点搜索"
          style="width: 100%"
        >
          <el-option v-for="o in peerOptions" :key="o.value" :label="o.label" :value="o.value" />
        </el-select>
      </el-form-item>
    </el-form>
    <div class="muted">
      候选只列出你可见范围内的资产（范围外的对端即便手工填也会被服务端拒绝）。
      目前 runs_on、member_of（容器 → 工作负载、实例 → 集群）与 exposes（K8s 服务 → 后端 Pod）由采集自动建立，depends_on 只能人工维护。
    </div>
    <template #footer>
      <el-button @click="$emit('update:linkAddVisible', false)">取消</el-button>
      <el-button type="primary" :loading="linkBusy" @click="$emit('submit-link-add')">确定</el-button>
    </template>
  </el-dialog>

  <!-- 批量转派责任人：留空即报错（要清空请点底部的「清空责任人」），
       避免"输入框忘了填"就把一整批责任人静默清掉。 -->
  <el-dialog v-model="batchOwner.visible" title="批量转派责任人" width="min(540px, 94vw)">
    <el-alert
      type="info"
      :closable="false"
      show-icon
      class="alert-gap"
      :title="`把选中的 ${selectionCount} 条资产的责任人统一改成下面的值（写入人工属性 owner，采集值不受影响）`"
    />
    <el-form label-width="90px">
      <el-form-item label="责任人">
        <el-input
          v-model="batchOwner.owner"
          placeholder="如 张三 / sre-team"
          @keyup.enter="$emit('submit-batch-owner', false)"
        />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="batchOwner.visible = false">取消</el-button>
      <el-button @click="$emit('submit-batch-owner', true)">清空责任人</el-button>
      <el-button type="primary" :loading="batchBusy" @click="$emit('submit-batch-owner', false)">保存转派</el-button>
    </template>
  </el-dialog>

  <!-- 批量打标签：写入会覆盖同名标签，删除会移除该键。
       两种方式放在一起，是因为"给这批机器打上 env=prod"与"把打错的 env 摘掉"是同一件事的两面。 -->
  <el-dialog v-model="batchLabel.visible" title="批量打标签" width="min(540px, 94vw)">
    <el-alert
      type="info"
      :closable="false"
      show-icon
      class="alert-gap"
      title="标签是分类维度（如 env=prod、system=order），与「其它属性」不同：只用于筛选与展示，不参与巡检比对。"
    />
    <el-form label-width="90px">
      <el-form-item label="方式">
        <el-radio-group v-model="batchLabel.mode">
          <el-radio value="set">写入 / 覆盖</el-radio>
          <el-radio value="remove">删除该标签</el-radio>
        </el-radio-group>
      </el-form-item>
      <el-form-item label="标签键">
        <el-input v-model="batchLabel.key" placeholder="如 env" />
      </el-form-item>
      <el-form-item v-if="batchLabel.mode === 'set'" label="值">
        <el-input v-model="batchLabel.value" placeholder="如 prod（可留空 = 只标记存在该键）" />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="batchLabel.visible = false">取消</el-button>
      <el-button type="primary" :loading="batchBusy" @click="$emit('submit-batch-label')">提交</el-button>
    </template>
  </el-dialog>

  <!-- 批量维护结果：逐条结论。部分成功是常态（范围外的按「不存在」计入），
       因此必须逐条说明原因，而不是只弹一句"成功 3 条"。 -->
  <el-dialog v-model="batchResultVisible" :title="resultTitle" width="min(660px, 94vw)">
    <div v-if="batchResult">
      <el-alert
        :type="batchResult.failed === 0 ? 'success' : batchResult.ok > 0 ? 'warning' : 'error'"
        :closable="false"
        show-icon
        class="alert-gap"
        :title="`目标 ${batchResult.total} 条：成功 ${batchResult.ok} 条，失败 ${batchResult.failed} 条`"
        :description="resultHint"
      />
      <template v-if="failedItems.length">
        <div class="batch-sec">
          <span>未成功（{{ failedItems.length }}）</span>
          <span class="muted">逐条原因</span>
        </div>
        <div v-for="it in failedItems" :key="it.id" class="batch-row">
          <span class="mono">{{ it.node || '—' }}</span>
          <span class="batch-name">{{ it.name || 'ast_' + it.id }}</span>
          <span class="warn-text">{{ it.error }}</span>
        </div>
      </template>
    </div>
    <template #footer>
      <el-button type="primary" @click="batchResultVisible = false">知道了</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { computed } from 'vue'
import AssetAttrEditor from './AssetAttrEditor.vue'
import { useAssetTypes } from '../../composables/useAssetTypes'
import { INSTANCE_LABELS, LINK_KINDS } from './assetFormat'

const props = defineProps({
  // —— 新建 / 编辑 ——
  createVisible: { type: Boolean, default: false },
  editVisible: { type: Boolean, default: false },
  saving: { type: Boolean, default: false },
  assetKind: { type: String, default: 'host' },
  instanceType: { type: String, default: 'redis' },
  instanceAddr: { type: String, default: '' },
  naturalKeyPreview: { type: String, default: '' },
  editLabel: { type: String, default: '' },
  editHadOwner: { type: Boolean, default: false },
  // 表单对象由编排层持有；本组件就地写其字段（props 浅只读，嵌套可变）
  form: { type: Object, required: true },
  ownerInput: { type: String, default: '' },
  editAttrs: { type: Array, required: true },
  editLabels: { type: Array, required: true },
  attrDict: { type: Object, default: () => ({}) },
  // —— 添加关系 ——
  linkAddVisible: { type: Boolean, default: false },
  linkBusy: { type: Boolean, default: false },
  linkForm: { type: Object, required: true },
  peerOptions: { type: Array, default: () => [] },
  peerLoading: { type: Boolean, default: false },
  // —— 批量 ——
  selectionCount: { type: Number, default: 0 },
  batchOwner: { type: Object, required: true },
  batchLabel: { type: Object, required: true },
  batchResultVisible: { type: Boolean, default: false },
  batchResult: { type: Object, default: null },
  batchBusy: { type: Boolean, default: false },
})

const emit = defineEmits([
  'update:createVisible', 'update:editVisible', 'update:assetKind', 'update:instanceType',
  'update:instanceAddr', 'update:ownerInput', 'update:linkAddVisible', 'update:batchResultVisible',
  'submit-create', 'submit-edit', 'add-label-row', 'remove-label-row',
  'search-peers', 'submit-link-add', 'submit-batch-owner', 'submit-batch-label',
])

const { labelOf } = useAssetTypes()

const batchResultVisible = computed({
  get: () => props.batchResultVisible,
  set: (v) => emit('update:batchResultVisible', v),
})

const failedItems = computed(() =>
  props.batchResult ? props.batchResult.items.filter((i) => !i.ok) : [],
)
const resultTitle = computed(() =>
  props.batchResult ? `批量${props.batchResult.opLabel}结果` : '批量维护结果',
)
const resultHint = computed(() => {
  const r = props.batchResult
  if (!r) return ''
  if (r.ok === 0) return r.message || '没有任何一条被更新，原因见下方。'
  if (r.failed === 0) return '全部成功。'
  return '部分成功：未成功的给出了具体原因；资源范围外的资产按「不存在」处理。'
})
</script>

<style scoped>
.alert-gap {
  margin-bottom: 12px;
}
.attr-editor {
  width: 100%;
}
.attr-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}
.label-hint {
  line-height: 18px;
  margin-bottom: 6px;
}
/* 批量结果面板：与节点操作页同一形态（分节标题 + 逐条一行）。
   scoped 样式不跨组件，因此 .warn-text 这类小类必须在本组件也定义一份。 */
.batch-sec {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  margin: 14px 0 6px;
  font-size: var(--fs-sm);
}
.batch-row {
  display: flex;
  gap: 10px;
  align-items: baseline;
  padding: 3px 0;
  font-size: var(--fs-sm);
}
.batch-name {
  flex: none;
  max-width: 220px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.warn-text {
  color: var(--warn);
}
.mono {
  font-family: var(--mono);
}
</style>
