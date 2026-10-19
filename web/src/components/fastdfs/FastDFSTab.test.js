// FastDFS Tab：声明化之后的行为回归。
//
// 后端从「扁平字段（totalSpaceMB 等）」改为与其它中间件一致的 metrics 数组，
// 前端随之改为共用基座 LightCollectorTab + 一份 spec。这里锁住三件事：
// 指标按 key 取到、角色/分组仍展示、空间仍按人读的单位格式化。
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import http from '../../api/http'
import FastDFSTab from './FastDFSTab.vue'

vi.mock('../../api/http', () => ({
  default: { get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() },
}))

if (!globalThis.ResizeObserver) {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
}

const PAYLOAD = {
  type: 'fastdfs',
  instances: [{
    node: 'web-01',
    instance: '10.0.0.10:22122',
    name: 'fs-1',
    role: 'tracker',
    group: 'g1',
    up: true,
    metrics: [
      { key: 'fastdfs_storage_online_count', label: '在线Storage', value: 12345 },
      { key: 'fastdfs_total_space', label: '总空间', value: 2048, unit: 'MB' },
    ],
  }],
}

let wrappers = []
function mountTab() {
  const w = mount(FastDFSTab, { global: { plugins: [ElementPlus] } })
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

describe('FastDFSTab 声明式实例表', () => {
  it('按 metrics 数组渲染指标列，空间按 MB/GB 格式化', async () => {
    const w = mountTab()
    await flushPromises()

    const text = w.text()
    expect(text).toContain('fs-1')
    expect(text).toContain('tracker')
    expect(text).toContain('g1')
    // 2048 MB 应显示成 2.0 GB，而不是裸数字
    expect(text).toContain('2.0 GB')
    expect(text).toContain('在线Storage')
  })

  it('KPI 行按 spec 聚合在线 Storage 数，未指定格式时保留千分位', async () => {
    const w = mountTab()
    await flushPromises()

    const text = w.text()
    expect(text).toContain('在线 Storage')
    expect(text).toContain('12,345')
  })
})
