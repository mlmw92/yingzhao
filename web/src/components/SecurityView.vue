<template>
  <div class="security-view">
    <PageHeader title="安全中心" desc="入侵防御、基线合规、文件完整性与安全事件的统一视图" />

    <RefreshBar :loading="refreshing" @refresh="refreshAll" />

    <!-- 加载失败提示：与「能力未启用」区分开，避免把失败当空数据 -->
    <el-alert
      v-if="loadError"
      type="error"
      :closable="false"
      show-icon
      class="sec-load-error"
    >
      <template #title>{{ failedCount }} 个模块加载失败：{{ loadError }}
        <el-button link type="primary" @click="refreshAll">全部重试</el-button>
      </template>
    </el-alert>

    <!-- 顶部 KPI 概览 -->
    <SectionCard id="sec-overview" title="安全概览" :loading="loading" :error="summaryErr" error-retry @retry="loadSummary">
      <div class="kpi-row">
        <div class="kpi-card score-kpi" :class="scoreClass">
          <div class="kpi-ring" :style="ringStyle">
            <div class="kpi-score">{{ summary.score.toFixed(0) }}</div>
          </div>
          <div class="kpi-meta">
            <div class="kpi-title">安全合规评分</div>
            <div class="kpi-sub">{{ summary.baselineHosts }} 台主机已评估</div>
          </div>
        </div>

        <KpiCard :value="summary.eventCount" label="安全事件" :tone="summary.eventCount > 0 ? 'alert' : 'total'">
          <template #icon>⚠</template>
        </KpiCard>

        <KpiCard :value="summary.riskNodes" label="风险主机" tone="warn">
          <template #icon>🛡</template>
        </KpiCard>

        <KpiCard :value="summary.fimChanges" label="文件完整性变化" tone="conn">
          <template #icon>📄</template>
        </KpiCard>
      </div>
    </SectionCard>

    <!-- 入侵防御（受控 fail2ban 专属 SSH 防护） -->
    <SectionCard id="sec-defense" title="入侵防御" dense v-loading="defenseLoading" :error="defenseErr" error-retry @retry="loadDefense">
      <template #actions>
        <span class="defense-tip">仅托管 yingzhao-monitor-sshd 专属 jail，不覆盖既有 fail2ban 配置</span>
      </template>

      <div class="defense-kpi" v-if="defenseSummary">
        <div class="dk-card"><span class="dk-val ok">{{ defenseSummary.protected }}</span><span class="dk-label">已防护</span></div>
        <div class="dk-card"><span class="dk-val">{{ defenseSummary.unmanaged }}</span><span class="dk-label">未启用</span></div>
        <div class="dk-card"><span class="dk-val warn">{{ defenseSummary.running }}</span><span class="dk-label">执行中</span></div>
        <div class="dk-card"><span class="dk-val bad">{{ defenseSummary.exception }}</span><span class="dk-label">异常</span></div>
      </div>

      <el-table :data="pagedDefenses" style="width: 100%; margin-top: 12px">
        <template #empty>
          <EmptyState
            :icon="Lock"
            title="暂无可防护节点"
            :hints="['Agent 需先上报才能托管专属 jail', '确认目标机已安装 fail2ban，且 Agent 以 root 运行']"
          />
        </template>
        <el-table-column label="节点" min-width="200">
          <template #default="{ row }">
            <div class="node-cell">
              <span class="node-ip">{{ row.nodeIp || row.node }}</span>
              <span v-if="row.displayName && row.displayName !== row.node" class="node-alias">· {{ row.displayName }}</span>
            </div>
            <span v-if="!row.agentSupported" class="need-upgrade">需升级 Agent</span>
          </template>
        </el-table-column>
        <el-table-column label="防护状态" width="150">
          <template #default="{ row }">
            <StatusPill :tone="defensePill(row).tone" :icon="defensePill(row).icon" :text="defenseStateText(row)" />
          </template>
        </el-table-column>
        <el-table-column label="jail" min-width="160">
          <template #default="{ row }">
            <span v-if="row.managedJail" class="jail-tag">yingzhao-monitor-sshd</span>
            <span v-else-if="row.jails && row.jails.length" class="dim">{{ row.jails.join(', ') }}</span>
            <span v-else class="muted">-</span>
            <div v-if="row.firewallAction" class="dim defense-action">{{ row.firewallAction }}</div>
          </template>
        </el-table-column>
        <el-table-column label="当前封禁" min-width="180">
          <template #default="{ row }">
            <template v-if="row.managedJail && row.firewallVerified">
              <span v-if="row.bannedIps && row.bannedIps.length" class="ban-count">{{ row.bannedIps.length }} 个 IP</span>
              <span v-else class="dim">当前无封禁</span>
              <div v-if="row.bannedIps && row.bannedIps.length" class="dim ban-ips" :title="row.bannedIps.join(', ')">{{ row.bannedIps.join(', ') }}</div>
            </template>
            <span v-else class="dim">未验证实际封禁</span>
          </template>
        </el-table-column>
        <el-table-column label="说明" min-width="200">
          <template #default="{ row }">
            <span class="dim">{{ row.message || (row.agentSupported ? '' : 'Agent 版本过低，不支持防护指令') }}</span>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="160" align="right">
          <template #default="{ row }">
            <el-button
              v-if="row.agentSupported && !row.managedJail"
              size="small" type="primary" plain
              :loading="actionId === row.node + '-enable'"
              @click="confirmDefense(row, 'enable')">启用防护</el-button>
            <el-button
              v-else-if="row.agentSupported && row.managedJail"
              size="small" type="warning" plain
              :loading="actionId === row.node + '-disable'"
              @click="confirmDefense(row, 'disable')">停用防护</el-button>
            <el-button v-else size="small" disabled>需升级 Agent</el-button>
          </template>
        </el-table-column>
      </el-table>
      <div class="pager-row">
        <el-pagination
          v-model:current-page="defCurrentPage"
          v-model:page-size="defPageSize"
          :total="sortedDefenses.length"
          :page-sizes="[10, 20, 50, 100]"
          layout="total, sizes, prev, pager, next, jumper"
          background
        />
      </div>
    </SectionCard>

    <!-- 防护操作确认 -->
    <el-dialog v-model="defDialogVisible" title="入侵防御操作确认" width="460px">
      <div v-if="defTarget">
        <p>即将对节点 <b>{{ defTarget.node }}</b> 执行
          <b>{{ defTarget.action === 'enable' ? '启用' : '停用' }}</b> 防护。</p>
        <ul class="def-confirm">
          <li>仅托管 <code>yingzhao-monitor-sshd</code> 专属 jail，不会修改你的 jail.local 或既有 jail。</li>
          <li>启用后会自动将你的操作来源 IP 加入 fail2ban 白名单，避免误封自己。</li>
          <li v-if="defTarget.action === 'disable'">停用只移除 yingzhao 专属配置并重载，<b>不卸载 fail2ban 软件包</b>。</li>
          <li>任务将在节点下次上报后由 Agent 异步执行，可在状态栏观察结果。</li>
        </ul>
      </div>
      <template #footer>
        <el-button @click="defDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="doDefenseAction">确认{{ defTarget && defTarget.action === 'enable' ? '启用' : '停用' }}</el-button>
      </template>
    </el-dialog>

    <!-- 护栏组合风险：平时是空的，因此**整卡不显示**（占着位置只会让人习惯性忽略它）；
         只有真有风险、或有节点没上报过这个检查时才出现 -->
    <SectionCard
      v-if="guardRiskNodes.length || guardUnreported || guardErr"
      id="sec-guard"
      title="护栏组合风险"
      dense
      v-loading="guardLoading"
      :error="guardErr"
      error-retry
      @retry="loadGuardRisks"
    >
      <template #actions>
        <span class="defense-tip">
          要消除它：去该机器的 agent.yaml 收掉 guards.ops.file.dirs 或 guards.ops.units 里的一个（改完需重启 Agent）
        </span>
      </template>
      <div v-for="n in guardRiskNodes" :key="n.node" class="guard-risk-node">
        <div class="guard-risk-head">
          <span class="node-name">
            {{ n.displayName || n.node }}<span v-if="n.nodeIp" class="node-ip"> · {{ n.nodeIp }}</span>
          </span>
          <StatusPill tone="danger" :text="n.risks.length + ' 条风险'" />
        </div>
        <div v-for="(r, i) in n.risks" :key="i" class="guard-risk-item">
          <StatusPill :tone="r.kind === 'exec' ? 'danger' : 'warn'" :text="r.kind === 'exec' ? '可执行文件暴露' : '写入即持久化'" />
          <span class="guard-risk-desc">{{ r.description }}</span>
        </div>
      </div>
      <!-- 未上报是"我们不知道"，不是"安全"——单独一行说清楚，别混进风险列表 -->
      <div v-if="guardUnreported" class="guard-risk-unknown">
        另有 {{ guardUnreported }} 台机器<strong>未上报该检查</strong>（Agent 需升级）——它们的状态是「未知」，不是「无风险」。
      </div>
    </SectionCard>

    <!-- 安全事件区 -->
    <SectionCard id="sec-events" title="安全事件" dense v-loading="loading" :error="eventsErr" error-retry @retry="loadEvents">
      <template #actions>
        <el-select v-model="filterCategory" placeholder="全部类别" clearable size="small" class="w-s" @change="loadEvents">
            <el-option v-for="c in categoryOptions" :key="c.value" :label="c.label" :value="c.value" />
          </el-select>
          <el-input v-model="filterNode" placeholder="按节点筛选" clearable size="small" class="w-s" @change="loadEvents" @clear="loadEvents" />
          <el-button size="small" @click="refreshAll">刷新</el-button>
      </template>

      <!-- 原本 max-height 写死 460：每页条数可切 10/20/50/100，
           于是切页时卡片高度在 380↔460 之间跳，100 条时表体内部还多出一个滚动区。
           条数已经由分页器控制，这里让表格按当前页自然展开，高度只随分页变化。 -->
      <el-table :data="pagedEvents" style="width: 100%" :row-class-name="rowClass">
        <template #empty>
          <EmptyState
            :icon="Lock"
            :title="filterCategory || filterNode ? '当前筛选下没有安全事件' : '暂无安全事件'"
            :hints="eventHints"
          />
        </template>
        <el-table-column label="级别" width="90">
          <template #default="{ row }">
            <StatusPill :tone="sevTone(row.severity)" :text="sevLabel(row.severity)" />
          </template>
        </el-table-column>
        <el-table-column label="类别" width="130">
          <template #default="{ row }">
            <el-tooltip v-if="catFix(row.category)" :content="'处置建议：' + catFix(row.category)" placement="top">
              <span class="cat-tag has-fix">{{ catLabel(row.category) }}</span>
            </el-tooltip>
            <span v-else class="cat-tag">{{ catLabel(row.category) }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="nodeIp" label="节点" width="150">
          <template #default="{ row }">
            <span :title="row.node">{{ row.nodeIp || row.node }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="message" label="描述" min-width="280" show-overflow-tooltip />
        <el-table-column label="来源 / 账户" width="190">
          <template #default="{ row }">
            <div v-if="row.sourceIp" class="src-block">
              <span class="dim">IP {{ row.sourceIp }}</span>
              <span v-if="row.sourceLocation" class="geo-tag" :title="row.sourceLocation">{{ row.sourceLocation }}</span>
            </div>
            <span v-if="row.user" class="dim">账户 {{ row.user }}</span>
            <span v-if="!row.sourceIp && !row.user" class="muted">-</span>
          </template>
        </el-table-column>
        <el-table-column label="时间" width="170">
          <template #default="{ row }">{{ fmtTime(row.timestamp) }}</template>
        </el-table-column>
      </el-table>
      <div class="pager-row">
        <el-pagination
          v-model:current-page="evCurrentPage"
          v-model:page-size="evPageSize"
          :total="events.length"
          :page-sizes="[10, 20, 50, 100]"
          layout="total, sizes, prev, pager, next, jumper"
          background
        />
      </div>
    </SectionCard>

    <!-- 基线与 FIM 区 -->
    <div class="grid-2">
      <!-- 基线合规明细 -->
      <SectionCard id="sec-baseline" title="基线合规明细" dense v-loading="loading" :error="baselinesErr" error-retry @retry="loadBaselines">
        <template #actions>
          <div class="panel-tools">
            <!-- 上百台服务器时，逐个滚动找一台很难；给一个节点名/IP 检索框 -->
            <el-input
              v-model="baselineKw"
              size="small"
              clearable
              placeholder="搜索节点 / IP"
              class="bl-search"
            />
            <el-button
              v-if="canExportMatrix"
              size="small"
              :loading="exportingMatrix"
              @click="exportMatrix"
            >
              导出矩阵 CSV
            </el-button>
            <el-switch
              v-model="onlyFailedBaseline"
              size="small"
              inline-prompt
              active-text="仅未通过"
              inactive-text="全部"
            />
          </div>
        </template>
        <EmptyState
          v-if="!filteredBaselines.length"
          :icon="Lock"
          :title="baselines.length ? (baselineKw ? '没有匹配的节点' : '没有未通过的节点') : '暂无基线数据'"
          :hints="
            baselines.length
              ? [
                  baselineKw
                    ? '清空搜索框可看全部节点'
                    : '当前所有节点都通过基线检查，关闭「仅未通过」可看全量明细',
                ]
              : ['基线数据来自配置巡检的 L2 快照，先跑一次巡检生成基线', '未产生快照的资产不会出现在这里']
          "
        />
        <div class="baseline-scroll">
          <div v-for="b in filteredBaselines" :key="b.node" class="baseline-node">
            <div class="baseline-head">
              <span class="node-name">{{ b.displayName || b.node }}<span v-if="b.nodeIp" class="node-ip"> · {{ b.nodeIp }}</span></span>
              <StatusPill :tone="scoreTone(b.score)" :text="b.score.toFixed(0)" />
            </div>
            <div class="baseline-items">
              <div v-for="it in b.items" :key="it.key" class="bl-item" :class="it.pass ? 'pass' : 'fail'">
                <span class="bl-dot"></span>
                <span class="bl-name">{{ it.name }}</span>
                <span class="bl-state">{{ it.pass ? '通过' : '未通过' }}</span>
                <!-- 修复建议：未通过项必须给出「怎么改」，否则只有"有问题"没有"怎么办" -->
                <span v-if="!it.pass && baselineFix(it.key)" class="bl-fix">整改：{{ baselineFix(it.key) }}</span>
              </div>
            </div>
          </div>
        </div>
      </SectionCard>

      <!-- FIM 变化时间线 -->
      <SectionCard title="文件完整性变化" dense v-loading="loading" :error="eventsErr" error-retry @retry="loadEvents">
        <EmptyState
          v-if="!fimEvents.length"
          :icon="Lock"
          title="暂无文件变化记录"
          :hints="['Agent 每轮上报时比对配置文件的 md5，变化才产生记录', '确认该节点已开启文件完整性采集']"
        />
        <div class="timeline">
          <div v-for="e in fimEvents" :key="e.id" class="tl-item">
            <span class="tl-dot" :class="'tl-' + e.detail.action"></span>
            <div class="tl-body">
              <div class="tl-msg">{{ e.message }}</div>
              <div class="tl-time">{{ fmtTime(e.timestamp) }} · {{ e.node }}</div>
            </div>
          </div>
        </div>
      </SectionCard>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { getSecuritySummary, getSecurityEvents, getSecurityBaselines, getDefenseStatuses, postDefenseAction, exportComplianceMatrix, getGuardRisks } from '../api/security'
import useAuth from '../composables/useAuth'
import { Lock, Check, Warning, WarningFilled } from '@element-plus/icons-vue'
import RefreshBar from './RefreshBar.vue'
import KpiCard from './KpiCard.vue'
import PageHeader from './common/PageHeader.vue'
import SectionCard from './common/SectionCard.vue'
import EmptyState from './common/EmptyState.vue'
import StatusPill from './common/StatusPill.vue'

const loading = ref(false)
const refreshing = ref(false)

// B4：错误**按模块拆分**。原实现共用一个 loadError，Promise.all 并发下
// 任一模块成功就把它清空 → 部分失败被静默吞掉。现在每个模块各持一个错误，
// 页面级汇总只用于"一眼看到几块挂了"。
const summaryErr = ref('')
const eventsErr = ref('')
const baselinesErr = ref('')
const defenseErr = ref('')
const guardErr = ref('')
const loadError = computed(() =>
  [summaryErr.value, eventsErr.value, baselinesErr.value, defenseErr.value, guardErr.value]
    .filter(Boolean)
    .join('；')
)
const failedCount = computed(() =>
  [summaryErr.value, eventsErr.value, baselinesErr.value, defenseErr.value, guardErr.value].filter(Boolean).length
)

const summary = ref({ score: 0, eventCount: 0, riskNodes: 0, fimChanges: 0, baselineHosts: 0 })
const events = ref([])
const baselines = ref([])
const guardRisks = ref([])
const guardLoading = ref(false)

// 只展示**真有风险**的节点：reported=true 且清单为空的节点是"已检查、无风险"，
// 不该出现在这张卡里（否则每次打开都有一排"无风险"，等于把这张卡变成噪音）。
const guardRiskNodes = computed(() => guardRisks.value.filter((n) => (n.risks || []).length > 0))
// 未上报的台数：那是"未知"，必须与"无风险"分开说（把未知显示成安全是本能力最坏的失败形态）。
const guardUnreported = computed(() => guardRisks.value.filter((n) => !n.reported).length)

// 安全事件空态：区分「确实没有事件」与「被筛选掉了」
const eventHints = computed(() => {
  if (filterCategory.value || filterNode.value) {
    return ['清空「类别」与「节点」筛选后再看一次', '也可以直接到「告警中心」「操作审计」交叉查证']
  }
  return [
    '没有事件通常是好事：安全模块未检测到异常',
    '若刚部署，确认目标机 Agent 已上报且安全采集项已开启',
  ]
})
const onlyFailedBaseline = ref(false)
// 节点检索：上百台服务器时靠滚动逐个找不现实
const baselineKw = ref('')
const filteredBaselines = computed(() => {
  const kw = baselineKw.value.trim().toLowerCase()
  let list = baselines.value
  if (onlyFailedBaseline.value) {
    list = list.filter((b) => !b.items || b.items.some((it) => !it.pass))
  }
  if (kw) {
    list = list.filter((b) => {
      const name = (b.displayName || '').toLowerCase()
      const node = (b.node || '').toLowerCase()
      const ip = (b.nodeIp || '').toLowerCase()
      return name.includes(kw) || node.includes(kw) || ip.includes(kw)
    })
  }
  return list
})

// 合规矩阵导出：服务端单设 security:export（一次把全部可见节点的合规结论落盘，
// 与"逐台翻看"不是一个量级的动作），因此无权限时**不显示按钮**而不是点下去才 403。
const auth = useAuth()
const canExportMatrix = computed(() => auth.can('security:export'))
const exportingMatrix = ref(false)

async function exportMatrix() {
  exportingMatrix.value = true
  try {
    const name = await exportComplianceMatrix()
    ElMessage.success('已导出 ' + name)
  } catch (e) {
    // 失败必须说出来：这里两种原因都要能区分（没有权限 / 还没有基线数据），
    // 静默失败会让人以为"导出没反应"而反复点。
    ElMessage.error((e && e.message) || '导出失败')
  } finally {
    exportingMatrix.value = false
  }
}
// 安全事件前端分页
const evCurrentPage = ref(1)
const evPageSize = ref(10)
const pagedEvents = computed(() => {
  const start = (evCurrentPage.value - 1) * evPageSize.value
  return events.value.slice(start, start + evPageSize.value)
})
watch(events, () => {
  const max = Math.max(1, Math.ceil(events.value.length / evPageSize.value))
  if (evCurrentPage.value > max) evCurrentPage.value = max
})
const filterCategory = ref('')
const filterNode = ref('')

// 受控入侵防御状态
const defenseLoading = ref(false)
const defenseStatuses = ref([])
const defDialogVisible = ref(false)
const defTarget = ref(null) // { node, action }
const actionId = ref('')

const defenseSummary = computed(() => {
  const s = { protected: 0, unmanaged: 0, running: 0, exception: 0 }
  for (const d of defenseStatuses.value) {
    if (!d.agentSupported) s.exception++
    else if (d.managedJail && d.running && d.firewallVerified) s.protected++
    else if (d.managedJail && d.running) s.running++
    else if (!d.supported) s.exception++
    else s.unmanaged++
  }
  return s
})

function defenseStateText(row) {
  if (!row.agentSupported) return '需升级 Agent'
  if (row.managedJail && row.running && row.firewallVerified) return '已防护'
  if (row.managedJail && row.running) return '仅审计/未验证'
  if (row.managedJail && !row.running) return '已停用'
  if (row.agentSupported && !row.managedJail) return '未启用'
  if (!row.supported) return '不支持'
  return '未知'
}
// 状态 → StatusPill 的 tone + 图标（图标是"颜色之外的第二编码"，色盲/黑白打印下仍可分辨）
function defensePill(row) {
  const cls = defenseBadgeClass(row)
  if (cls === 'good') return { tone: 'ok', icon: Check }
  if (cls === 'bad') return { tone: 'danger', icon: WarningFilled }
  if (cls === 'warn') return { tone: 'warn', icon: Warning }
  return { tone: 'muted', icon: null }
}
function defenseBadgeClass(row) {
  if (!row.agentSupported || !row.supported) return 'bad'
  if (row.managedJail && row.running && row.firewallVerified) return 'good'
  if (row.managedJail && row.running) return 'warn'
  if (row.managedJail) return 'warn'
  return 'neutral'
}

async function loadDefense() {
  // B5：defenseLoading 原从未置 true，却绑在 v-loading 上（等于永远不显示加载态）
  defenseLoading.value = true
  try {
    const d = await getDefenseStatuses()
    defenseStatuses.value = d.statuses || []
    defenseErr.value = ''
  } catch (e) {
    defenseStatuses.value = []
    defenseErr.value = (e && e.message) || '入侵防御状态加载失败'
  } finally {
    defenseLoading.value = false
  }
}

// IP 升序比较：点分十进制按四段数值比较，非标准 IP 退化为字符串比较
function ipCompare(a, b) {
  const pa = String(a || '').split('.')
  const pb = String(b || '').split('.')
  const isNum = (p) => p.length === 4 && p.every((x) => /^\d+$/.test(x))
  if (isNum(pa) && isNum(pb)) {
    for (let i = 0; i < 4; i++) {
      const x = +pa[i]
      const y = +pb[i]
      if (x !== y) return x - y
    }
    return 0
  }
  return String(a || '').localeCompare(String(b || ''))
}

// 稳定默认顺序：参考主机列表，按节点 IP 升序（不依赖后端顺序，刷新后保持一致）
const sortedDefenses = computed(() =>
  defenseStatuses.value.slice().sort((a, b) => ipCompare(a.nodeIp, b.nodeIp))
)

// 入侵防御列表前端分页
const defCurrentPage = ref(1)
const defPageSize = ref(10)
const pagedDefenses = computed(() => {
  const start = (defCurrentPage.value - 1) * defPageSize.value
  return sortedDefenses.value.slice(start, start + defPageSize.value)
})
watch(sortedDefenses, () => {
  const max = Math.max(1, Math.ceil(sortedDefenses.value.length / defPageSize.value))
  if (defCurrentPage.value > max) defCurrentPage.value = max
})

function confirmDefense(row, action) {
  defTarget.value = { node: row.node, action }
  defDialogVisible.value = true
}
async function doDefenseAction() {
  const t = defTarget.value
  if (!t) return
  actionId.value = t.node + '-' + t.action
  try {
    await postDefenseAction(t.node, t.action)
    ElMessage.success(t.action === 'enable' ? '已提交启用任务，节点将在下次上报后执行' : '已提交停用任务')
    defDialogVisible.value = false
    setTimeout(loadDefense, 1500)
  } catch (e) {
    const need = e && e.response && e.response.data && e.response.data.needAgentUpgrade
    ElMessage.error((e && e.response && e.response.data && e.response.data.error) || '操作失败')
    if (need) setTimeout(loadDefense, 800)
  } finally {
    actionId.value = ''
  }
}

const categoryOptions = [
  { value: 'ssh_bruteforce', label: 'SSH 暴力破解' },
  { value: 'ssh_audit', label: 'SSH 登录审计' },
  { value: 'fim', label: '文件完整性' },
  { value: 'process_anomaly', label: '异常进程' },
  { value: 'sudo_audit', label: 'sudo 提权审计' },
  // 高危命令是**事后发现**：由服务端从 sudo 命令文本里派生（不是 Agent 采的原始事件）。
  // 单列一类而不是混进 sudo 流水：sudo 审计量大且大多正常，混在一起等于把信号淹掉。
  { value: 'high_risk_cmd', label: '高危命令' },
  { value: 'cat_ban', label: 'fail2ban 封禁' },
]

const fimEvents = computed(() => events.value.filter((e) => e.category === 'fim').slice(0, 40))

const scoreClass = computed(() => {
  const s = summary.value.score
  if (s >= 80) return 'good'
  if (s >= 60) return 'warn'
  return 'bad'
})

const ringStyle = computed(() => {
  const pct = Math.max(0, Math.min(100, summary.value.score))
  const color = pct >= 80 ? 'var(--ok)' : pct >= 60 ? 'var(--warn)' : 'var(--danger)'
  return {
    background: `conic-gradient(${color} ${pct * 3.6}deg, var(--fill-2) 0deg)`,
  }
})

// 评分 → StatusPill tone（原 .score-pill 自绘一套）
function scoreTone(score) {
  if (score >= 80) return 'ok'
  if (score >= 60) return 'warn'
  return 'danger'
}

const SEV_TONE = { critical: 'danger', warning: 'warn', info: 'info' }
function sevTone(s) {
  return SEV_TONE[s] || 'muted'
}

// 修复建议：基线未通过项（数量最大）此前只有名称 + "未通过"，没有任何整改指引。
// 后端 SecurityBaselineItem 只给 Key/Name/Pass/Detail（检查说明，非整改指引），
// 因此整改动作由前端按 key 给出（与 Agent 侧 mkBaselineItem 的 key 一一对应）。
const BASELINE_FIX = {
  ssh_root_login: '编辑 /etc/ssh/sshd_config 设 PermitRootLogin no，执行 systemctl reload sshd 生效',
  ssh_password_auth: '设 PasswordAuthentication no 后 reload sshd（改前先确认密钥登录可用，避免把自己锁在外面）',
  firewall_enabled: '启用 ufw（ufw enable）或 firewalld（systemctl enable --now firewalld），并放行必要端口',
  fail2ban_running: '安装并启动 fail2ban：包管理器安装后 systemctl enable --now fail2ban',
  no_empty_password: '为存在空口令的账户设置口令或锁定：passwd <账户> / usermod -L <账户>',
}
function baselineFix(key) {
  return BASELINE_FIX[key] || ''
}

// 安全事件处置建议：按类别给出"下一步该做什么"，从"知道有问题"推进到"知道怎么改"
const CAT_FIX = {
  ssh_bruteforce: '确认来源 IP 是否可信；必要时在防火墙封禁，并启用 fail2ban 防护',
  ssh_audit: '核对登录时间与账户是否为本人操作；非本人立即改口令并排查来源 IP',
  fim: '确认该文件变更是否为你自己的配置调整；非预期变更立即回滚并排查入侵',
  process_anomaly: '核对该进程的启动来源与命令行；可疑时隔离主机并排查持久化项',
  sudo_audit: '确认提权操作是否已授权；异常提权立即改口令并检查 sudoers 配置',
  high_risk_cmd: '确认该高危命令是否授权执行；非预期执行立即排查来源与影响范围',
  cat_ban: '确认被封禁 IP 是否为攻击源；若为误封可将来源加入白名单',
}
function catFix(c) {
  return CAT_FIX[c] || ''
}

function sevLabel(s) {
  return { critical: '紧急', warning: '警告', info: '信息' }[s] || s
}
function catLabel(c) {
  const m = {
    ssh_bruteforce: 'SSH 暴力破解',
    ssh_audit: 'SSH 审计',
    fim: '文件完整性',
    process_anomaly: '异常进程',
    sudo_audit: 'sudo 审计',
    high_risk_cmd: '高危命令',
    cat_ban: 'fail2ban 封禁',
  }
  return m[c] || c
}
function rowClass({ row }) {
  return 'row-' + row.severity
}
function fmtTime(ts) {
  if (!ts) return '-'
  const d = new Date(ts)
  const p = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

async function loadSummary() {
  try {
    const d = await getSecuritySummary()
    summary.value = {
      score: d.score || 0,
      eventCount: d.eventCount || 0,
      riskNodes: d.riskNodes || 0,
      fimChanges: d.fimChanges || 0,
      baselineHosts: d.baselineHosts || 0,
    }
    summaryErr.value = ''
  } catch (e) {
    summaryErr.value = (e && e.message) || '安全概览加载失败'
  }
}
async function loadEvents() {
  try {
    const d = await getSecurityEvents({ limit: 2000, category: filterCategory.value, node: filterNode.value })
    events.value = d.events || []
    evCurrentPage.value = 1
    eventsErr.value = ''
  } catch (e) {
    events.value = []
    eventsErr.value = (e && e.message) || '安全事件加载失败'
  }
}
async function loadBaselines() {
  try {
    const d = await getSecurityBaselines()
    baselines.value = d.baselines || []
    baselinesErr.value = ''
  } catch (e) {
    baselines.value = []
    baselinesErr.value = (e && e.message) || '基线数据加载失败'
  }
}
async function loadGuardRisks() {
  guardLoading.value = true
  try {
    const d = await getGuardRisks()
    guardRisks.value = d.nodes || []
    guardErr.value = ''
  } catch (e) {
    guardRisks.value = []
    guardErr.value = (e && e.message) || '护栏风险加载失败'
  } finally {
    guardLoading.value = false
  }
}
async function refreshAll() {
  // 二次及以后的刷新此前**无任何加载反馈**（loading 只在 onMounted 置位）→ KPI/事件/基线/FIM 静默变数据。
  // 现在刷新也置 loading（RefreshBar 另有 refreshing 指示，两者不冲突）。
  refreshing.value = true
  loading.value = true
  await Promise.all([loadSummary(), loadEvents(), loadBaselines(), loadDefense(), loadGuardRisks()])
  loading.value = false
  refreshing.value = false
}

onMounted(refreshAll)
</script>

<style scoped>
.security-view {
  display: flex;
  flex-direction: column;
  gap: 14px;
}
/* 刷新条自带 16px 下边距，与容器的 14px 间距叠加成 30px，顶部会显得空；
   收掉它，间距统一由容器 gap 控制。 */
.security-view :deep(.refresh-bar) {
  margin-bottom: 0;
}
.sec-load-error {
  margin-bottom: 0;
}
.sec-load-error :deep(.el-button--primary) { font-size: var(--fs-sm); }
.kpi-row {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 16px;
}
.kpi-row > :deep(.kpi-card) {
  height: 100%;
  box-sizing: border-box;
}

.score-kpi {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 6px 4px;
}
.score-kpi .kpi-ring {
  width: 72px;
  height: 72px;
  border-radius: 50%;
  display: grid;
  place-items: center;
  flex-shrink: 0;
}
.score-kpi .kpi-score {
  width: 56px;
  height: 56px;
  border-radius: 50%;
  background: var(--s0);
  display: grid;
  place-items: center;
  font-size: 22px;
  font-weight: 700;
  color: var(--t1);
  line-height: 1;
}
.score-kpi .kpi-meta {
  min-width: 0;
}
.score-kpi .kpi-title {
  font-size: var(--fs-base);
  color: var(--t1);
  margin-top: 2px;
}
.score-kpi .kpi-sub {
  font-size: var(--fs-sm);
  color: var(--t3);
}
.score-kpi.good .kpi-score { color: var(--ok); }
.score-kpi.warn .kpi-score { color: var(--warn); }
.score-kpi.bad .kpi-score { color: var(--danger); }

/* 卡片标题统一走 SectionCard，不再自写 .panel-title / .panel-title-row。
   安全事件区的筛选控件直接进 SectionCard 的 #actions（.sec-actions 已提供
   flex + gap + wrap），不再自建 .filters 裸 flex 容器（§1.5 阶段 A）。 */
.grid-2 {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 16px;
}

.pager-row {
  margin-top: 12px;
  display: flex;
  justify-content: flex-end;
}

.cat-tag {
  font-size: var(--fs-sm);
  color: var(--t2);
}
.cat-tag.has-fix {
  cursor: help;
  border-bottom: 1px dashed var(--bd-strong);
}
/* 行状态色条：row-class-name 把类名加在 <tr> 自身，所以必须直接命中 tr——
   此前写成 `.row-critical :deep(.el-table__row)` 是后代选择器（要求 tr 的后代里有
   .el-table__row），永远匹配不到，紧急/警告行的色条等于没画。
   色条挂到首个单元格而不是 tr：表格是 separate 边框模型，tr 上的 box-shadow 不一定渲染。
   两档统一用 inset box-shadow，避免此前 critical/warning 两种实现（阴影 vs border-left）不一致。 */
:deep(tr.row-critical) td:first-child { box-shadow: inset 3px 0 0 var(--danger); }
:deep(tr.row-warning) td:first-child { box-shadow: inset 3px 0 0 var(--warn); }

.dim { color: var(--t2); font-size: var(--fs-sm); }

/* 来源 IP 属地标签 */
.src-block { display: flex; flex-direction: column; gap: 2px; }
.geo-tag {
  display: inline-block;
  align-self: flex-start;
  max-width: 100%;
  padding: 0 6px;
  border-radius: 4px;
  background: var(--accent-dim);
  color: var(--accent);
  font-size: var(--fs-sm);
  line-height: 18px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* 基线 */
.panel-tools {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: var(--sp-2);
}
.bl-search {
  width: 180px;
}
/* 基线明细：上百台服务器时给一个固定高度的内滚动区，
   配合「仅未通过」与节点检索把目标缩小——否则整页会被基线明细撑到几屏长。 */
.baseline-scroll {
  max-height: 560px;
  overflow-y: auto;
  padding-right: 4px;
}
.baseline-node {
  border: 1px solid var(--bd);
  border-radius: 10px;
  padding: 12px;
  margin-bottom: 12px;
  background: var(--fill-1);
}
.baseline-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 10px;
}
.node-name {
  font-size: var(--fs-sm);
  font-weight: 600;
  color: var(--t1);
}
.node-ip {
  font-size: var(--fs-sm);
  font-weight: 400;
  color: var(--t2);
}

.baseline-items {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.bl-item {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  font-size: var(--fs-sm);
  padding: 4px 6px;
  border-radius: 6px;
  transition: background 0.15s;
}
.bl-item:hover { background: var(--fill-1); }
.bl-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  flex-shrink: 0;
}
.bl-item.pass .bl-dot { background: var(--ok); box-shadow: 0 0 8px var(--ok-dim); }
.bl-item.fail .bl-dot { background: var(--danger); box-shadow: 0 0 8px var(--danger-dim); }
.bl-name { color: var(--t2); flex: 1; }
/* 同一条「未通过」此前是圆点红、名称琥珀、状态文字红三处不一致，
   扫读时「这到底算警告还是失败」没有统一答案。这里统一成 --danger。 */
.bl-item.fail .bl-name { color: var(--danger); }
.bl-state { font-size: var(--fs-sm); color: var(--t3); }
.bl-item.fail .bl-state { color: var(--danger); }
/* 未通过项的整改建议：独占一行，弱化但不隐藏 */
.bl-fix {
  flex-basis: 100%;
  font-size: var(--fs-xs);
  color: var(--t3);
  padding-left: 15px;
  line-height: 1.5;
}

/* FIM 时间线 */
.timeline {
  display: flex;
  flex-direction: column;
  gap: 0;
}
.tl-item {
  display: flex;
  gap: 12px;
  padding: 8px 0;
  border-left: 2px solid var(--bd);
  padding-left: 14px;
  position: relative;
}
.tl-dot {
  position: absolute;
  left: -7px;
  top: 12px;
  width: 12px;
  height: 12px;
  border-radius: 50%;
  border: 2px solid var(--s0);
}
.tl-dot.tl-added { background: var(--accent); box-shadow: 0 0 8px var(--accent-dim); }
.tl-dot.tl-modified { background: var(--warn); box-shadow: 0 0 8px var(--warn-dim); }
.tl-dot.tl-deleted { background: var(--danger); box-shadow: 0 0 8px var(--danger-dim); }
.tl-body { min-width: 0; }
.tl-msg {
  font-size: var(--fs-sm);
  color: var(--t2);
  line-height: 1.4;
}
.tl-time {
  font-size: var(--fs-sm);
  color: var(--t3);
  margin-top: 2px;
}

/* 网格里的卡片等高，避免左右两张卡底部参差 */
.grid-2 :deep(.sec-card) {
  height: 100%;
}
.defense-tip {
  font-size: var(--fs-sm);
  color: var(--t3);
}
/* 护栏组合风险：一条风险一行，描述用服务端给的人话（界面不自己拼措辞） */
.guard-risk-node {
  border: 1px solid var(--bd);
  border-radius: 10px;
  padding: 12px;
  margin-bottom: 10px;
}
.guard-risk-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-bottom: 8px;
}
.guard-risk-item {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 4px 0;
}
.guard-risk-desc {
  font-size: var(--fs-sm);
  line-height: 1.5;
  color: var(--t1);
}
.guard-risk-unknown {
  font-size: var(--fs-sm);
  color: var(--t3);
  padding-top: 4px;
}
.defense-kpi {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 12px;
}
.dk-card {
  background: var(--fill-1);
  border: 1px solid var(--bd);
  border-radius: 10px;
  padding: 12px;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 4px;
}
.dk-val {
  font-size: 22px;
  font-weight: 700;
  color: var(--t1);
}
.dk-val.ok { color: var(--ok); }
.dk-val.warn { color: var(--warn); }
.dk-val.bad { color: var(--danger); }
.dk-label {
  font-size: var(--fs-sm);
  color: var(--t3);
}
.node-cell { font-weight: 600; color: var(--t1); margin-right: 8px; }
.need-upgrade { font-size: var(--fs-sm); color: var(--warn); border: 1px solid var(--warn-bd); border-radius: 10px; padding: 1px 8px; }
.jail-tag {
  font-size: var(--fs-sm);
  color: var(--ok);
  background: var(--ok-dim);
  border-radius: 8px;
  padding: 2px 8px;
}
.defense-action, .ban-ips { margin-top: 4px; font-size: var(--fs-sm); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.ban-count { color: var(--warn); font-size: var(--fs-sm); font-weight: 600; }
.def-confirm {
  margin: 8px 0 0;
  padding-left: 18px;
  color: var(--t2);
  font-size: var(--fs-sm);
  line-height: 1.7;
}
.def-confirm code {
  background: var(--fill-3);
  padding: 0 4px;
  border-radius: 4px;
  color: var(--accent);
}

/* 合并原先两处独立的 1100px 断点 */
@media (max-width: 1024px) {
  .kpi-row { grid-template-columns: repeat(2, 1fr); }
  .grid-2 { grid-template-columns: 1fr; }
  .defense-kpi { grid-template-columns: repeat(2, 1fr); }
}
</style>
