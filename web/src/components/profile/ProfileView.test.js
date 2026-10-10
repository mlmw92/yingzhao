// 个人中心（ProfileView）回归测试。
//
// 该页已「还原到改造前的样子」：单个 el-card 表单承载 用户名 / 昵称 / 我的角色 / 数据范围，
// 底部一个「保存昵称」按钮（不再是 el-descriptions + 吸底保存条 + 离开拦截）。
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus, { ElMessage } from 'element-plus'
import http from '../../api/http'
import ProfileView from './ProfileView.vue'

vi.mock('../../api/http', () => ({
  default: { updateMe: vi.fn(), get: vi.fn() },
}))

const principal = vi.hoisted(() => ({
  username: 'admin',
  displayName: '超级管理员',
  roles: ['super_admin', 'audit'],
  permissions: [],
  scope: { mode: 'restricted', groups: ['国内'], assetMode: 'limited', assetLabels: [{ key: 'biz', value: 'pay' }] },
  loaded: true,
  assetScopeLabelKey: 'biz',
}))
const loadMe = vi.fn().mockResolvedValue(undefined)
vi.mock('../../composables/useAuth', () => ({
  useAuth: () => ({ principal, loadMe }),
}))

vi.spyOn(ElMessage, 'success').mockImplementation(() => {})

let wrapper

function mountView() {
  wrapper = mount(ProfileView, {
    global: {
      plugins: [ElementPlus],
      stubs: { ChangePasswordSubView: { template: '<div class="stub-pwd" />' } },
    },
  })
  return wrapper
}

beforeEach(() => {
  vi.clearAllMocks()
  http.updateMe.mockResolvedValue({ ok: 'true' })
})

afterEach(() => {
  if (wrapper) wrapper.unmount()
  wrapper = undefined
})

describe('ProfileView 个人中心（还原版）', () => {
  it('基本信息用表单承载：用户名只读、角色与数据范围只读展示', async () => {
    const w = mountView()
    await flushPromises()

    // 用户名是 disabled 输入框
    const usernameInput = w.findAll('.profile-form input')[0]
    expect(usernameInput.element.disabled).toBe(true)
    expect(usernameInput.element.value).toBe('admin')

    const text = w.text()
    expect(text).toContain('超级管理员')
    expect(text).toContain('限定分组')
    expect(text).toContain('国内')
  })

  it('不再是 el-descriptions / 吸底保存条 / 离开拦截那套', async () => {
    const w = mountView()
    await flushPromises()

    expect(w.find('.el-descriptions').exists()).toBe(false)
    expect(w.find('.save-bar').exists()).toBe(false)
  })

  it('点「保存昵称」调用 updateMe 并刷新授权', async () => {
    const w = mountView()
    await flushPromises()

    // 第二个输入框是昵称
    const nickname = w.findAll('.profile-form input')[1]
    await nickname.setValue('新昵称')
    await flushPromises()

    const save = w.findAll('button').find((b) => b.text().includes('保存昵称'))
    expect(save).toBeTruthy()
    await save.trigger('click')
    await flushPromises()

    expect(http.updateMe).toHaveBeenCalledWith({ displayName: '新昵称' })
    expect(loadMe).toHaveBeenCalled()
  })
})
