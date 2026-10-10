<template>
  <div class="audit-view">
    <PageHeader
      title="操作审计"
      desc="记录登录和管理操作，支持按分类、操作者、接口路径、结果与时间范围筛选"
    >
      <template #actions>
        <el-button size="small" :loading="loading" @click="loadEvents">刷新</el-button>
        <el-button size="small" :loading="verifying" @click="runVerify">完整性校验</el-button>
        <el-button size="small" type="primary" :loading="exporting" @click="exportCSV">导出 CSV</el-button>
        <el-button size="small" plain @click="printList">打印</el-button>
      </template>
    </PageHeader>

    <!-- 筛选条走全局 .filter-bar（§1.5）。结果 / method / 来源 IP 是排障三件套：
         表格里一直有这三列却都不可筛，"只看失败的操作""只看某个来源 IP"只能靠肉眼扫。 -->
    <div class="filter-bar">
      <el-select v-model="filters.category" clearable placeholder="全部分类" size="small" class="w-s" @change="applyFilters">
        <el-option value="authentication" label="认证" />
        <el-option value="management" label="管理操作" />
        <el-option value="security" label="安全中心" />
      </el-select>
      <el-input v-model="filters.user" clearable placeholder="操作者" size="small" class="w-m" @keyup.enter="applyFilters" />
      <el-input v-model="filters.path" clearable placeholder="接口路径" size="small" class="w-m" @keyup.enter="applyFilters" />
      <!-- 只看失败：审计排障最高频的一个动作，做成一次选择即可 -->
      <el-select v-model="filters.result" clearable placeholder="全部结果" size="small" class="w-s" @change="applyFilters">
        <el-option value="ok" label="只看成功" />
        <el-option value="fail" label="只看失败" />
      </el-select>
      <el-select v-model="filters.method" clearable placeholder="全部方法" size="small" class="w-s" @change="applyFilters">
        <el-option v-for="m in METHODS" :key="m" :value="m" :label="m" />
      </el-select>
      <el-input v-model="filters.remoteIP" clearable placeholder="来源 IP" size="small" class="w-s" @keyup.enter="applyFilters" />
      <!-- 时间范围：入库后审计按时间保留（默认 180 天），"能查到多久以前"要靠它才够得着。
           不选 = 不限时间，行为与改造前一致。 -->
      <el-date-picker
        v-model="filters.range"
        type="datetimerange"
        size="small"
        start-placeholder="开始时间"
        end-placeholder="结束时间"
        :shortcuts="rangeShortcuts"
        class="w-xl"
        style="max-width: 100%"
        @change="applyFilters"
      />
      <el-select v-model="filters.limit" size="small" class="w-s" @change="applyFilters">
        <el-option :value="50" label="每页 50 条" />
        <el-option :value="100" label="每页 100 条" />
        <el-option :value="200" label="每页 200 条" />
      </el-select>
      <el-button size="small" type="primary" @click="applyFilters">查询</el-button>
    </div>

    <!-- 生效中的筛选必须看得见，逐条可撤销 + 一键清空。
         尤其是从资产变更历史跳进来时带的关联 id（/audit?requestId=xxx）：
         它是隐形条件，不显式列出来，"审计怎么少了"就会变成下一个问题。 -->
    <FilterChips
      :chips="filterChips"
      clear-text="清空全部"
      @remove="removeFilter"
      @clear="clearFilters"
    />

    <!-- 审计完整性校验结果（防篡改链）。
         三档分开显示，且**都不说"安全"**：未启用/降级模式说"没有完整性保护"，
         通过时报出**覆盖区间与条数**（"已验证第 X..Y 条"），断裂时逐条列出原因。
         最下面那行常驻边界说明是刻意的：这不是"不可删除"，不能给人虚假的安全感。 -->
    <SectionCard v-if="verify || verifyError" dense class="audit-verify">
      <div class="verify-head">
        <el-tag v-if="verify" :type="verifyTone" size="small" effect="dark">{{ verifyTitle }}</el-tag>
        <el-tag v-else type="danger" size="small" effect="dark">校验失败</el-tag>
        <span v-if="verify" class="verify-summary">{{ verifySummary }}</span>
        <!-- 时间戳：凭证要能说明"这是什么时候验的"，否则无从判断结果是否已过期 -->
        <span v-if="verifyAt" class="verify-time">校验于 {{ verifyAt }}</span>
        <span class="verify-spacer"></span>
        <el-button link size="small" :loading="verifying" @click="runVerify">重新校验</el-button>
        <el-button link size="small" @click="closeVerify">收起</el-button>
      </div>
      <!-- 校验失败不再只弹 toast（一关就没了）：落进结果卡里，与通过/异常同处可见 -->
      <el-alert
        v-if="verifyError"
        type="error"
        :closable="false"
        show-icon
        :title="verifyError"
        class="verify-err"
      />
      <ul v-if="verify && verify.breaks && verify.breaks.length" class="verify-breaks">
        <li v-for="(b, i) in verify.breaks" :key="i">
          第 {{ b.seq }} 条：<strong>{{ b.reason }}</strong> —— {{ b.detail }}
        </li>
      </ul>
      <div v-if="verify && verify.prunedSegments && verify.prunedSegments.length" class="verify-extra">
        合法裁剪区间：{{ verify.prunedSegments.map((s) => `${s.from}..${s.to}`).join('、') }}
        （保留策略按时间/条数移除，不算篡改）
      </div>
      <div v-if="verify && verify.note" class="verify-note">{{ verify.note }}</div>
      <div v-if="verify" class="verify-caveat">
        这是「防篡改（**可检测**）」，不是「不可篡改」：拿到该机 root 的人仍能改库、读密钥、清日志。
        它让"改过 / 删过审计"变得**可检测**，而不是**不可为**。
      </div>
    </SectionCard>

    <SectionCard
      title="审计记录"
      :subtitle="listSubtitle"
      dense
      :loading="loading && events.length === 0"
      :error="loadError"
      error-retry
      @retry="loadEvents"
    >
      <el-table v-loading="loading" :data="events" :row-key="eventKey" style="width: 100%">
        <template #empty>
          <EmptyState
            title="暂无审计记录"
            :hints="[
              '审计仅在启用 RBAC 后记录，确认已在系统设置中开启',
              requestIdHint,
              '调整时间范围或清除筛选条件后重新查询',
            ]"
          />
        </template>
        <!-- 首列固定 + 右侧三列固定：原 9 列合计约 1400px，横向滚动后
             「状态码 / 结果 / 资产改动」这三列排障最关心的信息会滑出视野。
             收窄后合计 1130px ≤ 实测可用宽 1142px（1440 − 232 侧栏 − 40 页边距，
             卡片 dense 内边距 12px），1440 视口下不再横向滚动；窄屏时仍会滚，
             故把右侧三列也固定，滚到哪儿都看得到。 -->
        <el-table-column label="时间" width="160" fixed>
          <template #default="{ row }">
            <div class="time-cell">
              <span>{{ formatDate(row.time) }}</span>
              <span class="time-clock">{{ formatClock(row.time) }}</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="分类" width="84">
          <template #default="{ row }">
            <StatusPill tone="muted">{{ categoryLabel(row.category) }}</StatusPill>
          </template>
        </el-table-column>
        <el-table-column prop="user" label="操作者" width="112">
          <template #default="{ row }">{{ row.user || '未登录/未知' }}</template>
        </el-table-column>
        <el-table-column label="操作" min-width="180">
          <template #default="{ row }">
            <span class="method" :class="'method-' + (row.method || '').toLowerCase()">{{ row.method }}</span>
            <span class="path">{{ row.path }}</span>
            <div v-if="row.action" class="action">{{ row.action }}</div>
          </template>
        </el-table-column>
        <el-table-column label="变更摘要" min-width="180">
          <template #default="{ row }">
            <!-- 摘要按行预计算（loadEvents 时一次算完），不再在单元格里重复解析 6 次 -->
            <div v-if="row._summary.changed.length || row._summary.added.length || row._summary.removed.length" class="change-summary">
              <!-- 颜色按语义挂在各自的 class 上，不用 nth-child：
                   三段都带 v-if，缺「修改」时 nth-child 会整体前移，
                   出现「新增」显示成琥珀、「删除」显示成绿的语义反转。 -->
              <span v-if="row._summary.changed.length" class="cs-changed">修改：{{ row._summary.changed.join('、') }}</span>
              <span v-if="row._summary.added.length" class="cs-added">新增：{{ row._summary.added.join('、') }}</span>
              <span v-if="row._summary.removed.length" class="cs-removed">删除：{{ row._summary.removed.join('、') }}</span>
            </div>
            <span v-else class="muted">{{ row.detail || '-' }}</span>
          </template>
        </el-table-column>
        <el-table-column label="来源" min-width="150">
          <template #default="{ row }">
            <div class="source-cell">
              <span class="source-ip">{{ row.remoteIP }}</span>
              <span v-if="row.sourceLocation" class="source-loc">{{ row.sourceLocation }}</span>
              <span v-else class="source-loc source-loc--unknown">归属地未知</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="status" label="状态码" width="80" fixed="right" />
        <el-table-column label="结果" width="94" fixed="right">
          <template #default="{ row }">
            <!-- 颜色 + 图标双重编码：色盲用户也能分清成功/失败 -->
            <StatusPill
              :tone="row.succeeded ? 'ok' : 'danger'"
              :icon="row.succeeded ? CircleCheckFilled : CircleCloseFilled"
            >
              {{ row.succeeded ? '成功' : '失败' }}
            </StatusPill>
          </template>
        </el-table-column>
        <!-- 反向入口：审计说"调了哪个接口"，这里回答"那次调用实际动了哪些资产的哪些字段"。
             没有关联 id 的行（旧数据、采集上报）不给入口——不为一个查不到的操作造按钮。 -->
        <el-table-column label="资产改动" width="86" fixed="right">
          <template #default="{ row }">
            <el-button
              v-if="row.requestId && can('assets:read')"
              link
              type="primary"
              size="small"
              @click="openChanges(row)"
            >
              本次改动
            </el-button>
            <span v-else class="muted">—</span>
          </template>
        </el-table-column>
      </el-table>
      <!-- 分页：审计入库后不再被 2000 条截断，"往前翻"才有意义。
           总数来自服务端同条件计数（与列表共用一处 WHERE），不会出现"总数说 12、列表只有 3 条"。 -->
      <el-pagination
        v-if="total > 0"
        layout="total, prev, pager, next, jumper"
        :total="total"
        :page-size="filters.limit"
        :current-page="page"
        style="margin-top: 12px; justify-content: flex-end"
        @current-change="changePage"
      />
    </SectionCard>

    <!-- 「本次改动」抽屉：把这一次操作在资产台账里改动的字段列清楚（可跨多个资产）。
         与资产页的变更历史互为反面：那条回答"这台资产经历了什么"，这条回答"那次操作动了谁"。
         条数被服务端截断时显式说明——静默截断会让人以为"这次就动了这些"。 -->
    <el-drawer v-model="changesDrawer" :title="changesTitle" size="min(760px, 94vw)" direction="rtl">
      <div class="changes-head">
        <div class="mono">{{ currentEvent ? currentEvent.method + ' ' + currentEvent.path : '' }}</div>
        <div class="muted">
          操作者 {{ (currentEvent && currentEvent.user) || '未登录/未知' }} ·
          {{ currentEvent ? formatTime(currentEvent.time) : '' }} ·
          关联 id <span class="mono">{{ changesRequestId }}</span>
        </div>
      </div>

      <el-alert
        v-if="changesError"
        type="error"
        :closable="false"
        show-icon
        :title="changesError"
        class="audit-error"
      />
      <el-alert
        v-if="changesTruncated"
        type="warning"
        :closable="false"
        show-icon
        :title="`改动条数超过上限，这里只显示前 ${changes.length} 条`"
        description="这一次操作改动的字段比能列出的更多，不要据此判断它的全部影响"
        class="audit-error"
      />

      <el-table v-loading="changesLoading" :data="changes" style="width: 100%">
        <template #empty>
          <EmptyState
            title="这次操作没有改动资产"
            :hints="[
              '审计记录下来了，但没有任何资产字段发生变化（如只做了更新但值没变）',
              '也可能改动落在你的资源范围之外——范围外的资产不会出现在这里',
            ]"
          />
        </template>
        <el-table-column label="资产" min-width="200">
          <template #default="{ row }">
            <div class="asset-cell">
              <span>{{ row.name || row.naturalKey }}</span>
              <span class="muted">{{ row.typeKey }} · {{ row.naturalKey }}</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="字段" width="140">
          <template #default="{ row }"><span class="mono">{{ row.field }}</span></template>
        </el-table-column>
        <el-table-column label="变更" min-width="220">
          <template #default="{ row }">
            <span class="struck">{{ row.old || '（空）' }}</span> → <b>{{ row.new || '（已清除）' }}</b>
          </template>
        </el-table-column>
        <el-table-column label="来源" width="80">
          <template #default="{ row }">{{ row.source === 'manual' ? '人工' : '采集' }}</template>
        </el-table-column>
        <el-table-column label="时间" width="170">
          <template #default="{ row }">{{ formatTime(row.at) }}</template>
        </el-table-column>
      </el-table>
    </el-drawer>
  </div>
