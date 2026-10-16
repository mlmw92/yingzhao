<template>
  <div class="container-view">
    <PageHeader
      title="容器与工作负载"
      :desc="platformDesc"
    />

    <!-- 平台切换：K8s 与 Docker 是两条独立的数据通路——K8s 有 Server 侧只读实时代理（ADR-0004），
         Docker 走 Server 直连 daemon（ADR-0006），因此按平台分开而不是混在一张表里。
         它独占一行（内容区顶部）是为了别缩在页头右侧、和「选择集群」挤成同级；
         但**收在行尾**——它是"整页模式"，不必占着标题旁的视线焦点（2026-10-11 用户反馈）。 -->
    <div class="platform-bar">
      <template v-if="platform === 'k8s'">
        <el-select
          v-model="clusterKey"
          placeholder="选择集群"
          size="small"
          class="cluster-select"
          :loading="loading"
          @change="onClusterChange"
        >
          <el-option
            v-for="c in clusters"
            :key="keyOf(c)"
            :value="keyOf(c)"
            :label="clusterLabel(c)"
          />
        </el-select>
        <el-button size="small" :icon="Refresh" :loading="loading" @click="loadClusters">刷新集群</el-button>
      </template>
      <span class="platform-pick">
        <span class="platform-bar-label">平台</span>
        <!-- 没有 docker:read 时不显示开关：点了只会撞 403 的入口不该摆在那里。
             此时平台锁在 K8s，这里如实写出当前平台，不留一个悬空的「平台」标签。 -->
        <el-radio-group
          v-if="canDocker"
          v-model="platform"
          class="platform-switch"
          @change="onPlatformChange"
        >
          <el-radio-button value="k8s">Kubernetes</el-radio-button>
          <el-radio-button value="docker">Docker</el-radio-button>
        </el-radio-group>
        <span v-else class="platform-locked">Kubernetes</span>
      </span>
    </div>

    <!-- Docker 平台：端点 / 引擎概览 / 容器 / 镜像（Server 直连 daemon，ADR-0006） -->
    <DockerPanel v-if="platform === 'docker'" />

    <!-- 没配集群时先把"怎么配"说清楚，而不是给一个空表格 -->
    <EmptyState
      v-if="platform === 'k8s' && !loading && clusters.length === 0"
      :icon="Grid"
      title="尚未采集到 Kubernetes 集群"
      :hints="[
        '在目标机器的 agent.yaml 里配置 k8sInstances（apiserver 地址 + kubeconfig 或 token），Agent 采集（collectors.k8s: true）开启后这里才有集群可选',
        '查询走 Server 直连 apiserver，还需要在 Server 的 dataDir 下维护 k8s-live.yaml（同样的集群名 + 凭据）',
        '两处名字要对得上：选择器里的名字来自 Agent 上报，直连查询认的是 k8s-live.yaml 里的名字',
      ]"
    />

    <template v-else-if="platform === 'k8s'">
      <el-tabs v-model="activeTab" type="card" class="container-tabs">
        <el-tab-pane v-for="t in TABS" :key="t.key" :label="t.title" :name="t.key" />
      </el-tabs>

      <div v-if="!currentCluster" class="cluster-hint">请选择要查看的集群</div>

      <template v-else>
        <div v-if="!currentCluster.up" class="offline-hint">
          该集群当前不在线（上报节点 {{ currentCluster.node }} 可能已离线），查询会被服务端拒绝
        </div>
        <!-- 集群名对不上是最容易踩的一个坑：直连查询认的是 k8s-live.yaml 里的名字，
             而选择器里的名字来自 Agent 上报。先在界面上说清楚，别让人对着 502 猜。 -->
        <div v-else-if="!liveConfigured" class="offline-hint">
          该集群在 Server 侧没有直连凭据：请把名字「{{ currentCluster.name || currentCluster.instance }}」
          加进 Server 的 dataDir/k8s-live.yaml（见 docs/adr/0004-k8s-live-proxy.md）
        </div>

        <SectionCard title="查询结果" dense>
          <template #actions>
            <div class="query-toolbar">
              <el-autocomplete
                v-model="namespace"
                :fetch-suggestions="queryNs"
                trigger-on-focus
                placeholder="命名空间（留空为全部）"
                size="small"
                clearable
                class="ns-input"
                @keyup.enter="run(activeTab)"
              />
              <el-button
                size="small"
                type="primary"
                :loading="q.busy"
                :disabled="!canQuery"
                title="按当前集群与命名空间重新下发一次查询（这是这一页唯一会重新下发的地方）"
                @click="run(activeTab)"
              >
                查询
              </el-button>
            </div>
          </template>

          <el-alert
            v-if="q.error"
            type="error"
            :closable="false"
            show-icon
            class="query-error"
            :title="q.error"
          />

          <!-- 直连查询是同步的：没有"在等 Agent 上报"那一段，只需要一句"正在查询" -->
          <div v-if="q.busy && !q.error" class="query-state">
            <span class="state-dot delivering"></span>
            <span>正在查询…</span>
          </div>

          <template v-if="result">
            <div v-if="result.notice" class="result-notice">{{ result.notice }}</div>
            <el-table
              :data="resultRows"
              size="small"
              stripe
              :max-height="520"
              class="result-table"
              @row-click="onRowClick"
            >
              <el-table-column
                v-for="(col, i) in result.columns"
                :key="col + i"
                :label="col"
                :min-width="i === 0 ? 140 : 110"
                show-overflow-tooltip
              >
                <template #default="{ row }">{{ row.cells[i] }}</template>
              </el-table-column>
              <el-table-column
                v-if="canDescribe || canLogs || canLedger || canContainerWrite"
                label="操作"
                :width="actionColWidth"
                fixed="right"
              >
                <template #default="{ row }">
                  <el-button v-if="canDescribe" link size="small" @click.stop="describeRow(row)">详情</el-button>
                  <el-button v-if="canLogs" link size="small" @click.stop="openLogs(row)">日志</el-button>
                  <!-- 「日志」是按需拉取（不依赖任何采集配置，但行数/时间窗有上限）；
                       「检索日志」是集中日志（受该节点 podLogs 采集影响，可翻页、带字段与资产联动）。
                       两个入口的数据源不同，所以并排摆着而不是二选一。 -->
                  <el-button v-if="canLogs && canSearchLogs" link size="small" @click.stop="searchLogs(row)">检索日志</el-button>
                  <el-button v-if="canLedger" link size="small" :loading="ledgerBusy" @click.stop="openLedger(row)">台账</el-button>
                  <!-- 排障写动作（批次 C）：container:write 是超管专属权限点，无权时完全不出现。
                       重启只对 Deployment/StatefulSet/DaemonSet 有意义（Job 跑完就该退出）；
                       扩缩容对 DaemonSet 无意义（副本数由节点数决定）。 -->
                  <el-button v-if="canRestartWorkload(row)" link type="warning" size="small" @click.stop="restartWorkload(row)">重启</el-button>
                  <el-button v-if="canScaleWorkload(row)" link type="warning" size="small" @click.stop="scaleWorkload(row)">扩缩容</el-button>
                  <el-button v-if="canDeletePod(row)" link type="danger" size="small" @click.stop="deletePod(row)">删除</el-button>
                </template>
              </el-table-column>
            </el-table>

            <div class="result-foot">
              <span>
                共 {{ result.total }} 个对象{{ result.truncated ? '，仅显示前 ' + result.rows.length + ' 条' : '' }}
              </span>
              <span v-if="q.busy" class="foot-time">正在刷新…</span>
              <span v-if="result.truncated" class="foot-warn">结果已截断，请缩小命名空间范围</span>
              <!-- 结果会被保留、命名空间却随时可改：条件变了必须说出来，
                   否则就是"输入框写着 default、表里是全部命名空间的对象"而界面一言不发。 -->
              <span v-if="q.result && q.ns !== namespace.trim()" class="foot-warn">
                这份结果是「{{ q.ns || '全部命名空间' }}」的，改条件后请点「查询」
              </span>
            </div>
          </template>

          <EmptyState
            v-else-if="!q.busy && !q.error"
            :icon="activeIcon"
            title="还没有查询结果"
            :hints="[
              '点「查询」按当前集群与命名空间直连 apiserver 取一次当前态（同步返回，不需要等上报）',
              '需要先在 Server 的 dataDir 下维护 k8s-live.yaml（集群地址与凭据），否则这一页查不到东西',
            ]"
          />
        </SectionCard>

      </template>
    </template>

    <!-- 对象详情：只读投影，敏感字段不下发 -->
    <el-drawer v-model="detailVisible" :title="detailTitle" size="min(620px, 94vw)">
      <!-- 与主表同一套规矩：点开行不重新下发，要最新的自己点「刷新」 -->
      <div class="detail-bar">
        <span class="muted">
          <template v-if="detailBusy">正在刷新…</template>
          <template v-else-if="!detail">尚未查询</template>
        </span>
        <el-button size="small" :loading="detailBusy" @click="refreshDetail">刷新</el-button>
      </div>
      <div v-if="detailBusy" class="drawer-loading">正在查询…</div>
      <el-alert v-else-if="detailError" type="error" :closable="false" show-icon :title="detailError" />
      <template v-else-if="detail">
        <div v-if="detail.notice" class="result-notice">{{ detail.notice }}</div>
        <el-table :data="detailRows" size="small" stripe style="width: 100%">
          <el-table-column prop="field" label="字段" width="150" />
          <el-table-column prop="value" label="值" min-width="300" show-overflow-tooltip />
        </el-table>
      </template>
    </el-drawer>
    <!-- Pod 日志：与详情同一套直连规矩——每次打开现拉一次，要最新的点「重新拉取」 -->
    <el-drawer v-model="logsVisible" :title="logsTitle" size="min(720px, 94vw)">
      <div class="detail-bar">
        <span class="muted">
          <template v-if="logsBusy">正在拉取…</template>
          <template v-else-if="!logs">尚未拉取</template>
        </span>
        <div class="logs-toolbar">
          <el-input
            v-model="logsContainer"
            placeholder="容器（留空为第一个）"
            size="small"
            clearable
            class="logs-container"
          />
          <el-button size="small" :loading="logsBusy" @click="fetchLogs(true)">重新拉取</el-button>
          <!-- 这里是"想看得更多"的自然落点：按需拉取封顶 200 行 / 24 小时，
               超出就得走集中日志（另一次查询，不是同一次拉取的加长版）。 -->
          <el-button
            v-if="canSearchLogs && logsTarget"
            size="small"
            @click="searchLogsFor(logsTarget.namespace, logsTarget.pod)"
          >在集中日志中检索</el-button>
        </div>
      </div>
      <div v-if="logsBusy && !logs" class="drawer-loading">正在拉取…</div>
      <el-alert v-else-if="logsError" type="error" :closable="false" show-icon :title="logsError" />
      <template v-else-if="logs">
        <div v-if="logs.notice" class="result-notice">{{ logs.notice }}</div>
        <!-- 日志必须按原样呈现（保留空白与换行）：重排过的堆栈/表格等于换了一份内容 -->
        <pre v-if="logText" class="log-pre">{{ logText }}</pre>
        <EmptyState
          v-else
          title="这段时间窗内没有日志"
          :hints="logsEmptyHints"
        />
      </template>
    </el-drawer>
  </div>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Refresh, Grid, Monitor, Bell } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import http from '../../api/http'
