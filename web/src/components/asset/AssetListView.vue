<template>
  <section class="view">
    <PageHeader
      title="资产台账"
      desc="主机、中间件实例与 K8s 容器/工作负载由 Agent 每轮上报自动发现；人工值不覆盖采集值，两者差异在详情里逐字段可见"
    />

    <!-- 健康度：与其它监控页统一的 KpiCard 卡片行；数字与列表同一套条件，点卡片即下钻 -->
    <AssetKpiRow
      :summary="summary"
      @drill-all="drillAll"
      @drill-status="drillStatus"
      @drill-no-owner="drillNoOwner"
      @drill-conflict="drillConflict"
      @drill-ignored="drillIgnored"
    />

    <div class="panel">
      <!-- 筛选：默认只留类型/状态/关键词，其余收进「更多筛选」；生效条件（含下钻）汇总成可撤销的 chips -->
      <AssetFilterPanel
        v-model:show-ignored="showIgnored"
        :filter="filter"
        :drill="drill"
        :asset-types="assetTypes"
        :loading="loading"
        @reload="reload"
        @reset="resetFilter"
        @remove-chip="removeChip"
      />

      <div class="action-bar">
        <!-- 权限点不再做成按钮旁的小徽标：那是原型给开发看的标注，放在线上是噪声。
             需要时用悬停提示说明；真正缺权限的人看到的是下面那句只读提示。 -->
        <el-button
          v-if="canWrite"
          type="primary"
          title="需要 assets:write 权限；属高风险操作，提交前会二次确认"
          @click="openCreate"
        >新建资产</el-button>
        <span v-else class="muted">
          当前账号只读：缺 assets:write 权限，可在「权限模型 → 资产」中授予
        </span>
        <!-- 导出走独立权限点：一次把整份台账落盘。导的是**当前筛选命中的全部资产**，不是这一页。 -->
        <el-button
          v-if="canExport"
          :loading="exporting"
          title="导出当前筛选命中的全部资产（服务端导出，不受分页限制）；属高风险操作，需二次确认"
          @click="doExport"
        >导出清单 CSV</el-button>
        <span class="muted action-note">
          范围外资产按「不存在」返回，不做 403 区分；状态与来源由既有数据推导，不单独落库
        </span>
      </div>

      <el-alert v-if="loadError" type="error" :closable="false" show-icon :title="loadError" class="alert-gap" />

      <!-- 批量条：勾选后才出现。动作与单条一致（转派/标签/忽略/恢复），
           区别只在于"一次改一批"；结果逐条给结论（部分成功是常态）。 -->
      <BatchBar v-if="selection.length" :count="selection.length" @clear="clearSelection">
        <el-button size="small" @click="openBatchOwner">转派责任人</el-button>
        <el-button size="small" @click="openBatchLabel">打标签</el-button>
        <el-button size="small" @click="batchIgnore">忽略</el-button>
        <el-button size="small" @click="batchRestore">恢复</el-button>
      </BatchBar>

      <AssetTable
        ref="tableRef"
        :items="items"
        :loading="loading"
        :can-write="canWrite"
        :empty-title="listEmptyTitle"
        :empty-hints="listHints"
        @row-click="openDetail"
        @selection-change="onSelectionChange"
        @detail="openDetail"
        @edit="openEdit"
        @command="onRowCommand"
      />

      <div class="pager">
        <el-pagination
          background
          layout="total, sizes, prev, pager, next, jumper"
          :total="total"
          :page-size="pageSize"
          :current-page="page"
          :page-sizes="[10, 20, 50, 100]"
          @current-change="onPageChange"
          @size-change="onSizeChange"
        />
      </div>
    </div>

    <!-- 详情抽屉：三个 Tab 各自 loading / 错误 / 重试（并行取数，一段失败不影响其它段） -->
    <AssetDetailDrawer
      v-model="detailVisible"
      :detail="detail"
      :history="history"
      :links="links"
      :suppressed="suppressed"
      :baselines="baselines"
      :can-write="canWrite"
      :can-read-audit="canReadAudit"
      :tab="tab"
      :attr-loading="attrLoading"
      :attr-error="attrError"
      :history-loading="historyLoading"
      :history-error="historyError"
      :links-loading="linksLoading"
      :links-error="linksError"
      @update:tab="tab = $event"
      @retry-attr="retryAttr"
      @retry-history="retryHistory"
      @retry-links="retryLinks"
      @edit="openEdit"
      @restore="doRestore"
      @make-baseline="makeBaseline"
      @drop-baseline="dropBaseline"
      @restore-attr="restoreAttr"
      @purge="doPurge"
      @goto-container="gotoContainer"
      @goto-audit="gotoAudit"
      @goto-inspect="gotoInspect"
      @open-topology="openTopology"
      @add-link="openLinkAdd"
      @remove-link="removeLink"
      @restore-link="restoreLink"
    />

    <!-- 新建 / 编辑 / 添加关系 / 批量：状态与提交逻辑在本页，弹窗组件只负责呈现与转发 -->
    <AssetModals
      v-model:create-visible="createVisible"
      v-model:edit-visible="editVisible"
      v-model:asset-kind="assetKind"
      v-model:instance-type="instanceType"
      v-model:instance-addr="instanceAddr"
      v-model:owner-input="ownerInput"
      v-model:link-add-visible="linkAddVisible"
      v-model:batch-result-visible="batchResultVisible"
      :saving="saving"
      :natural-key-preview="naturalKeyPreview"
      :edit-label="editLabel"
      :edit-had-owner="editHadOwner"
      :form="form"
      :edit-attrs="editAttrs"
      :edit-labels="editLabels"
      :attr-dict="attrDict"
      :link-busy="linkBusy"
      :link-form="linkForm"
      :peer-options="peerOptions"
      :peer-loading="peerLoading"
      :selection-count="selection.length"
      :batch-owner="batchOwner"
      :batch-label="batchLabel"
      :batch-result="batchResult"
      :batch-busy="batchBusy"
      @submit-create="submitCreate"
      @submit-edit="submitEdit"
      @add-label-row="addLabelRow"
      @remove-label-row="removeLabelRow"
      @search-peers="searchLinkPeers"
      @submit-link-add="submitLinkAdd"
      @submit-batch-owner="submitBatchOwner"
      @submit-batch-label="submitBatchLabel"
    />

    <!-- 关系图：以本资产为中心的 N 跳邻域（图本身由 TopologyGraph 负责，
         与告警影响面共用同一个组件，两处的范围口径与交互才会一致）。 -->
    <el-dialog v-model="topoVisible" :title="topoTitle" width="min(900px, 94vw)">
      <TopologyGraph v-if="detail" :asset-id="detail.id" :height="520" />
    </el-dialog>
  </section>
