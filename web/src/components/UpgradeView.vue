<template>
  <div class="upgrade-view">
    <PageHeader
      title="版本升级"
      desc="上传服务端升级包并完成升级；升级前建议先查看当前版本与构建时间"
    />

    <!-- 页面级错误：升级失败时「待升级」卡片可能已被清空，错误必须有固定可见位置 -->
    <div v-if="applyError" class="page-error" role="alert">
      <el-icon class="pe-icon"><WarningFilled /></el-icon>
      <span class="pe-msg">{{ applyError }}</span>
      <el-button size="small" link @click="applyError = ''">关闭</el-button>
    </div>

    <!-- 部分数据加载失败：原来是 catch{/*ignore*/}，失败只会显示 '-'，分不清"没配"与"接口挂了" -->
    <div v-if="loadErrors.length" class="page-warn">
      <el-icon><WarningFilled /></el-icon>
      <span>部分数据加载失败：{{ loadErrors.join('、') }}，以下显示的可能不是最新数据。</span>
      <el-button size="small" link @click="reloadAll">重新加载</el-button>
    </div>

    <!-- 升级/回退进行中：把最长 40 秒的静默轮询变成可见的三步 -->
    <SectionCard
      v-if="applyPhase"
      id="sec-progress"
      :title="applyKind === 'rollback' ? '回退进行中' : '升级进行中'"
      :subtitle="applyKind === 'rollback' ? '按所选版本替换文件并重启 monitor-server' : '替换 Server / Web / Agent 并重启 monitor-server'"
    >
      <ol class="steps">
        <li
          v-for="(s, i) in steps"
          :key="s.key"
          class="step"
          :class="{
            on: stepIndex === i + 1,
            done: stepIndex > i + 1,
            failed: applyPhase === 'failed' && stepIndex === i + 1,
          }"
        >
          <span class="step-dot">
            <el-icon v-if="stepIndex > i + 1"><Check /></el-icon>
            <template v-else>{{ i + 1 }}</template>
          </span>
          <span class="step-text">{{ s.label }}</span>
        </li>
      </ol>
      <div
        class="step-hint"
        :class="{ danger: applyPhase === 'failed', warn: applyPhase === 'timeout' }"
      >{{ stepHint }}</div>
      <div v-if="applyPhase === 'failed' || applyPhase === 'timeout'" class="step-actions">
        <el-button size="small" @click="applyPhase = ''">关闭</el-button>
      </div>
    </SectionCard>

    <!-- 当前版本 -->
    <SectionCard id="sec-version" title="当前版本">
      <div class="ver-grid">
        <div class="ver-item">
          <span class="ver-label">版本</span>
          <span class="ver-val">{{ currentVersion.server || '...' }}</span>
        </div>
        <div class="ver-item">
          <span class="ver-label">构建时间</span>
          <span class="ver-val">{{ fmtBuildTime(currentVersion.buildTime) }}</span>
        </div>
      </div>
      <div v-if="versionErr" class="load-hint">
        未能获取服务端实际运行版本（{{ versionErr }}），上面显示的是 Web 构建内嵌版本。
      </div>
    </SectionCard>

    <!-- 上传 -->
    <SectionCard id="sec-upload" title="上传升级包">
      <el-upload
        drag
        :show-file-list="false"
        :http-request="onUpload"
        :before-upload="beforeUpload"
        :disabled="uploading"
        accept=".tar.gz,.tgz"
        class="upload-area"
      >
        <el-icon class="el-icon--upload"><upload-filled /></el-icon>
        <div class="el-upload__text">
          拖拽 upgrade 包到此，或<em>点击上传</em>
        </div>
        <template #tip>
          <div class="el-upload__tip">
            支持 *-upgrade.tar.gz（如 yingzhao-v1.30.80-upgrade.tar.gz）；最大 500MB
          </div>
        </template>
      </el-upload>

      <!-- 上传失败就地错误区：原来只弹一条消息，拖放区看不出任何原因 -->
      <div v-if="uploadError" class="upload-error" role="alert">
        <el-icon class="ue-icon"><WarningFilled /></el-icon>
        <div class="ue-body">
          <div class="ue-name">{{ uploadError.name }}</div>
          <div class="ue-reason">{{ uploadError.reason }}</div>
        </div>
        <el-button size="small" @click="retryUpload">重试</el-button>
        <el-button size="small" link @click="clearUploadError">忽略</el-button>
      </div>

      <div v-if="uploading" class="upload-progress">
        <el-progress
          :percentage="uploadProgress"
          :stroke-width="14"
          :status="uploadProgress >= 100 ? 'success' : undefined"
        />
        <div class="upload-progress-text">
          {{ uploadProgress >= 100 ? '服务端解析中…' : '上传中 ' + uploadProgress + '%' }}
        </div>
      </div>
    </SectionCard>

    <!-- 待应用 -->
    <SectionCard v-if="pending" id="sec-pending" title="待升级">
      <div class="pending-grid">
        <div class="pending-row">
          <span class="r-label">新版本</span>
          <span class="r-val highlight">v{{ pending.version }}</span>
        </div>
        <div class="pending-row">
          <span class="r-label">上传时间</span>
          <span class="r-val">{{ fmtTime(pending.uploadedAt) }}</span>
        </div>
        <div class="pending-row">
          <span class="r-label">Server</span>
          <span class="r-val">
            <el-tag v-if="pending.serverArch" type="success" size="small">{{ pending.serverArch }} · {{ fmtSize(pending.serverSize) }}</el-tag>
            <el-tag v-else type="info" size="small">未包含本架构</el-tag>
          </span>
        </div>
        <div class="pending-row">
          <span class="r-label">Web</span>
          <span class="r-val">
            <el-tag type="success" size="small">{{ fmtSize(pending.webSize) }}</el-tag>
          </span>
        </div>
        <div class="pending-row">
          <span class="r-label">Agent</span>
          <span class="r-val">
            <el-tag v-for="a in pending.agentArches" :key="a" type="success" size="small" class="tag-gap">{{ a }}</el-tag>
            <el-tag v-if="!pending.agentArches || !pending.agentArches.length" type="info" size="small">未包含</el-tag>
          </span>
        </div>
        <div class="pending-row" v-if="pending.components && pending.components.length">
          <span class="r-label">SHA256</span>
          <span class="r-val mono small">{{ pending.components[0].checksum || '-' }}</span>
        </div>
      </div>
      <div class="action-row">
        <el-button @click="cancelPending" :disabled="applying">取消</el-button>
        <el-button
          type="primary"
          :loading="applying"
          :disabled="applying || cooldown > 0 || !pending.serverArch"
          @click="doApply"
        >{{ cooldown > 0 ? `请等待 ${cooldown}s` : '立即升级' }}</el-button>
      </div>
      <el-alert
        v-if="!pending.serverArch"
        title="升级包未包含当前平台架构的 Server，无法执行升级。请联系管理员获取适配当前架构的升级包。"
        type="warning"
        show-icon
        :closable="false"
        class="card-alert"
      />
    </SectionCard>

    <!-- 升级历史与升级包合成一张表：每行既是"这次升级做了什么"，也直接显示该版本的
         升级包还在不在，并就地回退/删除 —— 分成两块时同一版本会出现两行、两个按钮。 -->
    <SectionCard id="sec-history" title="升级历史">
      <template #actions>
        <span v-if="archiveList.length" class="pkg-total">
          已保留 {{ archiveList.length }} 个升级包 · 合计 {{ fmtSize(archiveTotalSize) }}
        </span>
        <el-button size="small" link @click="refreshAll">刷新</el-button>
      </template>
      <el-table :data="pagedRows" v-loading="loadingHistory || archiveLoading">
        <template #empty>
          <EmptyState
            title="暂无升级记录"
            :hints="[
              '上传升级包并应用后，这里会记录每一次升级与回退',
              '升级包保留到被删除为止，删除后该版本无法再回退回来',
            ]"
          />
        </template>
        <el-table-column label="时间" width="160">
          <template #default="{ row }">{{ fmtTime(row.at) }}</template>
        </el-table-column>
        <el-table-column label="动作" width="80">
          <template #default="{ row }">
            <el-tag :type="actionTagType(row.action)" size="small" effect="plain">{{ actionLabel(row.action) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="版本" width="188">
          <template #default="{ row }">
            <span class="ver-cell">
              <span class="mono">v{{ row.version }}</span>
              <el-tag v-if="row.version === currentVersion.server" type="success" size="small" effect="plain">当前运行</el-tag>
            </span>
          </template>
        </el-table-column>
        <el-table-column label="升级包" width="150">
          <template #default="{ row }">
            <el-tag
              v-if="row.pkg"
              type="success"
              size="small"
              effect="plain"
              :title="'上传于 ' + fmtTime(row.pkg.uploadedAt)"
            >已保留 {{ fmtSize(row.pkg.size) }}</el-tag>
            <span v-else class="muted">已删除</span>
          </template>
        </el-table-column>
        <el-table-column label="结果" width="80">
          <template #default="{ row }">
            <el-tag :type="resultTagType(row.result)" size="small" effect="dark">{{ resultLabel(row.result) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="detail" label="详情" min-width="200" show-overflow-tooltip />
        <el-table-column label="操作" width="210" fixed="right" class-name="op-col">
          <template #default="{ row }">
            <el-button
              size="small"
              type="warning"
              plain
              :loading="rollingBack"
              :disabled="!canRollbackTo(row.version) || rollingBack || deletingVersion !== '' || cooldown > 0"
              @click="rollbackTo(row.version)"
            >{{ cooldown > 0 ? `请等待 ${cooldown}s` : '回退到此版本' }}</el-button>
            <el-button
              size="small"
              type="danger"
              plain
              :loading="deletingVersion === row.version"
              :disabled="!row.pkg || deletingVersion !== '' || rollingBack || row.version === currentVersion.server"
              :title="deleteHint(row)"
              @click="deleteArchive(row.version)"
            >删除</el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-pagination
        v-if="mergedRows.length"
        class="hist-pager"
        layout="total, sizes, prev, pager, next"
        :total="mergedRows.length"
        :page-size="historyPageSize"
        :current-page="historyPage"
        :page-sizes="[10, 20, 50]"
        background
        @current-change="(p) => (historyPage = p)"
        @size-change="(s) => { historyPageSize = s; historyPage = 1 }"
      />

      <!-- 危险区：回退与删除都是不可逆动作，与「刷新」明确隔离 -->
      <div v-if="mergedRows.length" class="danger-zone hist-danger">
        <div class="dz-title">危险操作</div>
        <p class="dz-desc">
          「回退到此版本」会按该版本重新替换 Server / Web / Agent 并重启 monitor-server，期间服务短暂不可用；
          「删除」会移除该版本的升级包，删除后不再可回退回来（需要时只能重新上传该版本的升级包）。
          当前运行版本的升级包不能删除。
        </p>
      </div>
    </SectionCard>

    <!-- IP 地理库（独立入口：只替换归属地数据，不影响 Server/Web/Agent，不重启服务） -->
    <SectionCard id="sec-geoip" title="IP 地理库" subtitle="只替换归属地数据，不影响系统升级、不重启服务">
      <template #actions>
        <el-button size="small" link @click="geoipOpen = !geoipOpen">{{ geoipOpen ? '收起' : '展开' }}</el-button>
        <el-button size="small" link @click="loadGeoip">刷新</el-button>
      </template>

      <div v-show="geoipOpen">
        <div class="pending-grid" v-loading="geoipLoading">
          <div class="pending-row">
            <span class="r-label">当前库</span>
            <span class="r-val">
              <el-tag :type="geoip.source === 'custom' ? 'success' : 'info'" size="small" effect="plain">
                {{ geoip.source === 'custom' ? '已更新的库' : '程序内置库' }}
              </el-tag>
              <el-tag v-if="geoip.loaded === false" type="danger" size="small" effect="dark" class="tag-gap">未加载</el-tag>
            </span>
          </div>
          <div class="pending-row">
            <span class="r-label">大小</span>
            <span class="r-val">{{ fmtSize(geoip.size) }}</span>
          </div>
          <div class="pending-row">
            <span class="r-label">更新时间</span>
            <span class="r-val">{{ geoip.updatedAt ? fmtTime(geoip.updatedAt) : '-' }}</span>
          </div>
          <div class="pending-row">
            <span class="r-label">存放路径</span>
            <span class="r-val mono small">{{ geoip.path || '未配置' }}</span>
          </div>
          <div class="pending-row">
            <span class="r-label">SHA256</span>
            <span class="r-val mono small">{{ geoip.sha256 || '-' }}</span>
          </div>
        </div>

        <el-alert
          v-if="geoip.error"
          :title="geoip.error"
          type="error"
          show-icon
          :closable="false"
          class="card-alert"
        />
        <el-alert
          v-else-if="geoip.editable === false"
          title="未配置 IP 地理库存放路径（server.yaml 的 geoipFile），当前仅能使用程序内置库"
          type="warning"
          show-icon
          :closable="false"
          class="card-alert"
        />

        <el-upload
          drag
          :show-file-list="false"
          :http-request="onGeoipUpload"
          :before-upload="beforeGeoipUpload"
          :disabled="geoipUploading || geoip.editable === false"
          accept=".xdb"
          class="upload-area upload-area-sp"
        >
          <el-icon class="el-icon--upload"><upload-filled /></el-icon>
          <div class="el-upload__text">拖拽 ip2region 库文件到此，或<em>点击上传</em></div>
          <template #tip>
            <div class="el-upload__tip">
              支持 ip2region v4（IPv4）的 .xdb 文件；上传后立即生效，无需重启服务或重新升级系统
            </div>
            <div class="el-upload__tip tip-2">
              升级包下载：ip2region 官方仓库
              <el-link type="primary" href="https://github.com/lionsoul2014/ip2region" target="_blank" underline="never">github.com/lionsoul2014/ip2region</el-link>
              的 data 目录下获取最新的 ip2region.xdb（需为 v4 格式），下载后将文件后缀/命名保留为 .xdb 即可上传
            </div>
          </template>
        </el-upload>
        <div v-if="geoipUploading" class="upload-progress">
          <el-progress
            :percentage="geoipProgress"
            :stroke-width="14"
            :status="geoipProgress >= 100 ? 'success' : undefined"
          />
          <div class="upload-progress-text">
            {{ geoipProgress >= 100 ? '服务端校验中…' : '上传中 ' + geoipProgress + '%' }}
          </div>
        </div>

        <div class="geo-test-row">
          <el-input
            v-model="geoipTestIP"
            placeholder="输入 IP 验证归属地，如 114.114.114.114"
            clearable
            class="geo-test-input"
            @keyup.enter="doGeoipTest"
          />
          <el-button :loading="geoipTesting" @click="doGeoipTest">查询</el-button>
          <span v-if="geoipTestResult" class="geo-test-result">
            {{ geoipTestResult }}
          </span>
        </div>

        <el-table
          v-if="geoip.samples && geoip.samples.length"
          :data="geoip.samples"
          size="small"
          class="geo-samples"
        >
          <el-table-column prop="ip" label="样本 IP" width="160" />
          <el-table-column prop="country" label="国家" width="120" />
          <el-table-column prop="province" label="省份" width="120" />
          <el-table-column prop="city" label="城市" width="120" />
          <el-table-column prop="region" label="原始记录" />
        </el-table>

        <!-- 危险区：恢复内置库会删除已上传的库文件 -->
        <div v-if="geoip.source === 'custom'" class="danger-zone geo-danger">
          <div class="dz-title">危险操作</div>
          <p class="dz-desc">
            恢复内置库会删除已上传的 .xdb 并改回程序内置库，删除后需重新上传才能恢复自定义归属地。
          </p>
          <el-button type="danger" plain :disabled="geoipUploading" @click="doGeoipReset">
            恢复程序内置库
          </el-button>
        </div>
      </div>
    </SectionCard>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { UploadFilled, WarningFilled, Check } from '@element-plus/icons-vue'
import http from '../api/http'
import PageHeader from './common/PageHeader.vue'
import SectionCard from './common/SectionCard.vue'
import EmptyState from './common/EmptyState.vue'
import { WEB_VERSION } from '../version'

const currentVersion = ref({ server: WEB_VERSION }) // 初始用构建内嵌版本，加载后覆盖为 Server 实际运行版本
const pending = ref(null)
const applying = ref(false)
const applyError = ref('')
const history = ref([])
const loadingHistory = ref(false)
const historyPage = ref(1)
const historyPageSize = ref(10)

// —— 加载失败标记（原来是 catch{/*ignore*/}，失败静默显示 '-'）——
const versionErr = ref('')
const pendingErr = ref('')
const historyErr = ref('')
const archiveErr = ref('')
const geoipLoadErr = ref('')
const loadErrors = computed(() => {
  const errs = []
  if (versionErr.value) errs.push('当前版本')
  if (pendingErr.value) errs.push('待升级状态')
  if (historyErr.value) errs.push('升级历史')
  if (archiveErr.value) errs.push('升级包清单')
  if (geoipLoadErr.value) errs.push('IP 地理库')
  return errs
})

// —— 升级/回退三步状态机 ——
// 应用后最长 40 秒是静默轮询，用户不知道是卡住还是在重启；这里把阶段暴露出来。
const applyPhase = ref('') // '' | 'submitted' | 'restarting' | 'verifying' | 'done' | 'failed' | 'timeout'
const applyKind = ref('upgrade') // 'upgrade' | 'rollback'
const steps = [
  { key: 'submitted', label: '已提交' },
  { key: 'restarting', label: '服务重启中' },
  { key: 'verifying', label: '正在校验版本' },
]
const stepIndex = computed(() => {
  const i = steps.findIndex((s) => s.key === applyPhase.value)
  if (i >= 0) return i + 1
  if (applyPhase.value === 'done' || applyPhase.value === 'failed') return 4
  return 0
})
const stepHint = computed(() => {
  switch (applyPhase.value) {
    case 'submitted':
      return '升级请求已提交，服务端正在准备替换文件…'
    case 'restarting':
      return 'monitor-server 正在重启，Web 端短暂不可用，请勿关闭页面…'
    case 'verifying':
      return '服务已恢复响应，正在确认实际运行版本是否已切换到目标版本…'
    case 'done':
      return '升级完成，页面即将自动刷新。'
    case 'failed':
      return applyError.value || '升级未完成，请查看下方升级历史。'
    case 'timeout':
      return '等待超时：服务恢复较慢，请稍后手动刷新页面并核对当前版本。'
    default:
      return ''
  }
})

// 历史事件 + 「已上传但从未应用」的升级包，合成一张表：后者保证每个保留的包都有行可回退/删除
// （上传本身不写历史，若只按历史出行，这类包就会无处可删）。
const mergedRows = computed(() => {
  const pkgByVersion = {}
  for (const p of archiveList.value) pkgByVersion[p.version] = p
  const rows = history.value.map((h) => ({
    ...h,
    key: h.id || `${h.action}-${h.version}-${h.at}`,
    pkg: pkgByVersion[h.version] || null,
  }))
  const seen = new Set(history.value.map((h) => h.version))
  for (const p of archiveList.value) {
    if (seen.has(p.version)) continue
    rows.push({
      key: 'pkg-' + p.version,
      action: 'upload',
      version: p.version,
      at: p.uploadedAt,
      result: 'pending',
      detail: '已上传，尚未应用',
      pkg: p,
    })
  }
  return rows.sort((a, b) => new Date(b.at) - new Date(a.at))
})
const pagedRows = computed(() => {
  const start = (historyPage.value - 1) * historyPageSize.value
  return mergedRows.value.slice(start, start + historyPageSize.value)
})
const uploading = ref(false)
const uploadProgress = ref(0)
const uploadError = ref(null) // { name, reason }
let lastUploadFile = null
const cooldown = ref(0)
let cooldownTimer = null

async function loadCurrentVersion() {
  try {
    currentVersion.value = await http.get('/api/v1/version')
    versionErr.value = ''
  } catch (e) {
    versionErr.value = e.message
  }
}
async function loadPending() {
  try {
    const r = await http.get('/api/v1/system/upgrade/current')
    pending.value = r.current || null
    pendingErr.value = ''
  } catch (e) {
    pendingErr.value = e.message
  }
}
async function loadHistory() {
  loadingHistory.value = true
  try {
    const r = await http.get('/api/v1/system/upgrade/history')
    history.value = r.history || []
    historyErr.value = ''
  } catch (e) {
    historyErr.value = e.message
  }
  loadingHistory.value = false
}

// 已归档升级包：只要没被删除，对应版本就能回退回去；删除后即从清单消失。
const archiveList = ref([])
const archiveVersions = computed(() => archiveList.value.map((p) => p.version))
const archiveTotalSize = computed(() => archiveList.value.reduce((sum, p) => sum + (p.size || 0), 0))
const archiveLoading = ref(false)
const rollingBack = ref(false)
const deletingVersion = ref('')

async function loadArchive() {
  archiveLoading.value = true
  try {
    const r = await http.get('/api/v1/system/upgrade/archive')
    archiveList.value = r.versions || []
    archiveErr.value = ''
  } catch (e) {
    archiveErr.value = e.message
  }
  archiveLoading.value = false
}

// 历史与升级包是同一张表的两个数据源，单独刷一个会出现「行在、包状态是旧的」
async function refreshAll() {
  await Promise.all([loadHistory(), loadArchive()])
}

// 加载失败汇总区的「重新加载」：把首屏 5 个请求整体重跑
async function reloadAll() {
  await Promise.all([
    loadCurrentVersion(),
    loadPending(),
    loadHistory(),
    loadArchive(),
    loadGeoip(),
  ])
}

async function deleteArchive(version) {
  // 当前运行版本不允许删除：删掉它就没有「正在跑的这一版」的升级包了（服务端同样会拒）
  if (version === currentVersion.value.server) {
    ElMessage.warning('当前运行版本的升级包不能删除')
    return
  }
  try {
    await ElMessageBox.confirm(
      `确认删除 v${version} 的升级包？删除后该版本将无法再回退回来，需要时只能重新上传该版本的升级包。`,
      '删除升级包',
      { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' }
    )
  } catch { return }
  deletingVersion.value = version
  try {
    const r = await http.post('/api/v1/system/upgrade/archive/delete', { version })
    archiveList.value = r.versions || []
    ElMessage.success(`已删除 v${version} 的升级包`)
  } catch (e) {
    ElMessage.error('删除失败：' + e.message)
  } finally {
    deletingVersion.value = ''
  }
}

// 切换/升级会重启当前 Server，HTTP 请求可能在响应返回前被主动断开，
// 代理也可能因上游断开返回 502。不能只凭请求结果判断升级失败：等待服务恢复并确认
// 实际运行版本后再刷新页面，避免服务未就绪时刷新到不可用页面。
async function waitForServerVersion(targetVersion, timeout = 40000, warnOnTimeout = true) {
  const deadline = Date.now() + timeout
  applyPhase.value = 'restarting'
  await new Promise((resolve) => setTimeout(resolve, 1200))
  while (Date.now() < deadline) {
    try {
      const v = await http.get('/api/v1/version')
      // 服务已恢复响应：进入"校验版本"阶段
      applyPhase.value = 'verifying'
      if (!targetVersion || v.server === targetVersion) {
        applyPhase.value = 'done'
        ElMessage.success('升级已完成，正在刷新页面…')
        setTimeout(() => window.location.reload(), 1500)
        return true
      }
    } catch (e) {
      // Server 正在重启，继续轮询。
      applyPhase.value = 'restarting'
    }
    await new Promise((resolve) => setTimeout(resolve, 1000))
  }
  applyPhase.value = 'timeout'
  if (warnOnTimeout) ElMessage.warning('服务恢复较慢，请稍后手动刷新页面查看新版本。')
  return false
}

async function confirmUpgradeAfterRestart(targetVersion, taskId) {
  if (await waitForServerVersion(targetVersion, 40000, false)) return

  try {
    const r = await http.get('/api/v1/system/upgrade/history')
    history.value = r.history || []
    const failed = history.value.find((entry) => entry.id === taskId && entry.action === 'apply')
    if (failed?.result === 'failed') {
      applyError.value = failed.detail || '升级未完成，请查看升级历史。'
      applyPhase.value = 'failed'
      ElMessage.error('升级失败：' + applyError.value)
      return
    }
    const current = await http.get('/api/v1/system/upgrade/current')
    if (current.current?.id === taskId && current.current.status === 'failed') {
      applyError.value = current.current.error || '升级未完成，请查看升级历史。'
      applyPhase.value = 'failed'
      ElMessage.error('升级失败：' + applyError.value)
      return
    }
  } catch (e) {
    // 服务尚未恢复，无法读取升级结果；不能仅凭 502 判断失败。
  }
  ElMessage.warning('暂时无法确认升级结果，请稍后刷新页面并查看当前版本和升级历史。')
}

function isRestartDisconnect(error) {
  return /Failed to fetch|网络错误|NetworkError|fetch|HTTP 502/i.test(error?.message || '')
}

// 该版本是否可回退：升级包仍保留（未被删除）、且不是当前运行版本。
function canRollbackTo(version) {
  if (!version || version === currentVersion.value.server) return false
  return archiveVersions.value.includes(version)
}

async function rollbackTo(version) {
  try {
    await ElMessageBox.confirm(
      `确认回退到版本 v${version}？将按该版本重新替换 Server / Web / Agent 并重启 monitor-server，期间服务短暂不可用。该操作同样会生成备份，可继续向后回退或再回退。`,
      '回退到指定版本',
      { type: 'warning', confirmButtonText: '回退', cancelButtonText: '取消' }
    )
  } catch { return }
  applyKind.value = 'rollback'
  applyPhase.value = 'submitted'
  rollingBack.value = true
  try {
    await http.post('/api/v1/system/upgrade/rollback-to', { version, operator: 'web' })
    ElMessage.success(`已回退到 v${version}，server 即将重启…`)
    startCooldown(15)
    await waitForServerVersion(version)
  } catch (e) {
    if (isRestartDisconnect(e)) {
      startCooldown(15)
      await waitForServerVersion(version)
      return
    }
    applyPhase.value = ''
    ElMessage.error('回退失败：' + e.message)
    await loadArchive()
  } finally {
    rollingBack.value = false
  }
}

function beforeUpload(file) {
  if (file.size > 500 * 1024 * 1024) {
    ElMessage.error('文件超过 500MB')
    return false
  }
  return true
}

async function onUpload(option) {
  const fd = new FormData()
  fd.append('file', option.file)
  lastUploadFile = option.file
  uploadError.value = null
  uploading.value = true
  uploadProgress.value = 0
  try {
    const task = await http.upload('/api/v1/system/upgrade/upload', fd, (pct) => {
      uploadProgress.value = pct
    })
    uploadProgress.value = 100
    pending.value = task
    await loadArchive()
    ElMessage.success('升级包解析成功：v' + task.version)
    option.onSuccess && option.onSuccess(task)
  } catch (e) {
    // 就地错误区：文件名 + 原因 + 重试
    uploadError.value = { name: option.file?.name || '升级包', reason: e.message }
    ElMessage.error('上传失败：' + e.message)
    option.onError && option.onError(e)
  } finally {
    uploading.value = false
  }
}

function retryUpload() {
  if (!lastUploadFile) return
  onUpload({ file: lastUploadFile, onSuccess: () => {}, onError: () => {} })
}

function clearUploadError() {
  uploadError.value = null
  lastUploadFile = null
}

function cancelPending() {
  // 仅清空本地预览：升级包已归档并保留在「升级包」列表中（不再自动清理），
  // 需要放弃它时到该列表里删除即可。
  pending.value = null
  applyError.value = ''
}

async function doApply() {
  try {
    await ElMessageBox.confirm(
      '升级 Server 会重启 monitor-server，期间 Web 端将短暂不可用。新 Agent 二进制将复制到自带 CDN，但不会自动推送到主机（请到「主机列表」手动点击「升级」）。是否继续？',
      '确认升级',
      { type: 'warning', confirmButtonText: '立即升级', cancelButtonText: '取消' }
    )
  } catch { return }
  applying.value = true
  applyError.value = ''
  applyKind.value = 'upgrade'
  applyPhase.value = 'submitted'
  const targetVersion = pending.value?.version || ''
  const taskId = pending.value?.id
  try {
    await http.post('/api/v1/system/upgrade/apply?operator=web', {})
    ElMessage.success('升级已提交，server 即将重启（约 5-15 秒）…')
    startCooldown(15)
    await waitForServerVersion(targetVersion)
  } catch (e) {
    if (isRestartDisconnect(e)) {
      startCooldown(15)
      await confirmUpgradeAfterRestart(targetVersion, taskId)
      return
    }
    applyPhase.value = 'failed'
    applyError.value = e.message
    ElMessage.error('升级失败：' + e.message)
    await loadPending()
    await loadHistory()
    await loadArchive()
  } finally {
    applying.value = false
  }
}

function fmtTime(s) {
  if (!s) return '-'
  return new Date(s).toLocaleString()
}
function fmtBuildTime(s) {
  if (!s || s === 'unknown') return '-'
  try {
    return new Date(s).toLocaleString()
  } catch {
    return s
  }
}
// 动作串来自服务端：apply / rollback_to（rollback 为历史值），合成行为 upload
function actionLabel(a) {
  if (a === 'apply') return '升级'
  if (a === 'rollback' || a === 'rollback_to') return '回退'
  if (a === 'upload') return '上传'
  return a || '-'
}
function actionTagType(a) {
  if (a === 'apply') return 'primary'
  if (a === 'rollback' || a === 'rollback_to') return 'warning'
  return 'info'
}
function resultLabel(r) {
  if (r === 'success') return '成功'
  if (r === 'failed') return '失败'
  if (r === 'pending') return '待应用'
  return r || '-'
}
function resultTagType(r) {
  if (r === 'success') return 'success'
  if (r === 'failed') return 'danger'
  return 'info'
}
// 删除按钮禁用时的原因（禁用态看不出原因就只能猜）
function deleteHint(row) {
  if (!row.pkg) return '该版本的升级包已删除'
  if (row.version === currentVersion.value.server) return '当前运行版本的升级包不能删除'
  return ''
}
function fmtSize(b) {
  if (!b) return '-'
  const u = ['B', 'KB', 'MB', 'GB']
  let i = 0
  while (b >= 1024 && i < u.length - 1) { b /= 1024; i++ }
  return b.toFixed(1) + ' ' + u[i]
}

// ===== IP 地理库（独立于系统升级：只替换归属地数据文件并热加载） =====
const geoip = ref({})
const geoipLoading = ref(false)
const geoipUploading = ref(false)
const geoipProgress = ref(0)
const geoipTestIP = ref('')
const geoipTesting = ref(false)
const geoipTestResult = ref('')
const geoipOpen = ref(false) // 与系统升级是两件事，默认折叠

async function loadGeoip() {
  geoipLoading.value = true
  try {
    geoip.value = await http.get('/api/v1/system/geoip')
    geoipLoadErr.value = ''
  } catch (e) {
    geoipLoadErr.value = e.message
  }
  geoipLoading.value = false
}

function beforeGeoipUpload(file) {
  if (!file.name.toLowerCase().endsWith('.xdb')) {
    ElMessage.error('仅支持 ip2region 的 .xdb 文件')
    return false
  }
  if (file.size > 64 * 1024 * 1024) {
    ElMessage.error('文件超过 64MB')
    return false
  }
  return true
}

async function onGeoipUpload(option) {
  const fd = new FormData()
  fd.append('file', option.file)
  geoipUploading.value = true
  geoipProgress.value = 0
  try {
    const r = await http.upload('/api/v1/system/geoip/upload', fd, (pct) => {
      geoipProgress.value = pct
    })
    geoipProgress.value = 100
    geoip.value = r
    ElMessage.success('IP 地理库已更新并生效')
    option.onSuccess && option.onSuccess(r)
  } catch (e) {
    ElMessage.error('更新失败：' + e.message)
    option.onError && option.onError(e)
  } finally {
    geoipUploading.value = false
  }
}

async function doGeoipReset() {
  try {
    await ElMessageBox.confirm('将删除已上传的地理库并恢复为程序内置库，是否继续？', '恢复内置库', {
      type: 'warning',
    })
  } catch { return }
  try {
    geoip.value = await http.post('/api/v1/system/geoip/reset', {})
    ElMessage.success('已恢复为程序内置库')
  } catch (e) {
    ElMessage.error('恢复失败：' + e.message)
  }
}

async function doGeoipTest() {
  const ip = (geoipTestIP.value || '').trim()
  if (!ip) {
    ElMessage.warning('请输入 IP')
    return
  }
  geoipTesting.value = true
  geoipTestResult.value = ''
  try {
    const r = await http.get('/api/v1/system/geoip/test?ip=' + encodeURIComponent(ip))
    geoipTestResult.value = [r.country, r.province, r.city].filter(Boolean).join(' / ') || r.region || '无结果'
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    geoipTesting.value = false
  }
}

function startCooldown(sec) {
  clearInterval(cooldownTimer)
  cooldown.value = sec
  cooldownTimer = setInterval(() => {
    cooldown.value -= 1
    if (cooldown.value <= 0) {
      clearInterval(cooldownTimer)
      cooldownTimer = null
    }
  }, 1000)
}

onMounted(async () => {
  await loadCurrentVersion()
  await loadPending()
  await loadHistory()
  await loadArchive()
  await loadGeoip()
})

onUnmounted(() => {
  if (cooldownTimer) clearInterval(cooldownTimer)
})
</script>

<style scoped>
.upgrade-view {
  display: flex;
  flex-direction: column;
  gap: var(--sp-4);
}

/* —— 页面级错误 / 警告 —— */
.page-error,
.page-warn {
  display: flex;
  align-items: center;
  gap: var(--sp-2);
  padding: 10px 12px;
  border-radius: var(--r-md);
  font-size: var(--fs-sm);
  line-height: 1.5;
}
.page-error {
  border: 1px solid var(--danger-bd);
  background: var(--danger-dim);
  color: var(--t1);
}
.page-error .pe-icon {
  color: var(--danger);
  flex-shrink: 0;
}
.page-error .pe-msg {
  flex: 1;
  min-width: 0;
  overflow-wrap: break-word;
}
.page-warn {
  border: 1px solid var(--warn-bd);
  background: var(--fill-1);
  color: var(--t2);
}
.page-warn .el-icon {
  color: var(--warn);
  flex-shrink: 0;
}
.page-warn span {
  flex: 1;
  min-width: 0;
}

/* —— 三步指示 —— */
.steps {
  display: flex;
  align-items: center;
  gap: var(--sp-3);
  list-style: none;
  flex-wrap: wrap;
}
.step {
  display: flex;
  align-items: center;
  gap: var(--sp-2);
  font-size: var(--fs-sm);
  color: var(--t3);
}
.step-dot {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  border-radius: var(--r-pill);
  border: 1px solid var(--bd-strong);
  font-size: var(--fs-xs);
  color: var(--t3);
  flex-shrink: 0;
  transition: background var(--dur-2) var(--ease), color var(--dur-2) var(--ease), border-color var(--dur-2) var(--ease);
}
.step.on .step-dot {
  border-color: var(--accent);
  color: var(--accent);
  background: var(--accent-dim);
}
.step.on .step-text {
  color: var(--t1);
  font-weight: 600;
}
.step.done .step-dot {
  border-color: var(--accent);
  background: var(--accent);
  color: var(--s1);
}
.step.done .step-text {
  color: var(--t2);
}
.step.failed .step-dot {
  border-color: var(--danger);
  background: var(--danger-dim);
  color: var(--danger);
}
.step-hint {
  margin-top: var(--sp-3);
  font-size: var(--fs-sm);
  color: var(--t2);
  line-height: 1.6;
}
.step-hint.danger {
  color: var(--danger);
}
.step-hint.warn {
  color: var(--warn);
}
.step-actions {
  margin-top: var(--sp-3);
  display: flex;
  justify-content: flex-end;
}

/* —— 当前版本 —— */
.ver-grid {
  display: grid;
  /* 模板只有「当前版本 / 构建时间」两项，写 3 列会让右侧空出 1/3 列宽 */
  grid-template-columns: repeat(2, 1fr);
  gap: var(--sp-4);
}
.ver-item {
  display: flex;
  flex-direction: column;
  gap: var(--sp-1);
}
.ver-label {
  font-size: var(--fs-sm);
  color: var(--t3);
}
.ver-val {
  font-family: var(--mono);
  font-size: var(--fs-base);
  color: var(--t1);
}
.load-hint {
  margin-top: var(--sp-3);
  font-size: var(--fs-xs);
  color: var(--warn);
  line-height: 1.6;
}

/* —— 上传 —— */
.upload-area :deep(.el-upload-dragger) {
  background: var(--fill-1);
  border: 1px dashed var(--bd-strong);
}
.upload-area-sp {
  margin-top: var(--sp-3);
}
.upload-progress {
  margin-top: var(--sp-3);
}
.upload-progress-text {
  font-size: var(--fs-sm);
  color: var(--t3);
  margin-top: var(--sp-1);
  text-align: center;
}
.upload-error {
  display: flex;
  align-items: center;
  gap: var(--sp-2);
  margin-top: var(--sp-3);
  padding: 10px 12px;
  border: 1px solid var(--danger-bd);
  background: var(--danger-dim);
  border-radius: var(--r-md);
}
.upload-error .ue-icon {
  color: var(--danger);
  flex-shrink: 0;
}
.upload-error .ue-body {
  flex: 1;
  min-width: 0;
}
.upload-error .ue-name {
  font-size: var(--fs-sm);
  color: var(--t1);
  font-family: var(--mono);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.upload-error .ue-reason {
  font-size: var(--fs-xs);
  color: var(--t2);
  margin-top: 2px;
  overflow-wrap: break-word;
}

/* —— 待升级 —— */
.pending-grid {
  display: flex;
  flex-direction: column;
  gap: var(--sp-2);
}
.pending-row {
  display: flex;
  align-items: center;
  gap: var(--sp-3);
  font-size: var(--fs-sm);
}
.r-label {
  color: var(--t3);
  width: 80px;
  flex-shrink: 0;
}
.r-val.highlight {
  font-family: var(--mono);
  font-size: var(--fs-lg);
  color: var(--accent);
  font-weight: 600;
}
.r-val.mono {
  font-family: var(--mono);
  color: var(--t1);
}
.r-val.small {
  font-size: var(--fs-sm);
}
.tag-gap {
  margin-left: 6px;
}
.action-row {
  display: flex;
  justify-content: flex-end;
  gap: var(--sp-2);
  margin-top: var(--sp-4);
}
.card-alert {
  margin-top: var(--sp-3);
}

/* —— 升级历史 —— */
.pkg-total {
  font-size: var(--fs-xs);
  color: var(--t3);
  margin-right: var(--sp-2);
}
/* 版本列：版本号在前、「当前运行」标记在后，同排不换行。
   此前标记在前、列宽 150，长版本号（如 v1.30.35）会被标记挤到看不见。 */
.ver-cell {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  white-space: nowrap;
}
.ver-cell .mono {
  font-family: var(--mono);
  color: var(--accent);
}
/* 操作列：两个按钮靠右一行排开，不换行 */
:deep(.op-col .cell) {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 6px;
  white-space: nowrap;
}
:deep(.op-col .cell .el-button + .el-button) { margin-left: 0; }
.hist-pager {
  display: flex;
  justify-content: flex-end;
  margin-top: var(--sp-3);
}

/* —— 危险区 —— */
.danger-zone {
  border: 1px solid var(--danger-bd);
  border-radius: var(--r-md);
  background: var(--fill-1);
  padding: var(--sp-3);
}
.hist-danger {
  margin-top: var(--sp-4);
}
.geo-danger {
  margin-top: var(--sp-4);
}
.dz-title {
  font-size: var(--fs-sm);
  font-weight: 600;
  color: var(--danger);
  margin-bottom: var(--sp-1);
}
.dz-desc {
  font-size: var(--fs-xs);
  color: var(--t3);
  line-height: 1.6;
  margin-bottom: var(--sp-3);
}

/* —— IP 地理库 —— */
.geo-test-row {
  display: flex;
  align-items: center;
  gap: var(--sp-2);
  margin-top: var(--sp-3);
  flex-wrap: wrap;
}
.geo-test-input {
  max-width: 320px;
}
.geo-test-result {
  font-size: var(--fs-sm);
  color: var(--accent);
  font-family: var(--mono);
}
.geo-samples {
  margin-top: var(--sp-3);
}
.tip-2 {
  margin-top: var(--sp-1);
  color: var(--t3);
}

@media (max-width: 1024px) {
  .ver-grid {
    grid-template-columns: 1fr;
  }
}
</style>