import { createOpsTask, getOpsTask } from '../../api/ops'
import { lookupAsset } from '../../api/asset'
import useAuth from '../../composables/useAuth'
import PageHeader from '../common/PageHeader.vue'
import SectionCard from '../common/SectionCard.vue'
import EmptyState from '../common/EmptyState.vue'
import DockerPanel from './DockerPanel.vue'
import { containerDetailState, containerQueryState } from './queryCache'

// 容器管理面（Kubernetes 侧）。
//
// 数据通路：**Server 直连 apiserver**（ADR-0004 + ADR-0006）。原先这 5 个只读查询走
// Agent 异步下行（提交 → 轮询，通常 15 秒以上），已整体删除——ADR-0004 写过的理由：
// 「异步下发的延迟等于一个上报周期，不满足管理面的要求」。
// 现在切集群 / 切 Tab / 点刷新都是**一次同步查询**，点开即得；界面不再需要"等待上报"那套状态。
//
// 写操作（重启工作负载 / 扩缩容 / 删除 Pod）**仍是异步下行**：它们要的是逐机护栏与
// Agent 本地的集群凭据，这两样都搬不到 Server 上。
const TABS = [
  { key: 'workloads', cap: 'workloads', title: '工作负载', icon: Grid },
  { key: 'pods', cap: 'pods', title: 'Pod', icon: Monitor },
  { key: 'events', cap: 'events', title: '事件', icon: Bell },
]

