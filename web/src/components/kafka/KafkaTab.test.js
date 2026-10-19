// Kafka Tab：声明化之后的行为回归（拓扑、版本列、指标数组）。
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import http from '../../api/http'
import KafkaTab from './KafkaTab.vue'

vi.mock('../../api/http', () => ({
  default: { get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() },
}))
vi.mock('../../charts/echarts', () => ({
  initChart: vi.fn(() => ({ setOption: vi.fn(), dispose: vi.fn(), resize: vi.fn(), isDisposed: () => false })),
  mwTrendOption: (series, opts) => ({ series: series || [], __opts: opts }),
}))

if (!globalThis.ResizeObserver) {
  globalThis.ResizeObserver = class { observe() {} unobserve() {} disconnect() {} }
}

const PAYLOAD = {
  type: 'kafka',
  instances: [{
    node: 'web-01', instance: '10.0.0.10:9092', name: 'kafka-1',
    role: 'broker', version: '3.7', group: 'dev-kafka', up: true,
    metrics: [
      { key: 'kafka_broker_count', label: 'Broker 数', value: 3 },
      { key: 'kafka_consumer_lag', label: '消费延迟', value: 120 },
    ],
  }],
}

let wrappers = []
function mountTab() {
  const w = mount(KafkaTab, { global: { plugins: [ElementPlus] } })
  wrappers.push(w)
  return w
}

beforeEach(() => {
  vi.clearAllMocks()
  wrappers = []
  http.get.mockResolvedValue(PAYLOAD)
})
afterEach(() => { for (const w of wrappers) w.unmount() })

describe('KafkaTab 声明式实例页', () => {
  it('实例拓扑按 group 聚合，版本列来自注册表', async () => {
    const w = mountTab()
    await flushPromises()
    const text = w.text()
    expect(text).toContain('实例拓扑')
    expect(text).toContain('dev-kafka')
    expect(text).toContain('3.7')
    expect(text).toContain('kafka-1')
  })

  it('指标列与 KPI 来自 metrics 数组', async () => {
    const w = mountTab()
    await flushPromises()
    const text = w.text()
    expect(text).toContain('Broker 数')
    expect(text).toContain('总消费延迟')
    expect(text).toContain('120')
  })
})
