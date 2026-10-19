package api

import (
	"log/slog"
	"net/http"
	"sort"

	"github.com/nebula/monitor/internal/server/instancereg"
)

// 本文件是 RabbitMQ / Elasticsearch / ClickHouse / Nacos / ZooKeeper 五类
// 「轻采集」内置中间件的通用实例接口：它们的采集都是「存活 + 少量核心指标」，
// 展示形态一致（实例表 + 指标列），因此共用一个表驱动的 handler，
// 而不是像 Redis/Kafka 那样每类各写一份专属 handler + 专属前端 Tab。
//
// 与内置专属 handler 的行为对齐：
//   - 以 *_instance_up 的最新样本为基准枚举实例；
//   - 注册表（instancereg）补充「已配置但 agent 离线」的实例；
//   - 各核心指标最新值按 node|instance 关联到实例；
//   - 资源范围过滤（受限用户只见范围内节点）。

// mwGenericMetricDef 描述实例上的一个核心指标（展示名与单位）。
type mwGenericMetricDef struct {
	Name  string
	Label string
	Unit  string
	// Scale 是展示前的换算系数（0 表示不换算）。
	// 采集侧统一写基础单位（字节、秒），展示单位由声明决定——
	// 这样「换成 MB 展示」不需要改采集器，也不会在 handler 里散落除法。
	Scale float64
}

// mwGenericRegistryRef 是注册表条目的统一投影（避免为每类写一套转换）。
type mwGenericRegistryRef struct {
	Node, Instance, Name, Group, Role, Version string
	Topology, Database                         string
	Uptime                                     float64
	Up                                         bool
}

// mwGenericSpec 是一类的完整描述。
type mwGenericSpec struct {
	Type     string // 类型键（rabbitmq / elasticsearch / ...）
	Label    string // 展示名
	UpMetric string
	// Registry 返回注册表中全部实例的统一投影。
	Registry func() []mwGenericRegistryRef
	// Metrics 实例上的核心指标定义（顺序即前端列顺序）。
	Metrics []mwGenericMetricDef
	// UptimeMetric 若声明，则把该指标的最新值同时投影到实例的 uptime 字段。
	// 声明它而不是把 uptime 当普通指标：详情抽屉要按「天/小时」展示，需要知道这是时长。
	UptimeMetric string
}

// mwGenericInstanceInfo 是通用实例接口的响应行。
type mwGenericInstanceInfo struct {
	Node     string             `json:"node"`
	NodeIP   string             `json:"nodeIp,omitempty"`
	Instance string             `json:"instance"`
	Name     string             `json:"name"`
	Group    string             `json:"group"`
	Role     string             `json:"role,omitempty"`
	Version  string             `json:"version,omitempty"`
	Topology string             `json:"topology,omitempty"`
	Database string             `json:"database,omitempty"`
	Uptime   float64            `json:"uptime,omitempty"`
	Up       bool               `json:"up"`
	Metrics  []mwGenericMetricT `json:"metrics,omitempty"`
}

// mwGenericMetricT 是实例上的一个指标值。
type mwGenericMetricT struct {
	Key   string  `json:"key"`
	Label string  `json:"label"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit,omitempty"`
}

