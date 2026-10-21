<template>
  <section class="view">
    <PageHeader
      title="集中日志"
      desc="检索各节点上报的日志行（Agent 侧只上传 logSources.patterns 命中的行）"
    />

    <!-- 当前日志后端的能力（批次 23）。**默认一行静默小字**：大多数部署一天也撞不到边界，
         常驻大横幅只会变成噪声；真撞到边界时下面那条截断提示会带上同一句指引。 -->
    <div v-if="backendLine" class="backend-line muted">{{ backendLine }}</div>
    <el-alert
      v-if="backendProbeError"
      type="warning"
      :closable="false"
      show-icon
      :title="'日志后端探测失败：' + backendProbeError"
      description="这不代表没有日志——是平台问不到后端。先确认后端进程与配置（server.yaml 的 logBackend）。"
      class="alert-gap"
    />

    <div class="card panel">
      <div class="filter-bar">
        <el-date-picker
          v-model="range"
          type="datetimerange"
          size="small"
          range-separator="至"
          start-placeholder="开始时间"
          end-placeholder="结束时间"
          :shortcuts="shortcuts"
          class="w-xl"
          style="max-width: 100%"
        />
        <el-select v-model="mode" size="small" class="w-xs">
          <el-option label="关键词" value="kw" />
          <el-option label="正则" value="re" />
        </el-select>
        <el-input
          v-model="keyword"
          size="small"
          clearable
          class="w-l"
          :placeholder="mode === 're' ? '正则，如 disk|oom' : '子串，如 error'"
          @keyup.enter="search"
        />
        <el-select
          v-model="nodes"
          multiple
          collapse-tags
          collapse-tags-tooltip
          clearable
          placeholder="全部节点"
          size="small"
          class="w-m"
        >
          <el-option v-for="n in nodeNames" :key="n" :label="n" :value="n" />
        </el-select>
        <el-select
          v-model="sources"
          multiple
          collapse-tags
          collapse-tags-tooltip
          clearable
          placeholder="全部来源"
          size="small"
          class="w-m"
        >
          <el-option v-for="s in sourceNames" :key="s" :label="s" :value="s" />
        </el-select>
        <el-input
          v-model="fieldInput"
          size="small"
          clearable
          class="w-m"
          placeholder="字段过滤，如 status:500，回车添加"
          @keyup.enter="addFieldFilter"
        />
        <el-button type="primary" size="small" :loading="loading" @click="search">查询</el-button>
        <!-- 日志来源配在 Agent 侧，页面上看不到任何字段；"没数据"时最缺的就是可复制的模板 -->
        <el-button size="small" @click="exampleVisible = true">配置示例</el-button>
      </div>

      <!-- 生效中的筛选条件**全部**回显（时间/模式/关键词/节点/来源/字段/容器）：
           此前只有字段过滤与容器过滤可见，其余改完就"隐形"，用户无法确认条件还在不在生效，
           于是"日志怎么少了"变成下一个问题。撤销任一条会立即重查，避免 chips 与实际结果脱节。 -->
      <FilterChips
        :chips="filterChips"
        class="chips-gap"
        clear-text="清空全部"
        @remove="removeFilter"
        @clear="clearFilters"
      />

      <!-- 字段过滤候选：服务端在**解析出的键值**上做等值匹配，比关键词精确（关键词会命中别的字段的值）。
           候选只列服务端真的见过的字段名，避免用户按一个永远查不到的名字去筛。 -->
      <div v-if="fieldCandidates.length" class="field-row">
        <span class="muted field-hint">
          已知字段（点一下填入输入框，补上值后回车）：
          <el-button v-for="n in fieldCandidates" :key="n" link size="small" @click="pickField(n)">{{ n }}</el-button>
        </span>
      </div>

      <el-alert
        v-if="loadError"
        type="error"
        :closable="false"
        show-icon
        :title="loadError"
        class="alert-gap"
      />
      <!-- 截断必须显式提示：静默截断会让人以为「日志就这么多」，从而得出错误结论 -->
      <el-alert
        v-if="truncated"
        type="warning"
        :closable="false"
        show-icon
        title="结果被截断：当前条件下还有更多日志"
        :description="truncatedDesc"
        class="alert-gap"
      />
      <el-alert
        v-else-if="scannedHint"
        type="info"
        :closable="false"
        show-icon
        :title="scannedHint"
        class="alert-gap"
      />

      <el-table
        ref="logTable"
        v-loading="loading"
        :data="lines"
        :row-key="rowKey"
        style="width: 100%"
        @selection-change="onSelectionChange"
      >
        <el-table-column type="selection" width="40" />
        <!-- 空态分两种：查过之后没有命中才是「没有命中的日志」；
             一次都没查过时不能复用这句话，否则首屏就给出一个错误结论。 -->
        <template #empty>
          <EmptyState
            v-if="searched"
            title="没有命中的日志"
            :hints="[
              '确认 Agent 的 logSources.patterns 已配置并包含目标文件路径',
              '容器日志：来源要开 podLogs 且路径要能匹配到实际文件（如 /var/log/pods/*/*/*.log）',
              '扩大时间范围——日志默认只查最近一段时间',
              '关键词模式下是子串匹配，正则模式需切换到「正则」',
            ]"
          />
          <EmptyState
            v-else
            title="按条件查询后显示结果"
            :hints="['选择时间范围与关键词，然后点「查询」']"
          />
        </template>
        <el-table-column label="时间" width="180">
          <template #default="{ row }">{{ fmtTime(row.ts) }}</template>
        </el-table-column>
        <el-table-column prop="node" label="节点" width="150" />
        <el-table-column label="来源 / 资产" width="220">
          <template #default="{ row }">
            <span>{{ row.source }}</span>
            <!-- 容器日志（行上带容器身份，Pod 日志）：身份来自 Agent 从文件路径解析的结果、
                 服务端已校验，资产是**那个 Pod** —— 与"某台机器上的某类来源"是两套归属，
                 不能混用，因此这类行只展示 Pod 那一套。 -->
            <template v-if="podOrigin(row)">
              <div class="pod-line">
                {{ podOrigin(row).namespace }}/{{ podOrigin(row).pod }}
                <span v-if="podOrigin(row).container" class="muted">· {{ podOrigin(row).container }}</span>
              </div>
              <span v-for="a in podAssetsOf(row)" :key="a.id" class="asset-chip" @click="openAsset(a)">
                <el-tag size="small" effect="plain" type="warning">{{ a.name || a.naturalKey }}</el-tag>
              </span>
              <!-- 「只看该 Pod」按容器身份收窄：即使台账里没有这个 Pod 也能用（过滤用的是日志自带的身份） -->
              <el-button
                link
                size="small"
                title="把检索范围收窄到该 Pod：节点 + 来源 + 容器"
                @click="onlyPod(row)"
              >
                只看该 Pod
              </el-button>
            </template>
            <!-- 资产映射来自服务端：资产上用人工属性 logSource 声明"这条来源属于我"。
                 点标签打开台账，点「只看该资产」把节点 + 来源一起收窄到该资产。 -->
            <template v-else-if="assetsOf(row).length">
              <!-- 点击挂在原生 span 上而不是 el-tag 上：标签组件的 attrs 透传行为不由我们决定，
                   而"点了没反应"是那种不会被报错暴露的失败 -->
              <span v-for="a in assetsOf(row)" :key="a.id" class="asset-chip" @click="openAsset(a)">
                <el-tag size="small" effect="plain">{{ a.name || a.naturalKey }}</el-tag>
              </span>
              <el-button
                v-if="assetsOf(row).length === 1"
                link
                size="small"
                title="把检索范围收窄到该资产：节点 + 来源"
                @click="onlyAsset(row)"
              >
                只看该资产
              </el-button>
            </template>
          </template>
        </el-table-column>
        <el-table-column label="命中模式" width="170">
          <template #default="{ row }">
            <span v-if="row.pattern" class="tag">{{ row.pattern }}</span>
            <span v-else class="muted">—</span>
            <!-- 由这一行直接建规则：Agent 已为每个模式产出独立指标
                 （<来源>_log_<模式>_total），这里只是把模式名翻译成规则模板。 -->
            <el-button
              v-if="row.pattern && can('alerts:write')"
              link
              size="small"
              @click="createRule(row)"
            >建规则</el-button>
          </template>
        </el-table-column>
        <!-- 日志列 min-width 由 420 收到 380：本表列宽合计必须 ≤ 可用宽 1142px
             （1440 − 232 侧栏 − 40 页边距）。加多选列（40px）后原合计 1180px 会横向溢出 38px，
             日志列是唯一弹性列（单行省略 + 点击展开），收它代价最小。 -->
        <el-table-column label="日志" min-width="380">
          <template #default="{ row }">
            <!-- 结构化字段贴在原文上方：两者是同一行的两面，分开一列会让阅读来回跳 -->
            <div v-if="fieldPairs(row).length" class="field-chips">
              <span v-for="p in fieldPairs(row)" :key="p" class="tag field-chip">{{ p }}</span>
            </div>
            <!-- 默认单行省略，点击展开（多行堆栈合并后的日志需要看全）。
                 命中词就地高亮：否则关键词往往落在省略号之后，用户看到的是"一条都不像命中的结果"。
                 用 button 而不是 div：可 Tab 聚焦、带 aria-expanded，键盘用户同样能展开。 -->
            <div class="logline-row">
              <button
                type="button"
                class="logline"
                :class="{ open: isOpen(row) }"
                :aria-expanded="isOpen(row)"
                :title="isOpen(row) ? '点击收起' : '点击展开'"
                @click="toggle(row)"
                v-html="highlight(row.text)"
              ></button>
              <span
                v-if="hitCount(row.text)"
                class="hit-badge"
                :title="`本行命中 ${hitCount(row.text)} 处`"
              >{{ hitCount(row.text) }}</span>
            </div>
          </template>
        </el-table-column>
      </el-table>

      <div class="foot">
        <el-button v-if="hasMore" size="small" :loading="loadingMore" @click="loadMore">
          加载更多
        </el-button>
        <span v-else-if="lines.length" class="muted">已到末尾</span>
        <span v-if="!lines.length && !loading" class="muted">
          {{ emptyHint }}
          <el-button link type="primary" @click="exampleVisible = true">查看配置示例</el-button>
        </span>
        <span class="foot-spacer"></span>
        <!-- 导出/复制是排障留证的最后一步：此前只有本页没有，用户只能手抄 -->
        <span v-if="lines.length" class="muted">已加载 {{ lines.length }} 条</span>
        <el-button v-if="selectedRows.length" size="small" @click="copySelected">
          复制选中 {{ selectedRows.length }} 行
        </el-button>
        <el-button v-if="lines.length" size="small" @click="exportLogs">
          <el-icon><Download /></el-icon> 导出 CSV
        </el-button>
      </div>
    </div>

    <!-- 配置示例：logSources 配在 Agent 侧，界面上看不到字段，是"查不到数据"时最需要的东西。
         复制走 execCommand 兜底：离线部署多为 http 源，navigator.clipboard 在非安全上下文不可用。 -->
    <el-dialog v-model="exampleVisible" title="Agent 侧 logSources 配置示例" width="min(760px, 94vw)">
      <el-alert
        type="info"
        :closable="false"
        show-icon
        title="在「被监控节点」的 /etc/monitor-agent/agent.yaml 里添加，改完重启 Agent；不配置则一条日志都不采集。"
        class="alert-gap"
      />
      <div class="ex-row">
        <el-select v-model="exampleKey" size="small" style="width: 320px">
          <el-option v-for="ex in LOG_EXAMPLES" :key="ex.key" :label="ex.label" :value="ex.key" />
        </el-select>
        <el-button size="small" type="primary" plain @click="copyExample">复制这段配置</el-button>
        <span class="muted">{{ currentLogExample.hint }}</span>
      </div>
      <pre class="ex-box">{{ currentLogExample.yaml }}</pre>
      <ul class="ex-notes">
        <li><code>id</code>：小写字母开头，只含小写字母 / 数字 / 下划线（它同时是存储分片名与指标前缀）</li>
        <li><code>patterns[].name</code>：字母或下划线开头（会拼进指标名 <code>&lt;id&gt;_log_&lt;name&gt;_total</code>）</li>
        <li><code>paths</code>：必须是绝对路径，不能含 <code>..</code>；<b>支持通配符</b>（如 <code>/var/log/pods/*/*/*.log</code>）</li>
        <li>
          <code>podLogs: true</code>：声明这些路径是 <b>Pod 的容器日志</b>，Agent 会从路径解析出
          命名空间/Pod/容器随日志上报（<b>只上报身份，不上报路径</b>），检索页据此标到 Pod 资产；
          此时路径必须落在 <code>/var/log/pods/</code> 下
        </li>
        <li>通配符没匹配到文件<b>不算采集故障</b>（Pod 是短命的）；单轮最多采 200 个文件，超出会告警</li>
        <li>不给 <code>patterns</code> 就必须显式写 <code>all: true</code>，否则 Agent 拒绝启动（刻意的隐私默认值）</li>
        <li>首次见到文件<strong>从末尾开始读，不回溯历史</strong>；想看历史日志请到机器上看</li>
        <li>单轮超上限会「跳过该文件剩余部分并计数」（<code>&lt;id&gt;_log_dropped_total</code>），不会延后补读</li>
      </ul>
      <template #footer>
        <span class="muted">写错时 Agent 启动会直接拒绝并指出是哪一处</span>
        <el-button @click="exampleVisible = false">关闭</el-button>
      </template>
    </el-dialog>
  </section>