// 详情要用到的定位信息。刻意在这里显式写出列下标，而不是让后端在载荷里塞一个"魔法列"：
// 载荷是通用表格，藏字段会让它变得不可预期。
const DETAIL_SPEC = {
  workloads: {
    nsCol: 1,
    nameCol: 2,
    resourceByKind: {
      Deployment: 'deployments',
      StatefulSet: 'statefulsets',
      DaemonSet: 'daemonsets',
      Job: 'jobs',
    },
  },
  pods: { nsCol: 0, nameCol: 1, resource: 'pods' },
}

// 后端没有命名空间列表接口（只有 /api/v1/container/k8s/clusters），所以不做下拉：
// 建议项来自「当前结果里出现过的命名空间」+「本机历史输入」，零后端改动。
const NS_HISTORY_KEY = 'nebula-container-ns-history'
const NS_HISTORY_MAX = 20

const route = useRoute()
const router = useRouter()
const { can } = useAuth()
const clusters = ref([])
const loading = ref(false)
const clusterKey = ref('')
const namespace = ref('')
const activeTab = ref('workloads')

// 平台切换：K8s 与 Docker 的授权面不同（集群内部对象 vs 本机容器运行时），因此分开授权、
// 也分开成两个面板。两者的读通路现在都是 Server 直连（ADR-0006）。没有 docker:read 时开关不渲染，
// 平台锁在 K8s（点了只会撞 403 的入口不该摆在那里）。
const canDocker = computed(() => can('docker:read'))
const platform = ref('k8s')
const platformDesc = computed(() =>
  platform.value === 'docker'
    ? '查看 Docker 端点的引擎概览、容器与镜像，并可启停 / 重启 / 删除容器（写操作仅超管）。由 Server 直连 daemon，点开即得'
    : '查看 K8s 集群的工作负载、Pod 与事件，并可按权限重启 / 扩缩容 / 删除 Pod。由 Server 直连 apiserver，点开即得'
)

// 切平台不重新下发：Docker 面板自己负责首次加载。
// 这里只负责"切回 K8s 时补一次查询"——否则从 Docker 切回来会停在空白面板上。
function onPlatformChange(val) {
  const p = String(val || platform.value)
  if (p === 'k8s') nextTick(() => run(activeTab.value))
}

const detailVisible = ref(false)
const detailTitle = ref('对象详情')

// 查询状态（含结果与**结果落地时间**）存在组件之外，理由见 queryCache.js：
// 直连之后"进页面就查"是默认行为，但状态仍在组件之外——换集群 / 换 Tab 时
// 旧结果继续显示（刷新期间不白屏），路由切回来也还能看到上次那份与它的时间。
const q = computed(() => containerQueryState(clusterKey.value, activeTab.value))
const currentCluster = computed(() => clusters.value.find((c) => keyOf(c) === clusterKey.value) || null)
const result = computed(() => q.value.result)

// ---- 命名空间建议 ----
const nsHistory = ref(readNsHistory())

function readNsHistory() {
  try {
    const raw = JSON.parse(localStorage.getItem(NS_HISTORY_KEY) || '[]')
    return Array.isArray(raw) ? raw.filter((x) => typeof x === 'string').slice(0, NS_HISTORY_MAX) : []
  } catch {
    return []
  }
}

function pushNsHistory(v) {
  const s = String(v || '').trim()
  if (!s) return
  const next = [s, ...nsHistory.value.filter((x) => x !== s)].slice(0, NS_HISTORY_MAX)
  nsHistory.value = next
  try {
    localStorage.setItem(NS_HISTORY_KEY, JSON.stringify(next))
  } catch {
    // 隐私模式下写不了 localStorage，建议项退化成"仅当前结果里的命名空间"，不影响使用
  }
}

// 结果里的命名空间列按**列名**找，不猜下标：载荷是通用表格，
// 不同 Tab（workloads/pods/events）的列顺序由后端决定（见 DETAIL_SPEC 的注释）。
// ⚠️ result.rows 是后端给的 [][]string（见下面的 resultRows：它才把数组摊成 { cells }）。
// 这里读的是原始结果，所以必须按数组下标取，写成 row.cells[idx] 恒为 undefined。
const nsFromResult = computed(() => {
  const r = result.value
  if (!r) return []
  const idx = (r.columns || []).findIndex((c) => String(c).toLowerCase().includes('namespace'))
  if (idx < 0) return []
  const out = new Set()
  for (const row of r.rows || []) {
    const v = String((Array.isArray(row) ? row[idx] : row?.cells?.[idx]) ?? '').trim()
    // 通配/占位值不作为建议
    if (v && v !== '-' && v !== '*') out.add(v)
  }
  return [...out]
})

const nsSuggestions = computed(() => {
  const out = []
  const seen = new Set()
  for (const v of [...nsFromResult.value, ...nsHistory.value]) {
    if (v && !seen.has(v)) {
      seen.add(v)
      out.push(v)
    }
  }
  return out.slice(0, 30)
})

