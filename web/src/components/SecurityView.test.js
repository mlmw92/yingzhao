// 安全中心「合规矩阵导出」的用例。
//
// 为什么只测这一小块：导出按钮的行为有三种失败都不报错——无权限时按钮还显示（点下去才 403）、
// 点击后没真的发请求、失败时什么都不说（用户以为"没反应"而反复点）。
// 下载本身的机制在 api/security.test.js 里测，这里只钉住"按钮何时出现 + 点了干什么 + 失败说不说"。
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus, { ElMessage } from 'element-plus'
import SecurityView from './SecurityView.vue'
import {
  getSecuritySummary,
  getSecurityEvents,
  getSecurityBaselines,
  getDefenseStatuses,
  exportComplianceMatrix,
  getGuardRisks,
} from '../api/security'

vi.mock('../api/security', () => ({
  getSecuritySummary: vi.fn(),
  getSecurityEvents: vi.fn(),
  getSecurityBaselines: vi.fn(),
  getDefenseStatuses: vi.fn(),
  postDefenseAction: vi.fn(),
  exportComplianceMatrix: vi.fn(),
  getGuardRisks: vi.fn(),
}))

vi.mock('element-plus', async (importOriginal) => ({
  ...(await importOriginal()),
  ElMessage: { success: vi.fn(), error: vi.fn(), warning: vi.fn() },
}))

// 权限按用例设置：导出按钮由 security:export 门控（服务端仍然会再校验一次）。
let permissions = []
vi.mock('../composables/useAuth', () => ({
  default: () => ({ can: (perm) => permissions.includes(perm) }),
}))

if (!globalThis.ResizeObserver) {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
}

beforeEach(() => {
  vi.clearAllMocks()
  permissions = []
  getSecuritySummary.mockResolvedValue({ score: 80, eventCount: 0, riskNodes: 0, fimChanges: 0, baselineHosts: 1 })
  getSecurityEvents.mockResolvedValue({ events: [] })
  getSecurityBaselines.mockResolvedValue({
    enabled: true,
    baselines: [{ node: 'web-01', displayName: '美国', score: 60, items: [{ key: 'k1', name: '防火墙已启用', pass: false }] }],
  })
  getDefenseStatuses.mockResolvedValue({ statuses: [] })
  getGuardRisks.mockResolvedValue({ nodes: [], enabled: true })
})

async function mountView() {
  const w = mount(SecurityView, { global: { plugins: [ElementPlus] }, attachTo: document.body })
  await flushPromises()
  return w
}

function exportButton(w) {
  return w.findAll('button').find((b) => b.text().includes('导出矩阵 CSV'))
}

describe('安全中心 · 合规矩阵导出', () => {
  it('没有 security:export 时不显示按钮（而不是点下去才 403）', async () => {
    permissions = ['security:read']
    const w = await mountView()
    expect(exportButton(w)).toBeUndefined()
  })

  it('有权限时显示按钮，点击调用导出并提示文件名', async () => {
    permissions = ['security:read', 'security:export']
    exportComplianceMatrix.mockResolvedValue('compliance-matrix-20261007-120000.csv')
    const w = await mountView()

    const btn = exportButton(w)
    expect(btn).toBeTruthy()
    await btn.trigger('click')
    await flushPromises()

    expect(exportComplianceMatrix).toHaveBeenCalledTimes(1)
    expect(ElMessage.success).toHaveBeenCalledWith(expect.stringContaining('compliance-matrix-20261007-120000.csv'))
  })

  it('导出失败时把服务端报文说出来（"没有数据"不能被静默吞掉）', async () => {
    permissions = ['security:export']
    exportComplianceMatrix.mockRejectedValue(new Error('还没有安全基线数据可导出：请确认 Agent 已上报安全基线'))
    const w = await mountView()

    await exportButton(w).trigger('click')
    await flushPromises()

    expect(ElMessage.error).toHaveBeenCalledWith(expect.stringContaining('还没有安全基线数据可导出'))
    expect(ElMessage.success).not.toHaveBeenCalled()
  })
})