</template>

<script setup>
import { ref, reactive, computed, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  listAssets,
  getAsset,
  getAssetHistory,
  getAssetLinks,
  createAssetLink,
  deleteAssetLink,
  restoreAssetLink,
  getAssetSummary,
  createAsset,
  updateAsset,
  listInspectBaselines,
  setAssetBaseline,
  clearAssetBaseline,
  ignoreAsset,
  restoreAsset,
  purgeAsset,
  updateAssetLabels,
  batchAssets,
  exportAssets,
  getAssetTypes,
} from '../../api/asset'
import { useAuth } from '../../composables/useAuth'
import { useAssetTypes } from '../../composables/useAssetTypes'
import PageHeader from '../common/PageHeader.vue'
// 批量条与节点操作页共用（同一个视觉与「取消选择」位置）
import BatchBar from '../BatchBar.vue'
// 关系图与告警影响面共用同一个组件：两处的范围裁剪、着色与交互必须一致
import TopologyGraph from './TopologyGraph.vue'
// 台账拆出的子组件：KPI 行 / 筛选面板 / 主表格 / 详情抽屉 / 弹窗集合
import AssetKpiRow from './AssetKpiRow.vue'
import AssetFilterPanel from './AssetFilterPanel.vue'
import AssetTable from './AssetTable.vue'
import AssetDetailDrawer from './AssetDetailDrawer.vue'
import AssetModals from './AssetModals.vue'
import { OWNER_KEY, labelFingerprint, typeLabel as fmtTypeLabel } from './assetFormat'

const auth = useAuth()
// 资产类型清单（含显示名）来自配置项模型接口，接口不可用时退回内置四类（见 composable）
const { types: assetTypes, labelOf } = useAssetTypes()
// 前端隐藏仅为体验：服务端 assets:write 是真正的边界（且属高风险权限，提交前二次确认）。
const canWrite = computed(() => auth.can('assets:write'))
// 读审计是独立权限点（audit:read）：没有它时不摆"查看对应审计"的入口。
const canReadAudit = computed(() => auth.can('audit:read'))
// 导出走独立权限点：一次把整份台账落盘。
const canExport = computed(() => auth.can('assets:export'))

const route = useRoute()
const router = useRouter()

const typeLabel = (row) => fmtTypeLabel(row, labelOf)

/* ================= 列表与摘要 ================= */

const items = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const loading = ref(false)
const loadError = ref('')
const summary = ref({ total: 0, missing: 0, noOwner: 0, conflict: 0, changes: 0, ignored: 0 })

const filter = ref({ type: '', status: '', source: '', node: '', keyword: '', label: '', ignored: '' })
// 摘要下钻的三个条件（无责任人 / 有冲突 / 已忽略）：不在下拉里，单独记状态以便显示与撤销
const drill = ref({ ownerMissing: false, conflict: false, ignored: false })

const selection = ref([])
const tableRef = ref(null)

// 业务范围（资产标签维度）生效时的空态说明。
//
// 为什么必须专门说一句：摘要与列表共用同一套范围条件，所以"范围把你挡在外面"时
// **两个数字都是 0**，与"台账真的还没有任何资产"长得一模一样。不说清楚的话，
// 用户会去查 Agent 有没有连上、采集是不是坏了——而真正的原因是他的账号只覆盖某几条业务线。
const scopeLimited = computed(() => {
  const sc = auth.principal && auth.principal.scope
  return !!sc && sc.assetMode === 'limited' // useAuth 已把服务端的 snake_case 归一成这个形状
})
const scopeLabelText = computed(() => {
  const sc = (auth.principal && auth.principal.scope) || {}
  const labels = Array.isArray(sc.assetLabels) ? sc.assetLabels : []
  return labels.map((l) => `${l.key}=${l.value}`).join('、')
})

