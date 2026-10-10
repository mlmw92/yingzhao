// 首页两个告警区块的去重逻辑（§33 #7 第一步）。
//
// 「紧急告警」（span 4）本质上是「最近告警」（span 12）的子集，两个区块同屏时会把
// 同一条告警渲染两次。这里把去重规则单独抽出来，原因有二：
//   1. 它是纯函数，可以脱离 OverviewView 单测（首页要挂载 ECharts / 十几个子组件，测不起）；
//   2. 「区块被隐藏时要还原完整列表」这条分支很容易在后继改动里被悄悄删掉——
//      一旦删掉，用户隐藏紧急告警区块后，最近告警也会跟着少几条，属于静默丢信息。
//
// 返回：{ list: 最近告警要显示的条目, deduped: 因重复被剔除的条数 }
export function alertKey(a, i) {
  if (!a) return ''
  // 优先用后端 id；没 id 时退化为「规则 + 触发时间」组合，与列表 :key 的退化规则保持一致
  return a.id || `${a.ruleId || a.ruleName}-${a.startsAt || i}`
}

function byTimeDesc(list) {
  return [...list].sort((a, b) => new Date(b?.startsAt || 0) - new Date(a?.startsAt || 0))
}

export function dedupeRecentAlerts({ alerts = [], critical = [], criticalVisible = true, limit = 12 } = {}) {
  const sorted = byTimeDesc(alerts)
  // 紧急告警区块不可见 → 不做任何剔除，否则等于把信息藏起来了
  if (!criticalVisible) return { list: sorted.slice(0, limit), deduped: 0 }
  const shown = new Set(critical.map((a, i) => alertKey(a, i)))
  const kept = []
  let deduped = 0
  for (let i = 0; i < sorted.length; i++) {
    const a = sorted[i]
    // 只在「原本会进入最近告警前 limit 条」的范围内计数，
    // 否则 deduped 会统计到那些本来就不会显示的老告警，提示文案的条数就对不上了
    if (shown.has(alertKey(a, i))) {
      if (kept.length < limit) deduped++
      continue
    }
    if (kept.length < limit) kept.push(a)
  }
  return { list: kept, deduped }
}