</template>

<script setup>
import { reactive, ref, computed, onMounted, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { CircleCheckFilled, CircleCloseFilled } from '@element-plus/icons-vue'
import http, { getToken, setToken } from '../api/http'
import PageHeader from './common/PageHeader.vue'
import SectionCard from './common/SectionCard.vue'
import EmptyState from './common/EmptyState.vue'
import StatusPill from './common/StatusPill.vue'
import FilterChips from './common/FilterChips.vue'
import { getAssetChangesByRequest } from '../api/asset'
import { useAuth } from '../composables/useAuth'
import { printPage } from '../utils/print'

const route = useRoute()
const router = useRouter()
// 仅用于体验级隐藏（服务端才是安全边界）：没有 assets:read 时不给「本次改动」入口。
const { can } = useAuth()

// method 是有限枚举：做成下拉比让人手敲更省事，也避免大小写/拼写带来的"筛不出来"。
const METHODS = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE']

const loading = ref(false)
const exporting = ref(false)
const loadError = ref('')
const events = ref([])
let latestRequest = 0
// total 是**同条件总数**（服务端算，与列表共用一处 WHERE）；page 从 1 开始。
const total = ref(0)
const page = ref(1)
// requestId 是"这一次操作"的关联 id：从资产变更历史跳进来时带上（见 applyDeepLink）。
const filters = reactive({
  category: '',
  user: '',
  path: '',
  result: '',
  method: '',
  remoteIP: '',
  limit: 100,
  range: [],
  requestId: '',
})

// 「本次改动」抽屉的状态。
const changesDrawer = ref(false)
const changesLoading = ref(false)
const changesError = ref('')
const changes = ref([])
const changesTruncated = ref(false)
const changesRequestId = ref('')
const currentEvent = ref(null)
let latestChanges = 0

const changesTitle = computed(() =>
  `本次操作改动的资产字段${changes.value.length ? `（${changes.value.length} 条）` : ''}`
)

// 卡片副标题：把"符合条件共 N 条"从分页器挪到卡片头，一眼就知道筛完还剩多少。
const listSubtitle = computed(() =>
  total.value ? `符合条件共 ${total.value} 条` : ''
)

// 空态里那条与关联 id 有关的提示：关联 id 是本次升级之后才写的，
// 旧记录天然为空——不说明的话，"按 id 查不到"会被当成功能坏了。
const requestIdHint = computed(() =>
  filters.requestId
    ? '关联 id 只在本次升级之后的操作上才有，更早的记录该字段为空'
    : '按「本次操作」的关联 id 直查：从资产变更历史的「查看对应审计」可跳到这里'
)

// 时间范围快捷项：审计排查最常用的就是「今天 / 近 7 天 / 近 30 天」。
const rangeShortcuts = [
  {
    text: '今天',
    value: () => {
      const start = new Date()
      start.setHours(0, 0, 0, 0)
      return [start, new Date()]
    }
  },
  {
    text: '近 7 天',
    value: () => {
      const start = new Date()
      start.setDate(start.getDate() - 7)
      return [start, new Date()]
    }
  },
  {
    text: '近 30 天',
    value: () => {
      const start = new Date()
      start.setDate(start.getDate() - 30)
      return [start, new Date()]
    }
  }
]

const categoryMap = {
  authentication: '认证',
  management: '管理操作',
  security: '安全中心'
}
function categoryLabel(value) {
  return categoryMap[value] || value || '其他'
}

const resultLabel = { ok: '只看成功', fail: '只看失败' }

// 生效中的筛选回显：维度名 + 当前值，逐条可撤销。
const filterChips = computed(() => {
  const chips = []
  if (filters.category) chips.push({ key: 'category', label: '分类', value: categoryLabel(filters.category) })
  if (filters.user) chips.push({ key: 'user', label: '操作者', value: filters.user })
  if (filters.path) chips.push({ key: 'path', label: '接口路径', value: filters.path })
  if (filters.result) chips.push({ key: 'result', label: '结果', value: resultLabel[filters.result] || filters.result })
  if (filters.method) chips.push({ key: 'method', label: '方法', value: filters.method })
  if (filters.remoteIP) chips.push({ key: 'remoteIP', label: '来源 IP', value: filters.remoteIP })
  if (filters.requestId) chips.push({ key: 'requestId', label: '本次操作', value: filters.requestId })
  const [from, to] = filters.range || []
  if (from) chips.push({ key: 'range', label: '时间范围', value: `${fmtShort(from)} ~ ${to ? fmtShort(to) : '现在'}` })
  return chips
})

// 按行预计算变更摘要：原实现在单个单元格里调 6 次 changeSummary、每次 3 个 new RegExp，
// 每行每次渲染 18 次正则匹配。改成加载时算一次，模板直接读。
function changeSummary(detail) {
  const result = { changed: [], added: [], removed: [] }
  if (!detail) return result
  for (const key of Object.keys(result)) {
    const match = detail.match(new RegExp(`${key}=([^ ]*)`))
    if (match && match[1]) result[key] = match[1].split(',').filter(Boolean)
  }
  return result
}
function decorate(rows) {
  return (rows || []).map((r) => ({ ...r, _summary: changeSummary(r.detail) }))
}

// row-key：审计事件没有稳定主键，用"时间 + 人 + 方法 + 路径 + 状态 + 来源 IP"组合。
// 目的是给 el-table 一个稳定的行标识（减少整表重绘），不是业务主键。
function eventKey(row) {
  return [row.time, row.user, row.method, row.path, row.status, row.remoteIP].join('|')
}

// 时间列拆成「日期 / 时分秒」两行：单行 toLocaleString 需要 180px，
// 两行 160px 就够，省下的宽度留给排障真正要看的几列。
function pad2(n) {
  return String(n).padStart(2, '0')
}
function toDate(v) {
  const d = new Date(v)
  return Number.isNaN(d.getTime()) ? null : d
}
function formatDate(value) {
  const d = toDate(value)
  return d ? `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())}` : value || '-'
}
function formatClock(value) {
  const d = toDate(value)
  return d ? `${pad2(d.getHours())}:${pad2(d.getMinutes())}:${pad2(d.getSeconds())}` : ''
}
function formatTime(value) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString('zh-CN', { hour12: false })
}
// 筛选条里时间范围用短格式（MM-DD HH:mm）：320px 的控件装不下完整年月日时分秒。
function fmtShort(value) {
  const d = toDate(value)
  return d ? `${pad2(d.getMonth() + 1)}-${pad2(d.getDate())} ${pad2(d.getHours())}:${pad2(d.getMinutes())}` : String(value || '')
}

