<template>
  <aside class="sidebar glass" :class="{ collapsed }">
      <div class="brand">
        <img v-if="brand.logo" :src="brand.logo" alt="logo" class="brand-logo" />
        <div class="brand-text" v-show="!collapsed">
          <h1 :title="brand.name">{{ brand.name }}</h1>
          <p v-if="brand.subtitle">{{ brand.subtitle }}</p>
        </div>
      </div>

    <nav class="nav">
      <el-tooltip content="首页概览" placement="right" :disabled="!collapsed">
        <router-link
          to="/"
          class="nav-item"
          :class="{ active: route.path === '/' }"
        >
          <el-icon :size="18"><Odometer /></el-icon>
          <span class="label" v-show="!collapsed">首页概览</span>
        </router-link>
      </el-tooltip>

      <div
        v-for="g in visibleGroups"
        :key="g.key"
        class="nav-group"
        :class="{ 'group-open': isGroupOpen(g), 'group-active': isGroupActive(g) }"
      >
        <el-tooltip :content="g.label" placement="right" :disabled="!collapsed">
          <div class="nav-group-title" @click="onGroupClick(g)">
            <el-icon :size="15"><component :is="g.icon" /></el-icon>
            <span class="label" v-show="!collapsed">{{ g.label }}</span>
            <el-icon v-show="!collapsed" class="caret"><ArrowDown v-if="isGroupOpen(g)" /><ArrowRight v-else /></el-icon>
          </div>
        </el-tooltip>
        <div v-show="!collapsed && isGroupOpen(g)" class="nav-group-items">
          <template v-for="sub in g.items" :key="sub.key">
            <!-- 免登录的独立页（对外状态页）：用新标签页打开，避免把管理台顶掉。
                 用原生 a + hash 链接：hash 路由下 " #/status " 就是它的地址，中间键/Ctrl+点击也照常工作。 -->
            <a
              v-if="sub.newTab"
              :href="'#' + sub.to"
              target="_blank"
              rel="noopener"
              class="nav-subitem"
            >
              <span class="sub-dot"></span>
              <span class="label">{{ sub.label }}</span>
              <el-icon class="ext-icon" :size="12"><TopRight /></el-icon>
            </a>
            <router-link
              v-else
              :to="sub.to"
              class="nav-subitem"
              :class="{ active: isActiveItem(sub) }"
            >
              <span class="sub-dot"></span>
              <span class="label">{{ sub.label }}</span>
              <el-badge
                v-if="sub.key === 'alerts' && alertCount > 0"
                :value="alertCount"
                :max="99"
                class="nav-badge"
              />
            </router-link>
          </template>
        </div>
      </div>
    </nav>

    <!-- 用户卡：头像 + 用户名 + 角色 -->
    <div class="nav-foot">
      <el-tooltip :content="username || 'admin'" placement="right" :disabled="!collapsed">
        <div class="avatar">{{ initials }}</div>
      </el-tooltip>
      <div v-show="!collapsed" class="u-meta">
        <div class="u-name">{{ username || 'admin' }}</div>
        <div class="u-sub">{{ roleText }}</div>
      </div>
    </div>

    <!-- 版本信息（统一取 Server 运行版本，不再区分 Web/Server 版本） -->
    <div class="version-info" v-show="!collapsed">
      <div class="ver-row">
        <span class="ver-label">版本</span>
        <span class="ver-val">{{ serverVersion }}</span>
      </div>
    </div>

    <div class="sidebar-footer">
      <el-tooltip :content="collapsed ? '展开侧栏' : '收起侧栏'" placement="right">
        <el-button link class="toggle-btn" @click="$emit('toggle')">
          <el-icon :size="18"><Fold v-if="!collapsed" /><Expand v-else /></el-icon>
          <span v-show="!collapsed" class="label">收起</span>
        </el-button>
      </el-tooltip>
    </div>
  </aside>
</template>

<script setup>
import { ref, computed, onMounted, watch } from 'vue'
import { useRoute } from 'vue-router'
import {
  Odometer,
  ArrowDown,
  ArrowRight,
  TopRight,
  // 收起/展开按钮的图标：此前模板用了 Fold/Expand 但没导入，
  // 渲染时解析不到组件，按钮上是个空图标（只在控制台留一条 warn）。
  Fold,
  Expand,
} from '@element-plus/icons-vue'
import { moduleGroups as groups } from '../modules/catalog'
import { useModules } from '../composables/useModules'
import http from '../api/http'
import { useBrand } from '../composables/useBrand'
import { useAuth } from '../composables/useAuth'
import { WEB_VERSION } from '../version'

const props = defineProps({
  collapsed: Boolean,
  alertCount: { type: Number, default: 0 },
  username: { type: String, default: '' },
  roleLabels: { type: Array, default: () => [] },
})
const emit = defineEmits(['toggle', 'logout'])

const route = useRoute()
const { brand } = useBrand()
const auth = useAuth()
const modules = useModules()