// 列表空态：区分「台账本身就是空的」与「被筛选掉了」，并指向下一步动作
const listEmptyTitle = computed(() => {
  if (scopeLimited.value) return '当前业务范围内没有资产'
  return summary.value.total > 0 ? '没有匹配的资产' : '资产台账为空'
})
const listHints = computed(() => {
  if (scopeLimited.value) {
    return [
      `你的账号限定了业务标签：${scopeLabelText.value || '（未配置任何取值 → 看不到任何资产）'}`,
      '业务范围与节点范围是「且」的关系：两个范围都放行才看得见该资产',
      '没打标签的资产不属于任何业务范围；需要更多资产请联系管理员调整范围或补打标签',
    ]
  }
  if (summary.value.total > 0) {
    return [
      `台账共 ${summary.value.total} 条资产，当前筛选条件下没有匹配项`,
      '清空上方筛选条件，或切换「含已忽略」再查一次',
    ]
  }
  return [
    '资产在 Agent 首次上报后自动出现：确认至少有一台 Agent 已连上 Server',
    '也可以直接人工建档，新建的资产会立即出现在列表里',
  ]
})

// 「含已忽略」开关：映射到 filter.ignored（with / 空串=默认隐藏）
const showIgnored = computed({
  get: () => filter.value.ignored === 'with',
  set: (v) => {
    filter.value = { ...filter.value, ignored: v ? 'with' : '' }
    reload()
  },
})

// 列表与摘要共用同一套筛选参数，保证「点数字看到的」与「数字本身」一致
function filterParams() {
  const params = {
    type: filter.value.type,
    status: filter.value.status,
    source: filter.value.source,
    node: filter.value.node,
    keyword: filter.value.keyword,
    label: filter.value.label,
    // ignored 空串=默认隐藏已忽略；with=连已忽略一起看
    ignored: filter.value.ignored,
  }
  if (drill.value.ownerMissing) params.ownerMissing = 'true'
  if (drill.value.conflict) params.conflict = 'true'
  return params
}

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    const params = { ...filterParams(), limit: pageSize.value, offset: (page.value - 1) * pageSize.value }
    const [res, sum] = await Promise.all([listAssets(params), getAssetSummary(filterParams())])
    items.value = (res && res.assets) || []
    total.value = (res && res.total) || 0
    summary.value = sum || summary.value
  } catch (e) {
    loadError.value = e.message || '加载资产失败'
    items.value = []
    total.value = 0
  } finally {
    loading.value = false
  }
}

function reload() {
  page.value = 1
  load()
}

function resetFilter() {
  filter.value = { type: '', status: '', source: '', node: '', keyword: '', label: '', ignored: '' }
  drill.value = { ownerMissing: false, conflict: false, ignored: false }
  reload()
}

/* ================= 健康度下钻 ================= */

// 每次下钻都先清掉其它下钻态：否则"先看无责任人、再点已忽略"会叠加成两个条件，
// 界面上的数字与列表就会对不上，而用户很难发现是残留条件造成的。
function clearDrillState() {
  drill.value = { ownerMissing: false, conflict: false, ignored: false }
  filter.value = { ...filter.value, ignored: '' }
}
function drillAll() {
  resetFilter()
}
function drillStatus(status) {
  clearDrillState()
  filter.value = { ...filter.value, status }
  reload()
}
function drillNoOwner() {
  clearDrillState()
  drill.value.ownerMissing = true
  reload()
}
function drillConflict() {
  clearDrillState()
  drill.value.conflict = true
  reload()
}
// 已忽略下钻：看"我藏起来的那些资产"，可逐个恢复
function drillIgnored() {
  clearDrillState()
  drill.value.ignored = true
  filter.value = { ...filter.value, ignored: 'only' }
  reload()
}

// 撤销单条生效条件（来自筛选摘要条的 chips）。key 带维度前缀（filter.* / drill.*），
// 因此这里能精确清除，不必猜"这个 ignored 到底是筛选里的还是下钻里的"。
function removeChip(key) {
  if (key.startsWith('filter.')) {
    filter.value = { ...filter.value, [key.slice('filter.'.length)]: '' }
  } else if (key.startsWith('drill.')) {
    const field = key.slice('drill.'.length)
    drill.value = { ...drill.value, [field]: false }
    // 「已忽略」下钻同时把 filter.ignored 置成了 only，撤销时一并还原
    if (field === 'ignored') filter.value = { ...filter.value, ignored: '' }
  }
  reload()
}

function onPageChange(p) {
  page.value = p
  load()
}
function onSizeChange(size) {
  pageSize.value = size
  page.value = 1
  load()
}

/* ================= 选择与行交互 ================= */

function onSelectionChange(rows) {
  selection.value = rows
}
function clearSelection() {
  selection.value = []
  tableRef.value?.clearSelection?.()
}

/* ================= 详情抽屉（三 Tab 各自取数，互不牵连） ================= */

const detailVisible = ref(false)
const detail = ref(null)
const history = ref([])
const links = ref([])
// 被人工隐藏（逻辑删除）的关系：必须让用户看见并能恢复
const suppressed = ref([])
const tab = ref('attr')
// 各资产类型当前的期望值（标杆）来源，用于抽屉里显示"本资产是不是标杆"
const baselines = ref([])
// 三个 Tab 各自的加载/错误态：此前四路数据走 Promise.all，任一失败整个抽屉打不开，
// 而用户最需要的「属性对比」其实早就回来了。
const attrLoading = ref(false)
const attrError = ref('')
const historyLoading = ref(false)
const historyError = ref('')
const linksLoading = ref(false)
const linksError = ref('')

