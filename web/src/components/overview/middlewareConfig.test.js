// 首页中间件概览的取值口径。
//
// 实例接口有两种形状：早期类型是扁平字段（usedSpaceMB），
// 声明化之后是 metrics 数组（{ key, value }）。首页必须两种都读得出来，
// 否则改造一类中间件就静默丢掉它的一张卡片。
import { describe, it, expect } from 'vitest'
import { instMetric } from './middlewareConfig'

describe('instMetric 实例指标取值', () => {
  it('优先按 key 从 metrics 数组取', () => {
    const inst = { usedSpaceMB: 999, metrics: [{ key: 'fastdfs_used_space', value: 42 }] }
    expect(instMetric(inst, 'fastdfs_used_space')).toBe(42)
  })

  it('扁平字段仍然可用（尚未声明化的类型）', () => {
    expect(instMetric({ messageAccumulation: 7 }, 'messageAccumulation')).toBe(7)
  })

  it('取不到时返回 undefined，由调用方决定展示什么', () => {
    expect(instMetric({ metrics: [] }, 'nope')).toBeUndefined()
    expect(instMetric({}, '')).toBeUndefined()
  })
})
