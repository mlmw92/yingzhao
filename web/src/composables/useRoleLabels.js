// 角色名 → 中文展示标签。
//
// 原先是 MainLayout / UsersView / RolesView / ProfileView 各抄一份（四份重复），
// 新增内置角色时极易漏改其中一两处，导致同一个角色在不同页面显示成不同中文名。
// 收敛到这里的单一来源；页面用 `roleLabel(name)` 或直接 `ROLE_LABELS[name] || name`。
export const ROLE_LABELS = {
  super_admin: '超级管理员',
  ops_admin: '运维管理员',
  alert_admin: '告警管理员',
  security_admin: '安全管理员',
  read_only: '只读用户',
  audit: '审计用户',
}

export function roleLabel(name) {
  return ROLE_LABELS[name] || name
}
