<template>
  <div class="rbac-view">
    <PageHeader
      title="角色与权限"
      desc="角色是一组权限点与默认数据范围的集合；内置角色不可删除，自定义角色的数据范围不得超过创建者自身范围"
    >
      <template #actions>
        <el-button v-if="auth.can('roles:manage')" type="primary" :icon="Plus" @click="openCreate">新建角色</el-button>
      </template>
    </PageHeader>

    <el-alert
      v-if="!auth.can('roles:manage')"
      type="info"
      :closable="false"
      title="当前账号仅有角色查看权限，编辑 / 删除操作需具备 roles:manage 权限。"
      class="page-alert"
    />

    <SectionCard title="角色列表" dense>
      <el-table :data="roles" v-loading="loading" stripe>
        <template #empty>
          <EmptyState
            title="暂无角色"
            :hints="['内置角色会随服务端下发；自定义角色可在右上角「新建角色」创建']"
          />
        </template>
        <el-table-column prop="name" label="角色名" min-width="130">
          <template #default="{ row }">
            <el-tag size="small" effect="dark" type="info">{{ roleLabel(row.name) }}</el-tag>
            <span v-if="row.builtin" class="builtin-tag">内置</span>
          </template>
        </el-table-column>
        <el-table-column prop="description" label="说明" min-width="200" show-overflow-tooltip />
        <el-table-column label="权限数" width="80">
          <template #default="{ row }">{{ Array.isArray(row.permissions) ? row.permissions.length : 0 }}</template>
        </el-table-column>
        <!-- 影响用户数：改角色前先知道会影响多少人（授权是安全边界，改错的代价按人数放大） -->
        <el-table-column label="影响用户数" width="100">
          <template #default="{ row }">
            <span :class="{ 'cnt-zero': userCountOf(row.name) === 0 }">{{ userCountOf(row.name) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="默认范围" min-width="180">
          <template #default="{ row }">
            <el-tag v-if="row.scope_mode === 'restricted'" size="small" type="warning" effect="plain">受限</el-tag>
            <el-tag v-else size="small" type="success" effect="plain">全部</el-tag>
            <!-- 业务范围是第二个维度，与节点范围取交集。没配就不显示：否则「全部」容易被读成
                 「业务上也不限」，而实际语义是「该维度不生效」。 -->
            <template v-if="row.asset_mode === 'limited'">
              <el-tag
                v-for="l in (row.asset_labels || [])"
                :key="l.key + '=' + l.value"
                size="small"
                type="info"
                effect="plain"
                class="grp-tag"
              >{{ l.key }}={{ l.value }}</el-tag>
              <el-tag v-if="!(row.asset_labels || []).length" size="small" type="danger" effect="plain" class="grp-tag">
                业务范围为空 → 无可见资产
              </el-tag>
            </template>
          </template>
        </el-table-column>
        <el-table-column v-if="auth.canAny(['roles:manage', 'roles:read'])" label="操作" min-width="140" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="openEdit(row)">编辑</el-button>
            <el-popconfirm
              v-if="!row.builtin && auth.can('roles:manage')"
              :title="`确认删除角色「${roleLabel(row.name)}」？`"
              @confirm="removeRole(row)"
            >
              <template #reference>
                <el-button link type="danger" size="small">删除</el-button>
              </template>
            </el-popconfirm>
            <span v-else-if="row.builtin" class="muted">内置不可删</span>
          </template>
        </el-table-column>
      </el-table>
    </SectionCard>

    <!-- 新建 / 编辑角色 -->
    <el-dialog v-model="formVisible" :title="editing ? '编辑角色' : '新建角色'" width="min(720px, 94vw)" top="5vh">
      <el-form :model="form" label-width="84px" @submit.prevent>
        <el-form-item label="角色名" required>
          <el-input v-model="form.name" :disabled="editing" placeholder="英文标识，如 custom_ops" />
        </el-form-item>
        <el-form-item label="说明">
          <el-input v-model="form.description" placeholder="角色用途说明" />
        </el-form-item>
        <el-form-item label="默认范围">
          <div class="scope-box">
            <el-radio-group v-model="form.scopeMode">
              <el-radio value="global">全部节点</el-radio>
              <el-radio value="restricted">限定分组</el-radio>
            </el-radio-group>
            <el-select
              v-if="form.scopeMode === 'restricted'"
              v-model="form.scopeGroups"
              multiple
              filterable
              placeholder="选择默认可见的节点分组"
              class="scope-select"
            >
              <el-option v-for="g in groupNames" :key="g" :label="g" :value="g" />
            </el-select>
            <p v-if="form.scopeMode === 'restricted' && !form.scopeGroups.length" class="scope-warn">
              受限模式未选任何分组时，归属此角色的用户将看不到任何节点资源。
            </p>
            <!-- 业务维度：与节点维度是两个独立维度，**取交集**（都放行才看得见）。
                 默认「不限」= 该维度不生效，因此存量角色的行为不会因为这次上线而变化。 -->
            <div class="scope-sub">
              <div class="scope-sub-title">
                业务范围（按资产标签 <code>{{ assetKey }}</code> 划）
              </div>
              <el-radio-group v-model="form.assetMode">
                <el-radio value="all">不限</el-radio>
                <el-radio value="limited">限定取值</el-radio>
              </el-radio-group>
              <el-select
                v-if="form.assetMode === 'limited'"
                v-model="form.assetValues"
                multiple
                filterable
                allow-create
                default-first-option
                :reserve-keyword="false"
                placeholder="选择或直接输入标签值（如 pay）"
                class="scope-select"
              >
                <el-option v-for="v in assetValueOptions" :key="v" :label="v" :value="v" />
              </el-select>
              <p class="scope-note">
                与上面的节点范围是<b>且</b>的关系：两个范围都放行才看得见该资产。
                标签值要先打在资产上（台账 → 资产 → 标签/批量维护）；<b>没打标签的资产不属于任何业务范围</b>。
              </p>
              <p v-if="form.assetMode === 'limited' && !form.assetValues.length" class="scope-warn">
                限定了业务范围却没有取值时，归属此角色的人将看不到任何资产（这是"无权限"，不是"不限"）。
              </p>
            </div>
          </div>
        </el-form-item>
        <el-form-item label="权限点">
          <div class="perm-matrix">
            <!-- 工具条：47 个权限点平铺在 19 个折叠域内，没有搜索/筛选就只能靠翻 -->
            <div class="perm-toolbar">
              <el-input
                v-model="permSearch"
                size="small"
                clearable
                :prefix-icon="Search"
                placeholder="搜索权限点或说明"
                class="perm-search"
              />
              <el-checkbox v-model="riskOnly" size="small">仅看高风险</el-checkbox>
              <span class="perm-count">已选 {{ form.permissions.length }} / {{ totalPermCount }} 项</span>
            </div>

            <el-alert type="warning" :closable="false" class="perm-hint">
              <template #title>
                带 <span class="risk-badge">高风险</span> 标记的权限涉及系统变更或敏感信息，请谨慎授予。
              </template>
            </el-alert>

            <el-collapse v-model="activeDomains" class="perm-collapse">
              <el-collapse-item v-for="d in visibleDomains" :key="d.domain" :name="d.domain">
                <template #title>
                  <span class="domain-title">{{ domainLabel(d.domain) }}</span>
                  <span class="domain-count">（已选 {{ selectedInDomain(d) }} / {{ d.items.length }} 项）</span>
                </template>
                <div class="domain-actions">
                  <el-button link size="small" @click="selectDomain(d)">全选本域</el-button>
                  <el-button link size="small" @click="clearDomain(d)">清空本域</el-button>
                </div>
                <el-checkbox-group v-model="form.permissions" class="perm-grid">
                  <el-checkbox
                    v-for="p in d.items"
                    :key="p.key"
                    :value="p.key"
                    class="perm-check"
                  >
                    <span class="perm-body">
                      <span class="perm-key">
                        <span v-if="isRisk(p.key)" class="risk-badge">高风险</span>{{ p.key }}
                      </span>
                      <span class="perm-desc">{{ p.description }}</span>
                    </span>
                  </el-checkbox>
                </el-checkbox-group>
              </el-collapse-item>
            </el-collapse>
            <div v-if="!visibleDomains.length" class="perm-empty">
              {{ permSearch.trim() || riskOnly ? '没有匹配的权限点' : '权限目录为空（可能加载失败或暂无权限点）' }}
            </div>
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="formVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="saveRole">{{ editing ? '保存' : '创建' }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, watch, onMounted } from 'vue'
import { Plus, Search } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import http from '../../api/http'
import PageHeader from '../common/PageHeader.vue'
import SectionCard from '../common/SectionCard.vue'
import EmptyState from '../common/EmptyState.vue'
import { useAuth } from '../../composables/useAuth'
import { roleLabel } from '../../composables/useRoleLabels'

const auth = useAuth()

// 后端 PermissionCatalog 的 Domain 已是中文（"仪表盘"/"主机 / 节点"…），因此这里只对
// **旧版后端可能返回的英文域键**做兜底翻译；中文域直接原样显示（不再有英文 key 永远命中不到的问题）。
const DOMAIN_LABELS = {
  dashboard: '仪表盘',
  nodes: '主机/节点',
  groups: '节点分组',
  middleware: '中间件',
  metrics: '数据导出',
  alerts: '告警',
  notify: '通知',
  report: '报告',
  security: '安全中心',
  audit: '审计',
  agent: 'Agent',
  system: '系统',
  users: '用户',
  roles: '角色',
}
function domainLabel(d) {
  return DOMAIN_LABELS[d] || d
}

// 高风险权限点：涉及系统变更或敏感信息。原来是顶部一段纯文本列举，勾选框上看不到任何标记。
// 取自后端权限目录中语义上属"系统变更 / 敏感信息"的点，另含后端显式标注"高风险"的 ops:exec。
const RISK_KEYS = new Set([
  'system:upgrade',
  'agent:secret:read',
  'agent:upgrade',
  'security:write',
  'notify:write',
  'users:manage',
  'roles:manage',
  'audit:export',
  'assets:export',
  'ops:exec',
])
function isRisk(key) {
  return RISK_KEYS.has(key)
}

const roles = ref([])
const users = ref([])
const catalog = reactive({ domains: [], assetScopeLabelKey: '' })
const groupNames = ref([])
const loading = ref(false)
const saving = ref(false)
const activeDomains = ref([])

// 权限矩阵筛选
const permSearch = ref('')
const riskOnly = ref(false)

// 影响用户数：按用户的 roles 反查
const userCounts = computed(() => {
  const m = {}
  for (const u of users.value) {
    for (const r of u.roles || []) m[r] = (m[r] || 0) + 1
  }
  return m
})
function userCountOf(name) {
  return userCounts.value[name] || 0
}

const totalPermCount = computed(() =>
  catalog.domains.reduce((n, d) => n + ((d.items && d.items.length) || 0), 0)
)

// 按搜索词 / 仅看高风险过滤后的域；空域直接剔除，避免一堆空折叠项
const visibleDomains = computed(() => {
  const kw = permSearch.value.trim().toLowerCase()
  return catalog.domains
    .map((d) => {
      let items = d.items || []
      if (riskOnly.value) items = items.filter((p) => isRisk(p.key))
      if (kw) {
        items = items.filter(
          (p) => p.key.toLowerCase().includes(kw) || String(p.description || '').toLowerCase().includes(kw)
        )
      }
      return { ...d, items }
    })
    .filter((d) => d.items.length)
})

// 筛选生效时自动展开命中的域，否则筛完还得手动一个个点开
watch([permSearch, riskOnly], () => {
  if (permSearch.value.trim() || riskOnly.value) {
    activeDomains.value = visibleDomains.value.map((d) => d.domain)
  }
})

function selectedInDomain(d) {
  return (d.items || []).filter((p) => form.permissions.includes(p.key)).length
}
function selectDomain(d) {
  for (const p of d.items || []) {
    if (!form.permissions.includes(p.key)) form.permissions.push(p.key)
  }
}
function clearDomain(d) {
  const keys = new Set((d.items || []).map((p) => p.key))
  form.permissions = form.permissions.filter((k) => !keys.has(k))
}

// 业务范围的约定标签键由服务端给出（可配）：硬编码在前端的话，部署方改过键之后
// 表单会静默地去查一个不存在的键，看起来像"没有可选值"。
const assetKey = computed(
  () => auth.principal.assetScopeLabelKey || catalog.assetScopeLabelKey || 'biz'
)
// 候选值（可选）：拿不到就留空，表单仍允许直接输入——不因为"没有候选项"就把能力藏起来。
const assetValueOptions = ref([])

const formVisible = ref(false)
const editing = ref(false)
const form = reactive({
  name: '',
  description: '',
  permissions: [],
  scopeMode: 'global',
  scopeGroups: [],
  // 业务维度：默认「不限」（= 该维度不生效），与后端的空模式一致。
  assetMode: 'all',
  assetValues: [],
})

async function loadAll() {
  loading.value = true
  try {
    const [r, c, g, u] = await Promise.all([
      http.listRoles().catch(() => ({ roles: [] })),
      http.permissionCatalog().catch(() => ({ domains: [] })),
      http.listGroups().catch(() => ({ groups: [] })),
      http.listUsers().catch(() => ({ users: [] })),
    ])
    roles.value = (Array.isArray(r) ? r : r.roles || []).map((x) =>
      typeof x === 'string' ? { name: x, builtin: false, permissions: [] } : x
    )
    catalog.domains = (c && c.domains) || []
    catalog.assetScopeLabelKey = (c && c.assetScopeLabelKey) || ''
    const gs = Array.isArray(g) ? g : g.groups || []
    groupNames.value = gs.map((x) => (typeof x === 'string' ? x : x.name)).filter(Boolean)
    users.value = Array.isArray(u) ? u : u.users || []
    // 折叠态默认收起；打开编辑时再按"已选域"展开（见 openEdit）
    activeDomains.value = []
    await loadAssetValues()
  } catch (e) {
    ElMessage.error(e.message || '加载角色数据失败')
  } finally {
    loading.value = false
  }
}

// loadAssetValues 取该标签键下的候选取值（服务端按调用者的可见节点收窄）。
// 失败（例如没有 assets:read）只是没有候选，不影响填写——`allow-create` 仍可直接输入。
async function loadAssetValues() {
  try {
    const d = await http.get('/api/v1/assets/label-values?key=' + encodeURIComponent(assetKey.value))
    assetValueOptions.value = (d && d.values) || []
  } catch (e) {
    assetValueOptions.value = []
  }
}

function resetForm() {
  form.name = ''
  form.description = ''
  form.permissions = []
  form.scopeMode = 'global'
  form.scopeGroups = []
  form.assetMode = 'all'
  form.assetValues = []
  permSearch.value = ''
  riskOnly.value = false
}

function openCreate() {
  editing.value = false
  resetForm()
  activeDomains.value = []
  formVisible.value = true
}

function openEdit(row) {
  editing.value = true
  form.name = row.name
  form.description = row.description || ''
  form.permissions = Array.isArray(row.permissions) ? [...row.permissions] : []
  form.scopeMode = row.scope_mode === 'restricted' ? 'restricted' : 'global'
  form.scopeGroups = Array.isArray(row.scope_groups) ? [...row.scope_groups] : []
  form.assetMode = row.asset_mode === 'limited' ? 'limited' : 'all'
  form.assetValues = Array.isArray(row.asset_labels) ? row.asset_labels.map((x) => x.value) : []
  permSearch.value = ''
  riskOnly.value = false
  // 默认收起，但把"已选权限所在的域"展开，否则编辑时看不到当前勾了什么
  activeDomains.value = catalog.domains
    .filter((d) => (d.items || []).some((p) => form.permissions.includes(p.key)))
    .map((d) => d.domain)
  formVisible.value = true
}

// buildAssetLabels 拼业务范围的选择器。
//
// 不限时返回**空数组**而不是省略该字段：服务端不允许"声明 all 却带着选择器"，
// 显式给空数组才表达"这次就是把业务维度关掉"，与"没传这个字段（不改）"区分开。
function buildAssetLabels() {
  return form.assetMode === 'limited'
    ? form.assetValues.map((v) => ({ key: assetKey.value, value: v }))
    : []
}

async function saveRole() {
  if (!form.name) {
    ElMessage.warning('请输入角色名')
    return
  }
  saving.value = true
  try {
    if (editing.value) {
      await http.updateRole(form.name, {
        description: form.description,
        permissions: [...form.permissions],
        scopeMode: form.scopeMode,
        scopeGroups: [...form.scopeGroups],
        assetMode: form.assetMode,
        assetLabels: buildAssetLabels(),
      })
      ElMessage.success('已保存')
    } else {
      await http.createRole({
        name: form.name,
        description: form.description,
        permissions: [...form.permissions],
        scopeMode: form.scopeMode,
        scopeGroups: [...form.scopeGroups],
        assetMode: form.assetMode,
        assetLabels: buildAssetLabels(),
      })
      ElMessage.success('已创建')
    }
    formVisible.value = false
    await loadAll()
  } catch (e) {
    ElMessage.error(e.message || '保存失败')
  } finally {
    saving.value = false
  }
}

async function removeRole(row) {
  try {
    await http.deleteRole(row.name)
    ElMessage.success('已删除')
    await loadAll()
  } catch (e) {
    ElMessage.error(e.message || '删除失败')
  }
}

onMounted(loadAll)
</script>

<style scoped>
.rbac-view {
  display: flex;
  flex-direction: column;
  gap: var(--sp-4);
}
.page-alert {
  margin-bottom: var(--sp-3);
}
.builtin-tag {
  margin-left: 6px;
  font-size: var(--fs-sm);
  color: var(--t3);
}
.cnt-zero {
  color: var(--t3);
}
.grp-tag {
  margin-left: 4px;
}
.scope-box {
  width: 100%;
}
.scope-select {
  width: 100%;
  margin-top: var(--sp-3);
}
.scope-warn {
  margin: var(--sp-2) 0 0;
  font-size: var(--fs-sm);
  color: var(--warn);
}
/* 业务范围是第二个维度，与节点范围是「且」的关系：视觉上也要能看出这是两块，
   否则很容易被读成"选一个就行"。 */
.scope-sub {
  margin-top: var(--sp-4);
  padding-top: var(--sp-3);
  border-top: 1px dashed var(--bd-strong);
}
.scope-sub-title {
  margin-bottom: var(--sp-2);
  font-size: var(--fs-sm);
  color: var(--t3);
}
.scope-note {
  margin: var(--sp-2) 0 0;
  font-size: var(--fs-xs);
  line-height: 1.6;
  color: var(--t3);
}

/* —— 权限矩阵 —— */
.perm-matrix {
  width: 100%;
}
.perm-toolbar {
  display: flex;
  align-items: center;
  gap: var(--sp-3);
  flex-wrap: wrap;
  margin-bottom: var(--sp-2);
}
.perm-search {
  max-width: 240px;
}
.perm-count {
  margin-left: auto;
  font-size: var(--fs-xs);
  color: var(--t3);
}
.perm-hint {
  margin-bottom: var(--sp-2);
}
.perm-collapse {
  border: none;
}
.perm-collapse :deep(.el-collapse-item__header) {
  background: transparent;
  color: var(--t1);
  font-size: var(--fs-sm);
  font-weight: 600;
  padding-left: var(--sp-1);
  border-bottom: 1px solid var(--bd);
}
.perm-collapse :deep(.el-collapse-item__wrap) {
  background: transparent;
  border-bottom: none;
}
.perm-collapse :deep(.el-collapse-item__content) {
  padding: var(--sp-3) var(--sp-1) var(--sp-1);
}
.domain-title {
  font-weight: 600;
}
.domain-count {
  margin-left: 6px;
  font-size: var(--fs-xs);
  font-weight: 400;
  color: var(--t3);
}
.domain-actions {
  display: flex;
  gap: var(--sp-3);
  margin-bottom: var(--sp-1);
}
.perm-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
  gap: var(--sp-1) var(--sp-3);
  max-height: 360px;
  overflow-y: auto;
  padding-right: var(--sp-1);
}
/* el-checkbox 自带原生 input：键盘可达、读屏可识别（原来是 <label> + @click.prevent，47 个勾选框键盘完全不可达） */
.perm-grid :deep(.el-checkbox) {
  display: flex;
  align-items: flex-start;
  height: auto;
  margin-right: 0;
  padding: var(--sp-1) var(--sp-2);
  border-radius: var(--r-sm);
  white-space: normal;
}
.perm-grid :deep(.el-checkbox:hover) {
  background: var(--fill-2);
}
.perm-grid :deep(.el-checkbox__input) {
  margin-top: 2px;
}
.perm-grid :deep(.el-checkbox__label) {
  white-space: normal;
  line-height: 1.3;
  padding-left: var(--sp-2);
}
.perm-body {
  display: flex;
  flex-direction: column;
  gap: 1px;
  min-width: 0;
}
.perm-key {
  font-family: var(--mono);
  font-size: var(--fs-sm);
  color: var(--t1);
  word-break: break-all;
}
.perm-desc {
  font-size: var(--fs-xs);
  color: var(--t3);
  line-height: 1.3;
}
.risk-badge {
  display: inline-block;
  margin-right: 4px;
  padding: 0 5px;
  font-size: 11px;
  font-family: inherit;
  line-height: 16px;
  color: var(--danger);
  background: var(--danger-dim);
  border: 1px solid var(--danger-bd);
  border-radius: var(--r-xs);
}
.perm-empty {
  padding: var(--sp-3) var(--sp-2);
  font-size: var(--fs-sm);
  color: var(--t3);
}
</style>
