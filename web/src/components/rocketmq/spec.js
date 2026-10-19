// RocketMQ 展示规格：列/趋势/KPI 按需在此增删，不影响其它类型。
export default {
  type: 'rocketmq',
  label: 'RocketMQ',
  emptyTitle: '尚未配置 RocketMQ 监控',
  emptyHints: [
    '当前没有已采集的 RocketMQ 实例，请在运行 Agent 的节点上配置 Broker 地址',
    '配置完成后约 15-30 秒数据将出现在此页面',
  ],
  // 按 group 聚合的实例拓扑：RocketMQ 的 nameserver / broker 关系靠分组看
  showTopology: true,
  metaColumns: ['role', 'version'],
  kpis: [
    { label: '总 Broker 数', metric: 'rocketmq_broker_count', agg: 'sum' },
    { label: '总 Topic 数', metric: 'rocketmq_topic_count', agg: 'sum' },
    { label: '总消息积压', metric: 'rocketmq_message_accumulation', agg: 'sum' },
  ],
  columns: [
    { key: 'rocketmq_broker_count', label: 'Broker 数' },
    { key: 'rocketmq_topic_count', label: 'Topic 数' },
    { key: 'rocketmq_consumer_group_count', label: '消费组' },
    { key: 'rocketmq_consumer_lag', label: '消费延迟' },
    { key: 'rocketmq_producer_tps', label: '生产 TPS' },
    { key: 'rocketmq_consumer_tps', label: '消费 TPS' },
  ],
  // 积压大于 0 就是需要关注的状态，详情里标红
  badMetrics: ['rocketmq_message_accumulation'],
  trends: [
    { name: '消息积压', metric: 'rocketmq_message_accumulation', unit: '条' },
    { name: '消费延迟', metric: 'rocketmq_consumer_lag', unit: '条' },
  ],
}
