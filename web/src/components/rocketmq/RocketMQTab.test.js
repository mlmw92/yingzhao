// RocketMQ Tab：声明化之后的行为回归。
//
// 与 FastDFS 的差异点都在这里锁住：实例拓扑、版本列（只存在于注册表）、
// 运行时长（注册表兜底，指标里也有）、消息积压的告警色。
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import http from '../../api/http'
import RocketMQTab from './RocketMQTab.vue'

vi.mock('../../api/http', () => ({
  default: { get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() },
}))
// jsdom 没有 canvas，趋势图只验证数据透传，不建真实实例
vi.mock('../../charts/echarts', () => ({
  initChart: vi.fn(() => ({ setOption: vi.fn(), dispose: vi.fn(), resize: vi.fn(), isDisposed: () => false })),
  mwTrendOption: (series, opts) => ({ series: series || [], __opts: opts }),
}))

if (!globalThis.ResizeObserver) {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
}

const PAYLOAD = {
  type: 'rocketmq',
  instances: [{
    node: 'web-01',
    nodeIp: '10.0.0.1',
    instance: '10.0.0.10:9876',
    name: 'mq-1',
    role: 'nameserver',
    version: '5.1.4',
    group: 'dev-rocketmq',
    uptime: 7200,
    up: true,
    metrics: [
      { key: 'rocketmq_broker_count', label: 'Broker 数', value: 2 },
      { key: 'rocketmq_topic_count', label: 'Topic 数', value: 77 },
      { key: 'rocketmq_consumer_lag', label: '消费延迟', value: 7 },
      { key: 'rocketmq_message_accumulation', label: '消息积压', value: 4321 },
    ],
  }],
}

let wrappers = []
function mountTab() {
  const w = mount(RocketMQTab, {
    global: { plugins: [ElementPlus] },
    attachTo: document.body,
  })
  wrappers.push(w)
  return w
}

beforeEach(() => {
  vi.clearAllMocks()
  wrappers = []
  document.body.innerHTML = ''
  http.get.mockResolvedValue(PAYLOAD)
})
afterEach(() => {
  for (const w of wrappers) w.unmount()
})

describe('RocketMQTab 声明式实例页', () => {
  it('实例拓扑按 group 聚合，版本列来自注册表', async () => {
    const w = mountTab()
    await flushPromises()

    const text = w.text()
    expect(text).toContain('实例拓扑')
    expect(text).toContain('dev-rocketmq')
    expect(text).toContain('5.1.4')
    expect(text).toContain('mq-1')
  })

  it('指标列按 metrics 数组渲染，KPI 聚合消息积压', async () => {
    const w = mountTab()
    await flushPromises()

    const text = w.text()
    expect(text).toContain('消息积压')
    expect(text).toContain('总消息积压')
    // 4,321 只可能来自 metrics 数组（旧实现读扁平字段会得到 0）
    expect(text).toContain('4,321')
    // Topic 数列也来自 metrics
    expect(text).toContain('77')
  })

  it('详情抽屉展示运行时长（秒转天/小时）', async () => {
    const w = mountTab()
    await flushPromises()

    await w.findAll('.el-table__row')[0].trigger('click')
    await flushPromises()
    // el-drawer 走 teleport + 过渡，等一帧再读 body
    await new Promise((r) => setTimeout(r, 0))

    // 抽屉内容在 body 上
    const text = document.body.textContent
    expect(text).toContain('运行时长')
    expect(text).toContain('2小时')
  })
})
