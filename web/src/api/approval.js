// approval.js — 高风险操作双人复核（审批单）API 封装。
//
// 只做路径拼接：权限（approval:read / approve / manage）与资源范围都在服务端校验，
// 前端隐藏按钮只是体验。批准还需要**该动作本身的权限点**，那个判断只有服务端有。
import http from './http'

const withQuery = (path, params = {}) => {
  const q = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== null && v !== '') q.append(k, v)
  }
  const s = q.toString()
  return path + (s ? '?' + s : '')
}

// 审批单列表。box='pending' 为待我审批（服务端已排除我自己提交的——自己不能批自己，
// 列出来只会白点一次）；box='mine' 为我提交的。
// 响应还带 enabled（审批是否开启）与 config，界面据此区分"没开审批"和"开了但没单子"。
export const listApprovals = (params = {}) => http.get(withQuery('/api/v1/approvals', params))

export const getApproval = (id) => http.get('/api/v1/approvals/' + encodeURIComponent(id))

// 批准：服务端会**立即执行**审批单里记下的动作。note 可选。
export const approveApproval = (id, note = '') =>
  http.post('/api/v1/approvals/' + encodeURIComponent(id) + '/approve', { note })

// 驳回：理由必填（服务端强制）。事后回看"为什么不同意"比"谁不同意"更有用。
export const rejectApproval = (id, note) =>
  http.post('/api/v1/approvals/' + encodeURIComponent(id) + '/reject', { note })

// 撤回：只有申请人本人（服务端校验）。
export const cancelApproval = (id) =>
  http.post('/api/v1/approvals/' + encodeURIComponent(id) + '/cancel', {})

export const getApprovalConfig = () => http.get('/api/v1/approvals/config')

export const saveApprovalConfig = (config) => http.put('/api/v1/approvals/config', config)
