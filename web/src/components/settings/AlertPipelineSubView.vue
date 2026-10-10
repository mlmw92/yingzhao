<template>
  <el-card class="settings-card" shadow="never">
    <template #header>
      <div class="card-head">
        <div>
          <span class="title">告警事件管道</span>
          <span class="sub">告警派发前改写标签、补充字段并按渠道渲染通知内容；保存后即时生效</span>
        </div>
        <div class="head-actions">
          <el-button :disabled="saving || loading" @click="load">重新加载</el-button>
          <el-button type="primary" :loading="saving" :disabled="loading" @click="save">保存</el-button>
        </div>
      </div>
    </template>

    <!-- 管道示意图：三段顺序与依赖此前完全不可见（只有一段文字说明）。
         节点显示规则条数，点击滚动定位到对应区块。 -->
    <div class="pipeline" aria-label="告警事件管道三步">
      <button type="button" class="pipe-node" @click="scrollTo(secRelabel)">
        <span class="pipe-idx">1</span>
        <span class="pipe-name">标签重写</span>
        <span class="pipe-count">{{ form.relabels.length }} 条</span>
      </button>
      <span class="pipe-arrow" aria-hidden="true">→</span>
      <button type="button" class="pipe-node" @click="scrollTo(secEnrich)">
        <span class="pipe-idx">2</span>
        <span class="pipe-name">标签增补</span>
        <span class="pipe-count">{{ form.enrich.length }} 条</span>
      </button>
      <span class="pipe-arrow" aria-hidden="true">→</span>
      <button type="button" class="pipe-node" @click="scrollTo(secTemplates)">
        <span class="pipe-idx">3</span>
        <span class="pipe-name">消息模板</span>
        <span class="pipe-count">{{ form.templates.length }} 条</span>
      </button>
      <span class="pipe-flow">上一步的输出 = 下一步的输入；未命中模板时回退系统内置描述</span>
    </div>

    <el-alert type="info" :closable="false" show-icon class="tip">
      执行顺序：先按「标签重写」「标签增补」变换事件标签，再按渠道匹配「消息模板」渲染通知正文。
      内置标签为 name / rule / node / instance / severity / metric；未命中模板或模板渲染失败时，
      一律回退系统内置描述，不会丢失告警。
    </el-alert>

    <!-- 示例模板：三个段的字段语义（四种 op、when 条件、模板可用变量）不直观，
         而配错的症状是「不报错、通知只是没按预期」——给可直接套用的场景化模板最省事。
         追加而非覆盖：不清掉用户已有的规则。 -->
    <div class="examples">
      <div class="section-head">
        <h3 class="section-title">示例模板</h3>
        <div class="head-actions">
          <el-select v-model="exampleKey" placeholder="选择一个场景" size="small" class="ex-select">
            <el-option-group v-for="g in EXAMPLE_GROUPS" :key="g.label" :label="g.label">
              <el-option v-for="ex in g.items" :key="ex.key" :label="ex.label" :value="ex.key" />
            </el-option-group>
          </el-select>
          <el-button size="small" type="primary" plain :disabled="!currentExample" @click="applyExample">
            追加到下方
          </el-button>
        </div>
      </div>
      <template v-if="currentExample">
        <div class="field-hint">{{ currentExample.hint }}</div>
        <pre class="preview-box">{{ currentExamplePreview }}</pre>
      </template>
      <div v-else class="field-hint">
        这些示例的条件只用「内置标签」（一定存在），避免出现「看着对、永远不生效」。
        追加只会往对应表格末尾加规则，不会覆盖你已有的配置；加完记得点右上角「保存」。
      </div>
    </div>

    <el-divider />

    <!-- 1. 标签重写 -->
    <div ref="secRelabel" class="section">
      <div class="section-head">
        <h3 class="section-title">① 标签重写</h3>
        <el-button size="small" @click="addRelabel">添加规则</el-button>
      </div>
      <div class="field-hint">
        「条件」留空表示无条件生效；填 <code>k=v,k2=v2</code> 表示仅当这些标签相等时才应用。点行首箭头展开完整编辑。
      </div>
      <el-table :data="form.relabels" size="small" border>
        <el-table-column type="expand" width="44">
          <template #default="{ row }">
            <div class="expand-grid">
              <label class="eg-item">
                <span class="eg-label">操作</span>
                <el-select v-model="row.op" size="small">
                  <el-option v-for="o in OPS" :key="o.value" :label="o.label" :value="o.value" />
                </el-select>
              </label>
              <label class="eg-item">
                <span class="eg-label">源标签</span>
                <el-input v-model="row.source" size="small" placeholder="如 node" />
              </label>
              <label class="eg-item">
                <span class="eg-label">目标标签</span>
                <el-input v-model="row.target" size="small" placeholder="如 host" />
              </label>
              <label class="eg-item">
                <span class="eg-label">正则</span>
                <el-input v-model="row.pattern" size="small" placeholder="正则（仅替换）" :disabled="row.op !== 'replace'" />
              </label>
              <label class="eg-item">
                <span class="eg-label">替换为</span>
                <el-input v-model="row.replace" size="small" placeholder="如 CN-$1" :disabled="row.op !== 'replace'" />
              </label>
              <label class="eg-item">
                <span class="eg-label">置值</span>
                <el-input v-model="row.value" size="small" placeholder="置值（仅置值）" :disabled="row.op !== 'set'" />
              </label>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="规则" min-width="220">
          <template #default="{ row }"><span class="rule-summary">{{ relabelSummary(row) }}</span></template>
        </el-table-column>
        <el-table-column label="条件" width="200">
          <template #default="{ row }">
            <el-input
              :model-value="row._whenText"
              size="small"
              placeholder="env=prod"
              @update:model-value="(v) => setWhenText(row, v)"
            />
            <span v-if="row._whenInvalid && row._whenInvalid.length" class="when-bad">条件需为 k=v 形式</span>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="64" align="center">
          <template #default="{ $index }">
            <el-button type="danger" link :icon="Delete" @click="removeRow('relabels', $index)" />
          </template>
        </el-table-column>
        <template #empty>
          <EmptyState
            size="table-inline"
            title="暂无标签重写规则"
            :hints="['点右上角「添加规则」新增', '或从上方「示例模板」追加一个场景']"
          />
        </template>
      </el-table>
    </div>

    <!-- 2. 标签增补 -->
    <div ref="secEnrich" class="section">
      <div class="section-head">
        <h3 class="section-title">② 标签增补</h3>
        <el-button size="small" @click="addEnrich">添加规则</el-button>
      </div>
      <div class="field-hint">按条件向事件注入固定标签，常用于补充 team / owner / env 等归属信息。</div>
      <el-table :data="form.enrich" size="small" border>
        <el-table-column label="目标标签" width="180">
          <template #default="{ row }">
            <el-input v-model="row.target" size="small" placeholder="如 team" />
          </template>
        </el-table-column>
        <el-table-column label="注入值" width="220">
          <template #default="{ row }">
            <el-input v-model="row.value" size="small" placeholder="如 sre" />
          </template>
        </el-table-column>
        <el-table-column label="条件">
          <template #default="{ row }">
            <el-input
              :model-value="row._whenText"
              size="small"
              placeholder="env=prod（留空表示无条件）"
              @update:model-value="(v) => setWhenText(row, v)"
            />
            <span v-if="row._whenInvalid && row._whenInvalid.length" class="when-bad">条件需为 k=v 形式</span>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="64" align="center">
          <template #default="{ $index }">
            <el-button type="danger" link :icon="Delete" @click="removeRow('enrich', $index)" />
          </template>
        </el-table-column>
        <template #empty>
          <EmptyState
            size="table-inline"
            title="暂无标签增补规则"
            :hints="['按条件注入固定标签，如 team / owner / env']"
          />
        </template>
      </el-table>
    </div>

    <!-- 3. 消息模板 -->
    <div ref="secTemplates" class="section">
      <div class="section-head">
        <h3 class="section-title">③ 消息模板</h3>
        <el-button size="small" @click="addTemplate">添加模板</el-button>
      </div>
      <div class="field-hint">
        模板为 Go text/template 语法，可访问事件字段（<code v-pre>{{.RuleName}}</code>、
        <code v-pre>{{.Node}}</code>、<code v-pre>{{.Value}}</code>、<code v-pre>{{.State}}</code>）与标签
        （<code v-pre>{{index .Labels "team"}}</code>）；时间戳用
        <code v-pre>{{ts .StartsAt}}</code> 格式化（毫秒 → 本地时间）。渠道专属模板优先于「全部渠道」。
      </div>
      <el-table :data="form.templates" size="small" border>
        <el-table-column label="名称" width="150">
          <template #default="{ row }">
            <el-input v-model="row.name" size="small" placeholder="如 钉钉精简版" />
          </template>
        </el-table-column>
        <el-table-column label="渠道" width="140">
          <template #default="{ row }">
            <el-select v-model="row.channel" size="small">
              <el-option v-for="c in CHANNELS" :key="c.value" :label="c.label" :value="c.value" />
            </el-select>
          </template>
        </el-table-column>
        <el-table-column label="级别" width="200">
          <template #default="{ row }">
            <el-select v-model="row.severity" size="small" multiple collapse-tags placeholder="全部级别">
              <el-option v-for="s in SEVERITIES" :key="s.value" :label="s.label" :value="s.value" />
            </el-select>
          </template>
        </el-table-column>
        <el-table-column label="规则 ID（逗号分隔，支持前缀）" width="200">
          <template #default="{ row }">
            <el-input
              :model-value="(row.ruleIds || []).join(',')"
              size="small"
              placeholder="留空=全部规则"
              @update:model-value="(v) => (row.ruleIds = splitList(v))"
            />
          </template>
        </el-table-column>
        <el-table-column label="模板内容">
          <template #default="{ row }">
            <el-input
              v-model="row.template"
              type="textarea"
              :rows="3"
              class="mono"
              :placeholder="templatePlaceholder"
            />
          </template>
        </el-table-column>
        <el-table-column label="操作" width="64" align="center">
          <template #default="{ $index }">
            <el-button type="danger" link :icon="Delete" @click="removeRow('templates', $index)" />
          </template>
        </el-table-column>
        <template #empty>
          <EmptyState
            size="table-inline"
            title="暂无消息模板"
            :hints="['未命中模板时使用系统内置描述', '或从上方「示例模板」追加一个场景']"
          />
        </template>
      </el-table>
    </div>

    <el-divider />

    <!-- 4. 效果预览 -->
    <div class="section">
      <div class="section-head">
        <h3 class="section-title">效果预览</h3>
        <div class="head-actions">
          <el-select v-model="previewChannel" size="small" class="pv-select">
            <el-option v-for="c in CHANNELS" :key="c.value" :label="c.label || '全部渠道'" :value="c.value" />
          </el-select>
          <el-button size="small" :loading="previewing" @click="runPreview">按当前配置试算</el-button>
        </div>
      </div>
      <div class="field-hint">
        试算使用「当前表单里尚未保存的配置」，不会影响线上；样例事件可自行编辑，留空则用服务端内置样例。
      </div>
      <el-row :gutter="20">
        <el-col :xs="24" :md="11">
          <el-form-item label="样例事件（JSON）">
            <el-input v-model="sampleEventText" type="textarea" :rows="14" class="mono" />
          </el-form-item>
        </el-col>
        <el-col :xs="24" :md="13">
          <el-form-item label="变换后的标签">
            <pre class="preview-box">{{ preview.labels }}</pre>
          </el-form-item>
          <el-form-item label="渲染后的通知正文">
            <pre class="preview-box">{{ preview.message }}</pre>
          </el-form-item>
          <el-form-item label="原始描述（回退值）">
            <pre class="preview-box muted">{{ preview.originalMessage }}</pre>
          </el-form-item>
        </el-col>
      </el-row>
    </div>

    <!-- 5. 高级：YAML 直编（与服务端落盘文件同格式） -->
    <el-collapse class="adv">
      <el-collapse-item name="adv">
        <template #title>
          <h3 class="section-title">高级：YAML 直编</h3>
          <span class="field-hint inline">与服务端落盘文件同格式，便于批量修改、复制与备份</span>
        </template>
        <el-alert
          v-if="yamlStale"
          type="warning"
          :closable="false"
          show-icon
          class="tip"
          title="表单已改动，下方 YAML 仍是旧内容；点「从表单生成」同步后再编辑，否则「应用到表单」会把表单改回旧配置。"
        />
        <el-input v-model="rawYaml" type="textarea" :rows="16" class="mono" />
        <div class="btn-line">
          <el-button size="small" @click="applyRawYaml">应用到表单</el-button>
          <el-button size="small" @click="genRawYaml">从表单生成</el-button>
          <el-button size="small" @click="checkYaml">仅做语法检查</el-button>
          <span v-if="yamlMsg" class="yaml-msg" :class="{ bad: yamlBad }">{{ yamlMsg }}</span>
        </div>
      </el-collapse-item>
    </el-collapse>
  </el-card>
