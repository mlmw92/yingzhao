// PS-03 首页概览三处改造的单测：
//  1) AlertList —— CriticalAlerts / RecentAlerts 合并后的参数化组件（两种 variant 的差异 + 空态文案）
//  2) TriageBar —— role="alert" 与「查看详情」带 node/rule 参数跳转
//  3) KpiBlock  —— 与全站 KpiCard 收敛（不再自持一套卡片视觉）
//
// 这三处都属于"看着正常但其实没生效"的类型：variant 判断写反 → 最近告警丢了状态药丸；
// 深链参数没带上 → 用户点进去还得自己找；KpiBlock 没复用 KpiCard → 同屏数字大小不一致。
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'

const routerPush = vi.fn()
vi.mock('vue-router', () => ({
  useRouter: () => ({ push: routerPush, replace: vi.fn() }),
}))

// vue-echarts 在 jsdom 里没有 canvas；sparkline 只是附加内容，stub 掉即可
vi.mock('vue-echarts', () => ({
  default: { name: 'VChart', template: '<div class="vchart-stub" />' },
}))

import AlertList from './AlertList.vue'
import TriageBar from './TriageBar.vue'
import KpiBlock from './KpiBlock.vue'

const stubs = {
  'el-icon': { name: 'ElIcon', template: '<i><slot /></i>' },
  'el-button': { name: 'ElButton', template: '<button class="el-button"><slot /></button>' },
}

function mountWith(comp, props) {
  return mount(comp, { props, global: { stubs } })
}

beforeEach(() => {
  routerPush.mockClear()
  localStorage.clear()
})

describe('AlertList · 紧急告警（variant=critical）', () => {
  const alerts = [
    { id: 'a1', ruleName: '磁盘使用率过高', node: 'web-01', nodeIp: '10.0.0.1', severity: 'critical', startsAt: Date.now() - 60_000 },
    { id: 'a2', ruleName: '内存使用率过高', node: 'web-02', severity: 'warning', startsAt: Date.now() - 3_600_000 },
  ]

  it('逐条渲染规则名与节点，且不显示状态药丸（状态是「最近告警」才有的维度）', () => {
    const w = mountWith(AlertList, { alerts, variant: 'critical' })
    expect(w.findAll('.al-item')).toHaveLength(2)
    expect(w.text()).toContain('磁盘使用率过高')
    expect(w.text()).toContain('web-01')
    expect(w.findAll('.pill')).toHaveLength(0)
  })

  it('空列表时说明「当前无紧急告警」，而不是留白', () => {
    const w = mountWith(AlertList, { alerts: [], variant: 'critical' })
    expect(w.find('.al-item').exists()).toBe(false)
    expect(w.text()).toContain('当前无紧急告警')
  })

  it('点击某一行带着该条的 node/rule 跳转（而不是丢到全量列表）', async () => {
    const w = mountWith(AlertList, { alerts, variant: 'critical' })
    await w.findAll('.al-item')[0].trigger('click')
    expect(routerPush).toHaveBeenCalledWith({
      path: '/alerts',
      query: { node: 'web-01', rule: '磁盘使用率过高' },
    })
  })

  it('键盘 Enter 同样可触发跳转（<div role=button> 的可访问性）', async () => {
    const w = mountWith(AlertList, { alerts, variant: 'critical' })
    const row = w.findAll('.al-item')[0]
    expect(row.attributes('tabindex')).toBe('0')
    await row.trigger('keydown.enter')
    expect(routerPush).toHaveBeenCalledTimes(1)
  })
})

describe('AlertList · 最近告警（variant=recent）', () => {
  const alerts = [
    { id: 'r1', ruleName: '磁盘空间不足', node: 'web-01', message: '可用空间低于 10%', state: 'firing', severity: 'warning', startsAt: Date.now() - 120_000 },
  ]

  it('渲染 message 与状态药丸', () => {
    const w = mountWith(AlertList, { alerts, variant: 'recent' })
    expect(w.text()).toContain('可用空间低于 10%')
    expect(w.findAll('.pill')).toHaveLength(1)
    expect(w.text()).toContain('告警中')
  })

  it('去重后为空时说的是「已在上方显示」，不是「暂无告警」', () => {
    const w = mountWith(AlertList, { alerts: [], variant: 'recent', dedupedCount: 3 })
    expect(w.text()).toContain('已在上方「紧急告警」中显示')
    expect(w.text()).not.toContain('暂无告警记录')
  })

  it('列表非空但有去重项时，底部显式说明少列了几条', () => {
    const w = mountWith(AlertList, { alerts, variant: 'recent', dedupedCount: 2 })
    expect(w.find('.al-dedup').text()).toContain('2')
  })

  it('真的没有告警时才说「暂无告警记录」', () => {
    const w = mountWith(AlertList, { alerts: [], variant: 'recent' })
    expect(w.text()).toContain('暂无告警记录')
  })
})

describe('TriageBar · 研判条', () => {
  const alerts = [
    {
      ruleName: '磁盘使用率过高',
      node: 'web-01',
      severity: 'critical',
      startsAt: '2026-01-01T00:00:00Z',
      value: 92,
      threshold: 85,
      operator: '>',
    },
  ]

  it('有活跃告警时渲染，并带 role="alert"（读屏应立即播报）', () => {
    const w = mountWith(TriageBar, { alerts, nodes: [], latestMap: {} })
    expect(w.find('.triage').attributes('role')).toBe('alert')
    expect(w.text()).toContain('最该处理：web-01')
  })

  it('「查看详情」带上这条告警的 node / rule 跳转', async () => {
    const w = mountWith(TriageBar, { alerts, nodes: [], latestMap: {} })
    await w.find('button.el-button').trigger('click')
    expect(routerPush).toHaveBeenCalledWith({
      path: '/alerts',
      query: { node: 'web-01', rule: '磁盘使用率过高' },
    })
  })

  it('无告警时整条不渲染', () => {
    const w = mountWith(TriageBar, { alerts: [], nodes: [], latestMap: {} })
    expect(w.find('.triage').exists()).toBe(false)
    expect(w.text()).toBe('')
  })
})

describe('KpiBlock · 与 KpiCard 收敛', () => {
  const kpis = [
    { label: '主机总数', value: 12, foot: '在线 10 · 离线 2', tone: 'warn', icon: null },
    { label: '活跃告警', value: 3, foot: '紧急 1 · 警告 2', tone: 'bad', icon: null },
  ]

  it('复用统一 KpiCard：卡片类名与数值/标签/口径行都来自同一组件', () => {
    const w = mountWith(KpiBlock, { kpis })
    expect(w.findAll('.kpi-card')).toHaveLength(2)
    expect(w.text()).toContain('主机总数')
    expect(w.text()).toContain('12')
    expect(w.text()).toContain('在线 10 · 离线 2')
  })

  it('tone 透传到 KpiCard 的色调类（bad → 红色档）', () => {
    const w = mountWith(KpiBlock, { kpis })
    expect(w.findAll('.kpi-card')[1].classes()).toContain('tone-bad')
  })

  it('采样点不足时仍占位，避免行高抖动', () => {
    const w = mountWith(KpiBlock, { kpis })
    expect(w.findAll('.kpi-spark-pending')).toHaveLength(2)
  })

  it('无指标时给出可执行的空态', () => {
    const w = mountWith(KpiBlock, { kpis: [] })
    expect(w.findAll('.kpi-card')).toHaveLength(0)
    expect(w.text()).toContain('暂无指标数据')
  })
})
