// MongoDB Tab：声明化之后的行为回归（副本角色、拓扑、MB 换算）。
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import http from '../../api/http'
import MongoTab from './MongoTab.vue'

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
  type: 'mongodb',
  instances: [{
    node: 'web-01', instance: '10.0.0.10:27017', name: 'mongo-1',
    role: 'PRIMARY', topology: 'replicaset', version: '7.0', up: true,
    metrics: [
      { key: 'mongodb_connections_current', label: '连接数', value: 12 },
      { key: 'mongodb_mem_resident_bytes', label: '常驻内存', value: 256, unit: 'MB' },
      { key: 'mongodb_db_objects', label: '文档数', value: 9 },
    ],
  }],
}

let wrappers = []
function mountTab() {
  const w = mount(MongoTab, { global: { plugins: [ElementPlus] } })
  wrappers.push(w)
  return w
}

beforeEach(() => {
  vi.clearAllMocks()
  wrappers = []
  http.get.mockResolvedValue(PAYLOAD)
})
afterEach(() => { for (const w of wrappers) w.unmount() })

describe('MongoTab 声明式实例页', () => {
  it('元信息展示角色/拓扑/版本', async () => {
    const w = mountTab()
    await flushPromises()
    const text = w.text()
    // 角色经 MwRoleTag 归一化展示（PRIMARY → Primary）
    expect(text).toContain('Primary')
    expect(w.find('.mw-role-master').exists()).toBe(true)
    expect(text).toContain('replicaset')
    expect(text).toContain('7.0')
    expect(text).toContain('mongo-1')
  })

  it('指标值按 metrics 数组渲染，KPI 聚合文档数', async () => {
    const w = mountTab()
    await flushPromises()
    const text = w.text()
    expect(text).toContain('常驻内存')
    expect(text).toContain('256')
    expect(text).toContain('文档总数')
    expect(text).toContain('9')
  })
})