</template>

<script setup>
import { ref, computed, watch, nextTick, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Delete } from '@element-plus/icons-vue'
// js-yaml v4 的 ESM 构建只提供具名导出，无 default
import { dump as yamlDump, load as yamlLoad } from 'js-yaml'
import http from '../../api/http'
import EmptyState from '../common/EmptyState.vue'

const OPS = [
  { label: '重命名', value: 'rename' },
  { label: '删除', value: 'delete' },
  { label: '正则替换', value: 'replace' },
  { label: '置值', value: 'set' },
]
const CHANNELS = [
  { label: '', value: '' },
  { label: '邮件', value: 'email' },
  { label: 'Webhook', value: 'webhook' },
  { label: '钉钉', value: 'dingtalk' },
  { label: '飞书', value: 'feishu' },
  { label: '企业微信', value: 'wecom' },
]
const SEVERITIES = [
  { label: 'Info', value: 'info' },
  { label: 'Warning', value: 'warning' },
  { label: 'Critical', value: 'critical' },
]

// 内置样例事件（与服务端 preview 的默认样例保持一致，便于离线理解字段含义）
const SAMPLE_EVENT = {
  id: 'preview-1',
  ruleId: 'rule-cpu-usage',
  ruleName: 'CPU 使用率过高',
  node: 'web-01',
  nodeIp: '10.0.0.11',
  metric: 'cpu_usage',
  value: 92.4,
  operator: '>',
  threshold: 90,
  severity: 'critical',
  state: 'firing',
  message: '主机 web-01 CPU 使用率 92.40% 超过阈值 90.00%',
  labels: { env: 'prod' },
}

