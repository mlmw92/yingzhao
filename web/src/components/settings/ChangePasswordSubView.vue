<template>
  <el-card class="settings-card" shadow="never">
    <template #header>
      <div class="card-head">
        <div>
          <span class="title">修改登录密码</span>
          <span class="sub">密码以国密 SM3 加盐哈希存储，服务端不会保存明文</span>
        </div>
      </div>
    </template>

    <el-form :model="form" label-position="top" class="pwd-form" @submit.prevent>
      <el-form-item label="原密码" :error="errors.oldPassword">
        <el-input
          v-model="form.oldPassword"
          type="password"
          show-password
          placeholder="请输入当前登录密码"
          autocomplete="current-password"
        />
      </el-form-item>

      <el-form-item label="新密码" :error="errors.newPassword">
        <el-input
          v-model="form.newPassword"
          type="password"
          show-password
          placeholder="至少 8 位，且含大小写字母 / 数字 / 特殊字符中的两类"
          autocomplete="new-password"
        />
        <!-- 强度条：与后端 ValidatePassword 同一套口径（长度 + 字符类别数） -->
        <div v-if="strength" class="strength">
          <div class="strength-bars">
            <span
              v-for="i in 4"
              :key="i"
              class="strength-bar"
              :class="[strength.tone, { on: i <= strength.score }]"
            ></span>
          </div>
          <span class="strength-label" :class="strength.tone">强度：{{ strength.label }}</span>
        </div>
      </el-form-item>

      <el-form-item label="确认新密码" :error="errors.confirm">
        <el-input
          v-model="form.confirm"
          type="password"
          show-password
          placeholder="请再次输入新密码"
          autocomplete="new-password"
        />
      </el-form-item>

      <p class="rule-hint">
        密码至少 <b>8 位</b>，且需包含<b>大小写字母、数字、特殊字符中的至少两类</b>（与服务端校验一致）。
      </p>

      <div class="actions">
        <el-button :loading="saving" type="primary" @click="submit">保存修改</el-button>
        <el-button :disabled="saving" @click="reset">重置</el-button>
      </div>
    </el-form>
  </el-card>
</template>

<script setup>
import { reactive, ref, computed } from 'vue'
import { ElMessage } from 'element-plus'
import http from '../../api/http'

const form = reactive({ oldPassword: '', newPassword: '', confirm: '' })
const errors = reactive({ oldPassword: '', newPassword: '', confirm: '' })
const saving = ref(false)

// 统计密码命中的字符类别数（小写 / 大写 / 数字 / 特殊），与后端 ValidatePassword 口径一致
function classCount(pw) {
  let n = 0
  if (/[a-z]/.test(pw)) n++
  if (/[A-Z]/.test(pw)) n++
  if (/[0-9]/.test(pw)) n++
  if (/[^a-zA-Z0-9]/.test(pw)) n++
  return n
}

const strength = computed(() => {
  const pw = form.newPassword || ''
  if (!pw) return null
  const classes = classCount(pw)
  let score = 1
  if (pw.length >= 8) score++
  if (classes >= 2) score++
  if (pw.length >= 12 || classes >= 3) score++
  const map = {
    1: { label: '弱', tone: 'danger' },
    2: { label: '中', tone: 'warn' },
    3: { label: '强', tone: 'ok' },
    4: { label: '很强', tone: 'ok' },
  }
  return { score, ...map[score] }
})

function reset() {
  form.oldPassword = ''
  form.newPassword = ''
  form.confirm = ''
  errors.oldPassword = ''
  errors.newPassword = ''
  errors.confirm = ''
}

// 与后端 auth.ValidatePassword 对齐：≥8 位 + ≥2 类字符，避免"前端放行、后端拒绝"的往返
function validate() {
  errors.oldPassword = ''
  errors.newPassword = ''
  errors.confirm = ''
  let ok = true
  if (!form.oldPassword) {
    errors.oldPassword = '请输入原密码'
    ok = false
  }
  if (!form.newPassword) {
    errors.newPassword = '请输入新密码'
    ok = false
  } else if (form.newPassword.length < 8) {
    errors.newPassword = '密码至少 8 位'
    ok = false
  } else if (classCount(form.newPassword) < 2) {
    errors.newPassword = '密码需至少包含大小写字母、数字、特殊字符中的两类'
    ok = false
  }
  if (form.newPassword !== form.confirm) {
    errors.confirm = '两次输入的新密码不一致'
    ok = false
  }
  return ok
}

async function submit() {
  if (!validate()) return
  saving.value = true
  try {
    await http.changePassword(form.oldPassword, form.newPassword)
    ElMessage.success('密码已修改，下次登录请使用新密码')
    reset()
  } catch (e) {
    // 后端校验信息就地回填到对应字段，而不是只弹一条顶部提示
    const msg = e.message || '修改失败'
    if (msg.includes('原密码')) errors.oldPassword = msg
    else errors.newPassword = msg
    ElMessage.error(msg)
  } finally {
    saving.value = false
  }
}
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
  font-size: var(--fs-md);
  font-weight: 600;
  margin-right: var(--sp-2);
}
.card-head .sub {
  font-size: var(--fs-sm);
  color: var(--t3);
}
.pwd-form {
  max-width: 460px;
}
.pwd-form :deep(.el-form-item__label) {
  font-weight: 500;
  padding-bottom: 6px;
}
.strength {
  display: flex;
  align-items: center;
  gap: var(--sp-2);
  margin-top: var(--sp-2);
  width: 100%;
}
.strength-bars {
  display: flex;
  gap: 4px;
  flex: 1;
  min-width: 0;
}
.strength-bar {
  flex: 1;
  height: 4px;
  border-radius: var(--r-xs);
  background: var(--fill-3);
}
.strength-bar.on.danger {
  background: var(--danger);
}
.strength-bar.on.warn {
  background: var(--warn);
}
.strength-bar.on.ok {
  background: var(--ok);
}
.strength-label {
  font-size: var(--fs-xs);
  flex-shrink: 0;
}
.strength-label.danger {
  color: var(--danger);
}
.strength-label.warn {
  color: var(--warn);
}
.strength-label.ok {
  color: var(--ok);
}
.rule-hint {
  margin: 0 0 var(--sp-3);
  font-size: var(--fs-xs);
  line-height: 1.6;
  color: var(--t3);
}
.actions {
  display: flex;
  gap: var(--sp-3);
  margin-top: var(--sp-2);
}
</style>
