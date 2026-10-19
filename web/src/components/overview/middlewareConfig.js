// 首页中间件概览区块配置：10 种中间件类型（与「中间件监控」的 Tab 一一对应）
// 每种类型定义：key、展示名、图标、跳转 tab、TopN 主指标、辅助指标、聚合接口
import redisIcon from '../../assets/img/redis.svg'
import mysqlIcon from '../../assets/img/mysql.svg'
import postgresIcon from '../../assets/img/postgresql.svg'
import nginxIcon from '../../assets/img/nginx.svg'
import kafkaIcon from '../../assets/img/Kafka.svg'
import dockerIcon from '../../assets/img/docker.svg'
import rocketmqIcon from '../../assets/img/rocketMQ.svg'
import k8sIcon from '../../assets/img/kubernetes.svg'
import mongodbIcon from '../../assets/img/mongoDB.svg'
import fastdfsIcon from '../../assets/img/fastdfs.svg'

// instMetric 读取实例上的一个指标值：优先 metrics 数组，回退扁平字段。
//
// 两种形状并存是过渡期的现实：已声明化的类型返回 { metrics: [{ key, value }] }，
// 尚未改造的类型仍返回扁平字段。取值收敛在这里，调用方不必知道是哪一种。
export function instMetric(inst, key) {
  if (!key) return undefined
  const hit = (inst.metrics || []).find((m) => m.key === key)
  if (hit) return hit.value
  return inst[key]
}

export const middlewareTypes = [
  {
    key: 'redis',
    label: 'Redis',
    icon: redisIcon,
    tab: 'redis',
    endpoint: '/api/v1/middleware/redis/instances',
    topKey: 'usedMemory',
    topLabel: '内存占用',
    fmt: 'bytes',
    subKey: 'connectedClients',
    subLabel: '连接数',
    subFmt: 'num0',
  },
  {
    key: 'mysql',
    label: 'MySQL',
    icon: mysqlIcon,
    tab: 'mysql',
    endpoint: '/api/v1/middleware/mysql/instances',
    topKey: 'queriesPerSec',
    topLabel: 'QPS',
    fmt: 'num',
    subKey: 'threadsConnected',
    subLabel: '连接数',
    subFmt: 'num0',
  },
  {
    key: 'postgres',
    label: 'PostgreSQL',
    icon: postgresIcon,
    tab: 'postgres',
    endpoint: '/api/v1/middleware/postgres/instances',
    topKey: 'postgres_cache_hit_ratio',
    topLabel: '缓存命中率',
    fmt: 'pct',
    subKey: 'postgres_numbackends',
    subLabel: '后端数',
    subFmt: 'num0',
  },
  {
    key: 'nginx',
    label: 'Nginx',
    icon: nginxIcon,
    tab: 'nginx',
    endpoint: '/api/v1/middleware/nginx/instances',
    topKey: 'nginx_requests',
    topLabel: '请求数',
    fmt: 'num0',
    subKey: 'nginx_active_connections',
    subLabel: '活跃连接',
    subFmt: 'num0',
  },
  {
    key: 'kafka',
    label: 'Kafka',
    icon: kafkaIcon,
    tab: 'kafka',
    endpoint: '/api/v1/middleware/kafka/instances',
    topKey: 'kafka_consumer_lag_max',
    topLabel: '最大积压',
    fmt: 'num0',
    subKey: 'kafka_broker_count',
    subLabel: 'Broker',
    subFmt: 'num0',
  },
  {
    key: 'docker',
    label: 'Docker',
    icon: dockerIcon,
    tab: 'docker',
    endpoint: '/api/v1/middleware/docker/containers',
    topKey: 'memPercent',
    topLabel: '内存占比',
    fmt: 'pct',
    subKey: 'cpuPercent',
    subLabel: 'CPU占比',
    subFmt: 'pct',
  },
  {
    key: 'rocketmq',
    label: 'RocketMQ',
    icon: rocketmqIcon,
    tab: 'rocketmq',
    endpoint: '/api/v1/middleware/rocketmq/instances',
    topKey: 'rocketmq_message_accumulation',
    topLabel: '消息堆积',
    fmt: 'num0',
    subKey: 'rocketmq_broker_count',
    subLabel: 'Broker',
    subFmt: 'num0',
  },
  {
    key: 'k8s',
    label: 'Kubernetes',
    icon: k8sIcon,
    tab: 'k8s',
    endpoint: '/api/v1/middleware/k8s/instances',
    topKey: 'nodesReady',
    topLabel: '就绪节点',
    fmt: 'pctNodes',
    subKey: 'podsRunning',
    subLabel: '运行Pod',
    subFmt: 'num0',
  },
  {
    key: 'mongodb',
    label: 'MongoDB',
    icon: mongodbIcon,
    tab: 'mongodb',
    endpoint: '/api/v1/middleware/mongodb/instances',
    topKey: 'mongodb_mem_resident_bytes',
    topLabel: '常驻内存(MB)',
    fmt: 'num0',
    subKey: 'mongodb_connections_current',
    subLabel: '连接数',
    subFmt: 'num0',
  },
  {
    key: 'fastdfs',
    label: 'FastDFS',
    icon: fastdfsIcon,
    tab: 'fastdfs',
    endpoint: '/api/v1/middleware/fastdfs/instances',
    topKey: 'fastdfs_used_space',
    topLabel: '已用空间(MB)',
    fmt: 'num0',
    subKey: 'fastdfs_storage_online_count',
    subLabel: '在线Storage',
    subFmt: 'num0',
  },
]