// handleGenericMWInstances 返回指定类型的实例列表（含核心指标最新值）。
func (a *API) handleGenericMWInstances(spec mwGenericSpec) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		upSeries, err := a.store.QueryAllLatest(spec.UpMetric, nil)
		if err != nil {
			slog.Error("查询中间件实例失败", "type", spec.Type, "err", err)
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}

		type state struct {
			info mwGenericInstanceInfo
			key  string
		}
		instances := map[string]*state{}
		var keys []string

		// latestTs 记录每个实例已采纳样本的数据点时间戳。
		// 同一 node|instance 可能存在多条存活序列（指标带 version/role 等
		// 「仅采集成功时才写入」的标签时，up=0 与 up=1 会落在不同序列上），
		// 离线期间写入的 up=0 在恢复后仍留在即时查询回看窗口内；只采纳时间戳
		// 最新的一条，避免已恢复的实例仍被判为离线。
		latestTs := map[string]int64{}
		// mergeMeta 只让**非空**的元信息覆盖已有值。
		//
		// 注册表里有 version / uptime，而指标 labels 里往往没有；若让空值覆盖，
		// 「版本」列就会在实例上线后反而变空（本仓库为此吃过一次亏）。
		mergeMeta := func(dst, src mwGenericInstanceInfo) mwGenericInstanceInfo {
			if src.Name != "" {
				dst.Name = src.Name
			}
			if src.Group != "" {
				dst.Group = src.Group
			}
			if src.Role != "" {
				dst.Role = src.Role
			}
			if src.Version != "" {
				dst.Version = src.Version
			}
			if src.Topology != "" {
				dst.Topology = src.Topology
			}
			if src.Database != "" {
				dst.Database = src.Database
			}
			if src.Uptime != 0 {
				dst.Uptime = src.Uptime
			}
			if src.NodeIP != "" {
				dst.NodeIP = src.NodeIP
			}
			return dst
		}
		// add 合并一条实例信息。isSample 为 false 表示来自注册表（只补元信息，
		// 不代表存活状态）；为 true 表示来自指标样本，时间更新的样本才改存活状态。
		add := func(key string, ts int64, info mwGenericInstanceInfo, isSample bool) {
			prev, ok := instances[key]
			if !ok {
				instances[key] = &state{info: info, key: key}
				keys = append(keys, key)
				latestTs[key] = ts
				return
			}
			// 注册表条目只补元信息，不代表存活状态，也不受时间戳约束
			if !isSample {
				prev.info = mergeMeta(prev.info, info)
				return
			}
			// 样本必须比已采纳的更新才生效：旧样本既不能改存活状态，也不能改元信息
			// （改了分组后，旧序列在回看窗口内仍存在，不能把界面改回旧分组）。
			if ts <= latestTs[key] {
				return
			}
			prev.info = mergeMeta(prev.info, info)
			prev.info.Up = info.Up
			latestTs[key] = ts
		}

		// 注册表先铺底：agent 离线导致 up 指标 stale 时，实例仍以离线状态列出；
		// 且 version / uptime 这类「只在注册表里」的元信息不会因指标 labels 为空而丢失。
		for _, ref := range spec.Registry() {
			add(ref.Node+"|"+ref.Instance, 0, mwGenericInstanceInfo{
				Node:     ref.Node,
				NodeIP:   a.nodeIP(ref.Node),
				Instance: ref.Instance,
				Name:     ref.Name,
				Group:    ref.Group,
				Role:     ref.Role,
				Version:  ref.Version,
				Topology: ref.Topology,
				Database: ref.Database,
				Uptime:   ref.Uptime,
				Up:       false,
			}, false)
		}

		for _, s := range upSeries {
			node := s.Labels["node"]
			instance := s.Labels["instance"]
			if node == "" || instance == "" || len(s.Points) == 0 {
				continue
			}
			last := s.Points[len(s.Points)-1]
			add(node+"|"+instance, last.Timestamp, mwGenericInstanceInfo{
				Node:     node,
				NodeIP:   a.nodeIP(node),
				Instance: instance,
				// 只用 labels 里真实存在的 name；group 兜底放到合并之后做，
				// 否则兜底出来的非空值会覆盖注册表里的真名。
				Name:     s.Labels["name"],
				Group:    s.Labels["group"],
				Role:     s.Labels["role"],
				Version:  s.Labels["version"],
				Topology: s.Labels["topology"],
				Database: s.Labels["database"],
				Up:      last.Value > 0,
			}, true)
		}

		// attach 把某指标的最新值按 node|instance 关联到实例上。
		// 指标查询失败只记日志、不影响其它指标（缺一个指标不该让整页失败）。
		attach := func(metric string, apply func(info *mwGenericInstanceInfo, value float64)) {
			series, err := a.store.QueryAllLatest(metric, nil)
			if err != nil {
				slog.Warn("聚合中间件指标查询失败", "type", spec.Type, "metric", metric, "err", err)
				return
			}
			for _, s := range series {
				node := s.Labels["node"]
				instance := s.Labels["instance"]
				if node == "" || instance == "" || len(s.Points) == 0 {
					continue
				}
				st, ok := instances[node+"|"+instance]
				if !ok {
					continue
				}
				apply(&st.info, s.Points[len(s.Points)-1].Value)
			}
		}

		// 核心指标最新值关联到实例
		for _, def := range spec.Metrics {
			def := def
			attach(def.Name, func(info *mwGenericInstanceInfo, value float64) {
				if def.Scale != 0 {
					value *= def.Scale
				}
				info.Metrics = append(info.Metrics, mwGenericMetricT{
					Key:   def.Name,
					Label: def.Label,
					Value: round2(value),
					Unit:  def.Unit,
				})
			})
		}

		// 运行时长单独投影：它要按天/小时展示，不属于指标网格，
		// 因此不放进 Metrics（否则同一页会显示两个「运行时长」）。
		if spec.UptimeMetric != "" {
			attach(spec.UptimeMetric, func(info *mwGenericInstanceInfo, value float64) {
				info.Uptime = round2(value)
			})
		}

		keys = filterByNodeScope(a, Principal(r), keys, nodeOfKey)
		out := make([]mwGenericInstanceInfo, 0, len(keys))
		for _, k := range keys {
			info := instances[k].info
			// 没有名称时用分组兜底（注册表与指标 labels 都没有 name 的实例）
			if info.Name == "" {
				info.Name = info.Group
			}
			out = append(out, info)
		}
		// 稳定输出顺序：按 instance 排序，前端展示不跳动
		sort.Slice(out, func(i, j int) bool { return out[i].Instance < out[j].Instance })
		writeJSON(w, 200, map[string]interface{}{"type": spec.Type, "instances": out})
	}
}

