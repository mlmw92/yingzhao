// 资产关系图（TopologyGraph）的用例（批次 12）。
//
// 这个组件有三个入口（台账详情 / 告警影响面 / 全库关系视图），因此「图四件套」
// （图例 / 缩放 / 全屏 / 导出）与两种工作模式（邻域 / 数据）必须在这一层就被钉死——
// 放到页面层会让三处各写一遍，迟早不一致。
//
// 盯的都是"不报错但会出错"的行为：
//   ① 点图例只是变灰、图上节点没少（那等于没过滤）；
//   ② 缩放/复位只改了本地状态、没落到图表（点半天没反应）；
//   ③ 全屏后没有键盘出口（工具栏被顶出视口就再也退不出来）；
//   ④ 邻域模式点节点换了中心、但 depth 参数丢了（图展开的层数默默回到默认）。
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick } from 'vue'
import ElementPlus from 'element-plus'
import { getAssetTopology } from '../../api/asset'
import TopologyGraph from './TopologyGraph.vue'

vi.mock('../../api/asset', () => ({ getAssetTopology: vi.fn() }))

// 捕获 echarts 实例与注册的事件处理器（点击节点走的是 chart.on('click')）。
// chart 只在 init 被调用时才赋值，因此这里不构成 vi.mock 工厂的求值期依赖。
let chart = null
const handlers = {}
vi.mock('echarts', () => ({
  init: vi.fn(() => {
    chart = {
      setOption: vi.fn(),
      on: vi.fn((ev, fn) => { handlers[ev] = fn }),
      off: vi.fn((ev) => { if (ev) delete handlers[ev] }),
      dispose: vi.fn(),
      resize: vi.fn(),
      getDataURL: vi.fn(() => 'data:image/png;base64,AAAA'),
    }
    return chart
  }),
}))

if (!globalThis.ResizeObserver) {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
}

const NEIGHBORHOOD = {
  total: 2,
  truncated: false,
  nodes: [
    { key: 'host|h1', id: 7, typeKey: 'host', typeTitle: '主机', naturalKey: 'h1', name: 'web-01', node: 'web-01', status: 'online', root: true, depth: 0 },
    { key: 'middleware-instance|r1', id: 8, typeKey: 'middleware-instance', typeTitle: '中间件实例', naturalKey: 'redis:1', name: 'redis', node: 'web-01', status: 'missing', depth: 1 },
  ],
  edges: [{ from: 'host|h1', to: 'middleware-instance|r1', kind: 'runs_on', source: 'discovery' }],
}

// 三类型三节点两条边：隐藏「容器（Pod）」后，主机→中间件那条边两端都还在（该留），
// 中间件→容器那条边被剔除（只隐藏一端会留下连到空气的线）。
const DATA = {
  total: 2,
  truncated: false,
  nodes: [
    { key: 'host|h1', id: 7, typeKey: 'host', typeTitle: '主机', naturalKey: 'h1', name: 'web-01', node: 'web-01', status: 'online' },
    { key: 'middleware-instance|r1', id: 8, typeKey: 'middleware-instance', typeTitle: '中间件实例', naturalKey: 'redis:1', name: 'redis', node: 'web-01', status: 'online' },
    { key: 'pod|p1', id: 9, typeKey: 'pod', typeTitle: '容器（Pod）', naturalKey: 'p1', name: 'web-abc', node: 'web-01', status: 'online' },
  ],
  edges: [
    { from: 'host|h1', to: 'middleware-instance|r1', kind: 'runs_on', source: 'discovery' },
    { from: 'middleware-instance|r1', to: 'pod|p1', kind: 'member_of', source: 'manual' },
  ],
}

const byTitle = (wrapper, title) =>
  wrapper.findAll('.topo-tools button').find((b) => b.attributes('title') === title)

const mountGraph = (props) => mount(TopologyGraph, {
  props,
  global: { plugins: [ElementPlus] },
  attachTo: document.body,
})

// 最近一次 setOption 的入参
const lastOption = () => chart.setOption.mock.calls[chart.setOption.mock.calls.length - 1][0]