</template>

<script setup>
import { ref, computed, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Download } from '@element-plus/icons-vue'
import http from '../api/http'
import useAuth from '../composables/useAuth'
import PageHeader from './common/PageHeader.vue'
import EmptyState from './common/EmptyState.vue'
import FilterChips from './common/FilterChips.vue'

const route = useRoute()
const router = useRouter()
// 「建规则」是写操作（保存规则需 alerts:write），无权时按钮不出现
const { can } = useAuth()

// ---- 日志 → 资产联动（全景表 8-6 的联动侧）----
// 资产上用人工属性 logSource 声明"这条采集来源属于我"，服务端据此把本页的
// (来源, 节点) 映射成资产。前端只做展示与跳转，不猜归属。
function assetsOf(row) {
  return assetMap.value[row.source + '|' + row.node] || []
}
function openAsset(item) {
  router.push({ path: '/assets', query: { id: String(item.id) } })
}
// onlyAsset 把检索范围收窄到该资产的日志：资产已声明来源，节点就是它的归属节点，
// 因此"只看这个资产"= 节点 + 来源两个筛选一起加上（不需要额外的资产参数）。
function onlyAsset(row) {
  nodes.value = [row.node]
  sources.value = [row.source]
  search()
}

// ---- 容器日志（Pod 日志）----
// 身份来自 Agent 从 kubelet 的容器日志路径解析出的 namespace/pod/container，
// 服务端校验后才落库；前端只展示与跳转，不猜归属。
// podOrigin 返回行的容器身份（普通文件日志返回 null）。
function podOrigin(row) {
  return row && row.origin && row.origin.pod ? row.origin : null
}
function podKey(o) {
  return o.namespace + '|' + o.pod
}
function podAssetsOf(row) {
  const o = podOrigin(row)
  return o ? podAssetMap.value[podKey(o)] || [] : []
}
// onlyPod 按容器身份收窄检索：与「只看资产」同一取向，先把节点 + 来源收窄到这一行，
// 再叠上容器条件——节点参与扫描收窄（本地后端按节点分文件），容器条件才是"只看这个 Pod"。
// 它不依赖台账（过滤用的是日志自带的身份），因此台账里没有这个 Pod 时按钮依然有意义。
function onlyPod(row) {
  const o = podOrigin(row)
  if (!o) return
  nodes.value = [row.node]
  sources.value = [row.source]
  const key = podKey(o)
  if (!pods.value.includes(key)) pods.value = pods.value.concat(key)
  search()
}
function removePodFilter(key) {
  pods.value = pods.value.filter((x) => x !== key)
}
function podFilterLabel(key) {
  const [ns, pod] = key.split('|')
  return ns + '/' + pod
}

