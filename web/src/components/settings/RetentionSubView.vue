<template>
  <div class="retention-view">
    <div class="toolbar">
      <span class="field-hint">
        可清理的本地数据只有两类：<b>告警处置记录</b>与<b>巡检报告</b>；审计事件与安全事件由内置上限约束，因此不会无界增长。
        指标数据的保留由时序库自身控制，这里只做只读呈现。
      </span>
      <div class="toolbar-actions">
        <el-button size="small" :loading="loading" @click="load">刷新</el-button>
      </div>
    </div>

    <el-alert v-if="error" :title="error" type="warning" :closable="false" show-icon class="err" />

    <!-- 策略 -->
    <div class="section">
      <h3 class="section-title">保留策略</h3>
      <div class="form-row">
        <div class="form-item">
          <span class="label">自动清理</span>
          <el-switch v-model="form.enabled" size="small" active-text="启用" inactive-text="关闭" />
        </div>
        <div class="form-item">
          <span class="label">告警处置记录保留</span>
          <el-input-number v-model="form.acksDays" :min="0" :max="3650" size="small" controls-position="right" />
          <span class="field-hint inline">天</span>
        </div>
        <div class="form-item">
          <span class="label">巡检报告保留</span>
          <el-input-number v-model="form.reportsDays" :min="0" :max="3650" size="small" controls-position="right" />
          <span class="field-hint inline">天</span>
        </div>
        <div class="form-item">
          <span class="label">集中日志保留</span>
          <el-input-number v-model="form.logsDays" :min="0" :max="3650" size="small" controls-position="right" />
          <span v-if="externalLogs" class="field-hint inline">
            天（当前日志走外部后端 {{ status.logsBackend }}，该值不生效——保留期由后端自己配置）
          </span>
          <span v-else class="field-hint inline">天（按日期分片整天删除，当天不删）</span>
        </div>
        <div class="form-item">
          <span class="label">清理周期</span>
          <el-input-number v-model="form.intervalHours" :min="1" :max="720" size="small" controls-position="right" />
          <span class="field-hint inline">小时</span>
        </div>
        <el-button type="primary" size="small" :loading="saving" @click="save">保存</el-button>
      </div>
      <div class="field-hint">
        <b>保留天数填 0 表示不清理该类数据（永久保留）</b>，不是"保留 0 天"。
        「自动清理」只控制周期性清理；「立即清理」始终按上面的保留天数执行。待处置（未认领）的告警记录不会被清理。
      </div>
    </div>

    <!-- 危险操作：立即清理是破坏性动作，与"保存"分开、单独隔离 -->
    <div class="section danger-zone">
      <h3 class="section-title">危险操作</h3>
      <div class="danger-row">
        <div class="danger-desc">
          <b>立即清理</b>
          <span class="field-hint inline">按上方保留天数立即删除过期数据，不可撤销；「自动清理」关闭时也会执行。</span>
        </div>
        <el-button size="small" type="danger" plain :loading="cleaning" @click="cleanup">立即清理</el-button>
      </div>
    </div>

    <!-- 上次清理结果：有失败分项时不得显示为成功 -->
    <el-alert
      v-if="lastResult"
      :type="lastResult.errors?.length ? 'warning' : 'success'"
      :closable="false"
      show-icon
      class="err"
    >
      <template #title>
        上次清理：{{ fmt(lastResult.at) }} · 告警记录 {{ lastResult.acksRemoved }} 条 · 报告文件
        {{ lastResult.reportFilesRemoved }} 个（历史 {{ lastResult.reportHistoryRemoved }} 条）· 日志
        {{ lastResult.logFilesRemoved ?? 0 }} 个文件（分片 {{ lastResult.logDirsRemoved ?? 0 }}）· 释放
        {{ fileSize(lastResult.freedBytes) }}
        <span v-if="lastResult.skipped" class="field-hint inline">（{{ lastResult.skipped }}）</span>
        <span v-if="lastResult.errors?.length" class="field-hint inline">（失败：{{ lastResult.errors.join('；') }}）</span>
      </template>
    </el-alert>

    <!-- 现状 -->
    <div class="section">
      <h3 class="section-title">当前占用</h3>
      <div v-loading="loading" class="desc-wrap">
        <el-descriptions :column="2" border size="small">
          <el-descriptions-item label="告警处置记录">
            {{ status.acks?.total ?? '—' }} 条（已处置 {{ status.acks?.handled ?? '—' }}）
            <span class="field-hint inline">最早 {{ fmt(status.acks?.oldest) }}</span>
          </el-descriptions-item>
          <el-descriptions-item label="巡检报告">
            {{ status.reports?.files ?? '—' }} 个文件 · {{ fileSize(status.reports?.bytes) }}
          </el-descriptions-item>
          <el-descriptions-item label="集中日志">
            <!-- 外部后端（如 VictoriaLogs）的占用与保留由该后端自己管，这里拿不到文件口径。
                 必须说清楚：否则「0 个文件 0 B」看起来像日志功能坏了。 -->
            <template v-if="externalLogs">
              {{ status.logsBackend }} 外部后端
              <span class="field-hint inline">保留与容量由该后端负责（如 VictoriaLogs 的 -retentionPeriod），平台不统计也不清理</span>
            </template>
            <template v-else>
              {{ status.logs?.files ?? '—' }} 个文件 · {{ fileSize(status.logs?.bytes) }}
              <span class="field-hint inline">来源 {{ status.logs?.sources ?? 0 }} · 日期分片 {{ status.logs?.days ?? 0 }}</span>
            </template>
          </el-descriptions-item>
          <el-descriptions-item label="审计事件">
            {{ status.audit?.count ?? '—' }} / {{ status.audit?.cap ?? '—' }} 条
            <span class="field-hint inline">超出上限自动淘汰最旧记录</span>
          </el-descriptions-item>
          <el-descriptions-item label="安全事件">
            {{ status.security?.count ?? '—' }} / {{ status.security?.cap ?? '—' }} 条
            <span class="field-hint inline">超出上限自动淘汰最旧记录</span>
          </el-descriptions-item>
        </el-descriptions>
      </div>
    </div>

    <!-- 时序库保留（只读） -->
    <div class="section">
      <h3 class="section-title">指标数据保留（时序库侧）</h3>
      <div v-loading="loading" class="desc-wrap">
        <el-descriptions :column="1" border size="small">
          <el-descriptions-item label="时序库地址">{{ status.tsdb?.addr || '未配置' }}</el-descriptions-item>
          <el-descriptions-item label="保留参数">
            <el-tag v-if="status.tsdb?.setting" type="success" size="small" effect="plain">{{ status.tsdb.setting }}</el-tag>
            <span v-else class="field-hint">{{ status.tsdb?.error || '未读取到' }}</span>
          </el-descriptions-item>
        </el-descriptions>
      </div>
      <div class="field-hint">
        指标保留期由时序库自身的启动参数决定（VictoriaMetrics 为 <code v-pre>-retentionPeriod</code>），
        Server 无法在运行期修改，需在时序库侧调整后重启。此处仅呈现当前值。
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import http from '../../api/http'

