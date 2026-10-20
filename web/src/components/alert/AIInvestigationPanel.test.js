import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import http from '../../api/http'
import AIInvestigationPanel from './AIInvestigationPanel.vue'

vi.mock('../../api/http', () => ({ default: { get: vi.fn(), retryAIInvestigation: vi.fn() } }))

let wrappers = []

function mountPanel(alertId = 'e1') {
  const w = mount(AIInvestigationPanel, {
    props: { alertId },
    global: { plugins: [ElementPlus] },
    attachTo: document.body,
  })
  wrappers.push(w)
  return w
}

beforeEach(() => {
  vi.clearAllMocks()
  wrappers = []
})

afterEach(() => {
  vi.useRealTimers()
  for (const w of wrappers) w.unmount()
})

describe('AIInvestigationPanel', () => {
  it('展示结论与步骤', async () => {
    http.get.mockResolvedValue({
      investigation: {
        state: 'done', answer: 'CPU 持续偏高', protocol: 'native',
        steps: [{ tool: 'query_metric', result: 'cpu=91' }],
      },
    })
    const w = mountPanel()
    await flushPromises()
    expect(w.text()).toContain('CPU 持续偏高')
    expect(w.text()).toContain('query_metric')
  })

  it('没有记录时说明自动调查未开启', async () => {
    http.get.mockResolvedValue({ investigation: null })
    const w = mountPanel()
    await flushPromises()
    expect(w.text()).toContain('暂无 AI 调查')
  })

  // 预算/超时收尾时 state 仍是 done，但结论并没有给出。
  // 面板必须把这个差别显示出来，否则值班的人会把「未完成」当成诊断结果。
  it('预算耗尽收尾时标为未完成，并显示 token、耗时与停止原因', async () => {
    http.get.mockResolvedValue({
      investigation: {
        state: 'done', stopped: 'tools', protocol: 'native',
        answer: '调查未完成：工具调用次数用尽',
        tokens: 8123, rounds: 4, startedAt: 1000000, endedAt: 1038200,
        steps: [{ tool: 'query_metric', result: 'cpu=91' }],
      },
    })
    const w = mountPanel()
    await flushPromises()

    const head = w.find('.inv-state').text()
    expect(head).toContain('未完成')
    expect(head).toContain('8,123')
    expect(head).toContain('4 轮')
    expect(head).toContain('38.2 秒')
    expect(w.text()).toContain('停止原因：工具调用次数用尽')
  })

  it('正常结束不显示停止原因', async () => {
    http.get.mockResolvedValue({
      investigation: {
        state: 'done', stopped: 'done', answer: '一切正常',
        tokens: 1200, rounds: 2, startedAt: 1000, endedAt: 3000, steps: [],
      },
    })
    const w = mountPanel()
    await flushPromises()
    expect(w.find('.inv-state').text()).toContain('已完成')
    expect(w.text()).not.toContain('停止原因')
  })

  // 未完成 / 失败时给一个入口：不必等下一次 critical 告警，也不必去翻告警列表。
  it('未完成时可以一键重跑，并轮询到新结论', async () => {
    vi.useFakeTimers()
    http.get
      .mockResolvedValueOnce({
        investigation: { state: 'done', stopped: 'tools', answer: '调查未完成', startedAt: 1000, endedAt: 2000, steps: [] },
      })
      .mockResolvedValueOnce({ investigation: { state: 'running', startedAt: 3000, steps: [] } })
      .mockResolvedValueOnce({
        investigation: { state: 'done', stopped: 'done', answer: '重跑后的结论', startedAt: 3000, endedAt: 9000, steps: [] },
      })
    http.retryAIInvestigation.mockResolvedValue({ message: '已开始重新调查' })

    const w = mountPanel()
    await flushPromises()

    const btn = w.findAll('button').find((b) => b.text().includes('重新调查'))
    expect(btn).toBeTruthy()
    await btn.trigger('click')
    await flushPromises()

    expect(http.retryAIInvestigation).toHaveBeenCalledWith('e1')
    expect(w.text()).toContain('调查中')

    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()
    expect(w.text()).toContain('重跑后的结论')
  })

  it('调查中时持续轮询，进入终态后停止', async () => {
    vi.useFakeTimers()
    http.get
      .mockResolvedValueOnce({ investigation: { state: 'running', startedAt: 1000, steps: [] } })
      .mockResolvedValueOnce({
        investigation: { state: 'done', stopped: 'done', answer: '已完成结论', startedAt: 1000, endedAt: 5000, steps: [] },
      })
    const w = mountPanel()
    await flushPromises()
    expect(w.text()).toContain('调查中')

    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()
    expect(w.text()).toContain('已完成结论')
    expect(http.get).toHaveBeenCalledTimes(2)

    await vi.advanceTimersByTimeAsync(6000)
    expect(http.get).toHaveBeenCalledTimes(2)
  })
})