// createRule 由这一行日志建阈值规则（全景表 9-05）：跳到告警页并带上来源/模式，
// 由那边向服务端取模板（指标名必须由服务端拼，见 logs_api.go 的说明）。
function createRule(row) {
  router.push({ path: '/alerts', query: { newLogRule: row.source + '|' + row.pattern, node: row.node } })
}

// 时间范围默认最近 1 小时：日志量远大于指标，默认范围必须收窄（与服务端默认一致）。
function defaultRange() {
  const now = Date.now()
  return [new Date(now - 3600 * 1000), new Date(now)]
}

const shortcuts = [
  { text: '最近 15 分钟', value: () => [new Date(Date.now() - 900 * 1000), new Date()] },
  { text: '最近 1 小时', value: () => [new Date(Date.now() - 3600 * 1000), new Date()] },
  { text: '最近 24 小时', value: () => [new Date(Date.now() - 24 * 3600 * 1000), new Date()] },
  { text: '最近 7 天', value: () => [new Date(Date.now() - 7 * 24 * 3600 * 1000), new Date()] },
]

const range = ref(defaultRange())
const mode = ref('kw')
const keyword = ref('')
const nodes = ref([])
const sources = ref([])
const nodeNames = ref([])
const sourceNames = ref([])

// 字段过滤：`key:value` 形态（与服务端参数一致，可重复出现）。
// 同一个键不允许两个值——它永远不会同时命中，留着只会让人以为"条件生效了但没结果"。
const fieldInput = ref('')
const fieldFilters = ref([])
const fieldCandidates = ref([])

