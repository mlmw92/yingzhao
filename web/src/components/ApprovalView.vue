<template>
  <div class="approval-page">
    <PageHeader
      :title="view === 'config' ? '审批设置' : '审批中心'"
      :desc="view === 'config'
        ? '配置哪些高风险动作需要双人复核、复核有效期'
        : '高风险动作的双人复核队列：批准后立即执行，请先看清参数'"
    >
      <template #actions>
        <template v-if="view === 'config'">
          <el-button size="small" @click="backToList">返回列表</el-button>
          <el-button size="small" type="primary" :loading="saving" @click="saveConfig">保存</el-button>
        </template>
        <template v-else>
          <el-button size="small" :icon="RefreshRight" :loading="loading" @click="reload">刷新</el-button>
          <!-- 设置是「配置」而不是「队列」：不再和两个工作流 Tab 挤在一起，
               否则不同角色看到的 Tab 数都不一样（无 manage 权限的人看不到设置）。 -->
          <el-button v-if="canManage" size="small" :icon="Setting" @click="openConfig">设置</el-button>
        </template>
      </template>
    </PageHeader>

    <!-- ================= 列表视图 ================= -->
    <template v-if="view === 'list'">
      <!-- 没开审批时要明确说清楚，否则用户会以为"我提交的动作怎么没了" -->
      <el-alert
        v-if="!enabled"
        type="info"
        :closable="false"
        show-icon
        title="审批未开启"
        description="高风险动作提交后会直接下发（与开启前行为一致）。开启需要 approval:manage 权限。"
        class="page-alert"
      />

      <!-- 页面级错误 + 重试：此前失败只弹 toast，关掉之后页面就是一张空表，与"真的没有单子"分不清 -->
      <el-alert v-if="listError" type="error" :closable="false" show-icon class="page-alert">
        <template #title>
          {{ listError }}
          <el-button link type="primary" @click="reload">重试</el-button>
        </template>
      </el-alert>

      <el-tabs v-model="box" class="box-tabs" @tab-change="onTabChange">
        <el-tab-pane name="pending">
          <template #label>
            <span class="tab-label">待我审批</span>
            <!-- 待办数徽标：不切过去也知道有没有活要干 -->
            <span v-if="pendingCount > 0" class="tab-count">{{ pendingCount }}</span>
          </template>
        </el-tab-pane>
        <el-tab-pane name="mine">
          <template #label><span class="tab-label">我的申请</span></template>
        </el-tab-pane>
      </el-tabs>

      <SectionCard dense>
        <template #actions>
          <div class="filter-bar">
            <el-select v-model="filterState" size="small" placeholder="全部状态" clearable style="width: 132px">
              <el-option v-for="s in STATE_ORDER" :key="s" :label="STATE_LABEL[s]" :value="s" />
            </el-select>
            <el-input v-model="filterAction" size="small" placeholder="动作关键词" clearable style="width: 170px" />
            <el-input v-model="filterRequester" size="small" placeholder="申请人" clearable style="width: 130px" />
          </div>
        </template>

        <el-table :data="pagedRows" size="small" v-loading="loading" style="width: 100%">
          <template #empty>
            <EmptyState
              v-if="rows.length === 0"
              :icon="Files"
              :title="box === 'pending' ? '当前没有待审批的高风险动作' : '还没有提交过审批申请'"
              :hints="emptyHints"
            />
            <EmptyState
              v-else
              :icon="Files"
              title="当前筛选条件下没有审批单"
              :hints="['清空上方的状态 / 动作 / 申请人筛选即可看到全部']"
            />
          </template>
          <el-table-column prop="id" label="单号" width="80" />
          <el-table-column prop="title" label="动作" min-width="200" show-overflow-tooltip />
          <el-table-column label="目标" width="150" show-overflow-tooltip>
            <template #default="{ row }">{{ targetsText(row) }}</template>
          </el-table-column>
          <el-table-column label="状态" width="124">
            <template #default="{ row }">
              <StatusPill :tone="stateTone(row.state)" :icon="stateIcon(row.state)" :text="stateLabel(row.state)" />
            </template>
          </el-table-column>
          <el-table-column prop="requester" label="申请人" width="100" />
          <el-table-column label="提交时间" width="150">
            <template #default="{ row }">{{ fmtTime(row.createdAt) }}</template>
          </el-table-column>
          <el-table-column label="操作" width="132" fixed="right">
            <template #default="{ row }">
              <div class="row-actions">
                <!-- 只留一个可见主按钮（按上下文取最该做的那件事），其余进下拉：
                     此前 132px 里塞 3 个按钮，窄屏下直接换行或被裁。 -->
                <el-button
                  v-if="primaryAction(row) === 'approve'"
                  size="small"
                  type="primary"
                  @click="doApprove(row)"
                >批准</el-button>
                <el-button
                  v-else-if="primaryAction(row) === 'cancel'"
                  size="small"
                  @click="doCancel(row)"
                >撤回</el-button>
                <el-button v-else size="small" @click="openDetail(row)">详情</el-button>

                <el-dropdown trigger="click" @command="(c) => onRowCmd(c, row)">
                  <el-button size="small" text :icon="MoreFilled" title="更多操作" />
                  <template #dropdown>
                    <el-dropdown-menu>
                      <el-dropdown-item command="detail">查看详情</el-dropdown-item>
                      <el-dropdown-item v-if="canReject(row)" command="reject">驳回…</el-dropdown-item>
                    </el-dropdown-menu>
                  </template>
                </el-dropdown>
              </div>
            </template>
          </el-table-column>
        </el-table>

        <div class="pager-row">
          <el-pagination
            v-model:current-page="page"
            v-model:page-size="pageSize"
            :total="filteredRows.length"
            :page-sizes="[10, 20, 50]"
            layout="total, sizes, prev, pager, next"
            background
          />
        </div>
      </SectionCard>
    </template>

    <!-- ================= 设置视图 ================= -->
    <template v-else>
      <el-alert v-if="configError" type="error" :closable="false" show-icon class="page-alert">
        <template #title>
          {{ configError }}（下方为上次成功加载的值，修改前请确认）
          <el-button link type="primary" @click="loadConfig">重试</el-button>
        </template>
      </el-alert>
      <SectionCard title="审批规则">
        <el-form label-width="150px" class="config-form">
          <el-form-item label="启用审批">
            <el-switch v-model="form.enabled" />
            <div class="hint">
              关闭时高风险动作直接下发（与开启前一致）。单管理员部署下开启会导致任何动作都无法执行——
              至少要有一个别人能批准。
            </div>
          </el-form-item>
          <el-form-item label="需要审批的动作">
            <el-checkbox-group v-model="form.requireFor">
              <el-checkbox v-for="e in entries" :key="e.value" :value="e.value">
                {{ e.label }}
                <span class="hint">（批准还需 {{ e.permission || '相应权限' }}）</span>
              </el-checkbox>
            </el-checkbox-group>
          </el-form-item>
          <el-form-item label="有效期（分钟）">
            <el-input-number v-model="form.ttlMinutes" :min="1" :max="1440" />
            <div class="hint">超时未审批即失效，动作不会执行，需重新申请。</div>
          </el-form-item>
        </el-form>
      </SectionCard>
    </template>

    <el-drawer v-model="detailVisible" title="审批详情" size="min(560px, 94vw)">
      <template v-if="current">
        <el-descriptions :column="1" border size="small">
          <el-descriptions-item label="单号">{{ current.id }}</el-descriptions-item>
          <el-descriptions-item label="动作">{{ current.title }}</el-descriptions-item>
          <el-descriptions-item label="目标">{{ targetsText(current) }}</el-descriptions-item>
          <el-descriptions-item label="状态">
            <StatusPill :tone="stateTone(current.state)" :icon="stateIcon(current.state)" :text="stateLabel(current.state)" />
          </el-descriptions-item>
          <el-descriptions-item label="申请人">{{ current.requester }}</el-descriptions-item>
          <el-descriptions-item label="理由">{{ current.reason || '（未填写）' }}</el-descriptions-item>
          <el-descriptions-item v-if="current.taskId" label="任务号">{{ current.taskId }}</el-descriptions-item>
          <el-descriptions-item v-if="current.message" label="说明">{{ current.message }}</el-descriptions-item>
        </el-descriptions>

        <!-- 参数就是"批 / 不批"的依据：此前压成 k=v，k=v 一行，在 560px 抽屉里被截断，
             真正要看的那个值反而看不见。改为键值分行 + 高风险键高亮。 -->
        <template v-if="paramEntries.length">
          <div class="chain-title">参数</div>
          <div class="param-list">
            <div v-for="p in paramEntries" :key="p.key" class="param-row" :class="{ risky: p.risky }">
              <span class="param-key">{{ p.key }}</span>
              <span class="param-val">{{ p.value }}</span>
              <StatusPill v-if="p.risky" tone="warn" text="高风险" size="sm" />
            </div>
          </div>
        </template>

        <!-- 审批链：把"谁申请、谁批、落到哪个任务"串起来，这就是事后回看的全部依据 -->
        <div class="chain-title">审批链</div>
        <el-timeline>
          <el-timeline-item :timestamp="fmtTime(current.createdAt)" type="primary">
            {{ current.requester }} 提交申请
          </el-timeline-item>
          <el-timeline-item
            v-if="current.decidedAt"
            :timestamp="fmtTime(current.decidedAt)"
            :type="current.state === 'rejected' ? 'danger' : 'success'"
          >
            {{ current.decidedBy }} {{ current.state === 'rejected' ? '驳回' : '批准' }}
            <span v-if="current.decision">：{{ current.decision }}</span>
          </el-timeline-item>
          <el-timeline-item v-if="current.executedAt" :timestamp="fmtTime(current.executedAt)" type="success">
            已执行<span v-if="current.taskId">（任务 {{ current.taskId }}）</span>
          </el-timeline-item>
          <el-timeline-item v-if="current.state === 'expired'" type="info">
            超时未审批，动作未执行
          </el-timeline-item>
        </el-timeline>
      </template>
    </el-drawer>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  RefreshRight, Setting, MoreFilled, Files,
  Clock, CircleCheck, CircleClose, WarningFilled, Timer, RefreshLeft,
} from '@element-plus/icons-vue'
import {
  listApprovals, approveApproval, rejectApproval, cancelApproval,
  getApprovalConfig, saveApprovalConfig,
} from '../api/approval'
import useAuth from '../composables/useAuth'
import PageHeader from './common/PageHeader.vue'
import SectionCard from './common/SectionCard.vue'
import EmptyState from './common/EmptyState.vue'
import StatusPill from './common/StatusPill.vue'

