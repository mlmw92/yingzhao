<template>
  <div class="rbac-view">
    <PageHeader
      title="用户管理"
      desc="管理可登录系统的账号及其角色与数据范围；删除 / 禁用最后一个超级管理员会被服务端拒绝"
    >
      <template #actions>
        <el-button v-if="auth.can('users:manage')" type="primary" :icon="Plus" @click="openCreate">新建用户</el-button>
      </template>
    </PageHeader>

    <SectionCard title="用户列表" dense>
      <template #actions>
        <span v-if="users.length" class="u-total">共 {{ users.length }} 个账号</span>
      </template>

      <div class="filter-bar">
        <el-input
          v-model="keyword"
          size="small"
          clearable
          :prefix-icon="Search"
          placeholder="搜索用户名 / 昵称 / 角色"
          class="u-search"
        />
      </div>

      <el-table :data="pagedUsers" v-loading="loading" stripe>
        <template #empty>
          <EmptyState
            :title="keyword.trim() ? '没有匹配的用户' : '暂无用户'"
            :hints="keyword.trim() ? ['换个关键词再试'] : ['账号可由具备 users:manage 权限的管理员在右上角创建']"
          />
        </template>
        <el-table-column prop="username" label="用户名" min-width="120" />
        <el-table-column prop="display_name" label="昵称" min-width="110" />
        <el-table-column label="角色" min-width="150">
          <template #default="{ row }">
            <el-tag v-for="r in row.roles" :key="r" size="small" class="role-tag" effect="dark" type="info">
              {{ roleLabel(r) }}
            </el-tag>
            <span v-if="!row.roles || !row.roles.length" class="muted">—</span>
          </template>
        </el-table-column>
        <!-- 数据范围拆成两列：节点维度与业务维度是"且"的关系，挤在一格很难扫读 -->
        <el-table-column label="节点范围" min-width="130">
          <template #default="{ row }">
            <template v-if="row.scope && row.scope.mode === 'restricted'">
              <el-tag size="small" type="warning" effect="plain">受限</el-tag>
              <el-tag v-for="g in row.scope.groups" :key="g" size="small" class="grp-tag">{{ g }}</el-tag>
              <span v-if="!row.scope.groups || !row.scope.groups.length" class="muted">（无分组，无可见资源）</span>
            </template>
            <el-tag v-else size="small" type="success" effect="plain">全部</el-tag>
          </template>
        </el-table-column>
        <!-- 业务范围是第二个维度，与节点范围取交集；没配就不显示值（语义是"该维度不生效"） -->
        <el-table-column label="业务范围" min-width="140">
          <template #default="{ row }">
            <template v-if="row.scope && row.scope.asset_mode === 'limited'">
              <el-tag
                v-for="l in (row.scope.asset_labels || [])"
                :key="l.key + '=' + l.value"
                size="small"
                type="info"
                effect="plain"
                class="grp-tag"
              >{{ l.key }}={{ l.value }}</el-tag>
              <el-tag v-if="!(row.scope.asset_labels || []).length" size="small" type="danger" effect="plain" class="grp-tag">
                业务范围为空 → 无可见资产
              </el-tag>
            </template>
            <span v-else class="muted">不限</span>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="84">
          <template #default="{ row }">
            <el-tag v-if="row.status === 'disabled'" size="small" type="info" effect="plain">已禁用</el-tag>
            <el-tag v-else size="small" type="success" effect="plain">启用</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="创建时间" min-width="140">
          <template #default="{ row }">{{ fmtDate(row.created_at) }}</template>
        </el-table-column>
        <el-table-column v-if="auth.can('users:manage')" label="操作" min-width="252" fixed="right" class-name="op-col">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="openEdit(row)">编辑</el-button>
            <el-button link type="warning" size="small" @click="openReset(row)">重置密码</el-button>
            <!-- 禁用/启用、删除直接平铺：此前收进「更多」下拉，反而多一次点击才能看到
                 （用户要求把「更多」里的按钮都放回操作列） -->
            <el-button v-if="row.status !== 'disabled'" link type="info" size="small" @click="disableUser(row)">禁用</el-button>
            <el-button v-else link type="success" size="small" @click="enableUser(row)">启用</el-button>
            <el-button link type="danger" size="small" @click="confirmRemove(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>

      <el-pagination
        v-if="filteredUsers.length"
        class="u-pager"
        layout="total, sizes, prev, pager, next"
        :total="filteredUsers.length"
        :page-size="pageSize"
        :current-page="page"
        :page-sizes="[10, 20, 50]"
        background
        @current-change="(p) => (page = p)"
        @size-change="(s) => { pageSize = s; page = 1 }"
      />
    </SectionCard>

    <!-- 新建 / 编辑用户 -->
    <el-dialog v-model="formVisible" :title="editing ? '编辑用户' : '新建用户'" width="min(560px, 94vw)">
      <el-form :model="form" label-width="92px" @submit.prevent>
        <el-form-item label="用户名" required>
          <el-input v-model="form.username" :disabled="editing" placeholder="3-32 位字母/数字/下划线" />
        </el-form-item>
        <el-form-item label="昵称">
          <el-input v-model="form.displayName" placeholder="可选，展示用名称" />
        </el-form-item>
        <el-form-item label="密码" :required="!editing">
          <el-input v-model="form.password" type="password" show-password autocomplete="new-password" :placeholder="editing ? '留空则不修改' : '登录密码'" />
        </el-form-item>
        <el-form-item label="角色" required>
          <el-select v-model="form.roles" multiple placeholder="选择角色" class="full">
            <el-option v-for="r in roleNames" :key="r" :label="roleLabel(r)" :value="r" />
          </el-select>
        </el-form-item>
        <el-form-item label="数据范围">
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
              placeholder="选择可见的节点分组"
              class="scope-select"
            >
              <el-option v-for="g in groupNames" :key="g" :label="g" :value="g" />
            </el-select>
            <p v-if="form.scopeMode === 'restricted' && !form.scopeGroups.length" class="scope-warn">
              受限模式未选任何分组时，该用户将看不到任何节点资源。
            </p>
            <!-- 业务维度：与节点维度独立，两者**取交集**。默认「不限」= 该维度不生效。
                 这一项通常不必按人配（角色上配一次即可），但"某个人这次只让它看某业务线"
                 是真实需求，且它**只收窄**：用户自己配了限定就生效，不会被角色的"不限"抵消。 -->
            <div class="scope-sub">
              <div class="scope-sub-title">
                业务范围（按资产标签 <code>{{ assetKey }}</code> 划，可选）
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
                与节点范围是<b>且</b>的关系。<b>这里配了限定就一定会生效</b>（不会被角色上的"不限"抵消）——
                它只用来给这个人单独收窄。
              </p>
              <p v-if="form.assetMode === 'limited' && !form.assetValues.length" class="scope-warn">
                限定了业务范围却没有取值时，该用户将看不到任何资产（这是"无权限"，不是"不限"）。
              </p>
            </div>
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="formVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="saveUser">{{ editing ? '保存' : '创建' }}</el-button>
      </template>
    </el-dialog>

    <!-- 重置密码 -->
    <el-dialog v-model="pwdVisible" title="重置密码" width="460px">
      <el-form :model="pwdForm" label-width="92px" @submit.prevent>
        <el-form-item label="用户">
          <el-input :model-value="pwdForm.username" disabled />
        </el-form-item>
        <el-form-item label="新密码" required>
          <el-input v-model="pwdForm.password" type="password" show-password autocomplete="new-password" placeholder="新登录密码" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="pwdVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="savePassword">确定</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, watch, onMounted } from 'vue'
