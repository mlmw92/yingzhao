<template>
  <Teleport to="body">
    <div v-if="visible" class="cmdk-mask" @click.self="close">
      <div
        class="cmdk"
        role="dialog"
        aria-modal="true"
        aria-label="命令面板"
        @keydown.tab.prevent
      >
        <div class="cmdk-in">
          <el-icon :size="15" class="cmdk-ic"><Search /></el-icon>
          <input
            ref="inputEl"
            v-model="query"
            class="cmdk-input"
            type="text"
            role="combobox"
            aria-label="搜索页面、主机或输入命令"
            aria-expanded="true"
            aria-controls="cmdk-listbox"
            aria-autocomplete="list"
            :aria-activedescendant="activeId"
            placeholder="搜索页面、主机，或输入命令…"
            autocomplete="off"
            spellcheck="false"
          />
          <kbd class="cmdk-kbd">ESC 关闭</kbd>
        </div>

        <div
          id="cmdk-listbox"
          ref="listEl"
          class="cmdk-list"
          :role="total > 0 ? 'listbox' : undefined"
          :aria-label="total > 0 ? '命令、页面与主机' : undefined"
        >
          <template v-if="total > 0">
            <template v-for="sec in sections" :key="sec.name">
              <div class="cmdk-sec">
                {{ sec.name }}
                <span v-if="sec.note" class="cmdk-sec-note">{{ sec.note }}</span>
              </div>
              <div
                v-for="it in sec.items"
                :id="'cmdk-opt-' + it.idx"
                :key="it.id"
                class="cmdk-row"
                :class="{ on: it.idx === activeIdx }"
                role="option"
                :aria-selected="it.idx === activeIdx"
                @mouseenter="activeIdx = it.idx"
                @click="choose(it.idx)"
              >
                <el-icon :size="14" class="cmdk-row-ic"><component :is="it.icon" /></el-icon>
                <span class="cmdk-row-label">{{ it.label }}</span>
                <span class="cmdk-row-rt">{{ it.hint }}</span>
              </div>
            </template>
          </template>
          <EmptyState
            v-else
            :icon="Search"
            title="没有匹配的页面、主机或命令"
            :hints="[
              '换个关键词试试，或直接点左侧菜单进入',
              query ? `当前关键词：${query}` : '支持搜索页面名称、主机名与命令',
            ]"
          />
        </div>

        <!-- 主机组状态：加载中 / 失败可重试（原实现失败后本次会话不再重试且无提示） -->
        <div v-if="!query && (hostsLoading || hostsError)" class="cmdk-status" :class="{ err: hostsError }">
          <template v-if="hostsError">
            <span role="alert">主机列表加载失败：{{ hostsError }}</span>
            <button type="button" class="cmdk-retry" @click="loadHosts(true)">重试</button>
          </template>
          <span v-else>正在加载主机…</span>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<script setup>
