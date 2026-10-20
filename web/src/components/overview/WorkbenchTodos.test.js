import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import http from '../../api/http'
import WorkbenchTodos from './WorkbenchTodos.vue'

vi.mock('../../api/http', () => ({
  default: { get: vi.fn() },
}))

beforeEach(() => {
  vi.clearAllMocks()
})

const global = {
  plugins: [ElementPlus],
  stubs: { 'router-link': { template: '<a><slot /></a>' } },
}

describe('WorkbenchTodos', () => {
  it('展示待确认告警数量，无权限的段显示无权限而不是 0', async () => {
    http.get.mockResolvedValue({
      todos: [
        { kind: 'alert', available: true, count: 7, items: [{ id: 'e1', title: 'CPU 高', node: 'web-01' }] },
        { kind: 'approval', available: false, count: 0, items: [] },
      ],
      errors: [],
    })
    const w = mount(WorkbenchTodos, { global })
    await flushPromises()
    expect(w.text()).toContain('待确认告警')
    expect(w.text()).toContain('7')
    expect(w.text()).toContain('无权限')
  })

  it('没有待办时显示明确空态', async () => {
    http.get.mockResolvedValue({
      todos: [
        { kind: 'alert', available: true, count: 0, items: [] },
        { kind: 'approval', available: true, count: 0, items: [] },
      ],
      errors: [],
    })
    const w = mount(WorkbenchTodos, { global })
    await flushPromises()
    expect(w.text()).toContain('当前没有需要你处理的事项')
  })
})
