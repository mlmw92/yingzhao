<template>
  <div class="approval-page">
    <el-card shadow="never">
      <template #header>
        <div class="head">
          <span>审批中心</span>
          <div class="tools">
            <el-button size="small" :loading="loading" @click="reload">刷新</el-button>
          </div>
        </div>
      </template>

      <!-- 没开审批时要明确说清楚，否则用户会以为"我提交的动作怎么没了" -->
      <el-alert
        v-if="!enabled"
        type="info"
        :closable="false"
        show-icon
        title="审批未开启"
        description="高风险动作提交后会直接下发（与开启前行为一致）。开启需要 approval:manage 权限。"
      />

      <el-tabs v-model="box" @tab-change="reload">
        <el-tab-pane label="待我审批" name="pending" />
        <el-tab-pane label="我的申请" name="mine" />
        <el-tab-pane v-if="canManage" label="设置" name="config" />
      </el-tabs>

      <template v-if="box !== 'config'">
        <el-table :data="rows" size="small" empty-text="暂无审批单">
          <el-table-column prop="id" label="单号" width="80" />
          <el-table-column prop="title" label="动作" min-width="220" show-overflow-tooltip />
          <el-table-column label="目标" width="160" show-overflow-tooltip>
            <template #default="{ row }">{{ targetsText(row) }}</template>
          </el-table-column>
          <el-table-column label="状态" width="90">
            <template #default="{ row }">
              <el-tag :type="stateType(row.state)" size="small">{{ stateLabel(row.state) }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="requester" label="申请人" width="100" />
          <el-table-column label="提交时间" width="160">
            <template #default="{ row }">{{ fmtTime(row.createdAt) }}</template>
          </el-table-column>
          <el-table-column label="操作" width="200" fixed="right">
            <template #default="{ row }">
              <el-button size="small" @click="openDetail(row)">详情</el-button>
              <!-- 自己提交的不能批（服务端也会拒），这里直接不给按钮 -->
              <el-button
                v-if="box === 'pending' && row.state === 'pending' && !row.mine"
                size="small"
                type="primary"
                @click="doApprove(row)"
              >
                批准
              </el-button>
              <el-button
                v-if="box === 'pending' && row.state === 'pending' && !row.mine"
                size="small"
                type="danger"
                @click="doReject(row)"
              >
                驳回
              </el-button>
              <el-button
                v-if="box === 'mine' && row.state === 'pending'"
                size="small"
                @click="doCancel(row)"
              >
                撤回
              </el-button>
            </template>
          </el-table-column>
        </el-table>
      </template>

      <template v-else>
        <el-form label-width="140px" style="max-width: 720px">
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
          <el-form-item>
            <el-button type="primary" :loading="saving" @click="saveConfig">保存</el-button>
          </el-form-item>
        </el-form>
      </template>
    </el-card>

    <el-drawer v-model="detailVisible" title="审批详情" size="min(540px, 94vw)">
      <template v-if="current">
        <el-descriptions :column="1" border size="small">
          <el-descriptions-item label="单号">{{ current.id }}</el-descriptions-item>
          <el-descriptions-item label="动作">{{ current.title }}</el-descriptions-item>
          <el-descriptions-item label="目标">{{ targetsText(current) }}</el-descriptions-item>
          <el-descriptions-item v-if="current.params && Object.keys(current.params).length" label="参数">
            {{ paramsText(current.params) }}
          </el-descriptions-item>
          <el-descriptions-item label="状态">
            <el-tag :type="stateType(current.state)" size="small">{{ stateLabel(current.state) }}</el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="理由">{{ current.reason || '（未填写）' }}</el-descriptions-item>
          <el-descriptions-item v-if="current.taskId" label="任务号">{{ current.taskId }}</el-descriptions-item>
          <el-descriptions-item v-if="current.message" label="说明">{{ current.message }}</el-descriptions-item>
        </el-descriptions>

        <!-- 审批链：把"谁申请、谁批、落到哪个任务"串起来，这就是事后回看的全部依据 -->
        <div class="chain-title">审批链</div>
        <el-timeline>
          <el-timeline-item :timestamp="fmtTime(current.createdAt)" type="primary">
            {{ current.requester }} 提交申请
          </el-timeline-item>
          <el-timeline-item v-if="current.decidedAt" :timestamp="fmtTime(current.decidedAt)"
            :type="current.state === 'rejected' ? 'danger' : 'success'">
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
import { ref, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  listApprovals, approveApproval, rejectApproval, cancelApproval,
  getApprovalConfig, saveApprovalConfig,
} from '../api/approval'
import useAuth from '../composables/useAuth'

const auth = useAuth()
const canManage = computed(() => auth.can('approval:manage'))

const box = ref('pending')
const rows = ref([])
const loading = ref(false)
const enabled = ref(true)
const entries = ref([])
const form = ref({ enabled: false, requireFor: [], ttlMinutes: 30, maxRequests: 500 })
const saving = ref(false)

const detailVisible = ref(false)
const current = ref(null)

const STATE_LABEL = {
  pending: '待审批',
  approved: '已批准',
  executed: '已执行',
  failed: '执行失败',
  rejected: '已驳回',
  cancelled: '已撤回',
  expired: '已失效',
}
const STATE_TYPE = {
  pending: 'warning',
  approved: 'success',
  executed: 'success',
  failed: 'danger',
  rejected: 'info',
  cancelled: 'info',
  expired: 'info',
}
const stateLabel = (s) => STATE_LABEL[s] || s
const stateType = (s) => STATE_TYPE[s] || 'info'

function fmtTime(ms) {
  if (!ms) return ''
  const d = new Date(ms)
  const p = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

function targetsText(row) {
  const ns = row.nodes && row.nodes.length ? row.nodes : (row.node ? [row.node] : [])
  if (!ns.length) return '系统级动作'
  if (ns.length <= 3) return ns.join(', ')
  return `${ns.slice(0, 3).join(', ')} 等 ${ns.length} 个`
}

function paramsText(params) {
  return Object.entries(params).map(([k, v]) => `${k}=${v}`).join('，')
}

async function reload() {
  if (box.value === 'config') {
    await loadConfig()
    return
  }
  loading.value = true
  try {
    const res = await listApprovals({ box: box.value })
    rows.value = res.approvals || []
    enabled.value = res.enabled !== false
  } catch (e) {
    ElMessage.error(e.message || '加载审批单失败')
  } finally {
    loading.value = false
  }
}

async function loadConfig() {
  try {
    const res = await getApprovalConfig()
    entries.value = res.entries || []
    form.value = { ...(res.config || {}) }
  } catch (e) {
    ElMessage.error(e.message || '加载审批设置失败')
  }
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
  padding: 12px;
}
.head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.tools {
  display: flex;
  gap: 8px;
}
.hint {
  color: var(--t3);
  font-size: 12px;
  line-height: 1.5;
}
.chain-title {
  margin: 18px 0 8px;
  font-weight: 600;
}
</style>
