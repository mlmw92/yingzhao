// RedisTab（批次 10 重写）冒烟测试。
//
// 重写把 .card/.glass 收敛到 SectionCard、空态收敛到 EmptyState、自绘 .dot 收敛到
// MwStatusDot、抽屉趋势图收敛到 TrendChart + mwTrendOption，并把趋势查询从串行改为并行。
// 这里只兜三类"静默坏掉"的回归：
//   1. 空状态引导（含可复制安装命令）真的渲染出来；
//   2. 拓扑按 group 真实聚合（集群/独立实例分组名出现在 DOM 里，不再全塞进一个组）；
//   3. 抽屉趋势查询的 step 必须是**毫秒**且 ≥1000 —— 后端 query.go 对 step<1000 直接 400，
//      一旦有人把 rangeToStep 的返回值当"秒"拼进 URL，所有趋势图会恒显"加载失败"。
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import http from '../../api/http'
import RedisTab from './RedisTab.vue'

vi.mock('../../api/http', () => ({ default: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))

// TrendChart 内部要建 ECharts 实例；jsdom 无真实画布尺寸，替换为桩，
// 只验证组件契约（option 透传、三态、销毁）。
const chartStub = vi.hoisted(() => {
  const inst = { setOption: vi.fn(), dispose: vi.fn(), resize: vi.fn(), isDisposed: vi.fn(() => false) }
  return { inst, initChart: vi.fn(() => inst) }
})
vi.mock('../../charts/echarts', () => ({
  initChart: chartStub.initChart,
  COLORS: { blue: '#3b82f6', green: '#22c55e', amber: '#f59e0b', red: '#ef4444', purple: '#a855f7', cyan: '#06b6d4' },
  tokenColor: () => '#9fb3c8',
  mwTrendOption: (series, opts) => ({ series: series || [], __opts: opts }),
}))

const MASTER = {
  node: 'n-standalone', nodeIp: '10.0.0.1', instance: '127.0.0.1:6379', name: 'redis-standalone',
  topology: 'standalone', role: 'master', up: true, version: '7.2.0', uptime: 3600,
  ops: 100, usedMemory: 1048576, memPercent: 20, clients: 5, hitRate: 95, keys: 100, fragmentation: 1.1,
}
const CLUSTER_MASTER = {
  node: 'n-cluster', nodeIp: '10.0.0.2', instance: '127.0.0.1:6380', name: 'redis-cluster', group: 'clusterA',
  topology: 'cluster', role: 'master', up: true, version: '7.2.0', uptime: 7200,
  ops: 200, usedMemory: 2097152, memPercent: 40, clients: 8, hitRate: 90, keys: 200,
}
const CLUSTER_SLAVE = {
  node: 'n-cluster', nodeIp: '10.0.0.2', instance: '127.0.0.1:6381', name: 'redis-cluster', group: 'clusterA',
  topology: 'cluster', role: 'slave', replicaOf: '127.0.0.1:6380', up: true, version: '7.2.0', uptime: 3600,
  ops: 50, usedMemory: 1048576, memPercent: 20, clients: 3, hitRate: 88, keys: 200,
}

function mockGet(instances) {
  http.get.mockImplementation((url) => {
    if (url.includes('/middleware/redis/instances')) return Promise.resolve({ instances })
    if (url.includes('/alerts')) return Promise.resolve({ alerts: [] })
    if (url.includes('/install-info')) return Promise.resolve({ serverURL: 'http://srv:8080' })
    if (url.includes('/query/range')) return Promise.resolve({ series: [{ points: [[1700000000000, 1]] }] })
    return Promise.resolve({})
  })
}

describe('RedisTab 重写后冒烟', () => {
  let wrapper

  beforeEach(() => {
    vi.clearAllMocks()
    Object.assign(navigator, { clipboard: { writeText: vi.fn(() => Promise.resolve()) } })
  })

  afterEach(() => {
    if (wrapper) wrapper.unmount()
    wrapper = undefined
    document.body.innerHTML = ''
  })

  async function mountTab(instances) {
    mockGet(instances)
    wrapper = mount(RedisTab, { global: { plugins: [ElementPlus] }, attachTo: document.body })
    await flushPromises()
    return wrapper
  }

  it('无实例时渲染空状态引导，并给出可复制的安装命令', async () => {
    await mountTab([])

    const text = wrapper.text()
    expect(text).toContain('尚未配置 Redis 监控')
    // 引导命令必须出现（否则用户不知道下一步做什么）
    expect(text).toContain('bash /etc/monitor-agent/agent-install.sh redis')

    const btn = wrapper.findAll('button').find((b) => b.text().includes('复制安装命令'))
    expect(btn).toBeDefined()
    await btn.trigger('click')
    await flushPromises()
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('bash /etc/monitor-agent/agent-install.sh redis')
  })

  it('按 group 真实聚合拓扑：集群分组名与独立实例同时出现', async () => {
    await mountTab([MASTER, CLUSTER_MASTER, CLUSTER_SLAVE])

    const text = wrapper.text()
    expect(text).toContain('集群 clusterA')      // 同 group 的两个 cluster 节点聚合到一张卡
    expect(text).toContain('独立实例')            // standalone 单独成组
    expect(text).toContain('实例拓扑')

    // 状态点统一走 MwStatusDot（不再有自绘 .dot）
    expect(wrapper.findAll('.mw-status').length).toBeGreaterThan(0)
    expect(wrapper.find('.dot').exists()).toBe(false)
  })

  it('抽屉趋势查询的 step 是毫秒且 ≥1000（否则后端 400，趋势图恒失败）', async () => {
    await mountTab([MASTER])

    // 点拓扑里的 standalone 节点打开抽屉
    const node = wrapper.find('.rel-node')
    expect(node.exists()).toBe(true)
    await node.trigger('click')
    await flushPromises()

    const trendUrls = http.get.mock.calls.map((c) => c[0]).filter((u) => u.includes('/query/range'))
    expect(trendUrls.length).toBeGreaterThan(0)

    for (const u of trendUrls) {
      const step = Number(new URL(u, 'http://x').searchParams.get('step'))
      expect(step, `step 必须是毫秒且 ≥1000：${u}`).toBeGreaterThanOrEqual(1000)
    }
    // master 实例：内存使用率 / 连接客户端数 / OPS / 命中率 / 键数量 / 碎片率 / 淘汰键数 / 运行时长 = 8 条
    expect(trendUrls).toHaveLength(8)
    expect(trendUrls.some((u) => u.includes('metric=redis_used_memory_percent'))).toBe(true)
    expect(trendUrls.every((u) => u.includes('labels.instance='))).toBe(true)
  })

  it('slave 实例额外查询复制延迟', async () => {
    await mountTab([CLUSTER_MASTER, CLUSTER_SLAVE])

    const slaveCard = wrapper.find('.ms-slave-card')
    expect(slaveCard.exists()).toBe(true)
    await slaveCard.trigger('click')
    await flushPromises()

    const trendUrls = http.get.mock.calls.map((c) => c[0]).filter((u) => u.includes('/query/range'))
    expect(trendUrls).toHaveLength(9) // 8 基础指标 + 复制延迟
    expect(trendUrls.some((u) => u.includes('metric=redis_replication_lag'))).toBe(true)
  })
})