const serverVersion = ref(WEB_VERSION) // 初始用构建内嵌版本，加载后覆盖为 Server 实际运行版本

// 头像首字母：英文取前两位，中文取首字
const initials = computed(() => {
  const n = (props.username || '').trim()
  if (!n) return 'AD'
  return /^[\x00-\x7F]+$/.test(n) ? n.slice(0, 2).toUpperCase() : n.slice(0, 1)
})

const roleText = computed(() => {
  const labels = props.roleLabels || []
  return labels.length ? labels.join(' · ') : '用户'
})

// 按当前用户权限与模块开关过滤可见的菜单项
// （前端隐藏仅为体验，真正鉴权以服务端为准）。
// principal 未加载（单管理员/未启用 RBAC）时 auth.can 恒为 true，不隐藏。
function isItemVisible(group, sub) {
  if (!modules.isVisible(sub.key, group.key)) return false
  if (!sub.perm) return true
  return auth.can(sub.perm)
}

// 仅含可见子项的菜单分组（无可见子项、或整域被关闭的分组不显示）
const visibleGroups = computed(() =>
  groups
    .filter((g) => !modules.isDisabled(g.key))
    .map((g) => ({ ...g, items: g.items.filter((sub) => isItemVisible(g, sub)) }))
    .filter((g) => g.items.length > 0),
)

// 分组的显式展开/收起状态。
// 默认**全部展开**——此前只展开当前路由所在分组，导致 22 个页面全靠逐层点开；
// 这里只记录"用户手动收起过"的分组（false），未记录的一律视为展开。
const openGroups = ref({})

// 高亮按**精确路径**判定：此前用 startsWith，于是 /assets 把 /assets/topology、
// /assets/model 一并点亮（两个菜单同时高亮）。菜单项都是一级页面，没有需要靠前缀
// 覆盖的详情路由，精确比较即可；详情路由（如 /node/:name）不在菜单里。
function isActiveItem(item) {
  if (item.key === 'overview') return route.path === '/'
  return route.path === item.to
}

function isGroupActive(g) {
  return g.items.some(isActiveItem)
}

function isGroupOpen(g) {
  return openGroups.value[g.key] !== false
}

function onGroupClick(g) {
  if (props.collapsed) {
    // 折叠态点击分组：先展开侧边栏，再展开该分组
    emit('toggle')
    openGroups.value[g.key] = true
    return
  }
  openGroups.value[g.key] = !isGroupOpen(g)
}

async function loadVersion() {
  try {
    const ver = await http.get('/api/v1/version')
    serverVersion.value = ver.server || '-'
  } catch (e) {
    serverVersion.value = '-'
  }
}

// 路由切换时确保目标分组是展开的（用户此前手动收起过也要重新展开，
// 否则会出现"点了命令面板跳转过来，却看不到自己在哪"的情况）。
watch(
  () => route.path,
  () => {
    const active = groups.find(isGroupActive)
    if (active && openGroups.value[active.key] === false) {
      openGroups.value[active.key] = true
    }
  }
)

onMounted(() => {
  loadVersion()
  modules.load()
})
</script>

