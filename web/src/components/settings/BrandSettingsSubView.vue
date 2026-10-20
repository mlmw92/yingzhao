<template>
  <el-card class="settings-card" shadow="never">
    <template #header>
      <div class="card-head">
        <div>
          <span class="title">站点与品牌</span>
          <span class="sub">自定义系统名称与 Logo，保存后全局生效</span>
        </div>
        <div class="head-actions">
          <el-button :disabled="saving" @click="resetDefault">恢复默认</el-button>
          <el-button type="primary" :loading="saving" @click="save">保存</el-button>
        </div>
      </div>
    </template>

    <!-- 影响范围提示放在这里（此前挂在设置页左侧导航下方，与本页操作离得远）：
         这几项是全局品牌，改动会外溢到登录页 / 浏览器标签 / 对外状态页，
         在真正编辑它们的地方说清楚，比在导航栏角落提示更有效。 -->
    <el-alert
      type="info"
      :closable="false"
      show-icon
      class="brand-scope-tip"
      title="图标、名称与主题色的改动会同步到登录页、浏览器标签与对外状态页"
    />

    <el-form label-position="top" class="two-col-form">
      <el-row :gutter="28">
        <el-col :xs="24" :md="12">
          <el-form-item label="系统名称">
            <el-input
              v-model="form.name"
              maxlength="64"
              show-word-limit
              placeholder="如 星云监控"
            />
            <div class="field-hint">在整个应用程序中显示的名称</div>
          </el-form-item>
        </el-col>

        <el-col :xs="24" :md="12">
          <el-form-item label="侧栏副标题">
            <el-input
              v-model="form.subtitle"
              maxlength="32"
              show-word-limit
              placeholder="如 运维平台、监控中心"
            />
            <div class="field-hint">显示在侧栏品牌区名称下方；留空则不显示</div>
          </el-form-item>
        </el-col>

        <!-- 占满整行：这一组只有三项，前两项各占半行，
             Logo 若也占半行就会独占一行且右侧空出半列，与上下两组对不齐。 -->
        <el-col :xs="24" :md="24">
          <el-form-item label="系统 Logo">
            <div class="logo-row">
              <div class="logo-preview">
                <img v-if="form.logo" :src="form.logo" alt="logo" />
                <div v-else class="logo-empty">未设置</div>
              </div>
              <div class="logo-actions">
                <input ref="fileInput" type="file" accept="image/*" hidden @change="onFile" />
                <div class="btn-line">
                  <el-button size="small" @click="pickFile">选择图片</el-button>
                  <el-button size="small" text type="danger" :disabled="!form.logo" @click="clearLogo">清除</el-button>
                </div>
                <div class="field-hint">建议 PNG/SVG，≤ 3MB；以内嵌方式存储。留空则不显示 Logo</div>
              </div>
            </div>
          </el-form-item>
        </el-col>
      </el-row>

      <el-divider />

      <el-row :gutter="28">
        <el-col :xs="24" :md="12">
          <el-form-item label="页脚文本">
            <el-input
              v-model="form.footer"
              type="textarea"
              :rows="4"
              maxlength="512"
              show-word-limit
              placeholder="如 © 2025 您的公司。保留所有权利。"
            />
            <div class="field-hint">留空则不在页面底部显示页脚；支持纯文本，保存后全局即时生效</div>
          </el-form-item>
        </el-col>

        <el-col :xs="24" :md="12">
          <el-form-item label="页脚预览">
            <div class="footer-preview">
              <span v-if="form.footer" class="preview-text">{{ form.footer }}</span>
              <span v-else class="preview-empty">当前未设置页脚文本</span>
            </div>
          </el-form-item>
        </el-col>
      </el-row>
    </el-form>
  </el-card>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useBrand } from '../../composables/useBrand'

const { brand, saveBrand } = useBrand()
const DEFAULTS = { name: '英招', logo: '', footer: '', subtitle: '运维平台' }
const form = ref({ ...DEFAULTS })
const baseline = ref({ ...DEFAULTS })
const fileInput = ref(null)
const saving = ref(false)

