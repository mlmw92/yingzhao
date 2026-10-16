// 「容器与工作负载」页的**数据通路**回归测试。
//
// 这个文件在 2026-10-10 重规划（ADR-0006）后重写过：这一页的读从「Agent 异步下行」
// （下发指令 → 等 Agent 下一轮上报 → 回执，通常 15 秒以上）改成了 **Server 直连**
// （k8sproxy / dockerproxy，同步返回）。
//
// 因此这里钉的东西也换了：
//  1. **读不经过下行动作通道**（createOpsTask 一次都不该被调用）——这是本次改动最核心的一条，
//     退化成"又走回上报"不会报错、只会让页面莫名其妙地慢十几秒；
//  2. 切集群 / 切 Tab / 点查询都是**一次同步 GET**，不再有"有缓存就不查"那套；
//  3. 写操作（K8s 的 restart/scale/delete）**仍然**走下行通道，且成功后必须重拉列表；
//  4. Docker 侧同样是直连（含写操作）。
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import http from '../../api/http'
import { createOpsTask, getOpsTask } from '../../api/ops'
import { lookupAsset } from '../../api/asset'
import ContainerView from './ContainerView.vue'
import { resetContainerQueryCache } from './queryCache'

vi.mock('../../api/http', () => ({
  default: { get: vi.fn(), post: vi.fn(), put: vi.fn(), del: vi.fn(), delete: vi.fn() },
}))
vi.mock('../../api/ops', () => ({
  createOpsTask: vi.fn(),
  getOpsTask: vi.fn(),
}))
vi.mock('../../api/asset', () => ({
  lookupAsset: vi.fn(),
}))
// 跨页联动要用到路由：push 用于跳台账，replace 用于"只认领一次"地清掉深链参数。
const routerPush = vi.fn()
const routerReplace = vi.fn()
let routeQuery = {}
vi.mock('vue-router', () => ({
  useRouter: () => ({ push: routerPush, replace: routerReplace }),
  useRoute: () => ({ query: routeQuery }),
}))
// 权限表要能逐例改：无权时按钮必须**不出现**，而不是点了撞 403。
const authState = vi.hoisted(() => ({ perms: ['logs:read'] }))
vi.mock('../../composables/useAuth', () => ({
  default: () => ({ can: (p) => authState.perms.includes(p) }),
}))

// jsdom 未实现 ResizeObserver，Element Plus 的表格会用到它。
if (!globalThis.ResizeObserver) {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
}

const CLUSTER_A = { node: 'web-01', instance: 'https://10.0.0.9:6443', name: 'k3s-dev', up: true }
const CLUSTER_B = { node: 'web-02', instance: 'https://10.0.0.10:6443', name: 'k8s-2', up: true }
// Server 侧 k8s-live.yaml 里配的集群名：与上报的 name 对得上才查得到东西。
const LIVE_CLUSTERS = ['k3s-dev', 'k8s-2']

const DOCKER_ENDPOINTS = [
  { node: 'web-01', name: 'local-docker', reachable: true, unixSocket: false },
  { node: 'db-01', name: 'db-docker', reachable: false, unixSocket: true },
]

const keyOf = (c) => c.node + '|' + c.instance

let wrappers = []
// liveCalls 记录每一次 K8s 直连查询（cluster + 能力），用来断言"什么时候查了"。
let liveCalls = []
// dockerCalls 记录每一次 Docker 直连请求的路径。
let dockerCalls = []
// payloadByCap 让用例按能力替换返回内容（默认是一份通用表）。
let payloadByCap = {}

function mountView() {
  const w = mount(ContainerView, { global: { plugins: [ElementPlus] }, attachTo: document.body })
  wrappers.push(w)
  return w
}

// settle 等三拍：loadClusters → loadLiveClusters → run（直连 GET）。
async function settle() {
  await flushPromises()
  await flushPromises()
  await flushPromises()
}