function queryNs(qs, cb) {
  const s = String(qs || '').trim().toLowerCase()
  const list = nsSuggestions.value.filter((v) => !s || v.toLowerCase().includes(s))
  cb(list.map((v) => ({ value: v })))
}
const canQuery = computed(() => !!currentCluster.value && currentCluster.value.up && !q.value.busy)
const canDescribe = computed(() => !!DETAIL_SPEC[activeTab.value])
// 日志只对 Pod 有意义：工作负载/事件列表里的对象不是容器实例。
const canLogs = computed(() => activeTab.value === 'pods')
// 台账入口：只有 Pod 与工作负载会落台账资产（事件不是对象；Job 刻意不建资产）。
const canLedger = computed(() => activeTab.value === 'pods' || activeTab.value === 'workloads')
// 「检索日志」跳的是集中日志页，路由权限 `logs:read`——与 `container:read` 是两个权限点
// （日志内容可能含敏感数据，见 router/index.js）。无权时按钮不出现：点了只会撞 403 的入口
// 不该摆在那里。这与台账入口同一取向（能否跳过去由目标页的权限决定）。
const canSearchLogs = computed(() => can('logs:read'))
// K8s 排障写动作（批次 C）：container:write 是**超管专属**权限点（用户 2026-10-10 拍板），
// 无权时按钮完全不出现——摆一个点了必 403 的按钮只会让人以为平台坏了。
const canContainerWrite = computed(() => can('container:write'))
// 与服务端动作目录的规格一致（正则卡 3 位、Agent 侧再校 0-500）。
const MAX_REPLICAS = 500
// writeBusy 是"正在下发一次写操作"的闸门：写操作不能靠连点两次来加速。
const writeBusy = ref(false)
// 操作列宽度按实际按钮数给：写死宽度会把「检索日志」挤成省略号。
// 写动作按 Tab 给（工作负载最多两个：重启 / 扩缩容；Pod 一个：删除）。
const actionColWidth = computed(() => {
  let w = canLogs.value ? (canSearchLogs.value ? 240 : 160) : 120
  if (canContainerWrite.value) w += activeTab.value === 'workloads' ? 80 : 40
  return w
})
// Pod 日志抽屉的空态提示按权限给：没有 logs:read 的用户不该被指去一个他进不去的页面。
const logsEmptyHints = computed(() => {
  const hints = ['容器可能还没启动，或该时间段确实没有输出', '可换一个容器名，或直接上机器查看完整日志']
  if (canSearchLogs.value) {
    hints.push('要看更早/更多的行：用「在集中日志中检索」（需该节点已开 podLogs 采集）')
  }
  return hints
})

// 工作负载列表的「类型」列 → 台账资产的自然键片段。
// Job 不在其中：台账不给 Job 建资产（它的 Pod 的 member_of 因此为空，那是如实的"没有归属"）。
const WORKLOAD_LEDGER_KINDS = { Deployment: 'deployment', StatefulSet: 'statefulset', DaemonSet: 'daemonset' }
const ledgerBusy = ref(false)

// ---- 排障写操作（批次 C）----
//
// 三项都只给超管（container:write），且都**仍走异步下发**：写操作需要逐机护栏与凭据边界，
// 而 ADR-0006 的 Server 侧直连刻意保持只读（写要过 Agent 那台机器自己的同意）。
// 因此这一页的"读"是点开即得，"写"仍是提交后等一个上报周期——差别是刻意的，不是没改完。

// 重启只对这三种有意义：Job 跑完就该退出，"重启 Job"没有语义。
const RESTARTABLE_WORKLOAD_KINDS = ['Deployment', 'StatefulSet', 'DaemonSet']
// 扩缩容不含 DaemonSet：它的副本数由"匹配到几个节点"决定，没有 replicas 可调。
const SCALABLE_WORKLOAD_KINDS = ['Deployment', 'StatefulSet']

function rowCell(row, idx) {
  return String((row && row.cells && row.cells[idx]) || '').trim()
}

function canRestartWorkload(row) {
  return canContainerWrite.value && activeTab.value === 'workloads' &&
    RESTARTABLE_WORKLOAD_KINDS.includes(rowCell(row, 0))
}

function canScaleWorkload(row) {
  return canContainerWrite.value && activeTab.value === 'workloads' &&
    SCALABLE_WORKLOAD_KINDS.includes(rowCell(row, 0))
}

function canDeletePod(row) {
  return canContainerWrite.value && activeTab.value === 'pods' && !!rowCell(row, DETAIL_SPEC.pods.nameCol)
}

function clusterParam() {
  return (currentCluster.value && (currentCluster.value.name || currentCluster.value.instance)) || ''
}

async function confirmWrite(text, title) {
  try {
    await ElMessageBox.confirm(text, title, {
      type: 'warning', confirmButtonText: '执行', cancelButtonText: '取消',
    })
    return true
  } catch (e) {
    return false
  }
}

// ---- 写操作的轮询（**只有写操作还走异步下行**）----
//
// 读已经全部改成 Server 直连（见文件头的说明），但写操作仍走统一下行通道：
// 它们要的是逐机护栏（这台机器同不同意被改）与 Agent 本地的集群凭据，两者都搬不到 Server 上。
// 因此"提交 → 轮询 → 回执"这套机制保留下来，但只为写操作服务——
// 别把它当成"这一页还是异步的"的证据，读那几条路径已经不经过它了。

const WRITE_POLL_INTERVAL = 3000
const WRITE_POLL_TIMEOUT = 60000

// writeTimers 只登记写操作的轮询定时器：组件卸载时要清掉，
// 否则一次未完成的写操作会在页面已经离开后继续打接口。
const writeTimers = new Set()

function waitTask(id, onTick, done) {
  return new Promise((resolve) => {
    let waited = 0
    const tick = async () => {
      let task = null
      try {
        const res = await getOpsTask(id)
        task = res && res.task
      } catch (e) {
        done(null, e.message || '查询任务失败')
        resolve()
        return
      }
      if (task) {
        onTick(task)
        if (['succeeded', 'failed', 'expired', 'cancelled'].includes(task.state)) {
          done(task)
          resolve()
          return
        }
      }
      waited += WRITE_POLL_INTERVAL
      if (waited >= WRITE_POLL_TIMEOUT) {
        done(null, '等待超过 60 秒仍未返回。确认目标节点在线，且已升级到支持容器写操作的 Agent 版本')
        resolve()
        return
      }
      const h = setTimeout(() => {
        writeTimers.delete(h)
        tick()
      }, WRITE_POLL_INTERVAL)
      writeTimers.add(h)
    }
    tick()
  })
}