// includeOffset=false 用于导出：导出的是「符合条件的前 N 条」，不该带上当前页码。
function queryString({ includeOffset = true, format = '' } = {}) {
  const params = new URLSearchParams({
    limit: String(filters.limit),
    user: filters.user,
    path: filters.path,
    category: filters.category,
  })
  if (filters.result) params.set('result', filters.result)
  if (filters.method) params.set('method', filters.method)
  const ip = (filters.remoteIP || '').trim()
  if (ip) params.set('remoteIP', ip)
  if (includeOffset && page.value > 1) params.set('offset', String((page.value - 1) * filters.limit))
  // 关联 id 是精确匹配（服务端不走子串）：带着它查就是"这一次操作"，不该被别的条件稀释
  if (filters.requestId) params.set('requestId', filters.requestId)
  const [from, to] = filters.range || []
  if (from) params.set('from', String(new Date(from).getTime()))
  if (to) params.set('to', String(new Date(to).getTime()))
  if (format) params.set('format', format)
  return params.toString()
}

// 审计打印件要能说明"按什么条件导出的"，否则作为凭证材料无效
function printList() {
  const [from, to] = filters.range || []
  const cond = [
    `分类=${categoryLabel(filters.category)}`,
    `操作者=${filters.user || '全部'}`,
    `接口路径=${filters.path || '全部'}`,
    `结果=${filters.result ? resultLabel[filters.result] : '全部'}`,
    `方法=${filters.method || '全部'}`,
    `来源 IP=${filters.remoteIP || '全部'}`,
    `关联 id=${filters.requestId || '不限'}`,
    `时间范围=${from ? `${formatTime(from)} ~ ${formatTime(to || new Date())}` : '不限'}`,
    `第 ${page.value} 页 / 每页 ${filters.limit} 条（符合条件共 ${total.value} 条）`,
  ]
  printPage({ title: '操作审计', meta: [`本页 ${events.value.length} 条`, cond.join(' · ')] })
}