function defaultLivePayload(cap) {
  if (cap === 'logs') {
    return { kind: 'container.logs', columns: ['日志'], rows: [['log-line-1'], ['log-line-2']], total: 2 }
  }
  if (cap === 'object') {
    return { kind: 'container.describe', columns: ['字段', '值'], rows: [['状态', '运行中']], total: 1 }
  }
  if (cap === 'pods') {
    return {
      kind: 'container.pods',
      columns: ['命名空间', '名称', '状态', '重启', '节点', 'Pod IP'],
      rows: [['default', 'web-1', '运行中', '0', 'node-1', '10.1.2.3']],
      total: 1,
    }
  }
  return {
    kind: 'container.' + cap,
    columns: ['类型', '命名空间', '名称', '期望', '就绪', '可用', '状态'],
    rows: [['Deployment', 'default', 'web', '3', '3', '3', '就绪']],
    total: 1,
  }
}

function defaultDockerPayload(path) {
  if (path.includes('/containers/') && path.includes('/logs')) {
    return { kind: 'docker.logs', columns: ['日志'], rows: [['container-log-1']], total: 1 }
  }
  if (path.includes('/containers/')) {
    return { kind: 'docker.inspect', columns: ['项目', '值'], rows: [['状态', 'running']], total: 1 }
  }
  if (path.includes('/containers')) {
    return {
      kind: 'docker.containers',
      columns: ['名称', '状态', '镜像', '端口', '创建时间', 'ID'],
      rows: [['web-1', 'Up 2 hours', 'nginx:1.27', '80/tcp', '2026-10-01 10:00:00', 'aaaa11112222']],
      total: 1,
    }
  }
  if (path.includes('/images')) {
    return { kind: 'docker.images', columns: ['仓库标签', '镜像 ID', '大小', '创建时间'], rows: [['nginx:1.27', 'aaaa11112222', '1 MiB', '2026-10-01']], total: 1 }
  }
  return { kind: 'docker.info', columns: ['项目', '值'], rows: [['引擎版本', '27.0.0']], total: 1 }
}

function mockHttp() {
  http.get.mockImplementation(async (path) => {
    const p = String(path)
    if (p.includes('/container/k8s/clusters')) return { clusters: [CLUSTER_A, CLUSTER_B] }
    if (p.includes('/container/k8s/live-configs')) return { clusters: LIVE_CLUSTERS }
    if (p.includes('/container/k8s/live/')) {
      const m = p.match(/\/live\/([^/]+)\/([^?]+)/)
      const cluster = m ? decodeURIComponent(m[1]) : ''
      const cap = m ? m[2] : ''
      liveCalls.push({ cluster, cap })
      return payloadByCap[cap] || defaultLivePayload(cap)
    }
    if (p.includes('/container/docker/endpoints')) return { endpoints: DOCKER_ENDPOINTS }
    if (p.includes('/container/docker/')) {
      dockerCalls.push(p)
      return defaultDockerPayload(p)
    }
    return {}
  })
}

const liveCaps = () => liveCalls.map((c) => c.cap)
const dockerWrites = () => ({
  posts: http.post.mock.calls.map((c) => String(c[0])),
  dels: http.del.mock.calls.map((c) => String(c[0])),
})

beforeEach(() => {
  vi.clearAllMocks()
  resetContainerQueryCache()
  wrappers = []
  routeQuery = {}
  liveCalls = []
  dockerCalls = []
  payloadByCap = {}
  authState.perms = ['logs:read']
  mockHttp()
  // K8s 写操作仍走下行通道：默认立即成功。
  createOpsTask.mockImplementation(async () => ({ task: { id: 't1', state: 'queued' } }))
  getOpsTask.mockResolvedValue({ task: { id: 't1', state: 'succeeded', json: '{}' } })
})

afterEach(() => {
  for (const w of wrappers) w.unmount()
  // ElMessageBox 会往 body 挂节点且**不随组件卸载移除**：不清掉会污染后续用例——
  // 它的弹窗是单例，上一用例没关掉的框会让下一个用例的 confirm 拿不到自己的 promise。
  for (const el of document.querySelectorAll('.el-overlay, .el-message-box__wrapper')) el.remove()
})

// 行内按钮**必须**收敛到真实表格行（`.el-table__row`）：Element Plus 会把列的插槽
// 在 `.hidden-columns` 里用 `row=undefined` 再渲染一次（用于测列宽），那里也会产出按钮节点。
const rowButtons = (w) => w.findAll('.el-table__row button')
const rowLabels = (w) => new Set(rowButtons(w).map((b) => b.text()))

