<template>
  <div class="docker-panel">
    <!-- 没配端点时先把"怎么配"说清楚，而不是给一个空表格 -->
    <EmptyState
      v-if="!loading && endpoints.length === 0"
      :icon="Grid"
      title="尚未采集到 Docker 端点"
      :hints="[
        '在目标机器的 agent.yaml 里配置 dockerInstances（unix:///var/run/docker.sock 或 tcp://host:2375），Agent 采集（collectors.docker: true）开启后这里才有端点可选',
        '查询由 Server 直连 daemon：unix socket 只在 Server 与 daemon 同机时可用；跨机器请在目标机器上把 daemon 暴露为 tcp://（或挂一个 socket 代理）',
        '端点清单来自 Agent 上报，配置生效需要等一个上报周期',
      ]"
    />

    <template v-else>
      <div class="docker-toolbar">
        <el-select
          v-model="epKey"
          size="small"
          placeholder="选择 Docker 端点"
          class="ep-select"
          @change="onEndpointChange"
        >
          <el-option v-for="e in endpoints" :key="keyOf(e)" :value="keyOf(e)" :label="labelOf(e)" />
        </el-select>
        <el-tabs v-model="activeTab" type="card" class="docker-tabs" @tab-change="onTabChange">
          <el-tab-pane v-for="t in TABS" :key="t.key" :label="t.title" :name="t.key" />
        </el-tabs>
        <el-button
          size="small"
          type="primary"
          :loading="q.busy"
          :disabled="!currentEndpoint"
          title="按当前端点重新查询一次（直连 daemon，同步返回）"
          @click="run(activeTab)"
        >
          查询
        </el-button>
      </div>

      <div v-if="!currentEndpoint" class="host-hint">请选择要查看的 Docker 端点</div>

      <template v-else>
        <!-- 两种"用不了"要分开说：地址形态问题（部署时要改）vs 端点当前不可达（等 Agent 下一轮） -->
        <div v-if="currentEndpoint.unixSocket && !currentEndpoint.reachable" class="offline-hint">
          该端点用的是 unix socket 且当前不可达：unix socket 只有 Server 与 daemon 在同一台机器时才能直连。
          跨机器请在目标机器上把 daemon 暴露为 tcp://（或前面挂一个 socket 代理）
        </div>
        <div v-else-if="!currentEndpoint.reachable" class="offline-hint">
          该端点当前不可达（上报节点可能已离线），查询会失败
        </div>

        <SectionCard title="查询结果" dense>
          <el-alert v-if="q.error" type="error" :closable="false" show-icon :title="q.error" />

          <template v-if="result">
            <div v-if="result.notice" class="result-notice">{{ result.notice }}</div>

            <el-table :data="resultRows" size="small" border :max-height="520">
              <el-table-column
                v-for="(col, i) in result.columns || []"
                :key="i"
                :label="col"
                :prop="String(i)"
                show-overflow-tooltip
              />
              <!-- 容器行：只读的详情 / 日志人人可用；写动作只对超管出现（docker:write 是超管专属） -->
              <el-table-column
                v-if="activeTab === 'containers'"
                label="操作"
                :width="canDockerWrite ? 280 : 120"
                fixed="right"
              >
                <template #default="{ row }">
                  <el-button link type="primary" size="small" @click="openDetail(row)">详情</el-button>
                  <el-button link type="primary" size="small" @click="openLogs(row)">日志</el-button>
                  <template v-if="canDockerWrite">
                    <el-button v-if="isRunning(row)" link type="warning" size="small" @click="runWrite('stop', row)">停止</el-button>
                    <el-button v-if="isRunning(row)" link type="warning" size="small" @click="runWrite('restart', row)">重启</el-button>
                    <el-button v-else link type="success" size="small" @click="runWrite('start', row)">启动</el-button>
                    <el-button link type="danger" size="small" @click="runWrite('rm', row)">删除</el-button>
                  </template>
                </template>
              </el-table-column>
              <!-- 镜像行：只有删除是写动作 -->
              <el-table-column
                v-if="activeTab === 'images' && canDockerWrite"
                label="操作"
                width="90"
                fixed="right"
              >
                <template #default="{ row }">
                  <el-button link type="danger" size="small" @click="runWrite('imageRm', row)">删除</el-button>
                </template>
              </el-table-column>
            </el-table>

            <div class="result-foot">
              <span v-if="result.total != null">共 {{ result.total }} 条</span>
              <span v-if="q.busy" class="foot-time">正在查询…</span>
              <span v-if="result.truncated" class="foot-warn">结果已截断</span>
            </div>
          </template>

          <EmptyState
            v-else-if="!q.busy && !q.error"
            :icon="Grid"
            title="还没有查询结果"
            :hints="['点「查询」按当前端点直连 daemon 取一次当前态（同步返回）']"
          />
        </SectionCard>
      </template>
    </template>

    <!-- 容器详情抽屉（docker.inspect：白名单投影，环境变量只给名称） -->
    <el-drawer v-model="detailVisible" :title="detailTitle" size="620px" destroy-on-close>
      <el-alert
        v-if="detail.error"
        type="error"
        :closable="false"
        :title="detail.error"
        class="drawer-alert"
      />
      <div v-else-if="detail.busy" class="q-hint">正在查询…</div>
      <div v-else-if="!detail.result" class="q-hint">尚无详情。</div>
      <template v-else>
        <div v-if="detail.result.notice" class="result-notice">{{ detail.result.notice }}</div>
        <el-table :data="detailRows" size="small" border>
          <el-table-column
            v-for="(col, i) in detail.result.columns || []"
            :key="i"
            :label="col"
            :prop="String(i)"
            show-overflow-tooltip
          />
        </el-table>
      </template>
    </el-drawer>

    <!-- 容器日志抽屉（docker.logs：按需拉取，行数有硬上限） -->
    <el-drawer v-model="logsVisible" :title="logsTitle" size="760px" destroy-on-close>
      <el-alert v-if="logs.error" type="error" :closable="false" :title="logs.error" class="drawer-alert" />
      <div v-else-if="logs.busy" class="q-hint">正在拉取…</div>
      <template v-else>
        <div class="logs-toolbar">
          <span v-if="logs.result" class="logs-notice">{{ logs.result.notice }}</span>
          <el-button size="small" :icon="Refresh" :loading="logs.busy" @click="fetchLogs()">
            重新拉取
          </el-button>
        </div>
        <pre v-if="logsLines" class="logs-body">{{ logsLines }}</pre>
        <EmptyState v-else title="这段时间窗内没有日志" />
      </template>
    </el-drawer>
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { Refresh, Grid } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import http from '../../api/http'
import useAuth from '../../composables/useAuth'
import SectionCard from '../common/SectionCard.vue'
import EmptyState from '../common/EmptyState.vue'
import { containerDetailState, containerQueryState } from './queryCache'

