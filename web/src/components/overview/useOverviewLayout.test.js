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
})