import { Plus, Search } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import http from '../../api/http'
import PageHeader from '../common/PageHeader.vue'
import SectionCard from '../common/SectionCard.vue'
import EmptyState from '../common/EmptyState.vue'
import { useAuth } from '../../composables/useAuth'
import { roleLabel } from '../../composables/useRoleLabels'

const auth = useAuth()

// 将后端 ISO 时间戳格式化为本地可读日期
function fmtDate(v) {
  if (!v) return '—'
  try {
    const d = new Date(v)
    if (isNaN(d.getTime())) return v
    const pad = (n) => String(n).padStart(2, '0')
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
  } catch (e) {
    return v
  }
}

const users = ref([])
const roleNames = ref([])
const groupNames = ref([])
const loading = ref(false)
const saving = ref(false)

// 搜索 + 分页
const keyword = ref('')
const page = ref(1)
const pageSize = ref(10)
const filteredUsers = computed(() => {
  const kw = keyword.value.trim().toLowerCase()
  if (!kw) return users.value
  return users.value.filter(
    (u) =>
      String(u.username || '').toLowerCase().includes(kw) ||
      String(u.display_name || '').toLowerCase().includes(kw) ||
      (u.roles || []).some(
        (r) => String(r).toLowerCase().includes(kw) || roleLabel(r).toLowerCase().includes(kw)
      )
  )
})
const pagedUsers = computed(() => {
  const start = (page.value - 1) * pageSize.value
  return filteredUsers.value.slice(start, start + pageSize.value)
})
watch(keyword, () => {
  page.value = 1
})