<style scoped>
.sidebar {
  position: fixed;
  left: 0;
  top: 0;
  bottom: 0;
  width: var(--sidebar-w);
  display: flex;
  flex-direction: column;
  padding: 16px 12px;
  border-radius: 0;
  border-right: 1px solid var(--bd);
  border-left: none;
  border-top: none;
  border-bottom: none;
  z-index: 50;
  transition: width 0.2s;
  overflow: hidden;
}
.sidebar.collapsed {
  width: var(--sidebar-w-collapsed);
}
.brand {
  display: flex;
  align-items: center;
  gap: 10px;
  /* 顶栏下边框落在 56px（--topbar-h），品牌区的下边框必须落在同一条线上，
     否则壳层顶部两条同级分隔线会撕开一道断口。侧栏自身 padding-top 是 16px，
     所以品牌区高度取 40px（16 + 40 = 56），而不是此前算错账的 52px（落到 68px）。 */
  padding: 3px 6px;
  height: 40px;
  border-bottom: 1px solid var(--bd);
  margin-bottom: 14px;
}
.brand-logo {
  width: 34px;
  height: 34px;
  border-radius: var(--r-lg);
  object-fit: contain;
  flex-shrink: 0;
  background: var(--fill-2);
  box-shadow: 0 0 16px var(--accent-glow);
}
.brand-text {
  min-width: 0;
  overflow: hidden;
}
.brand-text h1 {
  font-size: 17px;
  font-weight: 700;
  letter-spacing: 0.03em;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.brand-text p {
  font-size: 13px;
  color: var(--t2);
}
.nav {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 4px;
  /* 菜单超出视口时必须能滚动：此前这里没有 overflow，而 .sidebar 是 overflow: hidden，
     于是多展开几个分组后底部（系统设置里的升级/用户/角色）直接被裁掉且无法滚动到。
     min-height: 0 是必须的——flex 子项默认 min-height:auto 不会收缩，加了 overflow 也不生效。 */
  min-height: 0;
  overflow-y: auto;
  overflow-x: hidden;
}
.nav-item {
  display: flex;
  align-items: center;
  gap: 10px;
  height: 36px;
  padding: 0 10px;
  color: var(--t2);
  font-size: var(--fs-base);
  border-radius: var(--r-sm);
  text-decoration: none;
  transition: background var(--dur-1) var(--ease), color var(--dur-1) var(--ease);
  position: relative;
}
.nav-item:hover {
  background: var(--fill-1);
  color: var(--t1);
}
.nav-item.active {
  background: var(--accent-dim);
  color: var(--accent);
  font-weight: 600;
}
.nav-item.active::before {
  content: '';
  position: absolute;
  left: -12px;
  top: 50%;
  transform: translateY(-50%);
  width: 3px;
  height: 18px;
  background: var(--accent);
  border-radius: 0 2px 2px 0;
}
.label {
  white-space: nowrap;
}
.nav-badge {
  margin-left: auto;
}
/* 分组菜单：分组标题是"章节标签"，刻意做小、做淡，与子项拉开层级差 */
.nav-group {
  margin-top: 6px;
}
.nav-group-title {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 6px 10px;
  color: var(--t3);
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.06em;
  border-radius: var(--r-sm);
  cursor: pointer;
  transition: background var(--dur-1) var(--ease), color var(--dur-1) var(--ease);
  user-select: none;
}
.nav-group-title:hover {
  background: var(--fill-1);
  color: var(--t2);
}
.nav-group.group-active .nav-group-title {
  color: var(--accent);
}
.nav-group-title .caret {
  margin-left: auto;
  transition: transform 0.15s;
}
.nav-group-items {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 2px 0 2px 24px;
  position: relative;
}
.nav-subitem {
  position: relative;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 7px 10px;
  color: var(--t2);
  font-size: var(--fs-base);
  border-radius: var(--r-sm);
  text-decoration: none;
  transition: background var(--dur-1) var(--ease), color var(--dur-1) var(--ease);
}
.nav-subitem:hover {
  background: var(--fill-1);
  color: var(--t1);
}
.nav-subitem.active {
  background: var(--accent-dim);
  color: var(--accent);
  font-weight: 600;
}
/* 活动项左侧光条：与 .nav-item.active::before 落在同一条竖线上。
   偏移量要一路抵消到侧栏内缘：侧栏自身 padding-left 12px（.sidebar）
   + 子项容器 padding-left 24px（.nav-group-items）= 36px，故取 -36px。
   此前取 -24px 漏算了侧栏那 12px，两条光条实际错开 12px。 */
.nav-subitem.active::before {
  content: '';
  position: absolute;
  left: -36px;
  top: 50%;
  transform: translateY(-50%);
  width: 3px;
  height: 18px;
  background: var(--accent);
  border-radius: 0 2px 2px 0;
}
.sub-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: currentColor;
  opacity: 0.6;
  flex-shrink: 0;
}
/* 新标签页打开的入口（对外状态页）：给一个 ↖ 提示，
   避免"点了怎么多出一个页签"的困惑；同时区分于普通菜单项 */
.ext-icon {
  margin-left: auto;
  color: var(--t3);
}
/* 用户卡：头像 + 用户名 + 角色 · 版本 */
.nav-foot {
  margin-top: 8px;
  padding: 10px 4px 2px;
  border-top: 1px solid var(--bd);
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}
.avatar {
  width: 30px;
  height: 30px;
  border-radius: 50%;
  flex-shrink: 0;
  background: var(--accent-dim);
  border: 1px solid var(--accent);
  color: var(--accent);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.02em;
}
.u-meta {
  min-width: 0;
}
.u-name {
  font-size: var(--fs-sm);
  font-weight: 600;
  color: var(--t1);
  line-height: 1.3;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.u-sub {
  font-size: 12px;
  color: var(--t3);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.sidebar.collapsed .nav-foot {
  justify-content: center;
  padding-left: 0;
  padding-right: 0;
}
/* 版本信息（保持与改造前一致的呈现：左侧「版本」标签 + 右侧等宽字体版本号） */
.version-info {
  padding: 8px 12px;
  border-top: 1px solid var(--bd);
  display: flex;
  flex-direction: column;
  gap: 3px;
}
.ver-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: 13px;
}
.ver-label {
  color: var(--t3);
}
.ver-val {
  color: var(--t2);
  font-family: var(--mono);
  font-size: 13px;
}
.sidebar-footer {
  padding-top: 6px;
}
.toggle-btn {
  width: 100%;
  justify-content: flex-start;
  gap: 12px;
  color: var(--t2);
  height: 36px;
  padding: 0 12px;
}
.sidebar.collapsed .toggle-btn {
  justify-content: center;
}
</style>
