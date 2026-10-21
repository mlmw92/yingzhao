import { describe, it, expect, beforeEach } from 'vitest'
import { useOverviewLayout } from './useOverviewLayout'

beforeEach(() => localStorage.clear())

describe('首页区块默认布局', () => {
  it('待我处理固定置顶', () => {
    const { blocks, pinnedKeys } = useOverviewLayout()
    expect(blocks.value[0].key).toBe('todos')
    expect(pinnedKeys).toContain('todos')
  })

  it('智能运维降级到中间件概览之后', () => {
    const { blocks } = useOverviewLayout()
    const keys = blocks.value.map((b) => b.key)
    expect(keys.indexOf('intelligence')).toBeGreaterThan(keys.indexOf('middleware'))
  })

  it('升级前的本地布局也会把智能运维移到中间件之后', () => {
    // 旧版把智能运维固定在第一位，这是默认值残留、不是用户的选择
    localStorage.setItem('yingzhao-overview-layout', JSON.stringify([
      { key: 'intelligence', span: 12, visible: true },
      { key: 'health', span: 4, visible: true },
      { key: 'kpi', span: 4, visible: true },
      { key: 'criticalAlerts', span: 4, visible: true },
      { key: 'hostOverview', span: 12, visible: true },
      { key: 'middleware', span: 12, visible: true },
      { key: 'recentAlerts', span: 12, visible: true },
    ]))
    const { blocks } = useOverviewLayout()
    const keys = blocks.value.map((b) => b.key)
    expect(keys[0]).toBe('todos')
    expect(keys.indexOf('intelligence')).toBeGreaterThan(keys.indexOf('middleware'))
  })
})