const formVisible = ref(false)
const editing = ref(false)
const form = reactive({
  username: '',
  displayName: '',
  password: '',
  roles: [],
  scopeMode: 'global',
  scopeGroups: [],
  // 业务维度：默认「不限」（= 该维度不生效），与后端的空模式一致。
  assetMode: 'all',
  assetValues: [],
})

// 业务范围的约定标签键由服务端给出（/auth/me）：它可配，写死在前端会在改了键的部署上静默失效。
const assetKey = computed(() => auth.principal.assetScopeLabelKey || 'biz')
const assetValueOptions = ref([])

// loadAssetValues 取候选标签值；失败（例如没有 assets:read）只是没有候选，
// `allow-create` 仍允许直接输入——不因为"看不到候选"就把这个能力藏起来。
async function loadAssetValues() {
  try {
    const d = await http.get('/api/v1/assets/label-values?key=' + encodeURIComponent(assetKey.value))
    assetValueOptions.value = (d && d.values) || []
  } catch (e) {
    assetValueOptions.value = []
  }
}

const pwdVisible = ref(false)
const pwdForm = reactive({ username: '', password: '' })

async function loadAll() {
  loading.value = true
  try {
    const [u, r, g] = await Promise.all([
      http.listUsers(),
      http.listRoles().catch(() => ({ roles: [] })),
      http.listGroups().catch(() => ({ groups: [] })),
    ])
    users.value = Array.isArray(u) ? u : u.users || []
    roleNames.value = (Array.isArray(r) ? r : r.roles || []).map((x) => (typeof x === 'string' ? x : x.name))
    const gs = Array.isArray(g) ? g : g.groups || []
    groupNames.value = gs.map((x) => (typeof x === 'string' ? x : x.name)).filter(Boolean)
    // 业务范围的候选取值（拿不到就只是没有候选，表单仍可手输）
    await loadAssetValues()
  } catch (e) {
    ElMessage.error(e.message || '加载用户列表失败')
  } finally {
    loading.value = false
  }
}

function resetForm() {
  form.username = ''
  form.displayName = ''
  form.password = ''
  form.roles = []
  form.scopeMode = 'global'
  form.scopeGroups = []
  form.assetMode = 'all'
  form.assetValues = []
}

function openCreate() {
  editing.value = false
  resetForm()
  formVisible.value = true
}

function openEdit(row) {
  editing.value = true
  form.username = row.username
  form.displayName = row.display_name || ''
  form.password = ''
  form.roles = Array.isArray(row.roles) ? [...row.roles] : []
  form.scopeMode = row.scope && row.scope.mode === 'restricted' ? 'restricted' : 'global'
  form.scopeGroups = row.scope && Array.isArray(row.scope.groups) ? [...row.scope.groups] : []
  // 服务端返回的 scope 是 snake_case（`asset_mode` / `asset_labels`，与 display_name / scope_mode 一致）：
  // 按 camelCase 读会静默拿到 undefined → 表单显示"不限" → 一保存就把业务范围抹掉（权限静默放大）。
  form.assetMode = row.scope && row.scope.asset_mode === 'limited' ? 'limited' : 'all'
  form.assetValues = row.scope && Array.isArray(row.scope.asset_labels)
    ? row.scope.asset_labels.map((x) => x.value)
    : []
  formVisible.value = true
}

