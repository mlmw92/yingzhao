// PostgreSQL Tab：声明化之后的行为回归（拓扑角色着色、数据库列、指标数组）。
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import http from '../../api/http'
import PostgresTab from './PostgresTab.vue'

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
  type: 'postgres',
  instances: [{
    node: 'web-01', instance: '10.0.0.10:5432', name: 'pg-1',
    role: 'master', topology: 'replication', version: '16.2', database: 'app', up: true,
    metrics: [
      { key: 'postgres_numbackends', label: '连接数', value: 7 },
      { key: 'postgres_cache_hit_ratio', label: '缓存命中率', value: 99 },
    ],
  }],
}

let wrappers = []
function mountTab() {
  const w = mount(PostgresTab, { global: { plugins: [ElementPlus] } })
  wrappers.push(w)
  return w
}

beforeEach(() => {
  vi.clearAllMocks()
  wrappers = []
  http.get.mockResolvedValue(PAYLOAD)
})
afterEach(() => { for (const w of wrappers) w.unmount() })

describe('PostgresTab 声明式实例页', () => {
  it('元信息展示拓扑与数据库，主库节点按角色着色', async () => {
    const w = mountTab()
    await flushPromises()
    const text = w.text()
    expect(text).toContain('实例拓扑')
    expect(text).toContain('replication')
    expect(text).toContain('app')
    expect(text).toContain('16.2')
    expect(w.find('.rel-master').exists()).toBe(true)
  })

  it('KPI 与指标列来自 metrics 数组', async () => {
    const w = mountTab()
    await flushPromises()
    const text = w.text()
    expect(text).toContain('总连接数')
    expect(text).toContain('缓存命中率')
    expect(text).toContain('7')
  })
})
