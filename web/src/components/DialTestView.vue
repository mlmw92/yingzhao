<template>
  <div class="dialtest-view">
    <PageHeader title="服务拨测" desc="HTTP / HTTPS / TCP / ICMP 拨测监控，SSL 证书到期检测">
      <template #actions>
        <el-button type="primary" @click="showDialog = true">新建拨测任务</el-button>
      </template>
    </PageHeader>

    <SectionCard
      title="拨测任务列表"
      dense
      :error="loadError"
      error-retry
      @retry="load"
    >
      <div class="filter-bar">
        <el-input v-model="kw" size="small" class="w-m" clearable placeholder="搜索任务名 / 目标" />
        <el-select v-model="typeFilter" size="small" class="w-xs" placeholder="全部类型">
          <el-option label="全部类型" value="" />
          <el-option label="HTTP" value="http" />
          <el-option label="HTTPS" value="https" />
          <el-option label="TCP" value="tcp" />
          <el-option label="ICMP" value="icmp" />
        </el-select>
      </div>
      <el-table :data="filteredTasks" style="width: 100%" v-loading="loading">
        <template #empty>
          <EmptyState
            title="暂无拨测任务"
            :hints="['点右上角「新建拨测任务」，选择 HTTP / TCP / ICMP 与目标地址', '拨测由 Server 主动发起，无需在目标机安装 Agent']"
          />
        </template>
        <el-table-column prop="name" label="任务名称" min-width="118" show-overflow-tooltip />
        <el-table-column prop="type" label="类型" width="72">
          <template #default="{ row }"><el-tag size="small">{{ row.type }}</el-tag></template>
        </el-table-column>
        <el-table-column prop="target" label="目标" min-width="128" show-overflow-tooltip />
        <!-- 状态 / 最近延迟 / 24h 可用率拆成三列：
             状态回答"通不通"、延迟回答"多快"、可用率回答"稳不稳"，三者维度不同，
             挤在一个单元格里既无法排序，扫读时也分不清哪个数属于谁。 -->
        <el-table-column label="当前状态" width="102">
          <template #default="{ row }">
            <StatusPill :tone="curTone(row)" size="sm" dot :text="curText(row)" />
          </template>
        </el-table-column>
        <el-table-column label="延迟(ms)" width="100" class-name="num-cell">
          <template #default="{ row }">
            <span v-if="curResult(row)">{{ curResult(row).latency }}</span>
            <span v-else class="muted">—</span>
          </template>
        </el-table-column>
        <el-table-column label="可用率" width="96" class-name="num-cell">
          <template #default="{ row }">
            <span v-if="uptimePct(curResult(row)) !== null">{{ uptimePct(curResult(row)) }}%</span>
            <span v-else class="muted">—</span>
          </template>
        </el-table-column>
        <el-table-column prop="interval" label="间隔(s)" width="88" class-name="num-cell" />
        <el-table-column prop="timeout" label="超时(s)" width="88" class-name="num-cell" />
        <el-table-column label="启用" width="84" align="center">
          <template #default="{ row }">
            <el-switch :model-value="row.enabled" @change="(v) => toggleEnabled(row, v)" />
          </template>
        </el-table-column>
        <el-table-column label="告警级别" width="76">
          <template #default="{ row }">
            <StatusPill :tone="sevTone(row.severity)" size="sm" :text="sevLabel(row.severity)" />
          </template>
        </el-table-column>
        <el-table-column label="通知" width="82">
          <template #default="{ row }">
            <span v-if="!row.notify || row.notify.length === 0" class="muted">仅平台</span>
            <el-tag v-for="c in (row.notify || [])" :key="c" size="small" class="ch-tag">{{ chLabel(c) }}</el-tag>
          </template>
        </el-table-column>
        <!-- 对外发布（C3）：勾选后该任务会出现在免登录的「对外状态页」上 -->
        <el-table-column label="对外" width="92" align="center">
          <template #default="{ row }">
            <StatusPill v-if="row.public" tone="ok" size="sm" text="已发布" />
            <span v-else class="muted">—</span>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="104" fixed="right" class-name="op-col">
          <template #default="{ row }">
            <el-button link @click="editTask(row)">编辑</el-button>
            <el-button link type="danger" @click="deleteTask(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </SectionCard>

    <SectionCard v-if="!loadError && results.length > 0" title="最近拨测结果" dense>
      <el-table :data="pagedResults" style="width: 100%">
        <el-table-column prop="name" label="任务名" min-width="120" />
        <el-table-column prop="type" label="类型" width="80" />
        <el-table-column prop="target" label="目标" min-width="150" show-overflow-tooltip />
        <el-table-column label="状态" width="90">
          <template #default="{ row }">
            <StatusPill :tone="row.up ? 'ok' : 'danger'" size="sm" dot :text="row.up ? '正常' : '异常'" />
          </template>
        </el-table-column>
        <el-table-column label="原因" min-width="160" show-overflow-tooltip>
          <template #default="{ row }">
            <span v-if="!row.up" class="dial-err">{{ row.error || '未知错误' }}</span>
            <span v-else class="muted">-</span>
          </template>
        </el-table-column>
        <el-table-column prop="latency" label="延迟(ms)" width="100" sortable class-name="num-cell" />
        <!-- 延迟趋势：一眼看出是抖动还是持续劣化 -->
        <el-table-column label="延迟趋势" width="150">
          <template #default="{ row }">
            <Sparkline v-if="row.latencyHistory && row.latencyHistory.length > 1" :points="row.latencyHistory" unit="ms" />
            <span v-else class="muted">—</span>
          </template>
        </el-table-column>
        <el-table-column label="检测时间" width="160">
          <template #default="{ row }">
            <span v-if="row.checkedAt" class="num-cell">{{ fmtTime(row.checkedAt) }}</span>
            <span v-else class="muted">—</span>
          </template>
        </el-table-column>
        <el-table-column label="证书到期(天)" width="120">
          <template #default="{ row }">
            <span v-if="row.certExpiry" :class="certClass(row.certExpiry)" class="num-cell">{{ row.certExpiry.toFixed(0) }} 天</span>
            <span v-else class="muted">-</span>
          </template>
        </el-table-column>
      </el-table>
      <div v-if="results.length > resultPageSize" class="pager">
        <el-pagination
          layout="prev, pager, next, total"
          :total="results.length"
          :page-size="resultPageSize"
          :current-page="resultPage"
          @current-change="(p) => (resultPage = p)"
        />
      </div>
    </SectionCard>

    <!-- 新建/编辑对话框 -->
    <el-dialog v-model="showDialog" :title="editing ? '编辑拨测任务' : '新建拨测任务'" width="min(540px, 94vw)">
      <el-form :model="form" label-width="95px">
        <el-form-item label="名称"><el-input v-model="form.name" /></el-form-item>
        <el-form-item label="类型">
          <el-select v-model="form.type">
            <el-option label="HTTP" value="http" />
            <el-option label="HTTPS" value="https" />
            <el-option label="TCP" value="tcp" />
            <el-option label="ICMP" value="icmp" />
          </el-select>
        </el-form-item>
        <el-form-item label="目标"><el-input v-model="form.target" placeholder="host:port 或 host 或 域名" /></el-form-item>
        <el-form-item label="间隔(s)"><el-input-number v-model="form.interval" :min="10" :max="3600" /></el-form-item>
        <el-form-item label="超时(s)"><el-input-number v-model="form.timeout" :min="1" :max="60" /></el-form-item>
        <el-form-item label="启用"><el-switch v-model="form.enabled" /></el-form-item>
        <el-form-item label="对外发布">
          <el-switch v-model="form.public" />
          <span class="hint-inline">
            开启后出现在免登录的「对外状态页」上（只展示名称 / 可用性 / 延迟，不含目标地址与节点）
          </span>
        </el-form-item>
        <el-form-item label="告警级别">
          <el-select v-model="form.severity" placeholder="选择严重级别">
            <el-option label="紧急" value="critical" />
            <el-option label="警告" value="warning" />
            <el-option label="信息" value="info" />
          </el-select>
        </el-form-item>
        <el-form-item label="通知渠道">
          <el-select v-model="form.notify" multiple collapse-tags placeholder="留空=仅平台展示，不推送">
            <el-option label="邮件" value="email" />
            <el-option label="Webhook" value="webhook" />
            <el-option label="钉钉" value="dingtalk" />
            <el-option label="飞书" value="feishu" />
            <el-option label="企业微信" value="wecom" />
          </el-select>
        </el-form-item>
        <template v-if="form.type === 'https'">
          <el-form-item label="预警阈值(天)">
            <el-input-number v-model="form.cert_warn" :min="1" :max="365" :step="1" />
            <span class="hint-inline">剩余 ≤ 此天数触发「警告」（默认 30）</span>
          </el-form-item>
          <el-form-item label="告警阈值(天)">
            <el-input-number v-model="form.cert_crit" :min="1" :max="form.cert_warn || 30" :step="1" />
            <span class="hint-inline">剩余 ≤ 此天数触发「紧急」（默认 7）</span>
          </el-form-item>
        </template>
      </el-form>
      <template #footer>
        <el-button @click="showDialog = false">取消</el-button>
        <el-button type="primary" @click="saveTask">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import http from '../api/http'
