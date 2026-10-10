// AlertsView 的「测试事件」按钮可见性由 notify:write 决定。
// 该按钮会真实写事件并向通知渠道发消息，前端隐藏只是体验层（服务端独立审计拒绝），
// 但写反「未加载即放行」或漏判权限点，都会让受限用户看到无权执行的按钮。
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import useAuth from '../composables/useAuth'

// 组件挂载时会发一串请求（/alerts、/rules、/alerts/stats、/maintenance…），
// 统一返回安全的空结构，避免测试触发真实网络或渲染期取值崩溃。
vi.mock('../api/http', () => ({
  default: {
    get: vi.fn(async () => ({
      alerts: [],
      rules: [],
      groups: [],
      templates: [],
      acks: {},
      inhibits: [],
      firing: 0,
      suppressed: 0,
      total: 0,
      bySeverity: { critical: 0, warning: 0, info: 0 },
    })),
    post: vi.fn(async () => ({ ok: true })),
    put: vi.fn(async () => ({})),
    del: vi.fn(async () => ({})),
  },
  getToken: vi.fn(() => ''),
}))

vi.mock('echarts', () => ({
  init: vi.fn(() => ({ setOption: vi.fn(), resize: vi.fn(), dispose: vi.fn(), on: vi.fn(), isDisposed: vi.fn(() => false) })),
  graphic: { LinearGradient: vi.fn() },
}))

const routerPush = vi.fn()
const routerReplace = vi.fn()
// route.query 可变：用例之间按需切换深链参数（首页研判条带 node/rule 过来）
let routeQuery = {}

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: routerPush, replace: routerReplace }),
  // 组件读 route.query 处理「从日志页建规则」的深链与研判条深链，缺这个导出会在挂载期直接抛
  useRoute: () => ({ query: routeQuery, path: '/alerts' }),
}))

// 组件按需 import 的图标必须在这里都有同名导出，否则会在加载期直接抛
// "No \"Xxx\" export is defined on the \"@element-plus/icons-vue\" mock"
// 而让整个文件的用例全部失败（新增图标时记得同步这里）。
vi.mock('@element-plus/icons-vue', () => ({
  Plus: { name: 'Plus', template: '<i />' },
  Printer: { name: 'Printer', template: '<i />' },
  // FilterChips 用到的关闭图标
  Close: { name: 'Close', template: '<i />' },
}))

vi.mock('element-plus', () => ({
  ElMessage: { success: vi.fn(), error: vi.fn(), warning: vi.fn() },
  ElMessageBox: { confirm: vi.fn(async () => true) },
}))

vi.mock('./RuleModal.vue', () => ({ default: { name: 'RuleModal', template: '<div />' } }))

import AlertsView from './AlertsView.vue'

// el-button 需渲染默认插槽，才能断言按钮文案；其余 Element Plus 组件一律静默，
// 避免表格列的作用域插槽在 stub 下取值崩溃。
const ButtonStub = { name: 'ElButton', template: '<button class="el-button"><slot /></button>' }
const NoopStub = { name: 'Noop', template: '<div />' }

const stubs = {
  'el-button': ButtonStub,
  'el-alert': NoopStub,
  'el-date-picker': NoopStub,
  'el-dialog': NoopStub,
  'el-divider': NoopStub,
  'el-drawer': NoopStub,
  'el-dropdown': NoopStub,
  'el-dropdown-item': NoopStub,
  'el-dropdown-menu': NoopStub,
  // 规范件（SectionCard / EmptyState / StatusPill / PartialBar）内部都用 el-icon，
  // 不 stub 会为每个卡片报一次 "Failed to resolve component: el-icon"。
  'el-icon': NoopStub,
  'el-input': NoopStub,
  'el-input-number': NoopStub,
  'el-link': NoopStub,
  'el-option': NoopStub,
  'el-pagination': NoopStub,
  'el-radio': NoopStub,
  'el-radio-button': NoopStub,
  'el-radio-group': NoopStub,
  'el-select': NoopStub,
  'el-switch': NoopStub,
  'el-table': NoopStub,
  'el-table-column': NoopStub,
  'el-tag': NoopStub,
}

const { principal, clear } = useAuth()
const flush = () => new Promise((resolve) => setTimeout(resolve, 0))

function mountView() {
  return mount(AlertsView, {
    global: {
      stubs,
      directives: { loading: {} },
    },
  })
}

describe('AlertsView 测试事件按钮的权限可见性', () => {
  let wrapper

  beforeEach(() => {
    clear() // → loaded=false、permissions=[]，避免跨用例状态泄漏
  })

  afterEach(() => {
    if (wrapper) wrapper.unmount()
    wrapper = undefined
    clear()
  })

  it('已加载授权且只有 alerts:read 时不显示测试事件按钮', async () => {
    principal.loaded = true
    principal.permissions = ['alerts:read']

    wrapper = mountView()
    await flush()

    expect(wrapper.text()).not.toContain('测试事件')
  })

  it('已加载授权且有 notify:write 时显示测试事件按钮', async () => {
    principal.loaded = true
    principal.permissions = ['alerts:read', 'notify:write']

    wrapper = mountView()
    await flush()

    expect(wrapper.text()).toContain('测试事件')
  })

  it('未加载授权（单管理员 / 未启用 RBAC 兼容）时显示测试事件按钮', async () => {
    principal.loaded = false
    principal.permissions = []

    wrapper = mountView()
    await flush()

    expect(wrapper.text()).toContain('测试事件')
  })
})

// 首页研判条「查看详情」会带 ?node=&rule= 跳到告警中心。
// 关键不是"跳过来了"，而是"筛选被接住并显式回显"——否则用户面对空表只会以为数据没加载。
describe('AlertsView 深链筛选回显', () => {
  let wrapper

  beforeEach(() => {
    clear()
    principal.loaded = true
    principal.permissions = ['alerts:read']
    routeQuery = {}
    routerPush.mockClear()
    routerReplace.mockClear()
  })

  afterEach(() => {
    if (wrapper) wrapper.unmount()
    wrapper = undefined
    routeQuery = {}
    clear()
  })

  it('带 node/rule 进来时回显为可撤销的筛选条', async () => {
    routeQuery = { node: 'web-01', rule: '磁盘使用率过高' }
    wrapper = mountView()
    await flush()

    const chips = wrapper.findAll('.fc-chip')
    expect(chips).toHaveLength(2)
    expect(wrapper.text()).toContain('节点：web-01')
    expect(wrapper.text()).toContain('规则：磁盘使用率过高')
  })

  it('无深链参数时不渲染筛选条（避免出现空壳）', async () => {
    wrapper = mountView()
    await flush()
    expect(wrapper.findAll('.fc-chip')).toHaveLength(0)
  })

  it('清空筛选时同步清 URL（否则刷新又被参数带回筛选态）', async () => {
    routeQuery = { node: 'web-01' }
    wrapper = mountView()
    await flush()

    // 点筛选条右侧的「清空筛选」
    const clearBtn = wrapper.findAll('button').find((b) => b.text().includes('清空筛选'))
    expect(clearBtn).toBeTruthy()
    await clearBtn.trigger('click')

    expect(routerReplace).toHaveBeenCalledWith({ path: '/alerts', query: {} })
  })
})