// 模板输入框占位示例（字面量放在脚本里，避免与 Vue 插值定界符冲突）
const templatePlaceholder = '【{{.Severity}}】{{.RuleName}} | {{.Node}}'

// ---------- 示例模板 ----------
const emptyWhen = null
const relabelPayload = (op, source, target = '', extra = {}) => ({
  op, source, target, pattern: '', replace: '', value: '', when: emptyWhen, ...extra,
})
const enrichPayload = (target, value, when = null) => ({ target, value, when })
const templatePayload = (name, channel, template) => ({ name, channel, severity: [], ruleIds: [], template })

const EXAMPLE_GROUPS = [
  {
    label: '标签重写（relabel）',
    items: [
      {
        key: 'rl-hide-instance',
        kind: 'relabel',
        label: '删除 instance 标签（不把内部实例地址带出去）',
        hint: '通知里通常不需要实例地址；删掉既少一行噪声，也避免内部地址外泄到外部渠道。',
        payload: relabelPayload('delete', 'instance'),
      },
      {
        key: 'rl-name',
        kind: 'relabel',
        label: '规则名标签改名：name → alertname',
        hint: '对接外部平台（Alertmanager 风格的 alertname）时统一标签名。',
        payload: relabelPayload('rename', 'name', 'alertname'),
      },
      {
        key: 'rl-node',
        kind: 'relabel',
        label: '节点标签改名：node → host',
        hint: '部分工单/外部平台认 host。注意：若事件本身已带 host 标签，本规则会用它覆盖。',
        payload: relabelPayload('rename', 'node', 'host'),
      },
      {
        key: 'rl-sev-cn',
        kind: 'relabel',
        label: '级别本地化：新增 severity_cn = 紧急 / 警告 / 信息',
        hint: '三条规则，只在对应级别时生效；写进新标签而不是改写 severity——原始级别仍供抑制、路由等既有逻辑使用，模板里引用 severity_cn 即可。',
        payload: [
          relabelPayload('set', '', 'severity_cn', { value: '紧急', when: { match: { severity: 'critical' } } }),
          relabelPayload('set', '', 'severity_cn', { value: '警告', when: { match: { severity: 'warning' } } }),
          relabelPayload('set', '', 'severity_cn', { value: '信息', when: { match: { severity: 'info' } } }),
        ],
      },
    ],
  },
  {
    label: '标签增补（enrich）',
    items: [
      {
        key: 'en-critical-team',
        kind: 'enrich',
        label: '给紧急告警补归属：team=sre',
        hint: '条件用内置标签 severity（一定存在）；value 请改成你们实际的团队名。',
        payload: enrichPayload('team', 'sre', { match: { severity: 'critical' } }),
      },
      {
        key: 'en-action',
        kind: 'enrich',
        label: '按级别打处理动作：action=page / action=ticket',
        hint: '紧急走电话/值班（page）、警告走工单（ticket）；通知模板里可用 {{index .Labels "action"}} 展示。',
        payload: [
          enrichPayload('action', 'page', { match: { severity: 'critical' } }),
          enrichPayload('action', 'ticket', { match: { severity: 'warning' } }),
        ],
      },
      {
        key: 'en-env',
        kind: 'enrich',
        label: '给全部告警补环境：env=prod',
        hint: '条件留空＝无条件生效。只有一个生产环境时，让通知里始终带 env 最省心。',
        payload: enrichPayload('env', 'prod'),
      },
    ],
  },
  {
    label: '消息模板（template）',
    items: [
      {
        key: 'tpl-im',
        kind: 'template',
        label: '通用模板：单行精简版（适合钉钉/飞书/企微）',
        hint: '渠道留空＝全部渠道兜底。单行排版在 IM 里最清楚，邮件则由下面那条渠道专属模板接管。',
        payload: templatePayload(
          '单行精简版',
          '',
          '{{if eq .State "resolved"}}✅ 已恢复{{else}}🔥 {{.Severity}}{{end}} | {{.RuleName}} | {{.Node}} | {{.Metric}}={{.Value}}（阈值 {{.Threshold}}）',
        ),
      },
      {
        key: 'tpl-email',
        kind: 'template',
        label: '邮件详细版（多行，含实例与恢复时间）',
        hint: '渠道专属模板优先于通用模板：邮件会用它，IM 继续用上面的单行版。',
        payload: templatePayload(
          '邮件详细版',
          'email',
          '{{if eq .State "resolved"}}告警已恢复{{else}}告警触发{{end}}\n' +
            '规则：{{.RuleName}}（{{.RuleID}}）\n' +
            '级别：{{.Severity}}{{if .Labels.severity_cn}}（{{index .Labels "severity_cn"}}）{{end}}\n' +
            '节点：{{.Node}}{{if .NodeIP}}（{{.NodeIP}}）{{end}}\n' +
            '{{if .Instance}}实例：{{.Instance}}\n{{end}}' +
            '指标：{{.Metric}}\n' +
            '当前值：{{.Value}} {{.Operator}} 阈值 {{.Threshold}}\n' +
            '描述：{{.Message}}\n' +
            '触发时间：{{ts .StartsAt}}{{if .EndsAt}}\n恢复时间：{{ts .EndsAt}}{{end}}\n' +
            '{{if .Labels.action}}处理动作：{{index .Labels "action"}}\n{{end}}',
        ),
      },
    ],
  },
]

