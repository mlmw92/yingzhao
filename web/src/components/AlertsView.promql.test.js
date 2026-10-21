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
import http from '../api/http'

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

  const PROMQL_EVENT = {
    id: 'e1', ruleId: 'p1', ruleName: 'CPU 高', ruleType: 'promql',
    node: 'web-01', metric: 'cpu_usage > 0', value: 4.89,
    severity: 'critical', state: 'firing', startsAt: Date.now(), message: 'PromQL 命中',
  }

  async function openDetailFor(event) {
    http.get.mockImplementation(async (url) => {
      if (url === '/api/v1/rules') return { rules: [] }
      if (url.startsWith('/api/v1/alerts/impact')) return {}
      if (url.startsWith('/api/v1/alerts')) {
        return {
          alerts: [event], groups: [], templates: [], acks: {}, inhibits: [],
          firing: 1, suppressed: 0, total: 1,
          bySeverity: { critical: 1, warning: 0, info: 0 },
        }
      }
      if (url.startsWith('/api/v1/ai/investigations')) return { investigation: null }
      return {}
    })

    const w = mountView()
    await flushPromises()
    const btn = w.findAll('button').find((b) => b.text().includes('详情'))
    expect(btn).toBeTruthy()
    await btn.trigger('click')
    await flushPromises()
    return w
  }

  // PromQL 事件的 metric 是表达式，不是指标名：拿它去查 /query/range 只会得到错误。
  // 详情里必须明确说明「不提供单指标趋势」，而不是画一个永远失败的图。
  it('PromQL 事件详情不请求单指标趋势，改为文字说明', async () => {
    await openDetailFor(PROMQL_EVENT)
    expect(document.body.textContent).toContain('不提供单指标趋势')
    expect(http.get.mock.calls.some(([u]) => u.startsWith('/api/v1/query/range'))).toBe(false)
  })

  // 早期事件没有 ruleType（未落库，规则可能已删除）：靠「metric 不是合法指标名」兜底。
  it('历史事件缺少 ruleType 时也能识别 PromQL，不请求趋势', async () => {
    const legacy = { ...PROMQL_EVENT }
    delete legacy.ruleType
    await openDetailFor(legacy)
    expect(document.body.textContent).toContain('不提供单指标趋势')
    expect(http.get.mock.calls.some(([u]) => u.startsWith('/api/v1/query/range'))).toBe(false)
  })
})
