// AI 诊断组件的回归测试（设计件 2026-10-08-ai-assistant-design.md）。
//
// 守两件事：
//   ① 前端**只传对象标识**，上下文由服务端组装（前端不拼内容）；
//   ② 「未发送给模型」必须显示出来——否则用户会奇怪「为什么它没提到我的 IP / 连接串」。
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import http from '../../api/http'
import AiDiagnosePanel from './AiDiagnosePanel.vue'

vi.mock('../../api/http', () => ({
  default: { diagnoseAI: vi.fn() },
}))

const OK = {
  advice: '先看磁盘 inode 使用率。',
  model: 'fake-model',
  totalTokens: 42,
  elapsedMs: 1234,
  truncated: false,
  sensitive: false,
}

let wrappers = []

function mountPanel(props = { kind: 'node', id: 'web-01' }) {
  const w = mount(AiDiagnosePanel, { props, global: { plugins: [ElementPlus] }, attachTo: document.body })
  wrappers.push(w)
  return w
}

function clickButton(w) {
  return w.findAll('button').find((b) => b.text().includes('AI 诊断')).trigger('click')
}

beforeEach(() => {
  vi.clearAllMocks()
  wrappers = []
  document.body.innerHTML = ''
  http.diagnoseAI.mockResolvedValue(OK)
})

afterEach(() => {
  for (const w of wrappers) w.unmount()
})

describe('AI 诊断组件', () => {
  it('点按钮时只传对象标识（上下文由服务端组装）', async () => {
    const w = mountPanel({ kind: 'alert', id: 'evt-1' })
    await clickButton(w)
    await flushPromises()

    expect(http.diagnoseAI).toHaveBeenCalledWith({ kind: 'alert', id: 'evt-1' })
  })

  it('成功后显示建议、模型、耗时与 token', async () => {
    const w = mountPanel()
    await clickButton(w)
    await flushPromises()

    const text = document.body.textContent
    expect(text).toContain('先看磁盘 inode 使用率。')
    expect(text).toContain('fake-model')
    expect(text).toContain('1.2s')
    expect(text).toContain('42')
  })

  it('如实显示「未发送给模型」的内容', async () => {
    http.diagnoseAI.mockResolvedValue({ ...OK, notSent: ['属性值（未开启「允许发送敏感内容」）'] })
    const w = mountPanel({ kind: 'asset', id: '9' })
    await clickButton(w)
    await flushPromises()

    expect(document.body.textContent).toContain('未发送给模型')
    expect(document.body.textContent).toContain('允许发送敏感内容')
  })

  it('含敏感内容与截断时给出标记', async () => {
    http.diagnoseAI.mockResolvedValue({ ...OK, sensitive: true, truncated: true })
    const w = mountPanel()
    await clickButton(w)
    await flushPromises()

    const text = document.body.textContent
    expect(text).toContain('含敏感内容')
    expect(text).toContain('上下文已截断')
  })

  it('失败时显示服务端返回的原因', async () => {
    http.diagnoseAI.mockRejectedValue(new Error('AI 未配置或未启用，请先在「系统设置 → AI 助手」里配置模型'))
    const w = mountPanel()
    await clickButton(w)
    await flushPromises()

    expect(document.body.textContent).toContain('AI 未配置或未启用')
  })

  it('缺对象标识时不发请求', async () => {
    const w = mountPanel({ kind: 'node', id: '' })
    await clickButton(w)
    await flushPromises()

    expect(http.diagnoseAI).not.toHaveBeenCalled()
  })
})
