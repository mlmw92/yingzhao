// 告警规则弹窗（PS-15）的交互回归测试。
//
// 为什么值得单独守：这 618 行表单里的三类问题都是"界面上看着没事、实际会静默出错"——
//   ① 保存没有中态 → 一次误触发出两条同名规则；
//   ② 校验只有一条顶部 toast → 用户不知道到底缺哪一项；
//   ③ 切换规则类型不清异类字段 → metric/threshold 随 body 发出去，产生永不触发的规则。
// 这些都无法靠"肉眼看一遍"发现，只能靠"点一下再检查提交体"来守。
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import http from '../api/http'
import RuleModal from './RuleModal.vue'

const { confirmMock, pushMock } = vi.hoisted(() => ({
  confirmMock: vi.fn(async () => true),
  pushMock: vi.fn(),
}))

vi.mock('../api/http', () => ({
  default: {
    get: vi.fn(async () => ({ nodes: [], alertGroups: [] })),
    post: vi.fn(async () => ({})),
    put: vi.fn(async () => ({})),
  },
  getToken: vi.fn(() => ''),
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: pushMock, replace: vi.fn() }),
}))

// 只替换弹窗消息 API，其余（ElDialog / ElForm / ElSelect…）走真实实现——
// 本文件的重点正是"真实的表单校验与关闭流程"，把它们 stub 掉就什么也测不到了。
vi.mock('element-plus', async (importOriginal) => {
  const actual = await importOriginal()
  return {
    ...actual,
    ElMessage: { success: vi.fn(), error: vi.fn(), warning: vi.fn() },
    ElMessageBox: { confirm: confirmMock, alert: vi.fn(async () => {}) },
  }
})

let wrappers = []

function mountRule(props = {}) {
  const w = mount(RuleModal, {
    props: { rule: undefined, groups: [], channels: [], ...props },
    global: { plugins: [ElementPlus] },
    attachTo: document.body,
  })
  wrappers.push(w)
  return w
}

const saveBtn = (w) => w.findAll('button').find((b) => b.text().includes('保存'))
const selects = (w) => w.findAllComponents({ name: 'ElSelect' })

const VALID_RULE = {
  id: 'r1', name: 'CPU 高', type: '', metric: 'cpu_usage', operator: '>',
  threshold: 80, for: '5m', severity: 'critical', scope: 'all', nodes: [],
  enabled: true, notify: [],
}

function deferred() {
  let resolve
  const promise = new Promise((r) => { resolve = r })
  return { promise, resolve }
}

// el-form-item 的错误文案是 100ms 防抖后才上屏的（refDebounced），
// 断言 DOM 里的错误必须等过这个窗口，否则会误判成"没有错误提示"。
const waitDebounce = () => new Promise((r) => setTimeout(r, 160))

beforeEach(() => {
  vi.clearAllMocks()
  wrappers = []
  confirmMock.mockResolvedValue(true)
  // jsdom 未实现 scrollIntoView，而 el-form 的 scroll-to-error 会调它
  Element.prototype.scrollIntoView = vi.fn()
})

afterEach(() => {
  for (const w of wrappers) w.unmount()
})

describe('RuleModal 字段级校验', () => {
  it('必填项为空时不发请求，并把错误落到字段上', async () => {
    const w = mountRule() // 新建：名称/级别均为空
    await flushPromises()

    await saveBtn(w).trigger('click')
    await flushPromises()
    await waitDebounce()

    expect(http.post).not.toHaveBeenCalled()
    expect(http.put).not.toHaveBeenCalled()
    // 错误必须落在字段上（而不是只弹一条顶部 toast）
    const errors = w.findAll('.el-form-item__error').map((e) => e.text())
    expect(errors.length).toBeGreaterThan(0)
    expect(errors.join('|')).toContain('规则名称')
  })
})

