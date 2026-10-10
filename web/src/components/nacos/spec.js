// Nacos 展示规格（指标来自 Nacos 2.x 的 /actuator/prometheus 端点；
// 需在 Nacos 侧开启：nacos.prometheus.metrics.enabled=true 与
// management.endpoints.web.exposure.include=prometheus）。
export default {
  type: 'nacos',
  label: 'Nacos',
  kpis: [
    { label: '长连接数', metric: 'nacos_long_connection', agg: 'sum' },
    { label: '服务数', metric: 'nacos_naming_service_count', agg: 'sum' },
    { label: '配置数', metric: 'nacos_config_count', agg: 'sum' },
  ],
  columns: [
    { key: 'nacos_long_connection', label: '长连接' },
    { key: 'nacos_naming_service_count', label: '服务数' },
    { key: 'nacos_config_count', label: '配置数' },
    { key: 'nacos_subscriber_count', label: '订阅者' },
    { key: 'nacos_jvm_heap_used_percent', label: 'JVM 堆(%)' },
  ],
  trends: [
    { name: '客户端长连接', metric: 'nacos_long_connection' },
    { name: 'JVM 堆使用率(%)', metric: 'nacos_jvm_heap_used_percent' },
  ],
}