const lines = ref([])
// assetMap 的键是 `<source>|<node>`：资产上用人工属性 logSource 声明归属，
// 服务端只解析本页出现过的组合（见 logs_api.go 的 logsAssetMap）。
const assetMap = ref({})
// podAssetMap 的键是 `<namespace>|<pod>`：容器日志要标的是那个 Pod，
// 与"某台机器上的某类来源"是两套归属，服务端也分两张表给（见 logsPodAssetMap）。
const podAssetMap = ref({})
// pods 是生效中的容器过滤（值为 `<namespace>|<pod>`，与服务端参数同形）。
// 用字符串而不是对象：它要直接进 URLSearchParams，也要能原样显示与删除。
const pods = ref([])

// 容器身份各段的合法形态：与服务端 `model.LogOriginPartPattern` + `MaxLogOriginPartLen`
// 是**同一条规则**（k8s 的 DNS-1123 子域，小写字母数字与 `-`/`.`、首尾必须是字母数字）。
//
// 深链里的容器条件先在这挡一道：形态不对的项丢掉，而不是让它去撞服务端的 400——
// 那会让**整个查询**失败（用户看到一片报错），而问题只是 URL 里多了个字符。
// 与上面字段过滤的取向一致（同样在客户端先挡）。
const ORIGIN_PART_RE = /^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$/
const MAX_ORIGIN_PART_LEN = 253
// 服务端的 MaxLogPodFilters 是 8：多了会被 400 掉，这里先截断，保住其余条件照常生效。
const MAX_POD_FILTERS = 8

// podFilterValid 判断一个 `<命名空间>|<Pod>` 是否合法（与服务端 parseLogPodFilters 同规则）。
function podFilterValid(key) {
  const i = key.indexOf('|')
  if (i <= 0) return false
  return [key.slice(0, i), key.slice(i + 1)].every(
    (p) => p.length > 0 && p.length <= MAX_ORIGIN_PART_LEN && ORIGIN_PART_RE.test(p),
  )
}
const cursor = ref('')
const truncated = ref(false)
const scanned = ref({ bytes: 0, lines: 0, files: 0 })

// 当前日志后端的能力与现状（批次 23）。拿不到就当没有：它只是提示，不该让日志页报错。
const backend = ref(null)

const backendProbeError = computed(() => backend.value?.storage?.probeError || '')

// 一行说清"我处在哪一种后端下、能查多大"——这恰恰是过去看不到的那件事。
const backendLine = computed(() => {
  const cap = backend.value
  // 没有 backend 字段就不显示：宁可不说话，也不要对着一个不认识的载荷替它宣称是哪种后端
  if (!cap || !cap.backend) return ''
  const parts = [cap.backend === 'victorialogs' ? '后端 VictoriaLogs（索引检索）' : '后端 本地（有界扫描）']
  const budget = cap.capabilities?.scanBudget
  if (budget) {
    parts.push(`单次最多扫 ${Math.round(budget.bytes / 1048576)} MiB / ${budget.lines} 行`)
  }
  parts.push(cap.capabilities?.fullTextIndex
    ? '关键词与字段由后端索引检索'
    : '百万行级字段筛选需切到 VictoriaLogs')
  const st = cap.storage || {}
  if (st.sources) {
    const span = st.oldestDay && st.newestDay ? `（${st.oldestDay} ~ ${st.newestDay}）` : ''
    parts.push(`已有 ${st.sources} 个来源${span}`)
  }
  return parts.join(' · ')
})

// 截断提示按后端给指引：本地后端撞到的是"扫描预算"，答案不是"再翻几页"而是"换后端"。
const truncatedDesc = computed(() => {
  const base = '可以点下方「加载更多」继续翻，或缩小时间范围 / 指定节点或关键词，让结果更精确。'
  if (backend.value && !backend.value.capabilities?.fullTextIndex) {
    return base + '当前后端是有界扫描（每次查询有预算上限）：要在大范围里按字段筛百万行，需把日志后端切到 VictoriaLogs。'
  }
  return base
})

async function loadBackend() {
  try {
    backend.value = await http.get('/api/v1/logs/backend')
  } catch (e) {
    backend.value = null
  }
}
// 是否已经查过一次。没有它就无法区分「查过、确实没有命中」和「还没查」，
// 首屏会把后者渲染成「没有命中的日志」——那是一个会让运维得出错误结论的空态。
const searched = ref(false)
const loading = ref(false)
const loadingMore = ref(false)
const loadError = ref('')
const expanded = ref(new Set())
// 表格多选（用于「复制选中行」）。row-key 复用 rowKey()，翻页追加后选中态不会错位。
const logTable = ref(null)
const selectedRows = ref([])
function onSelectionChange(rows) {
  selectedRows.value = rows || []
}
// 高亮用的是"本次查询真正生效的关键词"，而不是输入框里的当前值：
// 用户改了输入框但没点查询时结果仍是旧的，此时按新词高亮会把位置标错。
const applied = ref({ kw: '', mode: 'kw' })