async function loadEvents() {
  const request = ++latestRequest
  loading.value = true
  try {
    const data = await http.get(`/api/v1/audit/events?${queryString()}`)
    if (request !== latestRequest) return
    events.value = decorate(data.events)
    total.value = Number(data.total) || 0
    loadError.value = ''
  } catch (error) {
    if (request !== latestRequest) return
    events.value = []
    total.value = 0
    loadError.value = error.message || '网络异常，请稍后重试'
  } finally {
    if (request === latestRequest) loading.value = false
  }
}

// 改筛选条件必须回到第 1 页：否则「第 3 页 + 新条件」会查出空列表，看起来像"没有记录"。
function applyFilters() {
  page.value = 1
  loadEvents()
}

function changePage(next) {
  page.value = next
  loadEvents()
}

// 撤销单条筛选（FilterChips 的 × ）。
function removeFilter(key) {
  if (key === 'range') {
    filters.range = []
  } else if (key === 'requestId') {
    filters.requestId = ''
    stripRequestIdFromURL()
  } else if (key in filters) {
    filters[key] = ''
  }
  applyFilters()
}

// 一键清空：把筛选条上能改的都复位（limit 保持用户选择，属于"每页条数"不是"筛选条件"）。
function clearFilters() {
  filters.category = ''
  filters.user = ''
  filters.path = ''
  filters.result = ''
  filters.method = ''
  filters.remoteIP = ''
  filters.range = []
  if (filters.requestId) {
    filters.requestId = ''
    stripRequestIdFromURL()
  }
  applyFilters()
}

