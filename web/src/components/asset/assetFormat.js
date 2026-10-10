// 资产台账的展示口径（列表 / 详情抽屉 / 新建·编辑弹窗 / 关系弹窗四处必须完全一致）。
//
// 为什么单独抽出来：拆子组件后同一句文案会在 3–4 个文件里各写一遍，而"两处措辞不一致"
// 不会报错、只会让人怀疑数据自相矛盾（例如列表写「上报正常」、抽屉写「在线」）。
// 集中在这里，改一处即全站生效。

// 实例的自然键前缀 → 产品名。**只用于中间件实例**：容器/工作负载的自然键是
// <集群>/<命名空间>/<kind>/<名称>，按 ':' 切会切出 apiserver 的 scheme（https）。
export const INSTANCE_LABELS = {
  redis: 'Redis', mysql: 'MySQL', postgres: 'PostgreSQL', mongodb: 'MongoDB', nginx: 'Nginx',
  kafka: 'Kafka', rocketmq: 'RocketMQ', rabbitmq: 'RabbitMQ', kubernetes: 'Kubernetes',
  elasticsearch: 'Elasticsearch', clickhouse: 'ClickHouse', nacos: 'Nacos', zookeeper: 'ZooKeeper',
  fastdfs: 'FastDFS', docker: 'Docker',
}

// 关系对端的类型短名：位置太窄，不复用完整类型标题，但语义必须一致——
// 否则「容器 → 主机」的 runs_on 会被显示成「实例 → 主机」。
export const PEER_TYPE_LABELS = {
  host: '主机', 'middleware-instance': '实例', pod: '容器', workload: '工作负载',
  service: 'K8s 服务', 'middleware-cluster': '中间件集群',
}

// 「上报状态」：描述 Agent 是否还在上报这条资产，与实例/容器自身是否可用无关。
// 措辞刻意避开"在线/离线"——那是采集侧的探活结果，两者同词会让人觉得自相矛盾。
export const STATUS_LABELS = { online: '上报正常', missing: '失联', archived: '归档' }
// 状态 → StatusPill tone（全站统一的状态表达）
export const STATUS_TONE = { online: 'ok', missing: 'danger', archived: 'muted' }

// 容器状态（Docker 采集上报的原始值）中文化：台账里"为什么不可达"要靠它说清
export const CONTAINER_STATUS_LABELS = {
  running: '运行中', exited: '已退出', paused: '已暂停', created: '已创建',
  restarting: '重启中', removing: '删除中', dead: '异常退出',
}

export const SOURCE_LABELS = { auto: '自动', manual: '人工', mixed: '混合' }
export const SOURCE_TONE = { auto: 'muted', manual: 'info', mixed: 'warn' }

// member_of 的落点从"归属集群"改成了"归属工作负载"（Pod → Deployment/StatefulSet/DaemonSet）：
// 集群维度由自然键前缀表达，不再单独建边。标签跟着改成中性的「归属」。
export const KIND_LABELS = {
  runs_on: 'runs_on 运行于', member_of: 'member_of 归属',
  depends_on: 'depends_on 依赖', exposes: 'exposes 暴露',
}
// 关系短名（表格列宽有限）
export const KIND_SHORT = { runs_on: '运行于', member_of: '归属', depends_on: '依赖', exposes: '暴露' }

// 关系类型清单与服务端 asset.LinkKind 对齐：这里的下拉只是可选项，真正的边界在服务端
// （未知类型一律 400）。
export const LINK_KINDS = [
  { value: 'runs_on', label: 'runs_on · 运行于' },
  { value: 'member_of', label: 'member_of · 归属' },
  { value: 'depends_on', label: 'depends_on · 依赖' },
  { value: 'exposes', label: 'exposes · 暴露' },
]

// 责任人的约定属性键，与服务端 asset.OwnerKey 一致
export const OWNER_KEY = 'owner'

export const statusLabel = (s) => STATUS_LABELS[s] || s || '—'
export const sourceLabel = (s) => SOURCE_LABELS[s] || s
export const kindLabel = (k) => KIND_LABELS[k] || k
export const kindShort = (k) => KIND_SHORT[k] || k
export const containerStatusLabel = (s) => CONTAINER_STATUS_LABELS[s] || `容器 ${s}`
export const peerTypeLabel = (t, labelOf) => PEER_TYPE_LABELS[t] || labelOf(t)