// confirmBox 点掉**最新**那个确认框的按钮。
// 必须取最后一个：ElMessageBox 是单例、弹窗挂在 body 上且不随组件卸载移除，
// 上一个用例残留的框会让这里点到"别人的按钮"，于是本用例的 confirm promise 永远不 resolve。
function confirmBox(label) {
  const boxes = [...document.body.querySelectorAll('.el-message-box')]
  const box = boxes[boxes.length - 1]
  const btn = box && [...box.querySelectorAll('button')].find((b) => b.textContent.includes(label))
  expect(btn, '确认框应已出现（label=' + label + '）').toBeTruthy()
  btn.click()
}

const lastCallOf = (kind) => createOpsTask.mock.calls.map((c) => c[0]).find((c) => c.kind === kind)

async function clickTab(w, index) {
  await w.findAll('.el-tabs__item')[index].trigger('click')
  await settle()
}

async function switchCluster(w, clusterKey) {
  const select = w.findComponent({ name: 'ElSelect' })
  select.vm.$emit('update:modelValue', clusterKey)
  select.vm.$emit('change', clusterKey)
  await settle()
}

describe('ContainerView · K8s 读走 Server 直连', () => {
  it('首次进入直连查询一次，结果直接落在表里（不再有「上次刷新」时间）', async () => {
    const w = mountView()
    await settle()

    expect(liveCalls).toEqual([{ cluster: 'k3s-dev', cap: 'workloads' }])
    expect(w.text()).toContain('web')
    // 2026-10-10 起去掉这行时间戳：直连点开即得，它只剩噪音。钉住它别再回来。
    expect(w.text()).not.toContain('上次刷新')
  })

  it('平台开关独占一行放在内容区顶部（不再缩在页头右侧与「选择集群」同级）', async () => {
    authState.perms = ['logs:read', 'docker:read']
    const w = mountView()
    await settle()

    const bar = w.find('.platform-bar')
    expect(bar.exists()).toBe(true)
    expect(bar.text()).toContain('平台')
    expect(bar.find('.platform-switch').exists()).toBe(true)
    // 页头里不该再有它：放回去就等于又变回那枚 12px / 22px 高、与旁边「选择集群」
    // 完全同级的小控件——用户 2026-10-10 的反馈正是"不明显"。
    expect(w.find('.page-header .platform-switch').exists()).toBe(false)
  })

  it('读**不经过**下行动作通道（本次改动最核心的一条）', async () => {
    const w = mountView()
    await settle()
    await clickTab(w, 1) // Pod
    await clickTab(w, 2) // 事件

    // 读全走直连：一条下行动作都不该被创建。
    // 退化成"又走回上报"不会报错，只会让页面莫名其妙地慢十几秒——所以必须钉住。
    expect(createOpsTask).not.toHaveBeenCalled()
    expect(liveCaps()).toEqual(['workloads', 'pods', 'events'])
  })

  it('切 Tab 直接重查：不再有"有缓存就不查"那套', async () => {
    const w = mountView()
    await settle()
    expect(liveCaps()).toEqual(['workloads'])

    await clickTab(w, 1) // Pod
    expect(liveCaps()).toEqual(['workloads', 'pods'])

    // 切回工作负载：直连是即时的，缓存旧结果只会让人以为"看到的是最新的"。
    await clickTab(w, 0)
    expect(liveCaps()).toEqual(['workloads', 'pods', 'workloads'])
  })

  it('换集群直接重查，且用上报里的集群名作为直连标识', async () => {
    const w = mountView()
    await settle()

    await switchCluster(w, keyOf(CLUSTER_B))
    expect(liveCalls).toEqual([
      { cluster: 'k3s-dev', cap: 'workloads' },
      { cluster: 'k8s-2', cap: 'workloads' },
    ])
  })

  it('集群不在 k8s-live.yaml 里时先给出提示，而不是让人对着 502 猜', async () => {
    // 上报里有这个集群，Server 侧却没配：直连查询会失败，界面要先把原因说出来。
    http.get.mockImplementation(async (path) => {
      const p = String(path)
      if (p.includes('/container/k8s/clusters')) return { clusters: [CLUSTER_A] }
      if (p.includes('/container/k8s/live-configs')) return { clusters: ['另一个集群'] }
      if (p.includes('/container/k8s/live/')) return defaultLivePayload('workloads')
      return {}
    })

    const w = mountView()
    await settle()

    expect(w.text()).toContain('没有直连凭据')
    expect(w.text()).toContain('k8s-live.yaml')
  })

  it('命名空间随查询一起下发，且改条件不会偷偷改表里的数据', async () => {
    const w = mountView()
    await settle()
    expect(liveCalls.length).toBe(1)

    // 输入命名空间后不自动查（边打字边查既吵又费），要点「查询」
    const input = w.find('.ns-input input')
    await input.setValue('kube-system')
    await settle()
    expect(liveCalls.length).toBe(1)

    const queryBtn = w.findAll('button').find((b) => b.text() === '查询')
    await queryBtn.trigger('click')
    await settle()
    expect(liveCalls.length).toBe(2)
  })
})

