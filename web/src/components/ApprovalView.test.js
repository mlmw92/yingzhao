// 审批中心页的用例。
//
// 只钉住最容易做错、且做错了不会报错的事：
//  ① 自己提交的单子不给"批准"按钮（服务端也会拒，但让人白点一次本身就是设计错误）；
//  ② 批准前的确认框必须写明**立即执行**——审批人最容易误判的就是"批了是不是还要再点一下"；
//  ③ "已批准但执行失败"必须报错，不能被当成成功吞掉（否则界面显示批准了、机器上什么都没发生）；
//  ④ PS-13 改造新增：待办数徽标、approved/executed 可区分、参数分行、筛选、页面级错误。
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
  document.body.innerHTML = ''
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

  it('目标列：K8s 写显示集群/命名空间，Docker 写显示节点/端点，而不是"系统级动作"', async () => {
    listApprovals.mockResolvedValue({
      enabled: true,
      approvals: [
        {
          id: 'ap-3', title: '重启工作负载 Deployment/web @k3s-dev', node: '',
          params: { cluster: 'k3s-dev', namespace: 'default', kind: 'Deployment', name: 'web' },
          state: 'pending', requester: 'ops1', mine: false, createdAt: 1791000002000,
        },
        {
          id: 'ap-4', title: '停止容器 web-1 @web-01/local-docker', node: 'web-01',
          params: { action: 'stop', node: 'web-01', endpoint: 'local-docker', id: 'web-1' },
          state: 'pending', requester: 'ops1', mine: false, createdAt: 1791000003000,
        },
      ],
    })
    const w = await mountView()
    // K8s 写的目标是集群：node 为空不代表"系统级动作"
    expect(w.text()).toContain('k3s-dev/default')
    // Docker 写：节点 + 端点才有辨识度（同名端点可能出现在多台机器上）
    expect(w.text()).toContain('web-01/local-docker')
    expect(w.text()).not.toContain('系统级动作')
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

  it('没有 approval:manage 时不显示「设置」入口；有权限时点开才加载配置', async () => {
    const w = await mountView()
    expect(w.text()).not.toContain('需要审批的动作')
    expect(buttonsOf(w).find((b) => b.text() === '设置')).toBeUndefined()
    // 没点之前不该去拉配置
    expect(getApprovalConfig).not.toHaveBeenCalled()

    permissions = ['approval:read', 'approval:manage']
    const w2 = await mountView()
    const setting = buttonsOf(w2).find((b) => b.text() === '设置')
    expect(setting).toBeTruthy()
    await setting.trigger('click')
    await flushPromises()

    expect(getApprovalConfig).toHaveBeenCalled()
    expect(w2.text()).toContain('需要审批的动作')
  })
})

describe('审批中心 · PS-13 改造', () => {
  it('「待我审批」Tab 上显示待办数徽标（不切过去也知道有没有活）', async () => {
    const w = await mountView()
    expect(w.find('.tab-count').text()).toBe('2')
  })

  it('没有待办时不显示徽标（0 是个噪音）', async () => {
    listApprovals.mockResolvedValue({ approvals: [], enabled: true })
    const w = await mountView()
    expect(w.find('.tab-count').exists()).toBe(false)
  })

  it('approved（已批准·待执行）与 executed（已执行）可区分：文案与色调都不同', async () => {
    listApprovals.mockResolvedValue({
      approvals: [
        { ...OTHER, id: 'a1', state: 'approved' },
        { ...OTHER, id: 'a2', state: 'executed' },
      ],
      enabled: true,
    })
    const w = await mountView()
    expect(w.text()).toContain('已批准·待执行')
    expect(w.text()).toContain('已执行')

    const pills = w.findAll('.pill')
    expect(pills).toHaveLength(2)
    // 同为 success 色的旧实现无法区分；现在 approved=info、executed=ok
    expect(pills[0].classes()).toContain('info')
    expect(pills[1].classes()).toContain('ok')
  })

  it('驳回 / 已撤回 / 已失效 三者色调不同（不再同为 info）', async () => {
    listApprovals.mockResolvedValue({
      approvals: [
        { ...OTHER, id: 'b1', state: 'rejected' },
        { ...OTHER, id: 'b2', state: 'cancelled' },
        { ...OTHER, id: 'b3', state: 'expired' },
      ],
      enabled: true,
    })
    const w = await mountView()
    const tones = w.findAll('.pill').map((p) => p.classes().join(' '))
    expect(tones[0]).toContain('danger')
    expect(tones[1]).toContain('muted')
    expect(tones[2]).toContain('muted')
  })

  it('抽屉内参数分行渲染，高风险键高亮并带「高风险」标记', async () => {
    listApprovals.mockResolvedValue({
      approvals: [
        {
          ...OTHER, id: 'ap-9', state: 'executed',
          params: { cmd: 'systemctl restart nginx', ttl: 30 },
        },
      ],
      enabled: true,
    })
    const w = await mountView()
    // state=executed → 主按钮是「详情」
    await buttonsOf(w).find((b) => b.text() === '详情').trigger('click')
    await flushPromises()

    // el-drawer 走 teleport 到 body，需在 document 上查
    const paramRows = document.querySelectorAll('.param-row')
    expect(paramRows.length).toBe(2)
    const risky = document.querySelector('.param-row.risky')
    expect(risky).toBeTruthy()
    expect(risky.textContent).toContain('cmd')
    expect(risky.textContent).toContain('高风险')
    expect(risky.textContent).toContain('systemctl restart nginx')
  })

  it('按动作关键词筛选（筛掉不匹配的行）', async () => {
    listApprovals.mockResolvedValue({
      approvals: [
        { ...OTHER, id: 'f1', title: '重启服务 nginx' },
        { ...OTHER, id: 'f2', title: '文件分发到节点' },
      ],
      enabled: true,
    })
    const w = await mountView()
    expect(w.text()).toContain('文件分发到节点')

    await w.find('input[placeholder="动作关键词"]').setValue('nginx')
    await flushPromises()

    expect(w.text()).toContain('重启服务 nginx')
    expect(w.text()).not.toContain('文件分发到节点')
  })

  it('列表加载失败时给页面级错误 + 重试，而不是只弹一次 toast', async () => {
    listApprovals.mockRejectedValueOnce(new Error('后端不可达'))
    const w = await mountView()

    expect(w.text()).toContain('后端不可达')
    const retry = buttonsOf(w).find((b) => b.text() === '重试')
    expect(retry).toBeTruthy()

    listApprovals.mockResolvedValue({ approvals: [OTHER], enabled: true })
    await retry.trigger('click')
    await flushPromises()
    expect(w.text()).toContain('重启服务 nginx.service @ web-01')
  })

  it('待办为空时的空态说明"没有待办"的含义，而不是干巴巴的"暂无数据"', async () => {
    listApprovals.mockResolvedValue({ approvals: [], enabled: true })
    const w = await mountView()
    expect(w.text()).toContain('当前没有待审批的高风险动作')
  })
})
