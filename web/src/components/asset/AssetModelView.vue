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
        <!-- 语义化切换控件：type=button 防误提交、aria-pressed 提供颜色之外的状态编码 -->
        <button
          v-for="t in types"
          :key="t.key"
          type="button"
          class="type-chip"
          :class="{ active: t.key === selectedKey }"
          :aria-pressed="t.key === selectedKey"
          @click="selectType(t.key)"
        >
          <span class="type-name">{{ t.title }}</span>
          <span class="muted">{{ t.assets }} 个资产</span>
          <!-- 短命对象：模型页要显示它们，但必须标出来（它们默认不计入台账健康度） -->
          <el-tag v-if="t.ephemeral" size="small" type="info" title="运行时短命对象：默认不计入台账健康度">短命对象</el-tag>
        </button>
      </div>
      <p class="muted muted-base">
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
        <!-- 与下方「比对」列双向联动：这里 × 掉 = 把该字段的比对档位改回「默认」 -->
        <el-tag
          v-for="k in runtimeKeys"
          :key="k"
          size="small"
          type="info"
          class="mono"
          closable
          :disable-transitions="true"
          title="点击 × 把它改回「默认」（即参与比对）"
          @close="setMode(k, 'default')"
        >{{ k }}</el-tag>
        <span v-if="!runtimeKeys.length" class="muted">（无）</span>
        <el-tag v-if="current.schema.runtimeFieldsDefault" size="small">内置默认</el-tag>
        <span class="spacer"></span>
        <el-button size="small" type="primary" :disabled="!canWrite" @click="openNewField">+ 新建字段</el-button>
        <el-button size="small" :disabled="!canWrite" @click="openSuggest">从模板新建</el-button>
        <el-button size="small" :disabled="!canWrite" @click="clearComparison">清空比对设置</el-button>
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

      <!-- 声明了但没数据的字段必须讲清归属：不说的话，用户会以为"保存了就会有值"，
           然后去巡检比对里找差异、找不到当成功能坏了。
           放在表格上方——常驻可见，且**不拦保存**（拦截会挡住既有流程）。 -->
      <el-alert
        v-if="uncarriedKeys.length"
        class="tip"
        type="info"
        :closable="false"
        show-icon
        :title="`有 ${uncarriedKeys.length} 个字段已声明但没有资产携带${freshKeys.length ? `（其中 ${freshKeys.length} 个是本次新增）` : ''}`"
        :description="`${uncarriedKeys.join('、')}。保存只改模型定义，不会给任何资产赋值——值要等采集上报或在资产台账里人工填写。`"
      />

      <p class="muted muted-base">
        生命周期 / 成本 / 维保到期这类管理属性不必硬编码：用「类型」把它们定义成枚举、数字或日期即可。
        来源与覆盖度这类只读统计收进了行内展开，把可编辑列让给「比对」。
      </p>

      <el-table :data="attrRows" v-loading="loading" style="width: 100%">
        <template #empty>
          <EmptyState
            title="该类型还没有任何字段"
            :hints="[
              '点右上角「+ 新建字段」自定义一个字段键',
              '或点「从模板新建」从常见管理属性里挑（生命周期 / 成本 / 维保到期…）',
            ]"
            action-text="新建字段"
            @action="openNewField"
          />
        </template>

        <!-- 只读统计（来源 / 覆盖度）收进行内展开：编辑表只留可编辑列，
            「比对」这个决策主列不再被 310px 只读列挤出视口。 -->
        <el-table-column type="expand" width="48">
          <template #default="{ row }">
            <div class="expand-detail">
              <div class="ed-item">
                <span class="muted">来源</span>
                <el-tag v-if="row.discovery" size="small">采集 {{ row.discovery }}</el-tag>
                <el-tag v-if="row.manual" size="small" type="warning">人工 {{ row.manual }}</el-tag>
                <span v-if="!row.discovery && !row.manual" class="muted">—</span>
              </div>
              <div class="ed-item">
                <span class="muted">覆盖度</span>
                <!-- 0 台资产携带时画空进度条会读成"覆盖率 0%"，像是采集坏了；
                     实际语义是"声明了、还没有数据"，直接写字更准 -->
                <span v-if="row.uncarried" class="muted" title="模型里已声明该字段，但当前没有资产携带它">未携带</span>
                <div v-else class="cover">
                  <div class="cover-track"><div class="cover-fill" :style="{ width: pct(row.coverage) }"></div></div>
                  <span class="muted">{{ row.assets }}/{{ current.assets }}</span>
                </div>
              </div>
            </div>
          </template>
        </el-table-column>

        <el-table-column label="字段" min-width="170">
          <template #default="{ row }">
            <span class="mono">{{ row.key }}</span>
            <el-tag
              v-if="row.fresh"
              size="small"
              type="warning"
              class="key-tag"
              title="本次新增：模型里还没有这个字段，也没有任何资产携带它"
            >新增</el-tag>
            <el-tag
              v-if="row.uncarried"
              size="small"
              type="info"
              class="key-tag"
              title="模型里已声明该字段，但当前没有资产携带它"
            >未携带</el-tag>
            <!-- 覆盖数（n/总数）留在主列可见：它是"这个字段有多少资产在报"的健康信号，
                 收进展开就没人会去看；完整的进度条与来源在行内展开里。 -->
            <span v-if="!row.uncarried" class="cover-mini muted">{{ row.assets }}/{{ current.assets }}</span>
          </template>
        </el-table-column>
        <el-table-column label="中文名" width="150">
          <template #default="{ row }">
            <el-input
              v-model="draft.attrMeta[row.key].title"
              size="small"
              :disabled="!canWrite"
              placeholder="未填则显示字段键"
            />
          </template>
        </el-table-column>
        <el-table-column label="单位" width="90">
          <template #default="{ row }">
            <el-input v-model="draft.attrMeta[row.key].unit" size="small" :disabled="!canWrite" placeholder="—" />
          </template>
        </el-table-column>
        <el-table-column label="备注" min-width="160">
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
        <el-table-column label="比对" width="140">
          <template #default="{ row }">
            <el-select v-model="draft.mode[row.key]" size="small" :disabled="!canWrite">
              <el-option label="默认" value="default" />
              <el-option label="关注" value="focus" />
              <el-option label="排除（运行态）" value="runtime" />
            </el-select>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="90">
          <template #default="{ row }">
            <el-button link type="danger" :disabled="!canWrite" @click="removeField(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>

      <!-- 新建字段：字段键是自由输入（模型层），中文名与类型可顺手填 -->
      <el-dialog v-model="newField.visible" title="新建字段" width="min(520px, 94vw)">
        <p class="muted note">
          字段键是「模型里的键」，要与采集上报或人工填写用的键一致；中文名只是显示用，留空则显示字段键。
        </p>
        <div class="nf-row">
          <span class="nf-label">字段键</span>
          <el-input v-model="newField.key" size="small" placeholder="如 lifecycle / cost" class="mono" />
        </div>
        <div class="nf-row">
          <span class="nf-label">中文名</span>
          <el-input v-model="newField.title" size="small" placeholder="如 生命周期阶段（可留空）" />
        </div>
        <div class="nf-row">
          <span class="nf-label">类型</span>
          <el-select v-model="newField.kind" size="small" class="w-s">
            <el-option label="文本" value="" />
            <el-option label="数字" value="number" />
            <el-option label="日期" value="date" />
            <el-option label="枚举" value="enum" />
            <el-option label="布尔" value="bool" />
          </el-select>
        </div>
        <template #footer>
          <el-button @click="newField.visible = false">取消</el-button>
          <el-button type="primary" @click="applyNewField">加入编辑态</el-button>
        </template>
      </el-dialog>

      <!-- 枚举取值编辑：Value 进台账，显示名只影响界面（改显示名不改数据） -->
      <el-dialog v-model="enumDialog.visible" title="枚举取值" width="min(560px, 94vw)">
        <p class="muted note">
          Value 是「存进台账」的值（如 <span class="mono">prod</span>），显示名只影响界面显示（如「生产」）。
          人工值必须落在这些取值里；采集值不受约束（真实环境上报什么就记什么）。
        </p>
        <div v-for="(opt, i) in enumDialog.options" :key="i" class="enum-row">
          <el-input v-model="opt.value" size="small" placeholder="值（如 prod）" class="w-m" />
          <el-input v-model="opt.label" size="small" placeholder="显示名（如 生产）" class="w-m" />
          <el-button link type="danger" @click="enumDialog.options.splice(i, 1)">删除</el-button>
        </div>
        <el-button size="small" @click="enumDialog.options.push({ value: '', label: '' })">添加取值</el-button>
        <!-- 批量粘贴：枚举动辄十几项，逐行手敲是最容易出错也最烦的一步 -->
        <div class="enum-paste">
          <el-input
            v-model="enumDialog.paste"
            type="textarea"
            :rows="3"
            size="small"
            placeholder="批量粘贴：每行一个值，支持「值,显示名」或「值 显示名」"
          />
          <el-button size="small" @click="applyPaste">导入到上面</el-button>
        </div>
        <template #footer>
          <el-button @click="enumDialog.visible = false">取消</el-button>
          <el-button type="primary" @click="applyEnum">确定</el-button>
        </template>
      </el-dialog>

      <!-- 建议属性（模板）：只把常见配法摆出来，**不自动写入**（没配过却出现字段会让人困惑） -->
      <el-dialog v-model="suggestDialog.visible" title="从模板新建" width="min(560px, 94vw)">
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
          <el-button type="primary" @click="applySuggest">加入编辑态</el-button>
        </template>
      </el-dialog>

      <p v-if="current.attrsTruncated" class="muted muted-base">
        字段数超过上限，这里只按覆盖数显示前 {{ current.attrs.length }} 个——不要据此判断「这个类型只有这些字段」。
      </p>

      <div v-if="current.synonymGroups && current.synonymGroups.length" class="synonyms">
        <div class="muted">疑似同义键（只是写法不同；不会自动合并——合并是不可逆的，请自行确认后决定）</div>
        <div v-for="(group, i) in current.synonymGroups" :key="i" class="mono">{{ group.join('  /  ') }}</div>
      </div>

      <p class="muted muted-base">
        这里不显示属性值：值里可能有连接串与口令，模型页只看模型；要看某台资产的具体值，去资产台账详情
        （那里有权限与范围约束）。
      </p>

      <!-- 吸底保存栏：字段表可以很长，改完最后一行时「保存」已经滚出视口 -->
      <div class="save-bar">
        <span v-if="dirty" class="muted">有 {{ dirtyCount }} 处未保存的改动</span>
        <span v-else class="muted">没有未保存的改动</span>
        <span class="spacer"></span>
        <el-button size="small" :disabled="!canWrite || !dirty" @click="revert">放弃改动</el-button>
        <el-button size="small" type="primary" :disabled="!canWrite" :loading="saving" @click="save">保存</el-button>
      </div>
    </SectionCard>
  </section>
