// MongoDB 展示规格：列/趋势/KPI 按需在此增删，不影响其它类型。
// 字节类指标在服务端已按 Scale 换算成 MB；趋势图直接查时序库，需在这里再换算。
const TO_MB = 1 / 1024 / 1024

export default {
  type: 'mongodb',
  label: 'MongoDB',
  emptyTitle: '尚未配置 MongoDB 监控',
  emptyHints: [
    '当前没有已采集的 MongoDB 实例，请在运行 Agent 的节点上配置连接信息',
    '配置完成后约 15-30 秒数据将出现在此页面',
  ],
  metaColumns: ['role', 'topology', 'version'],
  kpis: [
    { label: '当前总连接数', metric: 'mongodb_connections_current', agg: 'sum' },
    { label: '文档总数', metric: 'mongodb_db_objects', agg: 'sum' },
    { label: '库数据总量', metric: 'mongodb_db_dataSize_bytes', agg: 'sum', format: 'mb' },
  ],
  columns: [
    { key: 'mongodb_connections_current', label: '连接数' },
    { key: 'mongodb_mem_resident_bytes', label: '常驻内存', format: 'mb' },
    { key: 'mongodb_db_dataSize_bytes', label: '数据大小', format: 'mb' },
    { key: 'mongodb_db_objects', label: '文档数' },
    { key: 'mongodb_repl_lag', label: '复制延迟' },
  ],
  trends: [
    { name: '连接数', metric: 'mongodb_connections_current', unit: '个' },
    {
      name: '常驻内存',
      unit: 'MB',
      metrics: [{ name: '常驻内存', metric: 'mongodb_mem_resident_bytes', scale: TO_MB }],
    },
  ],
}
