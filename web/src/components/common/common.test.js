// PS-00 规范件增强（批次 8）：SectionCard 四态 / EmptyState size / StatusPill icon+text
//   / FilterChips / PartialBar 的渲染与事件契约。
import { describe, it, expect } from 'vitest'
import { markRaw } from 'vue'
import { mount } from '@vue/test-utils'
import SectionCard from './SectionCard.vue'
import EmptyState from './EmptyState.vue'
import StatusPill from './StatusPill.vue'
import FilterChips from './FilterChips.vue'
import PartialBar from './PartialBar.vue'

const SlotStub = { template: '<span><slot /></span>' }
// 透传 click 的按钮桩：让 `@click`（组件事件）能被 `trigger('click')` 触发
const ButtonStub = {
  emits: ['click'],
  template: '<button @click="$emit(\'click\')"><slot /></button>',
}
const IconStub = {
  emits: ['click'],
  template: '<i @click="$emit(\'click\')"><slot /></i>',
}
const global = {
  stubs: { 'el-button': ButtonStub, 'el-icon': IconStub, 'el-icon-close': SlotStub },
}

describe('SectionCard 四态', () => {
  it('默认渲染标题与内容插槽', () => {
    const w = mount(SectionCard, {
      props: { title: '安全事件', subtitle: '近 24h' },
      slots: { default: '<p class="body-x">正文</p>' },
      global,
    })
    expect(w.find('.sec-title').text()).toBe('安全事件')
    expect(w.find('.sec-sub').text()).toBe('近 24h')
    expect(w.find('.body-x').exists()).toBe(true)
  })

  it('loading=true 时渲染骨架、不渲染内容', () => {
    const w = mount(SectionCard, {
      props: { title: 'T', loading: true },
      slots: { default: '<p class="body-x">正文</p>' },
      global,
    })
    expect(w.find('.sec-skeleton').exists()).toBe(true)
    expect(w.find('.body-x').exists()).toBe(false)
  })

  it('error 非空时渲染错误条并优先于 loading 与内容', () => {
    const w = mount(SectionCard, {
      props: { title: 'T', loading: true, error: '拉取失败' },
      slots: { default: '<p class="body-x">正文</p>' },
      global,
    })
    expect(w.find('.sec-error').exists()).toBe(true)
    expect(w.find('.sec-error-msg').text()).toBe('拉取失败')
    expect(w.find('.sec-skeleton').exists()).toBe(false)
    expect(w.find('.body-x').exists()).toBe(false)
  })

  it('errorRetry=true 时展示重试按钮并 emit retry', async () => {
    const w = mount(SectionCard, {
      props: { error: '失败', errorRetry: true },
      global,
    })
    const btn = w.find('.sec-error button')
    expect(btn.exists()).toBe(true)
    await btn.trigger('click')
    expect(w.emitted('retry')).toHaveLength(1)
  })

  it('errorRetry=false 时不展示重试按钮', () => {
    const w = mount(SectionCard, { props: { error: '失败' }, global })
    expect(w.find('.sec-error button').exists()).toBe(false)
  })
})

describe('EmptyState', () => {
  it('渲染标题与 hints，action 按钮 emit action', async () => {
    const w = mount(EmptyState, {
      props: { title: '暂无数据', hints: ['检查筛选', '稍后重试'], actionText: '刷新' },
      global,
    })
    expect(w.find('.es-title').text()).toBe('暂无数据')
    expect(w.findAll('.es-hint')).toHaveLength(2)
    await w.find('.es-actions button').trigger('click')
    expect(w.emitted('action')).toHaveLength(1)
  })

  it('secondaryActionText 渲染次按钮并 emit secondary-action', async () => {
    const w = mount(EmptyState, {
      props: { actionText: '新建', secondaryActionText: '了解更多' },
      global,
    })
    const btns = w.findAll('.es-actions button')
    expect(btns).toHaveLength(2)
    await btns[1].trigger('click')
    expect(w.emitted('secondary-action')).toHaveLength(1)
  })

  it('size=table-inline 加类名，默认 block 不加', () => {
    const a = mount(EmptyState, { props: { title: 'x', size: 'table-inline' }, global })
    expect(a.find('.empty-state').classes()).toContain('table-inline')
    const b = mount(EmptyState, { props: { title: 'x' }, global })
    expect(b.find('.empty-state').classes()).not.toContain('table-inline')
  })
})

describe('StatusPill', () => {
  it('text prop 免插槽渲染', () => {
    const w = mount(StatusPill, { props: { tone: 'ok', text: '在线' }, global })
    expect(w.text()).toBe('在线')
    expect(w.classes()).toContain('ok')
  })

  it('icon prop 渲染第二编码图标', () => {
    const Icon = markRaw({ template: '<svg class="my-icon" />' })
    const w = mount(StatusPill, { props: { text: '异常', icon: Icon }, global })
    expect(w.find('.pill-icon').exists()).toBe(true)
    expect(w.find('.my-icon').exists()).toBe(true)
  })

  it('插槽优先于 text prop', () => {
    const w = mount(StatusPill, {
      props: { text: 'prop 文案' },
      slots: { default: '插槽文案' },
      global,
    })
    expect(w.text()).toBe('插槽文案')
  })
})

describe('FilterChips', () => {
  it('chips 为空时不渲染', () => {
    const w = mount(FilterChips, { props: { chips: [] }, global })
    expect(w.find('.filter-chips').exists()).toBe(false)
  })

  it('逐条渲染 label：value，点 × emit remove(key)', async () => {
    const w = mount(FilterChips, {
      props: {
        chips: [
          { key: 'node', label: '节点', value: 'web-01' },
          { key: 'q', label: '关键词', value: 'error' },
        ],
      },
      global,
    })
    expect(w.findAll('.fc-chip')).toHaveLength(2)
    expect(w.text()).toContain('节点：web-01')
    await w.findAll('.fc-x')[0].trigger('click')
    expect(w.emitted('remove')[0]).toEqual(['node'])
  })

  it('清空按钮 emit clear', async () => {
    const w = mount(FilterChips, {
      props: { chips: [{ key: 'a', label: 'A', value: 1 }] },
      global,
    })
    await w.find('.fc-clear').trigger('click')
    expect(w.emitted('clear')).toHaveLength(1)
  })

  it('值为空时不渲染冒号段', () => {
    const w = mount(FilterChips, {
      props: { chips: [{ key: 'a', label: '仅标签' }] },
      global,
    })
    expect(w.find('.fc-text').text()).toBe('仅标签')
  })
})

describe('PartialBar', () => {
  it('failed 为空时不渲染', () => {
    const w = mount(PartialBar, { props: { failed: [] }, global })
    expect(w.find('.partial-bar').exists()).toBe(false)
  })

  it('渲染失败项与重试按钮，emit retry', async () => {
    const w = mount(PartialBar, {
      props: { failed: ['通知渠道', '抑制规则'] },
      global,
    })
    expect(w.find('.pb-title').text()).toBe('部分数据加载失败')
    expect(w.text()).toContain('通知渠道、抑制规则')
    await w.find('button').trigger('click')
    expect(w.emitted('retry')).toHaveLength(1)
  })

  it('超过 maxShow 时折叠为「等 N 项」', () => {
    const w = mount(PartialBar, {
      props: { failed: ['a', 'b', 'c', 'd'], maxShow: 2 },
      global,
    })
    expect(w.find('.pb-list').text()).toContain('a、b 等 4 项')
  })
})