</template>

<script setup>
import { computed, ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import PageHeader from '../common/PageHeader.vue'
import SectionCard from '../common/SectionCard.vue'
import EmptyState from '../common/EmptyState.vue'
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
// removed 记录被删掉的字段键（模型层移除，不代表底层数据消失）。
const draft = ref({ mode: {}, attrMeta: {}, removed: [] })
// baseline 是进入本类型时的编辑态快照，用于"未保存改动"计数与切类型拦截。
const baseline = ref({ mode: {}, attrMeta: {} })

const current = computed(() => types.value.find((t) => t.key === selectedKey.value) || null)
// focusCount 用于提示"只比对点名字段"：非空即表示其余字段不参与比对
const focusCount = computed(() => Object.values(draft.value.mode).filter((m) => m === 'focus').length)
// 运行态字段列表由编辑态推导（与「比对」列双向联动）：改比对档位，上方标签立即跟着变
const runtimeKeys = computed(() =>
  Object.entries(draft.value.mode).filter(([, m]) => m === 'runtime').map(([k]) => k),
)

function pct(v) {
  return `${Math.round((Number(v) || 0) * 100)}%`
}

// emptyMeta 是一条字段说明的初始形态（含字典字段）。
function emptyMeta() {
  return { title: '', unit: '', note: '', kind: '', required: false, enum: [] }
}

// —— 未保存改动 ——
// 逐字段比对编辑态与基线：新增 / 删除 / 改档位 / 改说明都算一处改动。
const dirtyCount = computed(() => {
  const b = baseline.value
  const keys = new Set([
    ...Object.keys(draft.value.mode),
    ...Object.keys(draft.value.attrMeta),
    ...Object.keys(b.mode || {}),
    ...Object.keys(b.attrMeta || {}),
  ])
  let n = 0
  for (const k of keys) {
    const cur = JSON.stringify([draft.value.mode[k] ?? null, draft.value.attrMeta[k] ?? null])
    const old = JSON.stringify([(b.mode || {})[k] ?? null, (b.attrMeta || {})[k] ?? null])
    if (cur !== old) n++
  }
  return n + draft.value.removed.length
})
const dirty = computed(() => dirtyCount.value > 0)

// 新建字段弹窗
const newField = ref({ visible: false, key: '', title: '', kind: '' })

// 枚举取值编辑器：Value 进台账，显示名只影响界面（改显示名不改数据）。
const enumDialog = ref({ visible: false, key: '', options: [], paste: '' })

// 建议属性（模板）：把「生命周期 / 成本 / 维保到期」这类管理属性的**常见配法**摆出来。
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

// attrRows 是**表格数据**：后端返回的字段 ∪ 编辑态里新增的字段，减去被删除的字段。
// 少了"∪ 编辑态新增"，"刚加的字段"会因为"当前没有资产带这个键"而根本不出现在表里。
//
// 顺带区分两种"没有资产带这个键"：
//   uncarried —— 模型里声明了（或这次加进来了），但 0 台资产带它
//   fresh     —— 连后端 attrMeta 里都没有说明，是这次编辑才加进来的
// 两者在界面上要分开讲：前者是"声明了还没数据"，后者是"你现在正在新增"。
const attrRows = computed(() => {
  const m = current.value
  const removed = new Set(draft.value.removed)
  const declared = new Set(Object.keys(m?.schema?.attrMeta || {}))
  const rows = [...(m?.attrs || [])]
    .filter((r) => !removed.has(r.key))
    .map((r) => ({
      ...r,
      uncarried: !r.assets,
      fresh: !r.assets && !declared.has(r.key),
    }))
  const seen = new Set(rows.map((r) => r.key))
  for (const key of Object.keys(draft.value.attrMeta || {})) {
    if (seen.has(key) || removed.has(key)) continue
    rows.push({ key, assets: 0, coverage: 0, discovery: 0, manual: 0, uncarried: true, fresh: !declared.has(key) })
  }
  return rows
})

// 只统计"真正是这次新加的"：保存前的确认要按它来触发——
// 每次保存都弹"有字段未携带"会变成噪音，只有用户主动新增时才值得拦一次。
const freshKeys = computed(() => attrRows.value.filter((r) => r.fresh).map((r) => r.key))
const uncarriedKeys = computed(() => attrRows.value.filter((r) => r.uncarried).map((r) => r.key))

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
    draft.value.removed = draft.value.removed.filter((k) => k !== key)
  }
  suggestDialog.value.visible = false
  ElMessage.success('已加入编辑态，确认后点「保存」生效')
}