// applyLinks 把关联载荷落进状态：可见的边 + 被人工隐藏的边。
// 两者永远一起更新（写接口返回的也是同一个载荷），避免"表格已更新、隐藏区还是旧的"。
function applyLinks(payload) {
  links.value = (payload && payload.links) || []
  suppressed.value = (payload && payload.suppressed) || []
}

async function loadAttr(id) {
  attrLoading.value = true
  attrError.value = ''
  try {
    detail.value = await getAsset(id)
  } catch (e) {
    attrError.value = e.message || '加载资产详情失败'
  } finally {
    attrLoading.value = false
  }
  // 标杆清单失败不影响属性对比（只是少一句"本类型当前标杆是哪台"）
  try {
    const bl = await listInspectBaselines()
    baselines.value = (bl && bl.baselines) || []
  } catch (e) {
    baselines.value = []
  }
}

async function loadHistory(id) {
  historyLoading.value = true
  historyError.value = ''
  try {
    const hist = await getAssetHistory(id)
    history.value = (hist && hist.records) || []
  } catch (e) {
    historyError.value = e.message || '加载变更历史失败'
  } finally {
    historyLoading.value = false
  }
}

async function loadLinks(id) {
  linksLoading.value = true
  linksError.value = ''
  try {
    applyLinks(await getAssetLinks(id))
  } catch (e) {
    linksError.value = e.message || '加载关联关系失败'
  } finally {
    linksLoading.value = false
  }
}

const retryAttr = () => (detail.value ? loadAttr(detail.value.id) : undefined)
const retryHistory = () => (detail.value ? loadHistory(detail.value.id) : undefined)
const retryLinks = () => (detail.value ? loadLinks(detail.value.id) : undefined)

async function openDetail(row) {
  detail.value = row
  history.value = []
  links.value = []
  suppressed.value = []
  baselines.value = []
  attrError.value = ''
  historyError.value = ''
  linksError.value = ''
  tab.value = 'attr'
  detailVisible.value = true
  // 三段并行取、各自降级：任一段失败只影响它自己那一段
  await Promise.all([loadAttr(row.id), loadHistory(row.id), loadLinks(row.id)])
}

// 写入后统一刷新：详情（若打开着当前资产）、列表与摘要
async function afterWrite(id) {
  if (detailVisible.value && detail.value && String(detail.value.id) === String(id)) {
    await Promise.allSettled([loadAttr(id), loadHistory(id), loadLinks(id)])
  }
  await load()
}

/* ================= 关联关系的人工维护 ================= */

const linkAddVisible = ref(false)
const linkBusy = ref(false)
const linkForm = reactive({ direction: 'out', kind: 'runs_on', peer: '' })
const peerOptions = ref([])
const peerLoading = ref(false)

// linkAddress 把一条「读出来的边」翻译成写接口的寻址参数。
//
// 读接口返回的 direction 与 peer 就是写接口需要的全部信息，原样回传即可——
// 前端不自己算方向，也就不会出现"以为是出边、实际删了入边"。
function linkAddress(row) {
  return { toType: row.peerType, toKey: row.peerKey, kind: row.kind, direction: row.direction }
}

function openLinkAdd() {
  linkForm.direction = 'out'
  linkForm.kind = 'runs_on'
  linkForm.peer = ''
  peerOptions.value = []
  linkAddVisible.value = true
}

// searchLinkPeers 搜索对端候选：直接复用台账列表接口。
// 它已按资源范围裁剪，因此这里不必再判断"这个人能不能看到那台资产"。
async function searchLinkPeers(keyword) {
  const kw = (keyword || '').trim()
  if (!kw) {
    peerOptions.value = []
    return
  }
  peerLoading.value = true
  try {
    const resp = await listAssets({ keyword: kw, limit: 20 })
    peerOptions.value = ((resp && resp.assets) || [])
      // 自己跟自己建关系服务端也会拒，从候选里剔掉体验更好
      .filter((a) => String(a.id) !== String(detail.value && detail.value.id))
      .map((a) => ({
        value: a.typeKey + '|' + a.naturalKey,
        label: (a.name || a.naturalKey) + '（' + typeLabel(a) + '）',
      }))
  } catch (e) {
    ElMessage.error(e.message || '搜索资产失败')
  } finally {
    peerLoading.value = false
  }
}

async function submitLinkAdd() {
  if (!linkForm.peer) {
    ElMessage.warning('请选择对端资产')
    return
  }
  // 选项值形如「<类型>|<自然键>」；自然键本身含冒号（如 redis:127.0.0.1:6379），
  // 因此只按**第一个**分隔符切开，而不是 split 后取前两段。
  const sep = linkForm.peer.indexOf('|')
  linkBusy.value = true
  try {
    applyLinks(await createAssetLink(detail.value.id, {
      toType: linkForm.peer.slice(0, sep),
      toKey: linkForm.peer.slice(sep + 1),
      kind: linkForm.kind,
      direction: linkForm.direction,
    }))
    linkAddVisible.value = false
    ElMessage.success('已添加关系')
  } catch (e) {
    ElMessage.error(e.message || '添加关系失败')
  } finally {
    linkBusy.value = false
  }
}

