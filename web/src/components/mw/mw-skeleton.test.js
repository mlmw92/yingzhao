// 批次 9 · 中间件骨架组件（PS-06）：
//   MwStatusDot 三态与别名归一 / TrendChart 空·加载·错误三态 / MwHostCard / MwDetailHeader。
//
// TrendChart 内部要建 ECharts 实例，jsdom 里没有真实画布尺寸，
// 这里把 initChart 换成桩，只验证组件自身的契约（三态、重试、option 透传、卸载销毁）。
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

const h = vi.hoisted(() => {
  const setOption = vi.fn()
  const dispose = vi.fn()
  const resize = vi.fn()
  const isDisposed = vi.fn(() => false)
  const initChart = vi.fn(() => ({ setOption, dispose, resize, isDisposed }))
  return { setOption, dispose, resize, isDisposed, initChart }
})

vi.mock('../../charts/echarts', () => ({
  initChart: h.initChart,
  mwTrendOption: (series, opts) => ({ series: series || [], __opts: opts }),
}))

import MwStatusDot from './MwStatusDot.vue'
import TrendChart from './TrendChart.vue'
import MwHostCard from './MwHostCard.vue'
import MwDetailHeader from './MwDetailHeader.vue'
import { MW_RANGE_OPTIONS, rangeToWindow, rangeToStep } from './mwRange'

const ButtonStub = {
  emits: ['click'],
  template: '<button @click="$emit(\'click\')"><slot /></button>',
}
const IconStub = { template: '<i><slot /></i>' }
const RadioGroupStub = {
  name: 'ElRadioGroup',
  props: ['modelValue'],
  emits: ['update:modelValue'],
  template: '<div class="rg-stub"><slot /></div>',
}
const RadioButtonStub = {
  props: ['value'],
  template: '<button class="rb-stub"><slot /></button>',
}
const global = {
  stubs: {
    'el-button': ButtonStub,
    'el-icon': IconStub,
    'el-radio-group': RadioGroupStub,
    'el-radio-button': RadioButtonStub,
  },
}

describe('MwStatusDot 三态', () => {
  it('normal/warning/abnormal 各挂对应类名，默认文案为 正常/警告/异常', () => {
    const n = mount(MwStatusDot, { props: { status: 'normal' }, global })
    expect(n.classes()).toContain('normal')
    expect(n.text()).toBe('正常')

    const w = mount(MwStatusDot, { props: { status: 'warning' }, global })
    expect(w.classes()).toContain('warning')
    expect(w.text()).toBe('警告')

    const a = mount(MwStatusDot, { props: { status: 'abnormal' }, global })
    expect(a.classes()).toContain('abnormal')
    expect(a.text()).toBe('异常')
  })

  it('离线/失败类别名归一到 abnormal（红），不再与 warning 混为一谈', () => {
    for (const s of ['offline', 'down', 'failed', 'dead', 'exited', 'CrashLoopBackOff']) {
      const w = mount(MwStatusDot, { props: { status: s }, global })
      expect(w.classes(), `${s} 应归一为 abnormal`).toContain('abnormal')
    }
  })

  it('暂停/重启中/待就绪类别名归一到 warning', () => {
    for (const s of ['paused', 'restarting', 'pending', 'created']) {
      const w = mount(MwStatusDot, { props: { status: s }, global })
      expect(w.classes(), `${s} 应归一为 warning`).toContain('warning')
    }
  })

  it('未知取值兜底为 abnormal（宁可高估不可漏报）', () => {
    const w = mount(MwStatusDot, { props: { status: 'wat' }, global })
    expect(w.classes()).toContain('abnormal')
  })

  it('dotOnly=true 时只画点、不显示文案', () => {
    const w = mount(MwStatusDot, { props: { status: 'normal', dotOnly: true }, global })
    expect(w.find('.mw-status-dot').exists()).toBe(true)
    expect(w.find('.mw-status-label').exists()).toBe(false)
    expect(w.classes()).toContain('dot-only')
  })

  it('自定义 label 覆盖默认文案', () => {
    const w = mount(MwStatusDot, { props: { status: 'abnormal', label: '已停止' }, global })
    expect(w.text()).toBe('已停止')
  })
})

