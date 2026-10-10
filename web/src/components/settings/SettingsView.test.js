// 「系统设置」容器层（PS-26）的回归测试。
//
// 为什么单钉容器层：13 个设置类子页各自管理自己的表单，容器只负责三件事——
//   ① 导航项键盘可达（button + role=tab + aria-selected）
//   ② 切换分类写回 URL（刷新 / 分享 / 前进后退都能复现当前分类）
//   ③ 子页 dirty 契约驱动吸底「未保存改动」条（保存 / 放弃改动的唯一入口）
// 这三件事都在容器里，子页测试覆盖不到。
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { ref, h, nextTick } from 'vue'
import SettingsView from './SettingsView.vue'

const rtr = vi.hoisted(() => ({ replace: null, route: { query: {} } }))
vi.mock('vue-router', () => ({
  useRoute: () => rtr.route,
  useRouter: () => ({ replace: rtr.replace }),
}))

// 子页统一替换成暴露 { dirty, save, reset } 的桩：容器只认这个契约
const pane = { dirty: ref(false), save: 0, reset: 0 }
function makeStub(name) {
  return {
    name,
    setup(_, { expose }) {
      expose({
        dirty: pane.dirty,
        save: () => {
          pane.save++
        },
        reset: () => {
          pane.reset++
          pane.dirty.value = false
        },
      })
      return () => h('div', { class: 'stub-pane' }, name)
    },
  }
}

let wrapper

function mountView() {
  wrapper = mount(SettingsView, {
    global: {
      plugins: [ElementPlus],
      stubs: {
        BrandSettingsSubView: makeStub('BrandSettingsSubView'),
        AlertPipelineSubView: makeStub('AlertPipelineSubView'),
        SelfMonitorSubView: makeStub('SelfMonitorSubView'),
        RetentionSubView: makeStub('RetentionSubView'),
        AISettingsSubView: makeStub('AISettingsSubView'),
      },
    },
  })
  return wrapper
}

beforeEach(() => {
  pane.dirty.value = false
  pane.save = 0
  pane.reset = 0
  rtr.replace = vi.fn()
  rtr.route.query = {}
})

afterEach(() => {
  if (wrapper) wrapper.unmount()
  wrapper = undefined
})

describe('SettingsView 容器层', () => {
  it('导航项是 button[role=tab]，带 aria-selected，键盘可达', async () => {
    const w = mountView()
    await flushPromises()

    const tabs = w.findAll('button[role="tab"]')
    expect(tabs.length).toBe(5)
    // 默认选中第一个
    expect(tabs[0].attributes('aria-selected')).toBe('true')
    expect(tabs[1].attributes('aria-selected')).toBe('false')
    // 非选中项 tabindex=-1，选中项可聚焦（roving tabindex）
    expect(tabs[0].attributes('tabindex')).toBe('0')
    expect(tabs[1].attributes('tabindex')).toBe('-1')
  })

  it('切换分类写回 URL（tab 查询参数）', async () => {
    const w = mountView()
    await flushPromises()

    const tabs = w.findAll('button[role="tab"]')
    await tabs[2].trigger('click')
    await flushPromises()

    expect(rtr.replace).toHaveBeenCalledTimes(1)
    expect(rtr.replace.mock.calls[0][0].query.tab).toBe('selfmon')
  })

  it('子页 dirty 时显示吸底「未保存改动」条，保存 / 放弃走子页契约', async () => {
    const w = mountView()
    await flushPromises()

    // 初始不脏：没有吸底条
    expect(w.find('.save-bar').exists()).toBe(false)

    // 子页报告 dirty → 容器渲染吸底条
    pane.dirty.value = true
    await nextTick()
    expect(w.find('.save-bar').exists()).toBe(true)
    expect(w.text()).toContain('未保存的改动')

    // 保存：调用子页 save
    const saveBtn = w.findAll('.save-bar button').find((b) => b.text().includes('保存'))
    await saveBtn.trigger('click')
    await flushPromises()
    expect(pane.save).toBe(1)

    // 放弃改动：调用子页 reset
    const discardBtn = w.findAll('.save-bar button').find((b) => b.text().includes('放弃改动'))
    await discardBtn.trigger('click')
    await flushPromises()
    expect(pane.reset).toBe(1)
  })
})
