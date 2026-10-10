<template>
  <div class="login-page">
    <div class="bg-grid"></div>
    <div class="bg-glow"></div>

    <div class="login-card glass">
      <div class="brand">
        <img v-if="brand.logo" :src="brand.logo" class="logo-img" alt="logo" />
        <div class="brand-text">
          <h1>{{ brand.name || 'NebulaEye' }}</h1>
          <p v-if="brand.subtitle">{{ brand.subtitle }}</p>
          <p v-else class="default-sub">服务器监控系统</p>
        </div>
      </div>

      <el-form class="login-form" @submit.prevent="doLogin">
        <el-form-item :error="errors.username">
          <el-input
            v-model="username"
            size="large"
            placeholder="用户名"
            :prefix-icon="User"
            autofocus
            @input="errors.username = ''"
            @keyup.enter="doLogin"
          />
        </el-form-item>
        <el-form-item :error="errors.password">
          <el-input
            v-model="password"
            size="large"
            type="password"
            placeholder="密码"
            :prefix-icon="Lock"
            show-password
            @input="errors.password = ''"
            @keyup.enter="doLogin"
          />
        </el-form-item>
        <el-alert
          v-if="error"
          :title="error"
          type="error"
          show-icon
          :closable="false"
          style="margin-bottom: 16px"
        />
        <el-button
          type="primary"
          size="large"
          class="login-btn"
          :loading="loading"
          @click="doLogin"
        >
          登 录
        </el-button>
      </el-form>

      <p class="hint">账号与口令由部署时设定（安装脚本默认生成强口令，请查阅安装日志 / server.yaml）</p>
    </div>

    <footer v-if="brand.footer" class="login-footer">
      {{ brand.footer }}
    </footer>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { User, Lock } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import http, { setToken } from '../api/http'
import { useBrand } from '../composables/useBrand'
import { useAuth } from '../composables/useAuth'

const router = useRouter()
const { brand, loadBrand } = useBrand()
const auth = useAuth()
// 拉取最新品牌配置（含系统名称、Logo、页脚），确保登录页即时反映设置
loadBrand()

const username = ref('')
const password = ref('')
const error = ref('')
const errors = ref({ username: '', password: '' })
const loading = ref(false)

async function doLogin() {
  error.value = ''
  // 必填校验：空值不提交，就地提示并聚焦首个错误项
  errors.value = {
    username: username.value.trim() ? '' : '请输入用户名',
    password: password.value ? '' : '请输入密码',
  }
  if (errors.value.username || errors.value.password) {
    const first = document.querySelector('.login-form .el-input__inner')
    first && first.focus()
    return
  }
  loading.value = true
  try {
    const d = await http.post('/api/v1/login', {
      username: username.value,
      password: password.value,
    })
    if (d.token) {
      setToken(d.token)
      try { await auth.loadMe() } catch (e) { /* 授权信息非阻塞 */ }
      ElMessage.success('登录成功')
      router.replace('/')
    } else if (d.authEnabled === false) {
      setToken('')
      router.replace('/')
    }
  } catch (e) {
    error.value = e.message || '登录失败'
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.login-page {
  position: fixed;
  inset: 0;
  display: flex;
  /* 纵向排列：卡片在上、页脚在下。此前页脚是 absolute bottom，
     视口一矮就压在卡片上；让它回到流里，跟着内容一起滚动。 */
  flex-direction: column;
  align-items: center;
  justify-content: center;
  background: var(--bg);
  /* 居中 + 不可滚动在矮视口下是双向裁切：卡片上下都被切掉还滚不到。
     改成允许纵向滚动，并用 padding 保证短内容仍然居中（margin:auto 在 flex 居中
     下于溢出时仍能保持对称留白）。 */
  overflow-y: auto;
  padding: 24px 16px;
}
.bg-grid {
  position: absolute;
  inset: 0;
  background-image: linear-gradient(var(--grid-line) 1px, transparent 1px),
    linear-gradient(90deg, var(--grid-line) 1px, transparent 1px);
  background-size: 48px 48px;
  mask-image: radial-gradient(circle at 50% 50%, #000 0%, transparent 70%);
}
.bg-glow {
  position: absolute;
  width: 600px;
  height: 600px;
  border-radius: 50%;
  background: radial-gradient(circle, var(--glow-acc) 0%, transparent 70%);
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
  filter: blur(40px);
}
.login-card {
  position: relative;
  /* 写死 380px 在窄屏（<412px）会横向被切；与命令面板同一口径：上限 px，下限视口 */
  width: 380px;
  max-width: 100%;
  padding: 36px 32px 28px;
  border-radius: 16px;
  z-index: 1;
  box-shadow: 0 20px 60px rgba(0, 0, 0, 0.5), 0 0 0 1px var(--bd);
}
.brand {
  display: flex;
  align-items: center;
  gap: 14px;
  margin-bottom: 30px;
}
.logo-img {
  width: 44px;
  height: 44px;
  border-radius: 12px;
  object-fit: contain;
  background: var(--s1);
  box-shadow: 0 0 24px var(--accent-glow);
}
.brand-text h1 {
  font-size: 20px;
  font-weight: 700;
  letter-spacing: 0.04em;
  color: var(--t1);
}
.brand-text p {
  font-size: 13px;
  color: var(--t2);
  margin-top: 2px;
}
.default-sub {
  opacity: 0.8;
}
.login-form {
  display: flex;
  flex-direction: column;
}
.login-btn {
  width: 100%;
  letter-spacing: 0.1em;
  font-weight: 600;
}
.hint {
  text-align: center;
  font-size: 13px;
  color: var(--t3);
  margin-top: 20px;
}
.login-footer {
  flex: 0 0 auto;
  margin-top: 20px;
  text-align: center;
  font-size: 13px;
  color: var(--t3);
  z-index: 1;
}
</style>
