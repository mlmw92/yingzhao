import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus, { ElMessage, ElMessageBox } from 'element-plus'
import http from '../api/http'
import UpgradeView from './UpgradeView.vue'

vi.mock('../api/http', () => ({
  default: { get: vi.fn(), post: vi.fn(), upload: vi.fn() },
}))

// 用真实 Element Plus 挂载（要断言表格行）；只把消息提示换成间谍，避免测试里弹提示。
const msgSuccess = vi.spyOn(ElMessage, 'success').mockImplementation(() => {})
const msgError = vi.spyOn(ElMessage, 'error').mockImplementation(() => {})
const msgWarning = vi.spyOn(ElMessage, 'warning').mockImplementation(() => {})
vi.spyOn(ElMessageBox, 'confirm').mockResolvedValue(true)

function mountView() {
  return mount(UpgradeView, {
    global: {
      plugins: [ElementPlus],
      stubs: { 'el-upload': { template: '<div />' } },
    },
  })
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('UpgradeView 升级请求收到 502 时确认实际结果', () => {
  let wrapper
  let history
  let runningVersion

  beforeEach(() => {
    history = []
    runningVersion = '1.27.1'
    http.get.mockImplementation(async (path) => {
      if (path === '/api/v1/version') return { server: runningVersion, buildTime: '' }
      if (path === '/api/v1/system/upgrade/current') return {
        current: { id: 'upgrade-1', version: '1.28.0', status: 'pending', serverArch: 'amd64', components: [], agentArches: [] },
      }
      if (path === '/api/v1/system/upgrade/history') return { history }
      if (path === '/api/v1/system/upgrade/archive') return { versions: [] }
      if (path === '/api/v1/system/geoip') return { source: 'builtin' }
      throw new Error(`未知路径: ${path}`)
    })
    http.post.mockRejectedValue(new Error('HTTP 502'))
  })

  afterEach(() => {
    if (wrapper) wrapper.unmount()
    wrapper = undefined
    vi.useRealTimers()
  })

  async function applyUpgrade() {
    wrapper = mountView()
    await flushPromises()
    vi.useFakeTimers()
    const applyButton = wrapper.findAll('button').find((b) => b.text().includes('立即升级'))
    expect(applyButton).toBeDefined()
    await applyButton.trigger('click')
    await flushPromises()
  }

  it('502 后新版本已运行时提示成功，不误报升级失败', async () => {
    await applyUpgrade()
    runningVersion = '1.28.0'

    await vi.advanceTimersByTimeAsync(1200)

    expect(msgSuccess).toHaveBeenCalledWith('升级已完成，正在刷新页面…')
    expect(msgError).not.toHaveBeenCalled()
  })

  it('502 后版本未变化且升级历史记录失败时展示真实失败原因', async () => {
    await applyUpgrade()
    history = [{ id: 'upgrade-1', version: '1.28.0', action: 'apply', result: 'failed', detail: '替换 web 失败：磁盘空间不足' }]

    await vi.advanceTimersByTimeAsync(41000)

    expect(msgError).toHaveBeenCalledWith(expect.stringContaining('磁盘空间不足'))
    expect(msgSuccess).not.toHaveBeenCalled()
  })

  it('502 后版本未变化且任务记录失败时展示任务错误', async () => {
    const originalGet = http.get.getMockImplementation()
    http.get.mockImplementation(async (path) => {
      if (path === '/api/v1/system/upgrade/current') return {
        current: { id: 'upgrade-1', version: '1.28.0', status: 'failed', error: '备份 server 失败：权限不足', serverArch: 'amd64', components: [], agentArches: [] },
      }
      return originalGet(path)
    })
    await applyUpgrade()

    await vi.advanceTimersByTimeAsync(41000)

    expect(msgError).toHaveBeenCalledWith(expect.stringContaining('权限不足'))
    expect(msgSuccess).not.toHaveBeenCalled()
  })

  it('502 后既无目标版本也无失败记录时提示结果待确认', async () => {
    await applyUpgrade()

    await vi.advanceTimersByTimeAsync(41000)

    expect(msgWarning).toHaveBeenCalledWith(expect.stringContaining('无法确认升级结果'))
    expect(msgError).not.toHaveBeenCalled()
    expect(msgSuccess).not.toHaveBeenCalled()
  })

  it('普通服务端 500 错误直接提示失败，不进入重启确认流程', async () => {
    http.post.mockRejectedValue(new Error('替换 server 失败'))
    await applyUpgrade()

    expect(msgError).toHaveBeenCalledWith('升级失败：替换 server 失败')
    expect(msgSuccess).not.toHaveBeenCalled()
  })
})

// 升级历史与升级包已合并成一张表：一行同时给出「这次做了什么」与「该版本的包还在不在」。
describe('UpgradeView 升级历史与升级包（合并为一张表）', () => {
  let wrapper
  let archive
  let history

  function rowFor(version) {
    return wrapper.findAll('.el-table__row').find((r) => r.text().includes('v' + version))
  }
  function buttonOf(row, label) {
    return row.findAll('button').find((b) => b.text().includes(label))
  }

  beforeEach(() => {
    history = [
      { id: 'h1', version: '1.28.0', action: 'apply', result: 'success', detail: '', at: '2026-10-08T10:00:00Z' },
      { id: 'h2', version: '1.27.1', action: 'apply', result: 'success', detail: '', at: '2026-10-07T10:00:00Z' },
      // 包已被删除的历史版本：只能看记录，不能再切换
      { id: 'h3', version: '1.26.0', action: 'rollback_to', result: 'success', detail: '', at: '2026-10-06T10:00:00Z' },
    ]
    archive = [
      { id: 'a1', version: '1.28.0', size: 1024 * 1024, uploadedAt: '2026-10-08T09:00:00Z' },
      { id: 'a2', version: '1.27.1', size: 2 * 1024 * 1024, uploadedAt: '2026-10-07T09:00:00Z' },
      // 已上传但从未应用（历史里没有）：合并后也必须有一行，否则这个包无处可删
      { id: 'a3', version: '1.29.0', size: 3 * 1024 * 1024, uploadedAt: '2026-10-09T09:00:00Z' },
    ]
    http.get.mockImplementation(async (path) => {
      if (path === '/api/v1/version') return { server: '1.27.1', buildTime: '' }
      if (path === '/api/v1/system/upgrade/current') return { current: null }
      if (path === '/api/v1/system/upgrade/history') return { history }
      if (path === '/api/v1/system/upgrade/archive') return { versions: archive }
      if (path === '/api/v1/system/geoip') return { source: 'builtin' }
      throw new Error(`未知路径: ${path}`)
    })
  })

  afterEach(() => {
    if (wrapper) wrapper.unmount()
    wrapper = undefined
  })

  it('每行都带该版本的升级包状态；包已删除的历史行不能再切换', async () => {
    wrapper = mountView()
    await flushPromises()

    expect(rowFor('1.28.0').text()).toContain('已保留')
    expect(rowFor('1.26.0').text()).toContain('已删除')
    expect(buttonOf(rowFor('1.26.0'), '回退到此版本').attributes('disabled')).toBeDefined()
    expect(buttonOf(rowFor('1.26.0'), '删除').attributes('disabled')).toBeDefined()
    expect(buttonOf(rowFor('1.28.0'), '回退到此版本').attributes('disabled')).toBeUndefined()
  })

  it('已上传未应用的升级包也有独立一行，可切换可删除', async () => {
    wrapper = mountView()
    await flushPromises()

    const row = rowFor('1.29.0')
    expect(row).toBeTruthy()
    expect(row.text()).toContain('上传')
    expect(row.text()).toContain('待应用')
    expect(buttonOf(row, '回退到此版本').attributes('disabled')).toBeUndefined()
    expect(buttonOf(row, '删除').attributes('disabled')).toBeUndefined()
  })

  it('当前运行版本：版本号在前、「当前运行」标记在后，且不能切换、不能删除', async () => {
    wrapper = mountView()
    await flushPromises()

    const row = rowFor('1.27.1')
    const cells = [...row.find('.ver-cell').element.children]
    // 版本号放前面：此前标记在前 + 列宽不足，长版本号会被标记挤到看不见
    expect(cells[0].textContent).toContain('v1.27.1')
    expect(cells[1].textContent).toContain('当前运行')

    expect(buttonOf(row, '回退到此版本').attributes('disabled')).toBeDefined()
    const del = buttonOf(row, '删除')
    expect(del.attributes('disabled')).toBeDefined()
    await del.trigger('click')
    await flushPromises()
    expect(http.post).not.toHaveBeenCalled()
  })

  it('删除升级包后该行的包状态变为已删除，操作随之禁用', async () => {
    http.post.mockImplementation(async (path, body) => {
      if (path === '/api/v1/system/upgrade/archive/delete' && body.version === '1.28.0') {
        archive = archive.filter((p) => p.version !== '1.28.0')
        return { versions: archive }
      }
      throw new Error('未知请求: ' + path)
    })
    wrapper = mountView()
    await flushPromises()

    await buttonOf(rowFor('1.28.0'), '删除').trigger('click')
    await flushPromises()

    expect(http.post).toHaveBeenCalledWith('/api/v1/system/upgrade/archive/delete', { version: '1.28.0' })
    const row = rowFor('1.28.0')
    expect(row.text()).toContain('已删除')
    expect(buttonOf(row, '回退到此版本').attributes('disabled')).toBeDefined()
    expect(buttonOf(row, '删除').attributes('disabled')).toBeDefined()
  })

  it('回退动作显示为「回退」而不是原始动作串', async () => {
    wrapper = mountView()
    await flushPromises()

    expect(rowFor('1.26.0').text()).toContain('回退')
    expect(rowFor('1.26.0').text()).not.toContain('rollback_to')
  })
})