describe('ContainerView · 详情与日志抽屉', () => {
  it('Pod 行的「日志」直连 logs 并按原样展示日志行', async () => {
    const w = mountView()
    await settle()
    await clickTab(w, 1) // Pod

    const btn = rowButtons(w).find((b) => b.text() === '日志')
    expect(btn).toBeTruthy()
    await btn.trigger('click')
    await settle()

    expect(liveCaps()).toContain('logs')
    expect(w.text()).toContain('log-line-1')
  })

  it('点行打开详情：直连 object 并按字段/值两列展示', async () => {
    const w = mountView()
    await settle()

    await w.findAll('.el-table__row')[0].trigger('click')
    await settle()

    expect(liveCaps()).toContain('object')
    // 状态是服务端翻好的中文（k8sproxy.zhPodStatus）：判定值保持英文，展示层不出英文。
    expect(w.text()).toContain('运行中')
  })
})

describe('ContainerView · 跨页联动与权限门', () => {
  it('Pod 行「检索日志」跳到集中日志并按容器身份收窄', async () => {
    authState.perms = ['logs:read']
    const w = mountView()
    await settle()
    await clickTab(w, 1) // Pod

    const btn = rowButtons(w).find((b) => b.text() === '检索日志')
    expect(btn).toBeTruthy()
    await btn.trigger('click')
    await settle()

    expect(routerPush).toHaveBeenCalled()
    const target = routerPush.mock.calls[routerPush.mock.calls.length - 1][0]
    expect(target.path).toBe('/logs')
    expect(String(target.query.pods)).toContain('default|web-1')
  })

  it('没有 logs:read 时不出现集中日志入口（按需拉取仍可用）', async () => {
    authState.perms = []
    const w = mountView()
    await settle()
    await clickTab(w, 1) // Pod

    const labels = rowLabels(w)
    expect(labels.has('检索日志')).toBe(false)
    expect(labels.has('日志')).toBe(true)
  })

  it('Pod 行「台账」命中后跳到资产详情', async () => {
    lookupAsset.mockResolvedValue({ ok: true, status: 200, body: { asset: { id: 'asset-1' } } })
    const w = mountView()
    await settle()
    await clickTab(w, 1) // Pod

    const btn = rowButtons(w).find((b) => b.text() === '台账')
    expect(btn).toBeTruthy()
    await btn.trigger('click')
    await settle()

    expect(routerPush).toHaveBeenCalledWith({ path: '/assets', query: { id: 'asset-1' } })
  })

  it('带 ?cluster/namespace/pod 进入时直接定位并打开日志抽屉', async () => {
    routeQuery = { cluster: 'k3s-dev', namespace: 'default', pod: 'web-1' }
    const w = mountView()
    await settle()

    // 深链参数只认一次：消费后清掉，否则用户手动切集群又被拽回来
    expect(routerReplace).toHaveBeenCalledWith({ path: '/container', query: {} })
    expect(liveCaps()).toContain('logs')
  })

  it('没有 docker:read 时不出现平台切换开关', async () => {
    const w = mountView()
    await settle()
    expect(w.findAll('.el-radio-button').some((r) => r.text().includes('Docker'))).toBe(false)
  })
})

