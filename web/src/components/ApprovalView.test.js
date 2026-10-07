// 审批中心页的用例。
//
// 只钉住三件最容易做错、且做错了不会报错的事：
//  ① 自己提交的单子不给"批准"按钮（服务端也会拒，但让人白点一次本身就是设计错误）；
//  ② 批准前的确认框必须写明**立即执行**——审批人最容易误判的就是"批了是不是还要再点一下"；
//  ③ "已批准但执行失败"必须报错，不能被当成成功吞掉（否则界面显示批准了、机器上什么都没发生）。
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus, { ElMessage, ElMessageBox } from 'element-plus'
import ApprovalView from './ApprovalView.vue'
import { listApprovals, approveApproval, getApprovalConfig } from '../api/approval'

vi.mock('../api/approval', () => ({
  listApprovals: vi.fn(),
  getApproval: vi.fn(),
  approveApproval: vi.fn(),
  rejectApproval: vi.fn(),
  cancelApproval: vi.fn(),
  getApprovalConfig: vi.fn(),
  saveApprovalConfig: vi.fn(),
}))

vi.mock('element-plus', async (importOriginal) => ({
  ...(await importOriginal()),
  ElMessage: { success: vi.fn(), error: vi.fn(), warning: vi.fn() },
  ElMessageBox: { confirm: vi.fn(), prompt: vi.fn() },
}))

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

const OTHER = {
  id: 'ap-1', title: '重启服务 nginx.service @ web-01', node: 'web-01',
  state: 'pending', requester: 'ops1', mine: false, createdAt: 1791000000000,
}
const MINE = {
  id: 'ap-2', title: '文件分发 @ web-02', node: 'web-02',
  state: 'pending', requester: 'me', mine: true, createdAt: 1791000001000,
}

beforeEach(() => {
  vi.clearAllMocks()
  permissions = ['approval:read']
  listApprovals.mockResolvedValue({ approvals: [OTHER, MINE], enabled: true })
  getApprovalConfig.mockResolvedValue({
    config: { enabled: true, requireFor: ['ops:write'], ttlMinutes: 30, maxRequests: 500 },
    entries: [{ value: 'ops:write', label: '写类操作动作', permission: 'ops:exec' }],
  })
})

async function mountView() {
  const w = mount(ApprovalView, { global: { plugins: [ElementPlus] }, attachTo: document.body })
  await flushPromises()
  return w
}

function buttonsOf(w) {
  return w.findAll('button')
}
function approveButton(w) {
  return buttonsOf(w).find((b) => b.text() === '批准')
}

describe('审批中心', () => {
  it('自己提交的单子不显示"批准"按钮（服务端也会拒，但不该让人白点）', async () => {
    const w = await mountView()
    // 两条单子里只有别人的那条能给"批准"
    expect(approveButton(w)).toBeTruthy()
    const approveBtns = buttonsOf(w).filter((b) => b.text() === '批准')
    expect(approveBtns.length).toBe(1)
    expect(w.text()).toContain('文件分发 @ web-02') // 自己那条仍在列表里
  })

  it('批准前的确认框写明"立即执行"，并把理由发给服务端', async () => {
    ElMessageBox.confirm.mockResolvedValue('confirm')
    approveApproval.mockResolvedValue({ approval: { state: 'executed', taskId: 'ops-9' } })
    const w = await mountView()

    await approveButton(w).trigger('click')
    await flushPromises()

    const msg = ElMessageBox.confirm.mock.calls[0][0]
    expect(msg).toContain('立即执行')
    expect(approveApproval).toHaveBeenCalledWith('ap-1', '')
    expect(ElMessage.success).toHaveBeenCalledWith(expect.stringContaining('ops-9'))
  })

  it('"已批准但执行失败"必须报错，不能显示成成功', async () => {
    ElMessageBox.confirm.mockResolvedValue('confirm')
    approveApproval.mockResolvedValue({
      approval: { state: 'failed', message: '节点 web-01 当前不在线，无法下发防护指令' },
      error: '审批已通过，但动作下发失败：节点 web-01 当前不在线，无法下发防护指令',
    })
    const w = await mountView()

    await approveButton(w).trigger('click')
    await flushPromises()

    expect(ElMessage.error).toHaveBeenCalledWith(expect.stringContaining('动作下发失败'))
    expect(ElMessage.success).not.toHaveBeenCalled()
  })

  it('取消确认框时不发请求（点错了不该把动作执行出去）', async () => {
    ElMessageBox.confirm.mockRejectedValue(new Error('cancel'))
    const w = await mountView()

    await approveButton(w).trigger('click')
    await flushPromises()

    expect(approveApproval).not.toHaveBeenCalled()
  })

  it('审批未开启时明确说明"会直接下发"，避免用户以为动作丢了', async () => {
    listApprovals.mockResolvedValue({ approvals: [], enabled: false })
    const w = await mountView()
    expect(w.text()).toContain('审批未开启')
  })

  it('没有 approval:manage 时不显示「设置」标签页', async () => {
    const w = await mountView()
    expect(w.text()).not.toContain('需要审批的动作')
    // 有权限时设置页可用（这里顺带确认权限判定确实接上了）
    permissions = ['approval:read', 'approval:manage']
    const w2 = await mountView()
    await w2.findAll('.el-tabs__item').find((t) => t.text() === '设置').trigger('click')
    await flushPromises()
    expect(getApprovalConfig).toHaveBeenCalled()
  })
})
