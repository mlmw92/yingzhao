// 修改密码（PS-28 复用件）回归测试。
//
// 关键点：前端校验必须与后端 auth.ValidatePassword 对齐（≥8 位 + ≥2 类字符），
// 否则用户输入 6 位密码时前端放行、后端才拒绝，白跑一趟往返。
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick } from 'vue'
import ElementPlus, { ElMessage } from 'element-plus'
import http from '../../api/http'
import ChangePasswordSubView from './ChangePasswordSubView.vue'

vi.mock('../../api/http', () => ({
  default: { changePassword: vi.fn() },
}))

vi.spyOn(ElMessage, 'success').mockImplementation(() => {})
vi.spyOn(ElMessage, 'error').mockImplementation(() => {})

let wrapper

function mountView() {
  wrapper = mount(ChangePasswordSubView, { global: { plugins: [ElementPlus] } })
  return wrapper
}

// 按 label 定位输入框：三个密码框都是 type=password，不能靠顺序
function inputFor(labelText) {
  const item = wrapper.findAll('.el-form-item').find((i) => i.text().includes(labelText))
  return item.find('input')
}

// EP 的 el-form-item 错误文案是 debounce 100ms 后才渲染的（validateStateDebounced），
// 断言前必须等过这个窗口，否则拿到的是"还没显示"的假阴性。
async function errorOf(labelText) {
  await new Promise((r) => setTimeout(r, 130))
  await nextTick()
  const item = wrapper.findAll('.el-form-item').find((i) => i.text().includes(labelText))
  const err = item.find('.el-form-item__error')
  return err.exists() ? err.text() : ''
}

async function fill(oldPw, newPw, confirm) {
  await inputFor('原密码').setValue(oldPw)
  await inputFor('新密码').setValue(newPw)
  await inputFor('确认新密码').setValue(confirm)
  await flushPromises()
}

async function submit() {
  const btn = wrapper.findAll('button').find((b) => b.text().includes('保存修改'))
  await btn.trigger('click')
  await flushPromises()
}

beforeEach(() => {
  vi.clearAllMocks()
  http.changePassword.mockResolvedValue({ ok: 'true' })
})

afterEach(() => {
  if (wrapper) wrapper.unmount()
  wrapper = undefined
})

describe('ChangePasswordSubView 校验与后端对齐', () => {
  it('少于 8 位直接拦下，不发请求', async () => {
    mountView()
    await fill('old-pass-1', 'Ab1!', 'Ab1!')
    await submit()

    expect(await errorOf('新密码')).toContain('密码至少 8 位')
    expect(http.changePassword).not.toHaveBeenCalled()
  })

  it('8 位但只有一类字符（纯小写）被拒，提示需两类', async () => {
    mountView()
    await fill('old-pass-1', 'abcdefgh', 'abcdefgh')
    await submit()

    expect(await errorOf('新密码')).toContain('两类')
    expect(http.changePassword).not.toHaveBeenCalled()
  })

  it('≥8 位且含两类字符通过校验并提交', async () => {
    mountView()
    await fill('old-pass-1', 'abcd1234', 'abcd1234')
    await submit()

    expect(http.changePassword).toHaveBeenCalledWith('old-pass-1', 'abcd1234')
  })

  it('两次输入不一致时拦下', async () => {
    mountView()
    await fill('old-pass-1', 'abcd1234', 'abcd12345')
    await submit()

    expect(await errorOf('确认新密码')).toContain('不一致')
    expect(http.changePassword).not.toHaveBeenCalled()
  })

  it('后端「原密码错误」就地回填到原密码字段', async () => {
    http.changePassword.mockRejectedValue(new Error('原密码错误'))
    mountView()
    await fill('wrong-old', 'abcd1234', 'abcd1234')
    await submit()

    expect(await errorOf('原密码')).toContain('原密码错误')
  })
})

