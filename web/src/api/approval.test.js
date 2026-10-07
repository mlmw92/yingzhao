// 审批 API 客户端的用例：只钉住"路径与载荷对不对"。
// 权限与资源范围都在服务端校验，前端这几行只是把参数摆对。
import { describe, it, expect, beforeEach, vi } from 'vitest'

const get = vi.fn()
const post = vi.fn()
const put = vi.fn()
vi.mock('./http', () => ({ default: { get, post, put, del: vi.fn(), upload: vi.fn() } }))

const {
  listApprovals, getApproval, approveApproval, rejectApproval, cancelApproval,
  getApprovalConfig, saveApprovalConfig,
} = await import('./approval')

beforeEach(() => {
  get.mockReset().mockResolvedValue({})
  post.mockReset().mockResolvedValue({})
  put.mockReset().mockResolvedValue({})
})

describe('api/approval', () => {
  it('列表带上 box 与状态过滤，空值不拼进 query', async () => {
    await listApprovals({ box: 'pending', state: '' })
    expect(get).toHaveBeenCalledWith('/api/v1/approvals?box=pending')
  })

  it('批准把理由作为 note 发出去（服务端会立即执行这条单子里的动作）', async () => {
    await approveApproval('ap-7', '已核对变更单')
    expect(post).toHaveBeenCalledWith('/api/v1/approvals/ap-7/approve', { note: '已核对变更单' })
  })

  it('驳回必须带理由；单号要转义（防止拼出别的路径）', async () => {
    await rejectApproval('ap/7', '窗口外')
    expect(post).toHaveBeenCalledWith('/api/v1/approvals/ap%2F7/reject', { note: '窗口外' })
  })

  it('撤回与详情走各自的路径', async () => {
    await cancelApproval('ap-9')
    expect(post).toHaveBeenCalledWith('/api/v1/approvals/ap-9/cancel', {})
    await getApproval('ap-9')
    expect(get).toHaveBeenCalledWith('/api/v1/approvals/ap-9')
  })

  it('配置读写：读用 GET、存用 PUT（保存即热生效）', async () => {
    await getApprovalConfig()
    expect(get).toHaveBeenCalledWith('/api/v1/approvals/config')
    await saveApprovalConfig({ enabled: true, requireFor: ['ops:write'], ttlMinutes: 30, maxRequests: 500 })
    expect(put).toHaveBeenCalledWith('/api/v1/approvals/config',
      { enabled: true, requireFor: ['ops:write'], ttlMinutes: 30, maxRequests: 500 })
  })
})