// 日志是否走外部后端（后端标识不是 local 就是外部）：决定"集中日志"那一行
// 展示文件占用还是"由外部后端负责"，以及保留天数是否生效。
const externalLogs = computed(() => {
  const backend = status.value?.logsBackend
  return !!backend && backend !== 'local'
})

const loading = ref(false)
const saving = ref(false)
const cleaning = ref(false)
const error = ref('')
const status = ref({})
const lastResult = ref(null)
const form = ref({ enabled: true, acksDays: 90, reportsDays: 180, logsDays: 7, intervalHours: 24 })
const baseline = ref(null)

const dirty = computed(() => {
  const b = baseline.value
  if (!b) return false
  return (
    form.value.enabled !== b.enabled ||
    form.value.acksDays !== b.acksDays ||
    form.value.reportsDays !== b.reportsDays ||
    form.value.logsDays !== b.logsDays ||
    form.value.intervalHours !== b.intervalHours
  )
})

function fmt(ms) {
  if (!ms || ms <= 0) return '—'
  return new Date(ms).toLocaleString('zh-CN', { hour12: false })
}

function fileSize(bytes) {
  if (bytes === undefined || bytes === null) return '—'
  const v = Number(bytes)
  if (v < 1024) return v + ' B'
  if (v < 1024 * 1024) return (v / 1024).toFixed(1) + ' KB'
  if (v < 1024 * 1024 * 1024) return (v / 1024 / 1024).toFixed(1) + ' MB'
  return (v / 1024 / 1024 / 1024).toFixed(2) + ' GB'
}

