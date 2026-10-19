// Kafka 展示规格：列/趋势/KPI 按需在此增删，不影响其它类型。
export default {
  type: 'kafka',
  label: 'Kafka',
  emptyTitle: '尚未配置 Kafka 监控',
  emptyHints: [
    '当前没有已采集的 Kafka 实例，请在运行 Agent 的节点上配置 Broker 地址',
    '配置完成后约 15-30 秒数据将出现在此页面',
  ],
  showTopology: true,
  metaColumns: ['role', 'version'],
  kpis: [
    { label: '总 Broker 数', metric: 'kafka_broker_count', agg: 'sum' },
    { label: '总 Topic 数', metric: 'kafka_topic_count', agg: 'sum' },
    { label: '总消费延迟', metric: 'kafka_consumer_lag', agg: 'sum' },
  ],
  columns: [
    { key: 'kafka_broker_count', label: 'Broker 数' },
    { key: 'kafka_topic_count', label: 'Topic 数' },
    { key: 'kafka_partition_count', label: '分区数' },
    { key: 'kafka_under_replicated_partitions', label: '未复制分区' },
    { key: 'kafka_offline_partitions', label: '离线分区' },
    { key: 'kafka_consumer_group_count', label: '消费组' },
    { key: 'kafka_consumer_lag', label: '消费延迟' },
  ],
  trends: [
    { name: '消费积压(最大)', metric: 'kafka_consumer_lag_max', unit: '条' },
    { name: '副本不足分区', metric: 'kafka_under_replicated_partitions', unit: '个' },
  ],
}
