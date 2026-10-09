import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { ElMessage } from 'element-plus'
import http from '../api/http'
import UpgradeView from './UpgradeView.vue'

vi.mock('../api/http', () => ({
  default: { get: vi.fn(), post: vi.fn(), upload: vi.fn() },
}))

vi.mock('element-plus', () => ({
  ElMessage: { success: vi.fn(), error: vi.fn(), warning: vi.fn() },
  ElMessageBox: { confirm: vi.fn(async () => true) },
}))

// 透出 disabled，便于断言按钮可用性（Element Plus 的 el-button 用 prop 而非原生属性）
const ButtonStub = {
  name: 'ElButton',
  props: ['disabled'],
  template: '<button :disabled="disabled"><slot /></button>',
}
const NoopStub = { name: 'Noop', template: '<div />' }

function mountView() {
  return mount(UpgradeView, {
    global: {
      stubs: {
        'el-button': ButtonStub,
        'el-upload': NoopStub,
        'el-table': NoopStub,
        'el-table-column': NoopStub,
        'el-pagination': NoopStub,
        'el-tag': NoopStub,
        'el-alert': NoopStub,
        'el-input': NoopStub,
        'el-progress': NoopStub,
        'el-link': NoopStub,
        'el-icon': NoopStub,
      },
      directives: { loading: {} },
    },
  })
}

describe('UpgradeView 升级请求收到 502 时确认实际结果', () => {
  let wrapper
  let history
  let runningVersion

  beforeEach(() => {
    vi.clearAllMocks()
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

    expect(ElMessage.success).toHaveBeenCalledWith('升级已完成，正在刷新页面…')
    expect(ElMessage.error).not.toHaveBeenCalled()
  })

  it('502 后版本未变化且升级历史记录失败时展示真实失败原因', async () => {
    await applyUpgrade()
    history = [{ id: 'upgrade-1', version: '1.28.0', action: 'apply', result: 'failed', detail: '替换 web 失败：磁盘空间不足' }]

    await vi.advanceTimersByTimeAsync(41000)

    expect(ElMessage.error).toHaveBeenCalledWith(expect.stringContaining('磁盘空间不足'))
    expect(ElMessage.success).not.toHaveBeenCalled()
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

    expect(ElMessage.error).toHaveBeenCalledWith(expect.stringContaining('权限不足'))
    expect(ElMessage.success).not.toHaveBeenCalled()
  })

  it('502 后既无目标版本也无失败记录时提示结果待确认', async () => {
    await applyUpgrade()

    await vi.advanceTimersByTimeAsync(41000)

    expect(ElMessage.warning).toHaveBeenCalledWith(expect.stringContaining('无法确认升级结果'))
    expect(ElMessage.error).not.toHaveBeenCalled()
    expect(ElMessage.success).not.toHaveBeenCalled()
  })

  it('普通服务端 500 错误直接提示失败，不进入重启确认流程', async () => {
    http.post.mockRejectedValue(new Error('替换 server 失败'))
    await applyUpgrade()

    expect(ElMessage.error).toHaveBeenCalledWith('升级失败：替换 server 失败')
    expect(ElMessage.success).not.toHaveBeenCalled()
  })
})

describe('UpgradeView 升级包：未删除则可切换，删除后不可切换', () => {
  let wrapper
  let archive

  function pkgRow(version) {
    return wrapper.findAll('.pkg-row').find((r) => r.text().includes('v' + version))
  }
  function buttonOf(row, label) {
    return row.findAll('button').find((b) => b.text().includes(label))
  }

  beforeEach(() => {
    vi.clearAllMocks()
    archive = [
      { id: 'a1', version: '1.28.0', size: 1024 * 1024, uploadedAt: '2026-10-08T10:00:00Z' },
      { id: 'a2', version: '1.27.1', size: 2 * 1024 * 1024, uploadedAt: '2026-10-07T10:00:00Z' },
    ]
    http.get.mockImplementation(async (path) => {
      if (path === '/api/v1/version') return { server: '1.27.1', buildTime: '' }
      if (path === '/api/v1/system/upgrade/current') return { current: null }
      if (path === '/api/v1/system/upgrade/history') return { history: [] }
      if (path === '/api/v1/system/upgrade/archive') return { versions: archive }
      if (path === '/api/v1/system/geoip') return { source: 'builtin' }
      throw new Error(`未知路径: ${path}`)
    })
  })

  afterEach(() => {
    if (wrapper) wrapper.unmount()
    wrapper = undefined
  })

  it('已保留的升级包均可切换，当前运行版本的切换按钮禁用', async () => {
    wrapper = mountView()
    await flushPromises()

    expect(pkgRow('1.28.0')).toBeTruthy()
    expect(buttonOf(pkgRow('1.28.0'), '切换').attributes('disabled')).toBeUndefined()
    expect(buttonOf(pkgRow('1.27.1'), '切换').attributes('disabled')).toBeDefined()
  })

  it('删除升级包后该版本从清单消失，也不再提供切换', async () => {
    http.post.mockImplementation(async (path, body) => {
      if (path === '/api/v1/system/upgrade/archive/delete' && body.version === '1.28.0') {
        archive = archive.filter((p) => p.version !== '1.28.0')
        return { versions: archive }
      }
      throw new Error('未知请求: ' + path)
    })
    wrapper = mountView()
    await flushPromises()

    await buttonOf(pkgRow('1.28.0'), '删除').trigger('click')
    await flushPromises()

    expect(http.post).toHaveBeenCalledWith('/api/v1/system/upgrade/archive/delete', { version: '1.28.0' })
    expect(pkgRow('1.28.0')).toBeUndefined()
    expect(pkgRow('1.27.1')).toBeTruthy()
  })
})