describe('ContainerView · K8s 排障写操作（仍走下行通道）', () => {
  it('没有 container:write 时不出现写按钮，只读入口仍在', async () => {
    const w = mountView()
    await settle()

    const labels = rowLabels(w)
    expect(labels.has('重启')).toBe(false)
    expect(labels.has('扩缩容')).toBe(false)
    expect(labels.has('详情')).toBe(true)
  })

  it('超管点「重启」：先确认再下发 container.restart，并在成功后重拉列表', async () => {
    authState.perms = ['logs:read', 'container:write']
    const w = mountView()
    await settle()

    const before = liveCalls.length
    const btn = rowButtons(w).find((b) => b.text() === '重启')
    expect(btn).toBeTruthy()
    await btn.trigger('click')
    await settle()

    // 二次确认：此刻还没有下发
    expect(lastCallOf('container.restart')).toBeFalsy()
    confirmBox('执行')
    await settle()

    const call = lastCallOf('container.restart')
    expect(call).toBeTruthy()
    expect(call.node).toBe(CLUSTER_A.node)
    expect(call.params).toMatchObject({
      cluster: CLUSTER_A.name, namespace: 'default', kind: 'Deployment', name: 'web',
    })
    // 写操作改变了集群状态 → 必须重新拉一次列表（读仍然是直连）
    expect(liveCalls.length).toBeGreaterThan(before)
  })

  it('Job 行不给「重启」与「扩缩容」（跑完就该退出，没有可重启的语义）', async () => {
    authState.perms = ['logs:read', 'container:write']
    payloadByCap.workloads = {
      kind: 'container.workloads',
      columns: ['类型', '命名空间', '名称', '期望', '就绪', '可用', '状态'],
      rows: [['Job', 'default', 'batch', '—', '1', '成功 1 / 失败 0', '已完成']],
      total: 1,
    }
    const w = mountView()
    await settle()

    const labels = rowLabels(w)
    expect(labels.has('重启')).toBe(false)
    expect(labels.has('扩缩容')).toBe(false)
  })

  it('DaemonSet 行给「重启」但不给「扩缩容」（副本数由节点数决定）', async () => {
    authState.perms = ['logs:read', 'container:write']
    payloadByCap.workloads = {
      kind: 'container.workloads',
      columns: ['类型', '命名空间', '名称', '期望', '就绪', '可用', '状态'],
      rows: [['DaemonSet', 'default', 'agent', '3', '3', '3', '就绪']],
      total: 1,
    }
    const w = mountView()
    await settle()

    const labels = rowLabels(w)
    expect(labels.has('重启')).toBe(true)
    expect(labels.has('扩缩容')).toBe(false)
  })

  it('Pod Tab 的「删除」下发 container.delete', async () => {
    authState.perms = ['logs:read', 'container:write']
    const w = mountView()
    await settle()
    await clickTab(w, 1) // Pod

    const btn = rowButtons(w).find((b) => b.text() === '删除')
    expect(btn).toBeTruthy()
    await btn.trigger('click')
    await settle()
    confirmBox('执行')
    await settle()

    const call = lastCallOf('container.delete')
    expect(call).toBeTruthy()
    expect(call.params).toMatchObject({ cluster: CLUSTER_A.name, namespace: 'default', name: 'web-1' })
  })
})

