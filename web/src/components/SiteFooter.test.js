// PS-29 页脚：删死代码（links/icp 永不生效）+ 补版本号（运维截图问"这是哪版"时看得到）。
import { describe, it, expect, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { useBrand } from '../composables/useBrand'
import SiteFooter from './SiteFooter.vue'
import { WEB_VERSION } from '../version'

describe('站点页脚', () => {
  const { brand } = useBrand()

  beforeEach(() => {
    brand.name = '英招'
    brand.footer = ''
  })

  it('展示品牌名与构建版本号', () => {
    const w = mount(SiteFooter)
    expect(w.find('.site-footer__name').text()).toBe('英招')
    expect(w.find('.site-footer__ver').text()).toBe(`v${WEB_VERSION}`)
    w.unmount()
  })

  it('配置了页脚文案时显示在右侧', () => {
    brand.footer = '内部使用 · 请勿外传'
    const w = mount(SiteFooter)
    expect(w.find('.site-footer__meta').text()).toBe('内部使用 · 请勿外传')
    w.unmount()
  })

  it('未配置页脚文案时不渲染右侧区（也不渲染任何链接死代码）', () => {
    const w = mount(SiteFooter)
    expect(w.find('.site-footer__meta').exists()).toBe(false)
    // links / icp 死代码已删除
    expect(w.findAll('a')).toHaveLength(0)
    w.unmount()
  })
})
