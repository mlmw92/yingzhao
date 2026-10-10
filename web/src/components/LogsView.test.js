// 集中日志页的**结构化字段检索**回归测试。
//
// 为什么要有这个文件：字段过滤是本页唯一的"精确"筛法（关键词会在别的字段的值里误命中），
// 而它的三种失败都不报错：参数没拼进请求、条件形态不对被服务端 400、
// 候选字段列的是"可能存在的名字"（用户按它筛永远查不到）。只能靠断言钉住。
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import http from '../api/http'
import LogsView from './LogsView.vue'

vi.mock('../api/http', () => ({
  default: { get: vi.fn(), post: vi.fn(), put: vi.fn(), del: vi.fn() },
  getToken: vi.fn(() => ''),
}))

const routerReplace = vi.fn()
const routerPush = vi.fn()
let routeQuery = {}
vi.mock('vue-router', () => ({
  useRouter: () => ({ push: routerPush, replace: routerReplace }),
  useRoute: () => ({ query: routeQuery }),
}))

// 权限按用例设置：日志→资产、由日志建规则都受权限门控（UI 反映权限）。
let permissions = []
vi.mock('../composables/useAuth', () => ({
  default: () => ({ can: (perm) => permissions.includes(perm) }),
}))

if (!globalThis.ResizeObserver) {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
}

let wrappers = []

function mountView() {
  const w = mount(LogsView, { global: { plugins: [ElementPlus] }, attachTo: document.body })
  wrappers.push(w)
  return w
}

// logsCalls 返回对 /api/v1/logs 的查询串（排除字段候选接口）。
function logsQueries() {
  return http.get.mock.calls
    .map((c) => String(c[0]))
    .filter((p) => p.startsWith('/api/v1/logs?'))
}

beforeEach(() => {
  vi.clearAllMocks()
  wrappers = []
  routeQuery = {}
  permissions = []
  http.get.mockImplementation(async (path) => {
    if (String(path).includes('/api/v1/logs/fields')) {
      return { fields: { applog: ['level', 'status'] } }
    }
    if (String(path).includes('/api/v1/logs?')) {
      return {
        lines: [
          {
            ts: Date.now(), node: 'web-01', source: 'applog', pattern: 'err',
            text: '{"level":"error"}', fields: { level: 'error', status: '500' },
          },
        ],
        truncated: false,
        // 服务端给出的「来源 + 节点 → 资产」映射（资产上用人工属性 logSource 声明归属）
        assets: {
          'applog|web-01': [{ id: 7, typeKey: 'host', typeTitle: '主机', naturalKey: 'web-01', name: 'web-01', node: 'web-01' }],
        },
      }
    }
    if (String(path).includes('/api/v1/nodes')) {
      return { nodes: [{ hostname: 'web-01', logSources: ['applog'] }] }
    }
    return {}
  })
})

afterEach(() => {
  for (const w of wrappers) w.unmount()
  document.body.innerHTML = ''
})

describe('LogsView 结构化字段检索', () => {
  it('字段条件会拼进查询，形态不对则拒绝并提示', async () => {
    const w = mountView()
    await flushPromises()

    const input = w.findAll('input').find((i) => i.element.placeholder.includes('字段过滤'))
    expect(input).toBeTruthy()

    // 形态不对：不加入条件（否则服务端 400，用户只看到一个报错）
    await input.setValue('status500')
    await input.trigger('keyup.enter')
    await flushPromises()
    expect(logsQueries().some((q) => q.includes('field='))).toBe(false)

    // 正确形态：加入条件，下一次查询带上 field=status:500
    await input.setValue('status:500')
    await input.trigger('keyup.enter')
    await flushPromises()
    const button = w.findAll('button').find((b) => b.text().includes('查询'))
    await button.trigger('click')
    await flushPromises()

    const last = logsQueries().at(-1)
    expect(last).toContain('field=status%3A500')
  })

  it('同一个字段只允许一个值：第二次输入被拒绝', async () => {
    const w = mountView()
    await flushPromises()
    const input = w.findAll('input').find((i) => i.element.placeholder.includes('字段过滤'))

    await input.setValue('status:500')
    await input.trigger('keyup.enter')
    await input.setValue('status:404')
    await input.trigger('keyup.enter')
    await flushPromises()

    // 生效条件现在统一走 FilterChips 回显（label：value）：
    // 「时间」chip 默认恒在，因此这里断言的是"字段条件只有一条"
    const chips = w.findAll('.fc-chip')
    const fieldChips = chips.filter((c) => c.text().startsWith('字段：'))
    expect(fieldChips.length).toBe(1)
    expect(fieldChips[0].text()).toBe('字段：status:500')
  })

  it('候选字段只列服务端见过的名字，点击后填入输入框', async () => {
    const w = mountView()
    await flushPromises()

    expect(w.text()).toContain('已知字段')
    const pick = w.findAll('button').find((b) => b.text() === 'status')
    expect(pick).toBeTruthy()
    await pick.trigger('click')
    await flushPromises()

    const input = w.findAll('input').find((i) => i.element.placeholder.includes('字段过滤'))
    expect(input.element.value).toBe('status:')
  })

  it('结果里展示结构化字段，且深链带来的 field 条件被采纳', async () => {
    routeQuery = { field: ['level:error', 'status:500'] }
    const w = mountView()
    await flushPromises()

    // 3 条 chip = 时间（默认范围总是存在）+ 两条字段过滤
    expect(w.findAll('.fc-chip').length).toBe(3)
    const last = logsQueries().at(-1)
    expect(last).toContain('field=level%3Aerror')
    expect(last).toContain('field=status%3A500')
    // 字段贴在日志原文上方
    expect(w.findAll('.field-chip').length).toBe(2)
  })
})

