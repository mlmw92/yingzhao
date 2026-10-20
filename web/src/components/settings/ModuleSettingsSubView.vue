<template>
  <div class="module-settings">
    <el-alert
      type="info"
      :closable="false"
      show-icon
      title="关闭仅隐藏入口并停止对应定时任务，不影响已产生的数据，也不改变接口可达性。"
    />
    <div v-for="g in moduleGroups" :key="g.key" class="module-group">
      <div class="group-head">
        <strong>{{ g.label }}</strong>
        <el-switch
          :model-value="!disabled.includes(g.key)"
          :disabled="isLocked(g.key)"
          @update:model-value="(v) => toggle(g.key, v)"
        />
        <span v-if="isLocked(g.key)" class="locked-reason">不可关闭</span>
      </div>
      <div class="items">
        <el-checkbox
          v-for="item in g.items"
          :key="item.key"
          :model-value="!disabled.includes(item.key)"
          :disabled="isLocked(item.key) || disabled.includes(g.key)"
          @update:model-value="(v) => toggle(item.key, v)"
        >{{ item.label }}</el-checkbox>
      </div>
    </div>
    <el-button type="primary" :loading="saving" @click="save">保存</el-button>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import http from '../../api/http'
import { moduleGroups, isLocked } from '../../modules/catalog'
import { useModules } from '../../composables/useModules'

const disabled = ref([])
const saving = ref(false)
const modules = useModules()

async function loadConfig() {
  const cfg = await http.get('/api/v1/ui/settings')
  disabled.value = [...(cfg.disabled || [])]
}

onMounted(loadConfig)

// 域与页面共用一套勾选逻辑：关闭 = 加入清单，开启 = 移出清单
function toggle(key, on) {
  if (isLocked(key)) return
  const set = new Set(disabled.value)
  on ? set.delete(key) : set.add(key)
  disabled.value = [...set]
}

async function save() {
  saving.value = true
  try {
    // 只提交开关：接口是部分更新，品牌字段不会被清空
    await http.put('/api/v1/ui/settings', { disabled: disabled.value })
    await modules.load(true)
    ElMessage.success('功能开关已保存')
  } catch (e) {
    ElMessage.error('保存失败' + (e?.message ? '：' + e.message : ''))
  } finally {
    saving.value = false
  }
}
</script>

<style scoped>
.module-group {
  margin: 16px 0;
  padding: 12px 14px;
  background: var(--el-fill-color-lighter);
  border-radius: 8px;
}
.group-head {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 8px;
}
.locked-reason {
  color: var(--t3);
  font-size: var(--fs-sm);
}
.items {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 16px;
}
</style>
