<template>
  <section class="dash">
    <PageHeader
      title="自定义仪表盘"
      desc="把关心的指标曲线组合成看板；编辑态可拖拽排序、切换半宽/全宽"
    >
      <template #actions>
        <el-button size="small" @click="newDash">+ 新建看板</el-button>
        <template v-if="active">
          <el-button v-if="!editing" size="small" type="primary" @click="enterEdit">编辑</el-button>
          <template v-else>
            <el-button size="small" @click="addPanel">+ 添加面板</el-button>
            <el-button size="small" type="success" @click="save">保存</el-button>
            <el-button size="small" @click="cancelEdit">取消</el-button>
          </template>
          <el-button size="small" plain @click="renameDash">重命名</el-button>
          <el-popconfirm title="确认删除该看板？" @confirm="delDash">
            <template #reference>
              <el-button size="small" type="danger" plain>删除看板</el-button>
            </template>
          </el-popconfirm>
        </template>
      </template>
    </PageHeader>

    <!-- 看板切换改用全站统一的次级导航条：与主机列表等页面的次级导航一致 -->
    <div v-if="dashboards.length" class="subnav">
      <span
        v-for="d in dashboards"
        :key="d.id"
        class="subnav-item"
        :class="{ on: d.id === activeId }"
        @click="selectDash(d.id)"
      >{{ d.name }}</span>
    </div>

    <!-- 首屏骨架：数据未回来前先占位，避免先渲染「暂无看板」再整块翻成网格 -->
    <SectionCard v-if="loading && !dashboards.length" title="看板" dense :loading="true" />

    <el-alert
      v-else-if="loadError"
      class="dash-err"
      type="error"
      :closable="false"
      show-icon
      :title="'看板列表加载失败：' + loadError"
    >
      <template #default>
        <el-button size="small" @click="refresh">重试</el-button>
      </template>
    </el-alert>

    <EmptyState
      v-else-if="!active"
      title="暂无看板"
      :hints="['点上方「+ 新建看板」创建第一个自定义仪表盘', '看板由若干面板组成，每个面板绑定一个指标与图表类型']"
      action-text="新建看板"
      @action="newDash"
    />

    <div v-else-if="activePanels.length" class="grid">
      <div
        v-for="(p, i) in activePanels"
        :key="p._id"
        class="panel-wrap"
        :class="{ 'span-full': p.size === 'full', dragging: dragFrom === i }"
        :draggable="editing"
        @dragstart="onDragStart(i, $event)"
        @dragover.prevent
        @drop="onDrop(i)"
        @dragend="onDragEnd"
      >
        <PanelChart
          :panel="p"
          :editing="editing"
          :unit="unitOf(p.metric)"
          @edit="editPanel(i)"
          @remove="removePanel(i)"
          @toggle-size="toggleSize(i)"
        />
      </div>
    </div>

    <!-- 编辑态下把面板全删掉后 .grid 会塌陷成 0 高。这里补「面板已清空」空态；
         点它的「添加面板」会自动进入编辑态（修 B6：原实现直接写 draft 触发空指针）。 -->
    <EmptyState
      v-else
      title="这个看板还没有面板"
      :hints="['点「添加面板」把指标曲线放进来', '每个面板绑定一个指标与一种图表类型']"
      action-text="添加面板"
      @action="addPanel"
    />

    <!-- 面板配置弹窗 -->
    <el-dialog v-model="panelDlg" title="面板配置" width="460px">
      <el-form label-width="90px">
        <el-form-item label="面板标题"><el-input v-model="editForm.title" /></el-form-item>
        <el-form-item label="指标">
          <el-select v-model="editForm.metric" filterable placeholder="选择指标" style="width:100%">
            <el-option v-for="m in flatMetrics" :key="m.name" :label="`${m.title} (${m.name})`" :value="m.name" />
          </el-select>
        </el-form-item>
        <el-form-item label="图表类型">
          <el-select v-model="editForm.chartType" style="width:100%">
            <el-option label="折线" value="line" />
            <el-option label="面积" value="area" />
            <el-option label="柱状" value="bar" />
            <el-option label="仪表" value="gauge" />
          </el-select>
        </el-form-item>
        <el-form-item label="宽度">
          <el-radio-group v-model="editForm.size">
            <el-radio-button value="half">半宽</el-radio-button>
            <el-radio-button value="full">全宽</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="限定主机">
          <el-select v-model="editForm.node" filterable clearable allow-create placeholder="留空=全部" style="width:100%">
            <el-option v-for="n in nodeList" :key="n.hostname" :label="n.displayName || n.hostname" :value="n.hostname" />
          </el-select>
        </el-form-item>
        <el-form-item label="时间范围">
          <el-select v-model="editForm.range" style="width:100%">
            <el-option label="近1小时" value="1h" />
            <el-option label="近6小时" value="6h" />
            <el-option label="近24小时" value="24h" />
            <el-option label="近7天" value="7d" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="panelDlg = false">取消</el-button>
        <el-button type="primary" @click="savePanel">确定</el-button>
      </template>
    </el-dialog>

    <!-- 新建看板弹窗：避免使用浏览器原生 prompt，保持界面风格和校验一致。 -->
    <el-dialog v-model="newDashDlg" title="新建看板" width="420px" @closed="newDashName = ''">
      <el-form @submit.prevent="confirmNewDash">
        <el-form-item label="看板名称" label-width="90px">
          <el-input v-model="newDashName" placeholder="请输入看板名称" maxlength="50" show-word-limit @keyup.enter="confirmNewDash" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="newDashDlg = false">取消</el-button>
        <el-button type="primary" @click="confirmNewDash">创建</el-button>
      </template>
    </el-dialog>

    <!-- 重命名看板弹窗 -->
    <el-dialog v-model="renameDlg" title="重命名看板" width="420px">
      <el-form @submit.prevent="confirmRename">
        <el-form-item label="看板名称" label-width="90px">
          <el-input v-model="renameName" placeholder="请输入看板名称" maxlength="50" show-word-limit @keyup.enter="confirmRename" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="renameDlg = false">取消</el-button>
        <el-button type="primary" @click="confirmRename">保存</el-button>
      </template>
    </el-dialog>
  </section>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import PanelChart from './PanelChart.vue'
