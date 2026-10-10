<template>
  <div class="notify-view">
    <PageHeader
      title="通知配置"
      desc="配置告警通知渠道，保存后立即生效（热加载，无需重启）；配置独立存储，不修改 server.yaml"
    >
      <template #actions>
        <el-select v-model="testChannel" class="ch-pick" placeholder="选择渠道">
          <el-option v-for="c in CHANNELS" :key="c.key" :label="c.label" :value="c.key" />
        </el-select>
        <el-button :loading="testing" :disabled="loading || !!loadError" @click="testNotify">发送测试</el-button>
        <el-button type="primary" :loading="saving" :disabled="loading || !!loadError" @click="save">保存并生效</el-button>
      </template>
    </PageHeader>

    <!-- 渠道健康度摘要：先回答"现在到底有几个渠道是通的"，
         不必逐个卡片去数开关、再看哪个缺地址 -->
    <div v-if="!loading && !loadError" class="health-bar">
      <StatusPill :tone="health.tone" :dot="true" :text="health.text" size="md" />
      <span class="health-detail">{{ health.detail }}</span>
    </div>

    <!-- 加载失败：页面级错误态 + 重试。
         原来只弹一条 toast，页面留着空表单——用户既看不到原因，也没法重试。 -->
    <SectionCard v-if="loadError" title="通知配置" :error="loadError" error-retry @retry="load" />

    <SectionCard v-else-if="loading" title="通知配置" loading />

    <el-form v-else :model="notify" label-position="top" class="cards">
      <!-- 邮件 -->
      <SectionCard title="邮件 (SMTP)" subtitle="通过 SMTP 服务器发送告警邮件">
        <template #actions>
          <StatusPill :tone="statusOf('email').tone" :dot="true" :text="statusOf('email').text" />
          <el-switch v-model="notify.email.enabled" aria-label="启用渠道：邮件 (SMTP)" />
        </template>
        <el-row :gutter="16">
          <el-col :xs="24" :sm="24" :md="12">
            <el-form-item label="SMTP 主机" :error="issue('email')">
              <el-input v-model="notify.email.smtpHost" placeholder="smtp.example.com" />
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="24" :md="12"><el-form-item label="端口"><el-input-number v-model="notify.email.smtpPort" :min="1" :max="65535" controls-position="right" /></el-form-item></el-col>
          <el-col :xs="24" :sm="24" :md="12"><el-form-item label="用户名"><el-input v-model="notify.email.username" /></el-form-item></el-col>
          <el-col :xs="24" :sm="24" :md="12"><el-form-item label="发件人"><el-input v-model="notify.email.from" placeholder="monitor@example.com" /></el-form-item></el-col>
          <el-col :xs="24" :sm="24" :md="12">
            <el-form-item label="密码">
              <el-input v-model="notify.email.password" type="password" show-password placeholder="不修改请留空" />
              <!-- 服务端 GET 不返回明文密码（notify.go:19-21 主动置空），
                   所以前端**无法**判断"是否已保存过密码"——这里不编一个"已保存"徽标，
                   而是把契约说清楚：留空 = 沿用已保存的值。 -->
              <div class="field-hint">已保存的密码不会回显；此处留空表示继续使用已保存的密码。</div>
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="24" :md="12">
            <el-form-item label="加密方式">
              <el-radio-group v-model="emailEncryption">
                <el-radio value="none">无</el-radio>
                <el-radio value="starttls">STARTTLS</el-radio>
                <el-radio value="tls">SSL/TLS</el-radio>
              </el-radio-group>
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item label="收件人（多人每行一个）">
          <StringListInput v-model="notify.email.to" placeholder="ops@example.com" />
        </el-form-item>
      </SectionCard>

      <!-- Webhook -->
      <SectionCard title="Webhook" subtitle="以 HTTP POST 把 JSON 告警体推送到你的服务">
        <template #actions>
          <StatusPill :tone="statusOf('webhook').tone" :dot="true" :text="statusOf('webhook').text" />
          <el-switch v-model="notify.webhook.enabled" aria-label="启用渠道：Webhook" />
        </template>
        <el-form-item label="Webhook 地址（多个地址每行一个）" :error="issue('webhook')">
          <StringListInput v-model="notify.webhook.urls" placeholder="https://example.com/webhook" />
        </el-form-item>
      </SectionCard>

      <!-- 钉钉 -->
      <SectionCard title="钉钉机器人" subtitle="推送到钉钉群机器人">
        <template #actions>
          <StatusPill :tone="statusOf('dingtalk').tone" :dot="true" :text="statusOf('dingtalk').text" />
          <el-switch v-model="notify.dingtalk.enabled" aria-label="启用渠道：钉钉机器人" />
        </template>
        <el-form-item label="机器人 Webhook（多个群每行一个）" :error="issue('dingtalk')">
          <StringListInput v-model="notify.dingtalk.urls" placeholder="https://oapi.dingtalk.com/robot/send?access_token=xxx" />
        </el-form-item>
        <el-form-item label="加签密钥（可选）">
          <el-input v-model="notify.dingtalk.secret" type="password" show-password placeholder="不修改请留空" />
          <div class="field-hint">已保存的密钥不会回显；此处留空表示继续使用已保存的密钥。</div>
        </el-form-item>
        <el-form-item label="@ 手机号（可选，多人每行一个）">
          <StringListInput v-model="notify.dingtalk.atMobiles" placeholder="13800000000" />
        </el-form-item>
      </SectionCard>

      <!-- 飞书 -->
      <SectionCard title="飞书机器人" subtitle="推送到飞书群机器人">
        <template #actions>
          <StatusPill :tone="statusOf('feishu').tone" :dot="true" :text="statusOf('feishu').text" />
          <el-switch v-model="notify.feishu.enabled" aria-label="启用渠道：飞书机器人" />
        </template>
        <el-form-item label="机器人 Webhook（多个群每行一个）" :error="issue('feishu')">
          <StringListInput v-model="notify.feishu.urls" placeholder="https://open.feishu.cn/open-apis/bot/v2/hook/xxx" />
        </el-form-item>
        <el-form-item label="签名密钥（可选）">
          <el-input v-model="notify.feishu.secret" type="password" show-password placeholder="不修改请留空" />
          <div class="field-hint">已保存的密钥不会回显；此处留空表示继续使用已保存的密钥。</div>
        </el-form-item>
      </SectionCard>

      <!-- 企业微信 -->
      <SectionCard title="企业微信机器人" subtitle="推送到企业微信群机器人">
        <template #actions>
          <StatusPill :tone="statusOf('wecom').tone" :dot="true" :text="statusOf('wecom').text" />
          <el-switch v-model="notify.wecom.enabled" aria-label="启用渠道：企业微信机器人" />
        </template>
        <el-form-item label="机器人 Webhook（多个群每行一个）" :error="issue('wecom')">
          <StringListInput v-model="notify.wecom.urls" placeholder="https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxx" />
        </el-form-item>
        <el-form-item label="@ 成员（可选，userid 或 @all，多人每行一个）">
          <StringListInput v-model="notify.wecom.mentionedList" placeholder="@all" />
        </el-form-item>
      </SectionCard>
    </el-form>
  </div>
