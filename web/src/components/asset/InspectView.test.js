// 配置巡检「周期化巡检」卡的回归测试。
//
// 这张卡有几处"错了也不报错"的地方，只能靠断言钉住：
//   ① 保存时把**范围**也提交上去（范围是服务端按身份折算的，前端提交等于给越权留口子）；
//   ② 保存失败后界面停在一个"看起来生效了"的开关位置；
//   ③ 上一轮没跑（占用中 / 范围为空 / 范围来源账号已不存在）却不显示原因——
//      那会让人以为"巡检在跑，只是没问题"。
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick } from 'vue'
import ElementPlus from 'element-plus'
import {
  listInspectRuns,
  listInspectFindings,
  listInspectBaselines,
  getInspectSchedule,
  saveInspectSchedule,
  runInspectScheduleNow,
} from '../../api/asset'
import InspectView from './InspectView.vue'

// 组件用 useRouter().push 做「去资产台账」跳转；测试环境不装 router，
// 用桩替掉，既能消掉 injection 告警，也能断言跳转目标。
const { pushMock } = vi.hoisted(() => ({ pushMock: vi.fn() }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push: pushMock }) }))

vi.mock('../../api/asset', () => ({
  runInspect: vi.fn(),
  listInspectRuns: vi.fn(),
  listInspectFindings: vi.fn(),
  listInspectBaselines: vi.fn(),
  clearAssetBaseline: vi.fn(),
  getInspectSchedule: vi.fn(),
  saveInspectSchedule: vi.fn(),
  runInspectScheduleNow: vi.fn(),
}))

vi.mock('../../composables/useAuth', () => ({
  useAuth: () => ({ can: () => canRun }),
}))

// jsdom 未实现 ResizeObserver，Element Plus 的表格会用到它
if (!globalThis.ResizeObserver) {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
}

let canRun = true
let wrappers = []

function mountView() {
  const w = mount(InspectView, { global: { plugins: [ElementPlus] }, attachTo: document.body })
  wrappers.push(w)
  return w
}

const CONFIG = {
  enabled: false,
  intervalHours: 12,
  type: 'host',
  node: 'web-01',
  keyword: 'redis',
  lastRunAt: 0,
  lastRunId: 0,
  lastManual: false,
  lastError: '',
}

function schedulePayload(over = {}) {
  return { config: { ...CONFIG, ...over }, last: null, nextAt: 0 }
}

// findButton 按文字找按钮（Element Plus 的按钮文字在 span 里）
function findButton(w, text) {
  return w.findAll('button').find((b) => b.text().includes(text))
}

beforeEach(() => {
  vi.clearAllMocks()
  pushMock.mockClear()
  canRun = true
  wrappers = []
  listInspectRuns.mockResolvedValue({ runs: [] })
  listInspectBaselines.mockResolvedValue({ baselines: [] })
  getInspectSchedule.mockResolvedValue(schedulePayload())
  saveInspectSchedule.mockImplementation(async (cfg) => schedulePayload(cfg))
  runInspectScheduleNow.mockResolvedValue({ at: 1, runId: 9, assets: 3, findings: 1, actor: 'admin' })
})

afterEach(() => {
  wrappers.forEach((w) => w.unmount())
  document.body.innerHTML = ''
})