// 类型展示：主机固定；实例用自然键前缀（<类型>:<地址>）映射成产品名；
// 其余类型一律用类型标题（来自配置项模型接口）。
export function typeLabel(row, labelOf) {
  if (!row) return ''
  if (row.typeKey === 'middleware-instance') {
    const prefix = String(row.naturalKey || '').split(':')[0]
    return INSTANCE_LABELS[prefix] || prefix || '实例'
  }
  return labelOf(row.typeKey)
}

// 属性值的展示形式：原始布尔/状态码对运维没有阅读价值
export function attrText(key, value) {
  if (value === undefined || value === null || value === '') return ''
  const s = String(value)
  if (key === 'up') return s === 'true' ? '可达' : '不可达'
  if (key === 'status') return containerStatusLabel(s)
  return s
}

export function fmtTime(ts) {
  if (!ts) return '—'
  return new Date(ts).toLocaleString('zh-CN', { hour12: false })
}

// 相对时间：台账看的是「多久没上报了」，绝对时间在列表里反而要多算一步
export function relTime(ts) {
  if (!ts) return '—'
  const diff = Date.now() - ts
  if (diff < 60_000) return '刚刚'
  if (diff < 3600_000) return `${Math.floor(diff / 60_000)} 分钟前`
  if (diff < 86400_000) return `${Math.floor(diff / 3600_000)} 小时前`
  return `${Math.floor(diff / 86400_000)} 天前`
}

// 副标题：主机看 OS 与规格，实例看拓扑/角色与版本；有冲突时优先提示冲突
export function subtitle(row) {
  if (!row) return ''
  if (row.conflictCount > 0) {
    return `自然键 ${row.naturalKey} · ${row.conflictCount} 个字段的人工值与采集值不一致`
  }
  const v = row.values || {}
  if (row.typeKey === 'host') {
    const parts = [row.naturalKey]
    if (v.os) parts.push(v.os)
    if (v.cpuCores) parts.push(`${v.cpuCores}C`)
    if (v.memoryMB) parts.push(`${Math.round(Number(v.memoryMB) / 1024)}G`)
    return parts.join(' · ')
  }
  const parts = []
  if (v.topology) parts.push(v.topology)
  else if (v.role) parts.push(v.role)
  // 这里是「实例/容器自身是否可用」——采集侧的结论，与「上报状态」列是两个维度：
  // 容器退出了，Agent 仍然会把它（连 up=false 一起）每轮上报，于是上报状态依旧是"上报正常"。
  if (v.status) parts.push(containerStatusLabel(v.status))
  else if (v.up) parts.push(v.up === 'true' ? '实例可达' : '实例不可达')
  if (v.version) parts.push(v.version)
  else if (v.image) parts.push(v.image)
  return parts.length ? parts.join(' · ') : row.naturalKey
}

export function rowClass({ row }) {
  if (row.conflictCount > 0) return 'row-conflict'
  if (row.status === 'missing') return 'row-missing'
  return ''
}

// labelList 把标签对象转成可渲染的数组（键排序，展示稳定）
export function labelList(row) {
  const labels = (row && row.labels) || {}
  return Object.keys(labels)
    .sort()
    .map((key) => ({ key, value: labels[key] }))
}

// labelFingerprint 把标签集规范化成可比较的字符串（按键排序）：
// 直接 JSON.stringify 会受键顺序影响，出现"没改却判定改了"的假阳性。
export function labelFingerprint(labels) {
  return Object.keys(labels || {})
    .sort()
    .map((k) => `${k}=${labels[k]}`)
    .join('\n')
}

// 属性字典：从 schema.attrMeta 取该字段的控件类型与枚举项。
// 字典只是编辑体验的加成，取不到就退回自由文本输入，不阻断编辑。
export function attrKind(meta) {
  const kind = ((meta && meta.kind) || '').toLowerCase()
  return ['number', 'date', 'enum', 'bool'].includes(kind) ? kind : ''
}
export function attrEnum(meta) {
  const list = meta && meta.enum
  return Array.isArray(list) ? list.filter((o) => o && o.value) : []
}
