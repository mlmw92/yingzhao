// 系统健康度卡片的回归测试。
//
// 这张卡与「紧急告警」同处一行，会被后者拉高。只有圆环 + 四个标签时，
// 中间会留下一大块空白；这里钉住「四个构成项按行展示并给出扣分」这个结构，
// 它既是填充高度的内容，也是「分数为什么这么低」的解释。
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import HealthBlock from './HealthBlock.vue'

const HEALTH = {
  score: 0,
  statusText: '风险较高',
  rank: 'bad',
  total: 4,
  online: 4,
  offline: 0,
  criticalAlerts: 18,
  warningAlerts: 45,
  alertScore: 0,
  pressure: [
    { key: 'cpu', label: 'CPU', rate: 5, warnCount: 0, badCount: 0, count: 4, avgOver: 0, weight: 15 },
    { key: 'mem', label: '内存', rate: 41, warnCount: 0, badCount: 0, count: 4, avgOver: 0, weight: 12 },
    { key: 'disk', label: '磁盘', rate: 48, warnCount: 0, badCount: 0, count: 4, avgOver: 0, weight: 12 },
  ],
}

function mountBlock(health = HEALTH) {
  return mount(HealthBlock, { props: { health } })
}

describe('HealthBlock', () => {
  it('把四个构成项按行展示，并给出各自的扣分', () => {
    // 先隔离安全项：warningAlerts 置 0，验证 critical×5 的口径
    const w = mountBlock({ ...HEALTH, warningAlerts: 0 })
    const rows = w.findAll('.hb-row')
    expect(rows).toHaveLength(4)

    const keys = rows.map((r) => r.attributes('data-key'))
    expect(keys).toEqual(['avail', 'perf', 'capacity', 'security'])

    // 可用性满员：不扣分
    expect(w.find('.hb-row[data-key="avail"] .hb-deduct').text()).toBe('0')
    // 18 条紧急 × 5 = 90 分
    expect(w.find('.hb-row[data-key="security"] .hb-deduct').text()).toContain('90')
    expect(w.find('.hb-row[data-key="security"] .hb-value').text()).toContain('18 条紧急')
  })

  it('安全扣分封顶 100：告警足够多时分数直接归零', () => {
    // 18×5 + 45×2 = 180 → 封顶 100，与 healthScore.js 的 alertScore 下限 0 对应
    const w = mountBlock()
    expect(w.find('.hb-row[data-key="security"] .hb-deduct').text()).toContain('100')
  })

  it('资源压力超过阈值时按权重计入扣分', () => {
    const health = {
      ...HEALTH,
      pressure: [
        { key: 'cpu', label: 'CPU', rate: 90, warnCount: 4, badCount: 2, count: 4, avgOver: 20, weight: 15 },
        { key: 'mem', label: '内存', rate: 95, warnCount: 4, badCount: 3, count: 4, avgOver: 15, weight: 12 },
        { key: 'disk', label: '磁盘', rate: 50, warnCount: 0, badCount: 0, count: 4, avgOver: 0, weight: 12 },
      ],
    }
    const w = mountBlock(health)
    // CPU 20/100*15 = 3，内存 15/100*12 = 1.8 → 合计约 5
    expect(w.find('.hb-row[data-key="perf"] .hb-deduct').text()).toContain('5')
    expect(w.find('.hb-row[data-key="capacity"] .hb-deduct').text()).toBe('0')
  })

  it('说明分数的来源，避免只看到一个孤零零的数字', () => {
    const w = mountBlock()
    expect(w.text()).toContain('健康度')
    expect(w.text()).toContain('扣分')
  })
})