const auth = useAuth()
const canManage = computed(() => auth.can('approval:manage'))

const view = ref('list')
const box = ref('pending')
const rows = ref([])
const loading = ref(false)
const listError = ref('')
const enabled = ref(true)
const pendingCount = ref(0)
const entries = ref([])
const form = ref({ enabled: false, requireFor: [], ttlMinutes: 30, maxRequests: 500 })
const saving = ref(false)
const configError = ref('')

const detailVisible = ref(false)
const current = ref(null)

// ---------- 筛选 + 分页 ----------
const filterState = ref('')
const filterAction = ref('')
const filterRequester = ref('')
const page = ref(1)
const pageSize = ref(10)

const STATE_LABEL = {
  pending: '待审批',
  approved: '已批准·待执行',
  executed: '已执行',
  failed: '执行失败',
  rejected: '已驳回',
  cancelled: '已撤回',
  expired: '已失效',
}
const STATE_ORDER = ['pending', 'approved', 'executed', 'failed', 'rejected', 'cancelled', 'expired']

// 状态三重编码之一：颜色之外再给图标，红绿色盲 / 黑白打印下也能区分。
// approved（已批准·待执行）与 executed（已执行）此前同为 success 色，完全分不开。
const STATE_TONE = {
  pending: 'warn',
  approved: 'info',
  executed: 'ok',
  failed: 'danger',
  rejected: 'danger',
  cancelled: 'muted',
  expired: 'muted',
}
const STATE_ICON = {
  pending: Clock,
  approved: Timer,
  executed: CircleCheck,
  failed: WarningFilled,
  rejected: CircleClose,
  cancelled: RefreshLeft,
  expired: Timer,
}
const stateLabel = (s) => STATE_LABEL[s] || s
const stateTone = (s) => STATE_TONE[s] || 'muted'
const stateIcon = (s) => STATE_ICON[s] || Clock

