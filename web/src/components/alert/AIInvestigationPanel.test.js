import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import http from '../../api/http'
import AIInvestigationPanel from './AIInvestigationPanel.vue'

vi.mock('../../api/http', () => ({ default: { get: vi.fn() } }))

beforeEach(() => vi.clearAllMocks())

describe('AIInvestigationPanel', () => {
  it('展示结论与步骤', async () => {
    http.get.mockResolvedValue({
      investigation: {
        state: 'done', answer: 'CPU 持续偏高', protocol: 'native',
        steps: [{ tool: 'query_metric', result: 'cpu=91' }],
      },
    })
    const w = mount(AIInvestigationPanel, { props: { alertId: 'e1' }, global: { plugins: [ElementPlus] } })
    await flushPromises()
    expect(w.text()).toContain('CPU 持续偏高')
    expect(w.text()).toContain('query_metric')
  })

  it('没有记录时说明自动调查未开启', async () => {
    http.get.mockResolvedValue({ investigation: null })
    const w = mount(AIInvestigationPanel, { props: { alertId: 'e1' }, global: { plugins: [ElementPlus] } })
    await flushPromises()
    expect(w.text()).toContain('暂无 AI 调查')
  })
})
