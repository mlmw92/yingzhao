// DashboardView 自定义仪表盘契约回归。
// 重点锁定批次 14 修的三处可复现缺陷：
//   B6 非编辑态点「添加面板」不再空指针（会自动进入编辑态）
//   B7 面板 chartType（折线/面积/柱状/仪表）真正生效
//   导出 step 复用 rangeBounds（7d → 1800000ms），修正原先恒 60000 的采样错误
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'

const h = vi.hoisted(() => ({
  state: null,
  load: null,
  create: null,
  update: null,
  remove: null,
  httpGet: null,
  metricCatalog: null,
  exportMetricCSV: null,
  monitorOption: null,
}))

vi.mock('../../api/http', () => ({
  default: {
    get: (...a) => h.httpGet(...a),
    metricCatalog: (...a) => h.metricCatalog(...a),
    exportMetricCSV: (...a) => h.exportMetricCSV(...a),
  },
  get: (...a) => h.httpGet(...a),
  metricCatalog: (...a) => h.metricCatalog(...a),
  exportMetricCSV: (...a) => h.exportMetricCSV(...a),
}))

vi.mock('../../charts/echarts', () => ({
  initChart: vi.fn(() => ({
    setOption: vi.fn(),
    resize: vi.fn(),
    dispose: vi.fn(),
    isDisposed: () => false,
  })),
  monitorOption: (...a) => h.monitorOption(...a),
  COLORS: { cyan: '#22d3ee', blue: '#3b82f6', purple: '#a855f7', amber: '#f59e0b', green: '#22c55e', red: '#ef4444' },
}))

vi.mock('../../composables/useDashboards', async () => {
  const { reactive } = await import('vue')
  const state = reactive({ dashboards: [], loaded: false, error: '' })
  h.state = state
  h.load = vi.fn(async () => state.dashboards)
  h.create = vi.fn(async () => ({}))
  h.update = vi.fn(async () => ({}))
  h.remove = vi.fn(async () => ({}))
  const factory = () => ({ state, load: h.load, create: h.create, update: h.update, remove: h.remove })
  return { useDashboards: factory, default: factory }
})

vi.mock('element-plus', () => ({
  ElMessage: { success: vi.fn(), error: vi.fn(), warning: vi.fn() },
}))

import DashboardView from './DashboardView.vue'

const flush = () => new Promise((resolve) => setTimeout(resolve, 0))

const ButtonStub = { name: 'ElButton', emits: ['click'], template: '<button class="el-button" @click="$emit(\'click\')"><slot /></button>' }
const SelectStub = { name: 'ElSelect', props: ['modelValue'], emits: ['update:modelValue'], template: '<div class="sel"><slot /></div>' }
const InputStub = { name: 'ElInput', props: ['modelValue'], emits: ['update:modelValue'], template: '<input class="inp" />' }
const SlotStub = { template: '<div><slot /></div>' }
// 对话框需要同时渲染默认插槽与 footer 插槽，否则「确定」按钮取不到
const DialogStub = { template: '<div class="dlg"><slot /><slot name="footer" /></div>' }

const stubs = {
  'el-button': ButtonStub,
  'el-select': SelectStub,
  'el-input': InputStub,
  'el-option': SlotStub,
  'el-form': SlotStub,
  'el-form-item': SlotStub,
  'el-dialog': DialogStub,
  'el-popconfirm': SlotStub,
  'el-radio-group': SlotStub,
  'el-radio-button': SlotStub,
  'el-alert': SlotStub,
  'el-icon': SlotStub,
}

const CATALOG = { catalog: { system: [{ name: 'cpu_usage', title: 'CPU 使用率', unit: '%' }] } }

function mountView() {
  return mount(DashboardView, { global: { stubs } })
}

function findButton(wrapper, text) {
  return wrapper.findAll('button').find((b) => b.text().includes(text))
}

