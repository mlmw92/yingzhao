// 通知配置页（PS-16）的回归测试。
//
// 守的是两类"看着没事、实际收不到告警"的形态：
//   ① 渠道开关是开的、但地址为空 → 后端静默接受，通知永远发不出去；
//   ② 加载失败只弹一条 toast → 页面留着空表单，用户既不知原因也没法重试。
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus, { ElMessage } from 'element-plus'
import http from '../api/http'
import NotifyView from './NotifyView.vue'

vi.mock('../api/http', () => ({
  default: { get: vi.fn(), put: vi.fn(async () => ({})), post: vi.fn(async () => ({ ok: true })) },
  getToken: vi.fn(() => ''),
}))

vi.mock('element-plus', async (importOriginal) => {
  const actual = await importOriginal()
  return {
    ...actual,
    ElMessage: { success: vi.fn(), error: vi.fn(), warning: vi.fn() },
    ElMessageBox: { confirm: vi.fn(async () => true), alert: vi.fn(async () => {}) },
  }
})

const BASE = () => ({
  email: { enabled: false, smtpHost: '', smtpPort: 587, username: '', password: '', from: '', to: [], useTLS: true, useStartTLS: false },
  webhook: { enabled: false, urls: [] },
  dingtalk: { enabled: false, urls: [], secret: '', atMobiles: [] },
  feishu: { enabled: false, urls: [], secret: '' },
  wecom: { enabled: false, urls: [], mentionedList: [] },
})

let wrappers = []

function mountView() {
  const w = mount(NotifyView, {
    global: { plugins: [ElementPlus], stubs: { StringListInput: true } },
    attachTo: document.body,
  })
  wrappers.push(w)
  return w
}

const btn = (w, text) => w.findAll('button').find((b) => b.text().includes(text))

// el-form-item 的错误文案是 100ms 防抖后才上屏的（refDebounced），断言 DOM 错误必须等过这个窗口
const waitDebounce = () => new Promise((r) => setTimeout(r, 160))

beforeEach(() => {
  vi.clearAllMocks()
  wrappers = []
  Element.prototype.scrollIntoView = vi.fn()
  http.get.mockResolvedValue(BASE())
})

afterEach(() => {
  for (const w of wrappers) w.unmount()
})

describe('NotifyView 渠道健康度摘要', () => {
  it('没有启用任何渠道时说明告警不会外发', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('未启用任何渠道')
    expect(w.text()).toContain('不会外发')
  })

  it('启用且配置完整时给出「已启用 / 配置完整」', async () => {
    const cfg = BASE()
    cfg.webhook = { enabled: true, urls: ['https://example.com/hook'] }
    http.get.mockResolvedValue(cfg)

    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('1 个渠道已启用')
    expect(w.text()).toContain('配置完整')
  })
})

describe('NotifyView 配置不完整', () => {
  it('启用的渠道缺地址时卡片标为「配置不完整」，并在字段上给出错误', async () => {
    const cfg = BASE()
    cfg.email = { ...cfg.email, enabled: true, smtpHost: '' }
    http.get.mockResolvedValue(cfg)

    const w = mountView()
    await flushPromises()

    expect(w.text()).toContain('配置不完整')
    // 字段级错误落在 SMTP 主机那一项上
    await waitDebounce()
    const errs = w.findAll('.el-form-item__error').map((e) => e.text())
    expect(errs.join('|')).toContain('SMTP 主机')
  })

  it('保存时拦下配置不完整的渠道，不发 PUT', async () => {
    const cfg = BASE()
    cfg.webhook = { enabled: true, urls: [] } // 开了但没地址
    http.get.mockResolvedValue(cfg)

    const w = mountView()
    await flushPromises()

    await btn(w, '保存并生效').trigger('click')
    await flushPromises()

    expect(http.put).not.toHaveBeenCalled()
    expect(ElMessage.warning).toHaveBeenCalled()
  })

  it('配置完整时可以保存并发出 PUT', async () => {
    const cfg = BASE()
    cfg.webhook = { enabled: true, urls: ['https://example.com/hook'] }
    http.get.mockResolvedValue(cfg)

    const w = mountView()
    await flushPromises()

    await btn(w, '保存并生效').trigger('click')
    await flushPromises()

    expect(http.put).toHaveBeenCalledTimes(1)
    const [url, body] = http.put.mock.calls[0]
    expect(url).toBe('/api/v1/notify')
    expect(body.webhook.urls).toEqual(['https://example.com/hook'])
  })
})

describe('NotifyView 加载失败', () => {
  it('加载失败展示错误态，点重试后恢复', async () => {
    http.get.mockRejectedValueOnce(new Error('boom'))
    const w = mountView()
    await flushPromises()

    expect(w.text()).toContain('加载通知配置失败')
    // 空表单不该在错误态下继续展示
    expect(w.text()).not.toContain('企业微信机器人')

    const retry = btn(w, '重试')
    expect(retry).toBeTruthy()
    await retry.trigger('click')
    await flushPromises()

    expect(http.get).toHaveBeenCalledTimes(2)
    expect(w.text()).toContain('企业微信机器人')
  })
})

describe('NotifyView 可访问性', () => {
  it('每个渠道的启用开关都有可读名称', async () => {
    const w = mountView()
    await flushPromises()
    for (const label of ['邮件 (SMTP)', 'Webhook', '钉钉机器人', '飞书机器人', '企业微信机器人']) {
      expect(w.find(`[aria-label="启用渠道：${label}"]`).exists()).toBe(true)
    }
  })
})