import http from '../../api/http'
import { useDashboards } from '../../composables/useDashboards'
import PageHeader from '../common/PageHeader.vue'
import SectionCard from '../common/SectionCard.vue'
import EmptyState from '../common/EmptyState.vue'

const ACTIVE_KEY = 'yingzhao_dash_active'

const { state, load, create, update, remove } = useDashboards()
const dashboards = computed(() => state.dashboards)
const activeId = ref('')
const editing = ref(false)
const draft = ref(null)
const panelDlg = ref(false)
const newDashDlg = ref(false)
const newDashName = ref('')
const renameDlg = ref(false)
const renameName = ref('')
const editIndex = ref(-1)
const editForm = ref({ title: '', metric: '', chartType: 'line', node: '', range: '1h', size: 'half' })
const flatMetrics = ref([])
const nodeList = ref([])
const loading = ref(false)
const loadError = ref('')
const dragFrom = ref(-1)

const active = computed(() => dashboards.value.find((d) => d.id === activeId.value) || null)
const activePanels = computed(() => {
  const d = active.value
  if (!d) return []
  return editing.value ? (draft.value ? draft.value.panels : []) : d.panels
})

// 面板稳定 id：用 WeakMap 绑定对象身份，同一面板对象在多次渲染间拿到同一 key。
// 原实现 :key="i" 在删除/插入时会让 ECharts 实例与面板数据错配。
let idSeq = 0
function ensureIds() {
  for (const d of dashboards.value) {
    for (const p of (d.panels || [])) if (!p._id) p._id = 'pnl-' + (++idSeq)
  }
}

function unitOf(metric) {
  const m = flatMetrics.value.find((x) => x.name === metric)
  return m ? m.unit || '' : ''
}

async function refresh() {
  loading.value = true
  loadError.value = ''
  try {
    await load(true)
    ensureIds()
    if (state.error) loadError.value = state.error
    // 恢复上次打开的看板；不存在则回退到第一个
    const saved = activeId.value || localStorage.getItem(ACTIVE_KEY) || ''
    const exists = dashboards.value.some((d) => d.id === saved)
    activeId.value = exists ? saved : (dashboards.value[0] ? dashboards.value[0].id : '')
  } finally {
    loading.value = false
  }
}

function selectDash(id) {
  if (editing.value) cancelEdit()
  activeId.value = id
  try { localStorage.setItem(ACTIVE_KEY, id) } catch (_) { /* 忽略隐私模式 */ }
}

function newDash() {
  newDashName.value = ''
  newDashDlg.value = true
}

function confirmNewDash() {
  const name = newDashName.value.trim()
  if (!name) {
    ElMessage.warning('请输入看板名称')
    return
  }
  create(name, [])
    .then(() => {
      newDashDlg.value = false
      return refresh()
    })
    .catch((e) => ElMessage.error(e.message || e))
}