describe('LogsView 日志 → 资产联动', () => {
  it('把日志行标到资产上，并可「只看该资产」', async () => {
    const w = mountView()
    await flushPromises()

    const chip = w.find('.asset-chip')
    expect(chip.exists()).toBe(true)
    expect(chip.text()).toContain('web-01')

    // 「只看」= 节点 + 来源一起收窄（资产已声明来源，节点就是它的归属节点）
    const only = w.findAll('button').find((b) => b.text().includes('只看'))
    expect(only).toBeTruthy()
    await only.trigger('click')
    await flushPromises()

    const last = logsQueries().at(-1)
    expect(last).toContain('nodes=web-01')
    expect(last).toContain('sources=applog')
  })

  it('点资产标签跳到台账详情', async () => {
    const w = mountView()
    await flushPromises()
    await w.find('.asset-chip').trigger('click')
    expect(routerPush).toHaveBeenCalledWith({ path: '/assets', query: { id: '7' } })
  })

  it('「建规则」按权限显示，并把来源/模式/节点带给告警页', async () => {
    permissions = []
    let w = mountView()
    await flushPromises()
    expect(w.findAll('button').some((b) => b.text().includes('建规则'))).toBe(false)
    w.unmount()

    permissions = ['alerts:write']
    w = mountView()
    await flushPromises()
    const btn = w.findAll('button').find((b) => b.text().includes('建规则'))
    expect(btn).toBeTruthy()
    await btn.trigger('click')
    // 指标名由服务端拼（拼错的症状是"规则配好了却没有数据"），前端只传来源与模式
    expect(routerPush).toHaveBeenCalledWith({
      path: '/alerts',
      query: { newLogRule: 'applog|err', node: 'web-01' },
    })
  })
})

// 后端能力提示（批次 23）。
//
// 这一行要回答的是"我处在哪一种后端下、能查多大"——它错起来的两种方式都不报错：
// ① 本地后端不说"有界扫描"，用户撞到 64 MiB 预算时不知道为什么；
// ② 探测失败被显示成"没有日志"，那会把"问不到后端"读成"日志丢了"。
describe('LogsView 后端能力提示', () => {
  function mockBackend(cap, truncated = false) {
    http.get.mockImplementation(async (path) => {
      const p = String(path)
      if (p === '/api/v1/logs/backend') return cap
      if (p.includes('/api/v1/logs/fields')) return { fields: {} }
      if (p.includes('/api/v1/logs?')) {
        return { lines: [], truncated, cursor: '', scanned: { bytes: 0, lines: 0, files: 0 } }
      }
      return {}
    })
  }

  it('本地后端：说清"有界扫描 + 预算"，并在被截断时把指引指向切换后端', async () => {
    mockBackend({
      backend: 'local',
      capabilities: {
        fullTextIndex: false, fieldIndex: false,
        scanBudget: { bytes: 64 << 20, lines: 200000 }, notes: [],
      },
      storage: { sources: 3, nodes: 1, oldestDay: '2026-10-01', newestDay: '2026-10-07', bytes: 1024 },
    }, true)
    const w = mountView()
    await flushPromises()

    expect(w.text()).toContain('有界扫描')
    expect(w.text()).toContain('64 MiB')
    expect(w.text()).toContain('已有 3 个来源')
    // 撞到边界那一刻才是最需要"换后端"这句话的时候
    expect(w.text()).toContain('需把日志后端切到 VictoriaLogs')
  })

  it('VictoriaLogs 后端：说"索引检索"，不再提切换后端', async () => {
    mockBackend({
      backend: 'victorialogs',
      capabilities: { fullTextIndex: true, fieldIndex: true, scanBudget: null, notes: ['历史本地分片不会自动迁入'] },
      storage: { sources: 2, nodes: 0, bytes: 0 },
    })
    const w = mountView()
    await flushPromises()

    expect(w.text()).toContain('VictoriaLogs（索引检索）')
    expect(w.text()).toContain('关键词与字段由后端索引检索')
    expect(w.text()).not.toContain('需把日志后端切到 VictoriaLogs')
  })

  it('探测失败：说"问不到后端"，并明确这不是"没有日志"', async () => {
    mockBackend({
      backend: 'victorialogs',
      capabilities: { fullTextIndex: true, fieldIndex: true, scanBudget: null, notes: [] },
      storage: { sources: 0, nodes: 0, bytes: 0, probeError: '探测外部后端失败: 连接被拒绝' },
    })
    const w = mountView()
    await flushPromises()

    expect(w.text()).toContain('日志后端探测失败')
    expect(w.text()).toContain('这不代表没有日志')
  })
})