describe('DashboardView 自定义仪表盘（契约）', () => {
  let wrapper

  beforeEach(() => {
    localStorage.clear()
    h.monitorOption = vi.fn(() => ({}))
    h.httpGet = vi.fn(async () => ({ nodes: [] }))
    h.metricCatalog = vi.fn(async () => CATALOG)
    h.exportMetricCSV = vi.fn(async () => ({}))
    h.state.dashboards = []
    h.state.loaded = false
    h.state.error = ''
    h.load.mockClear()
    h.create.mockClear()
    h.update.mockClear()
    h.remove.mockClear()
  })

  afterEach(() => {
    if (wrapper) wrapper.unmount()
    wrapper = undefined
  })

  it('B6：非编辑态点空态「添加面板」自动进入编辑态且不崩溃', async () => {
    h.state.dashboards = [{ id: 'd1', name: '看板A', panels: [] }]
    wrapper = mountView()
    await flush()

    // 空态里没有面板
    expect(wrapper.findAll('.panel-chart').length).toBe(0)

    const addBtn = findButton(wrapper, '添加面板')
    expect(addBtn).toBeTruthy()
    await addBtn.trigger('click')
    await flush()

    // 进入编辑态：出现「保存」入口，且未抛异常
    expect(findButton(wrapper, '保存')).toBeTruthy()

    // 选指标并确认 → 面板被加入（原实现在这里 draft 为 null 直接空指针）
    const selects = wrapper.findAllComponents({ name: 'ElSelect' })
    selects[0].vm.$emit('update:modelValue', 'cpu_usage')
    await flush()
    const okBtn = findButton(wrapper, '确定')
    await okBtn.trigger('click')
    await flush()

    expect(wrapper.findAll('.panel-chart').length).toBe(1)
  })

  it('B7：面板 chartType 传给 monitorOption（仪表而非恒折线）', async () => {
    h.state.dashboards = [{ id: 'd1', name: '看板A', panels: [{ metric: 'cpu_usage', chartType: 'gauge', range: '1h' }] }]
    wrapper = mountView()
    await flush()

    const calls = h.monitorOption.mock.calls
    expect(calls.length).toBeGreaterThan(0)
    expect(calls[calls.length - 1][0]).toEqual(expect.objectContaining({ chartType: 'gauge' }))
  })

  it('B7：折线类型传 chartType=line', async () => {
    h.state.dashboards = [{ id: 'd1', name: '看板A', panels: [{ metric: 'cpu_usage', chartType: 'line', range: '1h' }] }]
    wrapper = mountView()
    await flush()

    const calls = h.monitorOption.mock.calls
    expect(calls[calls.length - 1][0]).toEqual(expect.objectContaining({ chartType: 'line' }))
  })

  it('导出 step 复用 rangeBounds：7d → 1800000ms', async () => {
    h.state.dashboards = [{ id: 'd1', name: '看板A', panels: [{ metric: 'cpu_usage', chartType: 'line', range: '7d' }] }]
    wrapper = mountView()
    await flush()

    const exportBtn = findButton(wrapper, '导出CSV')
    expect(exportBtn).toBeTruthy()
    await exportBtn.trigger('click')
    await flush()

    expect(h.exportMetricCSV).toHaveBeenCalledTimes(1)
    expect(h.exportMetricCSV.mock.calls[0][0]).toEqual(expect.objectContaining({ metric: 'cpu_usage', step: 1800000 }))
  })

  it('当前看板记忆：localStorage 中的 activeId 优先于首个看板', async () => {
    h.state.dashboards = [
      { id: 'd1', name: '看板A', panels: [] },
      { id: 'd2', name: '看板B', panels: [] },
    ]
    localStorage.setItem('nebula_dash_active', 'd2')
    wrapper = mountView()
    await flush()

    // 次级导航中 d2 处于选中态
    const on = wrapper.findAll('.subnav-item').find((s) => s.classes().includes('on'))
    expect(on.text()).toBe('看板B')
  })
})
