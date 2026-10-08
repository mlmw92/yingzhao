<template>
  <section class="view">
    <PageHeader
      title="配置项模型"
      desc="每个类型上会出现哪些配置项、哪些参与巡检比对；模型是「类型」的，不是「实例」的——在这里改不会动任何一台机器上已有的值"
    >
      <template #actions>
        <el-button size="small" :loading="loading" @click="reload">刷新</el-button>
      </template>
    </PageHeader>

    <el-alert
      v-if="loadError"
      type="error"
      :closable="false"
      show-icon
      title="读取配置项模型失败"
      :description="loadError"
    />

    <!-- 类型清单 -->
    <SectionCard title="类型" dense>
      <div class="type-row">
        <button
          v-for="t in types"
          :key="t.key"
          class="type-chip"
          :class="{ active: t.key === selectedKey }"
          @click="selectType(t.key)"
        >
          <span class="type-name">{{ t.title }}</span>
          <span class="muted">{{ t.assets }} 个资产</span>
          <!-- 短命对象：模型页要显示它们，但必须标出来（它们默认不计入台账健康度） -->
          <el-tag v-if="t.ephemeral" size="small" type="info" title="运行时短命对象：默认不计入台账健康度">短命对象</el-tag>
        </button>
      </div>
      <p class="muted note">
        类型与字段定义是「平台配置」（谁能进本页，看到的是同一份）；而资产数与下面的覆盖度按你的可见范围统计。
        <template v-if="current && current.baseline">
          本类型的标杆资产：<span class="mono">{{ current.baseline.assetKey }}</span>。
        </template>
      </p>
    </SectionCard>

    <!-- 字段表 -->
    <SectionCard v-if="current" :title="`字段（${current.title}）`" dense>
      <div class="toolbar">
        <span class="muted">运行态字段（不参与比对，避免探活抖动灌进差异清单）：</span>
        <el-tag v-for="k in current.schema.runtimeFields" :key="k" size="small" type="info" class="mono">{{ k }}</el-tag>
        <el-tag v-if="current.schema.runtimeFieldsDefault" size="small">内置默认</el-tag>
        <span class="spacer"></span>
        <el-button size="small" :disabled="!canWrite" @click="clearComparison">清空比对设置</el-button>
        <el-button size="small" type="primary" :disabled="!canWrite" :loading="saving" @click="save">保存</el-button>
        <span v-if="!canWrite" class="muted">缺 assets:write 权限，只能查看</span>
      </div>

      <!-- 关注字段非空时，只有被点名的字段参与比对——这点必须在页面上说清，
           否则"我明明看到这个字段有值，为什么没进差异"会变成一个谜 -->
      <el-alert
        v-if="focusCount > 0"
        class="tip"
        type="warning"
        :closable="false"
        show-icon
        :title="`当前只比对标注为「关注」的 ${focusCount} 个字段`"
        description="其余字段不参与比对（也不会被报成「缺失」）。全部改回「默认」即回到「全部字段减运行态字段」。"
      />

      <div class="dict-toolbar">
        <el-button size="small" :disabled="!canWrite" @click="openSuggest">添加建议属性</el-button>
        <span class="muted">
          生命周期 / 成本 / 维保到期这类管理属性不必硬编码：用「类型」把它们定义成枚举、数字或日期即可。
        </span>
      </div>

      <el-table :data="attrRows" style="width: 100%">
        <!-- 首列固定：这张表 9 列合计约 1360px，横向滚动后若首列跟着滑走，
             就不知道自己正在编辑哪个属性了。 -->
        <el-table-column label="字段" min-width="190" fixed>
          <template #default="{ row }"><span class="mono">{{ row.key }}</span></template>
        </el-table-column>
        <el-table-column label="中文名" width="170">
          <template #default="{ row }">
            <el-input
              v-model="draft.attrMeta[row.key].title"
              size="small"
              :disabled="!canWrite"
              placeholder="未填则显示字段键"
            />
          </template>
        </el-table-column>
        <el-table-column label="单位" width="100">
          <template #default="{ row }">
            <el-input v-model="draft.attrMeta[row.key].unit" size="small" :disabled="!canWrite" placeholder="—" />
          </template>
        </el-table-column>
        <el-table-column label="备注" min-width="180">
          <template #default="{ row }">
            <el-input v-model="draft.attrMeta[row.key].note" size="small" :disabled="!canWrite" placeholder="给同事看的一句话" />
          </template>
        </el-table-column>
        <el-table-column label="类型" width="120">
          <template #default="{ row }">
            <el-select
              v-model="draft.attrMeta[row.key].kind"
              size="small"
              :disabled="!canWrite"
              @change="onKindChange(row.key)"
            >
              <el-option label="文本" value="" />
              <el-option label="数字" value="number" />
              <el-option label="日期" value="date" />
              <el-option label="枚举" value="enum" />
              <el-option label="布尔" value="bool" />
            </el-select>
          </template>
        </el-table-column>
        <el-table-column label="枚举取值" min-width="150">
          <template #default="{ row }">
            <el-button
              v-if="draft.attrMeta[row.key].kind === 'enum'"
              size="small"
              :disabled="!canWrite"
              @click="editEnum(row.key)"
            >配置（{{ (draft.attrMeta[row.key].enum || []).length }} 项）</el-button>
            <span v-else class="muted">—</span>
          </template>
        </el-table-column>
        <el-table-column label="来源" width="150">
          <template #default="{ row }">
            <el-tag v-if="row.discovery" size="small">采集 {{ row.discovery }}</el-tag>
            <el-tag v-if="row.manual" size="small" type="warning">人工 {{ row.manual }}</el-tag>
            <span v-if="!row.discovery && !row.manual" class="muted">—</span>
          </template>
        </el-table-column>
        <el-table-column label="覆盖度" width="160">
          <template #default="{ row }">
            <div class="cover">
              <div class="cover-track"><div class="cover-fill" :style="{ width: pct(row.coverage) }"></div></div>
              <span class="muted">{{ row.assets }}/{{ current.assets }}</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="比对" width="140">
          <template #default="{ row }">
            <el-select v-model="draft.mode[row.key]" size="small" :disabled="!canWrite">
              <el-option label="默认" value="default" />
              <el-option label="关注" value="focus" />
              <el-option label="排除（运行态）" value="runtime" />
            </el-select>
          </template>
        </el-table-column>
      </el-table>

      <!-- 枚举取值编辑：Value 进台账，显示名只影响界面（改显示名不改数据） -->
      <el-dialog v-model="enumDialog.visible" title="枚举取值" width="min(560px, 94vw)">
        <p class="muted note">
          Value 是「存进台账」的值（如 <span class="mono">prod</span>），显示名只影响界面显示（如「生产」）。
          人工值必须落在这些取值里；采集值不受约束（真实环境上报什么就记什么）。
        </p>
        <div v-for="(opt, i) in enumDialog.options" :key="i" class="enum-row">
          <el-input v-model="opt.value" size="small" placeholder="值（如 prod）" style="width: 200px" />
          <el-input v-model="opt.label" size="small" placeholder="显示名（如 生产）" style="width: 200px" />
          <el-button link type="danger" @click="enumDialog.options.splice(i, 1)">删除</el-button>
        </div>
        <el-button size="small" @click="enumDialog.options.push({ value: '', label: '' })">添加取值</el-button>
        <template #footer>
          <el-button @click="enumDialog.visible = false">取消</el-button>
          <el-button type="primary" @click="applyEnum">确定</el-button>
        </template>
      </el-dialog>

      <!-- 建议属性：只把常见配法摆出来，**不自动写入**（没配过却出现字段会让人困惑） -->
      <el-dialog v-model="suggestDialog.visible" title="添加建议属性" width="min(560px, 94vw)">
        <p class="muted note">
          勾选后它们会进入当前类型的字段表（编辑态），确认无误再点「保存」。
          中文名与取值都可以按自己的口径改。
        </p>
        <el-checkbox-group v-model="suggestDialog.picked">
          <div v-for="s in suggestedAttrs" :key="s.key" class="suggest-row">
            <el-checkbox :value="s.key">
              {{ s.title }}（<span class="mono">{{ s.key }}</span> · {{ kindLabel(s.kind) }}）
            </el-checkbox>
          </div>
        </el-checkbox-group>
        <template #footer>
          <el-button @click="suggestDialog.visible = false">取消</el-button>
          <el-button type="primary" @click="applySuggest">添加</el-button>
        </template>
      </el-dialog>

      <p v-if="current.attrsTruncated" class="muted note">
        字段数超过上限，这里只按覆盖数显示前 {{ current.attrs.length }} 个——不要据此判断「这个类型只有这些字段」。
      </p>

      <div v-if="current.synonymGroups && current.synonymGroups.length" class="synonyms">
        <div class="muted">疑似同义键（只是写法不同；不会自动合并——合并是不可逆的，请自行确认后决定）</div>
        <div v-for="(group, i) in current.synonymGroups" :key="i" class="mono">{{ group.join('  /  ') }}</div>
      </div>

      <p class="muted note">
        这里不显示属性值：值里可能有连接串与口令，模型页只看模型；要看某台资产的具体值，去资产台账详情
        （那里有权限与范围约束）。
      </p>
    </SectionCard>
  </section>