describe('InspectView 周期化巡检卡', () => {
  it('显示服务端的上次运行与下次时间，并区分手动/定时', async () => {
    getInspectSchedule.mockResolvedValue({
      config: { ...CONFIG, enabled: true, lastRunAt: 1_800_000_000_000, lastRunId: 7 },
      last: { at: 1_800_000_000_000, runId: 7 },
      nextAt: 1_800_000_000_000 + 12 * 3600 * 1000,
    })
    const w = mountView()
    await flushPromises()

    expect(w.text()).toContain('上次定时执行')
    expect(w.text()).toContain('#7')
    expect(w.text()).toContain('下次约')
  })

  it('保存时不提交范围（范围由服务端按身份折算）', async () => {
    const w = mountView()
    await flushPromises()

    const sw = w.find('.el-switch')
    expect(sw.exists()).toBe(true)
    await sw.trigger('click')
    await flushPromises()

    expect(saveInspectSchedule).toHaveBeenCalled()
    const payload = saveInspectSchedule.mock.calls.at(-1)[0]
    expect(payload.enabled).toBe(true)
    expect(payload.intervalHours).toBe(12)
    expect(payload.type).toBe('host')
    // 范围绝不出现在前端提交的载荷里：它是授权的一部分
    expect(Object.keys(payload)).not.toContain('scope')
    expect(Object.keys(payload)).not.toContain('nodes')
    expect(Object.keys(payload)).not.toContain('labels')
  })

  it('保存失败要说明，并把界面拉回服务端的真实状态', async () => {
    saveInspectSchedule.mockRejectedValue(new Error('范围来源身份已不存在'))
    getInspectSchedule.mockResolvedValue(schedulePayload({ enabled: false }))
    const w = mountView()
    await flushPromises()

    await w.find('.el-switch').trigger('click')
    await flushPromises()

    // 失败后回拉一次，开关回到服务端报的 false（而不是停在刚点开的 true）
    expect(getInspectSchedule.mock.calls.length).toBeGreaterThan(1)
    expect(w.vm.schedule.enabled).toBe(false)
  })

  it('上一轮没跑（如占用中/范围为空）时把原因显示出来', async () => {
    getInspectSchedule.mockResolvedValue(schedulePayload({
      lastRunAt: 1_800_000_000_000,
      lastError: '范围已收窄为空，本轮不执行；请调整范围或重新保存配置',
    }))
    const w = mountView()
    await flushPromises()

    expect(w.text()).toContain('上一次巡检没有执行或执行失败')
    expect(w.text()).toContain('范围已收窄为空')
  })

  it('立即执行会刷新记录并把这次的结果报出来', async () => {
    const w = mountView()
    await flushPromises()

    const btn = findButton(w, '立即执行一次')
    expect(btn).toBeTruthy()
    await btn.trigger('click')
    await flushPromises()

    expect(runInspectScheduleNow).toHaveBeenCalled()
    // 执行后要重新拉记录（否则新记录要刷新页面才看得到）
    expect(listInspectRuns.mock.calls.length).toBeGreaterThan(1)
  })

  it('记录列表把定时触发标成「定时」', async () => {
    listInspectRuns.mockResolvedValue({
      runs: [
        { id: 1, scope: 'scale:mine', actor: 'schedule', startedAt: 1_800_000_000_000, assets: 3, baselined: 0, findings: 0, truncated: false },
        { id: 2, scope: 'all', actor: 'admin', startedAt: 1_800_000_000_000, assets: 2, baselined: 0, findings: 0, truncated: false },
      ],
    })
    const w = mountView()
    await flushPromises()

    expect(w.text()).toContain('定时')
    expect(w.text()).toContain('admin')
  })

  it('配置取不到时说明不可用，而不是把开关显示成"关着"', async () => {
    getInspectSchedule.mockRejectedValue(new Error('周期化巡检未启用'))
    const w = mountView()
    await flushPromises()

    expect(w.text()).toContain('周期化巡检不可用')
  })

  it('没有 inspect:run 权限时开关与立即执行都禁用', async () => {
    canRun = false
    const w = mountView()
    await flushPromises()

    expect(w.find('.el-switch').classes()).toContain('is-disabled')
    expect(findButton(w, '立即执行一次').attributes('disabled')).toBeDefined()
    expect(w.text()).toContain('缺 inspect:run 权限')
  })
})

// 与手动执行共用同一份记录列表：定时跑出来的也要能被选中看差异项（否则"定时跑了什么"看不到）
describe('InspectView 记录与周期化的衔接', () => {
  it('选中定时跑的记录能取回差异项', async () => {
    listInspectRuns.mockResolvedValue({
      runs: [{ id: 42, scope: 'scale:mine', actor: 'schedule', startedAt: 1_800_000_000_000, assets: 3, baselined: 0, findings: 1, truncated: false }],
    })
    listInspectFindings.mockResolvedValue({
      findings: [{
        id: 1, runId: 42, assetId: 3, assetType: 'host', assetKey: 'web-01', assetName: 'web-01',
        node: 'web-01', field: 'cpuCores', kind: 'changed', level: 'warning', expected: '4', actual: '8',
        at: 1_800_000_000_000,
      }],
    })
    const w = mountView()
    await flushPromises()

    const row = w.find('.el-table__row')
    expect(row.exists()).toBe(true)
    await row.trigger('click')
    await flushPromises()

    expect(listInspectFindings).toHaveBeenCalledWith(42)
    expect(w.text()).toContain('cpuCores')
  })
})