const ALL_EXAMPLES = EXAMPLE_GROUPS.flatMap((g) => g.items)
const exampleKey = ref('')
const currentExample = computed(() => ALL_EXAMPLES.find((e) => e.key === exampleKey.value) || null)
const currentExamplePreview = computed(() => {
  const ex = currentExample.value
  if (!ex) return ''
  const payloads = Array.isArray(ex.payload) ? ex.payload : [ex.payload]
  return payloads.map((p) => JSON.stringify(p, null, 2)).join('\n')
})

function applyExample() {
  const ex = currentExample.value
  if (!ex) return
  const target =
    ex.kind === 'relabel' ? form.value.relabels
      : ex.kind === 'enrich' ? form.value.enrich
        : form.value.templates
  const payloads = Array.isArray(ex.payload) ? ex.payload : [ex.payload]
  for (const p of payloads) target.push(withWhenText(JSON.parse(JSON.stringify(p))))
  ElMessage.success(`已追加 ${payloads.length} 条规则，核对后请点右上角「保存」使其生效`)
}

const loading = ref(false)
const saving = ref(false)
const previewing = ref(false)
const form = ref({ relabels: [], enrich: [], templates: [] })
const rawYaml = ref('')
const yamlMsg = ref('')
const yamlBad = ref(false)
const yamlStale = ref(false)
const previewChannel = ref('dingtalk')
const sampleEventText = ref(JSON.stringify(SAMPLE_EVENT, null, 2))
const preview = ref({ labels: '—', message: '—', originalMessage: '—' })

