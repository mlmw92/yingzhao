// Nginx 展示规格：列/趋势/KPI 按需在此增删，不影响其它类型。
export default {
  type: 'nginx',
  label: 'Nginx',
  emptyTitle: '尚未配置 Nginx 监控',
  emptyHints: [
    '当前没有已采集的 Nginx 实例，请在运行 Agent 的节点上启用 Nginx stub_status 模块',
    '配置完成后约 15-30 秒数据将出现在此页面',
  ],
  showTopology: true,
  // 实例地址展示节点 IP 比 127.0.0.1:80 更有辨识度
  instanceDisplay: 'nodeIp',
  metaColumns: ['version'],
  kpis: [
    { label: '总活动连接', metric: 'nginx_active_connections', agg: 'sum' },
    { label: '总请求', metric: 'nginx_requests', agg: 'sum', format: 'compact' },
    { label: '总等待连接', metric: 'nginx_waiting', agg: 'sum' },
  ],
  columns: [
    { key: 'nginx_active_connections', label: '活动连接' },
    { key: 'nginx_accepts', label: '已接收', format: 'compact' },
    { key: 'nginx_handled', label: '已处理', format: 'compact' },
    { key: 'nginx_requests', label: '总请求', format: 'compact' },
    { key: 'nginx_reading', label: '读取' },
    { key: 'nginx_writing', label: '写入' },
    { key: 'nginx_waiting', label: '等待' },
  ],
  trends: [
    { name: '活动连接', metric: 'nginx_active_connections', unit: '个' },
    { name: '等待连接', metric: 'nginx_waiting', unit: '个' },
  ],
}
