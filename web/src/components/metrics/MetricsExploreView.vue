<template>
  <section class="explore-view">
    <PageHeader
      title="指标浏览"
      desc="按分类浏览指标目录，查看趋势并导出；受限用户不会携带范围外节点参数"
    >
      <template #actions>
        <el-button size="small" :disabled="!selected" @click="onExport">导出 CSV</el-button>
        <el-button size="small" type="primary" :disabled="!selected" @click="openAddToDash">加入仪表盘</el-button>
      </template>
    </PageHeader>

    <div class="explore">
      <div class="left">
        <div class="left-head-row">
          <div class="left-head">指标目录</div>
          <span v-if="activeLoaded" class="discover-count">{{ activeCount }}/{{ metricCount }} 有数据</span>
          <span v-else class="discover-count muted">检测中…</span>
        </div>
        <el-input v-model="kw" placeholder="搜索指标名/中文名" size="small" clearable class="kw" />
        <el-tree
          :data="treeData"
          :props="{ label: 'label', children: 'children' }"
          node-key="key"
          highlight-current
          @node-click="onNodeClick"
          default-expand-all
        >
          <template #default="{ data }">
            <span class="metric-tree-node">
              <span>{{ data.label }}</span>
              <StatusPill
                v-if="data.meta"
                :tone="data.meta.active ? 'ok' : 'muted'"
                size="sm"
                dot
                :text="data.meta.active ? '有数据' : '暂无数据'"
              />
            </span>
          </template>
        </el-tree>
      </div>

      <div class="right">
        <EmptyState
          v-if="!selected"
          title="从左侧选择指标查看趋势"
          :hints="['点击目录中的指标名即可加载趋势图', '支持时间范围切换与标签过滤（key=value）']"
        />
        <template v-else>
          <div class="filter-row">
            <el-select v-model="range" size="small" class="rng" @change="renderChart">
              <el-option label="近 1 小时" value="1h" />
              <el-option label="近 6 小时" value="6h" />
              <el-option label="近 24 小时" value="24h" />
              <el-option label="近 7 天" value="7d" />
            </el-select>
            <el-input
              v-model="labelFilter"
              size="small"
              clearable
              class="lf"
              placeholder="标签过滤：instance=10.0.0.1:9100，多组用逗号分隔"
              @change="renderChart"
              @clear="renderChart"
            />
            <el-checkbox v-model="showLegend" size="small" @change="renderChart">图例</el-checkbox>
            <span v-if="labelError" class="lf-err">{{ labelError }}</span>
          </div>

          <SectionCard
            class="chart-card"
            dense
            :title="selected.title"
            :subtitle="`${selected.name} · 分类 ${selected.category} · 单位 ${selected.unit || '-'} · 推荐图表 ${selected.chart}`"
          >
            <div class="chart-wrap">
              <div ref="chartEl" class="chart"></div>
              <div v-if="chartLoading" class="chart-state">
                <span class="chart-spin"></span>
                <span>加载中…</span>
              </div>
              <div v-else-if="chartError" class="chart-state error">
                <span class="chart-err-msg">{{ chartError }}</span>
                <el-button size="small" @click="renderChart">重试</el-button>
              </div>
              <EmptyState
                v-else-if="chartEmpty"
                size="table-inline"
                title="该时间范围内没有数据"
                :hints="['换个时间范围，或清空标签过滤后重试']"
              />
            </div>
          </SectionCard>
        </template>
      </div>
    </div>

    <!-- 「加入仪表盘」：选择已有看板或新建（原实现固定写进「我的看板」，用户无法选择） -->
    <el-dialog v-model="addDlg" title="加入仪表盘" width="440px">
      <el-form label-width="90px">
        <el-form-item label="目标看板">
          <el-select v-model="addTarget" style="width:100%">
            <el-option v-for="d in dashList" :key="d.id" :label="d.name" :value="d.id" />
            <el-option label="+ 新建看板" value="__new__" />
          </el-select>
        </el-form-item>
        <el-form-item v-if="addTarget === '__new__'" label="看板名称">
          <el-input v-model="addNewName" placeholder="请输入看板名称" maxlength="50" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="addDlg = false">取消</el-button>
        <el-button type="primary" @click="confirmAddToDash">确定</el-button>
      </template>
    </el-dialog>
  </section>