import PageHeader from './common/PageHeader.vue'
import SectionCard from './common/SectionCard.vue'
import EmptyState from './common/EmptyState.vue'
import StatusPill from './common/StatusPill.vue'
import Sparkline from './common/Sparkline.vue'

const loading = ref(false)
const tasks = ref([])
const results = ref([])
const loadError = ref('')
const showDialog = ref(false)
const editing = ref(false)
const kw = ref('')
const typeFilter = ref('')
const resultPage = ref(1)
const resultPageSize = 10
const form = ref({ name: '', type: 'http', target: '', interval: 60, timeout: 10, enabled: true, public: false, severity: 'warning', notify: [], cert_warn: 30, cert_crit: 7 })

const filteredTasks = computed(() => {
  const k = kw.value.trim().toLowerCase()
  return tasks.value.filter((t) => {
    if (typeFilter.value && t.type !== typeFilter.value) return false
    if (!k) return true
    return (t.name || '').toLowerCase().includes(k) || (t.target || '').toLowerCase().includes(k)
  })
})

const pagedResults = computed(() => {
  const start = (resultPage.value - 1) * resultPageSize
  return results.value.slice(start, start + resultPageSize)
})

// 结果按任务名索引：任务表要显示「当前状态」而 results 只带 name
const resultByName = computed(() => {
  const m = {}
  for (const r of results.value) m[r.name] = r
  return m
})
function curResult(row) {
  return resultByName.value[row.name] || null
}
function curTone(row) {
  if (!row.enabled) return 'muted'
  const r = curResult(row)
  if (!r) return 'muted'
  return r.up ? 'ok' : 'danger'
}
function curText(row) {
  if (!row.enabled) return '已停用'
  const r = curResult(row)
  if (!r) return '未检测'
  return r.up ? '正常' : '异常'
}
function uptimePct(r) {
  if (!r || r.uptime == null || r.uptime < 0) return null
  return r.uptime
}