// 护栏组合风险（"两个护栏配在一起等于任意代码执行"）。
//
// 只钉住三件做错了不会报错的事：
//  ① 无风险且都已上报时**整卡不显示**（否则每次打开都有一张空卡，久了就没人看它）；
//  ② 有风险时显示的是**服务端给的人话**（界面不自己拼措辞，否则与 Agent 日志两处会漂移）；
//  ③ **未上报 ≠ 无风险**——把"我们不知道"显示成"安全"是这张卡最坏的失败形态。
describe('安全中心 · 护栏组合风险', () => {
  it('无风险且都已上报时不显示这张卡（避免空卡变成噪音）', async () => {
    getGuardRisks.mockResolvedValue({
      nodes: [{ node: 'web-01', reported: true, risks: [] }],
      enabled: true,
    })
    const w = await mountView()
    expect(w.text()).not.toContain('护栏组合风险')
  })

  it('有风险时显示，并原样使用服务端给的人话描述', async () => {
    const desc = '护栏组合风险：允许写入的目录「/etc/cron.d」覆盖了系统会自动执行的目录「/etc/cron.d」——写入即持久化，不需要重启任何服务'
    getGuardRisks.mockResolvedValue({
      nodes: [{
        node: 'web-01', displayName: '美国', nodeIp: '10.0.0.1', reported: true,
        risks: [{ kind: 'persistence', allowedDir: '/etc/cron.d', exposed: '/etc/cron.d', description: desc }],
      }],
      enabled: true,
    })
    const w = await mountView()

    expect(w.text()).toContain('护栏组合风险')
    expect(w.text()).toContain(desc) // 服务端给的整句，界面不重新拼
    expect(w.text()).toContain('写入即持久化')
    // 卡片头部要告诉人**怎么消掉**（只告警不拒绝是刻意的，但不能只告警不说下一步）
    expect(w.text()).toContain('guards.ops')
  })

  it('未上报的机器显示成「未知」，不能显示成「无风险」', async () => {
    getGuardRisks.mockResolvedValue({
      nodes: [
        { node: 'old-01', reported: false, risks: [] },
        { node: 'web-01', reported: true, risks: [] },
      ],
      enabled: true,
    })
    const w = await mountView()

    expect(w.text()).toContain('护栏组合风险')
    expect(w.text()).toContain('未上报该检查')
    expect(w.text()).toContain('未知')
    expect(w.text()).toContain('Agent 需升级')
  })
})

// 错误拆分（B4）+ 加载态（B5）。
//
// 原实现五块共用一个 loadError：Promise.all 并发下任一模块成功就把它清空 →
// 部分失败被静默吞掉（界面看起来"全绿"，其实有一块根本没拿到数据）。
// 只钉住"一个模块失败时，错误出现在**它自己**那张卡上，且不影响其他卡"。
describe('安全中心 · 错误拆分与加载态', () => {
  it('单个模块失败时错误只落在该卡，其他卡照常渲染（B4：部分失败不被吞掉）', async () => {
    getSecuritySummary.mockRejectedValue(new Error('安全概览接口 500'))
    getSecurityEvents.mockResolvedValue({ events: [] })
    const w = await mountView()

    // 概览卡显示自己的错误
    expect(w.text()).toContain('安全概览接口 500')
    // 事件卡不受牵连：仍显示空态而不是错误
    expect(w.text()).toContain('暂无安全事件')
  })

  it('多个模块失败时页面级提示给出失败模块数（一眼看到几块挂了）', async () => {
    getSecuritySummary.mockRejectedValue(new Error('概览挂了'))
    getSecurityBaselines.mockRejectedValue(new Error('基线挂了'))
    const w = await mountView()

    expect(w.text()).toContain('2 个模块加载失败')
    expect(w.text()).toContain('概览挂了')
    expect(w.text()).toContain('基线挂了')
  })
})

// 修复建议：从"知道有问题"推进到"知道怎么改"。
describe('安全中心 · 修复建议', () => {
  it('基线未通过项给出整改指引（不再只有"未通过"）', async () => {
    getSecurityBaselines.mockResolvedValue({
      enabled: true,
      baselines: [{
        node: 'web-01',
        score: 60,
        items: [{ key: 'firewall_enabled', name: '防火墙已启用', pass: false }],
      }],
    })
    const w = await mountView()

    expect(w.text()).toContain('未通过')
    expect(w.text()).toContain('整改：')
    expect(w.text()).toContain('ufw enable')
  })

  it('通过项不显示整改指引', async () => {
    getSecurityBaselines.mockResolvedValue({
      enabled: true,
      baselines: [{ node: 'web-01', score: 100, items: [{ key: 'firewall_enabled', name: '防火墙已启用', pass: true }] }],
    })
    const w = await mountView()

    expect(w.text()).not.toContain('整改：')
  })

  it('安全事件按类别挂处置建议（可 hover 查看）', async () => {
    getSecurityEvents.mockResolvedValue({
      events: [{
        id: 'e1', node: 'web-01', category: 'ssh_bruteforce', severity: 'critical',
        message: 'SSH 暴力破解', timestamp: 1700000000000,
      }],
    })
    const w = await mountView()

    expect(w.find('.cat-tag.has-fix').exists()).toBe(true)
  })
})