</template>

<script setup>
import { ref, computed, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { ElMessage } from 'element-plus'
import { initChart, monitorOption, COLORS } from '../../charts/echarts'
import http, { get as httpGet, metricCatalog, metricActive } from '../../api/http'
import { useDashboards } from '../../composables/useDashboards'
import PageHeader from '../common/PageHeader.vue'
import SectionCard from '../common/SectionCard.vue'
import EmptyState from '../common/EmptyState.vue'
import StatusPill from '../common/StatusPill.vue'

const kw = ref('')
const catalog = ref({})
const categories = ref([])
const activeMap = ref({})
const activeLoaded = ref(false)
const selected = ref(null)
const chartEl = ref(null)
// 时间范围：rangeBounds 本来就支持 4 档，此前 renderChart/onExport 各写死 '1h'
const range = ref('1h')
// 标签过滤：用户输入 `k=v`，多组逗号分隔；后端按 `?labels.<k>=<v>` 接收
// （parseLabelQuery，internal/server/api/query.go:1230），这里只负责拼串。
const labelFilter = ref('')
const labelError = ref('')
// 图表三态：骨架 / 空 / 错误（原先失败只弹 toast，图区留下上一张旧图，容易被误读为"查询成功"）
const chartLoading = ref(false)
const chartError = ref('')
const chartEmpty = ref(false)
const showLegend = ref(true)
let chart = null
const dash = useDashboards()

// 加入仪表盘弹窗
const addDlg = ref(false)
const dashList = ref([])
const addTarget = ref('__new__')
const addNewName = ref('')

const treeData = computed(() => {
  const list = []
  const cats = categories.value
  for (const cat of cats) {
    const items = (catalog.value[cat] || []).filter((m) => {
      if (!kw.value) return true
      const k = kw.value.toLowerCase()
      return m.name.toLowerCase().includes(k) || (m.title || '').toLowerCase().includes(k)
    })
    if (!items.length) continue
    list.push({
      key: 'cat:' + cat,
      label: cat,
      children: items.map((m) => ({
        key: 'm:' + m.name,
        label: `${m.title} (${m.name})`,
        meta: { ...m, active: activeMap.value[m.name] === true },
      })),
    })
  }
  return list
})

const metricCount = computed(() => Object.values(catalog.value).reduce((n, list) => n + (list || []).length, 0))
const activeCount = computed(() => Object.values(activeMap.value).filter(Boolean).length)

function onNodeClick(node) {
  if (node.meta) {
    selected.value = node.meta
    nextTick(() => renderChart())
  }
}

function rangeBounds(r) {
  const now = Date.now()
  switch (r || '1h') {
    case '6h': return { start: now - 6 * 3600000, end: now, step: 60000 }
    case '24h': return { start: now - 24 * 3600000, end: now, step: 300000 }
    case '7d': return { start: now - 7 * 86400000, end: now, step: 1800000 }
    default: return { start: now - 3600000, end: now, step: 60000 }
  }
}

// 解析标签过滤输入。返回 { labels } 或 { error }——格式不对时绝不能"忽略过滤条件继续查"，
// 否则用户以为过滤生效了，实际看到的是全量序列。
function parseLabels() {
  const out = {}
  const parts = String(labelFilter.value || '')
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
  for (const p of parts) {
    const i = p.indexOf('=')
    if (i <= 0) return { error: `「${p}」格式不对，应写成 key=value` }
    const k = p.slice(0, i).trim()
    const v = p.slice(i + 1).trim()
    if (!k || !v) return { error: `「${p}」格式不对，应写成 key=value` }
    out[k] = v
  }
  return { labels: out }
}

async function renderChart() {
  if (!selected.value) return
  const parsed = parseLabels()
  labelError.value = parsed.error || ''
  if (parsed.error) {
    ElMessage.warning(parsed.error)
    return
  }
  const lq = Object.entries(parsed.labels)
    .map(([k, v]) => `&labels.${encodeURIComponent(k)}=${encodeURIComponent(v)}`)
    .join('')
  await nextTick()
  if (!chartEl.value) {
    ElMessage.error('图表容器未准备好，请稍后重试')
    return
  }
  try {
    if (!chart || (typeof chart.isDisposed === 'function' && chart.isDisposed())) {
      chart = initChart(chartEl.value)
    }
  } catch (e) {
    ElMessage.error('图表初始化失败：' + (e.message || e))
    return
  }
  chartError.value = ''
  chartEmpty.value = false
  chartLoading.value = true
  const { start, end, step } = rangeBounds(range.value)
  try {
    const d = await httpGet(`/api/v1/query/range?metric=${encodeURIComponent(selected.value.name)}&start=${start}&end=${end}&step=${step}${lq}`)
    const data = (d.series || d.data || []).map((s, index) => {
      const labels = s.labels || {}
      const suffix = labels.instance || labels.node || labels.host || `序列 ${index + 1}`
      return {
        name: `${selected.value.name}·${suffix}`,
        data: (s.points || []).map((p) => [Number(p.timestamp), Number(p.value)]),
        color: COLORS.cyan,
      }
    })
    chartEmpty.value = !data.length || data.every((s) => !s.data.length)
    const opt = monitorOption({
      xMin: start,
      xMax: end,
      series: data,
      chartType: selected.value.chart || 'line',
      unit: selected.value.unit,
      yMin: selected.value.unit === '%' ? 0 : undefined,
      yMax: selected.value.unit === '%' ? 100 : undefined,
    })
    if (!showLegend.value) opt.legend = undefined
    chart.clear()
    chart.setOption(opt, true)
  } catch (e) {
    chartError.value = '查询失败：' + (e.message || e)
    ElMessage.error(chartError.value)
  } finally {
    chartLoading.value = false
  }
}

function onExport() {
  if (!selected.value) return
  const parsed = parseLabels()
  labelError.value = parsed.error || ''
  if (parsed.error) {
    ElMessage.warning(parsed.error)
    return
  }
  // step 此前写死 60000，7d 档下会拉出 10080 个点；跟图表用同一档
  const { start, end, step } = rangeBounds(range.value)
  const params = { metric: selected.value.name, start, end, step, filename: `metric_${selected.value.name}.csv` }
  for (const [k, v] of Object.entries(parsed.labels)) params['labels.' + k] = v
  http.exportMetricCSV(params).catch((e) => ElMessage.error('导出失败：' + (e.message || e)))
}

function buildPanel(parsed) {
  return {
    title: selected.value.title,
    chartType: selected.value.chart || 'line',
    metric: selected.value.name,
    range: range.value,
    step: 0,
    // panel.labels 把当前过滤条件存进去，加进看板后还是同一批序列
    labels: parsed.labels,
    size: 'half',
  }
}

// 打开「加入仪表盘」弹窗：拉取已有看板供选择
async function openAddToDash() {
  if (!selected.value) return
  const parsed = parseLabels()
  labelError.value = parsed.error || ''
  if (parsed.error) {
    ElMessage.warning(parsed.error)
    return
  }
  try {
    const list = await dash.load(true)
    dashList.value = Array.isArray(list) ? list : []
  } catch (_) {
    dashList.value = []
  }
  addTarget.value = dashList.value.length ? dashList.value[0].id : '__new__'
  addNewName.value = selected.value.title || '我的看板'
  addDlg.value = true
}

async function confirmAddToDash() {
  if (!selected.value) return
  const parsed = parseLabels()
  if (parsed.error) {
    ElMessage.warning(parsed.error)
    return
  }
  const panel = buildPanel(parsed)
  try {
    if (addTarget.value === '__new__') {
      const name = (addNewName.value || '').trim()
      if (!name) {
        ElMessage.warning('请输入看板名称')
        return
      }
      await dash.create(name, [panel])
      ElMessage.success('已创建「' + name + '」并加入指标')
    } else {
      const exist = dashList.value.find((d) => d.id === addTarget.value)
      if (!exist) {
        ElMessage.error('目标看板不存在')
        return
      }
      const panels = (exist.panels || []).concat(panel)
      await dash.update(exist.id, exist.name, panels)
      ElMessage.success('已添加到「' + exist.name + '」')
    }
    addDlg.value = false
  } catch (e) {
    ElMessage.error(e.message || String(e))
  }
}

onMounted(async () => {
  try {
    const d = await metricCatalog()
    catalog.value = d.catalog || {}
    categories.value = d.categories || []
    try {
      const a = await metricActive()
      activeMap.value = Object.fromEntries((a.items || []).map((item) => [item.name, item.active === true]))
    } catch (e) {
      // 自动发现失败不影响指标目录浏览，状态保持为“暂无数据/未检测”。
      ElMessage.warning('自动发现状态暂时不可用')
    }
  } catch (e) {
    ElMessage.error('加载指标目录失败：' + (e.message || e))
  } finally {
    activeLoaded.value = true
  }
})
onBeforeUnmount(() => {
  // 尺寸自适应已由 initChart 内置的 ResizeObserver 负责（侧栏折叠/断点变化都能覆盖），
  // 不再额外监听 window.resize（那条只在窗口缩放时触发，且与内置观察器重复）。
  chart && chart.dispose()
})
</script>

<style scoped>
/* 原来这里全是写死的十六进制色（var(--t1) / var(--ok) / var(--t3)），
   切到青绿或紫色主题时整页不跟随。全部收敛到令牌。 */
.explore-view { display: flex; flex-direction: column; gap: 12px; }
/* 140px 只算了顶栏，没算页脚（约 60px，且页脚按品牌配置可显隐），
   于是要么页面与内容区双滚动，要么底部空一截。留出 260px（含 PageHeader）并用下限兜住矮屏。 */
.explore { display: flex; height: clamp(420px, calc(100vh - 260px), 1400px); }
.left { width: 320px; border-right: 1px solid var(--bd); padding: 12px; overflow: auto; }
.left-head-row { display: flex; align-items: center; justify-content: space-between; margin-bottom: 8px; }
.left-head { font-size: var(--fs-md); font-weight: 700; color: var(--t1); }
.discover-count { font-size: var(--fs-sm); color: var(--ok); }
.discover-count.muted { color: var(--t3); }
.metric-tree-node { display: flex; align-items: center; justify-content: space-between; gap: 8px; width: 100%; min-width: 0; }
.kw { margin-bottom: 8px; }
.right { flex: 1; padding: 16px; min-width: 0; display: flex; flex-direction: column; }
.right :deep(.empty-state) { margin-top: 40px; }
.right-head { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 10px; }
/* 时间范围 + 标签过滤。宽度走 §1.5 令牌：选择框 S 档 130，输入框 XL 档 320。 */
.filter-row { display: flex; align-items: center; gap: 8px; margin-bottom: 10px; flex-wrap: wrap; }
.rng { width: 130px; }
.lf { width: 320px; max-width: 100%; }
.lf-err { font-size: var(--fs-sm); color: var(--danger); }
.m-title { font-size: var(--fs-lg); font-weight: 700; color: var(--t1); }
.m-name { font-size: var(--fs-sm); color: var(--t3); font-weight: 400; }
.m-meta { font-size: var(--fs-sm); color: var(--t2); margin-top: 4px; }
/* 图表外观收敛进 SectionCard：卡片撑满剩余高度，不再依赖猜出来的头部高度 */
.chart-card { flex: 1; min-height: 0; }
.chart-card :deep(.sec-body) { display: flex; min-height: 0; }
.chart-wrap { position: relative; flex: 1; min-height: 280px; }
.chart { width: 100%; height: 100%; background: var(--s1); border: 1px solid var(--bd); border-radius: var(--r-lg); }
.chart-state {
  position: absolute; inset: 0; display: flex; align-items: center; justify-content: center;
  gap: 8px; font-size: var(--fs-sm); color: var(--t3); background: var(--s1); border-radius: var(--r-lg);
}
.chart-state.error { color: var(--danger); }
.chart-err-msg { max-width: 70%; overflow-wrap: break-word; }
.chart-spin {
  width: 14px; height: 14px; border: 2px solid var(--bd); border-top-color: var(--accent);
  border-radius: 50%; animation: chart-spin 0.7s linear infinite;
}
@keyframes chart-spin { to { transform: rotate(360deg); } }
@media (max-width: 1024px) {
  .explore { flex-direction: column; height: auto; }
  .left { width: auto; border-right: none; border-bottom: 1px solid var(--bd); }
}
</style>