</template>

<script setup>
import { computed, ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import PageHeader from '../common/PageHeader.vue'
import SectionCard from '../common/SectionCard.vue'
import { getAssetTypes, saveAssetTypeModel } from '../../api/asset'
import { useAuth } from '../../composables/useAuth'

const auth = useAuth()
// 前端隐藏仅为体验：服务端 assets:write 才是边界（该权限点为高风险）
const canWrite = computed(() => auth.can('assets:write'))

const types = ref([])
const selectedKey = ref('')
const loading = ref(false)
const saving = ref(false)
const loadError = ref('')
// draft 是"正在编辑的模型"：每行的比对档位与属性说明。
// 与列表数据分开，是为了"没保存就离开"不会污染下一步（保存成功后才回填）。
const draft = ref({ mode: {}, attrMeta: {} })

const current = computed(() => types.value.find((t) => t.key === selectedKey.value) || null)
// focusCount 用于提示"只比对点名字段"：非空即表示其余字段不参与比对
const focusCount = computed(() => Object.values(draft.value.mode).filter((m) => m === 'focus').length)

function pct(v) {
  return `${Math.round((Number(v) || 0) * 100)}%`
}

// emptyMeta 是一条字段说明的初始形态（含本批新增的字典字段）。
function emptyMeta() {
  return { title: '', unit: '', note: '', kind: '', required: false, enum: [] }
}

// 枚举取值编辑器：Value 进台账，显示名只影响界面（改显示名不改数据）。
const enumDialog = ref({ visible: false, key: '', options: [] })

// 建议属性：把「生命周期 / 成本 / 维保到期」这类管理属性的**常见配法**摆出来。
//
// 刻意**不自动写入**：没配过却出现字段会让人困惑（与"播种 ≠ 模板库"同一取向）。
// 这也是「资产生命周期」那条定界的落地路径——它不需要专门代码，就是这里的几个属性。
const suggestedAttrs = [
  {
    key: 'lifecycle', title: '生命周期阶段', kind: 'enum',
    enum: [
      { value: 'active', label: '在用' }, { value: 'idle', label: '闲置' },
      { value: 'offline', label: '下线' }, { value: 'retired', label: '退役' },
    ],
  },
  { key: 'cost', title: '成本', kind: 'number', unit: '元/月' },
  { key: 'warrantyUntil', title: '维保到期', kind: 'date' },
  {
    key: 'env', title: '环境', kind: 'enum',
    enum: [
      { value: 'prod', label: '生产' }, { value: 'staging', label: '预发' }, { value: 'test', label: '测试' },
    ],
  },
  { key: 'system', title: '业务系统', kind: 'text' },
  { key: 'assetNo', title: '资产编号', kind: 'text' },
]

const suggestDialog = ref({ visible: false, picked: [] })

const KIND_LABELS = { '': '文本', number: '数字', date: '日期', enum: '枚举', bool: '布尔' }

function kindLabel(kind) {
  return KIND_LABELS[kind || ''] || '文本'
}

// attrRows 是**表格数据**：后端返回的字段 ∪ 编辑态里新增的字段。
// 少了后半段，"刚加的建议属性"会因为"当前没有资产带这个键"而根本不出现在表里。
const attrRows = computed(() => {
  const rows = [...(current.value?.attrs || [])]
  const seen = new Set(rows.map((r) => r.key))
  for (const key of Object.keys(draft.value.attrMeta || {})) {
    if (!seen.has(key)) rows.push({ key, assets: 0, coverage: 0, discovery: 0, manual: 0 })
  }
  return rows
})

function openSuggest() {
  suggestDialog.value = { visible: true, picked: [] }
}

function applySuggest() {
  for (const key of suggestDialog.value.picked) {
    const s = suggestedAttrs.find((x) => x.key === key)
    if (!s) continue
    const cur = draft.value.attrMeta[key] || emptyMeta()
    draft.value.attrMeta[key] = {
      ...cur,
      title: cur.title || s.title,
      unit: cur.unit || s.unit || '',
      kind: s.kind || '',
      enum: (s.enum || []).map((o) => ({ value: o.value, label: o.label || '' })),
    }
    if (draft.value.mode[key] === undefined) draft.value.mode[key] = 'default'
  }
  suggestDialog.value.visible = false
  ElMessage.success('已加入编辑态，确认后点「保存」生效')
}

function onKindChange(key) {
  const meta = draft.value.attrMeta[key]
  if (!meta) return
  if (meta.kind !== 'enum') {
    // 类型改成非枚举：取值列表留着没有意义（后端也会拒"非 enum 配了取值"）
    meta.enum = []
  } else if (!meta.enum || !meta.enum.length) {
    meta.enum = [{ value: '', label: '' }]
  }
}

function editEnum(key) {
  const meta = draft.value.attrMeta[key]
  const options = (meta?.enum || []).map((o) => ({ value: o.value || '', label: o.label || '' }))
  enumDialog.value = { visible: true, key, options: options.length ? options : [{ value: '', label: '' }] }
}

function applyEnum() {
  const meta = draft.value.attrMeta[enumDialog.value.key]
  if (!meta) return
  meta.enum = enumDialog.value.options
    .map((o) => ({ value: (o.value || '').trim(), label: (o.label || '').trim() }))
    .filter((o) => o.value)
  enumDialog.value.visible = false
}

// buildDraft 用服务端的模型铺一次编辑态（切类型、保存成功、刷新后都走它）
function buildDraft(model) {
  const mode = {}
  const attrMeta = {}
  for (const st of model.attrs || []) {
    mode[st.key] = 'default'
    attrMeta[st.key] = emptyMeta()
  }
  for (const k of model.schema?.runtimeFields || []) {
    if (mode[k] === undefined) mode[k] = 'runtime'
    else mode[k] = 'runtime'
  }
  for (const k of model.schema?.focusFields || []) {
    mode[k] = 'focus'
  }
  for (const [k, meta] of Object.entries(model.schema?.attrMeta || {})) {
    // 说明可能落在"当前没有资产带这个键"的字段上：也要能编辑，所以按出现过的键补齐
    attrMeta[k] = {
      title: meta.title || '',
      unit: meta.unit || '',
      note: meta.note || '',
      kind: meta.kind || '',
      required: !!meta.required,
      enum: Array.isArray(meta.enum) ? meta.enum.map((o) => ({ value: o.value || '', label: o.label || '' })) : [],
    }
    if (mode[k] === undefined) mode[k] = 'default'
  }
  draft.value = { mode, attrMeta }
}

function selectType(key) {
  selectedKey.value = key
  const model = types.value.find((t) => t.key === key)
  if (model) buildDraft(model)
}

function applyTypes(list) {
  types.value = list || []
  if (!selectedKey.value && types.value.length) {
    selectType(types.value[0].key)
    return
  }
  const model = types.value.find((t) => t.key === selectedKey.value) || types.value[0]
  if (model) selectType(model.key)
}

async function reload() {
  loading.value = true
  try {
    const data = await getAssetTypes()
    applyTypes(data.types)
    loadError.value = ''
  } catch (e) {
    loadError.value = e.message || '请稍后重试'
  } finally {
    loading.value = false
  }
}

// collect 把编辑态折成接口要的三项（判断口径与后端一致：空数组 = 用内置默认）。
//
// 有一处**必须**小心：如果用户没动过运行态字段（当前生效的就是内置默认那一份），
// 提交的 runtimeFields 要是**空数组**而不是把默认清单原样提交——
// 否则"只改了个中文名"的保存会把默认清单固化进模型，将来内置默认调整就再也影响不到它。
function collect() {
  const model = current.value
  const runtimeFields = []
  const focusFields = []
  for (const [key, mode] of Object.entries(draft.value.mode)) {
    if (mode === 'runtime') runtimeFields.push(key)
    if (mode === 'focus') focusFields.push(key)
  }
  const effective = [...(model?.schema?.runtimeFields || [])].sort()
  const picked = [...runtimeFields].sort()
  const untouchedDefault =
    model?.schema?.runtimeFieldsDefault &&
    effective.length === picked.length &&
    effective.every((k, i) => k === picked[i])
  const attrMeta = {}
  for (const [key, meta] of Object.entries(draft.value.attrMeta)) {
    const title = (meta.title || '').trim()
    const unit = (meta.unit || '').trim()
    const note = (meta.note || '').trim()
    const kind = meta.kind || ''
    const enumOpts = (meta.enum || [])
      .map((o) => ({ value: (o.value || '').trim(), label: (o.label || '').trim() }))
      .filter((o) => o.value)
    const entry = { title, unit, note }
    if (kind) entry.kind = kind
    if (kind === 'enum') entry.enum = enumOpts
    if (meta.required) entry.required = true
    // 只配了类型（或必填）也是**有内容**的配置：不能因为三个说明都空就丢掉
    if (title || unit || note || kind || meta.required) attrMeta[key] = entry
  }
  return { runtimeFields: untouchedDefault ? [] : runtimeFields, focusFields, attrMeta }
}

async function save() {
  if (!current.value) return
  saving.value = true
  try {
    const data = await saveAssetTypeModel(current.value.key, collect())
    applyTypes(data.types)
    ElMessage.success(`已保存「${current.value ? current.value.title : ''}」的模型`)
  } catch (e) {
    ElMessage.error(e.message || '保存失败')
  } finally {
    saving.value = false
  }
}

async function clearComparison() {
  try {
    await ElMessageBox.confirm(
      '清空后回到「全部字段减运行态字段」的内置默认（内置默认的运行态字段是 up / status / uptime / uptimeSeconds）。' +
        '属性说明（中文名/单位/备注）不受影响。',
      '清空比对设置',
      { type: 'warning', confirmButtonText: '清空', cancelButtonText: '取消' },
    )
  } catch (e) {
    return
  }
  const mode = {}
  for (const k of Object.keys(draft.value.mode)) mode[k] = 'default'
  draft.value = { ...draft.value, mode }
  ElMessage.info('已清空比对设置，记得点「保存」')
}

onMounted(reload)
</script>

<style scoped>
.view { padding: 0; display: flex; flex-direction: column; gap: 12px; }
.muted { color: var(--text-dim); font-size: 13px; }
.mono { font-family: var(--mono); }
.note { margin: 10px 0 0; line-height: 20px; }
.tip { margin-bottom: 10px; }
.type-row { display: flex; flex-wrap: wrap; gap: 8px; }
.type-chip {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: var(--fill-1);
  color: var(--text);
  cursor: pointer;
  font-size: 13px;
}
.type-chip.active { border-color: var(--accent); color: var(--accent); }
.type-name { font-weight: 600; }
.toolbar { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; margin-bottom: 10px; }
.spacer { flex: 1; }
.cover { display: flex; align-items: center; gap: 8px; }
.cover-track { flex: 1; height: 6px; border-radius: 3px; background: var(--fill-2); overflow: hidden; }
.dict-toolbar {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 10px;
}

.suggest-row {
  margin-bottom: 4px;
}

.enum-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}

.cover-fill { height: 100%; background: var(--accent); }
.synonyms { margin-top: 10px; line-height: 22px; }
</style>
