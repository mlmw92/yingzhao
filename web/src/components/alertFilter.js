/* 告警事件列表的筛选口径（纯函数，可单测）。
 *
 * 抽出来的原因：`AlertsView` 里 el-table 在单测中是 stub，行级断言拿不到；
 * 而「首页研判条 → 查看详情」带 node/rule 深链进来的筛选必须能被验证——
 * 否则"点了跳转但列表没过滤"这种静默失效测不出来。
 *
 * state 用告警自身的 state（firing/resolved…），与 AlertsView 的 eventFilter 同口径；
 * node / rule 是精确匹配（深链来自某一条具体告警，模糊匹配会带出无关项）。
 */
export function filterAlerts({ alerts = [], state = '', node = '', rule = '' } = {}) {
  let list = alerts
  if (state) list = list.filter((a) => a.state === state)
  if (node) list = list.filter((a) => a.node === node)
  if (rule) list = list.filter((a) => a.ruleName === rule)
  return list
}

export default filterAlerts
