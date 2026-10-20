<template>
  <el-card class="settings-card" shadow="never">
    <template #header>
      <div class="card-head">
        <div>
          <span class="title">AI 助手</span>
          <span class="sub">接入大模型辅助运维（只给建议，不改动任何状态）</span>
        </div>
        <div class="head-actions">
          <el-button :loading="testing" :disabled="saving" @click="test">测试连接</el-button>
          <el-button type="primary" :loading="saving" @click="save">保存</el-button>
        </div>
      </div>
    </template>

    <!-- 这一段必须显眼：这是平台第一条「数据会离开部署环境」的能力。
         把它藏在字段说明里，等于让开启的人不知道自己批准了什么。 -->
    <el-alert type="warning" :closable="false" show-icon class="notice">
      <template #title>开启前请确认数据去向</template>
      <div class="notice-body">
        启用后，平台会把运维数据（指标、告警、资产信息）发送到<b>下面配置的端点</b>。
        日志正文、属性值等更敏感的内容默认<b>不发送</b>，需要单独打开。
        <br />
        <b>不启用时平台完全按本地分析工作</b>——既有的智能分析（基线、容量预测、风险评分）不受任何影响。
      </div>
    </el-alert>

    <!-- 「测试连接」测的是服务端已保存的配置，不是当前表单——必须说清楚，否则改完端点直接点测试会得到旧结果 -->
    <div class="test-hint">
      <span class="field-hint">
        「测试连接」测的是<b>已保存</b>的配置；若刚改了端点/模型，请先保存再测试（点测试时若表单有改动会先提示保存）。
      </span>
    </div>

    <div v-loading="loading">
      <el-form label-position="top" class="two-col-form">
        <el-row :gutter="28">
          <el-col :xs="24" :md="8">
            <el-form-item label="启用">
              <el-switch v-model="form.enabled" />
              <div class="field-hint">默认关闭；关闭时不存在任何外呼路径</div>
            </el-form-item>
          </el-col>
          <el-col :xs="24" :md="16">
            <el-form-item label="端点地址">
              <el-input
                v-model="form.baseUrl"
                placeholder="https://api.deepseek.com/v1（本地模型如 http://127.0.0.1:11434/v1）"
              />
              <div class="field-hint">OpenAI 兼容端点，平台会自动拼 /chat/completions</div>
            </el-form-item>
          </el-col>

          <el-col :xs="24" :md="8">
            <el-form-item label="模型名">
              <el-input v-model="form.model" placeholder="如 deepseek-chat、qwen-plus" />
            </el-form-item>
          </el-col>
          <el-col :xs="24" :md="8">
            <el-form-item label="API Key">
              <el-input
                v-model="form.apiKey"
                type="password"
                show-password
                autocomplete="new-password"
                :placeholder="hasApiKey ? '已配置（不修改请留空）' : '本地模型通常不需要'"
              />
              <div class="field-hint">只写不读：保存后接口不再回显明文</div>
            </el-form-item>
          </el-col>
          <el-col :xs="24" :md="8">
            <el-form-item label="超时（秒）">
              <el-input-number v-model="form.timeoutSeconds" :min="5" :max="600" controls-position="right" />
              <div class="field-hint">大模型比通知慢，默认 60</div>
            </el-form-item>
          </el-col>

          <el-col :xs="24" :md="8">
            <el-form-item label="最大输出 tokens">
              <el-input-number v-model="form.maxTokens" :min="64" :max="32768" :step="256" controls-position="right" />
            </el-form-item>
          </el-col>
          <el-col :xs="24" :md="8">
            <el-form-item label="温度">
              <el-input-number v-model="form.temperature" :min="0" :max="2" :step="0.1" controls-position="right" />
              <div class="field-hint">运维结论要稳定，默认 0.2</div>
            </el-form-item>
          </el-col>
        </el-row>
      </el-form>
    </div>

    <!-- 工具与自动调查（阶段 2）：自动调查会持续产生数据出境，与常规开关分开呈现 -->
    <div class="section-block">
      <el-form label-position="top">
        <el-form-item label="工具调用协议">
          <el-select v-model="form.toolMode" style="width: 280px">
            <el-option value="auto" label="自动（先原生，失败回退 JSON）" />
            <el-option value="native" label="原生 function calling" />
            <el-option value="json" label="JSON 协议" />
          </el-select>
        </el-form-item>
        <el-form-item label="告警自动调查">
          <el-switch v-model="form.autoInvestigate" />
          <div class="field-hint">
            仅 <b>critical</b> 告警触发；同一规则 + 节点 30 分钟调查一次；全局并发 2。
            调查只读取告警所属节点，且不发送日志正文。
          </div>
        </el-form-item>
      </el-form>
    </div>

    <!-- 敏感开关：开启后诊断可携带资产属性值（可能含连接串、口令），与常规开关视觉隔离 -->
    <div class="danger-zone">
      <el-form label-position="top">
        <el-form-item label="允许发送敏感内容">
          <el-switch v-model="form.allowSensitive" />
          <div class="field-hint">
            打开后，诊断可以携带<b>资产属性值</b>（其中可能含连接串、口令）。该开关的每次变更都会记入审计。
          </div>
        </el-form-item>
      </el-form>
    </div>

    <el-alert v-if="lastTest" :type="lastTest.ok ? 'success' : 'error'" :closable="false" show-icon class="notice">
      <template #title>{{ lastTest.ok ? '连通正常' : '连接失败' }}</template>
      <div class="notice-body">{{ lastTest.text }}</div>
    </el-alert>
  </el-card>