function taskFailureText(t) {
  if (t.state === 'expired') return '任务已过期：Agent 未在有效期内领取，通常意味着目标节点离线或未声明支持该动作'
  if (t.state === 'cancelled') return '任务已被撤回'
  return t.message || '任务未成功'
}

// submitWrite 下发一次写操作：提交 → 轮询 → 提示 → 重新拉列表。
// 写操作是异步的，因此成功后必须重拉列表，否则界面还显示着旧的对象状态。
async function submitWrite(kind, params, label) {
  if (writeBusy.value) return
  const cluster = currentCluster.value
  if (!cluster) return
  writeBusy.value = true
  try {
    const res = await createOpsTask({ node: cluster.node, kind, params, reason: `容器排障：${label}` })
    // 平台若开启了高风险操作审批，写动作会返回 202 + pending：**尚未下发**。
    // 把它说成"已下发"会让操作者以为改动已经生效。
    if (res && res.pending) {
      ElMessage.warning(res.message || '已提交审批：本动作尚未下发，需另一位同事批准后才会执行')
      return
    }
    const task = res && res.task
    if (!task || !task.id) throw new Error('服务端未返回任务 ID')
    await waitTask(
      task.id,
      () => {},
      (t, err) => {
        if (err) ElMessage.error(err)
        else if (t.state !== 'succeeded') ElMessage.error(taskFailureText(t))
        else ElMessage.success(`${label}：已下发`)
      },
    )
    await run(activeTab.value)
  } catch (e) {
    ElMessage.error(e.message || `${label}失败`)
  } finally {
    writeBusy.value = false
  }
}

async function restartWorkload(row) {
  const kind = rowCell(row, 0)
  const ns = rowCell(row, DETAIL_SPEC.workloads.nsCol)
  const name = rowCell(row, DETAIL_SPEC.workloads.nameCol)
  if (!ns || !name) return
  const ok = await confirmWrite(
    `将对 ${kind}/${name} 执行滚动重启：控制器会逐个替换 Pod，滚动期间服务不中断。`,
    '确认重启工作负载',
  )
  if (!ok) return
  await submitWrite('container.restart', { cluster: clusterParam(), namespace: ns, kind, name },
    `重启 ${kind}/${name}`)
}

async function scaleWorkload(row) {
  const kind = rowCell(row, 0)
  const ns = rowCell(row, DETAIL_SPEC.workloads.nsCol)
  const name = rowCell(row, DETAIL_SPEC.workloads.nameCol)
  if (!ns || !name) return
  let input = ''
  try {
    const res = await ElMessageBox.prompt(
      `把 ${kind}/${name} 的副本数改为（0 到 ${MAX_REPLICAS}；填 0 等于让这项服务下线）：`,
      '调整副本数',
      {
        confirmButtonText: '调整',
        cancelButtonText: '取消',
        inputValue: rowCell(row, 3) || '1',
        inputPattern: /^[0-9]{1,3}$/,
        inputErrorMessage: '请填 0-999 的整数',
      },
    )
    input = String(res.value || '').trim()
  } catch (e) {
    return
  }
  const replicas = Number(input)
  if (!Number.isInteger(replicas) || replicas < 0 || replicas > MAX_REPLICAS) {
    ElMessage.error(`副本数应为 0-${MAX_REPLICAS} 的整数`)
    return
  }
  // 0 副本等于下线：单独再确认一次。这是"用户真知道自己在做什么"的唯一保障——
  // 服务端只校验取值范围，它无从判断填 0 是不是手滑。
  if (replicas === 0) {
    const ok = await confirmWrite(
      `副本数填 0 会让 ${kind}/${name} 的所有 Pod 被删除，这项服务会下线。确认继续？`,
      '确认把副本数改为 0',
    )
    if (!ok) return
  }
  await submitWrite('container.scale',
    { cluster: clusterParam(), namespace: ns, kind, name, replicas: String(replicas) },
    `把 ${kind}/${name} 的副本数改为 ${replicas}`)
}

async function deletePod(row) {
  const ns = rowCell(row, DETAIL_SPEC.pods.nsCol)
  const name = rowCell(row, DETAIL_SPEC.pods.nameCol)
  if (!ns || !name) return
  const ok = await confirmWrite(
    `将删除 Pod ${ns}/${name}。若它由控制器管理会被重建（对无状态服务通常无感）；` +
    '裸 Pod 与单副本有状态负载上就是一次真实中断。',
    '确认删除 Pod',
  )
  if (!ok) return
  await submitWrite('container.delete', { cluster: clusterParam(), namespace: ns, name },
    `删除 Pod ${ns}/${name}`)
}

// 详情抽屉的状态按对象身份分键：换对象是换内容，不能把上一个对象的详情显示在新标题下。
const detailKey = ref({ cluster: '', kind: '', namespace: '', name: '' })
// 记住打开的是哪一行：抽屉里的「刷新」要能重查同一个对象。
const detailRow = ref(null)
const detailState = computed(() => containerDetailState(
  detailKey.value.cluster, detailKey.value.kind, detailKey.value.namespace, detailKey.value.name
))
const detailBusy = computed(() => detailState.value.busy)
const detailError = computed(() => detailState.value.error)
const detail = computed(() => detailState.value.result)

// Pod 日志抽屉：与详情共用同一套「直连查询」状态机。
// 缓存键里带上容器名：换容器是换内容，不能把上一个容器的日志显示在新容器名下。
const logsVisible = ref(false)
const logsTitle = ref('Pod 日志')
const logsKey = ref({ cluster: '', kind: '', namespace: '', name: '' })
const logsTarget = ref(null)
const logsContainer = ref('')
const logsState = computed(() => containerDetailState(
  logsKey.value.cluster, logsKey.value.kind, logsKey.value.namespace, logsKey.value.name
))
const logsBusy = computed(() => logsState.value.busy)
const logsError = computed(() => logsState.value.error)
const logs = computed(() => logsState.value.result)
const logText = computed(() => (logs.value?.rows || []).map((cells) => cells[0]).join('\n'))

const activeMeta = computed(() => TABS.find((t) => t.key === activeTab.value) || TABS[0])
const activeIcon = computed(() => activeMeta.value.icon)