</template>

<script setup>
import { ref, computed, nextTick, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import http from '../api/http'
import StringListInput from './StringListInput.vue'
import PageHeader from './common/PageHeader.vue'
import SectionCard from './common/SectionCard.vue'
import StatusPill from './common/StatusPill.vue'

// 渠道标识与后端 notifier 的 Channel() 返回值一一对应
// （alert/notifier.go: email / webhook / dingtalk / feishu / wecom），
// 也等于 notify 配置对象的字段名，可直接用来读「是否已启用」。
const CHANNELS = [
  { key: 'email', label: '邮件 (SMTP)' },
  { key: 'webhook', label: 'Webhook' },
  { key: 'dingtalk', label: '钉钉机器人' },
  { key: 'feishu', label: '飞书机器人' },
  { key: 'wecom', label: '企业微信机器人' },
]

// 每个渠道"启用后必须填"的字段：启用却不填 = 一条通知也发不出去，
// 而界面上看不出问题（保存成功、开关是开的）——所以要在卡片里当场标出来。
const REQUIRED = {
  email: [{ field: 'smtpHost', label: 'SMTP 主机' }],
  webhook: [{ field: 'urls', label: 'Webhook 地址', isList: true }],
  dingtalk: [{ field: 'urls', label: '机器人 Webhook', isList: true }],
  feishu: [{ field: 'urls', label: '机器人 Webhook', isList: true }],
  wecom: [{ field: 'urls', label: '机器人 Webhook', isList: true }],
}

const loading = ref(false)
const saving = ref(false)
const testing = ref(false)
const loadError = ref('')
const notify = ref(defaultConfig())
const testChannel = ref('email')

// emailEncryption 将加密方式映射到 useTLS / useStartTLS 两个布尔字段。
const emailEncryption = computed({
  get() {
    if (notify.value.email.useStartTLS) return 'starttls'
    if (notify.value.email.useTLS) return 'tls'
    return 'none'
  },
  set(v) {
    notify.value.email.useTLS = v === 'tls'
    notify.value.email.useStartTLS = v === 'starttls'
  },
})

function defaultConfig() {
  return {
    email: { enabled: false, smtpHost: '', smtpPort: 587, username: '', password: '', from: '', to: [], useTLS: true, useStartTLS: false },
    webhook: { enabled: false, urls: [] },
    dingtalk: { enabled: false, urls: [], secret: '', atMobiles: [] },
    feishu: { enabled: false, urls: [], secret: '' },
    wecom: { enabled: false, urls: [], mentionedList: [] },
  }
}

function merge(dst, src) {
  if (!src) return dst
  for (const k of Object.keys(dst)) {
    if (src[k] && typeof src[k] === 'object' && !Array.isArray(src[k])) {
      merge(dst[k], src[k])
    } else if (src[k] !== undefined) {
      dst[k] = src[k]
    }
  }
  return dst
}

// ---- 渠道完整性（驱动卡片状态药丸 + 字段级错误）----
function missingOf(key) {
  const cfg = notify.value[key] || {}
  return (REQUIRED[key] || []).filter((r) => {
    const v = cfg[r.field]
    return r.isList ? !(Array.isArray(v) && v.length > 0) : !v
  })
}

// 未启用时不该报"缺字段"——没启用的渠道填不填都无所谓。
function issue(key) {
  if (!notify.value[key]?.enabled) return ''
  const miss = missingOf(key)
  return miss.length ? `已启用该渠道，请先填写${miss.map((m) => m.label).join('、')}` : ''
}

function statusOf(key) {
  if (!notify.value[key]?.enabled) return { tone: 'muted', text: '未启用' }
  return missingOf(key).length ? { tone: 'warn', text: '配置不完整' } : { tone: 'ok', text: '已启用' }
}

const health = computed(() => {
  const enabled = CHANNELS.filter((c) => notify.value[c.key]?.enabled)
  if (!enabled.length) return { tone: 'muted', text: '未启用任何渠道', detail: '告警仅进入平台消息，不会外发' }
  const bad = enabled.filter((c) => missingOf(c.key).length)
  if (bad.length) {
    return { tone: 'warn', text: `${enabled.length} 个渠道已启用`, detail: `${bad.map((c) => c.label).join('、')} 配置不完整，保存会被拦下` }
  }
  return { tone: 'ok', text: `${enabled.length} 个渠道已启用`, detail: '配置完整，保存后立即生效' }
})

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    const c = await http.get('/api/v1/notify')
    notify.value = merge(defaultConfig(), c)
  } catch (e) {
    // 不再只弹 toast：页面级错误态 + 重试按钮才能让用户知道发生了什么、怎么恢复
    loadError.value = '加载通知配置失败：' + (e.message || '网络异常')
  } finally {
    loading.value = false
  }
}