// handleRabbitMQInstances RabbitMQ 实例列表。
func (a *API) handleRabbitMQInstances(w http.ResponseWriter, r *http.Request) {
	refs := make([]mwGenericRegistryRef, 0)
	for _, ri := range instancereg.Default.RabbitMQInstances() {
		refs = append(refs, mwGenericRegistryRef{Node: ri.Node, Instance: ri.Instance, Name: ri.Name, Group: ri.Group, Version: ri.Version})
	}
	a.handleGenericMWInstances(mwGenericSpec{
		Type:     "rabbitmq",
		Label:    "RabbitMQ",
		UpMetric: "rabbitmq_instance_up",
		Registry: func() []mwGenericRegistryRef { return refs },
		Metrics: []mwGenericMetricDef{
			{Name: "rabbitmq_connections", Label: "连接数"},
			{Name: "rabbitmq_queues", Label: "队列数"},
			{Name: "rabbitmq_queue_messages", Label: "消息总数"},
			{Name: "rabbitmq_consumers", Label: "消费者数"},
			{Name: "rabbitmq_publishers", Label: "发布者数"},
			{Name: "rabbitmq_process_memory_bytes", Label: "进程内存", Unit: "B"},
		},
	})(w, r)
}

// handleElasticsearchInstances Elasticsearch 实例列表。
func (a *API) handleElasticsearchInstances(w http.ResponseWriter, r *http.Request) {
	refs := make([]mwGenericRegistryRef, 0)
	for _, ei := range instancereg.Default.ElasticsearchInstances() {
		refs = append(refs, mwGenericRegistryRef{Node: ei.Node, Instance: ei.Instance, Name: ei.Name, Group: ei.Group, Version: ei.Version})
	}
	a.handleGenericMWInstances(mwGenericSpec{
		Type:     "elasticsearch",
		Label:    "Elasticsearch",
		UpMetric: "es_instance_up",
		Registry: func() []mwGenericRegistryRef { return refs },
		Metrics: []mwGenericMetricDef{
			{Name: "es_cluster_status", Label: "集群状态(0绿/1黄/2红)"},
			{Name: "es_nodes", Label: "节点数"},
			{Name: "es_active_shards", Label: "活跃分片"},
			{Name: "es_unassigned_shards", Label: "未分配分片"},
		},
	})(w, r)
}

// handleClickHouseInstances ClickHouse 实例列表。
func (a *API) handleClickHouseInstances(w http.ResponseWriter, r *http.Request) {
	refs := make([]mwGenericRegistryRef, 0)
	for _, ci := range instancereg.Default.ClickHouseInstances() {
		refs = append(refs, mwGenericRegistryRef{Node: ci.Node, Instance: ci.Instance, Name: ci.Name, Group: ci.Group, Version: ci.Version})
	}
	a.handleGenericMWInstances(mwGenericSpec{
		Type:     "clickhouse",
		Label:    "ClickHouse",
		UpMetric: "clickhouse_instance_up",
		Registry: func() []mwGenericRegistryRef { return refs },
		Metrics: []mwGenericMetricDef{
			{Name: "clickhouse_tcp_connections", Label: "TCP连接"},
			{Name: "clickhouse_http_connections", Label: "HTTP连接"},
			{Name: "clickhouse_queries_running", Label: "运行中查询"},
			{Name: "clickhouse_merges_running", Label: "合并数"},
		},
	})(w, r)
}

