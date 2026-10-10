// 告警事件筛选口径的单测。
//
// 深链「首页研判条 → 查看详情」带 ?node=&rule= 进来时，筛选必须真的生效。
// 这类失效是静默的：页面能打开、URL 也对，只是列表没过滤——用户只会觉得"找不到"。
import { describe, it, expect } from 'vitest'
import { filterAlerts } from './alertFilter'

const ALERTS = [
  { ruleName: 'CPU 使用率过高', node: 'web-01', state: 'firing' },
  { ruleName: 'CPU 使用率过高', node: 'web-02', state: 'firing' },
  { ruleName: '磁盘空间不足', node: 'web-01', state: 'firing' },
  { ruleName: '磁盘空间不足', node: 'web-01', state: 'resolved' },
]

describe('filterAlerts', () => {
  it('无条件时返回全部', () => {
    expect(filterAlerts({ alerts: ALERTS })).toHaveLength(4)
  })

  it('按 state 过滤（与 eventFilter 同口径）', () => {
    expect(filterAlerts({ alerts: ALERTS, state: 'resolved' })).toHaveLength(1)
  })

  it('按 node 精确过滤', () => {
    const r = filterAlerts({ alerts: ALERTS, node: 'web-02' })
    expect(r).toHaveLength(1)
    expect(r[0].node).toBe('web-02')
  })

  it('按 rule 精确过滤', () => {
    const r = filterAlerts({ alerts: ALERTS, rule: '磁盘空间不足' })
    expect(r).toHaveLength(2)
  })

  it('node + rule + state 组合取交集（深链场景：锁定那一条）', () => {
    const r = filterAlerts({ alerts: ALERTS, node: 'web-01', rule: '磁盘空间不足', state: 'firing' })
    expect(r).toHaveLength(1)
    expect(r[0].state).toBe('firing')
  })

  it('精确匹配：node 不写前缀就匹配不上（避免深链带出无关项）', () => {
    expect(filterAlerts({ alerts: ALERTS, node: 'web' })).toHaveLength(0)
  })

  it('空数组/缺省入参不抛错', () => {
    expect(filterAlerts()).toEqual([])
    expect(filterAlerts({ alerts: [] })).toEqual([])
  })
})