function apply(statusBody) {
  status.value = statusBody || {}
  const cfg = statusBody?.config
  if (cfg) {
    form.value = {
      enabled: !!cfg.enabled,
      acksDays: cfg.acksDays ?? 0,
      reportsDays: cfg.reportsDays ?? 0,
      logsDays: cfg.logsDays ?? 0,
      intervalHours: cfg.intervalHours ?? 24,
    }
    baseline.value = { ...form.value }
  }
  if (statusBody?.lastCleanup) lastResult.value = statusBody.lastCleanup
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    apply(await http.getRetention())
  } catch (e) {
    error.value = '读取保留策略失败：' + (e.message || '请稍后重试')
  } finally {
    loading.value = false
  }
}

async function save() {
  saving.value = true
  try {
    apply(await http.saveRetention(form.value))
    ElMessage.success('保留策略已保存（热生效）')
  } catch (e) {
    ElMessage.error(e.message || '保存失败')
  } finally {
    saving.value = false
  }
}

async function cleanup() {
  try {
    await ElMessageBox.confirm(
      `将按当前保留天数立即清理过期数据（告警处置记录 ${form.value.acksDays} 天、巡检报告 ${form.value.reportsDays} 天、集中日志 ${form.value.logsDays} 天）。此操作不可撤销。`,
      '立即清理',
      { type: 'warning', confirmButtonText: '立即清理', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  cleaning.value = true
  try {
    const res = await http.cleanupRetention()
    lastResult.value = res
    if (res.errors?.length) {
      ElMessage.warning('清理未全部完成：' + res.errors.join('；'))
    } else if (res.skipped) {
      ElMessage.warning('未执行清理：' + res.skipped)
    } else {
      ElMessage.success(`已清理：告警记录 ${res.acksRemoved} 条、报告 ${res.reportFilesRemoved} 个（释放 ${fileSize(res.freedBytes)}）`)
    }
    await load()
  } catch (e) {
    ElMessage.error(e.message || '清理失败')
  } finally {
    cleaning.value = false
  }
}

function reset() {
  if (baseline.value) form.value = { ...baseline.value }
}

onMounted(load)

// dirty 契约：供 SettingsView 容器渲染吸底「未保存改动」条
defineExpose({ dirty, save, reset })
</script>

<style scoped>
.retention-view {
  display: flex;
  flex-direction: column;
  gap: var(--sp-4);
}
.toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--sp-3);
  flex-wrap: wrap;
}
.toolbar-actions {
  display: flex;
  align-items: center;
  gap: 10px;
}
.section {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.section-title {
  font-size: var(--fs-sm);
  font-weight: 600;
  margin: 0;
}
.form-row {
  display: flex;
  align-items: center;
  gap: 18px;
  flex-wrap: wrap;
}
.form-item {
  display: flex;
  align-items: center;
  gap: 6px;
}
.label {
  font-size: var(--fs-sm);
}
.field-hint {
  font-size: var(--fs-xs);
  color: var(--t2);
  line-height: 1.6;
}
.field-hint.inline {
  margin-left: 4px;
}
.desc-wrap {
  min-height: 60px;
}
/* 危险区：与普通表单视觉隔离 */
.danger-zone {
  border: 1px solid var(--danger-bd);
  border-radius: var(--r-md);
  padding: var(--sp-3);
  background: var(--fill-1);
}
.danger-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--sp-3);
  flex-wrap: wrap;
}
.danger-desc {
  min-width: 0;
}
.err {
  margin-bottom: 2px;
}
code {
  font-family: var(--mono);
  font-size: var(--fs-xs);
}
</style>
