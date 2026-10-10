// 关系视图（全库）的用例（批次 22，批次 12 补四态）。
//
// 这一页最容易出的两类问题都**不报错**，所以用例盯的就是它们：
//   ① 点总览的一行没把筛选带全 → 图看起来"点了没反应"，或者把别的类型也带进来（那是用户没点的东西）；
//   ② 命中关系被上限截断却不说 → 用户以为"关系就这么多"，那正是这张图存在的意义所在。
//
// 批次 12 追加：总览与图各自持有 loading / error——一段接口挂了不该让另一段一起变空，
// 也不该在数据还没回来时显示"没有关系"（那是误导，不是空态）。
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import http from '../../api/http'
import { getLinkStats, getTopologyGraph } from '../../api/asset'
import AssetTopologyView from './AssetTopologyView.vue'
import TopologyGraph from './TopologyGraph.vue'

vi.mock('../../api/http', () => ({
  default: { get: vi.fn(), post: vi.fn(), put: vi.fn(), del: vi.fn() },
  getToken: vi.fn(() => ''),
  setToken: vi.fn(),
}))

vi.mock('../../api/asset', () => ({
  getLinkStats: vi.fn(),
  getTopologyGraph: vi.fn(),
}))

// 记录 echarts 实例（页面用的是 TopologyGraph，它内部会 init）
let chart = null
vi.mock('echarts', () => ({
  init: vi.fn(() => {
    chart = { setOption: vi.fn(), on: vi.fn(), off: vi.fn(), dispose: vi.fn(), resize: vi.fn() }
    return chart
  }),
}))

const routerPush = vi.fn()
vi.mock('vue-router', () => ({
  useRouter: () => ({ push: routerPush, replace: vi.fn() }),
  useRoute: () => ({ query: {}, path: '/assets/topology' }),
}))

if (!globalThis.ResizeObserver) {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
}

const GROUPS = [
  {
    fromType: 'middleware-instance', kind: 'runs_on', toType: 'host', source: 'discovery', count: 42,
    sample: { fromKey: 'redis:10.0.0.10:7000', fromName: 'redis', toKey: 'VM-0-10-ubuntu', toName: 'VM-0-10-ubuntu' },
  },
  {
    fromType: 'middleware-instance', kind: 'depends_on', toType: 'middleware-instance', source: 'discovery', count: 3,
    sample: { fromKey: 'redis:10.0.0.10:7000', fromName: 'redis', toKey: 'redis:10.0.0.10:7004', toName: 'redis' },
  },
]

const GRAPH = {
  total: 3,
  truncated: false,
  nodes: [
    { key: 'middleware-instance|redis:10.0.0.10:7000', id: 7, typeKey: 'middleware-instance', typeTitle: '中间件实例', naturalKey: 'redis:10.0.0.10:7000', name: 'redis', node: 'VM-0-10-ubuntu', status: 'online' },
    { key: 'middleware-instance|redis:10.0.0.10:7004', id: 8, typeKey: 'middleware-instance', typeTitle: '中间件实例', naturalKey: 'redis:10.0.0.10:7004', name: 'redis', node: 'VM-0-10-ubuntu', status: 'online' },
  ],
  edges: [{ from: 'middleware-instance|redis:10.0.0.10:7000', to: 'middleware-instance|redis:10.0.0.10:7004', kind: 'depends_on', source: 'discovery' }],
}

function mountView() {
  return mount(AssetTopologyView, {
    global: { plugins: [ElementPlus] },
    attachTo: document.body,
  })
}

const findButton = (wrapper, text) => wrapper.findAll('button').find((b) => b.text().includes(text))