import { computed, markRaw, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import {
  Odometer, Monitor, Bell, Message, Connection, Document, Files, Setting,
  DataLine, Histogram, Grid, Lock, List, Aim, Tools, Search, Share,
  Brush, Refresh, DataAnalysis,
} from '@element-plus/icons-vue'
import http from '../api/http'
import { useAuth } from '../composables/useAuth'
import { useModules } from '../composables/useModules'
import { pathModule, pageDomain } from '../modules/catalog'
import EmptyState from './common/EmptyState.vue'

/* =========================================================
 * 数据源分三类：
 *  ① 页面导航 —— **只从 router 派生**（不手写第二份路由清单，否则必然漂移）。
 *     这里只提供「路由 name → 中文标题 / 图标 / 所属分组」的展示映射。
 *     新增页面时补一条即可；漏补时回退到路由 name，不会凭空消失。
 *  ② 命令 —— 面板内可执行的动作（切主题 / 刷新 / 直达大屏）。
 *  ③ 主机直达 —— 打开面板时拉一次并缓存。
 * ========================================================= */
const TITLES = {
  overview: '首页概览',
  hosts: '主机列表',
  middleware: '中间件监控',
  container: '容器与工作负载',
  logs: '集中日志',
  dialtest: '服务拨测',
  'metrics-explore': '指标浏览',
  'system-dashboards': '自定义仪表盘',
  assets: '资产台账',
  'assets-topology': '关系视图',
  inspect: '配置巡检',
  ops: '节点操作',
  alerts: '告警中心',
  intelligence: '智能分析',
  notify: '通知配置',
  report: '巡检报告',
  security: '安全中心',
  audit: '操作审计',
  'system-settings': '站点与品牌',
  'system-profile': '个人中心',
  'system-upgrade': '系统升级',
  'system-users': '用户管理',
  'system-roles': '角色与权限',
}

const ICONS = {
  overview: Odometer,
  hosts: Monitor,
  middleware: Connection,
  container: Grid,
  logs: List,
  dialtest: Aim,
  'metrics-explore': DataLine,
  'system-dashboards': Grid,
  assets: Files,
  'assets-topology': Share,
  inspect: Aim,
  ops: Tools,
  alerts: Bell,
  intelligence: Histogram,
  notify: Message,
  report: Document,
  security: Lock,
  audit: List,
  'system-settings': Setting,
  'system-profile': Setting,
  'system-upgrade': Setting,
  'system-users': Setting,
  'system-roles': Setting,
}

const GROUP_ORDER = ['概览', '观测监控', '资产与配置', '运维操作', '告警运维', '安全治理', '系统设置']
const GROUP_BY_PREFIX = [
  ['hosts', '观测监控'],
  ['middleware', '观测监控'],
  ['container', '观测监控'],
  ['logs', '观测监控'],
  ['dialtest', '观测监控'],
  ['metrics/', '观测监控'],
  ['system/dashboards', '观测监控'],
  ['assets', '资产与配置'],
  ['inspect', '资产与配置'],
  ['ops', '运维操作'],
  ['alerts', '告警运维'],
  ['intelligence', '告警运维'],
  ['notify', '告警运维'],
  ['report', '告警运维'],
  ['security', '安全治理'],
  ['audit', '安全治理'],
  ['system/', '系统设置'],
]

function groupOf(path) {
  if (path === '') return '概览'
  const hit = GROUP_BY_PREFIX.find(([p]) => path.startsWith(p))
  return hit ? hit[1] : '系统设置'
}

/* ---- 命令定义（面板内自足，不依赖页面上下文） ---- */
const THEME_NAMES = { b: '极光蓝', a: '星云青绿', c: '星河紫' }
function setTheme(t) {
  document.body.dataset.theme = t
  try { localStorage.setItem('nebula_theme', t) } catch (e) { /* ignore */ }
  // 图表组件监听此事件重新取色（与 AppTopBar 同一约定）
  window.dispatchEvent(new CustomEvent('nebula:theme-changed', { detail: t }))
}
const COMMANDS = [
  { id: 'cmd:theme:b', label: '切换主题：极光蓝', hint: '命令', icon: Brush, keywords: 'theme 主题 换肤 颜色 皮肤 极光蓝', run: () => setTheme('b') },
  { id: 'cmd:theme:a', label: '切换主题：星云青绿', hint: '命令', icon: Brush, keywords: 'theme 主题 换肤 颜色 皮肤 青绿 绿色', run: () => setTheme('a') },
  { id: 'cmd:theme:c', label: '切换主题：星河紫', hint: '命令', icon: Brush, keywords: 'theme 主题 换肤 颜色 皮肤 紫', run: () => setTheme('c') },
  { id: 'cmd:refresh', label: '刷新当前页数据', hint: '命令', icon: Refresh, keywords: 'refresh reload 刷新 重新加载 同步', run: () => window.dispatchEvent(new CustomEvent('nebula:refresh')) },
  { id: 'cmd:screen', label: '打开数据大屏', hint: '命令', icon: DataAnalysis, keywords: 'screen 大屏 监控墙 电视墙 dashboard', to: '/screen' },
  { id: 'cmd:home', label: '返回首页概览', hint: '命令', icon: Odometer, keywords: 'home 首页 概览 overview', to: '/' },
]

const HOST_LIMIT = 8
const RECENT_KEY = 'nebula-cmdk-recent'
const RECENT_MAX = 5

function loadRecent() {
  try {
    const a = JSON.parse(localStorage.getItem(RECENT_KEY) || '[]')
    return Array.isArray(a) ? a.filter((x) => typeof x === 'string') : []
  } catch (e) {
    return []
  }
}

const props = defineProps({
  // 主机直达的开关：需要 nodes:read，避免无权限账号看到一个必然 403 的分组
  enableHosts: { type: Boolean, default: true },
})

const router = useRouter()
const modules = useModules()
const auth = useAuth()

const visible = ref(false)
const query = ref('')
const activeIdx = ref(0)
const inputEl = ref(null)
const listEl = ref(null)
let lastFocused = null

const recentIds = ref(loadRecent())

const routeItems = computed(() => {
  const root = (router.options.routes || []).find((r) => r.path === '/')
  const children = (root && root.children) || []
  const out = []
  for (const c of children) {
    if (!c.name) continue
    // 需要路径参数的路由（如 node/:name）不能从面板直达
    if (c.path.includes(':')) continue
    if (c.meta && c.meta.perm && !auth.can(c.meta.perm)) continue
    // 与侧栏同一份判定：关闭的模块不出现在搜索结果里
    const moduleKey = pathModule['/' + c.path] || (c.meta && c.meta.module) || ''
    if (moduleKey && !modules.isVisible(moduleKey, pageDomain[moduleKey] || '')) continue
    const path = '/' + c.path
    out.push({
      id: 'page:' + c.name,
      label: TITLES[c.name] || c.name,
      hint: groupOf(c.path),
      icon: ICONS[c.name] || Grid,
      to: path,
    })
  }
  return out
})

// 主机直达：只在打开面板时拉一次并缓存（不新增接口）；失败可重试
const hostItems = ref([])
const hostsLoading = ref(false)
const hostsError = ref('')
let hostsLoaded = false
async function loadHosts(force = false) {
  if (!props.enableHosts || !auth.can('nodes:read')) return
  if (hostsLoaded && !force) return
  hostsLoading.value = true
  hostsError.value = ''
  try {
    const d = await http.get('/api/v1/nodes')
    hostItems.value = (d.nodes || [])
      .filter((n) => n.hostname)
      .map((n) => ({
        id: 'host:' + n.hostname,
        label: n.displayName || n.hostname,
        hint: n.group || '未分组',
        // markRaw：图标是组件对象，放进响应式数组会被 Vue 代理并告警
        icon: markRaw(Monitor),
        to: '/node/' + n.hostname,
      }))
    hostsLoaded = true
  } catch (e) {
    // 关键：失败时不置 hostsLoaded，下次打开/点重试仍会重新拉取
    hostsError.value = (e && e.message) || '服务不可用'
    hostsLoaded = false
  } finally {
    hostsLoading.value = false
  }
}

const routeGroups = computed(() => {
  const map = new Map()
  for (const it of routeItems.value) {
    if (!map.has(it.hint)) map.set(it.hint, [])
    map.get(it.hint).push(it)
  }
  return [...map.entries()]
    .map(([name, items]) => ({ name, items }))
    .sort((a, b) => GROUP_ORDER.indexOf(a.name) - GROUP_ORDER.indexOf(b.name))
})

const itemIndex = computed(() => {
  const m = new Map()
  for (const c of COMMANDS) m.set(c.id, c)
  for (const r of routeItems.value) m.set(r.id, r)
  for (const h of hostItems.value) m.set(h.id, h)
  return m
})

/* 相关度打分：子串命中优先（越靠前越高），否则退化为子序列匹配 */
function score(text, q) {
  if (!q) return 1
  const t = String(text).toLowerCase()
  const i = t.indexOf(q)
  if (i >= 0) return 1000 - i
  let ti = 0
  for (const ch of q) {
    const found = t.indexOf(ch, ti)
    if (found < 0) return -1
    ti = found + 1
  }
  return 1
}

function rank(items, q, toText) {
  const scored = []
  for (const it of items) {
    const s = q ? score(toText(it), q) : 1
    if (s < 0) continue
    scored.push({ it, s })
  }
  scored.sort((a, b) => b.s - a.s)
  return scored.map((x) => x.it)
}

const sections = computed(() => {
  const q = query.value.trim().toLowerCase()
  const out = []
  let idx = 0
  const emit = (name, items, note) => {
    if (!items.length) return
    out.push({ name, note, items: items.map((it) => ({ ...it, idx: idx++ })) })
  }

  if (!q) {
    const byId = itemIndex.value
    emit('最近访问', recentIds.value.map((id) => byId.get(id)).filter(Boolean))
  }

  emit('命令', rank(COMMANDS, q, (c) => `${c.label} ${c.keywords || ''}`))

  for (const g of routeGroups.value) {
    emit(g.name, rank(g.items, q, (it) => `${it.label} ${it.hint}`))
  }

  const hosts = rank(hostItems.value, q, (it) => `${it.label} ${it.hint}`)
  if (hosts.length) {
    const shown = hosts.slice(0, HOST_LIMIT)
    emit('主机', shown, hosts.length > shown.length ? `共 ${hosts.length} 台，继续输入以筛选` : '')
  }
  return out
})

// idx 是按渲染顺序连续分配的，所以条目总数就是各分组条目数之和
const total = computed(() => sections.value.reduce((n, s) => n + s.items.length, 0))
const activeId = computed(() => (total.value ? 'cmdk-opt-' + activeIdx.value : undefined))

watch(query, () => {
  activeIdx.value = 0
})

watch(activeIdx, async () => {
  await nextTick()
  const el = listEl.value && listEl.value.querySelector('.cmdk-row.on')
  if (el && el.scrollIntoView) el.scrollIntoView({ block: 'nearest' })
})

function open() {
  lastFocused = document.activeElement
  visible.value = true
  query.value = ''
  activeIdx.value = 0
  loadHosts()
  nextTick(() => {
    inputEl.value && inputEl.value.focus()
  })
}

function close() {
  visible.value = false
  const el = lastFocused
  lastFocused = null
  // 关闭后把焦点还给触发它的元素（顶栏搜索框/快捷键）
  if (el && typeof el.focus === 'function') nextTick(() => el.focus())
}

function setActive(i) {
  if (!total.value) return
  const n = total.value
  activeIdx.value = ((i % n) + n) % n
}

function move(delta) {
  setActive(activeIdx.value + delta)
}

function pushRecent(id) {
  const a = recentIds.value.filter((x) => x !== id)
  a.unshift(id)
  recentIds.value = a.slice(0, RECENT_MAX)
  try { localStorage.setItem(RECENT_KEY, JSON.stringify(recentIds.value)) } catch (e) { /* ignore */ }
}

function choose(i) {
  for (const sec of sections.value) {
    for (const it of sec.items) {
      if (it.idx === i) {
        pushRecent(it.id)
        close()
        if (it.run) it.run()
        else if (it.to) router.push(it.to)
        return
      }
    }
  }
}

function onKeydown(e) {
  const openHotkey = (e.metaKey || e.ctrlKey) && (e.key === 'k' || e.key === 'K')
  if (openHotkey) {
    e.preventDefault()
    visible.value ? close() : open()
    return
  }
  if (!visible.value) return
  if (e.key === 'Escape') {
    e.preventDefault()
    close()
  } else if (e.key === 'ArrowDown') {
    e.preventDefault()
    move(1)
  } else if (e.key === 'ArrowUp') {
    e.preventDefault()
    move(-1)
  } else if (e.key === 'Home') {
    e.preventDefault()
    setActive(0)
  } else if (e.key === 'End') {
    e.preventDefault()
    setActive(total.value - 1)
  } else if (e.key === 'PageDown') {
    e.preventDefault()
    move(5)
  } else if (e.key === 'PageUp') {
    e.preventDefault()
    move(-5)
  } else if (e.key === 'Enter') {
    e.preventDefault()
    choose(activeIdx.value)
  }
}

onMounted(() => {
  window.addEventListener('keydown', onKeydown)
})
onUnmounted(() => {
  window.removeEventListener('keydown', onKeydown)
})

defineExpose({ open, close, toggle: () => (visible.value ? close() : open()) })
</script>

<style scoped>
.cmdk-mask {
  position: fixed;
  inset: 0;
  z-index: 2000;
  background: rgba(0, 0, 0, 0.55);
  display: flex;
  justify-content: center;
  align-items: flex-start;
  padding-top: 15vh;
}
.cmdk {
  width: 560px;
  max-width: calc(100vw - 32px);
  background: var(--s3);
  border: 1px solid var(--bd-strong);
  border-radius: var(--r-lg);
  box-shadow: var(--sh-3);
  overflow: hidden;
}
.cmdk-in {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 14px;
  border-bottom: 1px solid var(--bd);
}
.cmdk-ic {
  color: var(--t3);
  flex-shrink: 0;
}
.cmdk-input {
  flex: 1;
  min-width: 0;
  background: transparent;
  border: none;
  outline: none;
  padding: 0;
  color: var(--t1);
  font-family: var(--font);
  font-size: var(--fs-md);
}
.cmdk-kbd {
  font-family: var(--mono);
  font-size: var(--fs-xs);
  padding: 1px 5px;
  border-radius: var(--r-xs);
  background: var(--fill-3);
  border: 1px solid var(--bd);
  color: var(--t3);
  flex-shrink: 0;
}
.cmdk-list {
  max-height: 320px;
  overflow-y: auto;
  padding: 6px;
}
.cmdk-sec {
  display: flex;
  align-items: baseline;
  gap: 8px;
  font-size: var(--fs-xs);
  font-weight: 600;
  letter-spacing: 0.06em;
  color: var(--t3);
  padding: 8px 10px 4px;
}
.cmdk-sec-note {
  font-weight: 400;
  letter-spacing: 0;
  color: var(--t3);
  opacity: 0.8;
}
.cmdk-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 10px;
  border-radius: var(--r-sm);
  font-size: var(--fs-base);
  color: var(--t2);
  cursor: pointer;
}
.cmdk-row.on {
  background: var(--accent-dim);
  color: var(--accent);
}
.cmdk-row-ic {
  flex-shrink: 0;
  opacity: 0.9;
}
.cmdk-row-label {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.cmdk-row-rt {
  margin-left: auto;
  font-size: var(--fs-xs);
  color: var(--t3);
  flex-shrink: 0;
}
.cmdk-status {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 16px 12px;
  font-size: var(--fs-xs);
  color: var(--t3);
}
.cmdk-status.err {
  color: var(--danger);
}
.cmdk-retry {
  border: 1px solid var(--bd-strong);
  background: transparent;
  color: inherit;
  border-radius: var(--r-xs);
  padding: 2px 8px;
  font-family: var(--font);
  font-size: var(--fs-xs);
  cursor: pointer;
}
.cmdk-retry:hover {
  background: var(--fill-hover);
}
</style>