// ---------- 配置示例 ----------
// 为什么放在页面里：logSources 配在 Agent 侧，界面上一个字段都看不到，
// 而"查不到数据"的头号原因就是"没配"（默认一条都不采集）。给可复制的模板最省事。
//
// 用 String.raw：正则里的 \b \d \. 在普通模板字符串里会被 JS 当转义吃掉，
// 生成出的 YAML 会静默变味（这类错误只有到机器上才会暴露）。
const LOG_EXAMPLES = [
  {
    key: 'nginx_access',
    label: 'Nginx 访问日志（只看 4xx/5xx）',
    hint: '状态码前是「引号+空格」，据此避免匹配到 URL 里的数字。',
    yaml: String.raw`
logSources:
  - id: nginx_access
    paths: ["/var/log/nginx/access.log"]
    patterns:
      - { name: http_5xx, regex: '" 5[0-9][0-9] ' }
      - { name: http_4xx, regex: '" 4[0-9][0-9] ' }
`.trim(),
  },
  {
    key: 'podlog',
    label: 'Pod 容器日志（kubelet 的 /var/log/pods）',
    hint: '必须开 podLogs 才会解析出容器身份并关联到 Pod 资产；路径要落在 /var/log/pods/ 下。',
    yaml: String.raw`
logSources:
  - id: podlog
    podLogs: true
    paths: ["/var/log/pods/*/*/*.log"]
    patterns:
      - { name: err, regex: '(?i)\b(ERROR|Exception|Caused by)\b' }
`.trim(),
  },
  {
    key: 'nginx_error',
    label: 'Nginx 错误日志',
    hint: '只收 error 及以上级别。',
    yaml: String.raw`
logSources:
  - id: nginx_error
    paths: ["/var/log/nginx/error.log"]
    patterns:
      - { name: err, regex: '(?i)\[(error|crit|alert|emerg)\]' }
`.trim(),
  },
  {
    key: 'applog',
    label: 'Java / Spring 应用日志（堆栈跨行合并）',
    hint: '行首不匹配「日期开头」的行会并入上一条——这是让堆栈成为一条记录的关键。',
    yaml: String.raw`
logSources:
  - id: applog
    paths: ["/opt/app/logs/app.log"]
    patterns:
      - { name: err, regex: '(?i)\b(ERROR|Exception|Caused by)\b' }
      - { name: warn, regex: '(?i)\bWARN\b' }
    multiline:
      startPattern: '^\d{4}-\d{2}-\d{2}'
      maxLines: 100
`.trim(),
  },
  {
    key: 'authlog',
    label: '系统认证 / sudo 审计',
    hint: 'CentOS/RHEL 用 /var/log/secure，Debian/Ubuntu 用 /var/log/auth.log——只写实际存在的那个。',
    yaml: String.raw`
logSources:
  - id: authlog
    paths: ["/var/log/secure"]
    patterns:
      - { name: failed, regex: '(?i)(Failed password|authentication failure|Invalid user)' }
      - { name: sudo, regex: '(?i)sudo:.*COMMAND=' }
`.trim(),
  },
  {
    key: 'mysql_slow',
    label: 'MySQL 慢查询日志',
    hint: '慢查询一条记录跨多行，按 "# Time:" 行首合并。',
    yaml: String.raw`
logSources:
  - id: mysql_slow
    paths: ["/var/log/mysql/slow.log"]
    patterns:
      - { name: slow, regex: '^# Query_time' }
    multiline:
      startPattern: '^# Time:'
      maxLines: 200
`.trim(),
  },
]

const exampleVisible = ref(false)
const exampleKey = ref(LOG_EXAMPLES[0].key)
// 模板里无条件引用它，因此必须有默认值（不能是 null）
const currentLogExample = computed(() => LOG_EXAMPLES.find((e) => e.key === exampleKey.value) || LOG_EXAMPLES[0])

// 离线部署多为 http 源：非安全上下文下 navigator.clipboard 不存在，必须留 execCommand 兜底，
// 否则「复制」按钮在真实环境里点了没反应——而这类问题只在部署后才发现。
function fallbackCopy(text) {
  const ta = document.createElement('textarea')
  ta.value = text
  ta.style.position = 'fixed'
  ta.style.top = '-1000px'
  document.body.appendChild(ta)
  ta.select()
  let ok = false
  try {
    ok = document.execCommand('copy')
  } catch (e) {
    ok = false
  }
  document.body.removeChild(ta)
  if (ok) ElMessage.success('已复制到剪贴板')
  else ElMessage.warning('复制失败，请手动选中文本复制')
}

function copyExample() {
  const text = currentLogExample.value.yaml
  if (navigator.clipboard && window.isSecureContext) {
    navigator.clipboard.writeText(text)
      .then(() => ElMessage.success('已复制到剪贴板'))
      .catch(() => fallbackCopy(text))
    return
  }
  fallbackCopy(text)
}

const hasMore = computed(() => truncated.value && cursor.value !== '')

// 扫描诊断：只在「没结果但确实扫过」时提示——解释「是确实没有，还是扫描预算先用完了」。
const scannedHint = computed(() => {
  if (lines.value.length || loading.value) return ''
  const s = scanned.value
  if (!s.lines) return ''
  return `扫描了 ${s.files} 个文件 / ${s.lines} 行，没有命中。可放宽时间范围或关键词再试。`
})

const emptyHint = computed(() => {
  if (!nodeNames.value.length) return '暂无节点上报数据。'
  // 最常见的原因：Agent 没配 logSources（那就根本不会有日志上行）
  if (!sourceNames.value.length) {
    return '没有任何节点配置日志来源（logSources）。请在目标机的 agent.yaml 里添加并重启 Agent —— 未配置时不会上传任何日志。'
  }
  return '没有命中的日志。可放宽时间范围、去掉关键词，或检查 Agent 侧 patterns 是否覆盖了你关心的行。'
})