describe('TrendChart 三态', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    h.isDisposed.mockReturnValue(false)
  })

  it('挂载时用 option 初始化图表', async () => {
    const option = { series: [{ name: 'CPU', data: [[1, 2]] }] }
    mount(TrendChart, { props: { option }, global })
    await flushPromises()
    expect(h.initChart).toHaveBeenCalledTimes(1)
    expect(h.setOption).toHaveBeenCalledWith(option, true)
  })

  it('error 非空时展示错误态与重试按钮，优先于 loading', async () => {
    const w = mount(TrendChart, {
      props: { option: { series: [] }, error: '查询失败', loading: true },
      global,
    })
    expect(w.find('.tc-error').exists()).toBe(true)
    expect(w.find('.tc-error').text()).toContain('查询失败')
    expect(w.find('.tc-loading').exists()).toBe(false)
    await w.find('.tc-error button').trigger('click')
    expect(w.emitted('retry')).toHaveLength(1)
  })

  it('retryable=false 时错误态不展示重试按钮', () => {
    const w = mount(TrendChart, { props: { error: '失败', retryable: false }, global })
    expect(w.find('.tc-error button').exists()).toBe(false)
  })

  it('loading=true 展示骨架', () => {
    const w = mount(TrendChart, { props: { option: { series: [] }, loading: true }, global })
    expect(w.find('.tc-loading').exists()).toBe(true)
    expect(w.find('.tc-sk').exists()).toBe(true)
  })

  it('series 全空时自动判定为空态；有数据则不空', () => {
    const empty = mount(TrendChart, { props: { option: { series: [{ name: 'a', data: [] }] } }, global })
    expect(empty.find('.tc-empty').exists()).toBe(true)

    const hasData = mount(TrendChart, {
      props: { option: { series: [{ name: 'a', data: [[1, 2]] }] } },
      global,
    })
    expect(hasData.find('.tc-empty').exists()).toBe(false)
  })

  it('empty prop 显式覆盖自动判定', () => {
    const w = mount(TrendChart, {
      props: { option: { series: [{ name: 'a', data: [[1, 2]] }] }, empty: true },
      global,
    })
    expect(w.find('.tc-empty').exists()).toBe(true)
  })

  it('卸载时销毁实例（关闭抽屉不再泄漏）', () => {
    const w = mount(TrendChart, { props: { option: { series: [] } }, global })
    w.unmount()
    expect(h.dispose).toHaveBeenCalledTimes(1)
  })

  it('height 数字转 px，字符串原样', () => {
    const a = mount(TrendChart, { props: { option: { series: [] }, height: 180 }, global })
    expect(a.attributes('style')).toContain('height: 180px')
    const b = mount(TrendChart, { props: { option: { series: [] }, height: '100%' }, global })
    expect(b.attributes('style')).toContain('height: 100%')
  })
})

describe('MwHostCard', () => {
  it('渲染名称、副名与计数文案', () => {
    const w = mount(MwHostCard, {
      props: { name: '10.0.0.1', sub: 'web-01', countText: '3 容器' },
      global,
    })
    expect(w.find('.mwh-name').text()).toBe('10.0.0.1')
    expect(w.find('.mwh-sub').text()).toBe('web-01')
    expect(w.find('.mwh-count').text()).toBe('3 容器')
  })

  it('无副名/计数时不渲染对应节点', () => {
    const w = mount(MwHostCard, { props: { name: 'n1' }, global })
    expect(w.find('.mwh-sub').exists()).toBe(false)
    expect(w.find('.mwh-count').exists()).toBe(false)
  })

  it('status 透传到状态点（离线主机显示红点）', () => {
    const w = mount(MwHostCard, { props: { name: 'n1', status: 'abnormal' }, global })
    expect(w.find('.mw-status').classes()).toContain('abnormal')
    expect(w.find('.mw-status-label').exists()).toBe(false) // dotOnly
  })

  it('dim/selected 类名', () => {
    const w = mount(MwHostCard, { props: { name: 'n1', dim: true, selected: true }, global })
    expect(w.classes()).toContain('is-dim')
    expect(w.classes()).toContain('is-selected')
  })

  it('点击卡片 emit select', async () => {
    const w = mount(MwHostCard, { props: { name: 'n1' }, global })
    await w.trigger('click')
    expect(w.emitted('select')).toHaveLength(1)
  })

  it('emptyText 在无默认插槽时作为占位', () => {
    const w = mount(MwHostCard, { props: { name: 'n1', emptyText: '暂无容器' }, global })
    expect(w.find('.mwh-empty').text()).toBe('暂无容器')
  })

  it('默认插槽渲染在卡片体内', () => {
    const w = mount(MwHostCard, {
      props: { name: 'n1' },
      slots: { default: '<span class="chip-x">c1</span>' },
      global,
    })
    expect(w.find('.mwh-body .chip-x').exists()).toBe(true)
  })
})

