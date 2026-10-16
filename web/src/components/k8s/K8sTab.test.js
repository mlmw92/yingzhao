// 「中间件监控 → Kubernetes」Tab 的**职责边界**回归测试（2026-10-10 重规划）。
//
// 这一页只做指标展示：集群 / 节点 / Pod 的聚合计数与用量。
// 对象级明细（异常 Pod 的 Pod 名、命名空间、原因）属于「容器与工作负载」那个信息面，
// 刻意不在这里——但"移除"必须让人知道明细去哪了，所以还得有跳转入口。
//
// 这类缺陷不报错、不白屏：只是"某条信息从此无处可查"，或者"点了没反应"。
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import http from '../../api/http'
import K8sTab from './K8sTab.vue'

vi.mock('../../api/http', () => ({
  default: { get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() },
}))
const routerPush = vi.fn()
vi.mock('vue-router', () => ({
  useRouter: () => ({ push: routerPush }),
  useRoute: () => ({ query: {} }),
}))

// jsdom 未实现 ResizeObserver，Element Plus 的表格会用到它。
if (!globalThis.ResizeObserver) {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
}

const PAYLOAD = {
  clusters: [{
    node: 'web-01', instance: 'https://10.0.0.9:6443', name: 'k3s-dev', version: 'v1.29.0',
    up: true, nodesReady: 1, nodesTotal: 1, podsRunning: 3, podsTotal: 4, podsAbnormal: 1,
    deploymentsUnhealthy: 0,
  }],
  nodes: [{
    nodeName: 'vm-0-10-ubuntu', ip: '10.0.0.9', cluster: 'k3s-dev', role: 'control-plane',
    ready: true, cpuCores: 0.12, memBytes: 2.4e9,
  }],
  pods: [{ pod: 'crash-demo', namespace: 'default', cluster: 'k3s-dev', status: 'RunContainerError', phase: 'Running' }],
}

let wrappers = []
function mountTab() {
  const w = mount(K8sTab, { global: { plugins: [ElementPlus] } })
  wrappers.push(w)
  return w
}

beforeEach(() => {
  vi.clearAllMocks()
  wrappers = []
  http.get.mockResolvedValue(PAYLOAD)
})
afterEach(() => {
  for (const w of wrappers) w.unmount()
})

describe('K8sTab 中间件监控的 Kubernetes Tab', () => {
  it('只呈现聚合计数与用量，不出现异常 Pod 明细', async () => {
    const w = mountTab()
    await flushPromises()

    // 计数在（它是指标：回答"有多少"）
    expect(w.text()).toContain('异常 Pod')
    expect(w.text()).toContain('节点列表')
    // 明细不在（Pod 名与状态是对象标识，属于管理面：回答"是哪个"）
    expect(w.text()).not.toContain('crash-demo')
    expect(w.text()).not.toContain('RunContainerError')
    // 但必须说明明细去哪了，否则用户会以为"功能没了"
    expect(w.text()).toContain('容器与工作负载')
  })

  it('页头跳转入口把用户带到容器页的 Kubernetes 平台', async () => {
    const w = mountTab()
    await flushPromises()

    const btn = w.findAll('button').find((b) => b.text().includes('容器与工作负载'))
    expect(btn).toBeTruthy()
    await btn.trigger('click')
    expect(routerPush).toHaveBeenCalledWith({ path: '/container', query: { platform: 'k8s' } })
  })

  it('异常 Pod 计数卡可点：计数留在这里，明细去管理面', async () => {
    const w = mountTab()
    await flushPromises()

    const card = w.find('.kpi-clickable')
    expect(card.exists()).toBe(true)
    await card.trigger('click')
    expect(routerPush).toHaveBeenCalledWith({ path: '/container', query: { platform: 'k8s' } })
  })
})