describe('ContainerView · Docker 平台（同样是直连）', () => {
  async function enterDocker(w) {
    const dockerBtn = w.findAll('.el-radio-button').find((r) => r.text().includes('Docker'))
    expect(dockerBtn).toBeTruthy()
    await dockerBtn.trigger('click')
    await settle()
  }

  it('切到 Docker：拉端点并直连查一次引擎概览（不经过下行通道）', async () => {
    authState.perms = ['logs:read', 'docker:read']
    const w = mountView()
    await settle()
    await enterDocker(w)

    expect(dockerCalls.some((p) => p.includes('/container/docker/info'))).toBe(true)
    // 端点定位是 (node, name) 二元组：只按 name 会命中同名的那台别的机器
    expect(dockerCalls[0]).toContain('node=web-01')
    expect(dockerCalls[0]).toContain('name=local-docker')
    expect(w.text()).toContain('27.0.0')
    expect(createOpsTask).not.toHaveBeenCalled()
  })

  it('容器 Tab：直连列表，且「详情」直连单个容器', async () => {
    authState.perms = ['logs:read', 'docker:read']
    const w = mountView()
    await settle()
    await enterDocker(w)

    await clickTab(w, 1) // 容器
    expect(dockerCalls.some((p) => p.includes('/containers') && p.includes('all=true'))).toBe(true)
    expect(w.text()).toContain('web-1')

    const btn = rowButtons(w).find((b) => b.text() === '详情')
    expect(btn).toBeTruthy()
    await btn.trigger('click')
    await settle()
    expect(dockerCalls.some((p) => /\/containers\/web-1(\?|$)/.test(p))).toBe(true)
  })

  it('没有 docker:write 时不出现写按钮，只读入口仍在', async () => {
    authState.perms = ['logs:read', 'docker:read']
    const w = mountView()
    await settle()
    await enterDocker(w)
    await clickTab(w, 1) // 容器

    const labels = rowLabels(w)
    expect(labels.has('停止')).toBe(false)
    expect(labels.has('删除')).toBe(false)
    expect(labels.has('详情')).toBe(true)
  })

  it('超管点「停止」：先二次确认再直连 POST，成功后重新拉列表', async () => {
    authState.perms = ['logs:read', 'docker:read', 'docker:write']
    const w = mountView()
    await settle()
    await enterDocker(w)
    await clickTab(w, 1) // 容器

    const before = dockerCalls.length
    const btn = rowButtons(w).find((b) => b.text() === '停止')
    expect(btn).toBeTruthy()
    await btn.trigger('click')
    await settle()

    // 二次确认：此刻还没有发请求
    expect(dockerWrites().posts.length).toBe(0)
    confirmBox('停止')
    await settle()

    const posts = dockerWrites().posts
    expect(posts.length).toBe(1)
    expect(posts[0]).toContain('/containers/web-1/stop')
    // 写操作改变了运行态 → 必须重新拉列表
    expect(dockerCalls.length).toBeGreaterThan(before)
  })

  it('运行中的容器只给「停止 / 重启」，不给「启动」', async () => {
    authState.perms = ['logs:read', 'docker:read', 'docker:write']
    const w = mountView()
    await settle()
    await enterDocker(w)
    await clickTab(w, 1)

    const labels = rowLabels(w)
    expect(labels.has('停止')).toBe(true)
    expect(labels.has('重启')).toBe(true)
    expect(labels.has('启动')).toBe(false)
  })

  it('已停止的容器只给「启动」，不给「停止 / 重启」', async () => {
    authState.perms = ['logs:read', 'docker:read', 'docker:write']
    http.get.mockImplementation(async (path) => {
      const p = String(path)
      if (p.includes('/container/k8s/clusters')) return { clusters: [CLUSTER_A] }
      if (p.includes('/container/k8s/live-configs')) return { clusters: LIVE_CLUSTERS }
      if (p.includes('/container/k8s/live/')) return defaultLivePayload('workloads')
      if (p.includes('/container/docker/endpoints')) return { endpoints: DOCKER_ENDPOINTS }
      if (p.includes('/container/docker/containers')) {
        return {
          kind: 'docker.containers',
          columns: ['名称', '状态', '镜像', '端口', '创建时间', 'ID'],
          rows: [['web-1', 'Exited (0) 2 hours ago', 'nginx:1.27', '—', '2026-10-01 10:00:00', 'aaaa11112222']],
          total: 1,
        }
      }
      return {}
    })

    const w = mountView()
    await settle()
    await enterDocker(w)
    await clickTab(w, 1)

    const labels = rowLabels(w)
    expect(labels.has('启动')).toBe(true)
    expect(labels.has('停止')).toBe(false)
    expect(labels.has('重启')).toBe(false)
  })

  it('unix socket 且不可达的端点给出可执行的提示（不是一句"失败了"）', async () => {
    authState.perms = ['logs:read', 'docker:read']
    const w = mountView()
    await settle()
    await enterDocker(w)

    // 切到第二个端点（db-docker：unix socket、不可达）
    const select = w.findComponent({ name: 'ElSelect' })
    select.vm.$emit('update:modelValue', 'db-01|db-docker')
    select.vm.$emit('change', 'db-01|db-docker')
    await settle()

    expect(w.text()).toContain('unix socket')
    expect(w.text()).toContain('tcp://')
  })
})
