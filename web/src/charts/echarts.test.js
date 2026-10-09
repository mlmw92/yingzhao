import { describe, it, expect } from 'vitest'
import { echarts, areaOption, areaMultiOption, monitorOption, rateFull } from './echarts'

describe('实时趋势图提示框数值格式化', () => {
  it('速率类（磁盘/网络 IO）按 KB/s 展示两位小数，不暴露浮点误差', () => {
    const opt = areaMultiOption(
      [{ name: '接收', color: '#22c55e' }, { name: '发送', color: '#a855f7' }],
      { unit: 'B/s' },
    )
    const fmt = opt.tooltip.valueFormatter
    expect(typeof fmt).toBe('function')
    expect(fmt(77617.62999999999)).toBe('75.80 KB/s')
    expect(fmt(55806.990000000005)).toBe('54.50 KB/s')
    expect(fmt(0)).toBe('0 B/s')
  })

  it('百分比类（CPU/内存）保留一位小数', () => {
    expect(areaOption('#22d3ee', '%').tooltip.valueFormatter(12.3456)).toBe('12.3%')
  })

  it('rateFull 分级与坐标轴用的 rateShort 一致', () => {
    expect(rateFull(1536)).toBe('1.50 KB/s')
    expect(rateFull(2 * 1024 * 1024)).toBe('2.00 MB/s')
    expect(rateFull(512)).toBe('512 B/s')
  })
})

// 图例与提示框的小圆点取的是 itemStyle.color（不是 lineStyle.color）：
// 只设 lineStyle 会让它们落回 ECharts 默认调色板 —— 表现为「图例是绿的，线是紫的」。
describe('折线系列颜色（图例/提示框圆点必须与线一致）', () => {
  it('monitorOption（历史趋势图）系列带 itemStyle.color', () => {
    const opt = monitorOption({
      unit: 'B/s',
      series: [
        { name: '接收', color: '#22c55e', data: [] },
        { name: '发送', color: '#a855f7', data: [] },
      ],
    })
    expect(opt.series[0].itemStyle.color).toBe('#22c55e')
    expect(opt.series[1].itemStyle.color).toBe('#a855f7')
    expect(opt.series[0].lineStyle.color).toBe('#22c55e')
  })

  it('实时趋势图（areaMultiOption）系列同样带 itemStyle.color', () => {
    const opt = areaMultiOption(
      [{ name: '接收', color: '#22c55e' }, { name: '发送', color: '#a855f7' }],
      { unit: 'B/s' },
    )
    expect(opt.series.map((s) => s.itemStyle.color)).toEqual(['#22c55e', '#a855f7'])
  })

  it('渲染后图例图标用的是系列色，不是默认调色板', () => {
    const el = document.createElement('div')
    document.body.appendChild(el)
    const chart = echarts.init(el, null, { renderer: 'svg', width: 480, height: 240 })
    chart.setOption(monitorOption({
      unit: 'B/s',
      series: [
        { name: '接收', color: '#22c55e', data: [[Date.now(), 1000]] },
        { name: '发送', color: '#a855f7', data: [[Date.now(), 2000]] },
      ],
    }))
    // 折线用 stroke 画，面积用渐变 url(#...)：出现纯色 fill 的只可能是图例图标
    const fills = [...el.querySelectorAll('svg [fill]')].map((n) => n.getAttribute('fill'))
    expect(fills).toContain('#22c55e')
    expect(fills).toContain('#a855f7')
    chart.dispose()
    el.remove()
  })
})

// 只测 option 里的格式化函数还不够：ECharts 是否真的在提示框里用它，必须走一遍渲染。
// jsdom 没有 canvas 2D 上下文，因此这里用 SVG 渲染器，让 option 真实经过 ECharts
// 的提示框组装逻辑后再断言 DOM 文本。
describe('实时趋势图提示框渲染（SVG 渲染器）', () => {
  // 容器 div 的 textContent 也含图例文字、坐标轴标签（SVG text），因此用谓词挑出提示框内容节点
  function renderTooltip(option, values, match) {
    const el = document.createElement('div')
    document.body.appendChild(el)
    const chart = echarts.init(el, null, { renderer: 'svg', width: 400, height: 200 })
    chart.setOption(option)
    chart.setOption({ series: values.map((v) => ({ data: [[Date.now(), v]] })) })
    chart.dispatchAction({ type: 'showTip', seriesIndex: 0, dataIndex: 0 })
    const tip = [...el.querySelectorAll('div')]
      .map((d) => d.textContent || '')
      .find(match) || ''
    chart.dispose()
    el.remove()
    return tip
  }

  it('网络 IO 提示框显示可读速率，不出现浮点误差', () => {
    const tip = renderTooltip(
      areaMultiOption([{ name: '接收', color: '#22c55e' }, { name: '发送', color: '#a855f7' }], { unit: 'B/s' }),
      [77617.62999999999, 55806.990000000005],
      (t) => t.includes('KB/s') && t.includes('接收') && t.includes('发送'),
    )
    expect(tip).toContain('75.80 KB/s')
    expect(tip).toContain('54.50 KB/s')
    expect(tip).not.toContain('6299999')
    expect(tip).not.toContain('000000005')
  })

  it('CPU 实时提示框显示一位小数百分比', () => {
    const tip = renderTooltip(areaOption('#22d3ee', '%'), [12.3456789], (t) => t.includes('%'))
    expect(tip).toContain('12.3%')
    expect(tip).not.toContain('12.3456789')
  })
})