onMounted(() => {
  const loaded = {
    name: brand.name || DEFAULTS.name,
    logo: brand.logo || '',
    footer: brand.footer || '',
    subtitle: brand.subtitle || DEFAULTS.subtitle,
  }
  form.value = { ...loaded }
  baseline.value = { ...loaded }
})

const dirty = computed(() => {
  const b = baseline.value
  return (
    form.value.name !== b.name ||
    form.value.logo !== b.logo ||
    form.value.footer !== b.footer ||
    form.value.subtitle !== b.subtitle
  )
})

function pickFile() {
  fileInput.value && fileInput.value.click()
}

function onFile(e) {
  const f = e.target.files && e.target.files[0]
  if (!f) return
  if (!f.type.startsWith('image/')) {
    ElMessage.error('请选择图片文件')
    return
  }
  if (f.size > 3 * 1024 * 1024) {
    ElMessage.error('Logo 过大，请控制在 3MB 以内')
    return
  }
  const reader = new FileReader()
  reader.onload = () => {
    form.value.logo = reader.result
  }
  // 读取失败（文件损坏 / 权限）时必须给反馈，否则点了「选择图片」毫无动静
  reader.onerror = () => {
    ElMessage.error('图片读取失败，请换一张再试')
  }
  reader.readAsDataURL(f)
  e.target.value = ''
}

function clearLogo() {
  form.value.logo = ''
}

async function resetDefault() {
  try {
    await ElMessageBox.confirm(
      '将把系统名称、副标题、Logo、页脚恢复为默认值。注意：需再点「保存」才会真正生效。',
      '恢复默认',
      { type: 'warning', confirmButtonText: '恢复默认', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  form.value = { ...DEFAULTS }
}

async function save() {
  if (!form.value.name || !form.value.name.trim()) {
    ElMessage.error('系统名称不能为空')
    return
  }
  saving.value = true
  try {
    await saveBrand(form.value.name.trim(), form.value.logo, form.value.footer, form.value.subtitle.trim())
    baseline.value = { ...form.value }
    ElMessage.success('已保存，全局即时生效')
  } catch (e) {
    ElMessage.error(e.message || '保存失败')
  } finally {
    saving.value = false
  }
}

function reset() {
  form.value = { ...baseline.value }
}

// dirty 契约：供 SettingsView 容器渲染吸底「未保存改动」条。
// ownsActions: 本页顶部已有「保存」，容器不再显示吸底保存条（否则两个保存入口）。
defineExpose({ dirty, save, reset, ownsActions: true })
</script>

<style scoped>
.settings-card {
  border-radius: var(--r-lg);
}
.brand-scope-tip {
  margin-bottom: var(--sp-4);
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
.logo-row {
  display: flex;
  gap: var(--sp-4);
  align-items: flex-start;
}
.logo-preview {
  width: 80px;
  height: 80px;
  border-radius: var(--r-lg);
  border: 1px dashed var(--bd);
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
  background: var(--fill-1);
  flex-shrink: 0;
}
.logo-preview img {
  max-width: 100%;
  max-height: 100%;
  object-fit: contain;
}
.logo-empty {
  font-size: var(--fs-sm);
  color: var(--t3);
  padding: 8px;
  text-align: center;
}
.logo-actions {
  display: flex;
  flex-direction: column;
  gap: 8px;
  align-items: flex-start;
}
.btn-line {
  display: flex;
  gap: 8px;
}
.footer-preview {
  width: 100%;
  padding: 16px 20px;
  border-radius: var(--r-md);
  border: 1px dashed var(--bd);
  background: var(--fill-1);
  text-align: center;
  color: var(--t2);
  font-size: var(--fs-sm);
}
.preview-text {
  color: var(--t1);
  white-space: pre-wrap;
}
.preview-empty {
  color: var(--t3);
}
</style>