// Docker 管理面。
//
// 数据通路：**Server 直连 Docker daemon**（ADR-0006 修订 ADR-0005）。
// 原先的形态是把 docker.* 动作下发给 Agent、由它查本机 socket 再随上报回执送回
// （延迟一个上报周期），已被取代——理由与 K8s 完全相同：
// 「异步下发的延迟等于一个上报周期，不满足管理面的要求」（ADR-0004）。
// 端点清单来自 Agent 上报的 dockerInstances（含 daemon 地址），运维不必在 Server 上另配一份。
//
// 唯一的形态约束：daemon 地址必须是 Server 够得着的。unix socket 只在同机时可用，
// 跨机器要把 daemon 暴露为 tcp://（或挂 socket 代理）——不可达时服务端会给出这句提示。

const TABS = [
  { key: 'info', cap: '/info', title: '引擎概览' },
  { key: 'containers', cap: '/containers', title: '容器' },
  { key: 'images', cap: '/images', title: '镜像' },
]

const endpoints = ref([])
const loading = ref(false)
const epKey = ref('')
const activeTab = ref('info')

const detailVisible = ref(false)
const detailTitle = ref('容器详情')
const logsVisible = ref(false)
const logsTitle = ref('容器日志')
const logsTarget = ref('')

// docker:write 是**超管专属**权限点（用户 2026-10-10 拍板）：写动作会直接改变机器上的运行态，
// 而 Docker 里一个容器往往就是一项业务。无权时按钮完全不出现——摆一个点了必 403 的按钮
// 只会让人以为平台坏了。
const { can } = useAuth()
const canDockerWrite = computed(() => can('docker:write'))
// writeBusy 是"正在执行一次写操作"的闸门：写操作不能靠连点两次来加速。
const writeBusy = ref(false)

const keyOf = (e) => e.node + '|' + e.name
const labelOf = (e) => `${e.name}（${e.node}${e.reachable ? '' : ' · 不可达'}）`
const currentEndpoint = computed(() => endpoints.value.find((e) => keyOf(e) === epKey.value) || null)

// 状态按「端点 + Tab」分键：换端点是换视图，切回来先显示上次那份（不白屏），随即被新结果替换。
const q = computed(() => containerQueryState('docker|' + epKey.value, activeTab.value))
const result = computed(() => q.value.result)

const detail = ref({ busy: false, error: '', result: null, at: 0 })
const logs = ref({ busy: false, error: '', result: null, at: 0 })

