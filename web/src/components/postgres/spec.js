// PostgreSQL 展示规格：列/趋势/KPI 按需在此增删，不影响其它类型。
export default {
  type: 'postgres',
  label: 'PostgreSQL',
  emptyTitle: '尚未配置 PostgreSQL 监控',
  emptyHints: [
    '当前没有已采集的 PostgreSQL 实例，请在运行 Agent 的节点上配置连接信息',
    '配置完成后约 15-30 秒数据将出现在此页面',
  ],
  showTopology: true,
  metaColumns: ['role', 'topology', 'version', 'database'],
  kpis: [
    { label: '总连接数', metric: 'postgres_numbackends', agg: 'sum' },
    { label: '总事务提交', metric: 'postgres_xact_commit', agg: 'sum' },
    { label: '总死锁数', metric: 'postgres_deadlocks', agg: 'sum' },
  ],
  columns: [
    { key: 'postgres_numbackends', label: '连接数' },
    { key: 'postgres_cache_hit_ratio', label: '缓存命中率' },
    { key: 'postgres_deadlocks', label: '死锁' },
    { key: 'postgres_replication_lag_bytes', label: '复制延迟(B)' },
    { key: 'postgres_database_size_bytes', label: '数据库大小' },
  ],
  trends: [
    { name: '连接数', metric: 'postgres_numbackends', unit: '个' },
    { name: '缓存命中率', metric: 'postgres_cache_hit_ratio', unit: '%' },
  ],
}