describe('TopologyGraph · 图四件套与两种模式', () => {
  let wrapper

  beforeEach(() => {
    vi.clearAllMocks()
    chart = null
    for (const k of Object.keys(handlers)) delete handlers[k]
    getAssetTopology.mockResolvedValue(NEIGHBORHOOD)
  })

  afterEach(() => {
    if (wrapper) wrapper.unmount()
    wrapper = undefined
    document.body.innerHTML = ''
    vi.restoreAllMocks()
  })

  it('邻域模式：按 assetId 取数，跳数默认 2', async () => {
    wrapper = mountGraph({ assetId: 7 })
    await flushPromises()

    expect(getAssetTopology).toHaveBeenCalledWith(7, { depth: 2 })
    expect(chart.setOption).toHaveBeenCalled()
    // 邻域模式有跳数切换
    expect(wrapper.text()).toContain('1 跳')
    expect(wrapper.text()).toContain('点节点可以「以它为中心」重新展开')
  })

  // change 事件会把新值当第一个实参传进来，而 load 的第一个参数是资产 id：
  // 若不显式忽略，切"3 跳"会变成"查资产 3"。
  it('跳数切换不会把跳数当成资产 id', async () => {
    wrapper = mountGraph({ assetId: 7 })
    await flushPromises()
    getAssetTopology.mockClear()

    const rg = wrapper.findComponent({ name: 'ElRadioGroup' })
    await rg.vm.$emit('change', 3)
    await flushPromises()

    expect(getAssetTopology).toHaveBeenCalled()
    expect(getAssetTopology.mock.calls[0][0]).toBe(7)
  })

  it('邻域模式点非中心节点 → 以它为中心重新展开', async () => {
    wrapper = mountGraph({ assetId: 7 })
    await flushPromises()
    getAssetTopology.mockClear()

    handlers.click({ data: { raw: { id: 8, root: false } } })
    await flushPromises()
    expect(getAssetTopology).toHaveBeenCalledWith(8, { depth: 2 })
  })

  it('数据模式：不自己取数，点节点向外抛 node-click', async () => {
    wrapper = mountGraph({ data: DATA, reCenter: false })
    await flushPromises()

    expect(getAssetTopology).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('点节点打开该资产的详情')

    handlers.click({ data: { raw: { id: 9, naturalKey: 'p1' } } })
    await nextTick()
    const emitted = wrapper.emitted('node-click')
    expect(emitted).toBeTruthy()
    expect(emitted[0][0].id).toBe(9)
  })

  // —— 图例（可点击过滤）——

  it('图例只列当前图出现过的类型，点击即过滤节点', async () => {
    wrapper = mountGraph({ data: DATA, reCenter: false })
    await flushPromises()

    const items = wrapper.findAll('.lg-item')
    expect(items.length).toBe(3) // host + middleware-instance + pod
    expect(wrapper.text()).toContain('主机')
    expect(wrapper.text()).toContain('容器（Pod）')

    expect(lastOption().series[0].data.length).toBe(3)
    expect(lastOption().series[0].links.length).toBe(2)

    const podItem = items.find((it) => it.text().includes('容器（Pod）'))
    await podItem.trigger('click') // 隐藏「容器（Pod）」
    await flushPromises()

    // 节点少了一个，且被隐藏的类型对应的按钮进入 off 态
    expect(lastOption().series[0].data.length).toBe(2)
    expect(podItem.classes()).toContain('off')
    // 只剩"主机 → 中间件"那条边：两端都还在图里（只隐藏一端会留下连到空气的线）
    expect(lastOption().series[0].links.length).toBe(1)

    await podItem.trigger('click') // 再点恢复
    await flushPromises()
    expect(lastOption().series[0].data.length).toBe(3)
    expect(lastOption().series[0].links.length).toBe(2)
  })

  // —— 缩放 ——

  it('放大 / 缩小 / 复位都把倍率落到图表上', async () => {
    wrapper = mountGraph({ data: DATA, reCenter: false })
    await flushPromises()

    await byTitle(wrapper, '放大').trigger('click')
    expect(lastOption().series[0].zoom).toBeCloseTo(1.2)

    await byTitle(wrapper, '缩小').trigger('click')
    expect(lastOption().series[0].zoom).toBeCloseTo(1)

    await byTitle(wrapper, '放大').trigger('click')
    await byTitle(wrapper, '放大').trigger('click')
    expect(lastOption().series[0].zoom).toBeCloseTo(1.44)

    await byTitle(wrapper, '复位缩放与视角').trigger('click')
    expect(lastOption().series[0].zoom).toBe(1)
  })

  // —— 全屏（含 Esc 出口）——

  it('全屏切换 + Esc 退出（全屏后必须有键盘出口）', async () => {
    wrapper = mountGraph({ data: DATA, reCenter: false })
    await flushPromises()

    expect(wrapper.find('.topo').classes()).not.toContain('fullscreen')

    await byTitle(wrapper, '全屏查看').trigger('click')
    await nextTick()
    expect(wrapper.find('.topo').classes()).toContain('fullscreen')

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await nextTick()
    expect(wrapper.find('.topo').classes()).not.toContain('fullscreen')
  })

  // —— 导出 PNG ——

  it('导出调用图表 getDataURL 并触发下载', async () => {
    wrapper = mountGraph({ data: DATA, reCenter: false })
    await flushPromises()

    const clickSpy = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
    await byTitle(wrapper, '导出为 PNG 图片').trigger('click')

    expect(chart.getDataURL).toHaveBeenCalled()
    expect(chart.getDataURL.mock.calls[0][0].pixelRatio).toBe(2)
    expect(clickSpy).toHaveBeenCalled()
  })

  // —— 加载态 ——

  it('loading 为真时画布盖加载遮罩', async () => {
    wrapper = mountGraph({ data: DATA, reCenter: false, loading: true })
    await flushPromises()
    expect(wrapper.find('.el-loading-mask').exists()).toBe(true)
  })

  // —— 截断提示随模式变化 ——

  it('数据模式的截断提示说的是"命中关系超上限"', async () => {
    wrapper = mountGraph({ data: { ...DATA, truncated: true }, reCenter: false })
    await flushPromises()
    expect(wrapper.text()).toContain('命中结果超过上限')
  })

  it('邻域模式的截断提示说的是"节点数达到上限"', async () => {
    getAssetTopology.mockResolvedValue({ ...NEIGHBORHOOD, truncated: true })
    wrapper = mountGraph({ assetId: 7 })
    await flushPromises()
    expect(wrapper.text()).toContain('节点数达到上限')
  })

  // 邻域模式取数失败要有错误条，而不是停在空白画布上
  it('邻域模式加载失败给出错误条', async () => {
    getAssetTopology.mockRejectedValue(new Error('查询邻域失败'))
    wrapper = mountGraph({ assetId: 7 })
    await flushPromises()
    expect(wrapper.text()).toContain('查询邻域失败')
  })
})