function fmtTime(ms) {
  if (!ms) return ''
  const d = new Date(ms)
  const p = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

// targetsText 是审批单的"目标"一句话。
//
// 三种形态必须分开说，否则审批人会按错的印象放行：
//   - 有节点（ops / 处置 / Agent 升级）：节点清单；Docker 写再加上端点——
//     同名端点可能出现在多台机器上，只给节点名对不上是哪台 daemon；
//   - 无节点但有集群（K8s 写操作，Server 直连 apiserver）：集群/命名空间。
//     **不能显示成"系统级动作"**——那会让人以为改的是平台自己；
//   - 都没有（Server 升级 / 回滚）：才是系统级动作。
function targetsText(row) {
  const ns = row.nodes && row.nodes.length ? row.nodes : (row.node ? [row.node] : [])
  const params = row.params || {}
  if (ns.length) {
    if (params.endpoint && ns.length === 1) return `${ns[0]}/${params.endpoint}`
    if (ns.length <= 3) return ns.join(', ')
    return `${ns.slice(0, 3).join(', ')} 等 ${ns.length} 个`
  }
  if (params.cluster) return params.namespace ? `${params.cluster}/${params.namespace}` : params.cluster
  return '系统级动作'
}

// 高风险参数：批准即执行，这些键的值会被原样下发到机器上，必须一眼可见
const RISKY_KEY = /cmd|command|script|shell|exec|path|file|content|payload|sql|url|force|reboot|restart/i
const paramEntries = computed(() => {
  const p = (current.value && current.value.params) || {}
  return Object.entries(p).map(([key, value]) => ({
    key,
    value: value === null || value === undefined ? '' : String(value),
    risky: RISKY_KEY.test(key),
  }))
})

const filteredRows = computed(() => {
  const kw = filterAction.value.trim().toLowerCase()
  const rq = filterRequester.value.trim().toLowerCase()
  return rows.value.filter((r) => {
    if (filterState.value && r.state !== filterState.value) return false
    if (kw && !String(r.title || '').toLowerCase().includes(kw)) return false
    if (rq && !String(r.requester || '').toLowerCase().includes(rq)) return false
    return true
  })
})
const pagedRows = computed(() => {
  const start = (page.value - 1) * pageSize.value
  return filteredRows.value.slice(start, start + pageSize.value)
})

// 筛选/页长变化后回到第一页，否则会出现"总数 3 条却停在空白的第 5 页"
watch([filterState, filterAction, filterRequester, pageSize], () => {
  page.value = 1
})
watch(filteredRows, () => {
  const max = Math.max(1, Math.ceil(filteredRows.value.length / pageSize.value))
  if (page.value > max) page.value = max
})

const emptyHints = computed(() =>
  box.value === 'pending'
    ? [
        '没有待办说明当前没有需要你复核的高风险动作',
        '有人提交申请后，这里会立即出现（并显示待办数）',
      ]
    : [
        '你提交的高风险动作会出现在这里，可随时撤回尚未审批的申请',
        '需要审批的动作在「设置」里配置',
      ]
)

// ---------- 行内动作：一个可见主按钮 + 一个「更多」下拉 ----------
function primaryAction(row) {
  if (box.value === 'pending' && row.state === 'pending' && !row.mine) return 'approve'
  if (box.value === 'mine' && row.state === 'pending') return 'cancel'
  return 'detail'
}
function canReject(row) {
  return box.value === 'pending' && row.state === 'pending' && !row.mine
}
function onRowCmd(cmd, row) {
  if (cmd === 'detail') openDetail(row)
  else if (cmd === 'reject') doReject(row)
}

// ---------- 数据加载 ----------
function onTabChange() {
  filterState.value = ''
  filterAction.value = ''
  filterRequester.value = ''
  page.value = 1
  reload()
}

async function reload() {
  loading.value = true
  listError.value = ''
  try {
    const res = await listApprovals({ box: box.value })
    rows.value = res.approvals || []
    enabled.value = res.enabled !== false
    if (box.value === 'pending') pendingCount.value = rows.value.filter((r) => r.state === 'pending').length
  } catch (e) {
    listError.value = e.message || '加载审批单失败'
    rows.value = []
  } finally {
    loading.value = false
  }
  // 徽标要在另一个 Tab 上也保持正确：切到「我的申请」时单独取一次待办数
  if (box.value !== 'pending') refreshPendingCount()
}

async function refreshPendingCount() {
  try {
    const res = await listApprovals({ box: 'pending' })
    pendingCount.value = (res.approvals || []).filter((r) => r.state === 'pending').length
  } catch (e) {
    /* 徽标拿不到不致命，不打扰用户 */
  }
}

async function loadConfig() {
  configError.value = ''
  try {
    const res = await getApprovalConfig()
    entries.value = res.entries || []
    // 加载失败时不再静默覆盖：保留上次成功加载的值，并显式提示"这是旧值"
    form.value = { ...(res.config || {}) }
  } catch (e) {
    configError.value = e.message || '加载审批设置失败'
  }
}

function openConfig() {
  view.value = 'config'
  loadConfig()
}
function backToList() {
  view.value = 'list'
  reload()
}

async function saveConfig() {
  saving.value = true
  try {
    await saveApprovalConfig(form.value)
    ElMessage.success('已保存（立即生效）')
    await loadConfig()
  } catch (e) {
    // 未知入口这类错误必须原样展示：静默接受会让管理员以为"配上了"，实际那条根本没被拦
    ElMessage.error(e.message || '保存失败')
  } finally {
    saving.value = false
  }
}

function openDetail(row) {
  current.value = row
  detailVisible.value = true
}

async function doApprove(row) {
  // 批准会**立即执行**：这句必须写进确认框。把它说成"通过审批"会让人以为还有一步。
  try {
    await ElMessageBox.confirm(
      `批准后**立即执行**：${row.title}。动作将以申请人（${row.requester}）的名义下发。`,
      '批准并执行', { type: 'warning', confirmButtonText: '批准并执行', cancelButtonText: '取消' },
    )
  } catch (e) {
    return
  }
  try {
    const res = await approveApproval(row.id, '')
    const a = res.approval || {}
    if (res.error) {
      ElMessage.error(res.error)
    } else if (a.state === 'executed') {
      ElMessage.success('已批准并下发（任务 ' + (a.taskId || '-') + '）')
    } else {
      ElMessage.warning('已批准，但动作未执行：' + (a.message || ''))
    }
    await reload()
  } catch (e) {
    ElMessage.error(e.message || '批准失败')
  }
}

async function doReject(row) {
  let note = ''
  try {
    const r = await ElMessageBox.prompt('驳回理由（必填，事后回看时"为什么不同意"比"谁不同意"更有用）', '驳回申请', {
      inputPlaceholder: '例如：变更窗口外 / 参数有误',
      inputValidator: (v) => (v && v.trim() ? true : '必须填写理由'),
      confirmButtonText: '驳回', cancelButtonText: '取消',
    })
    note = (r.value || '').trim()
  } catch (e) {
    return
  }
  try {
    await rejectApproval(row.id, note)
    ElMessage.success('已驳回')
    await reload()
  } catch (e) {
    ElMessage.error(e.message || '驳回失败')
  }
}

async function doCancel(row) {
  try {
    await ElMessageBox.confirm('撤回后该申请作废，需重新提交。', '撤回申请', {
      type: 'info', confirmButtonText: '撤回', cancelButtonText: '取消',
    })
  } catch (e) {
    return
  }
  try {
    await cancelApproval(row.id)
    ElMessage.success('已撤回')
    await reload()
  } catch (e) {
    ElMessage.error(e.message || '撤回失败')
  }
}

onMounted(reload)
</script>

<style scoped>
.approval-page {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.page-alert {
  margin: 0;
}
.box-tabs {
  margin-bottom: -4px;
}
.tab-label {
  font-size: var(--fs-base);
}
.tab-count {
  display: inline-block;
  margin-left: 6px;
  min-width: 18px;
  padding: 0 5px;
  border-radius: var(--r-pill);
  background: var(--danger);
  color: #fff;
  font-size: var(--fs-xs);
  font-weight: 700;
  line-height: 16px;
  text-align: center;
  vertical-align: 1px;
}
.row-actions {
  display: flex;
  align-items: center;
  gap: 4px;
}
.pager-row {
  display: flex;
  justify-content: flex-end;
  margin-top: 12px;
}
.config-form {
  max-width: 720px;
}
.hint {
  color: var(--t3);
  font-size: var(--fs-xs);
  line-height: 1.5;
}
.chain-title {
  margin: 18px 0 8px;
  font-weight: 600;
  color: var(--t1);
}
.param-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.param-row {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  padding: 8px 10px;
  border-radius: var(--r-md);
  background: var(--s2);
  border: 1px solid var(--bd);
}
.param-row.risky {
  border-color: var(--warn-bd);
  background: var(--warn-dim);
}
.param-key {
  flex-shrink: 0;
  min-width: 96px;
  font-family: var(--mono);
  font-size: var(--fs-xs);
  color: var(--t2);
  word-break: break-all;
}
.param-val {
  flex: 1;
  min-width: 0;
  font-size: var(--fs-sm);
  color: var(--t1);
  word-break: break-all;
  white-space: pre-wrap;
}
</style>
