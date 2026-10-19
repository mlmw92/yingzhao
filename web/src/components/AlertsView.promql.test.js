// PromQL 规则在告警页的展示：规则列表与事件详情都要显示表达式，
// 而不是「指标 运算符 阈值」——后者对 PromQL 规则是空的，看起来像规则没配好。
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

vi.mock('../api/http', () => ({
  default: {
    get: vi.fn(async (url) => {
      if (url === '/api/v1/rules') {
        return {
          rules: [{
            id: 'p1', name: '消费积压', type: 'promql', expr: 'kafka_consumer_lag > 100',
            for: '5m', severity: 'warning', scope: 'all', nodes: [], enabled: true,
          }],
        }
      }
      return {
        alerts: [], groups: [], templates: [], acks: {}, inhibits: [],
        firing: 0, suppressed: 0, total: 0,
        bySeverity: { critical: 0, warning: 0, info: 0 },
      }
    }),
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

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  useRoute: () => ({ query: {}, path: '/alerts' }),
}))

import ElementPlus from 'element-plus'
import AlertsView from './AlertsView.vue'

let wrappers = []
beforeEach(() => { wrappers = [] })
afterEach(() => { for (const w of wrappers) w.unmount() })

function mountView() {
  const w = mount(AlertsView, { global: { plugins: [ElementPlus] }, attachTo: document.body })
  wrappers.push(w)
  return w
}

describe('AlertsView PromQL 规则展示', () => {
  it('规则列表的触发条件显示表达式而不是指标/阈值', async () => {
    const w = mountView()
    await flushPromises()

    const text = w.text()
    expect(text).toContain('PromQL')
    expect(text).toContain('kafka_consumer_lag > 100')
  })
})