// ---- 完整性校验（审计防篡改链）----
//
// 三档分开显示，且**都不说"安全"**：未启用/降级模式明说"没有完整性保护"，
// 通过时报出**覆盖区间与条数**（"已验证第 X..Y 条"），断裂时逐条列出原因。
// 只显示一个绿色的"通过"是本功能最容易犯的错——历史已被保留策略裁剪过，
// "全部通过"在这套机制下永远不成立。
const verifying = ref(false)
const verify = ref(null)
const verifyError = ref('')
const verifyAt = ref('')

// 注意取值必须是 el-tag 认的那几个（success/info/warning/danger）。
// 第一版写了 'error'，Element Plus 会**静默退化成默认样式**——"发现异常"显示成普通灰标签，
// 而用例只看文案，看不出来。这类"能跑但没效果"的错最容易被漏掉。
const verifyTone = computed(() => {
  if (!verify.value) return 'info'
  if (!verify.value.enabled) return 'warning' // 没启用：既不是通过，也不是失败
  return verify.value.ok ? 'success' : 'danger'
})
const verifyTitle = computed(() => {
  const v = verify.value
  if (!v) return ''
  if (!v.enabled) return '未启用完整性保护'
  return v.ok ? '校验通过' : '发现异常'
})
const verifySummary = computed(() => {
  const v = verify.value
  if (!v) return ''
  if (!v.enabled) return v.mode === 'json' ? '当前为降级模式' : '审计链未启用'
  const parts = [`已验证第 ${v.verifiedFrom}..${v.verifiedTo} 条（共 ${v.rowsChecked} 条）`]
  if (v.unchainedRows) parts.push(`${v.unchainedRows} 条启用链之前的历史不可验`)
  if (v.keyMismatchRows) parts.push(`${v.keyMismatchRows} 条由别的密钥签、不可验`)
  if (v.linkUnverifiedRows) parts.push(`${v.linkUnverifiedRows} 条跨越裁剪区间、该处续接无法验证`)
  if (v.truncated) parts.push('只验了最新的部分')
  if (v.keyFingerprint) parts.push(`密钥指纹 ${v.keyFingerprint}`)
  return parts.join('；')
})
async function runVerify() {
  verifying.value = true
  verifyError.value = ''
  try {
    verify.value = await http.get('/api/v1/audit/verify')
    verifyAt.value = new Date().toLocaleTimeString('zh-CN', { hour12: false })
  } catch (e) {
    // 失败落进结果卡（不再只弹 toast：toast 一关，失败就无从查证）
    verifyError.value = (e && e.message) ? e.message : '完整性校验失败'
  } finally {
    verifying.value = false
  }
}
function closeVerify() {
  verify.value = null
  verifyError.value = ''
  verifyAt.value = ''
}

