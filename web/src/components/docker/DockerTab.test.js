// 「中间件监控 → Docker」Tab 的**职责边界**回归测试（2026-10-10 重规划）。
//
// 这一页只做指标展示：按主机的聚合计数（容器数 / 镜像数 / 在线）。
// 容器级明细（容器名、镜像、端口、资源占用）与启停删除等操作属于「容器与工作负载」那个信息面，
// 刻意不在这里——但"移除"必须让人知道明细去哪了，所以还得有跳转入口。
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import http from '../../api/http'
import DockerTab from './DockerTab.vue'

vi.mock('../../api/http', () => ({
  default: { get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() },
}))
const routerPush = vi.fn()
vi.mock('vue-router', () => ({
  useRouter: () => ({ push: routerPush }),
  useRoute: () => ({ query: {} }),
}))

if (!globalThis.ResizeObserver) {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
}

const PAYLOAD = {
  hosts: [{
    node: 'web-01', ip: '10.0.0.9', up: true,
    containersTotal: 3, containersRunning: 2, containersStopped: 1, imagesTotal: 5,
  }],
  containers: [{
    node: 'web-01', name: 'web-1', image: 'nginx:1.27', status: 'running',
    cpuPercent: 1.2, memUsage: 1024, memPercent: 3,
  }],
}

let wrappers = []
function mountTab() {
  const w = mount(DockerTab, { global: { plugins: [ElementPlus] } })
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

describe('DockerTab 中间件监控的 Docker Tab', () => {
  it('只呈现按主机的聚合计数，不出现容器明细', async () => {
    const w = mountTab()
    await flushPromises()

    // 聚合在（回答"有多少"）
    expect(w.text()).toContain('容器总数')
    expect(w.text()).toContain('实例拓扑')
    // 明细不在（容器名与镜像是对象标识，属于管理面：回答"是哪个"）
    expect(w.text()).not.toContain('web-1')
    expect(w.text()).not.toContain('nginx:1.27')
    // 但必须说明明细去哪了
    expect(w.text()).toContain('容器与工作负载')
  })

  it('跳转入口把用户带到容器页的 Docker 平台', async () => {
    const w = mountTab()
    await flushPromises()

    const btn = w.findAll('button').find((b) => b.text().includes('容器与工作负载'))
    expect(btn).toBeTruthy()
    await btn.trigger('click')
    expect(routerPush).toHaveBeenCalledWith({ path: '/container', query: { platform: 'docker' } })
  })
})
