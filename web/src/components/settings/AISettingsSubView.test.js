// AI 助手设置页的回归测试（设计件 2026-10-08-ai-assistant-design.md）。
//
// 为什么要有这个文件：本页是平台第一条"数据会离开部署环境"的配置入口，最容易失效的是三件事——
//   ① 默认值被写成"启用"（离线部署会莫名多出一条外呼路径）；
//   ② 已保存的密钥被回填到表单（GET 已脱敏，前端再"顺手"填回去等于把密钥摆到屏幕上）；
//   ③ 启用却没填端点/模型就提交（后端会 400，但用户要等到点了保存才知道）。
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import http from '../../api/http'
import AISettingsSubView from './AISettingsSubView.vue'

vi.mock('../../api/http', () => ({
  default: {
    getAIConfig: vi.fn(),
    saveAIConfig: vi.fn(),
    testAIConfig: vi.fn(),
  },
}))

const CFG = {
  enabled: false,
  baseUrl: '',
  model: '',
  apiKey: '',
  timeoutSeconds: 60,
  maxTokens: 1024,
  temperature: 0.2,
  allowSensitive: false,
}

let wrappers = []

function mountView() {
  const w = mount(AISettingsSubView, { global: { plugins: [ElementPlus] }, attachTo: document.body })
  wrappers.push(w)
  return w
}

function btn(w, text) {
  return w.findAll('button').find((b) => b.text().includes(text))
}

beforeEach(() => {
  vi.clearAllMocks()
  wrappers = []
  http.getAIConfig.mockResolvedValue({ config: CFG, ready: false, hasApiKey: false })
  http.saveAIConfig.mockResolvedValue({ message: '已保存并生效' })
  http.testAIConfig.mockResolvedValue({ model: 'fake-model', elapsedMs: 12, totalTokens: 3, reply: 'pong' })
})

afterEach(() => {
  for (const w of wrappers) w.unmount()
})

describe('AI 助手设置页', () => {
  it('默认关闭，并写明数据去向与"不启用时按本地分析工作"', async () => {
    const w = mountView()
    await flushPromises()

    const sw = w.findComponent({ name: 'ElSwitch' })
    expect(sw.props('modelValue')).toBe(false)

    const text = w.text()
    expect(text).toContain('开启前请确认数据去向')
    expect(text).toContain('不启用时平台完全按本地分析工作')
    expect(text).toContain('日志正文、属性值等更敏感的内容默认')
  })

  // 服务端已经脱敏（apiKey 恒为空），前端绝不能把它填回去——否则密钥会出现在屏幕上。
  it('不回填密钥，并按是否已配置切换 placeholder', async () => {
    http.getAIConfig.mockResolvedValue({
      config: { ...CFG, enabled: true, baseUrl: 'https://api.example.com/v1', model: 'm', apiKey: 'sk-leaked' },
      ready: true,
      hasApiKey: true,
    })
    const w = mountView()
    await flushPromises()

    const inputs = w.findAllComponents({ name: 'ElInput' })
    const key = inputs.find((i) => i.props('type') === 'password')
    expect(key).toBeTruthy()
    expect(key.props('modelValue')).toBe('')
    expect(key.props('placeholder')).toContain('已配置')
    expect(w.html()).not.toContain('sk-leaked')
  })

  it('启用但缺端点时不提交，并在本地就拦下', async () => {
    const w = mountView()
    await flushPromises()

    const switches = w.findAllComponents({ name: 'ElSwitch' })
    await switches[0].vm.$emit('update:modelValue', true)
    await flushPromises()

    await btn(w, '保存').trigger('click')
    await flushPromises()
    expect(http.saveAIConfig).not.toHaveBeenCalled()
  })

  it('填齐后保存：端点去尾斜杠，其余字段原样提交', async () => {
    const w = mountView()
    await flushPromises()

    const switches = w.findAllComponents({ name: 'ElSwitch' })
    await switches[0].vm.$emit('update:modelValue', true)
    const inputs = w.findAllComponents({ name: 'ElInput' })
    const url = inputs.find((i) => i.props('placeholder')?.includes('deepseek'))
    const model = inputs.find((i) => i.props('placeholder')?.includes('qwen-plus'))
    await url.vm.$emit('update:modelValue', 'https://api.example.com/v1/')
    await model.vm.$emit('update:modelValue', 'my-model')
    await flushPromises()

    await btn(w, '保存').trigger('click')
    await flushPromises()

    expect(http.saveAIConfig).toHaveBeenCalledTimes(1)
    const sent = http.saveAIConfig.mock.calls[0][0]
    expect(sent.baseUrl).toBe('https://api.example.com/v1')
    expect(sent.model).toBe('my-model')
    expect(sent.enabled).toBe(true)
  })

  it('测试连接成功后展示模型、耗时与回复', async () => {
    const w = mountView()
    await flushPromises()
    await btn(w, '测试连接').trigger('click')
    await flushPromises()

    expect(http.testAIConfig).toHaveBeenCalled()
    const text = w.text()
    expect(text).toContain('连通正常')
    expect(text).toContain('fake-model')
    expect(text).toContain('pong')
  })

  it('测试连接失败时明确显示错误', async () => {
    http.testAIConfig.mockRejectedValue(new Error('AI 端点返回 401（https://api.example.com/v1/chat/completions）'))
    const w = mountView()
    await flushPromises()
    await btn(w, '测试连接').trigger('click')
    await flushPromises()

    expect(w.text()).toContain('连接失败')
    expect(w.text()).toContain('401')
  })
})