const secRelabel = ref(null)
const secEnrich = ref(null)
const secTemplates = ref(null)
const baselineCfg = ref({ relabels: [], enrich: [], templates: [] })
function scrollTo(el) {
  if (el && typeof el.scrollIntoView === 'function') el.scrollIntoView({ behavior: 'smooth', block: 'start' })
}

// 表单变更即标记 YAML 区过期；fillForm/genRawYaml 内部会把标记复位（suppress 抑制自身写入）
let suppressYamlWatch = false
watch(
  form,
  () => {
    if (!suppressYamlWatch) yamlStale.value = true
  },
  { deep: true },
)

// ---------- when 条件（表格内以 k=v,k2=v2 简写编辑）----------
function whenText(when) {
  const m = (when && when.match) || {}
  return Object.entries(m)
    .map(([k, v]) => `${k}=${v}`)
    .join(',')
}

// 解析 k=v 简写：非法片段（无 =）记入 _whenInvalid，供保存前显式报错，而不是静默丢弃
function setWhenText(row, text) {
  const raw = String(text || '')
  row._whenText = raw
  const match = {}
  const invalid = []
  raw
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
    .forEach((pair) => {
      const i = pair.indexOf('=')
      if (i > 0) match[pair.slice(0, i).trim()] = pair.slice(i + 1).trim()
      else invalid.push(pair)
    })
  row._whenInvalid = invalid
  const matchRegex = (row.when && row.when.matchRegex) || {}
  if (Object.keys(match).length || Object.keys(matchRegex).length) {
    row.when = { match, matchRegex }
  } else {
    row.when = null
  }
}

// 给（从服务端/示例/ YAML 来的）行补上 _whenText 展示字段
function withWhenText(row) {
  return { ...row, _whenText: whenText(row.when), _whenInvalid: [] }
}

function splitList(text) {
  return String(text || '')
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
}