function fmtTime(ts) {
  if (!ts) return '-'
  const d = new Date(ts)
  const p = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

function fmtShort(d) {
  const p = (n) => String(n).padStart(2, '0')
  return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

// ---------- 生效条件回显（FilterChips） ----------
// 六个筛选维度全部列出：时间范围（总是存在）、关键词/正则、节点、来源、每个字段过滤、每个容器过滤。
const filterChips = computed(() => {
  const chips = []
  const r = range.value
  if (r && r[0] && r[1]) chips.push({ key: 'range', label: '时间', value: `${fmtShort(r[0])} ~ ${fmtShort(r[1])}` })
  const kw = keyword.value.trim()
  if (kw) chips.push({ key: 'keyword', label: mode.value === 're' ? '正则' : '关键词', value: kw })
  if (nodes.value.length) chips.push({ key: 'nodes', label: '节点', value: nodes.value.join('、') })
  if (sources.value.length) chips.push({ key: 'sources', label: '来源', value: sources.value.join('、') })
  for (const f of fieldFilters.value) chips.push({ key: 'field:' + f, label: '字段', value: f })
  for (const p of pods.value) chips.push({ key: 'pod:' + p, label: '容器', value: podFilterLabel(p) })
  return chips
})

// 撤销一条 chip 后立即重查：chips 与结果必须始终一致，
// 否则会出现"条件已经删了、列表还是旧结果"的错觉（这是回显条最容易踩的坑）。
function removeFilter(key) {
  if (key === 'range') range.value = defaultRange()
  else if (key === 'keyword') keyword.value = ''
  else if (key === 'nodes') nodes.value = []
  else if (key === 'sources') sources.value = []
  else if (key.startsWith('field:')) removeFieldFilter(key.slice(6))
  else if (key.startsWith('pod:')) removePodFilter(key.slice(4))
  else return
  search()
}

function clearFilters() {
  range.value = defaultRange()
  keyword.value = ''
  nodes.value = []
  sources.value = []
  fieldFilters.value = []
  pods.value = []
  search()
}

// ---------- 命中高亮 ----------
function escapeRegExp(s) {
  return String(s).replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

// 高亮用正则：关键词模式下把用户输入当字面量（先转义），正则模式原样使用。
// 用户的正则可能写错（如括号不配），编译失败时静默不高亮——不能因为高亮把整页结果弄崩。
const highlightRe = computed(() => {
  const kw = applied.value.kw
  if (!kw) return null
  try {
    return new RegExp(applied.value.mode === 're' ? kw : escapeRegExp(kw), 'gi')
  } catch (e) {
    return null
  }
})

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => (
    { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]
  ))
}

// 匹配次数上限：用户写的正则可能是灾难性回溯，日志又是长文本。
// 超过上限就停（宁可少标几处，也不能把浏览器卡住）。
const MAX_HL = 300

// highlight 返回可 v-html 的片段：原文先转义，只有命中的片段包 <mark>。
// 顺序很关键——必须在转义**之前**切分原文（转义后偏移量就变了），再对每段分别转义。
function highlight(text) {
  const t = String(text === undefined || text === null ? '' : text)
  const re = highlightRe.value
  if (!re) return escapeHtml(t)
  re.lastIndex = 0
  let out = ''
  let last = 0
  let m
  let n = 0
  while ((m = re.exec(t)) !== null) {
    if (m[0] === '') { re.lastIndex += 1; continue }
    out += escapeHtml(t.slice(last, m.index)) + '<mark>' + escapeHtml(m[0]) + '</mark>'
    last = m.index + m[0].length
    if (++n >= MAX_HL) break
  }
  return out + escapeHtml(t.slice(last))
}

function hitCount(text) {
  const re = highlightRe.value
  if (!re) return 0
  const t = String(text === undefined || text === null ? '' : text)
  re.lastIndex = 0
  let n = 0
  let m
  while ((m = re.exec(t)) !== null) {
    if (m[0] === '') { re.lastIndex += 1; continue }
    if (++n >= MAX_HL) break
  }
  return n
}

// ---------- 导出 / 复制 ----------
function toCsv(rows) {
  const head = ['时间', '节点', '来源', '容器', '命中模式', '字段', '日志']
  const esc = (v) => `"${String(v === undefined || v === null ? '' : v).replace(/"/g, '""')}"`
  const out = [head.join(',')]
  for (const r of rows) {
    const o = podOrigin(r)
    out.push([
      fmtTime(r.ts),
      r.node,
      r.source,
      o ? `${o.namespace}/${o.pod}${o.container ? ' · ' + o.container : ''}` : '',
      r.pattern || '',
      fieldPairs(r).join(' '),
      r.text || '',
    ].map(esc).join(','))
  }
  // BOM：不带它 Excel 打开中文列会乱码（导出功能最常见的"看起来坏了"）
  return '\ufeff' + out.join('\r\n')
}

function download(name, text) {
  const url = URL.createObjectURL(new Blob([text], { type: 'text/csv;charset=utf-8' }))
  const a = document.createElement('a')
  a.href = url
  a.download = name
  a.click()
  URL.revokeObjectURL(url)
}

function exportLogs() {
  if (!lines.value.length) { ElMessage.warning('没有可导出的日志'); return }
  const d = new Date()
  const p = (n) => String(n).padStart(2, '0')
  const stamp = `${d.getFullYear()}${p(d.getMonth() + 1)}${p(d.getDate())}-${p(d.getHours())}${p(d.getMinutes())}`
  download(`logs-${stamp}.csv`, toCsv(lines.value))
  ElMessage.success(`已导出 ${lines.value.length} 条`)
}

// 复制选中行：与 OpsView 同一取向——剪贴板失败要**如实报错**，
// 静默失败最气人（用户以为复制到了，粘出来却是旧内容）。
function copySelected() {
  if (!selectedRows.value.length) return
  const text = selectedRows.value
    .map((r) => `${fmtTime(r.ts)} [${r.node}] [${r.source}] ${r.text || ''}`)
    .join('\n')
  if (navigator.clipboard && window.isSecureContext) {
    navigator.clipboard.writeText(text)
      .then(() => ElMessage.success(`已复制 ${selectedRows.value.length} 行`))
      .catch(() => fallbackCopy(text))
    return
  }
  fallbackCopy(text)
}

function rowKey(row) {
  return `${row.node}|${row.source}|${row.ts}|${row.text.slice(0, 32)}`
}
function isOpen(row) {
  return expanded.value.has(rowKey(row))
}
function toggle(row) {
  const next = new Set(expanded.value)
  const k = rowKey(row)
  if (next.has(k)) next.delete(k)
  else next.add(k)
  expanded.value = next
}

// ---------- 结构化字段 ----------

const FIELD_FILTER_RE = /^[A-Za-z_][A-Za-z0-9_.-]{0,63}:.+$/

// addFieldFilter 把输入框里的 `key:value` 变成一个过滤条件（回车触发）。
// 形态在客户端先挡一道：服务端会 400，但让用户先看到"该写成什么样"比看一个报错更好。
function addFieldFilter() {
  const v = fieldInput.value.trim()
  if (!v) return
  if (!FIELD_FILTER_RE.test(v)) {
    ElMessage.warning('字段过滤需为 key:value 形态，例如 status:500')
    return
  }
  const key = v.slice(0, v.indexOf(':'))
  if (fieldFilters.value.some((f) => f.slice(0, f.indexOf(':')) === key)) {
    ElMessage.warning('同一个字段只能给一个值：' + key)
    return
  }
  fieldFilters.value.push(v)
  fieldInput.value = ''
}

function removeFieldFilter(f) {
  fieldFilters.value = fieldFilters.value.filter((x) => x !== f)
}

// pickField 点候选字段名时先把 `名字:` 填进输入框，用户只需补值。
function pickField(name) {
  fieldInput.value = name + ':'
}

// fieldPairs 把一条日志的字段摊成 `k=v`（按键排序，保证同一行每次渲染顺序一致）。
function fieldPairs(row) {
  const f = row && row.fields
  if (!f) return []
  return Object.keys(f).sort().map((k) => k + '=' + f[k])
}

// loadFieldCandidates 拉取"服务端真的见过的字段名"作为候选。
//
// 只列见过的名字：列"可能存在的字段"会让用户按一个永远查不到的名字去筛，
// 然后怀疑功能坏了。接口要 logs:read，拿不到就静默留空（不打扰查询主链路）。
async function loadFieldCandidates() {
  try {
    const params = sources.value.length ? '?sources=' + encodeURIComponent(sources.value.join(',')) : ''
    const data = await http.get('/api/v1/logs/fields' + params)
    const all = new Set()
    for (const names of Object.values((data && data.fields) || {})) {
      for (const n of names) all.add(n)
    }
    fieldCandidates.value = Array.from(all).sort().slice(0, 24)
  } catch (e) {
    fieldCandidates.value = []
  }
}

function buildQuery(withCursor) {
  const p = new URLSearchParams()
  if (range.value && range.value[0] && range.value[1]) {
    p.set('from', String(range.value[0].getTime()))
    p.set('to', String(range.value[1].getTime()))
  }
  const kw = keyword.value.trim()
  if (kw) p.set(mode.value === 're' ? 'regex' : 'q', kw)
  if (nodes.value.length) p.set('nodes', nodes.value.join(','))
  if (sources.value.length) p.set('sources', sources.value.join(','))
  for (const f of fieldFilters.value) p.append('field', f)
  // 容器过滤可重复出现（服务端最多 8 个），与 field 同一形态
  for (const pod of pods.value) p.append('pods', pod)
  p.set('limit', '200')
  if (withCursor && cursor.value) p.set('cursor', cursor.value)
  return p.toString()
}

async function search() {
  loading.value = true
  loadError.value = ''
  expanded.value = new Set()
  selectedRows.value = []
  logTable.value?.clearSelection()
  try {
    const data = await http.get('/api/v1/logs?' + buildQuery(false))
    lines.value = data.lines || []
    cursor.value = data.cursor || ''
    truncated.value = !!data.truncated
    scanned.value = { bytes: data.scannedBytes || 0, lines: data.scannedLines || 0, files: data.files || 0 }
    // 服务端给出的「来源 + 节点 → 资产」映射：本页共享，逐行查它即可。
    assetMap.value = data.assets || {}
    // 容器身份 → Pod 资产（键 `<namespace>|<pod>`）：与上面的主机映射分开两张表
    podAssetMap.value = data.podAssets || {}
    // 只有查询成功才把"当前输入"认作"本次生效的条件"（失败时结果被清空，高亮也无意义）
    applied.value = { kw: keyword.value.trim(), mode: mode.value }
  } catch (e) {
    loadError.value = e.message || '查询失败'
    lines.value = []
    cursor.value = ''
    truncated.value = false
    assetMap.value = {}
    podAssetMap.value = {}
    applied.value = { kw: '', mode: 'kw' }
  } finally {
    loading.value = false
    searched.value = true
  }
}

async function loadMore() {
  if (!hasMore.value) return
  loadingMore.value = true
  loadError.value = ''
  try {
    const data = await http.get('/api/v1/logs?' + buildQuery(true))
    lines.value = lines.value.concat(data.lines || [])
    cursor.value = data.cursor || ''
    truncated.value = !!data.truncated
    // 翻页后的映射要**合并**而不是覆盖：否则前几页的资产标签会突然全部消失。
    assetMap.value = { ...assetMap.value, ...(data.assets || {}) }
    podAssetMap.value = { ...podAssetMap.value, ...(data.podAssets || {}) }
  } catch (e) {
    loadError.value = e.message || '加载更多失败'
  } finally {
    loadingMore.value = false
  }
}

// 节点与来源下拉的数据来自节点清单：来源就是各节点声明的 logSources 并集，
// 因此列表本身就是「哪些来源真的可能上报」——也为空状态提示提供了判断依据。
async function loadMeta() {
  try {
    const data = await http.get('/api/v1/nodes')
    const list = data.nodes || []
    nodeNames.value = list.map((n) => n.hostname).filter(Boolean)
    const set = new Set()
    for (const n of list) {
      for (const s of n.logSources || []) set.add(s)
    }
    sourceNames.value = Array.from(set).sort()
  } catch (e) {
    // 元信息拿不到不影响查询（可手动不选节点/来源）
    console.error('加载节点与日志来源失败', e)
  }
}

// 深链：/logs?node=<节点>&from=<ms>&to=<ms>&q=<关键词>&field=&pods=
// 只覆盖显式传入的项，其余保持默认，避免「跳过来却看不到东西」。
//
// `pods=<命名空间>|<Pod>` 是**容器页（与 Pod 资产）跳进来的入口**：容器页只知道
// 命名空间与 Pod 名，日志的归属由 Agent 从 kubelet 路径解析、服务端校验后落库，
// 因此这里只搬条件、不猜归属。不带节点是刻意的：多节点时前端不知道这个 Pod 此刻
// 落在哪台机器，猜错就是一个空结果（条件本身已经足够精确，扫描范围由服务端按授权收窄）。
function applyDeepLink() {
  const q = route.query
  if (q.node) nodes.value = String(q.node).split(',').map((s) => s.trim()).filter(Boolean)
  const from = Number(q.from)
  const to = Number(q.to)
  if (from > 0 && to > from) range.value = [new Date(from), new Date(to)]
  if (q.regex) {
    keyword.value = String(q.regex)
    mode.value = 're'
  } else if (q.q) {
    keyword.value = String(q.q)
    mode.value = 'kw'
  }
  // 字段过滤可重复出现：单个时是字符串，多个时是数组，两种都要吃下
  const fields = q.field === undefined ? [] : Array.isArray(q.field) ? q.field : [q.field]
  fieldFilters.value = fields
    .map((f) => String(f).trim())
    .filter((f) => FIELD_FILTER_RE.test(f))
  // 容器过滤同理（与 field 同一形态）。认领后它会以可删除的 chip 出现在工具条上——
  // 生效中的条件必须看得见，否则"日志怎么少了"会变成下一个问题。
  const podList = q.pods === undefined ? [] : Array.isArray(q.pods) ? q.pods : [q.pods]
  pods.value = podList
    .map((p) => String(p).trim())
    .filter(podFilterValid)
    .slice(0, MAX_POD_FILTERS)
}

// 来源变了就重新取一次字段候选：不同来源的字段集合本来就不一样
// （applog 有 level/status，nginx_access 可能是 status/upstream）。
watch(sources, () => loadFieldCandidates())

onMounted(async () => {
  applyDeepLink()
  loadBackend() // 与检索并行：它只影响一行提示，不该拖慢首屏
  await loadMeta()
  await loadFieldCandidates()
  await search()
})
</script>

<style scoped>
/* 内边距由 MainLayout 的 .content 统一提供，页面不再自己套一层 */
.view { padding: 0; display: flex; flex-direction: column; gap: 12px; }
/* .card 只给底色与描边，内边距由此处的 .panel 提供（与元素表共用一套卡片语言） */
.panel { padding: 12px; }
.alert-gap { margin-bottom: 10px; }
.tag { display: inline-block; padding: 1px 6px; border-radius: 4px; background: var(--accent-dim); font-size: 12px; }
/* 生效条件回显条与筛选条之间留一口气 */
.chips-gap { margin-bottom: 10px; }
/* 字段候选行（点击填入输入框） */
.field-row { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; margin-bottom: 10px; }
.field-hint { display: inline-flex; align-items: center; gap: 2px; flex-wrap: wrap; }
.field-hint .el-button + .el-button { margin-left: 0; }
/* 字段贴在日志原文上方：同一行的两面，分开一列会让阅读来回跳 */
.field-chips { display: flex; gap: 4px; flex-wrap: wrap; margin-bottom: 2px; }
/* 资产标签：可点（跳台账），因此要有指针与间距 */
.asset-chip { margin-left: 4px; cursor: pointer; }
/* 容器身份：一行小字而不是标签——它是"这行日志的出处"，本身不是可点的实体
   （可点的是它下面的 Pod 资产标签） */
.pod-line { font-size: 12px; margin-top: 2px; }
.field-chip { background: var(--ok-dim); font-family: var(--mono); }
/* 日志行 = 可聚焦按钮 + 命中计数徽标。button 做视觉重置，但保留键盘可达性。 */
.logline-row { display: flex; align-items: flex-start; gap: 6px; }
.logline {
  flex: 1;
  min-width: 0;
  display: block;
  margin: 0;
  padding: 0;
  border: 0;
  background: none;
  text-align: left;
  color: inherit;
  font-family: var(--mono);
  font-size: 12px;
  line-height: inherit;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  cursor: pointer;
}
.logline.open { white-space: pre-wrap; word-break: break-all; }
.logline:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; border-radius: 2px; }
/* v-html 里的 <mark> 拿不到 scoped 的 data 属性，必须用 :deep 才能着色 */
.logline :deep(mark) {
  background: var(--warn-dim);
  color: var(--warn);
  border-radius: 2px;
  padding: 0 1px;
}
.hit-badge {
  flex: 0 0 auto;
  padding: 0 5px;
  border-radius: var(--r-pill);
  background: var(--warn-dim);
  color: var(--warn);
  font-family: var(--mono);
  font-size: 11px;
  line-height: 1.6;
}
.foot { margin-top: var(--desc-base); display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.foot-spacer { flex: 1; }
/* 配置示例弹窗 */
.ex-row { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; margin: 10px 0; }
.ex-row .muted { flex: 1; min-width: 200px; }
.ex-box {
  margin: 0;
  padding: 12px;
  max-height: 300px;
  overflow: auto;
  font-family: var(--mono);
  font-size: 13px;
  line-height: 1.6;
  white-space: pre;
  background: rgba(127, 127, 127, 0.08);
  border: 1px solid var(--bd);
  border-radius: 6px;
  color: var(--t1);
}
.ex-notes { margin: 12px 0 0; padding-left: 18px; font-size: 13px; color: var(--t2); line-height: 1.9; }
.ex-notes code { font-family: var(--mono); background: rgba(127, 127, 127, 0.12); padding: 0 4px; border-radius: 3px; }
</style>
