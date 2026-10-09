// 侧栏高亮：按**精确路径**判定。
// 缺陷现场：菜单用 startsWith 匹配，于是进「关系视图」（/assets/topology）时
// 「资产台账」（/assets）也一起高亮，看起来像同时选中了两个菜单。
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import http from '../api/http'

const routeState = vi.hoisted(() => ({ path: '/' }))
vi.mock('vue-router', () => ({ useRoute: () => routeState }))
vi.mock('../api/http', () => ({ default: { get: vi.fn() } }))

import Sidebar from './Sidebar.vue'

const SlotStub = { template: '<span><slot /></span>' }

function mountSidebar() {
  return mount(Sidebar, {
    props: { collapsed: false, alertCount: 0, username: 'admin', roleLabels: [] },
    global: {
      stubs: {
        'router-link': { template: '<a><slot /></a>' },
        'el-tooltip': SlotStub,
        'el-icon': SlotStub,
        'el-badge': SlotStub,
        'el-button': SlotStub,
      },
    },
  })
}

describe('侧栏菜单高亮', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    http.get.mockResolvedValue({ server: '1.30.63', buildTime: '' })
  })

  it('在 /assets/topology 只高亮「关系视图」', async () => {
    routeState.path = '/assets/topology'
    const w = mountSidebar()
    await flushPromises()

    const active = w.findAll('.nav-subitem.active')
    expect(active).toHaveLength(1)
    expect(active[0].text()).toContain('关系视图')
    w.unmount()
  })

  it('在 /assets 只高亮「资产台账」', async () => {
    routeState.path = '/assets'
    const w = mountSidebar()
    await flushPromises()

    const active = w.findAll('.nav-subitem.active')
    expect(active).toHaveLength(1)
    expect(active[0].text()).toContain('资产台账')
    w.unmount()
  })

  it('在 /assets/model 只高亮「配置项模型」', async () => {
    routeState.path = '/assets/model'
    const w = mountSidebar()
    await flushPromises()

    const active = w.findAll('.nav-subitem.active')
    expect(active).toHaveLength(1)
    expect(active[0].text()).toContain('配置项模型')
    w.unmount()
  })
})