// 表格行：把 [][]string 摊成对象，模板里好取值（保留原始 cells，详情要用）。
const resultRows = computed(() => (result.value?.rows || []).map((cells) => ({ cells })))
const detailRows = computed(() =>
  (detail.value?.rows || []).map((cells) => ({ field: cells[0], value: cells[1] }))
)

// liveClusters 是 Server 侧 k8s-live.yaml 里配好的集群名。
//
// 它只用来提示"当前选中的集群在 Server 侧没配"：直连查询认的就是那份清单里的名字，
// 两处名字对不上时查询会以"集群未配置"失败，而那句话不会告诉你"名字该怎么对齐"。
const liveClusters = ref([])

async function loadLiveClusters() {
  try {
    const res = await http.get('/api/v1/container/k8s/live-configs')
    liveClusters.value = res.clusters || []
  } catch {
    // 拿不到清单就不做任何断言：这条提示是帮忙的，不该自己变成一个错误来源。
    liveClusters.value = []
  }
}

// liveConfigured：当前集群是否在 Server 侧配了直连凭据。
// 清单为空（没配 / 没权限）时一律返回 true——宁可不提示，也不误报"没配"。
const liveConfigured = computed(() => {
  const c = currentCluster.value
  if (!c || !liveClusters.value.length) return true
  return liveClusters.value.includes(c.name || c.instance)
})

const keyOf = (c) => c.node + '|' + c.instance
const clusterLabel = (c) => `${c.name || c.instance}（${c.node}${c.up ? '' : ' · 离线'}）`
/* ===== 集群清单 ===== */
async function loadClusters() {
  loading.value = true
  try {
    const res = await http.get('/api/v1/container/k8s/clusters')
    clusters.value = res.clusters || []
    // 默认选第一个在线集群：直接给一个可用的起点，省掉一次"再选一次"。
    if (!clusters.value.some((c) => keyOf(c) === clusterKey.value)) {
      const first = clusters.value.find((c) => c.up) || clusters.value[0]
      clusterKey.value = first ? keyOf(first) : ''
    }
  } catch (e) {
    clusters.value = []
    ElMessage.error(e.message || '加载集群清单失败')
  } finally {
    loading.value = false
  }
}

function onClusterChange() {
  // 换集群直接重查：状态按「集群 + 动作」分键，切回来会先显示那个集群上次的结果
  // （刷新期间不白屏），随即被新结果替换——结构上不可能把 A 的表显示在 B 下面。
  run(activeTab.value)
}

/* ===== 查询（Server 直连） ===== */

// liveURL 组出直连查询的地址。
//
// 集群用**上报里的集群名**（k8s-live.yaml 里配的必须是同一个名字）：
// 两处名字不一致时服务端会明确回"集群 X 未配置"，而不是给一份空数据。
function liveURL(cap, extra = {}) {
  const params = new URLSearchParams()
  for (const [k, v] of Object.entries(extra)) {
    if (v !== '' && v !== undefined && v !== null) params.set(k, String(v))
  }
  const qs = params.toString()
  return '/api/v1/container/k8s/live/' + encodeURIComponent(clusterParam()) + '/' + cap + (qs ? '?' + qs : '')
}

// run 按当前（集群 + Tab + 命名空间）发起一次同步查询。
//
// 直连之后没有"下发 → 等上报"这段路：切集群、切 Tab、点「查询」都是同一次调用，
// 因此每次换视图都直接查——"只在没有缓存时查一次"那条规矩的理由（避免让人白等一个上报周期）
// 已经不存在了。
//
// **不清 result**：刷新期间旧结果继续留在表里，否则点一次刷新就是白屏。
async function run(tab) {
  const target = TABS.find((t) => t.key === tab)
  const item = containerQueryState(clusterKey.value, tab)
  const cluster = currentCluster.value
  if (!target || !item || !cluster) return

  const ns = namespace.value.trim()
  item.error = ''
  item.busy = true
  try {
    const res = await http.get(liveURL(target.cap, ns ? { namespace: ns } : {}))
    item.result = res
    item.ns = ns
    // 只有真的查到了结果才把它记进历史，避免把拼错的命名空间当建议推给用户
    pushNsHistory(ns)
  } catch (e) {
    // 服务端的说明就是给用户的下一步（凭据没配 / 名字对不上 / apiserver 拒绝），原样展示。
    item.error = (e && e.message) || '查询失败'
  } finally {
    item.busy = false
  }
}

/* ===== 对象详情 ===== */
async function onRowClick(row) {
  if (canDescribe.value) await describeRow(row)
}

// describeRow 打开某个对象的详情。force=true 表示用户明确要求刷新（抽屉里的「刷新」）。
//
// 直连之后每次打开都是一次即时查询，因此**不再"命中缓存就不查"**：
// 那条规矩是为了避开一次 15 秒的下发，代价是看到的可能是一份很久以前的快照。
// 现在只跳过"同一个对象正在查"的重复请求。
async function describeRow(row, force = false) {
  const spec = DETAIL_SPEC[activeTab.value]
  if (!spec || !currentCluster.value) return
  const cells = row.cells || []
  const name = cells[spec.nameCol]
  const ns = cells[spec.nsCol]
  const resource = spec.resource || spec.resourceByKind[cells[0]]
  if (!name || !ns || !resource) return

  detailTitle.value = `${resource}/${name}`
  detailKey.value = { cluster: clusterKey.value, kind: resource, namespace: ns, name }
  detailRow.value = row
  detailVisible.value = true

  const st = containerDetailState(clusterKey.value, resource, ns, name)
  if (st.busy && !force) return

  st.error = ''
  st.busy = true
  try {
    st.result = await http.get(liveURL('object', { namespace: ns, resource, name }))
  } catch (e) {
    st.error = (e && e.message) || '查询失败'
  } finally {
    st.busy = false
  }
}

// refreshDetail 是抽屉里的「刷新」：显式重查同一个对象的详情。
function refreshDetail() {
  if (detailRow.value) describeRow(detailRow.value, true)
}

/* ===== Pod 日志 ===== */