function renameDash() {
  if (!active.value) return
  renameName.value = active.value.name
  renameDlg.value = true
}

function confirmRename() {
  const name = renameName.value.trim()
  if (!name) {
    ElMessage.warning('请输入看板名称')
    return
  }
  const panels = active.value.panels || []
  update(active.value.id, name, panels)
    .then(() => {
      renameDlg.value = false
      return refresh()
    })
    .catch((e) => ElMessage.error(e.message || e))
}

function delDash() {
  if (!active.value) return
  remove(active.value.id).then(() => {
    activeId.value = ''
    try { localStorage.removeItem(ACTIVE_KEY) } catch (_) { /* 忽略 */ }
    refresh()
  }).catch((e) => ElMessage.error(e.message || e))
}

function enterEdit() {
  if (!active.value) return
  editing.value = true
  draft.value = JSON.parse(JSON.stringify(active.value))
  // 克隆体也要有稳定 id，否则编辑态一进一出 key 全变、图表全部重挂
  for (const p of (draft.value.panels || [])) if (!p._id) p._id = 'pnl-' + (++idSeq)
}

function cancelEdit() {
  editing.value = false
  draft.value = null
}

// 修 B6：非编辑态点「添加面板」（含空态按钮）时先进入编辑态，
// 否则后续 savePanel 会写 draft.value.panels 触发空指针。
function addPanel() {
  if (!editing.value) {
    if (!active.value) return
    enterEdit()
  }
  editIndex.value = -1
  editForm.value = { title: '', metric: '', chartType: 'line', node: '', range: '1h', size: 'half' }
  panelDlg.value = true
}

function editPanel(i) {
  editIndex.value = i
  const p = draft.value.panels[i]
  editForm.value = { ...p }
  panelDlg.value = true
}

function savePanel() {
  if (!editForm.value.metric) { ElMessage.warning('请选择指标'); return }
  if (!draft.value) return
  const next = { ...editForm.value, size: editForm.value.size || 'half' }
  if (editIndex.value >= 0) {
    next._id = draft.value.panels[editIndex.value]._id
    draft.value.panels[editIndex.value] = next
  } else {
    next._id = 'pnl-' + (++idSeq)
    draft.value.panels.push(next)
  }
  panelDlg.value = false
}

function removePanel(i) {
  if (!draft.value) return
  draft.value.panels.splice(i, 1)
}

function toggleSize(i) {
  const p = draft.value && draft.value.panels[i]
  if (!p) return
  p.size = p.size === 'full' ? 'half' : 'full'
}

function onDragStart(i, e) {
  if (!editing.value) return
  dragFrom.value = i
  if (e && e.dataTransfer) {
    e.dataTransfer.effectAllowed = 'move'
    try { e.dataTransfer.setData('text/plain', String(i)) } catch (_) { /* 部分浏览器只读 */ }
  }
}

function onDrop(i) {
  const from = dragFrom.value
  dragFrom.value = -1
  if (from < 0 || from === i || !draft.value) return
  const arr = draft.value.panels
  const [moved] = arr.splice(from, 1)
  arr.splice(i, 0, moved)
}

function onDragEnd() {
  dragFrom.value = -1
}

function save() {
  if (!active.value || !draft.value) return
  update(active.value.id, draft.value.name, draft.value.panels)
    .then(() => { editing.value = false; draft.value = null; refresh() })
    .catch((e) => ElMessage.error(e.message || e))
}

onMounted(async () => {
  await refresh()
  try {
    const d = await http.metricCatalog()
    const cat = d.catalog || {}
    const arr = []
    for (const k in cat) arr.push(...cat[k])
    flatMetrics.value = arr
  } catch (_) { /* 指标目录非阻塞 */ }
  try {
    const nd = await http.get('/api/v1/nodes')
    nodeList.value = nd.nodes || []
  } catch (_) { /* 静默失败，不影响其他功能 */ }
})
</script>

<style scoped>
.dash {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.dash-err {
  margin: 0;
}
.grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
  align-items: stretch;
}
.panel-wrap {
  min-width: 0;
}
/* 全宽面板独占一行 */
.panel-wrap.span-full {
  grid-column: 1 / -1;
}
.panel-wrap.span-full :deep(.chart-wrap) {
  height: 300px;
}
.panel-wrap.dragging {
  opacity: 0.5;
}
.panel-wrap[draggable='true'] {
  cursor: grab;
}
@media (max-width: 1024px) {
  .grid {
    grid-template-columns: minmax(0, 1fr);
  }
  .panel-wrap.span-full {
    grid-column: auto;
  }
}
</style>
