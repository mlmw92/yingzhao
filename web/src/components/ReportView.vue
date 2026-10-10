<template>
  <div class="report-view">
    <PageHeader
      title="巡检报告"
      desc="日报 / 周报 / 月报，含主机资源趋势图、中间件监控指标与健康巡检发现"
    />

    <SectionCard title="生成报告">
      <div class="generate-row">
        <el-select v-model="reportType" style="width: 200px">
          <el-option label="日报" value="daily" />
          <el-option label="周报" value="weekly" />
          <el-option label="月报" value="monthly" />
        </el-select>
        <el-button type="primary" @click="generate" :loading="generating">生成报告</el-button>
      </div>
      <div class="generate-row sections-row">
        <span class="schedule-label">报告章节</span>
        <el-checkbox-group v-model="sections" size="small">
          <el-checkbox v-for="opt in sectionOptions" :key="opt.value" :value="opt.value">{{ opt.label }}</el-checkbox>
        </el-checkbox-group>
      </div>
      <div class="schedule-hint">
        报告是**当前状态快照**：时间窗口由报告类型固定（日报 / 周报 / 月报），不支持自定义时间与节点范围；
        覆盖范围（能看哪些节点与资产）由服务端按你的权限折算，不受此处影响；
        缺少某域权限的章节会被跳过，并在报告头部标注原因。
      </div>
      <el-alert
        class="tip"
        type="info"
        :closable="false"
        show-icon
        title="报告内容"
        description="报告包含资源趋势柱状图/折线图、各主机健康评分、中间件连接数/响应时间/内存使用率/命中率明细，以及按严重程度排序的巡检发现（问题描述 / 影响范围 / 修复建议）。"
      />
    </SectionCard>

    <!-- 周期化生成：默认关闭，打开后按间隔自动生成一份（全景表 11-4）。
         与上方「生成报告」的分工：那个是"现在生成一份"，这里是"以后自动生成"。 -->
    <SectionCard title="周期化生成">
      <div class="generate-row">
        <el-switch v-model="schedule.enabled" @change="saveSchedule" />
        <span class="schedule-label">每</span>
        <el-input-number
          v-model="schedule.intervalHours"
          :min="1"
          :max="720"
          size="small"
          controls-position="right"
          @change="saveSchedule"
        />
        <span class="schedule-label">小时自动生成</span>
        <el-select v-model="schedule.type" style="width: 140px" size="small" @change="saveSchedule">
          <el-option label="日报" value="daily" />
          <el-option label="周报" value="weekly" />
          <el-option label="月报" value="monthly" />
        </el-select>
        <el-button size="small" :loading="scheduleRunning" @click="runScheduleNow">立即生成一次</el-button>
      </div>
      <div class="schedule-hint">
        <template v-if="scheduleError">{{ scheduleError }}</template>
        <template v-else-if="schedule.lastRunAt">
          上次运行 {{ formatTime(schedule.lastRunAt) }}
          <span v-if="schedule.lastReportId">· 报告 {{ schedule.lastReportId }}</span>
          <span v-if="schedule.lastError" class="schedule-fail">· 失败：{{ schedule.lastError }}</span>
          <span v-if="schedule.nextAt && schedule.enabled">· 下次约 {{ formatTime(schedule.nextAt) }}</span>
        </template>
        <template v-else>尚未运行过。关闭时不影响上方的手动生成。</template>
      </div>
      <el-alert
        v-if="schedule.lastError"
        class="tip"
        type="warning"
        :closable="false"
        show-icon
        title="上次自动生成失败"
        :description="schedule.lastError"
      />
    </SectionCard>

    <SectionCard title="历史报告">
      <el-alert
        v-if="historyError"
        type="error"
        :closable="false"
        show-icon
        class="history-error"
        title="历史报告加载失败"
        :description="historyError"
      />
      <!-- 加载中先用骨架占位：此前 history 初值就是 []，
           首屏会先渲染「暂无历史报告」，请求回来再整块翻转成表格。 -->
      <div v-if="historyLoading" class="sk-list">
        <div v-for="i in 4" :key="i" class="sk sk-row"></div>
      </div>
      <template v-else-if="!historyError">
        <div v-if="history.length" class="filter-bar">
          <el-input v-model="historyKw" size="small" class="w-m" clearable placeholder="搜索周期 / 类型 / 范围" />
        </div>
        <el-table v-if="filteredHistory.length > 0" :data="pagedHistory" style="width: 100%">
          <el-table-column label="类型" width="80">
            <template #default="{ row }">
              <StatusPill v-if="row.type" size="sm" :tone="typeTone(row.type)" :text="typeLabel(row.type)" />
              <span v-else class="muted">—</span>
            </template>
          </el-table-column>
          <el-table-column label="统计周期" min-width="150">
            <template #default="{ row }">{{ row.period || '—' }}</template>
          </el-table-column>
          <el-table-column label="生成时间" width="180">
            <template #default="{ row }">{{ formatTime(row.generatedAt) }}</template>
          </el-table-column>
          <el-table-column label="范围" min-width="180">
            <template #default="{ row }">
              <span>{{ row.scope || '—' }}</span>
              <el-tag
                v-if="row.skipped && row.skipped.length"
                size="small"
                type="warning"
                class="skip-tag"
              >已跳过 {{ row.skipped.length }} 章</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="200" fixed="right">
            <template #default="{ row }">
              <el-button link type="primary" @click="preview(row.id)">预览</el-button>
              <el-button link @click="openNewTab(row.id)">新标签页</el-button>
              <el-button link type="danger" @click="deleteReport(row)">删除</el-button>
            </template>
          </el-table-column>
        </el-table>
        <EmptyState
          v-else
          :title="history.length ? '没有匹配的报告' : '暂无历史报告'"
          :hints="history.length ? ['换个关键词，或清空搜索框'] : ['在上方选择日报 / 周报 / 月报，点「生成报告」创建第一份']"
          :action-text="history.length ? '' : '生成报告'"
          @action="generate"
        />
        <div v-if="filteredHistory.length > historyPageSize" class="pager">
          <el-pagination
            layout="prev, pager, next, total"
            :total="filteredHistory.length"
            :page-size="historyPageSize"
            :current-page="historyPage"
            @current-change="(p) => (historyPage = p)"
          />
        </div>
      </template>
    </SectionCard>

    <!-- 预览改用右侧抽屉：此前预览是页尾一整块卡片，历史长时要点「预览」再往下滚很远，
         关掉预览也没有入口。抽屉常驻在视口右侧，关闭即回到列表原位。 -->
    <el-drawer v-model="previewOpen" title="报告预览" size="min(880px, 92vw)" direction="rtl">
      <template #header>
        <div class="drawer-head">
          <span class="drawer-title">报告预览</span>
          <div class="drawer-actions">
            <el-button size="small" @click="openNewTabById(currentId)">新标签页打开</el-button>
            <!-- 「打印」在报告这种交付物上等于「存 PDF」，但用户不一定知道——
                 按钮上直接写出来，比让人猜 Ctrl+P 之后能干什么强 -->
            <el-button
              size="small"
              title="在弹出窗口里调起浏览器打印，目标选「另存为 PDF」即可。报告自带 A4 纵向打印样式，无需另配"
              @click="printReport(currentId)"
            >打印 / 存为 PDF</el-button>
            <el-button size="small" type="primary" @click="download(currentId)">下载 HTML</el-button>
          </div>
        </div>
      </template>
      <div class="report-frame">
        <div v-if="previewLoading" class="frame-state">
          <span class="frame-spin"></span>
          <span>正在加载报告…</span>
        </div>
        <div v-if="previewError" class="frame-state error">
          <span>{{ previewError }}</span>
          <el-button size="small" @click="openNewTabById(currentId)">新标签页打开</el-button>
        </div>
        <iframe
          v-show="!previewError"
          :src="previewUrl"
          class="report-iframe"
          title="巡检报告预览"
          @load="onIframeLoad"
          @error="onIframeError"
        ></iframe>
      </div>
    </el-drawer>
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

