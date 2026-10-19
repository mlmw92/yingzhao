// PromQL 规则表单：类型切换后的表达式输入与试运行。
//
// 用 props.rule 直接载入一条 promql 规则来驱动表单（与用户从列表点开同一条路径），
// 避免在 jsdom 里模拟 el-select 下拉的键盘交互。
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import http from '../api/http'
import RuleModal from './RuleModal.vue'

const { pushMock } = vi.hoisted(() => ({ pushMock: vi.fn() }))

vi.mock('../api/http', () => ({
  default: {
    get: vi.fn(async () => ({ nodes: [], alertGroups: [] })),
    post: vi.fn(async () => ({
      count: 2,
      truncated: false,
      series: [{ labels: { node: 'web-01' }, value: 91 }],
    })),
    put: vi.fn(async () => ({})),
  },
  getToken: vi.fn(() => ''),
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: pushMock, replace: vi.fn() }),
}))

vi.mock('element-plus', async (importOriginal) => {
  const actual = await importOriginal()
  return {
    ...actual,
    ElMessage: { success: vi.fn(), error: vi.fn(), warning: vi.fn() },
    ElMessageBox: { confirm: vi.fn(async () => true), alert: vi.fn(async () => {}) },
  }
})

const PROMQL_RULE = {
  id: 'p1', name: '消费积压', type: 'promql', expr: 'kafka_consumer_lag > 100',
  for: '5m', severity: 'warning', scope: 'all', nodes: [], enabled: true, notify: [],
}

let wrappers = []
function mountRule(props = {}) {
  const w = mount(RuleModal, {
    props: { rule: undefined, groups: [], channels: [], ...props },
    global: { plugins: [ElementPlus] },
    attachTo: document.body,
  })
  wrappers.push(w)
  return w
}

beforeEach(() => {
  vi.clearAllMocks()
  wrappers = []
  Element.prototype.scrollIntoView = vi.fn()
})

afterEach(() => {
  for (const w of wrappers) w.unmount()
})

describe('RuleModal PromQL 规则', () => {
  it('选择 PromQL 后显示表达式输入与试运行', async () => {
    const w = mountRule({ rule: PROMQL_RULE })
    await flushPromises()

    expect(w.text()).toContain('表达式')
    expect(w.text()).toContain('试运行')
    expect(w.find('textarea').element.value).toBe('kafka_consumer_lag > 100')
  })

  it('试运行展示命中条数', async () => {
    const w = mountRule({ rule: PROMQL_RULE })
    await flushPromises()

    const btn = w.findAll('button').find((b) => b.text().includes('试运行'))
    expect(btn).toBeTruthy()
    await btn.trigger('click')
    await flushPromises()

    expect(http.post).toHaveBeenCalledWith('/api/v1/rules/promql/preview', {
      expr: 'kafka_consumer_lag > 100',
    })
    expect(w.text()).toContain('命中 2 条')
  })
})