async function removeLink(row) {
  // 解除 = 逻辑删除：服务端会记一条抑制，采集不会再把它建回来。
  // 这不是"少显示一行"，而是表达了「这条关系不存在」这个判断，所以先把后果说清楚。
  try {
    await ElMessageBox.confirm(
      '解除后采集不会再自动建立这条关系（服务端会记一条抑制记录，可在「已人工隐藏」里恢复）。确认解除？',
      '解除关系',
      { type: 'warning', confirmButtonText: '解除', cancelButtonText: '取消' }
    )
  } catch {
    return
  }
  try {
    applyLinks(await deleteAssetLink(detail.value.id, linkAddress(row)))
    ElMessage.success('已解除，可在「已人工隐藏」里恢复')
  } catch (e) {
    ElMessage.error(e.message || '解除关系失败')
  }
}

async function restoreLink(row) {
  try {
    applyLinks(await restoreAssetLink(detail.value.id, linkAddress(row)))
    ElMessage.success('已恢复：这条关系交还给采集')
  } catch (e) {
    ElMessage.error(e.message || '恢复关系失败')
  }
}

/* ================= 生命周期：忽略 / 恢复 / 彻底删除 ================= */

// 行内「更多」菜单：忽略 / 恢复（彻底删除已移入详情抽屉的危险区）
function onRowCommand(cmd, row) {
  if (cmd === 'ignore') doIgnore(row)
  else if (cmd === 'restore') doRestore(row)
}

// 忽略：从台账隐藏。采集仍在继续，因此这不是删除，也不会被下一轮上报复活。
async function doIgnore(row) {
  const reason = await ElMessageBox.prompt(
    `${row.name || row.naturalKey}\n\n忽略后该资产从台账与统计中隐藏（采集仍在继续），可随时恢复。`,
    '忽略资产',
    { type: 'warning', confirmButtonText: '忽略', cancelButtonText: '取消', inputPlaceholder: '忽略理由（可选，便于日后理解为什么藏了它）' },
  ).then((r) => r.value).catch(() => null)
  if (reason === null) return
  try {
    await ignoreAsset(row.id, reason || '')
    ElMessage.success('已忽略（可在筛选栏勾选「含已忽略」查看或恢复）')
    await afterWrite(row.id)
  } catch (e) {
    ElMessage.error(e.message || '忽略失败')
  }
}

// 恢复：重新纳入台账
async function doRestore(row) {
  try {
    await restoreAsset(row.id)
    ElMessage.success('已恢复到台账')
    await afterWrite(row.id)
  } catch (e) {
    ElMessage.error(e.message || '恢复失败')
  }
}

// 彻底删除：仅纯人工资产（采集资产会被下一轮上报重建，服务端也会拒绝）
async function doPurge(row) {
  if (!(await confirmWrite(`将彻底删除「${row.name || row.naturalKey}」及其变更历史与快照，删除后不可恢复`))) return
  try {
    await purgeAsset(row.id)
    ElMessage.success('已彻底删除')
    detailVisible.value = false
    await afterWrite(row.id)
  } catch (e) {
    ElMessage.error(e.message || '删除失败')
  }
}

/* ================= 新建 / 编辑 ================= */

const createVisible = ref(false)
const editVisible = ref(false)
const editId = ref('')
const editAttrs = ref([])
const form = ref({ typeKey: 'host', naturalKey: '', name: '', node: '' })
// 表单与列表列一一对应：类型（主机/实例 + 实例类型）、责任人（owner 键）
const assetKind = ref('host')
const instanceType = ref('redis')
const instanceAddr = ref('')
const ownerInput = ref('')
const editHadOwner = ref(false)
// 编辑前的名称：用于判断「名称是否真的被改过」，未改则不提交该字段
const editOriginalName = ref('')
// 标签编辑：表单里的行 + 原标签（后者用于推断"哪些键被删掉了"）
const editLabels = ref([])
const editOriginalLabels = ref({})
const saving = ref(false)

// 实例的自然键 = <类型>:<地址>，由表单自动拼出
const naturalKeyPreview = computed(() => {
  const addr = instanceAddr.value.trim()
  return instanceType.value && addr ? `${instanceType.value}:${addr}` : ''
})

const editLabel = computed(() => {
  const kind = form.value.typeKey === 'host' ? '主机' : '中间件实例'
  return `${form.value.naturalKey}（${kind}）`
})

// ---- 属性字典（设计件 2026-10-08-asset-attr-dictionary-design.md）----
//
// 与 useAssetTypes 分开：那个只保留类型清单（key/title），编辑弹窗要的是 schema.attrMeta，
// 用它把「值」输入换成对应的控件（enum 下拉 / 日期选择 / 布尔开关）。
const attrDict = ref({})
let attrDictCache = null

async function loadAttrDict(typeKey) {
  if (!attrDictCache) {
    try {
      const data = await getAssetTypes()
      const cache = {}
      for (const t of (data?.types || [])) cache[t.key] = t.schema?.attrMeta || {}
      attrDictCache = cache
    } catch (e) {
      // 字典只是编辑体验的加成：取不到就退回自由文本输入，不阻断编辑
      attrDictCache = {}
    }
  }
  attrDict.value = attrDictCache[typeKey] || {}
}