async function exportCSV() {
  exporting.value = true
  try {
    // 导出走独立路由（需 audit:export 权限点），查看与导出在服务端权限点不同
    const token = getToken()
    const response = await fetch(`/api/v1/audit/export?${queryString({ includeOffset: false })}`, {
      credentials: 'include',
      headers: token ? { Authorization: `Bearer ${token}` } : {},
    })
    if (response.status === 401) {
      setToken('')
      window.dispatchEvent(new CustomEvent('auth-expired'))
      throw new Error('未登录或登录已过期')
    }
    if (!response.ok) throw new Error('导出失败')
    const blob = await response.blob()
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = 'audit-events.csv'
    link.click()
    URL.revokeObjectURL(url)
    ElMessage.success('已导出审计记录 CSV')
  } catch (error) {
    ElMessage.error('导出失败：' + (error.message || '请稍后重试'))
  } finally {
    exporting.value = false
  }
}

// 深链：/audit?requestId=xxx（从资产变更历史的「查看对应审计」跳来）。
// 只认这一个参数，其余筛选保持默认——"跳过来却什么都没看到"比多点一次筛选糟得多。
function applyDeepLink() {
  const rid = route.query.requestId
  if (typeof rid === 'string' && rid.trim()) filters.requestId = rid.trim()
}