// openLogs 打开某个 Pod 的日志抽屉（每次打开都现拉一次：直连是即时的）。
function openLogs(row) {
  const spec = DETAIL_SPEC.pods
  const cells = row.cells || []
  openLogsFor(cells[spec.nsCol], cells[spec.nameCol])
}

// openLogsFor 是 openLogs 的本体：反向联动（从台账点进来）只有身份、没有表格行，
// 因此身份必须能独立传入——否则那条路径只能伪造一行假数据。
function openLogsFor(namespace, pod) {
  if (!namespace || !pod) return
  logsTarget.value = { namespace, pod }
  logsContainer.value = ''
  logsTitle.value = '日志 · ' + pod
  logsVisible.value = true
  fetchLogs(false)
}

/* ===== 跨页联动 ===== */

// searchLogsFor 跳到集中日志检索，按容器身份收窄（`pods=<命名空间>|<Pod>`）。
//
// 与「日志」是两条**数据源不同**的路径：那条是**按需拉取**（Agent 现拉现回，不依赖任何采集配置，
// 但行数与时间窗封顶），这条是**集中日志**（靠该节点开了 `logSources[].podLogs` 采到的东西，
// 因此**可能是空的**——"配了却没数据"的原因由检索页的空态提示说清，不能让人以为日志丢了）。
//
// 两个刻意的"不带"：
//   · 节点：这里只知道 Pod 名，不知道它此刻落在哪台机器上；容器条件本身已经足够精确，
//     猜一个节点猜错就是一个空结果（扫描范围由服务端按授权收窄）。
//   · 时间窗：用检索页自己的默认（最近 1 小时），与那一页的口径一致；要看更早的范围在那里改。
function searchLogsFor(namespace, pod) {
  if (!namespace || !pod) return
  router.push({ path: '/logs', query: { pods: namespace + '|' + pod } })
}

// searchLogs 从表格行取身份——与「日志」同一个来源（DETAIL_SPEC.pods 的列下标）。
function searchLogs(row) {
  const spec = DETAIL_SPEC.pods
  const cells = row.cells || []
  searchLogsFor(cells[spec.nsCol], cells[spec.nameCol])
}

// openLedger 跳到台账里这条 Pod / 工作负载的资产详情。
//
// 自然键由服务端按身份拼（前端不拼）：集群取 **apiserver 地址**而不是别名——
// 台账的 Pod/工作负载资产就是按地址建的（见 collector.k8s.go 的 clusterOf）。
async function openLedger(row) {
  const spec = DETAIL_SPEC[activeTab.value]
  const cluster = currentCluster.value
  if (!spec || !cluster) return
  const cells = row.cells || []
  const params = {
    type: activeTab.value === 'pods' ? 'pod' : 'workload',
    cluster: cluster.instance,
    namespace: cells[spec.nsCol],
    name: cells[spec.nameCol],
  }
  if (activeTab.value === 'workloads') {
    const kind = WORKLOAD_LEDGER_KINDS[cells[0]]
    if (!kind) {
      ElMessage.info('该对象类型不进台账：只有 Deployment / StatefulSet / DaemonSet 会建资产')
      return
    }
    params.kind = kind
  }
  ledgerBusy.value = true
  try {
    const res = await lookupAsset(params)
    // 404 在这里是**正常结果**：清单上报有一个采集周期的延迟，刚建的 Pod 可能还没进台账
    if (res.status === 404) {
      ElMessage.info('该对象尚未进入台账（清单上报约一个采集周期，稍后再看）')
      return
    }
    if (!res.ok) {
      ElMessage.error((res.body && res.body.error) || '查询台账失败')
      return
    }
    const item = res.body && res.body.asset
    if (!item) {
      ElMessage.error('台账返回内容不完整')
      return
    }
    if (item.ignored) {
      ElMessage.warning('该资产已被从台账隐藏，可在「资产台账 → 含已忽略」中恢复')
    }
    router.push({ path: '/assets', query: { id: item.id } })
  } catch (e) {
    ElMessage.error(e.message || '查询台账失败')
  } finally {
    ledgerBusy.value = false
  }
}

// applyDeepLink 处理从台账反向点进来的地址（?cluster=&namespace=&pod=）：
// 选中集群 → 切到 Pod Tab → 打开该 Pod 的日志抽屉。
//
// 只认领一次（消费后把 query 里的定位参数清掉）：否则用户在这个页面上手动切集群，
// 又被 query 拽回去，看起来就像"选了没用"。
async function applyDeepLink() {
  const q = route.query
  const pod = String(q.pod || '').trim()
  const tab = String(q.tab || '').trim()
  const clusterRef = String(q.cluster || '').trim()
  const ns = String(q.namespace || '').trim()
  if (!pod && !tab && !clusterRef) return
  router.replace({ path: '/container', query: {} })

  const hit = clusters.value.find((c) => c.instance === clusterRef || c.name === clusterRef)
  if (!hit) {
    ElMessage.warning('未找到该对象所属的集群：可能该节点未上报，或集群凭据已变更')
    return
  }
  clusterKey.value = keyOf(hit)
  if (ns) namespace.value = ns
  if (tab && TABS.some((t) => t.key === tab)) activeTab.value = tab
  if (pod) activeTab.value = 'pods'
  await nextTick()
  // 列表：进页面就查一次（直连是同步的）；日志抽屉另走一次按需拉取
  run(activeTab.value)
  if (pod) openLogsFor(ns, pod)
}

// fetchLogs 拉取当前 Pod 的最近日志；force=true 表示用户明确要求重新拉取。
//
// 行数与时间窗由服务端的规格兜住（默认 200 行、最多 500 行 / 24 小时）：
// 界面不提供"全量下载"，那是另一条通道（集中日志）的事，不是这一页能顺手打开的。
async function fetchLogs(force = false) {
  const target = logsTarget.value
  if (!target || !currentCluster.value) return
  const container = logsContainer.value.trim()
  // 缓存键带上容器名：换容器是换内容，不能把上一个容器的日志显示在新容器名下。
  const cacheName = target.pod + (container ? '/' + container : '')
  logsKey.value = {
    cluster: clusterKey.value, kind: 'container.logs', namespace: target.namespace, name: cacheName,
  }
  const st = containerDetailState(clusterKey.value, 'container.logs', target.namespace, cacheName)
  // 与详情同一取向：直连之后不再"命中缓存就不查"，只跳过正在进行的同一次请求。
  if (st.busy && !force) return

  st.error = ''
  st.busy = true
  try {
    const extra = { namespace: target.namespace, name: target.pod }
    if (container) extra.container = container
    st.result = await http.get(liveURL('logs', extra))
  } catch (e) {
    st.error = (e && e.message) || '拉取失败'
  } finally {
    st.busy = false
  }
}

