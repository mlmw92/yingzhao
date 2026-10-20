import { describe, it, expect, vi, beforeEach } from 'vitest'
import http from '../api/http'

vi.mock('../api/http', () => ({
  default: { get: vi.fn() },
}))

import { moduleGroups, pageDomain, isLocked } from './catalog'
import { useModules } from '../composables/useModules'

describe('模块目录与开关 store', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('目录包含六个域，页面能映射回所属域', () => {
    expect(moduleGroups.map((g) => g.key)).toEqual([
      'monitoring', 'assets', 'ops', 'alerting', 'security', 'system',
    ])
    expect(pageDomain['logs']).toBe('monitoring')
    expect(pageDomain['assets']).toBe('assets')
  })

  it('不可关闭项与设计稿一致', () => {
    expect(isLocked('overview')).toBe(true)
    expect(isLocked('alerts')).toBe(true)
    expect(isLocked('alerting')).toBe(true)
    expect(isLocked('system')).toBe(true)
    expect(isLocked('assets')).toBe(false)
  })

  it('关闭清单同时按页面与域生效', async () => {
    http.get.mockResolvedValue({ disabled: ['assets', 'logs'] })
    const m = useModules()
    await m.load(true)
    expect(m.isVisible('assets', 'assets')).toBe(false)
    expect(m.isVisible('logs', 'monitoring')).toBe(false)
    expect(m.isVisible('hosts', 'monitoring')).toBe(true)
  })
})