const reportType = ref('daily')
const generating = ref(false)
const history = ref([])
const historyError = ref('')
const historyLoading = ref(false)
const historyKw = ref('')
const historyPage = ref(1)
const historyPageSize = 10
const previewUrl = ref('')
const currentId = ref('')
const previewOpen = ref(false)
const previewLoading = ref(false)
const previewError = ref('')
let previewTimer = 0

// 报告章节（设计件 2026-10-08-reporting-views-design.md）。
// 空数组在后端表示"全选"，因此初始就是全选；手动生成与周期化生成共用这一组选择。
const sectionOptions = [
  { value: 'hosts', label: '主机巡检明细' },
  { value: 'middleware', label: '中间件监控明细' },
  { value: 'security', label: '安全中心巡检' },
  { value: 'analysis', label: '智能分析与预测' },
  { value: 'assets', label: '资产与配置' },
  { value: 'container', label: '容器与工作负载' },
  { value: 'logs', label: '日志' },
]
const sections = ref(sectionOptions.map((o) => o.value))

const filteredHistory = computed(() => {
  const k = historyKw.value.trim().toLowerCase()
  if (!k) return history.value
  return history.value.filter((r) =>
    [r.id, r.type, typeLabel(r.type), r.period, r.scope]
      .some((v) => String(v || '').toLowerCase().includes(k))
  )
})
const pagedHistory = computed(() => {
  const start = (historyPage.value - 1) * historyPageSize
  return filteredHistory.value.slice(start, start + historyPageSize)
})

