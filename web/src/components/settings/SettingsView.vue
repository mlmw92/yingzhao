<template>
  <div class="settings-view">
    <PageHeader title="系统设置" desc="站点品牌、告警管道、自监控、数据保留与 AI 助手" />

    <!-- 二级导航改为纵向：
         原来用横向 el-tabs，而子页内部还会横向分块，两层都是横的，层级分不清；
         纵向之后左列是「分类」、右列是「内容」，一眼可分。
         导航项用原生 <button role="tab">：键盘可 Tab 到、Enter/Space 可切换、读屏识别选中态。 -->
    <div class="settings-layout">
      <aside class="settings-nav" role="tablist" aria-label="设置分类">
        <div class="sn-title">设置分类</div>
        <button
          v-for="p in panes"
          :key="p.name"
          type="button"
          role="tab"
          class="sn-item"
          :class="{ on: active === p.name }"
          :aria-selected="active === p.name"
          :tabindex="active === p.name ? 0 : -1"
          @click="selectPane(p.name)"
          @keydown.enter.prevent="selectPane(p.name)"
          @keydown.space.prevent="selectPane(p.name)"
        >
          {{ p.label }}
        </button>
      </aside>

      <div class="settings-body">
        <keep-alive>
          <component :is="currentComp" ref="paneRef" />
        </keep-alive>

        <!-- 未保存改动条：由子页通过 dirty 契约（expose 出 dirty/save/reset）驱动 -->
        <div v-if="activeDirty" class="save-bar">
          <span class="sb-text">「{{ activeLabel }}」有未保存的改动</span>
          <div class="sb-actions">
            <el-button size="small" :disabled="saving" @click="discardActive">放弃改动</el-button>
            <el-button size="small" type="primary" :loading="saving" @click="saveActive">保存</el-button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import PageHeader from '../common/PageHeader.vue'
import BrandSettingsSubView from './BrandSettingsSubView.vue'
import ModuleSettingsSubView from './ModuleSettingsSubView.vue'
import AlertPipelineSubView from './AlertPipelineSubView.vue'
import SelfMonitorSubView from './SelfMonitorSubView.vue'
import RetentionSubView from './RetentionSubView.vue'
import AISettingsSubView from './AISettingsSubView.vue'

const route = useRoute()
const router = useRouter()

const panes = [
  { name: 'brand', label: '站点与品牌', comp: BrandSettingsSubView },
  { name: 'modules', label: '功能开关', comp: ModuleSettingsSubView },
  { name: 'pipeline', label: '告警管道', comp: AlertPipelineSubView },
  { name: 'selfmon', label: '系统自监控', comp: SelfMonitorSubView },
  { name: 'retention', label: '数据保留', comp: RetentionSubView },
  { name: 'ai', label: 'AI 助手', comp: AISettingsSubView },
]

const isValidTab = (t) => panes.some((p) => p.name === t)

// 支持 /system/settings?tab=pipeline 直达某个分类
const active = ref(isValidTab(route.query.tab) ? route.query.tab : 'brand')

// keep-alive 保留子页实例，切回来时未保存的表单状态不丢
const currentComp = computed(() => (panes.find((p) => p.name === active.value) || panes[0]).comp)
const activeLabel = computed(() => (panes.find((p) => p.name === active.value) || panes[0]).label)

// 切换分类：写回 URL（刷新 / 分享 / 前进后退都可复现当前分类）
function selectPane(name) {
  if (!isValidTab(name) || name === active.value) return
  active.value = name
  router.replace({ query: { ...route.query, tab: name } })
}

// 前进/后退或外部改 URL 时同步当前分类
watch(
  () => route.query.tab,
  (t) => {
    if (isValidTab(t) && t !== active.value) active.value = t
  },
)

// —— 子页 dirty 契约 ——
// 子页 expose { dirty, save, reset }；dirty 为 ref/computed，父层读它决定是否显示吸底保存条。
const paneRef = ref(null)
const saving = ref(false)

function readDirty(inst) {
  if (!inst) return false
  const d = inst.dirty
  // 兼容 expose 出来的是 ref（未解包）或已解包的值
  if (d && typeof d === 'object' && 'value' in d) return !!d.value
  return !!d
}

const activeDirty = computed(() => readDirty(paneRef.value))

async function saveActive() {
  const inst = paneRef.value
  if (!inst || typeof inst.save !== 'function') return
  saving.value = true
  try {
    await inst.save()
  } finally {
    saving.value = false
  }
}

function discardActive() {
  const inst = paneRef.value
  if (inst && typeof inst.reset === 'function') inst.reset()
}
</script>

<style scoped>
.settings-layout {
  display: grid;
  grid-template-columns: 200px minmax(0, 1fr);
  gap: var(--sp-5);
  align-items: start;
}
.settings-nav {
  display: flex;
  flex-direction: column;
  gap: 2px;
  position: sticky;
  top: 0;
}
.sn-title {
  font-size: var(--fs-xs);
  color: var(--t3);
  padding: 0 10px;
  margin-bottom: 6px;
}
.sn-item {
  font-family: inherit;
  font-size: var(--fs-sm);
  text-align: left;
  width: 100%;
  background: none;
  color: var(--t3);
  padding: 7px 10px;
  border-radius: var(--r-sm);
  border: none;
  border-left: 2px solid transparent;
  cursor: pointer;
  transition: color var(--dur-1) var(--ease), background var(--dur-1) var(--ease);
  user-select: none;
}
.sn-item:hover {
  color: var(--t2);
  background: var(--fill-2);
}
.sn-item:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 1px;
}
.sn-item.on {
  color: var(--accent);
  background: var(--accent-dim);
  border-left-color: var(--accent);
  font-weight: 600;
}
.settings-body {
  min-width: 0;
}
.save-bar .sb-text {
  font-size: var(--fs-sm);
  color: var(--t2);
  flex: 1;
  min-width: 0;
}
.save-bar .sb-actions {
  display: flex;
  gap: var(--sp-2);
}
@media (max-width: 1024px) {
  .settings-layout {
    grid-template-columns: 1fr;
  }
  .settings-nav {
    position: static;
    flex-direction: row;
    flex-wrap: wrap;
  }
  .sn-item {
    width: auto;
  }
}
</style>