function relabelSummary(row) {
  const opLabel = (OPS.find((o) => o.value === row.op) || {}).label || row.op || '?'
  const src = row.source || '?'
  const tgt = row.target || '?'
  if (row.op === 'delete') return `${opLabel} ${src}`
  if (row.op === 'rename') return `${opLabel} ${src} → ${tgt}`
  if (row.op === 'replace') return `${opLabel} ${src}${row.pattern ? ` /${row.pattern}/ → ${row.replace || '（空）'}` : '（未填正则）'}`
  if (row.op === 'set') return `${opLabel} ${tgt} = ${row.value || '（空）'}`
  return opLabel
}

// ---------- 行操作 ----------
function addRelabel() {
  form.value.relabels.push(withWhenText({ op: 'rename', source: '', target: '', pattern: '', replace: '', value: '', when: null }))
}
function addEnrich() {
  form.value.enrich.push(withWhenText({ target: '', value: '', when: null }))
}
function addTemplate() {
  form.value.templates.push(withWhenText({ name: '', channel: '', severity: [], ruleIds: [], template: '' }))
}

async function removeRow(key, index) {
  try {
    await ElMessageBox.confirm('确定删除这条规则吗？删除后需点右上角「保存」才会生效。', '删除规则', {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  form.value[key].splice(index, 1)
}

// ---------- 类型规整 ----------
function str(v) {
  return v === undefined || v === null ? '' : String(v)
}

function toArray(v) {
  if (Array.isArray(v)) return v.map(str).filter(Boolean)
  const s = str(v).trim()
  return s ? splitList(s) : []
}

function asArray(v) {
  return Array.isArray(v) ? v.filter((x) => x && typeof x === 'object') : []
}

function whenOrNull(w) {
  return w && typeof w === 'object' && !Array.isArray(w) ? w : null
}

// ---------- 配置归一化 ----------
function normalizeWhen(when) {
  if (!when) return null
  const out = {}
  const match = {}
  Object.entries(when.match || {}).forEach(([k, v]) => {
    if (k && k.trim() && String(v).trim()) match[k.trim()] = String(v).trim()
  })
  if (Object.keys(match).length) out.match = match
  const matchRegex = {}
  Object.entries(when.matchRegex || {}).forEach(([k, v]) => {
    if (k && k.trim() && String(v).trim()) matchRegex[k.trim()] = String(v).trim()
  })
  if (Object.keys(matchRegex).length) out.matchRegex = matchRegex
  return Object.keys(out).length ? out : null
}

function normalize() {
  const cfg = { relabels: [], enrich: [], templates: [] }

  for (const r of form.value.relabels) {
    const op = str(r.op).trim()
    if (!op) continue
    const item = { op }
    const source = str(r.source).trim()
    const target = str(r.target).trim()
    if (source) item.source = source
    if (target) item.target = target
    if (op === 'replace') {
      item.pattern = str(r.pattern)
      item.replace = str(r.replace)
    }
    if (op === 'set') item.value = str(r.value)
    const when = normalizeWhen(r.when)
    if (when) item.when = when
    cfg.relabels.push(item)
  }

  for (const e of form.value.enrich) {
    const target = str(e.target).trim()
    if (!target) continue
    const item = { target, value: str(e.value) }
    const when = normalizeWhen(e.when)
    if (when) item.when = when
    cfg.enrich.push(item)
  }

  for (const t of form.value.templates) {
    const item = {
      name: str(t.name).trim() || '未命名模板',
      channel: str(t.channel),
      severity: toArray(t.severity),
      ruleIds: toArray(t.ruleIds),
      template: str(t.template),
    }
    const when = normalizeWhen(t.when)
    if (when) item.when = when
    cfg.templates.push(item)
  }

  return cfg
}

// ---------- 保存前校验：这些错误此前会被静默丢弃（不报错、通知只是没按预期）----------
function validate() {
  const relabels = form.value.relabels
  for (let i = 0; i < relabels.length; i++) {
    const r = relabels[i]
    const n = i + 1
    const op = str(r.op).trim()
    if ((op === 'rename' || op === 'replace') && !str(r.source).trim()) {
      return `标签重写 第 ${n} 行：操作「${(OPS.find((o) => o.value === op) || {}).label || op}」必须填「源标签」`
    }
    if ((op === 'rename' || op === 'set') && !str(r.target).trim()) {
      return `标签重写 第 ${n} 行：操作「${(OPS.find((o) => o.value === op) || {}).label || op}」必须填「目标标签」`
    }
    if (op === 'replace') {
      const pat = str(r.pattern)
      if (!pat.trim()) return `标签重写 第 ${n} 行：正则替换必须填「正则」`
      try {
        new RegExp(pat)
      } catch {
        return `标签重写 第 ${n} 行：正则非法，无法编译`
      }
    }
  }
  for (let i = 0; i < form.value.enrich.length; i++) {
    if (!str(form.value.enrich[i].target).trim()) return `标签增补 第 ${i + 1} 行：必须填「目标标签」`
  }
  for (let i = 0; i < form.value.templates.length; i++) {
    if (!str(form.value.templates[i].template).trim()) return `消息模板 第 ${i + 1} 行：模板内容不能为空`
  }
  const allRows = [...form.value.relabels, ...form.value.enrich, ...form.value.templates]
  for (const row of allRows) {
    if (row._whenInvalid && row._whenInvalid.length) {
      return `条件「${row._whenInvalid.join('、')}」格式非法，需写成 k=v 形式`
    }
  }
  return ''
}

// ---------- 加载 / 保存 ----------
function fillForm(cfg) {
  const c = cfg && typeof cfg === 'object' ? cfg : {}
  // 记录基线：reset() 直接从这里恢复，无需重新请求
  baselineCfg.value = c
  suppressYamlWatch = true
  form.value = {
    relabels: asArray(c.relabels).map((r) =>
      withWhenText({
        op: str(r.op) || 'rename',
        source: str(r.source),
        target: str(r.target),
        pattern: str(r.pattern),
        replace: str(r.replace),
        value: str(r.value),
        when: whenOrNull(r.when),
      }),
    ),
    enrich: asArray(c.enrich).map((e) =>
      withWhenText({ target: str(e.target), value: str(e.value), when: whenOrNull(e.when) }),
    ),
    templates: asArray(c.templates).map((t) =>
      withWhenText({
        name: str(t.name),
        channel: str(t.channel),
        severity: toArray(t.severity),
        ruleIds: toArray(t.ruleIds),
        template: str(t.template),
        when: whenOrNull(t.when),
      }),
    ),
  }
  rawYaml.value = yamlDump(dumpable(), { noRefs: true, lineWidth: 120 })
  yamlMsg.value = ''
  yamlStale.value = false
  nextTick(() => {
    suppressYamlWatch = false
  })
}

// 只导出真实字段，剔除 _whenText / _whenInvalid 这类展示辅助
function dumpable() {
  const clean = (arr, keys) =>
    arr.map((row) => {
      const o = {}
      for (const k of keys) o[k] = row[k]
      return o
    })
  return {
    relabels: clean(form.value.relabels, ['op', 'source', 'target', 'pattern', 'replace', 'value', 'when']),
    enrich: clean(form.value.enrich, ['target', 'value', 'when']),
    templates: clean(form.value.templates, ['name', 'channel', 'severity', 'ruleIds', 'template', 'when']),
  }
}

async function load() {
  loading.value = true
  try {
    const cfg = await http.get('/api/v1/alert-pipeline')
    fillForm(cfg || {})
  } catch (e) {
    ElMessage.error('加载告警管道配置失败：' + e.message)
  } finally {
    loading.value = false
  }
}

async function save() {
  const err = validate()
  if (err) {
    ElMessage.error('校验未通过：' + err)
    return
  }
  saving.value = true
  try {
    const cfg = normalize()
    await http.put('/api/v1/alert-pipeline', cfg)
    ElMessage.success('已保存并即时生效')
    // 用服务端归一化后的结果回填，避免前端与服务端理解不一致
    fillForm(cfg)
  } catch (e) {
    ElMessage.error('保存失败：' + e.message)
  } finally {
    saving.value = false
  }
}

// ---------- 预览 ----------
async function runPreview() {
  previewing.value = true
  try {
    const payload = { config: normalize(), channel: previewChannel.value }
    if (sampleEventText.value.trim()) {
      payload.event = JSON.parse(sampleEventText.value)
    }
    const res = await http.post('/api/v1/alert-pipeline/preview', payload)
    preview.value = {
      labels: JSON.stringify(res.labels || {}, null, 2),
      message: res.message || '（空）',
      originalMessage: res.originalMessage || '（空）',
    }
  } catch (e) {
    ElMessage.error('试算失败：' + e.message)
  } finally {
    previewing.value = false
  }
}

// ---------- 高级：YAML 直编 ----------
function parseRawYaml() {
  const text = (rawYaml.value || '').trim()
  const parsed = text ? yamlLoad(text) : {}
  if (parsed !== null && parsed !== undefined && typeof parsed !== 'object') {
    throw new Error('顶层必须是映射（relabels / enrich / templates）')
  }
  return parsed || {}
}

function yamlReason(e) {
  return String((e && e.message) || e).split('\n')[0]
}

async function applyRawYaml() {
  // 「应用到表单」会用 YAML 覆盖当前表单，先确认避免误丢未保存的表单改动
  try {
    await ElMessageBox.confirm('「应用到表单」会用下方 YAML 覆盖当前表单内容（含未保存的改动）。确定继续？', '应用到表单', {
      type: 'warning',
      confirmButtonText: '覆盖表单',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  try {
    fillForm(parseRawYaml())
    yamlBad.value = false
    yamlMsg.value = '语法正确，已应用到表单，确认无误后请点击「保存」'
    ElMessage.success('已应用到表单，确认无误后请点击保存')
  } catch (e) {
    yamlBad.value = true
    yamlMsg.value = '语法错误：' + yamlReason(e)
    ElMessage.error('YAML 解析失败：' + yamlReason(e))
  }
}

function checkYaml() {
  try {
    parseRawYaml()
    yamlBad.value = false
    yamlMsg.value = '语法正确'
  } catch (e) {
    yamlBad.value = true
    yamlMsg.value = '语法错误：' + yamlReason(e)
  }
}

function genRawYaml() {
  rawYaml.value = yamlDump(dumpable(), { noRefs: true, lineWidth: 120 })
  yamlBad.value = false
  yamlStale.value = false
  yamlMsg.value = '已按当前表单重新生成'
}

function reset() {
  // 放弃未保存改动：用缓存基线回填，不重新请求
  fillForm(baselineCfg.value)
  ElMessage.info('已放弃未保存的改动')
}

onMounted(load)

// dirty 契约：供 SettingsView 容器渲染吸底「未保存改动」条
defineExpose({ dirty: computed(() => yamlStale.value), save, reset })
</script>

<style scoped>
.settings-card {
  border: 1px solid var(--bd);
}
.card-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
}
.card-head .title {
  font-size: var(--fs-base);
  font-weight: 600;
  color: var(--t1);
}
.card-head .sub {
  display: block;
  margin-top: 4px;
  font-size: var(--fs-sm);
  color: var(--t2);
}
.head-actions {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-shrink: 0;
}
/* 管道示意图 */
.pipeline {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  padding: 10px 12px;
  margin-bottom: 16px;
  background: var(--fill-1);
  border: 1px solid var(--bd);
  border-radius: var(--r-md);
}
.pipe-node {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-family: inherit;
  font-size: var(--fs-sm);
  color: var(--t1);
  background: var(--s1);
  border: 1px solid var(--bd-strong);
  border-radius: var(--r-md);
  padding: 6px 12px;
  cursor: pointer;
}
.pipe-node:hover {
  border-color: var(--accent);
}
.pipe-node:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 1px;
}
.pipe-idx {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 18px;
  height: 18px;
  border-radius: 50%;
  background: var(--accent-dim);
  color: var(--accent);
  font-size: var(--fs-xs);
  font-weight: 700;
}
.pipe-count {
  color: var(--t3);
  font-size: var(--fs-xs);
}
.pipe-arrow {
  color: var(--t3);
}
.pipe-flow {
  margin-left: 6px;
  font-size: var(--fs-xs);
  color: var(--t3);
}
.tip {
  margin-bottom: 18px;
}
.examples {
  margin-bottom: 6px;
}
.examples .preview-box {
  max-height: 260px;
}
.ex-select {
  width: 320px;
}
.pv-select {
  width: 130px;
}
.section {
  margin-bottom: 22px;
}
.section-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 8px;
}
.section-title {
  font-size: var(--fs-sm);
  font-weight: 600;
  color: var(--t1);
  margin: 0;
}
.field-hint {
  font-size: var(--fs-xs);
  color: var(--t3);
  line-height: 1.6;
  margin-bottom: 8px;
}
.field-hint.inline {
  margin: 0 0 0 10px;
  display: inline;
}
.field-hint code {
  font-family: var(--mono);
  background: var(--fill-3);
  padding: 0 4px;
  border-radius: 3px;
}
.expand-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px 16px;
  padding: 4px 8px 8px;
}
.eg-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.eg-label {
  font-size: var(--fs-xs);
  color: var(--t3);
}
.rule-summary {
  font-family: var(--mono);
  font-size: var(--fs-xs);
  color: var(--t2);
}
.when-bad {
  display: block;
  margin-top: 2px;
  font-size: var(--fs-xs);
  color: var(--danger);
}
.preview-box {
  margin: 0;
  padding: 10px 12px;
  min-height: 44px;
  max-height: 220px;
  overflow: auto;
  font-family: var(--mono);
  font-size: var(--fs-sm);
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-all;
  background: var(--fill-2);
  border: 1px solid var(--bd);
  border-radius: var(--r-sm);
  color: var(--t1);
}
.preview-box.muted {
  color: var(--t2);
}
.mono :deep(textarea) {
  font-family: var(--mono);
  font-size: var(--fs-sm);
}
.btn-line {
  margin-top: 10px;
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}
.yaml-msg {
  font-size: var(--fs-xs);
  color: var(--t2);
}
.yaml-msg.bad {
  color: var(--danger);
}
.adv {
  margin-top: 4px;
}
@media (max-width: 1024px) {
  .expand-grid {
    grid-template-columns: 1fr;
  }
}
</style>
