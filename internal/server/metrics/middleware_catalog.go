package metrics

// init 注册中间件（Agent/直连采集）指标元数据。
//
// 不变量：此处登记的 Name 必须与「Agent 采集器 / Server receiver 实际写入时序库的名字」
// 逐字一致，由 TestCatalogNamesHaveProducers 守卫（扫描 internal/agent/collector 与
// internal/server/receiver 的字符串字面量）。名字写错的后果不是报错，而是静默失效——
// 「指标浏览」按目录查询永远查不到数据、按目录配的告警规则恒不触发，
// 且 `/metrics/active` 会把它标为未上线，排查时极易被误判为「采集没开」。
//
// 注意两种采集模式的名字来源不同：
//   - **直连采集**（默认）：名字由本仓库代码决定，见 internal/agent/collector/*.go 与
//     internal/server/receiver/receiver.go，此处登记的就是这些名字（存活指标为
//     `<mw>_instance_up`，MongoDB/FastDFS 为 `<mw>_up`，K8s 为 `k8s_cluster_up`）。
//   - **exporter 模式**：名字来自外部 exporter 的 /metrics 文本（按中间件前缀透传），
//     例如 mysqld_exporter 暴露的 mysql_up（不加反引号，避免被源码扫描类工具误判为指标字面量）。
//     这类名字随 exporter 实现而定，不在此处登记；需要时在「指标浏览」中直接搜索即可。
//
// WorseWhen/NoAlert 是给**告警表单**用的：前者决定选中指标后默认的比较运算符，
// 后者把「累计计数器 / 容量信息 / 运行时长」这类设阈值没有意义的指标排到选择器后面。
func init() {
	// —— Redis ——
	Register(MetricMeta{Name: "redis_instance_up", Title: "Redis 存活", Category: CatRedis, Unit: "", Chart: ChartGauge,
		WorseWhen: WorseLow, Desc: "1=存活、0=不可达；服务离线类规则按它判定"})
	Register(MetricMeta{Name: "redis_used_memory_percent", Title: "内存使用率", Category: CatRedis, Unit: "%", Chart: ChartArea,
		WorseWhen: WorseHigh, Desc: "相对 maxmemory 的占用；未设置 maxmemory 时该值不可用"})
	Register(MetricMeta{Name: "redis_used_memory", Title: "内存占用", Category: CatRedis, Unit: "B", Chart: ChartLine, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "redis_maxmemory", Title: "内存上限", Category: CatRedis, Unit: "B", Chart: ChartGauge, NoAlert: true, Desc: "配置值"})
	Register(MetricMeta{Name: "redis_used_memory_rss", Title: "RSS 内存", Category: CatRedis, Unit: "B", Chart: ChartLine, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "redis_used_memory_peak", Title: "内存峰值", Category: CatRedis, Unit: "B", Chart: ChartLine, NoAlert: true})
	Register(MetricMeta{Name: "redis_memory_fragmentation_ratio", Title: "内存碎片率", Category: CatRedis, Unit: "", Chart: ChartLine,
		WorseWhen: WorseHigh, Desc: "持续大于 1.5 说明碎片严重，重启或开启 activedefrag 可缓解"})
	Register(MetricMeta{Name: "redis_connected_clients", Title: "已连接客户端", Category: CatRedis, Unit: "个", Chart: ChartLine, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "redis_maxclients", Title: "连接数上限", Category: CatRedis, Unit: "个", Chart: ChartGauge, NoAlert: true, Desc: "配置值"})
	Register(MetricMeta{Name: "redis_blocked_clients", Title: "阻塞客户端", Category: CatRedis, Unit: "个", Chart: ChartLine,
		WorseWhen: WorseHigh, Desc: "阻塞数持续不为 0 通常意味着慢命令或大 key"})
	Register(MetricMeta{Name: "redis_rejected_connections", Title: "累计被拒连接", Category: CatRedis, Unit: "个", Chart: ChartLine,
		WorseWhen: WorseHigh, Desc: "累计计数器，但任何增长都说明触过 maxclients，适合用「变化」语义关注"})
	Register(MetricMeta{Name: "redis_ops_per_sec", Title: "QPS", Category: CatRedis, Unit: "次/s", Chart: ChartLine,
		Desc: "速率高低取决于业务，无通用阈值"})
	Register(MetricMeta{Name: "redis_total_commands_processed", Title: "累计命令数", Category: CatRedis, Unit: "个", Chart: ChartLine, NoAlert: true, Desc: "累计计数器"})
	Register(MetricMeta{Name: "redis_total_connections_received", Title: "累计连接数", Category: CatRedis, Unit: "个", Chart: ChartLine, NoAlert: true, Desc: "累计计数器"})
	Register(MetricMeta{Name: "redis_keys", Title: "键总数", Category: CatRedis, Unit: "个", Chart: ChartLine, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "redis_hit_rate", Title: "命中率", Category: CatRedis, Unit: "%", Chart: ChartArea,
		WorseWhen: WorseLow, Desc: "低于 80% 通常意味着缓存设计或容量问题"})
	Register(MetricMeta{Name: "redis_evicted_keys", Title: "累计淘汰键", Category: CatRedis, Unit: "个", Chart: ChartLine,
		WorseWhen: WorseHigh, Desc: "增长说明内存已达上限并开始丢数据"})
	Register(MetricMeta{Name: "redis_expired_keys", Title: "累计过期键", Category: CatRedis, Unit: "个", Chart: ChartLine, NoAlert: true})
	Register(MetricMeta{Name: "redis_cmd_latency_ms", Title: "命令延迟", Category: CatRedis, Unit: "ms", Chart: ChartLine, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "redis_uptime_in_seconds", Title: "运行时长", Category: CatRedis, Unit: "s", Chart: ChartLine,
		NoAlert: true, Desc: "重启会归零；用「变化率」看重启比设阈值更合适"})
	Register(MetricMeta{Name: "redis_net_input_bytes", Title: "网络输入", Category: CatRedis, Unit: "B", Chart: ChartLine, NoAlert: true})
	Register(MetricMeta{Name: "redis_net_output_bytes", Title: "网络输出", Category: CatRedis, Unit: "B", Chart: ChartLine, NoAlert: true})
	Register(MetricMeta{Name: "redis_connected_slaves", Title: "从节点数", Category: CatRedis, Unit: "个", Chart: ChartGauge,
		WorseWhen: WorseLow, Desc: "主库上该值为 0 而配置了复制，说明从库掉线"})
	Register(MetricMeta{Name: "redis_replication_offset", Title: "复制偏移量", Category: CatRedis, Unit: "", Chart: ChartLine, NoAlert: true})
	Register(MetricMeta{Name: "redis_replication_lag", Title: "复制延迟", Category: CatRedis, Unit: "s", Chart: ChartLine, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "redis_sentinel_masters", Title: "Sentinel 监控主库数", Category: CatRedis, Unit: "个", Chart: ChartGauge, NoAlert: true})
	Register(MetricMeta{Name: "redis_sentinel_slaves", Title: "Sentinel 监控从库数", Category: CatRedis, Unit: "个", Chart: ChartGauge, WorseWhen: WorseLow})
	Register(MetricMeta{Name: "redis_sentinel_sentinels", Title: "Sentinel 节点数", Category: CatRedis, Unit: "个", Chart: ChartGauge, WorseWhen: WorseLow})
	Register(MetricMeta{Name: "redis_sentinel_tilt", Title: "Sentinel tilt 模式(1=异常)", Category: CatRedis, Unit: "", Chart: ChartGauge, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "redis_cluster_state", Title: "集群状态(1=ok)", Category: CatRedis, Unit: "", Chart: ChartGauge, WorseWhen: WorseLow})
	Register(MetricMeta{Name: "redis_cluster_slots_assigned", Title: "已分配槽位", Category: CatRedis, Unit: "个", Chart: ChartGauge, WorseWhen: WorseLow})
	Register(MetricMeta{Name: "redis_cluster_slots_ok", Title: "正常槽位", Category: CatRedis, Unit: "个", Chart: ChartGauge, WorseWhen: WorseLow})
	Register(MetricMeta{Name: "redis_cluster_slots_fail", Title: "故障槽位", Category: CatRedis, Unit: "个", Chart: ChartGauge, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "redis_cluster_known_nodes", Title: "集群已知节点", Category: CatRedis, Unit: "个", Chart: ChartGauge, WorseWhen: WorseLow})
	Register(MetricMeta{Name: "redis_cluster_size", Title: "集群主节点数", Category: CatRedis, Unit: "个", Chart: ChartGauge, NoAlert: true})
	Register(MetricMeta{Name: "redis_cluster_slot_range", Title: "槽位区间", Category: CatRedis, Unit: "", Chart: ChartGauge, NoAlert: true, Dynamic: true,
		Desc: "名字带区间维度（由采集器拼出），用于核对槽位覆盖"})
	Register(MetricMeta{Name: "redis_rdb_last_bgsave_status", Title: "RDB 落盘状态(1=ok)", Category: CatRedis, Unit: "", Chart: ChartGauge, WorseWhen: WorseLow,
		Desc: "0 说明最近一次 bgsave 失败（磁盘满/fork 失败），宕机后数据丢失窗口取决于它"})
	Register(MetricMeta{Name: "redis_rdb_changes_since_last_save", Title: "未落盘变更数", Category: CatRedis, Unit: "个", Chart: ChartLine, WorseWhen: WorseHigh,
		Desc: "自上次 RDB 落盘以来累积的写操作数，越大宕机丢得越多"})
	Register(MetricMeta{Name: "redis_rdb_last_save_time", Title: "上次 RDB 落盘时间", Category: CatRedis, Unit: "s", Chart: ChartLine, NoAlert: true,
		Desc: "epoch 秒，与当前时间差即未落盘时长"})
	Register(MetricMeta{Name: "redis_aof_enabled", Title: "AOF 开关(1=启用)", Category: CatRedis, Unit: "", Chart: ChartGauge, NoAlert: true})
	Register(MetricMeta{Name: "redis_aof_last_write_status", Title: "AOF 写入状态(1=ok)", Category: CatRedis, Unit: "", Chart: ChartGauge, WorseWhen: WorseLow,
		Desc: "0 说明 AOF 落盘失败，未启用 AOF 时恒为 1"})

	// —— MySQL ——
	Register(MetricMeta{Name: "mysql_instance_up", Title: "MySQL 存活", Category: CatMySQL, Unit: "", Chart: ChartGauge, WorseWhen: WorseLow})
	Register(MetricMeta{Name: "mysql_threads_connected", Title: "当前连接数", Category: CatMySQL, Unit: "个", Chart: ChartLine,
		WorseWhen: WorseHigh, Desc: "配合 mysql_max_connections 使用（例如达到上限的 80%）"})
	Register(MetricMeta{Name: "mysql_threads_running", Title: "运行中线程", Category: CatMySQL, Unit: "个", Chart: ChartLine,
		WorseWhen: WorseHigh, Desc: "持续偏高通常意味着慢 SQL 堆积"})
	Register(MetricMeta{Name: "mysql_max_connections", Title: "连接数上限", Category: CatMySQL, Unit: "个", Chart: ChartGauge, NoAlert: true, Desc: "配置值"})
	Register(MetricMeta{Name: "mysql_connection_errors_total", Title: "累计连接错误", Category: CatMySQL, Unit: "个", Chart: ChartLine,
		WorseWhen: WorseHigh, Desc: "增长说明客户端被拒（连接数打满或认证失败）"})
	Register(MetricMeta{Name: "mysql_queries_per_sec", Title: "QPS", Category: CatMySQL, Unit: "次/s", Chart: ChartLine, Desc: "无通用阈值"})
	Register(MetricMeta{Name: "mysql_slow_queries", Title: "慢查询数", Category: CatMySQL, Unit: "个", Chart: ChartLine,
		WorseWhen: WorseHigh, Desc: "累计值；按窗口增量关注比绝对值更有意义"})
	Register(MetricMeta{Name: "mysql_query_latency_ms", Title: "查询延迟", Category: CatMySQL, Unit: "ms", Chart: ChartLine, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "mysql_innodb_buffer_pool_hit_rate", Title: "缓冲池命中率", Category: CatMySQL, Unit: "%", Chart: ChartArea,
		WorseWhen: WorseLow, Desc: "低于 95% 通常意味着缓冲池过小"})
	Register(MetricMeta{Name: "mysql_innodb_buffer_pool_size", Title: "缓冲池大小", Category: CatMySQL, Unit: "B", Chart: ChartGauge, NoAlert: true, Desc: "配置值"})
	Register(MetricMeta{Name: "mysql_innodb_row_lock_waits", Title: "行锁等待", Category: CatMySQL, Unit: "次", Chart: ChartLine, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "mysql_innodb_deadlocks", Title: "死锁次数", Category: CatMySQL, Unit: "次", Chart: ChartLine,
		WorseWhen: WorseHigh, Desc: "任何增长都值得看一眼"})
	Register(MetricMeta{Name: "mysql_seconds_behind_master", Title: "主从延迟", Category: CatMySQL, Unit: "s", Chart: ChartLine, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "mysql_slave_io_running", Title: "从库 IO 线程(1=运行)", Category: CatMySQL, Unit: "", Chart: ChartGauge, WorseWhen: WorseLow})
	Register(MetricMeta{Name: "mysql_slave_sql_running", Title: "从库 SQL 线程(1=运行)", Category: CatMySQL, Unit: "", Chart: ChartGauge, WorseWhen: WorseLow})
	Register(MetricMeta{Name: "mysql_created_tmp_disk_tables", Title: "磁盘临时表", Category: CatMySQL, Unit: "个", Chart: ChartLine,
		WorseWhen: WorseHigh, Desc: "增长说明存在无法用内存临时表完成的排序/分组"})
	Register(MetricMeta{Name: "mysql_bytes_received", Title: "累计接收字节", Category: CatMySQL, Unit: "B", Chart: ChartLine, NoAlert: true})
	Register(MetricMeta{Name: "mysql_bytes_sent", Title: "累计发送字节", Category: CatMySQL, Unit: "B", Chart: ChartLine, NoAlert: true})
	Register(MetricMeta{Name: "mysql_com_commit", Title: "累计提交", Category: CatMySQL, Unit: "次", Chart: ChartLine, NoAlert: true})
	Register(MetricMeta{Name: "mysql_com_rollback", Title: "累计回滚", Category: CatMySQL, Unit: "次", Chart: ChartLine, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "mysql_innodb_rows_read", Title: "累计读取行", Category: CatMySQL, Unit: "行", Chart: ChartLine, NoAlert: true})
	Register(MetricMeta{Name: "mysql_innodb_rows_inserted", Title: "累计插入行", Category: CatMySQL, Unit: "行", Chart: ChartLine, NoAlert: true})
	Register(MetricMeta{Name: "mysql_innodb_rows_updated", Title: "累计更新行", Category: CatMySQL, Unit: "行", Chart: ChartLine, NoAlert: true})
	Register(MetricMeta{Name: "mysql_innodb_rows_deleted", Title: "累计删除行", Category: CatMySQL, Unit: "行", Chart: ChartLine, NoAlert: true})
	Register(MetricMeta{Name: "mysql_uptime", Title: "运行时长", Category: CatMySQL, Unit: "s", Chart: ChartLine, NoAlert: true, Desc: "重启会归零"})
	Register(MetricMeta{Name: "mysql_gr_single_primary_mode", Title: "GR 单主模式(1=单主)", Category: CatMySQL, Unit: "", Chart: ChartGauge,
		NoAlert: true, Desc: "集群健康判定的辅助指标，单独设阈值意义不大"})
	Register(MetricMeta{Name: "mysql_gr_view_size", Title: "GR 组视图成员数", Category: CatMySQL, Unit: "个", Chart: ChartGauge,
		WorseWhen: WorseLow, Desc: "成员数减少说明节点掉出集群"})
	Register(MetricMeta{Name: "mysql_gr_view_member", Title: "GR 成员状态(1=ONLINE)", Category: CatMySQL, Unit: "", Chart: ChartGauge, WorseWhen: WorseLow})
	Register(MetricMeta{Name: "mysql_opened_tables", Title: "累计打开表", Category: CatMySQL, Unit: "次", Chart: ChartLine, WorseWhen: WorseHigh,
		Desc: "持续增长说明 table_open_cache 太小，每次查询都要重新打开表文件"})
	Register(MetricMeta{Name: "mysql_innodb_log_waits", Title: "InnoDB 日志等待", Category: CatMySQL, Unit: "次", Chart: ChartLine, WorseWhen: WorseHigh,
		Desc: "非零增长说明 redo log 过小或刷盘跟不上写入"})

	// —— PostgreSQL ——
	Register(MetricMeta{Name: "postgres_instance_up", Title: "PostgreSQL 存活", Category: CatPostgres, Unit: "", Chart: ChartGauge, WorseWhen: WorseLow})
	Register(MetricMeta{Name: "postgres_numbackends", Title: "连接数", Category: CatPostgres, Unit: "个", Chart: ChartLine, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "postgres_active_connections", Title: "活跃连接", Category: CatPostgres, Unit: "个", Chart: ChartLine, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "postgres_max_connections", Title: "连接数上限", Category: CatPostgres, Unit: "个", Chart: ChartGauge, NoAlert: true, Desc: "配置值"})
	Register(MetricMeta{Name: "postgres_cache_hit_ratio", Title: "缓存命中率", Category: CatPostgres, Unit: "%", Chart: ChartArea, WorseWhen: WorseLow})
	Register(MetricMeta{Name: "postgres_deadlocks", Title: "死锁次数", Category: CatPostgres, Unit: "次", Chart: ChartLine, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "postgres_replication_lag_bytes", Title: "复制延迟(字节)", Category: CatPostgres, Unit: "B", Chart: ChartLine, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "postgres_query_latency_ms", Title: "查询延迟", Category: CatPostgres, Unit: "ms", Chart: ChartLine, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "postgres_database_size_bytes", Title: "数据库大小", Category: CatPostgres, Unit: "B", Chart: ChartLine, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "postgres_blks_read", Title: "累计磁盘块读", Category: CatPostgres, Unit: "块", Chart: ChartLine, NoAlert: true})
	Register(MetricMeta{Name: "postgres_blks_hit", Title: "累计内存块命中", Category: CatPostgres, Unit: "块", Chart: ChartLine, NoAlert: true})
	Register(MetricMeta{Name: "postgres_tup_fetched", Title: "累计取回行", Category: CatPostgres, Unit: "行", Chart: ChartLine, NoAlert: true})
	Register(MetricMeta{Name: "postgres_tup_inserted", Title: "累计插入行", Category: CatPostgres, Unit: "行", Chart: ChartLine, NoAlert: true})
	Register(MetricMeta{Name: "postgres_tup_updated", Title: "累计更新行", Category: CatPostgres, Unit: "行", Chart: ChartLine, NoAlert: true})
	Register(MetricMeta{Name: "postgres_tup_deleted", Title: "累计删除行", Category: CatPostgres, Unit: "行", Chart: ChartLine, NoAlert: true})
	Register(MetricMeta{Name: "postgres_tup_returned", Title: "累计返回行", Category: CatPostgres, Unit: "行", Chart: ChartLine, NoAlert: true})
	Register(MetricMeta{Name: "postgres_xact_commit", Title: "累计提交事务", Category: CatPostgres, Unit: "次", Chart: ChartLine, NoAlert: true})
	Register(MetricMeta{Name: "postgres_xact_rollback", Title: "累计回滚事务", Category: CatPostgres, Unit: "次", Chart: ChartLine, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "postgres_uptime_seconds", Title: "运行时长", Category: CatPostgres, Unit: "s", Chart: ChartLine, NoAlert: true})
	Register(MetricMeta{Name: "postgres_longest_transaction_seconds", Title: "最长事务时长", Category: CatPostgres, Unit: "s", Chart: ChartLine, WorseWhen: WorseHigh,
		Desc: "长事务会顶住 vacuum、行锁与连接池，是 PG 生产环境最高频的问题来源"})
	Register(MetricMeta{Name: "postgres_idle_in_transaction", Title: "idle in transaction 连接", Category: CatPostgres, Unit: "个", Chart: ChartLine, WorseWhen: WorseHigh,
		Desc: "事务开着的空闲连接，同样阻塞 vacuum 并占住连接"})
	Register(MetricMeta{Name: "postgres_dead_tuples", Title: "死元组数(当前库)", Category: CatPostgres, Unit: "行", Chart: ChartLine, WorseWhen: WorseHigh,
		Desc: "表膨胀的直接来源，持续上涨说明 vacuum 跟不上"})
	Register(MetricMeta{Name: "postgres_replication_slot_lag_bytes", Title: "复制槽 WAL 滞后", Category: CatPostgres, Unit: "B", Chart: ChartLine, WorseWhen: WorseHigh,
		Desc: "失效的复制槽永不推进会导致 WAL 无限堆积直至磁盘打满"})

	// —— Nginx ——
	Register(MetricMeta{Name: "nginx_instance_up", Title: "Nginx 存活", Category: CatNginx, Unit: "", Chart: ChartGauge, WorseWhen: WorseLow})
	Register(MetricMeta{Name: "nginx_active_connections", Title: "活跃连接", Category: CatNginx, Unit: "个", Chart: ChartLine, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "nginx_connection_drop_rate", Title: "连接丢弃速率", Category: CatNginx, Unit: "个/s", Chart: ChartLine,
		WorseWhen: WorseHigh, Desc: "accepts-handled 的增长，说明连接被丢弃（backlog 打满）"})
	Register(MetricMeta{Name: "nginx_requests", Title: "累计请求数", Category: CatNginx, Unit: "个", Chart: ChartLine, NoAlert: true, Desc: "累计计数器"})
	Register(MetricMeta{Name: "nginx_accepts", Title: "累计接受连接", Category: CatNginx, Unit: "个", Chart: ChartLine, NoAlert: true})
	Register(MetricMeta{Name: "nginx_handled", Title: "累计处理连接", Category: CatNginx, Unit: "个", Chart: ChartLine, NoAlert: true})
	Register(MetricMeta{Name: "nginx_reading", Title: "正在读请求头", Category: CatNginx, Unit: "个", Chart: ChartLine, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "nginx_writing", Title: "正在写响应", Category: CatNginx, Unit: "个", Chart: ChartLine, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "nginx_waiting", Title: "空闲连接(keepalive)", Category: CatNginx, Unit: "个", Chart: ChartLine, NoAlert: true})
	Register(MetricMeta{Name: "nginx_access_requests_rate", Title: "访问日志请求速率", Category: CatNginx, Unit: "次/s", Chart: ChartLine,
		Dynamic: true, Desc: "来自访问日志解析（需开启 nginx 访问日志采集）"})
	Register(MetricMeta{Name: "nginx_access_requests", Title: "累计请求数(访问日志)", Category: CatNginx, Unit: "个", Chart: ChartLine, NoAlert: true})
	Register(MetricMeta{Name: "nginx_access_requests_by_status", Title: "按状态码分类请求数", Category: CatNginx, Unit: "个", Chart: ChartBar,
		WorseWhen: WorseHigh, Dynamic: true, Desc: "带状态码维度；5xx 增长是服务端故障的第一信号"})
	Register(MetricMeta{Name: "nginx_access_avg_latency", Title: "平均响应时间(访问日志)", Category: CatNginx, Unit: "ms", Chart: ChartLine, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "nginx_access_bytes", Title: "累计响应字节", Category: CatNginx, Unit: "B", Chart: ChartLine, NoAlert: true})
	Register(MetricMeta{Name: "nginx_access_bytes_rate", Title: "响应字节速率", Category: CatNginx, Unit: "B/s", Chart: ChartLine})

	// —— Kafka ——
	Register(MetricMeta{Name: "kafka_instance_up", Title: "Kafka 存活", Category: CatKafka, Unit: "", Chart: ChartGauge, WorseWhen: WorseLow})
	Register(MetricMeta{Name: "kafka_broker_count", Title: "Broker 数", Category: CatKafka, Unit: "个", Chart: ChartGauge, WorseWhen: WorseLow})
	Register(MetricMeta{Name: "kafka_active_controller_count", Title: "Active Controller 数", Category: CatKafka, Unit: "个", Chart: ChartGauge,
		Desc: "正常恒为 1；大于 1 是脑裂征兆"})
	Register(MetricMeta{Name: "kafka_under_replicated_partitions", Title: "副本不足分区数", Category: CatKafka, Unit: "个", Chart: ChartBar,
		WorseWhen: WorseHigh, Desc: "持续大于 0 说明有 Broker 掉线或磁盘故障"})
	Register(MetricMeta{Name: "kafka_offline_partitions", Title: "离线分区数", Category: CatKafka, Unit: "个", Chart: ChartBar,
		WorseWhen: WorseHigh, Desc: "任何非 0 都意味着分区不可用"})
	Register(MetricMeta{Name: "kafka_consumer_lag_max", Title: "消费积压(最大分组)", Category: CatKafka, Unit: "条", Chart: ChartLine,
		WorseWhen: WorseHigh, Desc: "取所有消费组里的最大值，最贴近「业务是否在堆积」"})
	Register(MetricMeta{Name: "kafka_consumer_lag", Title: "消费积压(按分组)", Category: CatKafka, Unit: "条", Chart: ChartLine,
		WorseWhen: WorseHigh, Desc: "带消费组维度；告警引擎按指标名取样本、不做标签筛选，建议用 kafka_consumer_lag_max"})
	Register(MetricMeta{Name: "kafka_consumer_group_count", Title: "消费组数", Category: CatKafka, Unit: "个", Chart: ChartGauge, NoAlert: true})
	Register(MetricMeta{Name: "kafka_topic_count", Title: "Topic 数", Category: CatKafka, Unit: "个", Chart: ChartGauge, NoAlert: true})
	Register(MetricMeta{Name: "kafka_partition_count", Title: "分区数", Category: CatKafka, Unit: "个", Chart: ChartGauge, NoAlert: true})

	// —— Elasticsearch ——
	Register(MetricMeta{Name: "es_instance_up", Title: "Elasticsearch 存活", Category: CatElasticsearch, Unit: "", Chart: ChartGauge, WorseWhen: WorseLow})
	Register(MetricMeta{Name: "es_cluster_status", Title: "集群健康(0=green/1=yellow/2=red)", Category: CatElasticsearch, Unit: "", Chart: ChartGauge,
		WorseWhen: WorseHigh, Desc: "yellow 表示副本未分配（不丢数据但无冗余），red 表示主分片缺失（数据不可用）"})
	Register(MetricMeta{Name: "es_unassigned_shards", Title: "未分配分片", Category: CatElasticsearch, Unit: "个", Chart: ChartBar,
		WorseWhen: WorseHigh, Desc: "yellow/red 的直接原因，持续不为 0 需查磁盘水位与分片分配策略"})
	Register(MetricMeta{Name: "es_nodes", Title: "节点数", Category: CatElasticsearch, Unit: "个", Chart: ChartGauge, WorseWhen: WorseLow})
	Register(MetricMeta{Name: "es_data_nodes", Title: "数据节点数", Category: CatElasticsearch, Unit: "个", Chart: ChartGauge, WorseWhen: WorseLow})
	Register(MetricMeta{Name: "es_active_shards", Title: "活跃分片", Category: CatElasticsearch, Unit: "个", Chart: ChartGauge, NoAlert: true})
	Register(MetricMeta{Name: "es_primary_shards", Title: "主分片数", Category: CatElasticsearch, Unit: "个", Chart: ChartGauge, NoAlert: true})
	Register(MetricMeta{Name: "es_jvm_heap_used_percent", Title: "JVM 堆使用率(最大节点)", Category: CatElasticsearch, Unit: "%", Chart: ChartArea, WorseWhen: WorseHigh,
		Desc: "超过 85% 通常伴随 GC 抖动与熔断风险，是 ES 最常见的生产故障前兆"})
	Register(MetricMeta{Name: "es_thread_pool_rejected_total", Title: "线程池拒绝(累计)", Category: CatElasticsearch, Unit: "次", Chart: ChartLine, WorseWhen: WorseHigh,
		Desc: "累计计数器（节点重启归零）；增长说明写入/搜索请求被拒，客户端会收到 429/503"})
	Register(MetricMeta{Name: "es_breakers_tripped_total", Title: "熔断器触发(累计)", Category: CatElasticsearch, Unit: "次", Chart: ChartLine, WorseWhen: WorseHigh,
		Desc: "熔断触发时对应请求直接失败（circuit_breaking_exception）"})
	Register(MetricMeta{Name: "es_pending_tasks", Title: "待处理管理任务", Category: CatElasticsearch, Unit: "个", Chart: ChartGauge, WorseWhen: WorseHigh,
		Desc: "长期堆积说明元数据操作（建索引/映射变更）卡住"})

	// —— ClickHouse ——
	Register(MetricMeta{Name: "clickhouse_instance_up", Title: "ClickHouse 存活", Category: CatClickHouse, Unit: "", Chart: ChartGauge, WorseWhen: WorseLow})
	Register(MetricMeta{Name: "clickhouse_queries_running", Title: "运行中查询", Category: CatClickHouse, Unit: "个", Chart: ChartLine, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "clickhouse_merges_running", Title: "运行中合并", Category: CatClickHouse, Unit: "个", Chart: ChartLine,
		WorseWhen: WorseHigh, Desc: "持续偏高说明写入过快或分区设计不合理"})
	Register(MetricMeta{Name: "clickhouse_http_connections", Title: "HTTP 连接数", Category: CatClickHouse, Unit: "个", Chart: ChartLine, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "clickhouse_tcp_connections", Title: "TCP 连接数", Category: CatClickHouse, Unit: "个", Chart: ChartLine, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "clickhouse_uptime_seconds", Title: "运行时长", Category: CatClickHouse, Unit: "s", Chart: ChartLine, NoAlert: true})
	Register(MetricMeta{Name: "clickhouse_active_parts", Title: "活跃 parts 数", Category: CatClickHouse, Unit: "个", Chart: ChartLine, WorseWhen: WorseHigh,
		Desc: "超过 max_parts_in_total（默认 10 万）该表直接拒绝写入，写入过频或分区粒度过细所致"})
	Register(MetricMeta{Name: "clickhouse_replica_delay_seconds", Title: "副本延迟", Category: CatClickHouse, Unit: "s", Chart: ChartLine, WorseWhen: WorseHigh,
		Desc: "仅 Replicated 系列表有数据；单机部署恒为 0"})

	// —— Nacos ——
	Register(MetricMeta{Name: "nacos_instance_up", Title: "Nacos 存活", Category: CatNacos, Unit: "", Chart: ChartGauge, WorseWhen: WorseLow})
	Register(MetricMeta{Name: "nacos_long_connection", Title: "客户端长连接", Category: CatNacos, Unit: "个", Chart: ChartLine, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "nacos_config_count", Title: "配置数", Category: CatNacos, Unit: "个", Chart: ChartGauge, NoAlert: true})
	Register(MetricMeta{Name: "nacos_naming_service_count", Title: "服务数", Category: CatNacos, Unit: "个", Chart: ChartGauge, NoAlert: true})
	Register(MetricMeta{Name: "nacos_subscriber_count", Title: "订阅者数", Category: CatNacos, Unit: "个", Chart: ChartGauge, NoAlert: true})
	Register(MetricMeta{Name: "nacos_jvm_heap_used_bytes", Title: "JVM 堆占用", Category: CatNacos, Unit: "B", Chart: ChartLine, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "nacos_jvm_heap_max_bytes", Title: "JVM 堆上限", Category: CatNacos, Unit: "B", Chart: ChartGauge, NoAlert: true})
	Register(MetricMeta{Name: "nacos_jvm_heap_used_percent", Title: "JVM 堆使用率", Category: CatNacos, Unit: "%", Chart: ChartArea, WorseWhen: WorseHigh,
		Desc: "持续超过 85% 会引发频繁 Full GC，注册中心整体变慢"})
	Register(MetricMeta{Name: "nacos_jvm_threads_live", Title: "JVM 线程数", Category: CatNacos, Unit: "个", Chart: ChartLine, NoAlert: true})

	// —— ZooKeeper ——
	Register(MetricMeta{Name: "zookeeper_instance_up", Title: "ZooKeeper 存活", Category: CatZooKeeper, Unit: "", Chart: ChartGauge, WorseWhen: WorseLow})
	Register(MetricMeta{Name: "zookeeper_avg_latency", Title: "平均延迟", Category: CatZooKeeper, Unit: "ms", Chart: ChartLine, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "zookeeper_outstanding_requests", Title: "排队请求数", Category: CatZooKeeper, Unit: "个", Chart: ChartLine,
		WorseWhen: WorseHigh, Desc: "持续排队说明请求处理跟不上"})
	Register(MetricMeta{Name: "zookeeper_alive_connections", Title: "活跃连接数", Category: CatZooKeeper, Unit: "个", Chart: ChartLine, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "zookeeper_znode_count", Title: "Znode 数", Category: CatZooKeeper, Unit: "个", Chart: ChartLine, NoAlert: true})
	Register(MetricMeta{Name: "zookeeper_followers", Title: "Follower 数", Category: CatZooKeeper, Unit: "个", Chart: ChartGauge, WorseWhen: WorseLow})
	Register(MetricMeta{Name: "zookeeper_synced_followers", Title: "已同步 Follower 数", Category: CatZooKeeper, Unit: "个", Chart: ChartGauge,
		WorseWhen: WorseLow, Desc: "小于 Follower 数说明有节点在追数据"})

	// —— RabbitMQ ——
	Register(MetricMeta{Name: "rabbitmq_instance_up", Title: "RabbitMQ 存活", Category: CatRabbitMQ, Unit: "", Chart: ChartGauge, WorseWhen: WorseLow})
	Register(MetricMeta{Name: "rabbitmq_queue_messages", Title: "队列消息积压", Category: CatRabbitMQ, Unit: "条", Chart: ChartLine,
		WorseWhen: WorseHigh, Desc: "积压持续增长说明消费者跟不上生产"})
	Register(MetricMeta{Name: "rabbitmq_queues", Title: "队列数", Category: CatRabbitMQ, Unit: "个", Chart: ChartGauge, NoAlert: true})
	Register(MetricMeta{Name: "rabbitmq_connections", Title: "连接数", Category: CatRabbitMQ, Unit: "个", Chart: ChartLine, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "rabbitmq_consumers", Title: "消费者数", Category: CatRabbitMQ, Unit: "个", Chart: ChartGauge, WorseWhen: WorseLow})
	Register(MetricMeta{Name: "rabbitmq_publishers", Title: "生产者数", Category: CatRabbitMQ, Unit: "个", Chart: ChartGauge, NoAlert: true})
	Register(MetricMeta{Name: "rabbitmq_fd_used", Title: "已用文件描述符", Category: CatRabbitMQ, Unit: "个", Chart: ChartLine,
		WorseWhen: WorseHigh, Desc: "接近 ulimit 时会出现连接被拒"})
	Register(MetricMeta{Name: "rabbitmq_process_memory_bytes", Title: "进程内存", Category: CatRabbitMQ, Unit: "B", Chart: ChartLine, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "rabbitmq_queue_messages_ready", Title: "待消费消息(ready)", Category: CatRabbitMQ, Unit: "条", Chart: ChartLine, WorseWhen: WorseHigh,
		Desc: "消费者处理能力不足的直接表现"})
	Register(MetricMeta{Name: "rabbitmq_queue_messages_unacked", Title: "未确认消息(unacked)", Category: CatRabbitMQ, Unit: "条", Chart: ChartLine, WorseWhen: WorseHigh,
		Desc: "消费者处理缓慢或挂住时上涨——ready 与 unacked 的分化才是「消费者卡死」的信号，合计值看不出来"})
	Register(MetricMeta{Name: "rabbitmq_alarms_memory_used_watermark", Title: "内存告警(1=触发)", Category: CatRabbitMQ, Unit: "", Chart: ChartGauge, WorseWhen: WorseHigh,
		Desc: "触发后 broker 会阻塞生产者"})
	Register(MetricMeta{Name: "rabbitmq_alarms_free_disk_space_watermark", Title: "磁盘告警(1=触发)", Category: CatRabbitMQ, Unit: "", Chart: ChartGauge, WorseWhen: WorseHigh,
		Desc: "触发后 broker 会阻塞生产者"})
	Register(MetricMeta{Name: "rabbitmq_alarms_file_descriptor_limit", Title: "文件描述符告警(1=触发)", Category: CatRabbitMQ, Unit: "", Chart: ChartGauge, WorseWhen: WorseHigh,
		Desc: "触发后拒绝新连接"})

	// —— Docker ——
	Register(MetricMeta{Name: "docker_container_up", Title: "容器存活", Category: CatDocker, Unit: "", Chart: ChartGauge, WorseWhen: WorseLow})
	Register(MetricMeta{Name: "docker_containers_total", Title: "容器总数", Category: CatDocker, Unit: "个", Chart: ChartGauge, NoAlert: true})
	Register(MetricMeta{Name: "docker_containers_running", Title: "运行中容器", Category: CatDocker, Unit: "个", Chart: ChartGauge, WorseWhen: WorseLow})
	Register(MetricMeta{Name: "docker_containers_stopped", Title: "已停止容器", Category: CatDocker, Unit: "个", Chart: ChartGauge})
	Register(MetricMeta{Name: "docker_containers_paused", Title: "已暂停容器", Category: CatDocker, Unit: "个", Chart: ChartGauge})
	Register(MetricMeta{Name: "docker_container_cpu_percent", Title: "容器 CPU 使用率", Category: CatDocker, Unit: "%", Chart: ChartLine, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "docker_container_mem_percent", Title: "容器内存使用率", Category: CatDocker, Unit: "%", Chart: ChartLine, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "docker_container_mem_usage_bytes", Title: "容器内存用量", Category: CatDocker, Unit: "B", Chart: ChartLine, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "docker_container_mem_limit_bytes", Title: "容器内存上限", Category: CatDocker, Unit: "B", Chart: ChartGauge, NoAlert: true})
	Register(MetricMeta{Name: "docker_container_net_rx_bytes", Title: "容器网络接收", Category: CatDocker, Unit: "B", Chart: ChartLine, NoAlert: true})
	Register(MetricMeta{Name: "docker_container_net_tx_bytes", Title: "容器网络发送", Category: CatDocker, Unit: "B", Chart: ChartLine, NoAlert: true})
	Register(MetricMeta{Name: "docker_container_disk_read_bytes", Title: "容器磁盘读", Category: CatDocker, Unit: "B", Chart: ChartLine, NoAlert: true})
	Register(MetricMeta{Name: "docker_container_disk_write_bytes", Title: "容器磁盘写", Category: CatDocker, Unit: "B", Chart: ChartLine, NoAlert: true})
	Register(MetricMeta{Name: "docker_container_pids_current", Title: "容器进程数", Category: CatDocker, Unit: "个", Chart: ChartLine, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "docker_images_total", Title: "镜像数", Category: CatDocker, Unit: "个", Chart: ChartGauge, NoAlert: true})

	// —— Kubernetes ——
	Register(MetricMeta{Name: "k8s_cluster_up", Title: "Kubernetes 存活", Category: CatK8s, Unit: "", Chart: ChartGauge, WorseWhen: WorseLow})
	Register(MetricMeta{Name: "k8s_nodes_ready", Title: "就绪节点数", Category: CatK8s, Unit: "个", Chart: ChartGauge, WorseWhen: WorseLow})
	Register(MetricMeta{Name: "k8s_nodes_total", Title: "节点总数", Category: CatK8s, Unit: "个", Chart: ChartGauge, NoAlert: true})
	Register(MetricMeta{Name: "k8s_node_ready", Title: "节点就绪状态(1=Ready)", Category: CatK8s, Unit: "", Chart: ChartGauge, WorseWhen: WorseLow})
	Register(MetricMeta{Name: "k8s_node_cpu_usage_cores", Title: "节点 CPU 用量", Category: CatK8s, Unit: "核", Chart: ChartLine, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "k8s_node_mem_usage_bytes", Title: "节点内存用量", Category: CatK8s, Unit: "B", Chart: ChartLine, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "k8s_pods_total", Title: "Pod 总数", Category: CatK8s, Unit: "个", Chart: ChartGauge, NoAlert: true})
	Register(MetricMeta{Name: "k8s_pods_running", Title: "运行中 Pod", Category: CatK8s, Unit: "个", Chart: ChartGauge, WorseWhen: WorseLow})
	Register(MetricMeta{Name: "k8s_pods_failed", Title: "失败 Pod", Category: CatK8s, Unit: "个", Chart: ChartBar, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "k8s_pods_pending", Title: "Pending Pod", Category: CatK8s, Unit: "个", Chart: ChartBar,
		WorseWhen: WorseHigh, Desc: "长期 Pending 通常是资源不足或调度约束无法满足"})
	Register(MetricMeta{Name: "k8s_pods_succeeded", Title: "已完成 Pod", Category: CatK8s, Unit: "个", Chart: ChartGauge, NoAlert: true})
	Register(MetricMeta{Name: "k8s_pod_phase", Title: "Pod 阶段", Category: CatK8s, Unit: "", Chart: ChartGauge, Dynamic: true, Desc: "带 Pod 维度"})
	Register(MetricMeta{Name: "k8s_deployments_total", Title: "Deployment 数", Category: CatK8s, Unit: "个", Chart: ChartGauge, NoAlert: true})
	Register(MetricMeta{Name: "k8s_deployments_unhealthy", Title: "异常 Deployment", Category: CatK8s, Unit: "个", Chart: ChartBar,
		WorseWhen: WorseHigh, Desc: "就绪副本数未达期望的 Deployment 数"})
	Register(MetricMeta{Name: "k8s_deployment_replicas_desired", Title: "Deployment 期望副本", Category: CatK8s, Unit: "个", Chart: ChartLine, NoAlert: true})
	Register(MetricMeta{Name: "k8s_deployment_replicas_ready", Title: "Deployment 就绪副本", Category: CatK8s, Unit: "个", Chart: ChartLine, WorseWhen: WorseLow})
	Register(MetricMeta{Name: "k8s_daemonsets_total", Title: "DaemonSet 数", Category: CatK8s, Unit: "个", Chart: ChartGauge, NoAlert: true})
	Register(MetricMeta{Name: "k8s_daemonsets_unhealthy", Title: "异常 DaemonSet", Category: CatK8s, Unit: "个", Chart: ChartBar, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "k8s_daemonset_desired", Title: "DaemonSet 期望副本", Category: CatK8s, Unit: "个", Chart: ChartLine, NoAlert: true})
	Register(MetricMeta{Name: "k8s_daemonset_ready", Title: "DaemonSet 就绪副本", Category: CatK8s, Unit: "个", Chart: ChartLine, WorseWhen: WorseLow})
	Register(MetricMeta{Name: "k8s_statefulsets_total", Title: "StatefulSet 数", Category: CatK8s, Unit: "个", Chart: ChartGauge, NoAlert: true})
	Register(MetricMeta{Name: "k8s_statefulsets_unhealthy", Title: "异常 StatefulSet", Category: CatK8s, Unit: "个", Chart: ChartBar, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "k8s_statefulset_replicas_desired", Title: "StatefulSet 期望副本", Category: CatK8s, Unit: "个", Chart: ChartLine, NoAlert: true})
	Register(MetricMeta{Name: "k8s_statefulset_replicas_ready", Title: "StatefulSet 就绪副本", Category: CatK8s, Unit: "个", Chart: ChartLine, WorseWhen: WorseLow})

	// —— MongoDB ——
	Register(MetricMeta{Name: "mongodb_up", Title: "MongoDB 存活", Category: CatMongo, Unit: "", Chart: ChartGauge, WorseWhen: WorseLow})
	Register(MetricMeta{Name: "mongodb_connections_current", Title: "当前连接数", Category: CatMongo, Unit: "个", Chart: ChartLine, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "mongodb_connections_available", Title: "可用连接数", Category: CatMongo, Unit: "个", Chart: ChartGauge, WorseWhen: WorseLow})
	Register(MetricMeta{Name: "mongodb_mem_resident_bytes", Title: "常驻内存", Category: CatMongo, Unit: "B", Chart: ChartLine, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "mongodb_mem_virtual_bytes", Title: "虚拟内存", Category: CatMongo, Unit: "B", Chart: ChartLine, NoAlert: true})
	Register(MetricMeta{Name: "mongodb_repl_health", Title: "副本集健康(1=正常)", Category: CatMongo, Unit: "", Chart: ChartGauge, WorseWhen: WorseLow})
	Register(MetricMeta{Name: "mongodb_repl_lag", Title: "副本延迟", Category: CatMongo, Unit: "s", Chart: ChartLine, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "mongodb_repl_state", Title: "副本角色(1=主)", Category: CatMongo, Unit: "", Chart: ChartGauge, NoAlert: true})
	Register(MetricMeta{Name: "mongodb_opcounters_", Title: "操作计数(按操作类型)", Category: CatMongo, Unit: "次", Chart: ChartLine,
		Dynamic: true, Desc: "名字由 mongodb_opcounters_ + 操作名拼成（insert/query/update/delete…），如 mongodb_opcounters_query"})
	Register(MetricMeta{Name: "mongodb_db_dataSize_bytes", Title: "库数据大小", Category: CatMongo, Unit: "B", Chart: ChartLine, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "mongodb_db_storageSize_bytes", Title: "库存储大小", Category: CatMongo, Unit: "B", Chart: ChartLine, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "mongodb_db_indexSize_bytes", Title: "库索引大小", Category: CatMongo, Unit: "B", Chart: ChartLine, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "mongodb_db_indexes", Title: "库索引数", Category: CatMongo, Unit: "个", Chart: ChartGauge, NoAlert: true})
	Register(MetricMeta{Name: "mongodb_db_objects", Title: "库对象数", Category: CatMongo, Unit: "个", Chart: ChartGauge, NoAlert: true})
	Register(MetricMeta{Name: "mongodb_uptime_seconds", Title: "运行时长", Category: CatMongo, Unit: "s", Chart: ChartLine, NoAlert: true})
	Register(MetricMeta{Name: "mongodb_version", Title: "版本", Category: CatMongo, Unit: "", Chart: ChartGauge, NoAlert: true, Desc: "信息类指标"})
	Register(MetricMeta{Name: "mongodb_instance_version", Title: "实例版本", Category: CatMongo, Unit: "", Chart: ChartGauge, NoAlert: true})
	Register(MetricMeta{Name: "mongodb_tickets_available_reads", Title: "可用读 tickets", Category: CatMongo, Unit: "个", Chart: ChartLine, WorseWhen: WorseLow,
		Desc: "归零意味着整库所有读请求排队，是 Mongo「突然全慢」的最典型原因"})
	Register(MetricMeta{Name: "mongodb_tickets_available_writes", Title: "可用写 tickets", Category: CatMongo, Unit: "个", Chart: ChartLine, WorseWhen: WorseLow,
		Desc: "归零意味着整库所有写请求排队"})
	Register(MetricMeta{Name: "mongodb_wt_cache_used_bytes", Title: "WiredTiger 缓存占用", Category: CatMongo, Unit: "B", Chart: ChartLine, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "mongodb_wt_cache_max_bytes", Title: "WiredTiger 缓存上限", Category: CatMongo, Unit: "B", Chart: ChartGauge, NoAlert: true})
	Register(MetricMeta{Name: "mongodb_wt_cache_used_percent", Title: "WiredTiger 缓存使用率", Category: CatMongo, Unit: "%", Chart: ChartArea, WorseWhen: WorseHigh,
		Desc: "逼近上限后读写延迟明显抖动，通常需要加大 wiredTigerCacheSizeGB"})
	Register(MetricMeta{Name: "mongodb_oplog_window_hours", Title: "oplog 窗口(小时)", Category: CatMongo, Unit: "h", Chart: ChartLine, WorseWhen: WorseLow,
		Desc: "窗口过小意味着从库追不上时会触发全量重同步"})

	// —— RocketMQ ——
	Register(MetricMeta{Name: "rocketmq_instance_up", Title: "RocketMQ 存活", Category: CatRocketMQ, Unit: "", Chart: ChartGauge, WorseWhen: WorseLow})
	Register(MetricMeta{Name: "rocketmq_message_accumulation", Title: "消息积压", Category: CatRocketMQ, Unit: "条", Chart: ChartLine,
		WorseWhen: WorseHigh, Desc: "积压持续增长说明消费跟不上"})
	Register(MetricMeta{Name: "rocketmq_consumer_lag", Title: "消费延迟", Category: CatRocketMQ, Unit: "条", Chart: ChartLine, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "rocketmq_broker_count", Title: "Broker 数", Category: CatRocketMQ, Unit: "个", Chart: ChartGauge, WorseWhen: WorseLow})
	Register(MetricMeta{Name: "rocketmq_producer_tps", Title: "生产 TPS", Category: CatRocketMQ, Unit: "次/s", Chart: ChartLine, NoAlert: true})
	Register(MetricMeta{Name: "rocketmq_consumer_tps", Title: "消费 TPS", Category: CatRocketMQ, Unit: "次/s", Chart: ChartLine, NoAlert: true})
	Register(MetricMeta{Name: "rocketmq_broker_tps", Title: "Broker TPS", Category: CatRocketMQ, Unit: "次/s", Chart: ChartLine, NoAlert: true})
	Register(MetricMeta{Name: "rocketmq_topic_count", Title: "Topic 数", Category: CatRocketMQ, Unit: "个", Chart: ChartGauge, NoAlert: true})
	Register(MetricMeta{Name: "rocketmq_consumer_group_count", Title: "消费组数", Category: CatRocketMQ, Unit: "个", Chart: ChartGauge, NoAlert: true})
	Register(MetricMeta{Name: "rocketmq_uptime_seconds", Title: "运行时长", Category: CatRocketMQ, Unit: "s", Chart: ChartLine, NoAlert: true})

	// —— FastDFS ——
	Register(MetricMeta{Name: "fastdfs_up", Title: "FastDFS 存活", Category: CatFastDFS, Unit: "", Chart: ChartGauge, WorseWhen: WorseLow})
	Register(MetricMeta{Name: "fastdfs_storage_count", Title: "Storage 节点数", Category: CatFastDFS, Unit: "个", Chart: ChartGauge, WorseWhen: WorseLow})
	Register(MetricMeta{Name: "fastdfs_storage_online_count", Title: "在线 Storage", Category: CatFastDFS, Unit: "个", Chart: ChartLine, WorseWhen: WorseLow})
	Register(MetricMeta{Name: "fastdfs_storage_offline_count", Title: "离线 Storage", Category: CatFastDFS, Unit: "个", Chart: ChartLine, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "fastdfs_group_count", Title: "Group 数", Category: CatFastDFS, Unit: "个", Chart: ChartGauge, NoAlert: true})
	Register(MetricMeta{Name: "fastdfs_total_space", Title: "总空间", Category: CatFastDFS, Unit: "B", Chart: ChartLine, NoAlert: true})
	Register(MetricMeta{Name: "fastdfs_free_space", Title: "空闲空间", Category: CatFastDFS, Unit: "B", Chart: ChartLine, WorseWhen: WorseLow})
	Register(MetricMeta{Name: "fastdfs_used_space", Title: "已用空间", Category: CatFastDFS, Unit: "B", Chart: ChartLine, WorseWhen: WorseHigh})
	Register(MetricMeta{Name: "fastdfs_trunk_free_space", Title: "Trunk 空闲空间", Category: CatFastDFS, Unit: "B", Chart: ChartLine, WorseWhen: WorseLow})
	Register(MetricMeta{Name: "fastdfs_disk_read_bytes", Title: "磁盘读字节", Category: CatFastDFS, Unit: "B", Chart: ChartLine, NoAlert: true})
	Register(MetricMeta{Name: "fastdfs_disk_write_bytes", Title: "磁盘写字节", Category: CatFastDFS, Unit: "B", Chart: ChartLine, NoAlert: true})
	Register(MetricMeta{Name: "fastdfs_net_recv_bytes", Title: "网络接收字节", Category: CatFastDFS, Unit: "B", Chart: ChartLine, NoAlert: true})
	Register(MetricMeta{Name: "fastdfs_net_sent_bytes", Title: "网络发送字节", Category: CatFastDFS, Unit: "B", Chart: ChartLine, NoAlert: true})
}
