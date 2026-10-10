// 巡检报告页的**周期化生成**回归测试（全景表 11-4）。
//
// 为什么要有这个文件：周期化生成是"以后会自动产出文件"的开关，三种失败都不显眼——
// 开关看着是开的但服务端没接受（保存失败后界面没回滚）、上次生成失败了却不说、
// 该能力未启用却把开关显示成"关着"（用户以为打开就好）。
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus, { ElMessageBox } from 'element-plus'
import http from '../api/http'
import ReportView from './ReportView.vue'

vi.mock('../api/http', () => ({
  default: { get: vi.fn(), post: vi.fn(), put: vi.fn(), del: vi.fn() },
  getToken: vi.fn(() => ''),
}))

const HISTORY = { reports: [{ id: 'rpt-1', type: 'weekly', period: '2026-09-29 ~ 2026-10-06', generatedAt: 1700000000000 }] }
const SCHEDULE = {
  config: { enabled: true, type: 'weekly', intervalHours: 24 },
  last: { at: 1700000000000, reportId: 'rpt-1' },
  nextAt: 1700000000000 + 24 * 3600 * 1000,
}

let wrappers = []

function mountView() {
  const w = mount(ReportView, { global: { plugins: [ElementPlus] }, attachTo: document.body })
  wrappers.push(w)
  return w
}

beforeEach(() => {
  vi.clearAllMocks()
  wrappers = []
  http.get.mockImplementation(async (path) => {
    if (String(path).includes('/api/v1/report/schedule')) return SCHEDULE
    return HISTORY
  })
  http.put.mockResolvedValue(SCHEDULE)
  http.post.mockResolvedValue({ at: 1700000000000, reportId: 'rpt-2', manual: true })
})

afterEach(() => {
  for (const w of wrappers) w.unmount()
  document.body.innerHTML = ''
})

describe('ReportView 周期化生成', () => {
  it('展示上次运行与下次时间，并说明手动生成不受影响', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('周期化生成')
    expect(w.text()).toContain('上次运行')
    expect(w.text()).toContain('rpt-1')
    expect(w.text()).toContain('下次约')
  })

  it('从未运行过时如实说明，而不是留空', async () => {
    http.get.mockImplementation(async (path) => {
      if (String(path).includes('/api/v1/report/schedule')) {
        return { config: { enabled: false, type: 'weekly', intervalHours: 24 } }
      }
      return { reports: [] }
    })
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('尚未运行过')
    expect(w.text()).toContain('不影响上方的手动生成')
  })

  it('上次自动生成失败要显式提示（否则"报告怎么不出了"只能靠翻文件时间猜）', async () => {
    http.get.mockImplementation(async (path) => {
      if (String(path).includes('/api/v1/report/schedule')) {
        return {
          config: { enabled: true, type: 'weekly', intervalHours: 24, lastError: '磁盘只读' },
          last: { at: 1700000000000, error: '磁盘只读' },
        }
      }
      return HISTORY
    })
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('磁盘只读')
    expect(w.text()).toContain('上次自动生成失败')
  })

  it('该能力未启用（503）时如实说明，而不是把开关显示成"关着"', async () => {
    http.get.mockImplementation(async (path) => {
      if (String(path).includes('/api/v1/report/schedule')) throw new Error('报告调度未启用')
      return HISTORY
    })
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('报告调度未启用')
  })

  it('保存失败后回读服务端真实状态（不把界面留在"我改成了这样"）', async () => {
    let calls = 0
    http.get.mockImplementation(async (path) => {
      if (String(path).includes('/api/v1/report/schedule')) {
        calls++
        // 第一次给"关着"的状态，保存失败后回读仍是"关着"
        return { config: { enabled: false, type: 'weekly', intervalHours: 24 } }
      }
      return HISTORY
    })
    http.put.mockRejectedValue(new Error('保存失败'))
    const w = mountView()
    await flushPromises()

    const sw = w.find('.el-switch')
    await sw.trigger('click')
    await flushPromises()
    expect(http.put).toHaveBeenCalled()
    // 回读：至少再次拉取了状态
    expect(calls).toBeGreaterThan(1)
  })

  it('立即生成会调用调度接口并刷新历史', async () => {
    const w = mountView()
    await flushPromises()
    const btn = w.findAll('button').find((b) => b.text().includes('立即生成一次'))
    expect(btn).toBeTruthy()
    await btn.trigger('click')
    await flushPromises()
    expect(http.post).toHaveBeenCalledWith('/api/v1/report/schedule/run', {})
    const paths = http.get.mock.calls.map((c) => String(c[0]))
    expect(paths.some((p) => p.includes('/api/v1/report/history'))).toBe(true)
  })
})