describe('MwDetailHeader', () => {
  it('渲染标题、副标题与元信息字段', () => {
    const w = mount(MwDetailHeader, {
      props: {
        title: 'redis-01',
        subtitle: '10.0.0.5:6379',
        fields: [{ label: '版本', value: '7.2' }, { label: '角色', value: 'Master' }],
      },
      global,
    })
    expect(w.find('.mdh-title').text()).toBe('redis-01')
    expect(w.find('.mdh-sub').text()).toBe('10.0.0.5:6379')
    expect(w.findAll('.mdh-field')).toHaveLength(2)
  })

  it('status 为空时不渲染状态点', () => {
    const w = mount(MwDetailHeader, { props: { title: 'x' }, global })
    expect(w.find('.mw-status').exists()).toBe(false)
  })

  it('status 非空时按三态渲染大号状态点', () => {
    const w = mount(MwDetailHeader, { props: { title: 'x', status: 'warning' }, global })
    expect(w.find('.mw-status').classes()).toContain('warning')
    expect(w.find('.mw-status').classes()).toContain('lg')
  })

  it('默认渲染 5 档时间范围，切换时 emit update:range', async () => {
    const w = mount(MwDetailHeader, { props: { title: 'x', range: '1h' }, global })
    expect(w.findAll('.rb-stub')).toHaveLength(5)
    w.findComponent(RadioGroupStub).vm.$emit('update:modelValue', '6h')
    expect(w.emitted('update:range')).toEqual([['6h']])
  })

  it('ranges 为空数组时不渲染切换器', () => {
    const w = mount(MwDetailHeader, { props: { title: 'x', ranges: [] }, global })
    expect(w.find('.rg-stub').exists()).toBe(false)
  })
})

describe('mwRange 工具', () => {
  it('rangeToWindow 按档位换算跨度，未知值回退 1 小时', () => {
    const now = 1_000_000_000
    expect(rangeToWindow('5m', now)).toEqual({ start: now - 5 * 60 * 1000, end: now })
    expect(rangeToWindow('24h', now).start).toBe(now - 24 * 60 * 60 * 1000)
    expect(rangeToWindow('bogus', now).start).toBe(now - 60 * 60 * 1000)
  })

  it('rangeToStep 返回毫秒步长（后端 step 契约是毫秒），长窗口用更大步长', () => {
    // 关键：必须 ≥1000（query.go 判 step<1000 直接 400），且 ≤ 对应窗口跨度。
    expect(rangeToStep('5m')).toBe(5000)
    expect(rangeToStep('15m')).toBe(15000)
    expect(rangeToStep('1h')).toBe(60000)
    expect(rangeToStep('6h')).toBe(120000)
    expect(rangeToStep('24h')).toBe(300000)
    for (const v of ['5m', '15m', '1h', '6h', '24h']) {
      expect(rangeToStep(v)).toBeGreaterThanOrEqual(1000)
    }
  })

  it('默认档位为 5 档且含 1h', () => {
    expect(MW_RANGE_OPTIONS).toHaveLength(5)
    expect(MW_RANGE_OPTIONS.map((r) => r.value)).toContain('1h')
  })
})