// handleNacosInstances Nacos 实例列表。
func (a *API) handleNacosInstances(w http.ResponseWriter, r *http.Request) {
	refs := make([]mwGenericRegistryRef, 0)
	for _, ni := range instancereg.Default.NacosInstances() {
		refs = append(refs, mwGenericRegistryRef{Node: ni.Node, Instance: ni.Instance, Name: ni.Name, Group: ni.Group})
	}
	a.handleGenericMWInstances(mwGenericSpec{
		Type:     "nacos",
		Label:    "Nacos",
		UpMetric: "nacos_instance_up",
		Registry: func() []mwGenericRegistryRef { return refs },
	})(w, r)
}

// handleZooKeeperInstances ZooKeeper 实例列表。
func (a *API) handleZooKeeperInstances(w http.ResponseWriter, r *http.Request) {
	refs := make([]mwGenericRegistryRef, 0)
	for _, zi := range instancereg.Default.ZooKeeperInstances() {
		refs = append(refs, mwGenericRegistryRef{Node: zi.Node, Instance: zi.Instance, Name: zi.Name, Group: zi.Group, Role: zi.Role, Version: zi.Version})
	}
	a.handleGenericMWInstances(mwGenericSpec{
		Type:     "zookeeper",
		Label:    "ZooKeeper",
		UpMetric: "zookeeper_instance_up",
		Registry: func() []mwGenericRegistryRef { return refs },
		Metrics: []mwGenericMetricDef{
			{Name: "zookeeper_avg_latency", Label: "平均延迟", Unit: "ms"},
			{Name: "zookeeper_outstanding_requests", Label: "未处理请求"},
			{Name: "zookeeper_alive_connections", Label: "活跃连接"},
			{Name: "zookeeper_znode_count", Label: "Znode数"},
		},
	})(w, r)
}

// handleNginxInstances Nginx 实例列表。
func (a *API) handleNginxInstances(w http.ResponseWriter, r *http.Request) {
	refs := make([]mwGenericRegistryRef, 0)
	for _, ni := range instancereg.Default.NginxInstances() {
		refs = append(refs, mwGenericRegistryRef{
			Node: ni.Node, Instance: ni.Instance, Name: ni.Name, Group: ni.Group, Version: ni.Version,
		})
	}
	a.handleGenericMWInstances(mwGenericSpec{
		Type:     "nginx",
		Label:    "Nginx",
		UpMetric: "nginx_instance_up",
		Registry: func() []mwGenericRegistryRef { return refs },
		Metrics: []mwGenericMetricDef{
			{Name: "nginx_active_connections", Label: "活动连接"},
			{Name: "nginx_accepts", Label: "已接收"},
			{Name: "nginx_handled", Label: "已处理"},
			{Name: "nginx_requests", Label: "总请求"},
			{Name: "nginx_reading", Label: "读取"},
			{Name: "nginx_writing", Label: "写入"},
			{Name: "nginx_waiting", Label: "等待"},
			{Name: "nginx_connection_drop_rate", Label: "连接丢弃率"},
		},
	})(w, r)
}