// —— 批次 12（PS-11）：差异项从"选中才出现的一块"升级为"常驻 + 真 diff + 可筛可分组" ——
//
// 这几条都在守"错了也不报错"的地方：
//   ① 差异项区块未选中时必须给引导（整块消失会让人不知道点行会发生什么）；
//   ② diff 不能用占位符顶替（"（字段已缺失）"写在期望列里，等于没说清谁变谁）；
//   ③ 筛选/分页与数据不同步时最容易静默出错（筛选后停在空页）。
describe('InspectView 差异项（常驻 + 真 diff + 筛选）', () => {
  const RUNS = [
    { id: 42, scope: 'scale:mine', actor: 'admin', startedAt: 1_800_000_000_000, assets: 2, baselined: 0, findings: 2, truncated: false },
  ]

  function withFindings(findings) {
    listInspectRuns.mockResolvedValue({ runs: RUNS })
    listInspectFindings.mockResolvedValue({ findings })
  }

  async function selectFirstRun(w) {
    const row = w.find('.el-table__row')
    await row.trigger('click')
    await flushPromises()
  }

  it('未选中记录时差异项区块常驻并给引导空态（而不是整块消失）', async () => {
    const w = mountView()
    await flushPromises()

    // 区块常驻：标题在
    expect(w.text()).toContain('差异项')
    // 引导空态在
    expect(w.text()).toContain('未选择巡检记录')
    expect(w.text()).toContain('点击上方「巡检记录」里的任意一行')
  })

  it('变更类差异渲染成"期望（划掉）→ 实际"的真 diff', async () => {
    withFindings([{
      id: 1, runId: 42, assetId: 3, assetType: 'host', assetKey: 'web-01', assetName: 'web-01',
      node: 'web-01', field: 'cpuCores', kind: 'changed', level: 'warning', expected: '4', actual: '8',
    }])
    const w = mountView()
    await flushPromises()
    await selectFirstRun(w)

    const struck = w.find('.struck')
    expect(struck.exists()).toBe(true)
    expect(struck.text()).toBe('4')
    const fresh = w.find('.diff-new')
    expect(fresh.exists()).toBe(true)
    expect(fresh.text()).toBe('8')
    // 有箭头，不是两列并排
    expect(w.find('.diff-arrow').exists()).toBe(true)
  })

  it('缺失类差异用"（字段已缺失）"标在实际侧，而不是把占位符塞进期望列', async () => {
    withFindings([{
      id: 2, runId: 42, assetId: 3, assetType: 'host', assetKey: 'web-01', assetName: 'web-01',
      node: 'web-01', field: 'maxMemory', kind: 'missing', level: 'critical', expected: '2g', actual: '',
    }])
    const w = mountView()
    await flushPromises()
    await selectFirstRun(w)

    expect(w.find('.struck').text()).toBe('2g')
    const miss = w.find('.diff-miss')
    expect(miss.exists()).toBe(true)
    expect(miss.text()).toContain('字段已缺失')
  })

  it('新增类差异用"（原无）→ 实际"表达', async () => {
    withFindings([{
      id: 3, runId: 42, assetId: 3, assetType: 'host', assetKey: 'web-01', assetName: 'web-01',
      node: 'web-01', field: 'newField', kind: 'added', level: 'info', expected: '', actual: 'on',
    }])
    const w = mountView()
    await flushPromises()
    await selectFirstRun(w)

    expect(w.find('.diff-new').text()).toBe('on')
    // 新增没有"期望"侧，应显示"（原无）"而不是空占位
    expect(w.text()).toContain('（原无）')
  })

  it('按差异类型筛选后只保留匹配项', async () => {
    withFindings([
      { id: 1, runId: 42, assetKey: 'web-01', assetName: 'web-01', field: 'alpha', kind: 'changed', level: 'warning', expected: '1', actual: '2' },
      { id: 2, runId: 42, assetKey: 'web-02', assetName: 'web-02', field: 'beta', kind: 'missing', level: 'critical', expected: '9', actual: '' },
    ])
    const w = mountView()
    await flushPromises()
    await selectFirstRun(w)
    expect(w.text()).toContain('alpha')
    expect(w.text()).toContain('beta')

    w.vm.findingFilter.kind = 'missing'
    await nextTick()
    await flushPromises()

    expect(w.text()).toContain('beta')
    expect(w.text()).not.toContain('alpha')
  })

  it('同一资产的多条差异在「资产」列上纵向合并（按资产分组）', async () => {
    withFindings([
      { id: 1, runId: 42, assetKey: 'web-01', assetName: 'web-01', field: 'f1', kind: 'changed', level: 'warning', expected: '1', actual: '2' },
      { id: 2, runId: 42, assetKey: 'web-01', assetName: 'web-01', field: 'f2', kind: 'changed', level: 'warning', expected: '3', actual: '4' },
    ])
    const w = mountView()
    await flushPromises()
    await selectFirstRun(w)

    // 合并后只剩一个「资产」单元格，rowspan=2
    expect(w.find('td[rowspan="2"]').exists()).toBe(true)
  })

  it('「去资产台账设置标杆」给出跳转入口（与「清除」成对）', async () => {
    listInspectRuns.mockResolvedValue({ runs: RUNS })
    listInspectBaselines.mockResolvedValue({
      baselines: [{ typeKey: 'host', assetKey: 'web-01', assetId: 3, setBy: 'admin', setAt: 1_800_000_000_000 }],
    })
    const w = mountView()
    await flushPromises()

    const link = findButton(w, '去资产台账设置标杆')
    expect(link).toBeTruthy()
    await link.trigger('click')
    expect(pushMock).toHaveBeenCalledWith('/assets')
  })

  it('打印按钮文案随选中状态变化，并带 title 说明', async () => {
    listInspectRuns.mockResolvedValue({ runs: RUNS })
    const w = mountView()
    await flushPromises()

    let btn = findButton(w, '打印')
    expect(btn.text()).toContain('打印巡检记录')

    await selectFirstRun(w)
    btn = findButton(w, '打印')
    expect(btn.text()).toContain('打印本次差异项')
    expect(btn.attributes('title')).toContain('差异项')
  })

  it('差异项加载失败给错误态与重试，而不是只 toast 后停在空白', async () => {
    listInspectRuns.mockResolvedValue({ runs: RUNS })
    listInspectFindings.mockRejectedValue(new Error('findings 接口挂了'))
    const w = mountView()
    await flushPromises()
    await selectFirstRun(w)

    expect(w.text()).toContain('findings 接口挂了')
    expect(findButton(w, '重试')).toBeTruthy()
  })
})