function fmtTime(ts) {
  if (!ts) return ''
  const d = new Date(ts)
  const p = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    const [taskData, resData] = await Promise.all([
      http.get('/api/v1/dialtest/tasks'),
      http.get('/api/v1/dialtest/latest'),
    ])
    tasks.value = taskData.tasks || []
    results.value = resData.results || []
    if (resultPage.value > 1 && (resultPage.value - 1) * resultPageSize >= results.value.length) {
      resultPage.value = 1
    }
  } catch (e) {
    // 此前只 console.error：整块列表空着，用户不知道是"没有任务"还是"没加载上"
    loadError.value = e.message || '加载拨测数据失败'
  } finally {
    loading.value = false
  }
}

function editTask(row) {
  editing.value = true
  form.value = { ...row }
  showDialog.value = true
}

async function saveTask() {
  try {
    if (editing.value) {
      await http.put(`/api/v1/dialtest/tasks/${form.value.id}`, form.value)
    } else {
      await http.post('/api/v1/dialtest/tasks', form.value)
    }
    showDialog.value = false
    editing.value = false
    form.value = { name: '', type: 'http', target: '', interval: 60, timeout: 10, enabled: true, public: false, severity: 'warning', notify: [], cert_warn: 30, cert_crit: 7 }
    await load()
  } catch (e) {
    ElMessage.error(e.message || '保存失败')
  }
}