// handleRocketMQInstances RocketMQ 实例列表。
//
// version / uptime 只在注册表里（采集成功时 Agent 才上报），指标 labels 不含它们；
// 通用 handler 的「注册表先铺底」保证这两列不会在实例上线后变空。
func (a *API) handleRocketMQInstances(w http.ResponseWriter, r *http.Request) {
	refs := make([]mwGenericRegistryRef, 0)
	for _, ri := range instancereg.Default.RocketMQInstances() {
		refs = append(refs, mwGenericRegistryRef{
			Node: ri.Node, Instance: ri.Instance, Name: ri.Name, Group: ri.Group,
			Role: ri.Role, Version: ri.Version, Uptime: ri.Uptime,
		})
	}
	a.handleGenericMWInstances(mwGenericSpec{
		Type:         "rocketmq",
		Label:        "RocketMQ",
		UpMetric:     "rocketmq_instance_up",
		UptimeMetric: "rocketmq_uptime_seconds",
		Registry:     func() []mwGenericRegistryRef { return refs },
		Metrics: []mwGenericMetricDef{
			{Name: "rocketmq_broker_count", Label: "Broker 数"},
			{Name: "rocketmq_topic_count", Label: "Topic 数"},
			{Name: "rocketmq_consumer_group_count", Label: "消费组"},
			{Name: "rocketmq_consumer_lag", Label: "消费延迟"},
			{Name: "rocketmq_producer_tps", Label: "生产 TPS"},
			{Name: "rocketmq_consumer_tps", Label: "消费 TPS"},
			{Name: "rocketmq_broker_tps", Label: "Broker TPS"},
			{Name: "rocketmq_message_accumulation", Label: "消息积压"},
		},
	})(w, r)
}

// handleKafkaInstances Kafka 实例列表。
func (a *API) handleKafkaInstances(w http.ResponseWriter, r *http.Request) {
	refs := make([]mwGenericRegistryRef, 0)
	for _, ki := range instancereg.Default.KafkaInstances() {
		refs = append(refs, mwGenericRegistryRef{
			Node: ki.Node, Instance: ki.Instance, Name: ki.Name, Group: ki.Group,
			Role: ki.Role, Version: ki.Version,
		})
	}
	a.handleGenericMWInstances(mwGenericSpec{
		Type:     "kafka",
		Label:    "Kafka",
		UpMetric: "kafka_instance_up",
		Registry: func() []mwGenericRegistryRef { return refs },
		Metrics: []mwGenericMetricDef{
			{Name: "kafka_broker_count", Label: "Broker 数"},
			{Name: "kafka_topic_count", Label: "Topic 数"},
			{Name: "kafka_partition_count", Label: "分区数"},
			{Name: "kafka_under_replicated_partitions", Label: "欠副本分区"},
			{Name: "kafka_offline_partitions", Label: "离线分区"},
			{Name: "kafka_consumer_group_count", Label: "消费组"},
			{Name: "kafka_consumer_lag", Label: "消费延迟"},
			{Name: "kafka_consumer_lag_max", Label: "最大消费延迟"},
			{Name: "kafka_active_controller_count", Label: "Controller 数"},
		},
	})(w, r)
}

// handlePostgresInstances PostgreSQL 实例列表。
func (a *API) handlePostgresInstances(w http.ResponseWriter, r *http.Request) {
	refs := make([]mwGenericRegistryRef, 0)
	for _, pi := range instancereg.Default.PostgresInstances() {
		refs = append(refs, mwGenericRegistryRef{
			Node: pi.Node, Instance: pi.Instance, Name: pi.Name, Group: pi.Group,
			Role: pi.Role, Version: pi.Version, Topology: pi.Topology, Database: pi.Database,
		})
	}
	a.handleGenericMWInstances(mwGenericSpec{
		Type:         "postgres",
		Label:        "PostgreSQL",
		UpMetric:     "postgres_instance_up",
		UptimeMetric: "postgres_uptime_seconds",
		Registry:     func() []mwGenericRegistryRef { return refs },
		Metrics: []mwGenericMetricDef{
			{Name: "postgres_numbackends", Label: "连接数"},
			{Name: "postgres_max_connections", Label: "最大连接数"},
			{Name: "postgres_xact_commit", Label: "事务提交"},
			{Name: "postgres_xact_rollback", Label: "事务回滚"},
			{Name: "postgres_cache_hit_ratio", Label: "缓存命中率", Unit: "%"},
			{Name: "postgres_deadlocks", Label: "死锁数"},
			{Name: "postgres_replication_lag_bytes", Label: "复制延迟", Unit: "B"},
			{Name: "postgres_database_size_bytes", Label: "数据库大小", Unit: "B"},
		},
	})(w, r)
}