// 报告章节与生成范围（设计件 2026-10-08-reporting-views-design.md）。
//
// 守两件事：
//  1. 章节选择要真的送到服务端（界面上勾了却没传，等于白勾）；
//  2. 历史列表要能看出「这一份覆盖的是谁、跳过了什么」——报告是会被转发出去的文件。
describe('报告章节与生成范围', () => {
  it('默认全选章节，生成时把选择带上', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('报告章节')
    expect(w.text()).toContain('资产与配置')

    const btn = w.findAll('button').find((b) => b.text().includes('生成报告'))
    expect(btn).toBeTruthy()
    await btn.trigger('click')
    await flushPromises()

    const call = http.post.mock.calls.find((c) => String(c[0]).includes('/api/v1/report/generate'))
    expect(call).toBeTruthy()
    expect(call[1].sections).toContain('assets')
    expect(call[1].sections).toContain('logs')
  })

  it('历史列表显示生成范围与「已跳过」标记', async () => {
    http.get.mockImplementation(async (path) => {
      if (String(path).includes('/api/v1/report/schedule')) return SCHEDULE
      return {
        reports: [{
          id: 'rpt-1', type: 'daily', period: '2026-10-08', generatedAt: 1700000000000,
          scope: '指定 2 个节点（1 台在范围内）', skipped: ['日志'],
        }],
      }
    })
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('指定 2 个节点')
    expect(w.text()).toContain('已跳过 1 章')
  })

  it('保存周期化生成时也带上章节选择', async () => {
    const w = mountView()
    await flushPromises()
    await w.find('.el-switch').trigger('click')
    await flushPromises()
    const call = http.put.mock.calls.find((c) => String(c[0]).includes('/api/v1/report/schedule'))
    expect(call).toBeTruthy()
    expect(Array.isArray(call[1].sections)).toBe(true)
    expect(call[1].sections.length).toBeGreaterThan(0)
  })
})

// 历史报告的搜索 / 分页 / 删除，以及预览抽屉的加载态（批次 14 新增）。
describe('历史报告搜索、分页与删除', () => {
  function manyReports(n) {
    return {
      reports: Array.from({ length: n }, (_, i) => ({
        id: `rpt-${i + 1}`,
        type: i % 2 ? 'weekly' : 'daily',
        period: `2026-10-${String(i + 1).padStart(2, '0')}`,
        generatedAt: 1700000000000 + i * 1000,
        scope: i === 0 ? '指定 2 个节点' : '全部节点',
      })),
    }
  }

  it('历史报告按关键词过滤', async () => {
    http.get.mockImplementation(async (path) => {
      if (String(path).includes('/api/v1/report/schedule')) return SCHEDULE
      return manyReports(3)
    })
    const w = mountView()
    await flushPromises()

    const input = w.find('.filter-bar input')
    expect(input.exists()).toBe(true)
    await input.setValue('2026-10-02')
    await flushPromises()
    const rows = w.findAll('.el-table__row')
    expect(rows.length).toBe(1)
    expect(rows[0].text()).toContain('2026-10-02')
  })

  it('历史报告分页：每页 10 条', async () => {
    http.get.mockImplementation(async (path) => {
      if (String(path).includes('/api/v1/report/schedule')) return SCHEDULE
      return manyReports(12)
    })
    const w = mountView()
    await flushPromises()
    expect(w.findAll('.el-table__row').length).toBe(10)
    expect(w.find('.el-pagination').exists()).toBe(true)
  })

  it('删除报告走 DELETE 接口并刷新历史', async () => {
    const confirmSpy = vi.spyOn(ElMessageBox, 'confirm').mockResolvedValue('confirm')
    http.get.mockImplementation(async (path) => {
      if (String(path).includes('/api/v1/report/schedule')) return SCHEDULE
      return { reports: [{ id: 'rpt-1', type: 'daily', period: '2026-10-08', generatedAt: 1700000000000 }] }
    })
    http.del.mockResolvedValue({ status: 'ok' })
    const w = mountView()
    await flushPromises()

    const delBtn = w.findAll('button').find((b) => b.text().includes('删除'))
    expect(delBtn).toBeTruthy()
    await delBtn.trigger('click')
    await flushPromises()

    expect(http.del).toHaveBeenCalledWith('/api/v1/report/history/rpt-1')
    confirmSpy.mockRestore()
  })

  it('预览打开抽屉；iframe 加载失败时给出兜底提示', async () => {
    const w = mountView()
    await flushPromises()

    const previewBtn = w.findAll('button').find((b) => b.text().includes('预览'))
    await previewBtn.trigger('click')
    await flushPromises()

    const iframe = w.find('iframe')
    expect(iframe.exists()).toBe(true)
    expect(iframe.attributes('src')).toContain('/api/v1/report/download?id=rpt-1')

    await iframe.trigger('error')
    await flushPromises()
    expect(w.text()).toContain('报告预览加载失败')
  })
})
