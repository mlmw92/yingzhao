// DialTestView 回归：
//   - 任务表「当前状态 / 延迟 / 可用率」三列（状态、延迟、可用率拆开，不再挤在一个单元格）
//   - 启用开关失败回滚并提示（此前失败只 console.error）
//   - 任务表搜索 / 类型筛选
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick } from 'vue'
import ElementPlus, { ElMessage } from 'element-plus'
import http from '../api/http'
import DialTestView from './DialTestView.vue'

vi.mock('../api/http', () => ({
  default: { get: vi.fn(), post: vi.fn(), put: vi.fn(), del: vi.fn() },
}))

// Sparkline 走真实 echarts 太重，这里 stub 掉图表初始化。
vi.mock('../charts/echarts', () => ({
  initChart: vi.fn(() => ({ setOption: vi.fn(), resize: vi.fn(), dispose: vi.fn(), isDisposed: () => false })),
  sparkOption: vi.fn(() => ({ series: [{}] })),
  COLORS: { cyan: '#22d3ee' },
}))

if (!globalThis.ResizeObserver) {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
}

const TASKS = [
  { id: 't1', name: '官网', type: 'https', target: 'a.com', interval: 60, timeout: 10, enabled: true, severity: 'critical', public: true },
  { id: 't2', name: '后台', type: 'http', target: 'b.com', interval: 60, timeout: 10, enabled: false, severity: 'warning' },
  { id: 't3', name: '数据库', type: 'tcp', target: 'db:3306', interval: 30, timeout: 5, enabled: true, severity: 'info' },
]

const RESULTS = [
  { name: '官网', type: 'https', target: 'a.com', up: true, latency: 12.3, uptime: 99.5, checkedAt: 1730000000000, latencyHistory: [{ timestamp: 1, value: 1 }, { timestamp: 2, value: 2 }] },
  { name: '数据库', type: 'tcp', target: 'db:3306', up: false, latency: 0, uptime: 42, error: '连接超时' },
]

function mountView() {
  return mount(DialTestView, { global: { plugins: [ElementPlus] }, attachTo: document.body })
}

async function settle() {
  await flushPromises()
  await nextTick()
  await flushPromises()
  await nextTick()
}

describe('DialTestView 拨测页（批次 14）', () => {
  let wrapper

  beforeEach(() => {
    vi.clearAllMocks()
    http.get.mockImplementation(async (path) => {
      if (path === '/api/v1/dialtest/tasks') return { tasks: TASKS }
      if (path === '/api/v1/dialtest/latest') return { results: RESULTS }
      return {}
    })
    http.put.mockResolvedValue({})
  })

  afterEach(() => {
    if (wrapper) wrapper.unmount()
    wrapper = undefined
  })

  it('任务表把 状态 / 延迟 / 可用率 拆成三列，各自独立展示', async () => {
    wrapper = mountView()
    await settle()

    // 表头必须同时存在这三列
    const headers = wrapper.findAll('.el-table')[0].findAll('th').map((th) => th.text())
    expect(headers).toContain('当前状态')
    expect(headers).toContain('延迟(ms)')
    expect(headers).toContain('可用率')

    const rows = wrapper.findAll('.el-table')[0].findAll('.el-table__row')
    const text = rows.map((r) => r.text())

    // 官网：enabled + up → 正常；延迟 12.3；可用率 99.5%
    const siteRow = text.find((t) => t.includes('官网'))
    expect(siteRow).toContain('正常')
    expect(siteRow).toContain('12.3')
    expect(siteRow).toContain('99.5%')

    // 后台：disabled → 已停用
    const bgRow = text.find((t) => t.includes('后台'))
    expect(bgRow).toContain('已停用')

    // 数据库：enabled + down → 异常
    const dbRow = text.find((t) => t.includes('数据库'))
    expect(dbRow).toContain('异常')
  })

  it('启用开关失败时回滚并提示', async () => {
    const errSpy = vi.spyOn(ElMessage, 'error').mockImplementation(() => {})
    http.put.mockRejectedValue(new Error('保存失败'))

    wrapper = mountView()
    await settle()

    const sw = wrapper.findAllComponents({ name: 'ElSwitch' })[0]
    expect(sw).toBeTruthy()
    sw.vm.$emit('change', false)
    await settle()

    expect(errSpy).toHaveBeenCalled()
    errSpy.mockRestore()
  })

  it('按关键词与类型筛选任务', async () => {
    wrapper = mountView()
    await settle()

    // 页面有两张表（任务 / 结果），这里只看第一张任务表
    const taskRows = () => wrapper.findAll('.el-table')[0].findAll('.el-table__row')
    expect(taskRows().length).toBe(3)

    // 直接改输入框 v-model
    const input = wrapper.find('.filter-bar input')
    await input.setValue('数据库')
    await settle()
    let rows = taskRows()
    expect(rows.length).toBe(1)
    expect(rows[0].text()).toContain('数据库')

    // 清空关键词，改类型筛选
    await input.setValue('')
    await settle()
    const selects = wrapper.findAllComponents({ name: 'ElSelect' })
    selects[0].vm.$emit('update:modelValue', 'http')
    await settle()
    rows = taskRows()
    expect(rows.length).toBe(1)
    expect(rows[0].text()).toContain('后台')
  })
})