describe('RuleModal 保存中态', () => {
  it('保存进行中重复点击只发出一次请求', async () => {
    const d = deferred()
    http.put.mockReturnValueOnce(d.promise)

    const w = mountRule({ rule: VALID_RULE })
    await flushPromises()

    await saveBtn(w).trigger('click')
    await flushPromises() // 让 validate 走完、put 真正发出（deferred 仍 pending）
    expect(http.put).toHaveBeenCalledTimes(1)
    // 按钮进入 loading（视觉上的"进行中"）
    expect(saveBtn(w).classes()).toContain('is-loading')

    // 第二次点击：无论被 loading 禁用拦住、还是被 saving 守卫拦住，都不该再发一次
    await saveBtn(w).trigger('click')
    await flushPromises()
    expect(http.put).toHaveBeenCalledTimes(1)

    d.resolve({})
    await flushPromises()
  })
})

describe('RuleModal 切换规则类型', () => {
  it('从阈值规则切到主机离线时清空指标/阈值等异类字段', async () => {
    const w = mountRule()
    await flushPromises()

    // 按"阈值规则"填满：名称 + 级别 + 指标 + 阈值
    await w.find('input[placeholder="例如：主机离线告警"]').setValue('阈值规则')
    selects(w)[1].vm.$emit('update:modelValue', 'critical') // 1=告警级别
    selects(w)[2].vm.$emit('update:modelValue', 'cpu_usage') // 2=监控指标
    w.findAllComponents({ name: 'ElInputNumber' })[0].vm.$emit('update:modelValue', 80)
    await flushPromises()

    // 切到「主机离线」——触发条件整块换掉，旧的 metric/threshold 必须一起清掉
    selects(w)[0].vm.$emit('update:modelValue', 'node_offline') // 0=规则类型
    await flushPromises()

    // 分区摘要随类型切换
    expect(w.text()).toContain('离线持续')

    await saveBtn(w).trigger('click')
    await flushPromises()

    expect(http.post).toHaveBeenCalledTimes(1)
    const body = http.post.mock.calls[0][1]
    expect(body.type).toBe('node_offline')
    expect(body.metric).toBe('')
    expect(body.threshold).toBe(0)
  })
})

describe('RuleModal 关闭确认', () => {
  const beforeClose = (w) => w.findComponent({ name: 'ElDialog' }).props('beforeClose')

  it('有未保存修改时关闭要先确认', async () => {
    const w = mountRule()
    await flushPromises()
    await w.find('input[placeholder="例如：主机离线告警"]').setValue('改过了')
    await flushPromises()

    const done = vi.fn()
    await beforeClose(w)(done)
    await flushPromises()

    expect(confirmMock).toHaveBeenCalled()
    expect(done).toHaveBeenCalled()
  })

  it('没有改动时关闭不弹确认', async () => {
    const w = mountRule({ rule: VALID_RULE })
    await flushPromises()

    const done = vi.fn()
    await beforeClose(w)(done)
    await flushPromises()

    expect(confirmMock).not.toHaveBeenCalled()
    expect(done).toHaveBeenCalled()
  })

  it('用户选择「继续编辑」时不关闭', async () => {
    confirmMock.mockRejectedValueOnce(new Error('cancel'))
    const w = mountRule()
    await flushPromises()
    await w.find('input[placeholder="例如：主机离线告警"]').setValue('改过了')
    await flushPromises()

    const done = vi.fn()
    await beforeClose(w)(done)
    await flushPromises()

    expect(done).not.toHaveBeenCalled()
  })
})

describe('RuleModal 未启用渠道跳转', () => {
  it('列出未启用渠道并提供「去通知配置启用」入口', async () => {
    const w = mountRule({
      channels: [
        { value: 'email', label: '邮件 (SMTP)', enabled: false },
        { value: 'webhook', label: 'Webhook', enabled: true },
      ],
    })
    await flushPromises()

    expect(w.text()).toContain('邮件 (SMTP) 尚未启用')
    const link = w.findAll('.el-link').find((l) => l.text().includes('去通知配置启用'))
    expect(link).toBeTruthy()

    await link.trigger('click')
    expect(pushMock).toHaveBeenCalledWith('/notify')
    expect(w.emitted('close')).toBeTruthy()
  })
})