// —— 新建字段 ——
function openNewField() {
  newField.value = { visible: true, key: '', title: '', kind: '' }
}

function applyNewField() {
  const key = (newField.value.key || '').trim()
  const title = (newField.value.title || '').trim()
  const kind = newField.value.kind || ''
  if (!key) {
    ElMessage.warning('请填写字段键')
    return
  }
  if (draft.value.attrMeta[key] || attrRows.value.some((r) => r.key === key)) {
    ElMessage.warning('该字段已存在')
    return
  }
  // 字段必须至少有一项配置：模型的 attrMeta 只存"有内容"的字段（空项不入库），
  // 一个只有键、什么都没有的字段在服务端存不下来，保存后它会消失——这里先说清。
  if (!title && !kind) {
    ElMessage.warning('请填写中文名或选择类型：字段至少要有一样配置，否则保存后不会留下它')
    return
  }
  draft.value.attrMeta[key] = { ...emptyMeta(), title, kind }
  draft.value.mode[key] = 'default'
  draft.value.removed = draft.value.removed.filter((k) => k !== key)
  newField.value.visible = false
  ElMessage.success('已加入编辑态，确认后点「保存」生效')
}

// —— 删除字段 ——
// 模型层移除：清掉说明与比对档位。**不是**删数据——采集资产携带的字段下次仍会因数据出现。
function removeField(row) {
  const carried = row.assets > 0
  ElMessageBox.confirm(
    carried
      ? `「${row.key}」有 ${row.assets} 台资产携带数据。删除只移除模型里的说明与比对设置；下次加载它仍会因数据出现在表里。`
      : `将移除字段「${row.key}」的模型配置。`,
    '删除字段',
    { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' },
  )
    .then(() => {
      draft.value.removed = [...draft.value.removed, row.key]
      delete draft.value.attrMeta[row.key]
      delete draft.value.mode[row.key]
      ElMessage.info('已移除，记得点「保存」')
    })
    .catch(() => {})
}

// 上方运行态标签的 × = 把该字段改回「默认」
function setMode(key, mode) {
  draft.value.mode[key] = mode
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
  enumDialog.value = { visible: true, key, options: options.length ? options : [{ value: '', label: '' }], paste: '' }
}

// 批量粘贴：每行「值」或「值,显示名」或「值 显示名」；已存在的值跳过
function applyPaste() {
  const lines = (enumDialog.value.paste || '').split('\n').map((s) => s.trim()).filter(Boolean)
  let added = 0
  for (const line of lines) {
    const parts = line.split(/[,，\t]/)
    let value = (parts[0] || '').trim()
    let label = parts.slice(1).join(',').trim()
    if (!label && /\s/.test(value)) {
      // 没有分隔符时，退一步按首个空白切「值 显示名」
      const idx = value.search(/\s/)
      label = value.slice(idx + 1).trim()
      value = value.slice(0, idx).trim()
    }
    if (!value) continue
    if (enumDialog.value.options.some((o) => o.value === value)) continue
    enumDialog.value.options.push({ value, label })
    added++
  }
  enumDialog.value.options = enumDialog.value.options.filter((o) => o.value || o.label)
  enumDialog.value.paste = ''
  ElMessage.success(added ? `已导入 ${added} 项` : '没有可导入的新取值')
}

function applyEnum() {
  const meta = draft.value.attrMeta[enumDialog.value.key]
  if (!meta) return
  meta.enum = enumDialog.value.options
    .map((o) => ({ value: (o.value || '').trim(), label: (o.label || '').trim() }))
    .filter((o) => o.value)
  enumDialog.value.visible = false
}

// buildDraft 用服务端的模型铺一次编辑态（切类型、保存成功、刷新后都走它），
// 并把当前编辑态存为基线（未保存改动的比较基准）。
function buildDraft(model) {
  const mode = {}
  const attrMeta = {}
  for (const st of model.attrs || []) {
    mode[st.key] = 'default'
    attrMeta[st.key] = emptyMeta()
  }
  for (const k of model.schema?.runtimeFields || []) {
    mode[k] = 'runtime'
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
  draft.value = { mode, attrMeta, removed: [] }
  baseline.value = { mode: { ...mode }, attrMeta: JSON.parse(JSON.stringify(attrMeta)) }
}

// 切类型前拦截：有未保存改动先确认（改了半天切个类型全没了，是最难发现的丢数据方式）
async function selectType(key, force = false) {
  if (!force && key !== selectedKey.value && dirty.value) {
    try {
      await ElMessageBox.confirm(
        `「${current.value?.title || '当前类型'}」有 ${dirtyCount.value} 处未保存的改动，切换后这些改动会丢失。`,
        '未保存的改动',
        { type: 'warning', confirmButtonText: '放弃改动并切换', cancelButtonText: '留在本页' },
      )
    } catch (e) {
      return
    }
  }
  selectedKey.value = key
  const model = types.value.find((t) => t.key === key)
  if (model) buildDraft(model)
}

async function applyTypes(list) {
  types.value = list || []
  if (!selectedKey.value && types.value.length) {
    await selectType(types.value[0].key, true)
    return
  }
  const model = types.value.find((t) => t.key === selectedKey.value) || types.value[0]
  if (model) await selectType(model.key, true)
}

async function reload() {
  if (dirty.value) {
    try {
      await ElMessageBox.confirm(
        `有 ${dirtyCount.value} 处未保存的改动，刷新会丢弃它们。`,
        '未保存的改动',
        { type: 'warning', confirmButtonText: '放弃并刷新', cancelButtonText: '取消' },
      )
    } catch (e) {
      return
    }
  }
  loading.value = true
  try {
    const data = await getAssetTypes()
    await applyTypes(data.types)
    loadError.value = ''
  } catch (e) {
    loadError.value = e.message || '请稍后重试'
  } finally {
    loading.value = false
  }
}

// 放弃改动：回到进入本类型时的编辑态
function revert() {
  if (current.value) buildDraft(current.value)
}

// collect 把编辑态折成接口要的三项（判断口径与后端一致：空数组 = 用内置默认）。
//
// 有一处**必须**小心：如果用户没动过运行态字段（当前生效的就是内置默认那一份），
// 提交的 runtimeFields 要是**空数组**而不是把默认清单原样提交——
// 否则"只改了个中文名"的保存会把默认清单固化进模型，将来内置默认调整就再也影响不到它。
function collect() {
  const model = current.value
  const removed = new Set(draft.value.removed)
  const runtimeFields = []
  const focusFields = []
  for (const [key, mode] of Object.entries(draft.value.mode)) {
    if (removed.has(key)) continue
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
    if (removed.has(key)) continue
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

// 新增字段是"模型层"改动，不会凭空产生值——但**不用模态框拦保存**：
// 每次保存都弹一次会变成噪音，而且拦截会挡住"改完顺手保存"的既有流程。
// 改成表上方常驻提示（见 uncarried alert）+ 吸底栏的"未保存改动"计数。
async function save() {
  if (!current.value) return
  saving.value = true
  try {
    const data = await saveAssetTypeModel(current.value.key, collect())
    await applyTypes(data.types)
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
.mono { font-family: var(--mono); }
/* 「新增」角标紧跟字段键后面，不留大间隙，否则会读成另一列 */
.key-tag { margin-left: 6px; transform: translateY(-1px); }
.cover-mini { margin-left: 6px; font-size: var(--fs-xs); }
.note { margin: var(--desc-base) 0 0; line-height: 20px; }
.tip { margin-bottom: var(--desc-base); }
.type-row { display: flex; flex-wrap: wrap; gap: 8px; }
.type-chip {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  border: 1px solid var(--bd);
  border-radius: var(--r-lg);
  background: var(--fill-1);
  color: var(--t1);
  cursor: pointer;
  font-size: var(--fs-sm);
  font-family: inherit;
}
.type-chip.active { border-color: var(--accent); color: var(--accent); }
.type-name { font-weight: 600; }
.toolbar { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; margin-bottom: var(--desc-base); }
.spacer { flex: 1; }
.cover { display: flex; align-items: center; gap: 8px; min-width: 160px; }
.cover-track { flex: 1; height: 6px; border-radius: var(--r-xs); background: var(--fill-2); overflow: hidden; }
.cover-fill { height: 100%; background: var(--accent); }

/* 行内展开：只读统计（来源 / 覆盖度） */
.expand-detail { display: flex; gap: 32px; flex-wrap: wrap; padding: 4px 8px; }
.ed-item { display: flex; align-items: center; gap: 8px; }

.nf-row { display: flex; align-items: center; gap: 10px; margin-bottom: 10px; }
.nf-label { width: 56px; color: var(--t3); font-size: var(--fs-sm); flex: none; }
.enum-paste { margin-top: 10px; display: flex; gap: 8px; align-items: flex-start; }

.suggest-row { margin-bottom: 4px; }

.enum-row { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; }

.synonyms { margin-top: var(--desc-base); line-height: 22px; }
</style>
