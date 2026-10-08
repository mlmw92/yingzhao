// security.js — 安全中心 API 封装，复用 http 的 get。
import http, { getToken } from './http'

// 安全态势概览：合规评分均值、事件总数、风险主机数、FIM 变化数
export const getSecuritySummary = () => http.get('/api/v1/security/summary')

// 安全事件列表（按时间倒序），支持 limit/category/node 筛选
export const getSecurityEvents = (params = {}) => {
  const q = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== null && v !== '') {
      q.append(k, v)
    }
  }
  const s = q.toString()
  return http.get('/api/v1/security/events' + (s ? '?' + s : ''))
}

// 各主机安全基线评分与检查项明细
export const getSecurityBaselines = () => http.get('/api/v1/security/baselines')

// 各节点「护栏组合风险」：允许写入的目录覆盖了某个可重启单元的可执行文件，
// 或者覆盖了系统自己会执行的目录（systemd/cron/profile.d/sudoers.d/root 的 .ssh）——
// 这两种组合都等于**任意代码执行**，而且不需要任何"任意命令"能力。
//
// 响应里每台机器有 reported 标志：false = 该节点从未上报过这个字段（旧 Agent，压根没检查过），
// 与"已检查、无风险"**必须分开显示**——把前者显示成后者会让人以为那台机器是安全的。
// 每条风险带 description（服务端用 Describe() 生成），界面直接显示，不自己拼措辞。
export const getGuardRisks = () => http.get('/api/v1/security/guard-risks')

// 导出合规矩阵 CSV（节点 × 检查项；该节点没上报的项留空，**空 ≠ 未通过**）。
//
// 与资产导出一致：文件名优先用服务端给的（自带导出时间，同一天导多份不会互相覆盖）；
// 失败时把服务端的报文抛出去——"点了没反应"是最糟的失败形态，
// 而这里恰好有两种常见失败要说清楚：没有 security:export 权限、还没有基线数据。
export async function exportComplianceMatrix(fallbackName = 'compliance-matrix.csv') {
  const token = getToken()
  const res = await fetch('/api/v1/security/baselines/export', {
    headers: token ? { Authorization: 'Bearer ' + token } : {},
  })
  if (!res.ok) {
    let msg = 'HTTP ' + res.status
    try {
      const body = await res.json()
      if (body && body.error) msg = body.error
    } catch (e) {
      /* 非 JSON 响应体：保留状态码文案 */
    }
    throw new Error(msg)
  }
  const disposition = res.headers.get('Content-Disposition') || ''
  const hit = /filename=([^;]+)/.exec(disposition)
  const name = hit ? hit[1].trim() : fallbackName
  const blob = await res.blob()
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = name
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
  return name
}

// 受控 fail2ban 入侵防御：各节点防护状态（含 Agent 是否支持）
export const getDefenseStatuses = () => http.get('/api/v1/security/defense/status')

// 单节点防护状态
export const getDefenseStatus = (node) => http.get(`/api/v1/security/defense/status/${encodeURIComponent(node)}`)

// 创建防护任务：action 为 enable / disable / status
export const postDefenseAction = (node, action) =>
  http.post(`/api/v1/security/defense/${encodeURIComponent(node)}/${encodeURIComponent(action)}`)

// 防护任务列表（可按节点过滤）
export const getDefenseTasks = (node) => {
  const s = node ? `?node=${encodeURIComponent(node)}` : ''
  return http.get('/api/v1/security/defense/tasks' + s)
}