async function loadHistory() {
  historyLoading.value = true
  try {
    const data = await http.get('/api/v1/report/history')
    history.value = data.reports || []
    historyError.value = ''
    if (historyPage.value > 1 && (historyPage.value - 1) * historyPageSize >= filteredHistory.value.length) {
      historyPage.value = 1
    }
  } catch (e) {
    console.error(e)
    historyError.value = e.message || '网络异常，请稍后重试'
  } finally {
    historyLoading.value = false
  }
}

async function generate() {
  generating.value = true
  try {
    const data = await http.post('/api/v1/report/generate', { type: reportType.value, sections: sections.value })
    if (data && data.id) {
      preview(data.id)
      await loadHistory()
      ElMessage.success('报告已生成')
    }
  } catch (e) {
    console.error(e)
    ElMessage.error('生成报告失败：' + (e.message || '请稍后重试'))
  } finally { generating.value = false }
}

async function deleteReport(row) {
  try {
    await ElMessageBox.confirm(`确认删除报告 ${row.id}？删除后不可恢复。`, '删除报告', {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  try {
    await http.del(`/api/v1/report/history/${encodeURIComponent(row.id)}`)
    ElMessage.success('已删除')
    await loadHistory()
  } catch (e) {
    ElMessage.error(e.message || '删除失败')
  }
}

/* ================= 周期化生成（全景表 11-4） ================= */

// 调度配置与运行状态。lastRunAt/lastReportId/lastError 由服务端维护（界面上只读）。
const schedule = ref({ enabled: false, type: 'weekly', intervalHours: 24, lastRunAt: 0, lastReportId: '', lastError: '', nextAt: 0 })
const scheduleError = ref('')
const scheduleRunning = ref(false)

function applySchedule(data) {
  const cfg = (data && data.config) || {}
  const last = (data && data.last) || {}
  schedule.value = {
    enabled: !!cfg.enabled,
    type: cfg.type || 'weekly',
    intervalHours: cfg.intervalHours || 24,
    lastRunAt: last.at || cfg.lastRunAt || 0,
    lastReportId: last.reportId || cfg.lastReportId || '',
    lastError: last.error || cfg.lastError || '',
    nextAt: (data && data.nextAt) || 0,
  }
  // 章节选择回读服务端真实值；空数组表示「全选」，此时保持界面全选（不要显示成全不选）。
  if (Array.isArray(cfg.sections) && cfg.sections.length) sections.value = cfg.sections
}

async function loadSchedule() {
  try {
    applySchedule(await http.get('/api/v1/report/schedule'))
    scheduleError.value = ''
  } catch (e) {
    // 该能力未启用（503）时如实说明，而不是让开关看起来"关着"——
    // 那会让人以为打开就好了，实际上后端根本没有调度器。
    scheduleError.value = e.message || '报告调度不可用'
  }
}

async function saveSchedule() {
  try {
    applySchedule(
      await http.put('/api/v1/report/schedule', {
        enabled: schedule.value.enabled,
        type: schedule.value.type,
        intervalHours: schedule.value.intervalHours,
        sections: sections.value,
      })
    )
    ElMessage.success('已保存（热生效）')
  } catch (e) {
    ElMessage.error(e.message || '保存失败')
    // 回读服务端真实状态：否则界面会停留在"我改成了这样"，而后端并没有接受
    await loadSchedule()
  }
}

async function runScheduleNow() {
  scheduleRunning.value = true
  try {
    const run = await http.post('/api/v1/report/schedule/run', {})
    await Promise.all([loadSchedule(), loadHistory()])
    ElMessage.success('已生成一份报告')
    if (run && run.reportId) preview(run.reportId)
  } catch (e) {
    ElMessage.error(e.message || '生成失败')
    await loadSchedule()
  } finally {
    scheduleRunning.value = false
  }
}

function clearPreviewTimer() {
  if (previewTimer) {
    clearTimeout(previewTimer)
    previewTimer = 0
  }
}

function preview(id) {
  if (!id) return
  currentId.value = id
  previewUrl.value = `/api/v1/report/download?id=${encodeURIComponent(id)}`
  previewError.value = ''
  previewLoading.value = true
  previewOpen.value = true
  // iframe 的 load 事件在多数浏览器里连 404 页也会触发，光靠 load 判断不了"没加载上"。
  // 用一个兜底超时：迟迟没有 load 就给出失败提示 + 新标签页入口。
  clearPreviewTimer()
  previewTimer = setTimeout(() => {
    if (previewLoading.value) {
      previewLoading.value = false
      previewError.value = '报告预览加载超时，可点「新标签页打开」查看'
    }
  }, 8000)
}

function onIframeLoad() {
  clearPreviewTimer()
  previewLoading.value = false
}

function onIframeError() {
  clearPreviewTimer()
  previewLoading.value = false
  previewError.value = '报告预览加载失败，可点「新标签页打开」查看'
}

function openNewTab(id) { if (id) window.open(`/api/v1/report/download?id=${encodeURIComponent(id)}`, '_blank') }
function openNewTabById(id) { if (id) openNewTab(id) }

// 报告本身是独立 HTML（自带 @media print），必须在新窗口里打，
// 直接 window.print() 会把外层深色控制台一起印出来。
function printReport(id) {
  if (!id) return
  const w = window.open(`/api/v1/report/download?id=${encodeURIComponent(id)}`, '_blank')
  if (!w) {
    ElMessage.warning('浏览器拦截了弹窗，请允许弹出窗口后重试，或先点「新标签页打开」再 Ctrl+P')
    return
  }
  w.addEventListener('load', () => { w.focus(); w.print() }, { once: true })
}
function download(id) {
  if (!id) return
  const a = document.createElement('a')
  a.href = `/api/v1/report/download?id=${encodeURIComponent(id)}`
  a.download = `${id}.html`
  a.target = '_blank'
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
}

function typeLabel(t) { return { daily: '日报', weekly: '周报', monthly: '月报' }[t] || t }
function typeTone(t) { return { daily: 'info', weekly: 'ok', monthly: 'warn' }[t] || 'muted' }
function formatTime(ms) { return new Date(ms).toLocaleString('zh-CN') }

onMounted(() => {
  loadHistory()
  loadSchedule()
})
</script>

<style scoped>
.report-view { display: flex; flex-direction: column; gap: 12px; }
.schedule-label { color: var(--t3); font-size: var(--fs-sm); }
.schedule-hint { margin-top: 8px; color: var(--t3); font-size: var(--fs-sm); }
.schedule-fail { color: var(--danger); }
.sections-row {
  flex-wrap: wrap;
  gap: 6px;
}

.generate-row { display: flex; gap: 12px; align-items: center; flex-wrap: wrap; }
.tip { margin-top: 14px; }
.history-error { margin-bottom: 12px; }
.filter-bar { margin-bottom: 10px; }
.skip-tag { margin-left: 6px; }
.pager { display: flex; justify-content: flex-end; margin-top: 10px; }
/* 骨架占位（.sk / .sk-row 是全局类）：与表格行的纵向节奏对齐，避免加载完成时高度突变 */
.sk-list { padding: 10px 0; }
.drawer-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; flex-wrap: wrap; width: 100%; }
.drawer-title { font-size: var(--fs-base); font-weight: 600; color: var(--t1); }
.drawer-actions { display: flex; gap: 8px; flex-wrap: wrap; }
.report-frame { position: relative; height: 100%; }
.frame-state {
  position: absolute; inset: 0; display: flex; align-items: center; justify-content: center;
  gap: 8px; font-size: var(--fs-sm); color: var(--t3); background: var(--s1); border-radius: var(--r-md);
}
.frame-state.error { flex-direction: column; color: var(--danger); }
.frame-spin {
  width: 14px; height: 14px; border: 2px solid var(--bd); border-top-color: var(--accent);
  border-radius: 50%; animation: frame-spin 0.7s linear infinite;
}
@keyframes frame-spin { to { transform: rotate(360deg); } }
.report-iframe {
  width: 100%;
  height: 100%;
  min-height: 420px;
  border: 1px solid var(--bd);
  border-radius: var(--r-md);
  background: #fff;
}
@media (max-width: 768px) {
  .generate-row { align-items: flex-start; }
  .generate-row :deep(.el-select) { width: 100% !important; }
}
</style>