onMounted(async () => {
  // 平台深链（?platform=docker|k8s）：中间件监控的 Docker Tab 跳过来时用得上。
  // 只认一次（消费后把参数清掉），否则用户手动切回去又被 query 拽回来。
  const wantPlatform = String(route.query.platform || '').trim()
  if (wantPlatform) {
    const next = { ...route.query }
    delete next.platform
    router.replace({ path: '/container', query: next })
    if (wantPlatform === 'docker' && canDocker.value) {
      platform.value = 'docker'
      // Docker 面板自己负责首次加载，这里不必再触发。
      return
    }
  }
  await loadClusters()
  // Server 侧配了哪些集群：用来提示"当前这个没配直连凭据"，拉不到就不提示。
  await loadLiveClusters()
  // 从台账反向点进来时优先按地址定位（applyDeepLink 内部会自己触发列表与日志的拉取）
  await applyDeepLink()
  // 进页面就查一次：直连是同步的，没有"等一个上报周期"的代价，没必要让人对着空面板猜。
  await run(activeTab.value)
})

// 切 Tab 直接查：直连查询是即时的，缓存旧结果的唯一价值是刷新期间不白屏。
watch(activeTab, (tab) => {
  nextTick(() => run(tab))
})

onBeforeUnmount(() => {
  for (const h of writeTimers) clearTimeout(h)
  writeTimers.clear()
})
</script>

<style scoped>
.container-view {
  display: flex;
  flex-direction: column;
  gap: 16px;
}
/* 平台切换：独占一行放在内容区顶部（别缩在页头右侧与「选择集群」挤成同级），
   但**收在行尾**——它是"整页模式"，不必占着标题旁的视线焦点。
   尺寸只比工具控件略收一点（13px / 27px 高 vs 下拉框的 13px / 32px）：够看得见，不喧宾夺主。
   行间距由 .container-view 的 gap 给，这里不再加 margin。 */
.platform-bar {
  display: flex;
  align-items: center;
  gap: var(--sp-3);
  flex-wrap: wrap;
}
/* 靠右：margin-left:auto 把「平台 + 开关」推到行尾，左边留给它依赖的目标选择器。 */
.platform-pick {
  display: flex;
  align-items: center;
  gap: var(--sp-2);
  margin-left: auto;
}
.platform-bar-label {
  font-size: var(--fs-sm);
  color: var(--t3);
  letter-spacing: 0.04em;
}
/* 无 docker:read 时开关不渲染，这里如实写出当前平台，不留悬空的「平台」标签。 */
.platform-locked {
  font-size: var(--fs-sm);
  font-weight: 600;
  color: var(--t2);
}
.platform-switch :deep(.el-radio-button__inner) {
  padding: 7px 14px;
  font-size: var(--fs-sm);
  font-weight: 600;
}
.cluster-select {
  width: 240px;
}
.container-tabs {
  margin-bottom: 0;
}
.container-tabs :deep(.el-tabs__header) {
  margin: 0;
}
.container-tabs :deep(.el-tabs__content) {
  display: none;
}
.cluster-hint,
.offline-hint {
  padding: 10px 14px;
  border-radius: var(--r-md);
  border: 1px solid var(--bd);
  background: var(--fill-1);
  font-size: var(--fs-sm);
  color: var(--t2);
}
.offline-hint {
  border-color: var(--warn-bd);
  background: var(--warn-dim);
  color: var(--warn);
}
.query-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
}
.ns-input {
  width: 220px;
}
.query-error {
  margin-bottom: 12px;
}
.query-state {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 14px;
  margin-bottom: 12px;
  border-radius: var(--r-md);
  border: 1px solid var(--bd);
  background: var(--fill-1);
  font-size: var(--fs-sm);
  color: var(--t2);
}
.state-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
}
.state-dot.delivering {
  background: var(--accent);
}
.result-notice {
  margin-bottom: 10px;
  padding: 8px 12px;
  border-radius: var(--r-sm);
  background: var(--info-dim);
  border: 1px solid var(--info-bd);
  color: var(--info);
  font-size: var(--fs-sm);
}
.result-table :deep(.el-table__row) {
  cursor: pointer;
}
.result-foot {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 12px;
  margin-top: 10px;
  font-size: var(--fs-xs);
  color: var(--t3);
}
.foot-warn {
  color: var(--warn);
}
/* 刷新中的提示推到右侧，不和计数挤在一起。 */
.foot-time {
  margin-left: auto;
}
.detail-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 12px;
  font-size: var(--fs-xs);
}
.drawer-loading {
  padding: 24px 0;
  text-align: center;
  color: var(--t3);
  font-size: var(--fs-sm);
}
.logs-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
}
.logs-container {
  width: 200px;
}
/* 日志按原样呈现：pre-wrap 保留缩进与换行，等宽字体让堆栈/表格对得齐。
   行数上限由服务端动作目录兜住，这里只负责滚动，不做二次截断。 */
.log-pre {
  margin: 0;
  padding: 12px;
  max-height: calc(100vh - 220px);
  overflow: auto;
  background: var(--s0);
  /* 与同页 .result-notice / .query-state 一致用「描边 + 语义底色」：
     此前这里是未定义的 var(--bg-sunken) 回退 4% 黑，深色下几乎看不出底色且没有边界，
     长日志块会与页面底色糊在一起。 */
  border: 1px solid var(--bd);
  border-radius: 6px;
  font-family: var(--mono);
  font-size: var(--fs-xs);
  line-height: 1.5;
  white-space: pre-wrap;
  word-break: break-all;
}
</style>
