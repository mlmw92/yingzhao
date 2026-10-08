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

      <el-divider />

      <el-row :gutter="28">
        <el-col :xs="24" :md="12">
          <el-form-item label="允许发送敏感内容">
            <el-switch v-model="form.allowSensitive" />
            <div class="field-hint">
              打开后，诊断可以携带<b>日志正文与属性值</b>（其中可能含连接串、口令）。
              该开关的每次变更都会记入审计。
            </div>
          </el-form-item>
        </el-col>
        <el-col :xs="24" :md="12">
          <el-form-item label="诊断默认带日志片段">
            <el-switch v-model="form.includeLogs" :disabled="!form.allowSensitive" />
            <div class="field-hint">
              仅在「允许发送敏感内容」打开时生效；单次诊断仍可在界面上临时关掉。
            </div>
          </el-form-item>
        </el-col>
      </el-row>
    </el-form>

    <el-alert v-if="lastTest" :type="lastTest.ok ? 'success' : 'error'" :closable="false" show-icon class="notice">
      <template #title>{{ lastTest.ok ? '连通正常' : '连接失败' }}</template>
      <div class="notice-body">{{ lastTest.text }}</div>
    </el-alert>
  </el-card>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
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
})
const hasApiKey = ref(false)
const loading = ref(false)
const saving = ref(false)
const testing = ref(false)
const lastTest = ref(null)

async function load() {
  loading.value = true
  try {
    const res = await http.getAIConfig()
    const c = res.config || {}
    // apiKey 恒为空（服务端脱敏）；hasApiKey 决定 placeholder 文案。
    form.value = { ...form.value, ...c, apiKey: '' }
    hasApiKey.value = !!res.hasApiKey
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

onMounted(load)
</script>

<style scoped>
.settings-card {
  border-radius: 12px;
}
.card-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.card-head .title {
  font-size: 15px;
  font-weight: 600;
  margin-right: 10px;
}
.card-head .sub {
  font-size: 13px;
  color: var(--text-muted);
}
.head-actions {
  display: flex;
  gap: 10px;
}
.notice {
  margin-bottom: var(--sp-4);
}
.notice-body {
  font-size: 13px;
  line-height: 1.7;
}
.two-col-form :deep(.el-form-item__label) {
  font-weight: 500;
  padding-bottom: 6px;
}
.field-hint {
  font-size: 13px;
  color: var(--text-muted);
  line-height: 1.5;
  margin-top: 6px;
}
</style>