// handleMongoDBInstances MongoDB 实例列表。
func (a *API) handleMongoDBInstances(w http.ResponseWriter, r *http.Request) {
	refs := make([]mwGenericRegistryRef, 0)
	for _, mi := range instancereg.Default.MongoDBInstances() {
		refs = append(refs, mwGenericRegistryRef{
			Node: mi.Node, Instance: mi.Instance, Name: mi.Name, Group: mi.Group,
			Role: mi.Role, Version: mi.Version, Topology: mi.Topology,
		})
	}
	const toMB = 1.0 / 1024 / 1024
	a.handleGenericMWInstances(mwGenericSpec{
		Type:         "mongodb",
		Label:        "MongoDB",
		UpMetric:     "mongodb_up",
		UptimeMetric: "mongodb_uptime_seconds",
		Registry:     func() []mwGenericRegistryRef { return refs },
		Metrics: []mwGenericMetricDef{
			{Name: "mongodb_connections_current", Label: "连接数"},
			{Name: "mongodb_connections_available", Label: "可用连接"},
			{Name: "mongodb_mem_resident_bytes", Label: "常驻内存", Unit: "MB", Scale: toMB},
			{Name: "mongodb_mem_virtual_bytes", Label: "虚拟内存", Unit: "MB", Scale: toMB},
			{Name: "mongodb_opcounters_insert", Label: "插入次数"},
			{Name: "mongodb_opcounters_query", Label: "查询次数"},
			{Name: "mongodb_opcounters_update", Label: "更新次数"},
			{Name: "mongodb_opcounters_delete", Label: "删除次数"},
			{Name: "mongodb_opcounters_command", Label: "命令次数"},
			{Name: "mongodb_db_dataSize_bytes", Label: "数据大小", Unit: "MB", Scale: toMB},
			{Name: "mongodb_db_storageSize_bytes", Label: "存储大小", Unit: "MB", Scale: toMB},
			{Name: "mongodb_db_indexSize_bytes", Label: "索引大小", Unit: "MB", Scale: toMB},
			{Name: "mongodb_db_objects", Label: "文档数"},
			{Name: "mongodb_db_indexes", Label: "索引数"},
			{Name: "mongodb_repl_state", Label: "副本状态"},
			{Name: "mongodb_repl_health", Label: "副本健康"},
			{Name: "mongodb_repl_lag", Label: "复制延迟", Unit: "s"},
		},
	})(w, r)
}

// handleFastDFSInstances FastDFS 实例列表。
//
// 空间类指标在采集侧以字节上报，这里用 Scale 换算成 MB 展示：
// 换单位属于展示口径，不该回采集器改一遍。
func (a *API) handleFastDFSInstances(w http.ResponseWriter, r *http.Request) {
	refs := make([]mwGenericRegistryRef, 0)
	for _, fi := range instancereg.Default.FastDFSInstances() {
		refs = append(refs, mwGenericRegistryRef{
			Node: fi.Node, Instance: fi.Instance, Name: fi.Name, Group: fi.Group, Role: fi.Role,
		})
	}
	const toMB = 1.0 / 1024 / 1024
	a.handleGenericMWInstances(mwGenericSpec{
		Type:     "fastdfs",
		Label:    "FastDFS",
		UpMetric: "fastdfs_up",
		Registry: func() []mwGenericRegistryRef { return refs },
		Metrics: []mwGenericMetricDef{
			{Name: "fastdfs_group_count", Label: "Group总数"},
			{Name: "fastdfs_storage_count", Label: "Storage总数"},
			{Name: "fastdfs_storage_online_count", Label: "在线Storage"},
			{Name: "fastdfs_storage_offline_count", Label: "离线Storage"},
			{Name: "fastdfs_total_space", Label: "总空间", Unit: "MB", Scale: toMB},
			{Name: "fastdfs_free_space", Label: "空闲空间", Unit: "MB", Scale: toMB},
			{Name: "fastdfs_used_space", Label: "已用空间", Unit: "MB", Scale: toMB},
			{Name: "fastdfs_trunk_free_space", Label: "Trunk空闲", Unit: "MB", Scale: toMB},
			{Name: "fastdfs_disk_read_bytes", Label: "磁盘读", Unit: "MB", Scale: toMB},
			{Name: "fastdfs_disk_write_bytes", Label: "磁盘写", Unit: "MB", Scale: toMB},
			{Name: "fastdfs_net_recv_bytes", Label: "网络接收", Unit: "MB", Scale: toMB},
			{Name: "fastdfs_net_sent_bytes", Label: "网络发送", Unit: "MB", Scale: toMB},
		},
	})(w, r)
}