function buildScope() {
  const node = form.scopeMode === 'restricted'
    ? { mode: 'restricted', groups: [...form.scopeGroups] }
    : { mode: 'global', groups: [] }
  // 业务维度：不限时显式给 all + 空数组。服务端不允许"声明 all 却带着选择器"，
  // 也不把"limited 却没有选择器"当成不限（那是无权限）——两种状态都要能表达出来。
  const asset = form.assetMode === 'limited'
    ? {
        asset_mode: 'limited',
        asset_labels: form.assetValues.map((v) => ({ key: assetKey.value, value: v })),
      }
    : { asset_mode: 'all', asset_labels: [] }
  return { ...node, ...asset }
}

async function saveUser() {
  if (!form.username || form.username.length < 3) {
    ElMessage.warning('用户名至少 3 位')
    return
  }
  if (!editing.value && !form.password) {
    ElMessage.warning('请设置登录密码')
    return
  }
  if (!form.roles.length) {
    ElMessage.warning('请至少分配一个角色')
    return
  }
  saving.value = true
  try {
    if (editing.value) {
      await http.updateUser(form.username, {
        display_name: form.displayName,
        roles: form.roles,
        scope: buildScope(),
      })
      ElMessage.success('已保存')
    } else {
      await http.createUser({
        username: form.username,
        display_name: form.displayName,
        password: form.password,
        roles: form.roles,
        scope: buildScope(),
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

function openReset(row) {
  pwdForm.username = row.username
  pwdForm.password = ''
  pwdVisible.value = true
}

async function savePassword() {
  if (!pwdForm.password) {
    ElMessage.warning('请输入新密码')
    return
  }
  saving.value = true
  try {
    await http.resetUserPassword(pwdForm.username, pwdForm.password)
    ElMessage.success('密码已重置')
    pwdVisible.value = false
  } catch (e) {
    ElMessage.error(e.message || '重置失败')
  } finally {
    saving.value = false
  }
}

async function disableUser(row) {
  try {
    await http.disableUser(row.username)
    ElMessage.success('已禁用')
    await loadAll()
  } catch (e) {
    ElMessage.error(e.message || '禁用失败')
  }
}

async function enableUser(row) {
  try {
    await http.enableUser(row.username)
    ElMessage.success('已启用')
    await loadAll()
  } catch (e) {
    ElMessage.error(e.message || '启用失败')
  }
}

// 删除是破坏性动作：用带用户名的确认框，避免"到底删的是谁"看不出来
async function confirmRemove(row) {
  const who = row.display_name ? `${row.display_name}（${row.username}）` : row.username
  try {
    await ElMessageBox.confirm(`确认删除用户「${who}」？删除后该账号将无法登录，且此操作不可恢复。`, '删除用户', {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  try {
    await http.deleteUser(row.username)
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
.u-total {
  font-size: var(--fs-xs);
  color: var(--t3);
}
.u-search {
  max-width: 260px;
}
.role-tag {
  margin-right: 4px;
}
.grp-tag {
  margin: 0 4px 2px 0;
}
/* 操作列：5 个链接按钮靠右一行排开，不换行。
   用 flex + gap 而非 .el-button + .el-button 外边距——flex 布局下相邻选择器仍生效，
   两套间距叠加会让列宽估算失准。 */
:deep(.op-col .cell) {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 4px;
  white-space: nowrap;
}
:deep(.op-col .cell .el-button + .el-button) { margin-left: 0; }
.u-pager {
  display: flex;
  justify-content: flex-end;
  margin-top: var(--sp-3);
}
.full {
  width: 100%;
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
/* 业务范围是第二个维度，与节点范围是「且」：视觉上要能看出这是两块。 */
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
</style>
