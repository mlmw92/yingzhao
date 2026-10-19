// Nginx Tab：声明化之后的行为回归。
//
// Nginx 的差异点：实例地址优先展示节点 IP、数值按 K/M 紧凑展示、
// 版本只存在于注册表。
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import http from '../../api/http'
import NginxTab from './NginxTab.vue'

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
  type: 'nginx',
  instances: [{
    node: 'web-01',
    nodeIp: '10.0.0.1',
    instance: '127.0.0.1:80',
    name: 'edge-nginx',
    group: 'edge',
    version: '1.24.0',
    up: true,
    metrics: [
      { key: 'nginx_active_connections', label: '活动连接', value: 3 },
      { key: 'nginx_requests', label: '总请求', value: 1500 },
      { key: 'nginx_waiting', label: '等待', value: 2 },
    ],
  }],
}

let wrappers = []
function mountTab() {
  const w = mount(NginxTab, {
    global: { plugins: [ElementPlus] },
  })
  wrappers.push(w)
  return w
}

beforeEach(() => {
  vi.clearAllMocks()
  wrappers = []
  http.get.mockResolvedValue(PAYLOAD)
})
afterEach(() => {
  for (const w of wrappers) w.unmount()
})

describe('NginxTab 声明式实例页', () => {
  it('实例地址优先展示节点 IP，拓扑按 group 聚合', async () => {
    const w = mountTab()
    await flushPromises()

    const text = w.text()
    expect(text).toContain('实例拓扑')
    expect(text).toContain('edge')
    expect(text).toContain('10.0.0.1')
  })

  it('数值按 K/M 紧凑展示，KPI 聚合总请求', async () => {
    const w = mountTab()
    await flushPromises()

    const text = w.text()
    expect(text).toContain('总请求')
    expect(text).toContain('1.5K')
    expect(text).toContain('活动连接')
  })
})
