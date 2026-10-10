// 首页告警去重的回归测试（§33 #7）。
// 守两条规则：① 紧急与最近不重复渲染同一条；② 紧急区块被隐藏时，最近告警必须还原完整列表。
import { describe, it, expect } from 'vitest'
import { dedupeRecentAlerts, alertKey } from './alertDedupe'

const mk = (id, severity, startsAt) => ({ id, severity, ruleName: 'r-' + id, startsAt })

const critical = [mk('a1', 'critical', '2026-10-09T10:00:00Z')]
const alerts = [
  mk('a1', 'critical', '2026-10-09T10:00:00Z'),
  mk('a2', 'warning', '2026-10-09T09:00:00Z'),
  mk('a3', 'warning', '2026-10-09T08:00:00Z'),
]

describe('首页告警去重', () => {
  it('紧急告警区块可见时，最近告警不再重复列出同一条', () => {
    const { list, deduped } = dedupeRecentAlerts({ alerts, critical, criticalVisible: true })
    expect(list.map((a) => a.id)).toEqual(['a2', 'a3'])
    expect(deduped).toBe(1)
  })

  it('紧急告警区块被隐藏时，最近告警还原完整列表（不能静默丢信息）', () => {
    const { list, deduped } = dedupeRecentAlerts({ alerts, critical, criticalVisible: false })
    expect(list.map((a) => a.id)).toEqual(['a1', 'a2', 'a3'])
    expect(deduped).toBe(0)
  })

  it('去重后仍按时间倒序，且遵守条数上限', () => {
    const many = Array.from({ length: 20 }, (_, i) =>
      mk('m' + i, 'warning', new Date(Date.UTC(2026, 9, 9, 0, 0, i)).toISOString())
    )
    const { list } = dedupeRecentAlerts({ alerts: many, critical: [], criticalVisible: true, limit: 5 })
    expect(list).toHaveLength(5)
    expect(list[0].id).toBe('m19')
    expect(list[4].id).toBe('m15')
  })

  it('deduped 只统计原本会显示、但被剔除的条数（不数本就超出上限的老告警）', () => {
    const many = [
      mk('c1', 'critical', '2026-10-09T10:00:00Z'),
      mk('c2', 'critical', '2026-10-09T09:00:00Z'),
      ...Array.from({ length: 10 }, (_, i) => mk('n' + i, 'warning', new Date(Date.UTC(2026, 9, 9, 0, 0, i)).toISOString())),
    ]
    const crit = [mk('c1', 'critical', '2026-10-09T10:00:00Z'), mk('c2', 'critical', '2026-10-09T09:00:00Z')]
    const { list, deduped } = dedupeRecentAlerts({ alerts: many, critical: crit, criticalVisible: true, limit: 3 })
    expect(deduped).toBe(2)
    expect(list).toHaveLength(3)
    expect(list.some((a) => a.id.startsWith('c'))).toBe(false)
  })

  it('没有后端 id 时退化为「规则+时间」组合键，仍能去重', () => {
    const a = { ruleId: 'rule-x', severity: 'critical', startsAt: '2026-10-09T10:00:00Z' }
    const b = { ruleId: 'rule-x', severity: 'critical', startsAt: '2026-10-09T10:00:00Z' }
    expect(alertKey(a)).toBe(alertKey(b))
    const { list } = dedupeRecentAlerts({ alerts: [a, b], critical: [a], criticalVisible: true })
    expect(list).toHaveLength(0)
  })
})
