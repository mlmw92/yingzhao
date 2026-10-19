// FastDFS 展示规格：列/趋势/KPI 按需在此增删，不影响其它类型。
//
// 空间类指标由服务端按字节换算成 MB 返回，这里只管展示格式与聚合，
// 不再参与单位换算。
const GB = 1 / 1024

export default {
  type: 'fastdfs',
  label: 'FastDFS',
  emptyTitle: '尚未配置 FastDFS 监控',
  emptyHints: [
    '当前没有已采集的 FastDFS 实例，请在运行 Agent 的节点上配置 tracker / storage 连接信息，或指向 fastdfs_exporter',
    '配置完成后约 15-30 秒数据将出现在此页面',
  ],
  kpis: [
    { label: '在线 Storage', metric: 'fastdfs_storage_online_count', agg: 'sum' },
    { label: '总空间', metric: 'fastdfs_total_space', agg: 'sum', format: 'mb' },
    { label: '已用空间', metric: 'fastdfs_used_space', agg: 'sum', format: 'mb' },
  ],
  // 角色/分组是实例元信息，不是指标列，由基座按 metaColumns 渲染
  metaColumns: ['role', 'group'],
  columns: [
    { key: 'fastdfs_total_space', label: '总空间', format: 'mb' },
    { key: 'fastdfs_used_space', label: '已用', format: 'mb' },
    { key: 'fastdfs_free_space', label: '空闲', format: 'mb' },
    { key: 'fastdfs_storage_online_count', label: '在线Storage' },
  ],
  trends: [
    {
      name: '空间使用',
      unit: 'GB',
      // 总空间与已用空间同量纲，合到一张图对比（原始值为 MB）
      metrics: [
        { name: '总空间', metric: 'fastdfs_total_space', scale: GB },
        { name: '已用空间', metric: 'fastdfs_used_space', scale: GB },
      ],
    },
  ],
}