// 命中高亮 / 筛选回显 / 导出。
//
// 这一批的共同点是"错了也不报错"：高亮不生效只是看起来没命中、chip 撤了不重查
// 会让人以为条件还在生效、导出的 CSV 少了 BOM 只在 Excel 里才乱码。
describe('LogsView 命中高亮 / 筛选回显 / 导出', () => {
  const LINE = {
    ts: Date.now(), node: 'web-01', source: 'applog', pattern: 'err',
    text: 'error: disk error on sda', fields: {},
  }

  function mockLines(lines) {
    http.get.mockImplementation(async (path) => {
      const p = String(path)
      if (p.includes('/api/v1/logs/fields')) return { fields: {} }
      if (p.includes('/api/v1/logs/backend')) return {}
      if (p.includes('/api/v1/logs?')) return { lines, truncated: false, cursor: '', assets: {}, podAssets: {} }
      if (p.includes('/api/v1/nodes')) return { nodes: [{ hostname: 'web-01', logSources: ['applog'] }] }
      return {}
    })
  }

  async function searchWith(w, placeholder, value) {
    const input = w.findAll('input').find((i) => i.element.placeholder.includes(placeholder))
    await input.setValue(value)
    await w.findAll('button').find((b) => b.text().includes('查询')).trigger('click')
    await flushPromises()
  }

  it('关键词命中就地高亮，并给出行内命中数', async () => {
    mockLines([LINE])
    const w = mountView()
    await flushPromises()
    await searchWith(w, '子串', 'error')

    expect(w.find('.logline').findAll('mark').length).toBe(2)
    // 高亮不能吃掉原文：非命中片段必须还在
    expect(w.find('.logline').text()).toContain('disk')
    expect(w.find('.hit-badge').text()).toBe('2')
  })

  it('关键词模式按字面量匹配：正则元字符不当语法', async () => {
    mockLines([{ ...LINE, text: 'aXb a.b' }])
    const w = mountView()
    await flushPromises()
    await searchWith(w, '子串', 'a.b')

    // 若把 'a.b' 当正则，'aXb' 也会被标；字面量匹配则只有 'a.b' 一处
    expect(w.find('.logline').findAll('mark').length).toBe(1)
  })

  it('未查询时不标错位置：高亮只认"本次生效"的关键词', async () => {
    mockLines([LINE])
    const w = mountView()
    await flushPromises()
    // 只改输入框、不点查询
    const input = w.findAll('input').find((i) => i.element.placeholder.includes('子串'))
    await input.setValue('disk')
    await flushPromises()
    expect(w.find('.logline').findAll('mark').length).toBe(0)
  })

  it('撤销筛选 chip 立即重查，且该条件从请求里消失', async () => {
    mockLines([LINE])
    const w = mountView()
    await flushPromises()

    const fieldInput = w.findAll('input').find((i) => i.element.placeholder.includes('字段过滤'))
    await fieldInput.setValue('status:500')
    await fieldInput.trigger('keyup.enter')
    await w.findAll('button').find((b) => b.text().includes('查询')).trigger('click')
    await flushPromises()
    expect(logsQueries().at(-1)).toContain('field=status%3A500')

    const chip = w.findAll('.fc-chip').find((c) => c.text().includes('status:500'))
    expect(chip).toBeTruthy()
    await chip.find('.fc-x').trigger('click')
    await flushPromises()

    expect(logsQueries().at(-1)).not.toContain('field=')
  })

  it('导出 CSV：带 BOM、列头与当前结果', async () => {
    mockLines([LINE])
    // jsdom 的 Blob 没有 .text()，这里从构造入参直接取回原始 CSV 文本
    const parts = []
    const OrigBlob = globalThis.Blob
    globalThis.Blob = class extends OrigBlob {
      constructor(bs, opts) {
        super(bs, opts)
        parts.push(bs.join(''))
      }
    }
    const origCreate = URL.createObjectURL
    const origRevoke = URL.revokeObjectURL
    URL.createObjectURL = vi.fn(() => 'blob:logs')
    URL.revokeObjectURL = vi.fn()
    const clickSpy = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})

    try {
      const w = mountView()
      await flushPromises()
      const btn = w.findAll('button').find((b) => b.text().includes('导出 CSV'))
      expect(btn).toBeTruthy()
      await btn.trigger('click')
      await flushPromises()

      expect(parts.length).toBe(1)
      expect(clickSpy).toHaveBeenCalled()
      // 不带 BOM 时 Excel 打开中文列会乱码
      expect(parts[0].charCodeAt(0)).toBe(0xfeff)
      expect(parts[0]).toContain('时间,节点,来源')
      expect(parts[0]).toContain('error: disk error on sda')
    } finally {
      globalThis.Blob = OrigBlob
      URL.createObjectURL = origCreate
      URL.revokeObjectURL = origRevoke
      clickSpy.mockRestore()
    }
  })
})