// 关掉"只看这一次操作"这个条件时，要把它从地址栏一起摘掉：
// 只清状态不清 URL 的话，刷新一次条件又回来了，用户会以为关不掉。
function stripRequestIdFromURL() {
  if (route.query.requestId) {
    const query = { ...route.query }
    delete query.requestId
    router.replace({ path: route.path, query })
  }
}

// openChanges 打开「本次改动」：一次操作 = 一次请求 = 一个关联 id，
// 服务端按它把这次操作在台账里改动的字段（可能跨多个资产）取回来。
// 取不到关联 id 就不发请求（空值在服务端是 400，且语义上是"采集侧的全部变更"）。
async function openChanges(row) {
  changesRequestId.value = row.requestId || ''
  currentEvent.value = row
  changes.value = []
  changesTruncated.value = false
  changesError.value = ''
  changesDrawer.value = true
  if (!changesRequestId.value) return
  const request = ++latestChanges
  changesLoading.value = true
  try {
    const data = await getAssetChangesByRequest(changesRequestId.value)
    if (request !== latestChanges) return
    changes.value = data.changes || []
    changesTruncated.value = !!data.truncated
  } catch (error) {
    if (request !== latestChanges) return
    changesError.value = '读取本次改动失败：' + (error.message || '请稍后重试')
  } finally {
    if (request === latestChanges) changesLoading.value = false
  }
}