const resultRows = computed(() => toRows(result.value))
const detailRows = computed(() => toRows(detail.value.result))
const logsLines = computed(() => {
  const r = logs.value.result
  if (!r || !r.rows) return ''
  return r.rows.map((row) => (Array.isArray(row) ? row[0] : '')).join('\n')
})

function toRows(payload) {
  if (!payload) return []
  return (payload.rows || []).map((row) => {
    const cells = Array.isArray(row) ? row : row.cells || []
    const obj = {}
    cells.forEach((v, i) => {
      obj[String(i)] = v
    })
    return obj
  })
}

// epURL 组出一次直连请求的地址。
// node + name 共同定位端点（同一个 name 可能出现在多台机器上，只按 name 会命中错的那台）；
// 对象标识走 encodeURIComponent——它会被拼进 daemon 的 URL 路径。
function epURL(path, extra = {}) {
  const ep = currentEndpoint.value
  if (!ep) return ''
  const params = new URLSearchParams({ node: ep.node, name: ep.name })
  for (const [k, v] of Object.entries(extra)) {
    if (v !== '' && v !== undefined && v !== null) params.set(k, String(v))
  }
  return '/api/v1/container/docker' + path + '?' + params.toString()
}

async function loadEndpoints() {
  loading.value = true
  try {
    const res = await http.get('/api/v1/container/docker/endpoints')
    endpoints.value = (res && res.endpoints) || []
    if (!endpoints.value.some((e) => keyOf(e) === epKey.value)) {
      // 默认选第一个可达端点：直接给一个可用的起点。
      const first = endpoints.value.find((e) => e.reachable) || endpoints.value[0]
      epKey.value = first ? keyOf(first) : ''
    }
  } catch (e) {
    endpoints.value = []
    ElMessage.error((e && e.message) || '获取 Docker 端点失败')
  } finally {
    loading.value = false
  }
}

// run 按当前（端点 + Tab）查询一次。直连是同步的，因此每次换视图都直接查。
// **不清 result**：刷新期间旧结果继续留在表里，避免白屏。
async function run(tab) {
  const target = TABS.find((t) => t.key === tab)
  const item = containerQueryState('docker|' + epKey.value, tab)
  if (!target || !currentEndpoint.value) return

  item.error = ''
  item.busy = true
  try {
    const extra = tab === 'containers' ? { all: 'true' } : {}
    item.result = await http.get(epURL(target.cap, extra))
  } catch (e) {
    // 服务端的说明就是给用户的下一步（daemon 不可达 / unix socket 跨机 / 对象不在），原样展示。
    item.error = (e && e.message) || '查询失败'
  } finally {
    item.busy = false
  }
}

function onTabChange(tab) {
  run(tab)
}

function onEndpointChange() {
  run(activeTab.value)
}

// 容器行的标识取「名称」列（第 1 列）：确认框里显示名称比显示 ID 有用得多。
function containerName(row) {
  return String(row?.['0'] ?? '').trim()
}

// 镜像行的标识取「镜像 ID」列（第 2 列）：服务端刻意只接受 ID——
// 含 registry 路径的镜像名里有斜杠，会改变请求路径。
function imageID(row) {
  return String(row?.['1'] ?? '').trim()
}

// isRunning 按 `docker ps` 的口径判断：运行中的容器 Status 以 "Up" 开头
// （如 "Up 3 hours"、"Up 3 hours (healthy)"）；停止的是 "Exited (0) …"。
// 用它决定显示「停止/重启」还是「启动」——比两个按钮都摆出来更不容易点错。
//
// 取值一律走可选链：表格在某些渲染路径下会给出没有 row 的空单元格，
// 那时应当**什么都不给**（当作"状态未知"），而不是把"启动"摆到一个正在运行的容器上。
function isRunning(row) {
  return String(row?.['1'] ?? '').trim().startsWith('Up')
}

function openDetail(row) {
  const name = containerName(row)
  if (!name) return
  detailTitle.value = `容器详情 · ${name}`
  detailVisible.value = true
  fetchDetail(name)
}

async function fetchDetail(name) {
  detail.value = { busy: true, error: '', result: null, at: 0 }
  try {
    const res = await http.get(epURL('/containers/' + encodeURIComponent(name)))
    detail.value = { busy: false, error: '', result: res, at: Date.now() }
  } catch (e) {
    detail.value = { busy: false, error: (e && e.message) || '查询失败', result: null, at: 0 }
  }
}

function openLogs(row) {
  const name = containerName(row)
  if (!name) return
  logsTitle.value = `容器日志 · ${name}`
  logsVisible.value = true
  logsTarget.value = name
  fetchLogs()
}