async function deleteTask(row) {
  // 此前用原生 window.confirm：系统级白底弹窗与深色主题直接冲突，
  // 也与全站其它确认框（ElMessageBox）不是一套东西。
  try {
    await ElMessageBox.confirm(`确认删除任务 "${row.name}"？`, '删除拨测任务', {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  try {
    await http.del(`/api/v1/dialtest/tasks/${row.id}`)
    ElMessage.success('已删除')
    await load()
  } catch (e) {
    // 此前失败只 console.error，界面无任何反馈，用户看到的是「点了没反应」
    ElMessage.error(e.message || '删除失败')
  }
}

// 列表内直接开关启用状态：乐观更新，失败回滚并提示
async function toggleEnabled(row, val) {
  const prev = row.enabled
  row.enabled = val
  try {
    await http.put(`/api/v1/dialtest/tasks/${row.id}`, { ...row })
  } catch (e) {
    row.enabled = prev
    ElMessage.error(e.message || '切换启用状态失败')
  }
}

function certClass(days) { if (days <= 7) return 'metric-bad'; if (days <= 30) return 'metric-warn'; return 'metric-good' }

function sevLabel(s) { return s === 'critical' ? '紧急' : s === 'info' ? '信息' : '警告' }
function sevTone(s) { return s === 'critical' ? 'danger' : s === 'info' ? 'info' : 'warn' }
function chLabel(c) {
  return { email: '邮件', webhook: 'Webhook', dingtalk: '钉钉', feishu: '飞书', wecom: '企业微信' }[c] || c
}

onMounted(load)
</script>

<style scoped>
.dialtest-view { display: flex; flex-direction: column; gap: 12px; }
.filter-bar { display: flex; gap: 8px; flex-wrap: wrap; margin-bottom: 10px; }
/* 操作列：链接按钮靠右一行排开。用 flex + gap 而不是依赖 .el-button + .el-button
   的 12px 外边距——「更多」是 el-dropdown（inline-flex 包裹），相邻选择器对它不生效，
   三件套时容易被挤到第二行。 */
.dialtest-view :deep(.op-col .cell) {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 4px;
  white-space: nowrap;
}
.dialtest-view :deep(.op-col .cell .el-button + .el-button) { margin-left: 0; }
/* 表头不换行，避免“延迟(ms)”等列头被挤压换行 */
.dialtest-view :deep(th .cell) { white-space: nowrap; }
.num-cell { font-variant-numeric: tabular-nums; }
.dial-err { color: var(--danger); font-size: var(--fs-sm); word-break: break-all; }
.metric-good { color: var(--ok); }
.metric-warn { color: var(--warn); }
.metric-bad { color: var(--danger); }
.ch-tag { margin-right: 4px; }
.hint-inline { font-size: var(--fs-sm); color: var(--t3); margin-left: 8px; }
.pager { display: flex; justify-content: flex-end; margin-top: 10px; }
.el-form-item :deep(.el-input-number) { width: 120px; }
@media (max-width: 1024px) {
  .filter-bar .w-m { width: 100%; }
}
@media (max-width: 768px) {
  .dialtest-view :deep(.el-table) { font-size: var(--fs-xs); }
}
</style>
