// 中间件详情抽屉的时间范围档位（RedisTab 既有 5 档，其余 Tab 统一补齐）。
// 独立成模块：既能被 MwDetailHeader 复用，也便于单测直接引用。
export const MW_RANGE_OPTIONS = [
  { label: '近5分钟', value: '5m' },
  { label: '近15分钟', value: '15m' },
  { label: '近1小时', value: '1h' },
  { label: '近6小时', value: '6h' },
  { label: '近24小时', value: '24h' },
]

// 时间范围值 → 毫秒跨度
const RANGE_MS = {
  '5m': 5 * 60 * 1000,
  '15m': 15 * 60 * 1000,
  '1h': 60 * 60 * 1000,
  '6h': 6 * 60 * 60 * 1000,
  '24h': 24 * 60 * 60 * 1000,
}

// 把范围值换算成 [start, end]（毫秒时间戳）。未知值回退到 1 小时。
export function rangeToWindow(range, now) {
  const end = now == null ? Date.now() : now
  const span = RANGE_MS[range] || RANGE_MS['1h']
  return { start: end - span, end }
}

// 按范围跨度决定采样步长（**毫秒**），避免 24h 用 60s 步长拉回上千点。
//
// 单位必须是毫秒：/api/v1/query/range 的 step 契约是毫秒（见 query.go
// "step 必须为合法毫秒数" 与 "step 必须在 1 秒到查询跨度之间"）。此前这里返回秒
// （60/120/300），经 9 个中间件 Tab 直接拼进 URL 后，后端判 `step < 1000` 一律 400，
// 表现为所有抽屉趋势图恒显"趋势数据加载失败"。这里统一改为毫秒。
// 每档目标约 60–300 个点：
//   5m→5s、15m→15s、1h→60s、6h→120s、24h→300s
export function rangeToStep(range) {
  if (range === '24h') return 300000
  if (range === '6h') return 120000
  if (range === '15m') return 15000
  if (range === '5m') return 5000
  return 60000
}