async function fetchLogs() {
  const target = logsTarget.value
  if (!target) return
  logs.value = { busy: true, error: '', result: null, at: 0 }
  try {
    const res = await http.get(epURL('/containers/' + encodeURIComponent(target) + '/logs'))
    logs.value = { busy: false, error: '', result: res, at: Date.now() }
  } catch (e) {
    logs.value = { busy: false, error: (e && e.message) || '拉取失败', result: null, at: 0 }
  }
}

// ---- 写操作（docker:write，仅超管）----

const WRITE_SPEC = {
  start: { method: 'POST', label: '启动', tail: '/start', what: '容器' },
  stop: { method: 'POST', label: '停止', tail: '/stop', what: '容器' },
  restart: { method: 'POST', label: '重启', tail: '/restart', what: '容器' },
  rm: { method: 'DELETE', label: '删除', tail: '', what: '容器' },
  imageRm: { method: 'DELETE', label: '删除', tail: '', what: '镜像' },
}

// runWrite 执行一次写操作：二次确认 → 同步请求 → 提示 → 重新拉列表。
//
// 写操作是**同步直连**的（ADR-0006：读写都直连），因此这里没有"等一个上报周期"那段，
// daemon 的结论（"容器已经在运行"「被引用的镜像删不掉」）会当场回到界面上。
async function runWrite(action, row) {
  if (writeBusy.value) return
  const spec = WRITE_SPEC[action]
  const ep = currentEndpoint.value
  if (!spec || !ep) return
  const isImage = action === 'imageRm'
  const id = isImage ? imageID(row) : containerName(row)
  if (!id) return

  const what = `${spec.what} ${id}`
  let tip = `将对 ${what} 执行「${spec.label}」（端点 ${ep.name} · 节点 ${ep.node}）。`
  if (action === 'rm') tip += '容器会被删除，但卷不会被删除（数据仍在）。'
  if (action === 'imageRm') tip += '被容器引用的镜像会报错，而不是被强行删掉。'
  if (action === 'stop' || action === 'restart') tip += '容器正在跑的业务会中断。'
  try {
    await ElMessageBox.confirm(tip, `确认${spec.label}`, {
      type: 'warning', confirmButtonText: spec.label, cancelButtonText: '取消',
    })
  } catch {
    return
  }

  writeBusy.value = true
  try {
    const path = isImage
      ? '/images/' + encodeURIComponent(id)
      : '/containers/' + encodeURIComponent(id) + spec.tail
    const res = spec.method === 'DELETE'
      ? await http.del(epURL(path))
      : await http.post(epURL(path), {})
    // 平台若开启了高风险操作审批，写动作会返回 202 + pending：**尚未执行**。
    // 把它说成"成功"会让操作者以为容器已经停了。
    if (res && res.pending) {
      ElMessage.warning(res.message || '已提交审批：本动作尚未执行，需另一位同事批准后才会执行')
      return
    }
    ElMessage.success(`${spec.label}成功：${what}`)
    // 写操作改变了运行态：列表必须重新拉一次，否则界面还显示着旧的状态。
    await run(activeTab.value)
  } catch (e) {
    ElMessage.error((e && e.message) || `${spec.label}失败`)
  } finally {
    writeBusy.value = false
  }
}

onMounted(async () => {
  await loadEndpoints()
  run(activeTab.value)
})
</script>

<style scoped>
.docker-panel {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.docker-toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}
.ep-select {
  width: 260px;
}
.docker-tabs {
  margin-bottom: -18px;
}
.host-hint,
.q-hint {
  color: var(--text-muted, #8a94a6);
  font-size: 13px;
  padding: 8px 0;
}
.offline-hint {
  color: var(--warn, #f0a020);
  font-size: 13px;
  padding: 6px 0;
}
.result-notice {
  font-size: 12px;
  color: var(--text-muted, #8a94a6);
  padding: 4px 0 8px;
}
.result-foot {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-top: 10px;
  font-size: 12px;
  color: var(--text-muted, #8a94a6);
}
.foot-warn {
  color: var(--warn, #f0a020);
}
.drawer-alert {
  margin-bottom: 12px;
}
.logs-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-bottom: 8px;
}
.logs-notice {
  font-size: 12px;
  color: var(--text-muted, #8a94a6);
}
.logs-body {
  margin: 0;
  padding: 12px;
  background: var(--bg-elev, #0b1220);
  border-radius: 6px;
  font-family: var(--mono, monospace);
  font-size: 12px;
  line-height: 1.6;
  max-height: 620px;
  overflow: auto;
  white-space: pre-wrap;
  word-break: break-all;
}
</style>