async function save() {
  // 启用却缺地址的渠道会被后端"静默接受"（通知永远发不出去），必须在提交前拦下。
  // 提示已经常驻在各卡片字段上（el-form-item 的 :error），这里只负责把用户带到第一个问题处。
  const bad = CHANNELS.filter((c) => notify.value[c.key]?.enabled && missingOf(c.key).length)
  if (bad.length) {
    ElMessage.warning(`${bad.map((c) => c.label).join('、')} 配置不完整，请检查标红字段`)
    await nextTick()
    document.querySelector('.notify-view .el-form-item.is-error')?.scrollIntoView({ block: 'center' })
    return
  }
  saving.value = true
  try {
    await http.put('/api/v1/notify', notify.value)
    ElMessage.success('已保存并生效')
  } catch (e) {
    ElMessage.error('保存失败：' + e.message)
  } finally {
    saving.value = false
  }
}

async function testNotify() {
  const ch = CHANNELS.find((c) => c.key === testChannel.value) || CHANNELS[0]
  // 后端只把「已启用」的渠道放进 notifiers 快照，未启用时按 channel 过滤后为空，
  // 结果是「返回成功但一条通知都没发出去」——所以这里必须提前拦住。
  if (!notify.value[ch.key]?.enabled) {
    ElMessage.warning(`「${ch.label}」当前未启用，请先启用并保存后再测试`)
    return
  }

  // 邮件走 /notify/test：只验证 SMTP 链路，不产生告警事件。
  // 其余渠道没有对应的纯连通性接口，只能走 /alerts/test：
  // 它会写入一条 Test=true 的告警事件并广播到所有在线客户端，必须先告知。
  if (ch.key !== 'email') {
    try {
      await ElMessageBox.confirm(
        `将向「${ch.label}」发送一条测试告警。\n\n该操作会生成一条测试告警事件并出现在告警列表中（已标记为测试事件），同时推送到所有在线客户端。`,
        '发送测试告警',
        { type: 'warning', confirmButtonText: '确认发送', cancelButtonText: '取消' }
      )
    } catch {
      return // 用户取消
    }
  }

  testing.value = true
  try {
    const r =
      ch.key === 'email'
        ? await http.post('/api/v1/notify/test')
        : await http.post('/api/v1/alerts/test?channel=' + encodeURIComponent(ch.key))
    if (r?.ok) {
      ElMessage.success(ch.key === 'email' ? r.message || '测试邮件已发送' : `已向「${ch.label}」发送测试告警`)
    } else {
      ElMessageBox.alert(r?.error || '测试失败', `${ch.label}发送失败`, {
        type: 'error',
        confirmButtonText: '关闭',
      })
    }
  } catch (e) {
    ElMessageBox.alert(e.message || '请求失败', `${ch.label}发送失败`, {
      type: 'error',
      confirmButtonText: '关闭',
    })
  } finally {
    testing.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.notify-view {
  /* 920px 在宽屏上把内容钉在左侧、右侧大片留白；
     放宽到 1120px，同时保留上限——表单行过长反而不好扫读。 */
  max-width: 1120px;
}
/* 渠道健康度摘要条：与卡片同一视觉语言，但更轻（无阴影） */
.health-bar {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 14px;
  flex-wrap: wrap;
}
.health-detail {
  font-size: var(--fs-sm);
  color: var(--t3);
}
.cards {
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.field-hint {
  font-size: var(--fs-xs);
  color: var(--t3);
  line-height: 1.5;
  margin-top: 2px;
}
/* 渠道选择：180px 对应宽度令牌 M 档（§1.5），放得下最长的「企业微信机器人」 */
.ch-pick {
  width: 180px;
}
</style>
