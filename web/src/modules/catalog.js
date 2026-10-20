// 模块目录：侧栏、路由守卫、命令面板与设置页共用的一份定义。
//
// 为什么单独成文件：这些地方都要回答「某个页面属于哪个域、是否被关闭」。
// 各自维护一份清单必然漂移（例如关了模块但命令面板还能搜到）。
import {
  Monitor,
  Connection,
  Grid,
  List,
  Aim,
  View,
  DataLine,
  Files,
  Share,
  Tools,
  Document,
  Bell,
  Histogram,
  Message,
  Lock,
  Setting,
} from '@element-plus/icons-vue'

export const moduleGroups = [
  {
    key: 'monitoring',
    label: '观测监控',
    icon: DataLine,
    items: [
      { key: 'hosts', to: '/hosts', label: '主机列表', icon: Monitor, perm: 'nodes:read' },
      { key: 'middleware', to: '/middleware', label: '中间件监控', icon: Connection, perm: 'middleware:read' },
      // 容器只读管理面：读权限是 container:read（与 middleware:read 分开，见路由注释）
      { key: 'container', to: '/container', label: '容器与工作负载', icon: Grid, perm: 'container:read' },
      { key: 'logs', to: '/logs', label: '集中日志', icon: List, perm: 'logs:read' },
      { key: 'dialtest', to: '/dialtest', label: '服务拨测', icon: Aim, perm: 'probe:read' },
      // 对外状态页入口：页面本身免登录、且在管理壳之外（/status 与 /screen 同级），
      // 所以在管理台里点它应当新开标签页——否则看完外部视角就回不到原来的页面了。
      { key: 'status', to: '/status', label: '对外状态页', icon: View, perm: 'probe:read', newTab: true },
      { key: 'metrics-explore', to: '/metrics/explore', label: '指标浏览', icon: DataLine, perm: 'nodes:read' },
      { key: 'dashboards', to: '/system/dashboards', label: '自定义仪表盘', icon: Grid, perm: 'dashboard:read' },
    ],
  },
  {
    key: 'assets',
    label: '资产与配置',
    icon: Files,
    items: [
      // 读权限即可进入；维护按钮另行按 assets:write 门控（该权限点为高风险，需二次确认）
      { key: 'assets', to: '/assets', label: '资产台账', icon: Files, perm: 'assets:read' },
      // 关系视图：只读（关系的维护仍在台账详情里），同样只需读权限
      { key: 'assets-topology', to: '/assets/topology', label: '关系视图', icon: Share, perm: 'assets:read' },
      // 配置项模型：看类型与字段（含哪些字段参与巡检比对）；改模型另有 assets:write（页面内门控）
      { key: 'assets-model', to: '/assets/model', label: '配置项模型', icon: Grid, perm: 'assets:read' },
      // 配置巡检：读看记录与差异；触发巡检另有 inspect:run（页面内门控）
      { key: 'inspect', to: '/inspect', label: '配置巡检', icon: Aim, perm: 'inspect:read' },
    ],
  },
  {
    key: 'ops',
    label: '运维操作',
    icon: Tools,
    items: [
      // 下行操作：读看任务与动作目录需 ops:read；**下发**另有 ops:exec（高风险，页面内门控）。
      // 能不能真的执行还取决于目标机器自己的 guards.ops——权限只是四道护栏之一。
      { key: 'ops', to: '/ops', label: '节点操作', icon: Tools, perm: 'ops:read' },
      // 审批中心：高风险动作的第二个人在这里点。看单子需 approval:read；
      // 能不能批由服务端再判一次（approval:approve + 动作本身的权限点）。
      { key: 'approvals', to: '/approvals', label: '审批中心', icon: Document, perm: 'approval:read' },
    ],
  },
  {
    key: 'alerting',
    label: '告警运维',
    icon: Bell,
    items: [
      { key: 'alerts', to: '/alerts', label: '告警中心', icon: Bell, perm: 'alerts:read' },
      { key: 'intelligence', to: '/intelligence', label: '智能分析', icon: Histogram, perm: 'nodes:read' },
      { key: 'notify', to: '/notify', label: '通知配置', icon: Message, perm: 'notify:read' },
      { key: 'report', to: '/report', label: '巡检报告', icon: Document, perm: 'report:read' },
    ],
  },
  {
    key: 'security',
    label: '安全治理',
    icon: Lock,
    items: [
      { key: 'security', to: '/security', label: '安全中心', icon: Lock, perm: 'security:read' },
      { key: 'audit', to: '/audit', label: '操作审计', icon: List, perm: 'audit:read' },
    ],
  },
  {
    key: 'system',
    label: '系统设置',
    icon: Setting,
    items: [
      { key: 'settings', to: '/system/settings', label: '站点与品牌' },
      { key: 'profile', to: '/system/profile', label: '个人中心' },
      { key: 'upgrade', to: '/system/upgrade', label: '系统升级', perm: 'system:upgrade' },
      { key: 'users', to: '/system/users', label: '用户管理', perm: 'users:manage' },
      { key: 'roles', to: '/system/roles', label: '角色与权限', perm: 'roles:read' },
    ],
  },
]

// pageDomain 把页面 key 映射到所属域，路由守卫与命令面板共用。
export const pageDomain = Object.fromEntries(
  moduleGroups.flatMap((g) => g.items.map((i) => [i.key, g.key])),
)

// pathModule 把路由 path 映射到页面 key。
// 比给 27 条路由逐条加 meta 更不容易漂移：新增页面本来就该进这份目录。
export const pathModule = Object.fromEntries(
  moduleGroups.flatMap((g) => g.items.map((i) => [i.to, i.key])),
)

// moduleKeyForRoute 返回路由对应的页面 key：
// 优先 meta.module（详情页如 /node/:name 不在侧栏里），其次按 path 查目录。
export function moduleKeyForRoute(route) {
  if (route.meta && route.meta.module) return route.meta.module
  return pathModule[route.path] || ''
}

// 首页不在 groups 里，单独补上域映射（不受任何域开关影响）
pageDomain['overview'] = ''

// 不可关闭项：值班台、告警中心、告警运维整域、系统设置。
// 与后端 uicfg.LockedModuleKeys 保持一致；两边注释互相指明，改动时要同时改。
const LOCKED = new Set(['overview', 'alerts', 'alerting', 'system'])

export function isLocked(key) {
  return LOCKED.has(key)
}