onMounted(() => {
  applyDeepLink()
  loadEvents()
})
onUnmounted(() => { latestRequest++; latestChanges++ })
</script>

<style scoped>
.audit-view { display: flex; flex-direction: column; gap: 16px; }
.audit-error :deep(.el-button--primary) { font-size: 13px; }
/* 完整性校验结果卡：三档配色由 el-tag 的 type 决定（success/warning/danger） */
.audit-verify { padding: 0; }
.verify-head { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.verify-spacer { flex: 1; }
.verify-summary { font-size: 13px; color: var(--t1); }
.verify-time { font-size: 12px; color: var(--t3); }
.verify-err { margin-top: var(--desc-base); }
.verify-breaks { margin: 8px 0 0; padding-left: 18px; font-size: 13px; color: var(--danger); }
/* 描述段统一走 --desc-base（§1.6）：此前 .verify-note 6px 是全站最贴脸的距离 */
.verify-extra, .verify-note { margin-top: var(--desc-base); font-size: 13px; color: var(--t3); }
/* 常驻边界说明：这不是"不可删除"，不能给人虚假的安全感 */
.verify-caveat {
  margin-top: var(--desc-base); padding-top: 8px; border-top: 1px dashed var(--bd);
  font-size: 12px; color: var(--t3);
}
/* 页头与筛选分离：筛选条走全局 .filter-bar（§1.5），不再自建 .card 双层包裹 */
.method { display: inline-block; min-width: 48px; margin-right: 8px; color: var(--accent); font-size: 13px; font-weight: 700; }
.method-post, .method-put, .method-patch, .method-delete { color: var(--warn); }
.path { color: var(--t1); font-family: var(--mono); font-size: 13px; }
.change-summary { display: flex; flex-direction: column; gap: 3px; color: var(--t3); font-size: 13px; }
/* 按语义类名上色（不用 nth-child：三段都是条件渲染，位置会变） */
.change-summary .cs-changed { color: var(--warn); }
.change-summary .cs-added { color: var(--ok); }
.change-summary .cs-removed { color: var(--danger); }
.mono { font-family: var(--mono); }
/* 删除/旧值用删除线：与资产页的变更 diff 同一读法（那里也是"旧值划线 + 新值加粗"） */
.struck { color: var(--t3); text-decoration: line-through; }
/* 「本次改动」抽屉：头部先把"这是哪一次操作"说清楚，再列改动 */
.changes-head {
  display: flex;
  flex-direction: column;
  gap: 4px;
  margin-bottom: 12px;
  padding-bottom: 10px;
  border-bottom: 1px solid var(--bd);
  font-size: 13px;
}
.asset-cell { display: flex; flex-direction: column; gap: 2px; line-height: 1.3; font-size: 13px; }
.source-cell { display: flex; flex-direction: column; gap: 2px; line-height: 1.3; }
.source-ip { font-family: var(--mono); font-size: 13px; }
.source-loc { font-size: 13px; color: var(--t3); }
.source-loc--unknown { font-style: italic; opacity: 0.7; }
.time-cell { display: flex; flex-direction: column; line-height: 1.35; }
.time-clock { color: var(--t3); font-size: 12px; }

/* 窄屏：筛选条控件两两占行，时间选择器独占整行。
   .filter-bar 自身已 flex-wrap，这里只调整"每行几个"，避免 6 个控件挤成一列。
   用 :deep 提升特异度，压过全局 .w-* 的固定宽度。 */
@media (max-width: 1024px) {
  .filter-bar :deep(.el-date-editor.w-xl) { width: 100%; flex: 1 1 100%; }
  .filter-bar :deep(.el-input.w-m) { width: calc(50% - 5px); }
}
</style>