</template>

<script setup>
import { ref, computed, watch, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import http from '../../api/http'

const form = ref({
  enabled: false,
  baseUrl: '',
  model: '',
  apiKey: '',
  timeoutSeconds: 60,
  maxTokens: 1024,
  temperature: 0.2,
  allowSensitive: false,
  includeLogs: false,
  toolMode: 'auto',
  autoInvestigate: false,
})
const hasApiKey = ref(false)
const loading = ref(false)
const saving = ref(false)
const testing = ref(false)
const lastTest = ref(null)
const baseline = ref(null)

const dirty = computed(() => {
  const b = baseline.value
  if (!b) return false
  return (
    form.value.enabled !== b.enabled ||
    form.value.baseUrl !== b.baseUrl ||
    form.value.model !== b.model ||
    form.value.apiKey !== b.apiKey ||
    form.value.timeoutSeconds !== b.timeoutSeconds ||
    form.value.maxTokens !== b.maxTokens ||
    form.value.temperature !== b.temperature ||
    form.value.allowSensitive !== b.allowSensitive ||
    form.value.toolMode !== b.toolMode ||
    form.value.autoInvestigate !== b.autoInvestigate
  )
})

// 改了任何字段就作废上一次测试结论——否则"测试通过"的绿条会挂在一个已经改过的配置上，误导性极强
watch(
  form,
  () => {
    lastTest.value = null
  },
  { deep: true },
)

async function load() {
  loading.value = true
  try {
    const res = await http.getAIConfig()
    const c = res.config || {}
    // apiKey 恒为空（服务端脱敏）；hasApiKey 决定 placeholder 文案。
    form.value = { ...form.value, ...c, apiKey: '' }
    hasApiKey.value = !!res.hasApiKey
    baseline.value = { ...form.value }
  } catch (e) {
    ElMessage.error(e.message || '读取 AI 配置失败')
  } finally {
    loading.value = false
  }
}

function validate() {
  if (!form.value.enabled) return true
  if (!form.value.baseUrl || !form.value.baseUrl.trim()) {
    ElMessage.error('启用时必须填写端点地址')
    return false
  }
  if (!/^https?:\/\//i.test(form.value.baseUrl.trim())) {
    ElMessage.error('端点地址必须是 http(s):// 开头')
    return false
  }
  if (!form.value.model || !form.value.model.trim()) {
    ElMessage.error('启用时必须填写模型名')
    return false
  }
  return true
}

async function save() {
  if (!validate()) return
  saving.value = true
  try {
    // 提交前把端点尾斜杠去掉：服务端也会做，但先做一次能避免"保存后看到的和填的不一样"
    form.value.baseUrl = (form.value.baseUrl || '').trim().replace(/\/+$/, '')
    await http.saveAIConfig(form.value)
    ElMessage.success('已保存并生效')
    lastTest.value = null
    await load()
  } catch (e) {
    ElMessage.error(e.message || '保存失败')
  } finally {
    saving.value = false
  }
}

async function test() {
  // 测试的是"已保存"配置：表单有改动时先确认，避免用户以为测的是刚填的端点
  if (dirty.value) {
    try {
      await ElMessageBox.confirm(
        '「测试连接」测试的是已保存的配置，当前表单还有未保存的改动。是否先保存再测试？',
        '测试连接',
        { type: 'info', confirmButtonText: '保存并测试', cancelButtonText: '取消' },
      )
    } catch {
      return
    }
    await save()
    if (dirty.value) return // 保存失败（或校验未过），不再测试
  }
  testing.value = true
  lastTest.value = null
  try {
    const res = await http.testAIConfig()
    lastTest.value = {
      ok: true,
      text: `模型 ${res.model} · 耗时 ${res.elapsedMs}ms · token ${res.totalTokens} · 回复「${res.reply}」`,
    }
  } catch (e) {
    lastTest.value = { ok: false, text: e.message || '连接失败' }
  } finally {
    testing.value = false
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
.settings-card {
  border-radius: var(--r-lg);
}
.card-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--sp-3);
}
.card-head .title {
  font-size: var(--fs-base);
  font-weight: 600;
  margin-right: 10px;
}
.card-head .sub {
  font-size: var(--fs-sm);
  color: var(--t3);
}
.head-actions {
  display: flex;
  gap: 10px;
}
.notice {
  margin-bottom: var(--sp-4);
}
.notice-body {
  font-size: var(--fs-sm);
  line-height: 1.7;
}
.test-hint {
  margin-bottom: var(--sp-4);
}
.two-col-form :deep(.el-form-item__label) {
  font-weight: 500;
  padding-bottom: 6px;
}
.field-hint {
  font-size: var(--fs-sm);
  color: var(--t3);
  line-height: 1.5;
  margin-top: 6px;
}
.danger-zone {
  border: 1px solid var(--danger-bd);
  border-radius: var(--r-md);
  padding: var(--sp-3) var(--sp-3) 0;
  margin-bottom: var(--sp-4);
  background: var(--fill-1);
}
</style>
