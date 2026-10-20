import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import http from '../../api/http'
import ModuleSettingsSubView from './ModuleSettingsSubView.vue'

vi.mock('../../api/http', () => ({
  default: { get: vi.fn(), put: vi.fn() },
}))

beforeEach(() => {
  vi.clearAllMocks()
  http.get.mockResolvedValue({ name: '英招', subtitle: '运维平台', disabled: ['assets'] })
  http.put.mockResolvedValue({ ok: true })
})

describe('ModuleSettingsSubView', () => {
  it('列出各域与页面，不可关闭项置灰并说明原因', async () => {
    const w = mount(ModuleSettingsSubView, { global: { plugins: [ElementPlus] } })
    await flushPromises()
    expect(w.text()).toContain('观测监控')
    expect(w.text()).toContain('资产与配置')
    expect(w.text()).toContain('告警中心')
    expect(w.text()).toContain('不可关闭')
  })

  it('保存只提交 disabled，不覆盖其它字段', async () => {
    const w = mount(ModuleSettingsSubView, { global: { plugins: [ElementPlus] } })
    await flushPromises()
    const btn = w.findAll('button').find((b) => b.text().includes('保存'))
    await btn.trigger('click')
    await flushPromises()
    expect(http.put).toHaveBeenCalledWith('/api/v1/ui/settings', { disabled: ['assets'] })
  })
})