// 新建弹窗换类型时重载字典（否则会拿主机的字典去渲染中间件实例的属性）
watch(assetKind, (kind) => {
  if (createVisible.value) loadAttrDict(kind === 'instance' ? 'middleware-instance' : 'host')
})

function openCreate() {
  assetKind.value = 'host'
  instanceType.value = 'redis'
  instanceAddr.value = ''
  ownerInput.value = ''
  form.value = { typeKey: 'host', naturalKey: '', name: '', node: '' }
  editAttrs.value = [{ key: '', value: '' }]
  // 新建时也按字典渲染（默认类型是主机；换类型时由 watch 重载）
  loadAttrDict(form.value.typeKey)
  createVisible.value = true
}

function openEdit(row) {
  editId.value = row.id
  // 字典异步取（不 await：弹窗要立刻打开，控件随字典到达再切换）
  loadAttrDict(row.typeKey)
  // 名称预填当前值：否则用户看到空框，会以为「名称改不了」或担心提交后名称被清空。
  editOriginalName.value = row.name || ''
  form.value = { typeKey: row.typeKey, naturalKey: row.naturalKey, name: row.name || '', node: row.node || '' }
  ownerInput.value = row.owner || ''
  editHadOwner.value = !!row.owner
  // 标签：预填当前值，并在提交时对比出"被删掉的键"（不需要额外的删除态）
  editOriginalLabels.value = { ...(row.labels || {}) }
  editLabels.value = Object.entries(row.labels || {}).map(([key, value]) => ({ key, value }))
  // 预填当前人工值：采集值不预填，避免误以为「提交就会覆盖采集值」
  editAttrs.value = (row.attrs || [])
    .filter((a) => a.source === 'manual' && a.key !== OWNER_KEY)
    .map((a) => ({ key: a.key, value: a.value }))
  if (!editAttrs.value.length) editAttrs.value = [{ key: '', value: '' }]
  editVisible.value = true
}

function addLabelRow() {
  editLabels.value.push({ key: '', value: '' })
}
function removeLabelRow(idx) {
  editLabels.value.splice(idx, 1)
}

function attrsPayload(rows) {
  const out = {}
  for (const row of rows) {
    const key = (row.key || '').trim()
    if (key) out[key] = row.value
  }
  return out
}

async function confirmWrite(action) {
  // 资产维护属高风险权限（服务端 HighRiskPermissions）：写之前让操作者再看一眼。
  try {
    await ElMessageBox.confirm(action, '确认修改资产', { type: 'warning', confirmButtonText: '确认', cancelButtonText: '取消' })
    return true
  } catch (e) {
    return false
  }
}

async function submitCreate() {
  let typeKey = 'host'
  let naturalKey = form.value.naturalKey.trim()
  let node = form.value.node.trim()
  if (assetKind.value === 'instance') {
    typeKey = 'middleware-instance'
    naturalKey = naturalKeyPreview.value
    if (!instanceType.value || !instanceAddr.value.trim()) {
      ElMessage.warning('实例类型与实例地址为必填项')
      return
    }
  } else if (!naturalKey) {
    ElMessage.warning('主机名为必填项')
    return
  }
  // 主机的归属节点默认是它自己：主机资产的 Node 恒等于 hostname（与服务端自动发现一致）
  if (assetKind.value === 'host' && !node) node = naturalKey
  const attrs = attrsPayload(editAttrs.value)
  const owner = ownerInput.value.trim()
  if (owner) attrs[OWNER_KEY] = owner
  const payload = { typeKey, naturalKey, name: form.value.name.trim(), node, attrs }
  if (!(await confirmWrite(`将新建资产 ${naturalKey}（归属节点 ${node || '未指定'}）`))) return
  saving.value = true
  try {
    await createAsset(payload)
    ElMessage.success('资产已创建')
    createVisible.value = false
    reload()
  } catch (e) {
    ElMessage.error(e.message || '创建失败')
  } finally {
    saving.value = false
  }
}

async function submitEdit() {
  const id = editId.value
  const attrs = attrsPayload(editAttrs.value)
  const resetAttrs = []
  const owner = ownerInput.value.trim()
  if (owner) attrs[OWNER_KEY] = owner
  else if (editHadOwner.value) resetAttrs.push(OWNER_KEY)
  const name = form.value.name.trim()
  // 名称预填的是原值：只有真正改过才提交（服务端把「空名称」视为不修改）
  const nameChanged = name !== '' && name !== editOriginalName.value.trim()

  // 标签：本次表单里的键值；原标签里不在其中的键就是要删除的
  const labels = {}
  for (const row of editLabels.value) {
    const key = (row.key || '').trim()
    if (key) labels[key] = (row.value || '').trim()
  }
  const remove = Object.keys(editOriginalLabels.value).filter((k) => !(k in labels))
  const labelsChanged = labelFingerprint(labels) !== labelFingerprint(editOriginalLabels.value)

  if (!nameChanged && !labelsChanged && !Object.keys(attrs).length && !resetAttrs.length) {
    ElMessage.warning('没有需要更新的内容')
    return
  }
  const what = []
  if (nameChanged) what.push(`资产名称改为「${name}」`)
  if (labelsChanged) what.push('更新标签')
  if (Object.keys(attrs).length || resetAttrs.length) what.push('写入人工值')
  if (!(await confirmWrite(`将${what.join('、')}；采集值不会被覆盖`))) return
  saving.value = true
  try {
    // 名称与人工值走台账接口；标签有独立接口（职责分开：属性 vs 分类维度）
    if (nameChanged || Object.keys(attrs).length || resetAttrs.length) {
      await updateAsset(id, { name: nameChanged ? name : '', attrs, resetAttrs })
    }
    if (labelsChanged) await updateAssetLabels(id, labels, remove)
    ElMessage.success('已保存')
    editVisible.value = false
    await afterWrite(id)
  } catch (e) {
    ElMessage.error(e.message || '保存失败')
  } finally {
    saving.value = false
  }
}