describe('关系视图（全库）', () => {
  let wrapper

  beforeEach(() => {
    vi.clearAllMocks()
    chart = null
    getLinkStats.mockResolvedValue({ groups: GROUPS, totalLinks: 45, assetsWithoutLinks: 12 })
    getTopologyGraph.mockResolvedValue(GRAPH)
    http.get.mockResolvedValue({ nodes: [{ hostname: 'VM-0-10-ubuntu' }, { hostname: 'db-01' }] })
  })

  afterEach(() => {
    if (wrapper) wrapper.unmount()
    wrapper = undefined
    document.body.innerHTML = ''
  })

  it('总览按形态聚合展示，并给出无边资产数', async () => {
    wrapper = mountView()
    await flushPromises()

    expect(getLinkStats).toHaveBeenCalled()
    expect(wrapper.text()).toContain('运行于')
    expect(wrapper.text()).toContain('依赖')
    expect(wrapper.text()).toContain('45') // 关系总数
    expect(wrapper.text()).toContain('12') // 无边资产
    // 样本边要显示出来（"这行到底是什么"不点进去也能看懂）：显示的是名字，
    // 精确自然键在 title 里（同名实例不少，要核对时得有准值）
    expect(wrapper.text()).toContain('redis → VM-0-10-ubuntu')
    expect(wrapper.find('.ov-table .mono').attributes('title')).toBe('redis:10.0.0.10:7000 → VM-0-10-ubuntu')
  })

  // Service 是本批新增的资产类型，也是 exposes 的**第一个自动来源**：
  // 总览里必须显示中文名（「K8s 服务」/「暴露」），不能是原始 key——
  // 否则用户看到一行 "service → pod / exposes" 根本不知道它在说什么关系。
  it('新增的 service 类型与 exposes 种类都有中文名', async () => {
    getLinkStats.mockResolvedValue({
      groups: [{
        fromType: 'service', kind: 'exposes', toType: 'pod', source: 'discovery', count: 2,
        sample: {
          fromKey: 'https://10.0.0.9:6443/default/service/web', fromName: 'web',
          toKey: 'https://10.0.0.9:6443/default/pod/web-abc', toName: 'web-abc',
        },
      }],
      totalLinks: 2,
      assetsWithoutLinks: 0,
    })
    wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('K8s 服务')
    expect(wrapper.text()).toContain('暴露')
    expect(wrapper.text()).toContain('web → web-abc')
  })

  it('点总览的一行把「种类 + 边来源 + 来源类型」三个维度一起带给图', async () => {
    wrapper = mountView()
    await flushPromises()
    getTopologyGraph.mockClear()

    const rows = wrapper.findAll('.el-table__row')
    expect(rows.length).toBe(2)
    // 点第二行（依赖那一组）
    await rows[1].trigger('click')
    await flushPromises()

    const params = getTopologyGraph.mock.calls[0][0]
    expect(params.kind).toEqual(['depends_on'])
    expect(params.source).toBe('discovery')
    // 来源类型也要带上：否则会顺带把"其它类型 → 中间件实例"也拉进图里（用户没点那个）
    expect(params.type).toEqual(['middleware-instance'])
  })

  it('命中关系被截断时显式提示，并给出总数', async () => {
    getTopologyGraph.mockResolvedValue({ ...GRAPH, total: 3456, truncated: true })
    wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('3456')
    expect(wrapper.text()).toContain('超过上限')
  })

  it('点图上的节点 → 打开该资产详情（台账页 ?id= 深链）', async () => {
    wrapper = mountView()
    await flushPromises()

    wrapper.findComponent(TopologyGraph).vm.$emit('node-click', { id: 8, naturalKey: 'redis:10.0.0.10:7004' })
    await flushPromises()
    expect(routerPush).toHaveBeenCalledWith({ path: '/assets', query: { id: '8' } })
  })

  it('总览加载失败时给出错误而不是空表', async () => {
    getLinkStats.mockRejectedValue(new Error('统计资产关系失败'))
    wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('统计资产关系失败')
  })

  // —— 批次 12：两段各自四态 ——

  it('关系图加载失败时给出错误与重试，总览不受影响', async () => {
    getTopologyGraph.mockRejectedValue(new Error('查询资产关系图失败'))
    wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('关系图加载失败')
    expect(wrapper.text()).toContain('查询资产关系图失败')
    // 总览是另一条请求，不该跟着一起变空
    expect(wrapper.text()).toContain('运行于')
    expect(wrapper.text()).toContain('45')
    // 错误态要能重试
    expect(findButton(wrapper, '重试')).toBeTruthy()
  })

  it('关系图错误态点重试会重新拉取并恢复', async () => {
    getTopologyGraph.mockRejectedValue(new Error('boom'))
    wrapper = mountView()
    await flushPromises()
    expect(wrapper.text()).toContain('boom')

    getTopologyGraph.mockClear()
    getTopologyGraph.mockResolvedValue(GRAPH)
    await findButton(wrapper, '重试').trigger('click')
    await flushPromises()

    expect(getTopologyGraph).toHaveBeenCalled()
    expect(wrapper.text()).not.toContain('boom')
    expect(wrapper.findComponent(TopologyGraph).exists()).toBe(true)
  })

  it('总览错误态点重试会重新拉取', async () => {
    getLinkStats.mockRejectedValue(new Error('统计失败'))
    wrapper = mountView()
    await flushPromises()

    getLinkStats.mockClear()
    getLinkStats.mockResolvedValue({ groups: GROUPS, totalLinks: 45, assetsWithoutLinks: 12 })
    await findButton(wrapper, '重试').trigger('click')
    await flushPromises()

    expect(getLinkStats).toHaveBeenCalled()
    expect(wrapper.text()).toContain('运行于')
  })

  // 首次加载（还没有任何边）时不能显示"当前筛选下没有关系"——那是空态语义，
  // 数据没回来就写这句会让人以为筛选把关系全过滤掉了。
  it('关系图首次加载未返回时显示加载骨架而不是"没有关系"空态', async () => {
    let resolveGraph
    getTopologyGraph.mockReturnValue(new Promise((r) => { resolveGraph = r }))
    wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).not.toContain('当前筛选下没有关系')
    expect(wrapper.find('[aria-busy="true"]').exists()).toBe(true)

    resolveGraph(GRAPH)
    await flushPromises()
    expect(wrapper.findComponent(TopologyGraph).exists()).toBe(true)
  })

  it('切筛选时把 loading 透传给关系图（旧图停留有反馈）', async () => {
    wrapper = mountView()
    await flushPromises()
    expect(wrapper.findComponent(TopologyGraph).props('loading')).toBe(false)

    let resolveGraph
    getTopologyGraph.mockReturnValue(new Promise((r) => { resolveGraph = r }))
    const selects = wrapper.findAllComponents({ name: 'ElSelect' })
    await selects[0].vm.$emit('change')
    await flushPromises()

    // 旧图还在（edges 未清空），但 loading 已置起 → 图上盖遮罩，不会读成"点了没反应"
    expect(wrapper.findComponent(TopologyGraph).props('loading')).toBe(true)

    resolveGraph(GRAPH)
    await flushPromises()
    expect(wrapper.findComponent(TopologyGraph).props('loading')).toBe(false)
  })
})
