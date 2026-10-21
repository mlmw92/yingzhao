// PS-29 命令面板：语义化 / 焦点管理 / 命令条目 / 模糊匹配 / 最近访问 / 主机加载态。
// 这些点此前全是缺陷：无 aria-label、列表无 listbox/option 语义、无最近访问、
// 主机拉取失败后 hostsLoaded 被置 true 导致本次会话不再重试且无提示。
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

const routerState = vi.hoisted(() => ({
  routes: [
    {
      path: '/',
      children: [
        { path: '', name: 'overview', meta: {} },
        { path: 'hosts', name: 'hosts', meta: {} },
        { path: 'alerts', name: 'alerts', meta: {} },
        // 需要路径参数的路由不应出现在面板里
        { path: 'node/:name', name: 'node', meta: {} },
      ],
    },
  ],
  push: vi.fn(),
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ options: { routes: routerState.routes }, push: routerState.push }),
}))
vi.mock('../api/http', () => ({ default: { get: vi.fn() } }))
vi.mock('../composables/useAuth', () => ({ useAuth: () => ({ can: () => true }) }))

import http from '../api/http'
import CommandPalette from './CommandPalette.vue'

const SlotStub = { template: '<span><slot /></span>' }

function mountPalette() {
  return mount(CommandPalette, {
    global: {
      stubs: { teleport: true, 'el-icon': SlotStub },
    },
  })
}

async function openPalette(w) {
  w.vm.open()
  await flushPromises()
}

describe('命令面板', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    http.get.mockResolvedValue({ nodes: [{ hostname: 'web-01', group: 'web' }] })
  })
  afterEach(() => {
    localStorage.clear()
  })

  it('打开后具备对话框与列表框语义', async () => {
    const w = mountPalette()
    await openPalette(w)

    const dialog = w.find('.cmdk')
    expect(dialog.attributes('role')).toBe('dialog')
    expect(dialog.attributes('aria-modal')).toBe('true')
    expect(dialog.attributes('aria-label')).toBe('命令面板')

    const list = w.find('#cmdk-listbox')
    expect(list.attributes('role')).toBe('listbox')

    const opts = w.findAll('[role="option"]')
    expect(opts.length).toBeGreaterThan(0)
    // 高亮项通过 aria-activedescendant 指到具体 option id
    const input = w.find('.cmdk-input')
    const ad = input.attributes('aria-activedescendant')
    expect(ad).toBe('cmdk-opt-0')
    expect(w.find(`#${ad}`).attributes('aria-selected')).toBe('true')
    w.unmount()
  })

  it('命令分组含主题切换与刷新命令，且不含带参路由', async () => {
    const w = mountPalette()
    await openPalette(w)

    const text = w.text()
    expect(text).toContain('命令')
    expect(text).toContain('切换主题：极光蓝')
    expect(text).toContain('刷新当前页数据')
    // node/:name 不能直达
    expect(text).not.toContain('主机详情')
    w.unmount()
  })

  it('支持子序列模糊匹配（无需连续字符）', async () => {
    const w = mountPalette()
    await openPalette(w)

    await w.find('.cmdk-input').setValue('切极')
    const labels = w.findAll('[role="option"]').map((o) => o.text())
    expect(labels.join('|')).toContain('切换主题：极光蓝')
    w.unmount()
  })

  it('选中后写入最近访问，重新打开出现「最近访问」分组', async () => {
    const w = mountPalette()
    await openPalette(w)

    // 空查询下第一个分组是「命令」，首项即切换主题
    await w.find('[role="option"]').trigger('click')
    expect(localStorage.getItem('yingzhao-cmdk-recent')).toContain('cmd:theme:b')

    await openPalette(w)
    expect(w.text()).toContain('最近访问')
    w.unmount()
  })

  it('主机加载失败时提示并可重试（不再一次性放弃）', async () => {
    http.get.mockRejectedValueOnce(new Error('服务不可用'))
    const w = mountPalette()
    await openPalette(w)

    expect(w.find('.cmdk-status.err').exists()).toBe(true)
    expect(w.text()).toContain('主机列表加载失败')

    // 点重试 → 第二次成功
    http.get.mockResolvedValueOnce({ nodes: [{ hostname: 'web-01', group: 'web' }] })
    await w.find('.cmdk-retry').trigger('click')
    await flushPromises()

    expect(w.find('.cmdk-status.err').exists()).toBe(false)
    expect(w.text()).toContain('web-01')
    w.unmount()
  })

  it('方向键 / Home / End 可移动高亮', async () => {
    const w = mountPalette()
    await openPalette(w)

    const activeId = () => w.find('.cmdk-input').attributes('aria-activedescendant')

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown' }))
    await flushPromises()
    expect(activeId()).toBe('cmdk-opt-1')

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'End' }))
    await flushPromises()
    const opts = w.findAll('[role="option"]')
    expect(activeId()).toBe(`cmdk-opt-${opts.length - 1}`)

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Home' }))
    await flushPromises()
    expect(activeId()).toBe('cmdk-opt-0')
    w.unmount()
  })
})