// 恢复采集值：删掉该字段的人工值，让生效值回落到采集值（不是把采集值写回）
async function restoreAttr(key) {
  const id = detail.value.id
  if (!(await confirmWrite(`将清除字段「${key}」的人工值，生效值回落为采集值`))) return
  try {
    detail.value = await updateAsset(id, { resetAttrs: [key] })
    ElMessage.success('已恢复采集值')
    await afterWrite(id)
  } catch (e) {
    ElMessage.error(e.message || '恢复失败')
  }
}

// 把本资产的当前配置设为该资产类型的期望值（标杆）：配置巡检的合规偏差以它为基准。
async function makeBaseline() {
  const d = detail.value
  if (!(await confirmWrite(`将把 ${d.name || d.naturalKey} 的当前配置设为「${typeLabel(d)}」类型的期望值（每个类型只保留一个）`))) return
  try {
    await setAssetBaseline(d.id)
    ElMessage.success('已设为期望值')
    await afterWrite(d.id)
  } catch (e) {
    ElMessage.error(e.message || '设置期望值失败')
  }
}

// 清除该资产所属类型的期望值：此后只做快照前后比对，不再产出合规偏差。
async function dropBaseline() {
  const d = detail.value
  if (!(await confirmWrite(`将清除「${typeLabel(d)}」类型的期望值，此后不再做合规偏差比对`))) return
  try {
    await clearAssetBaseline(d.id)
    ElMessage.success('已清除期望值')
    await afterWrite(d.id)
  } catch (e) {
    ElMessage.error(e.message || '清除期望值失败')
  }
}

/* ================= 批量维护与清单导出 ================= */

const exporting = ref(false)
const batchBusy = ref(false)
const batchResult = ref(null)
const batchResultVisible = ref(false)
const batchOwner = reactive({ visible: false, owner: '' })
const batchLabel = reactive({ visible: false, key: '', value: '', mode: 'set' })

// runBatch 是所有批量动作的统一出口：结果面板、部分成功的呈现、清空选中与刷新列表
// 只写一遍。各动作自己拼一遍的话，迟早在某个分支上漏掉"刷新列表"或"清空选中"。
async function runBatch(op, payload, actionLabel) {
  const ids = selection.value.map((r) => r.id)
  if (!ids.length) return
  batchBusy.value = true
  try {
    const { status, body } = await batchAssets({ ids, op, ...payload })
    // 没有 batch 字段 = 请求本身就没被接受（旧服务端 405、网关 404 之类），
    // 这时**不能**弹"逐条结论"面板：面板是给"每条改没改成"用的，
    // 用它去呈现"整个请求失败了"只会显示成"成功 0 条、失败 0 条"这种自相矛盾的话。
    if (!body || !body.batch) {
      ElMessage.error((body && body.error) || `批量维护未被执行（HTTP ${status}）：请确认服务端已升级到支持批量接口的版本`)
      return
    }
    const b = body.batch
    batchResult.value = {
      opLabel: b.opLabel || actionLabel,
      total: b.total || ids.length,
      ok: b.ok || 0,
      failed: b.failed || 0,
      items: b.items || [],
      message: (body && body.error) || '',
    }
    // 409（一条都没成功）也照常弹面板：items 里的逐条原因才是用户要看的东西
    batchResultVisible.value = true
    if (status >= 500) ElMessage.error('服务端处理失败，请稍后重试')
    clearSelection()
    await load()
  } catch (e) {
    ElMessage.error(e.message || '批量维护失败')
  } finally {
    batchBusy.value = false
  }
}

function openBatchOwner() {
  batchOwner.owner = ''
  batchOwner.visible = true
}
async function submitBatchOwner(clear) {
  const n = selection.value.length
  if (!n) return
  const owner = batchOwner.owner.trim()
  if (!clear && !owner) {
    ElMessage.warning('请填写责任人；要清空请点「清空责任人」')
    return
  }
  const what = clear ? `清空 ${n} 条资产的责任人` : `把 ${n} 条资产的责任人统一改为「${owner}」`
  if (!(await confirmWrite(`${what}；写入的是人工值，采集值不受影响`))) return
  batchOwner.visible = false
  await runBatch(clear ? 'ownerClear' : 'owner', clear ? {} : { owner }, clear ? '清除责任人' : '转派责任人')
}

function openBatchLabel() {
  batchLabel.key = ''
  batchLabel.value = ''
  batchLabel.mode = 'set'
  batchLabel.visible = true
}
async function submitBatchLabel() {
  const key = batchLabel.key.trim()
  if (!key) {
    ElMessage.warning('标签键不能为空')
    return
  }
  const n = selection.value.length
  const value = batchLabel.value.trim()
  const what =
    batchLabel.mode === 'set'
      ? `给 ${n} 条资产写入标签 ${key}${value ? '=' + value : ''}（同名标签会被覆盖）`
      : `从 ${n} 条资产上删除标签「${key}」`
  if (!(await confirmWrite(what))) return
  batchLabel.visible = false
  await runBatch('labels', batchLabel.mode === 'set' ? { labels: { [key]: value } } : { remove: [key] }, '打标签')
}

async function batchIgnore() {
  const n = selection.value.length
  if (!n) return
  const reason = await ElMessageBox.prompt(
    `把选中的 ${n} 条资产从台账隐藏（忽略）：采集仍在继续、可随时恢复，列表与统计默认不再计入。`,
    '批量忽略',
    {
      type: 'warning',
      confirmButtonText: '忽略',
      cancelButtonText: '取消',
      inputPlaceholder: '忽略理由（可选，便于日后理解为什么藏了它们）',
    },
  )
    .then((r) => r.value)
    .catch(() => null)
  if (reason === null) return
  await runBatch('ignore', { reason: reason || '' }, '忽略')
}

async function batchRestore() {
  const n = selection.value.length
  if (!n) return
  if (!(await confirmWrite(`把选中的 ${n} 条资产恢复到台账（解除忽略）`))) return
  await runBatch('restore', {}, '恢复')
}

// 导出清单：走服务端导出（与列表同一套筛选、不受分页限制），
// 因此导出的就是"筛出来的那一份"，而不是当前这一页。
async function doExport() {
  if (!(await confirmWrite('导出当前筛选命中的全部资产（不受分页限制，含责任人、标签与归属节点），以 CSV 下载'))) {
    return
  }
  exporting.value = true
  try {
    const name = await exportAssets(filterParams())
    ElMessage.success(`已导出 ${name}`)
  } catch (e) {
    ElMessage.error(e.message || '导出失败')
  } finally {
    exporting.value = false
  }
}

/* ================= 跨页联动（容器页 ⇄ 台账） ================= */

// openFromQuery 处理 ?id=<资产 ID>：容器页的「台账」按钮就是这么跳过来的。
//
// 只认领一次（消费后清掉 query）：否则用户关掉抽屉再刷新页面，抽屉会自己又弹出来。
async function openFromQuery() {
  const id = String(route.query.id || '').trim()
  if (!id) return
  router.replace({ path: '/assets', query: {} })
  try {
    const item = await getAsset(id)
    if (item && item.id) openDetail(item)
  } catch (e) {
    // 范围外与已删除在这里是同一句话：不给范围探测留信息（与服务端的 404 口径一致）
    ElMessage.warning('未找到该资产：可能已被删除，或不在你的资源范围内')
  }
}

// gotoContainer 跳到容器页看这个 Pod / 工作负载。
//
// 身份取自服务端解出的 container 字段（不是前端 split 自然键）：集群是 apiserver 地址
// （含 `://`），自己解析一次就可能解错，而解错的联动不报错、只会跳到别的对象上。
function gotoContainer(item) {
  const c = item && item.container
  if (!c) return
  const query = { cluster: c.cluster, namespace: c.namespace }
  if (item.typeKey === 'pod') {
    query.pod = c.name
  } else {
    // 工作负载没有单一日志可看：只定位到集群 + 命名空间，并切到工作负载 Tab
    query.tab = 'workloads'
  }
  router.push({ path: '/container', query })
}

// gotoAudit 跳到审计页并只筛「这一次操作」。
//
// 用深链（/audit?requestId=xxx）而不是就地弹个抽屉：审计行的全部信息都在那一页，
// 就地做个阉割版只会让人以为"审计就这么点内容"。
function gotoAudit(requestId) {
  if (!requestId) return
  router.push({ path: '/audit', query: { requestId } })
}

// gotoInspect 跳到配置巡检页（抽屉里"查看配置巡检"的出口）
function gotoInspect() {
  router.push({ path: '/inspect' })
}

/* ================= 关系图（拓扑） ================= */

// 图本身由 TopologyGraph 组件负责（与告警影响面共用）：这里只管"打开"。
const topoVisible = ref(false)
const topoTitle = computed(() => {
  const name = detail.value ? detail.value.name || detail.value.naturalKey : ''
  return name ? `关系图 · ${name}` : '关系图'
})

// openTopology 打开关系图（以当前详情抽屉里的资产为中心）。
function openTopology() {
  if (!detail.value) return
  topoVisible.value = true
}

onMounted(async () => {
  await load()
  await openFromQuery()
})
</script>

<style scoped>
/* 页头统一走 PageHeader；内容边距交给 MainLayout 的 .content，各页不再自带缩进 */
.action-bar {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  margin-bottom: 12px;
}
.action-note {
  margin-left: auto;
  text-align: right;
}
.alert-gap {
  margin-bottom: 12px;
}
.pager {
  display: flex;
  justify-content: flex-end;
  margin-top: 14px;
}
</style>
