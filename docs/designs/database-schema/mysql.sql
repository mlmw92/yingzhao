-- yingzhao 状态库最终表结构（mysql）

CREATE TABLE IF NOT EXISTS config_documents (
  namespace   VARCHAR(64)  NOT NULL COMMENT '配置命名空间（notify / ui / alert_rules …）',
  version     BIGINT       NOT NULL DEFAULT 1 COMMENT '乐观锁版本号，每次写入 +1',
  payload     JSON         NOT NULL COMMENT 'JSON 信封：format / encoding / nonce / data',
  updated_at  BIGINT       NOT NULL COMMENT '最后写入时间（epoch ms）',
  updated_by  VARCHAR(128) NOT NULL DEFAULT '' COMMENT '最后写入人（预留）',
  PRIMARY KEY (namespace)
) COMMENT='整份读写的配置文档（JSON 信封；敏感内容 AES-GCM 加密）' ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS nodes(
  hostname     VARCHAR(255) NOT NULL COMMENT '主机名（主键，Agent 上报原样）',
  mode         VARCHAR(32)  NOT NULL DEFAULT '' COMMENT '运行模式 collect / edge / hub',
  display_name VARCHAR(255) NOT NULL DEFAULT '' COMMENT '界面显示名（人工设置，不改 hostname）',
  ip           VARCHAR(64)  NOT NULL DEFAULT '' COMMENT '最近上报 IP',
  os           VARCHAR(128) NOT NULL DEFAULT '' COMMENT '操作系统',
  arch         VARCHAR(64)  NOT NULL DEFAULT '' COMMENT 'CPU 架构',
  group_name   VARCHAR(128) NOT NULL DEFAULT '' COMMENT '所属分组（Server 端管理）',
  labels       JSON         NULL COMMENT '标签 JSON',
  version      VARCHAR(64)  NOT NULL DEFAULT '' COMMENT 'Agent 版本',
  host_info    JSON         NULL COMMENT '主机硬件信息 JSON（注册级字段）',
  log_sources  JSON         NULL COMMENT '日志来源 JSON',
  status       VARCHAR(32)  NOT NULL DEFAULT '' COMMENT 'online / offline',
  last_seen    BIGINT       NOT NULL DEFAULT 0 COMMENT '最近心跳时间（epoch ms）',
  created_at   BIGINT       NOT NULL DEFAULT 0 COMMENT '首次注册时间',
  updated_at   BIGINT       NOT NULL DEFAULT 0 COMMENT '最近写入时间',
  PRIMARY KEY (hostname),
  KEY idx_nodes_group(group_name)
) COMMENT='节点注册信息与最近心跳' ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS node_groups(
  name        VARCHAR(128) NOT NULL COMMENT '分组名（主键）',
  description VARCHAR(255) NOT NULL DEFAULT '' COMMENT '分组说明',
  created_at  BIGINT       NOT NULL DEFAULT 0 COMMENT '创建时间',
  PRIMARY KEY (name)
) COMMENT='节点分组' ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS asset_types(
		`key`  VARCHAR(64) PRIMARY KEY COMMENT '类型键（host / redis / mysql …）' COMMENT '类型键（host / redis / mysql …）',
		title   VARCHAR(255) NOT NULL DEFAULT '' COMMENT '显示名' COMMENT '显示名',
		builtin TINYINT NOT NULL DEFAULT 0 COMMENT '是否内置类型（1/0）' COMMENT '是否内置类型（1/0）',
		`schema`  LONGTEXT NOT NULL DEFAULT ('{}') COMMENT '类型模型 JSON（关注字段、属性元数据）' COMMENT '类型模型 JSON（关注字段、属性元数据）'
	) COMMENT='资产类型与类型模型' COMMENT='资产类型与类型模型' ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS assets(
		id          BIGINT AUTO_INCREMENT PRIMARY KEY COMMENT '自增主键' COMMENT '自增主键',
		type_key    VARCHAR(64) NOT NULL COMMENT '资产类型键' COMMENT '资产类型键',
		natural_key VARCHAR(255) NOT NULL COMMENT '自然键（类型内唯一：hostname / host:port …）' COMMENT '自然键（类型内唯一：hostname / host:port …）',
		name        VARCHAR(255) NOT NULL DEFAULT '' COMMENT '显示名' COMMENT '显示名',
		node        VARCHAR(255) NOT NULL DEFAULT '' COMMENT '归属节点' COMMENT '归属节点',
		created_at  BIGINT NOT NULL COMMENT '创建时间' COMMENT '创建时间',
		updated_at  BIGINT NOT NULL COMMENT '最近更新时间' COMMENT '最近更新时间',
		UNIQUE KEY uk_assets_type_natural(type_key, natural_key),
		KEY idx_assets_node(node),
		KEY idx_assets_type(type_key),
		CONSTRAINT fk_assets_type FOREIGN KEY(type_key) REFERENCES asset_types(`key`)
	) COMMENT='资产台账主表' COMMENT='资产台账主表' ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS asset_attrs(
		asset_id   BIGINT NOT NULL COMMENT '资产 ID' COMMENT '资产 ID',
		`key`      VARCHAR(128) NOT NULL COMMENT '属性键' COMMENT '属性键',
		value      LONGTEXT NOT NULL COMMENT '属性值（文本）' COMMENT '属性值（文本）',
		source     VARCHAR(32) NOT NULL COMMENT '来源 discovery / manual / derived' COMMENT '来源 discovery / manual / derived',
		updated_at BIGINT NOT NULL COMMENT '更新时间' COMMENT '更新时间',
		updated_by VARCHAR(128) NOT NULL DEFAULT '' COMMENT '更新人' COMMENT '更新人',
		PRIMARY KEY(asset_id, `key`, source),
		CONSTRAINT fk_attrs_asset FOREIGN KEY(asset_id) REFERENCES assets(id) ON DELETE CASCADE
	) COMMENT='资产属性（采集 / 人工 / 派生）' COMMENT='资产属性（采集 / 人工 / 派生）' ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS asset_links(
		id         BIGINT AUTO_INCREMENT PRIMARY KEY COMMENT '自增主键' COMMENT '自增主键',
		from_id    BIGINT NOT NULL COMMENT '起点资产 ID' COMMENT '起点资产 ID',
		to_id      BIGINT NOT NULL COMMENT '终点资产 ID' COMMENT '终点资产 ID',
		kind       VARCHAR(64) NOT NULL COMMENT '关系类型（runs_on / depends_on …）' COMMENT '关系类型（runs_on / depends_on …）',
		source     VARCHAR(32) NOT NULL DEFAULT 'discovery' COMMENT '来源 discovery / manual' COMMENT '来源 discovery / manual',
		created_at BIGINT NOT NULL COMMENT '创建时间' COMMENT '创建时间',
		UNIQUE KEY uk_links(from_id, to_id, kind),
		KEY idx_links_from(from_id),
		KEY idx_links_to(to_id),
		CONSTRAINT fk_links_from FOREIGN KEY(from_id) REFERENCES assets(id) ON DELETE CASCADE,
		CONSTRAINT fk_links_to FOREIGN KEY(to_id) REFERENCES assets(id) ON DELETE CASCADE
	) COMMENT='资产关系' COMMENT='资产关系' ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS asset_link_suppressions(
		id         BIGINT AUTO_INCREMENT PRIMARY KEY COMMENT '自增主键' COMMENT '自增主键',
		from_id    BIGINT NOT NULL COMMENT '起点资产 ID' COMMENT '起点资产 ID',
		to_id      BIGINT NOT NULL COMMENT '终点资产 ID' COMMENT '终点资产 ID',
		kind       VARCHAR(64) NOT NULL COMMENT '关系类型' COMMENT '关系类型',
		created_at BIGINT NOT NULL COMMENT '抑制时间' COMMENT '抑制时间',
		created_by VARCHAR(128) NOT NULL DEFAULT '' COMMENT '操作人' COMMENT '操作人',
		UNIQUE KEY uk_link_supp(from_id, to_id, kind),
		KEY idx_link_supp_from(from_id),
		KEY idx_link_supp_to(to_id),
		CONSTRAINT fk_supp_from FOREIGN KEY(from_id) REFERENCES assets(id) ON DELETE CASCADE,
		CONSTRAINT fk_supp_to FOREIGN KEY(to_id) REFERENCES assets(id) ON DELETE CASCADE
	) COMMENT='人工删除过的采集关系（阻止采集侧重建）' COMMENT='人工删除过的采集关系（阻止采集侧重建）' ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS asset_changes(
		id         BIGINT AUTO_INCREMENT PRIMARY KEY COMMENT '自增主键' COMMENT '自增主键',
		asset_id   BIGINT NOT NULL COMMENT '资产 ID' COMMENT '资产 ID',
		field      VARCHAR(128) NOT NULL COMMENT '变更字段' COMMENT '变更字段',
		old_value  LONGTEXT NOT NULL COMMENT '旧值' COMMENT '旧值',
		new_value  LONGTEXT NOT NULL COMMENT '新值' COMMENT '新值',
		source     VARCHAR(32) NOT NULL DEFAULT '' COMMENT '变更来源' COMMENT '变更来源',
		actor      VARCHAR(128) NOT NULL DEFAULT '' COMMENT '操作人 / 采集器' COMMENT '操作人 / 采集器',
		kind       VARCHAR(64) NOT NULL COMMENT '变更类型（create / update / delete …）' COMMENT '变更类型（create / update / delete …）',
		at         BIGINT NOT NULL COMMENT '变更时间（epoch ms）' COMMENT '变更时间（epoch ms）',
		request_id VARCHAR(64) NOT NULL DEFAULT '' COMMENT '关联请求 ID（与审计对齐）' COMMENT '关联请求 ID（与审计对齐）',
		KEY idx_changes_asset(asset_id, at),
		KEY idx_changes_request(request_id),
		CONSTRAINT fk_changes_asset FOREIGN KEY(asset_id) REFERENCES assets(id) ON DELETE CASCADE
	) COMMENT='资产字段级变更记录' COMMENT='资产字段级变更记录' ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS snapshots(
		id       BIGINT AUTO_INCREMENT PRIMARY KEY COMMENT '自增主键' COMMENT '自增主键',
		asset_id BIGINT NOT NULL COMMENT '资产 ID' COMMENT '资产 ID',
		taken_at BIGINT NOT NULL COMMENT '快照时间' COMMENT '快照时间',
		KEY idx_snapshots_asset(asset_id, taken_at),
		CONSTRAINT fk_snapshots_asset FOREIGN KEY(asset_id) REFERENCES assets(id) ON DELETE CASCADE
	) COMMENT='配置快照头' COMMENT='配置快照头' ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS snapshot_fields(
		snapshot_id BIGINT NOT NULL COMMENT '快照 ID' COMMENT '快照 ID',
		`key`         VARCHAR(128) NOT NULL COMMENT '字段名' COMMENT '字段名',
		value       LONGTEXT NOT NULL COMMENT '字段值' COMMENT '字段值',
		PRIMARY KEY(snapshot_id, `key`),
		CONSTRAINT fk_sfields_snapshot FOREIGN KEY(snapshot_id) REFERENCES snapshots(id) ON DELETE CASCADE
	) COMMENT='配置快照字段' COMMENT='配置快照字段' ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS asset_seen(
		asset_id     BIGINT PRIMARY KEY COMMENT '资产 ID（主键）' COMMENT '资产 ID（主键）',
		last_seen_at BIGINT NOT NULL COMMENT '最近一次采集上报时间' COMMENT '最近一次采集上报时间',
		CONSTRAINT fk_seen_asset FOREIGN KEY(asset_id) REFERENCES assets(id) ON DELETE CASCADE
	) COMMENT='资产最近一次采集上报时间' COMMENT='资产最近一次采集上报时间' ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS inspect_runs(
		id         BIGINT AUTO_INCREMENT PRIMARY KEY COMMENT '自增主键' COMMENT '自增主键',
		scope      VARCHAR(255) NOT NULL DEFAULT '' COMMENT '巡检范围' COMMENT '巡检范围',
		actor      VARCHAR(128) NOT NULL DEFAULT '' COMMENT '触发人 / 调度器' COMMENT '触发人 / 调度器',
		started_at BIGINT NOT NULL COMMENT '开始时间' COMMENT '开始时间',
		assets     INT NOT NULL DEFAULT 0 COMMENT '巡检资产数' COMMENT '巡检资产数',
		baselined  INT NOT NULL DEFAULT 0 COMMENT '命中标杆数' COMMENT '命中标杆数',
		findings   INT NOT NULL DEFAULT 0 COMMENT '差异数' COMMENT '差异数',
		truncated  INT NOT NULL DEFAULT 0 COMMENT '是否因上限截断（1/0）' COMMENT '是否因上限截断（1/0）',
		KEY idx_inspect_runs_at(started_at)
	) COMMENT='巡检批次' COMMENT='巡检批次' ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS inspect_run_manifest(
		run_id BIGINT PRIMARY KEY COMMENT '巡检批次 ID（主键）' COMMENT '巡检批次 ID（主键）',
		CONSTRAINT fk_manifest_run FOREIGN KEY(run_id) REFERENCES inspect_runs(id) ON DELETE CASCADE
	) COMMENT='巡检批次清单标记' COMMENT='巡检批次清单标记' ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS inspect_run_members(
		run_id      BIGINT NOT NULL COMMENT '巡检批次 ID' COMMENT '巡检批次 ID',
		asset_id    BIGINT NOT NULL COMMENT '资产 ID' COMMENT '资产 ID',
		node_at_run VARCHAR(255) NOT NULL DEFAULT '' COMMENT '巡检时的归属节点' COMMENT '巡检时的归属节点',
		baselined   INT NOT NULL DEFAULT 0 COMMENT '该资产是否命中标杆（1/0）' COMMENT '该资产是否命中标杆（1/0）',
		findings    INT NOT NULL DEFAULT 0 COMMENT '该资产的差异数' COMMENT '该资产的差异数',
		PRIMARY KEY(run_id, asset_id),
		KEY idx_inspect_members_asset(asset_id, run_id),
		CONSTRAINT fk_members_run FOREIGN KEY(run_id) REFERENCES inspect_runs(id) ON DELETE CASCADE
	) COMMENT='巡检批次包含的资产' COMMENT='巡检批次包含的资产' ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS inspect_findings(
		id         BIGINT AUTO_INCREMENT PRIMARY KEY COMMENT '自增主键' COMMENT '自增主键',
		run_id     BIGINT NOT NULL COMMENT '巡检批次 ID' COMMENT '巡检批次 ID',
		asset_id   BIGINT NOT NULL COMMENT '资产 ID' COMMENT '资产 ID',
		asset_type VARCHAR(64) NOT NULL DEFAULT '' COMMENT '资产类型键（快照）' COMMENT '资产类型键（快照）',
		asset_key  VARCHAR(255) NOT NULL DEFAULT '' COMMENT '资产自然键（快照）' COMMENT '资产自然键（快照）',
		asset_name VARCHAR(255) NOT NULL DEFAULT '' COMMENT '资产显示名（快照）' COMMENT '资产显示名（快照）',
		node       VARCHAR(255) NOT NULL DEFAULT '' COMMENT '归属节点（快照）' COMMENT '归属节点（快照）',
		field      VARCHAR(128) NOT NULL DEFAULT '' COMMENT '差异字段' COMMENT '差异字段',
		kind       VARCHAR(64) NOT NULL COMMENT '差异类型（missing / extra / changed …）' COMMENT '差异类型（missing / extra / changed …）',
		level      VARCHAR(32) NOT NULL DEFAULT '' COMMENT '级别（warning / critical …）' COMMENT '级别（warning / critical …）',
		expected   LONGTEXT NOT NULL COMMENT '期望值' COMMENT '期望值',
		actual     LONGTEXT NOT NULL COMMENT '实际值' COMMENT '实际值',
		at         BIGINT NOT NULL COMMENT '产生时间' COMMENT '产生时间',
		KEY idx_inspect_findings_run(run_id, level, kind),
		CONSTRAINT fk_findings_run FOREIGN KEY(run_id) REFERENCES inspect_runs(id) ON DELETE CASCADE
	) COMMENT='巡检差异项' COMMENT='巡检差异项' ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS inspect_finding_baselines(
		finding_id BIGINT PRIMARY KEY COMMENT '差异项 ID（主键）' COMMENT '差异项 ID（主键）',
		asset_id   BIGINT NOT NULL COMMENT '标杆来源资产 ID' COMMENT '标杆来源资产 ID',
		CONSTRAINT fk_fbase_finding FOREIGN KEY(finding_id) REFERENCES inspect_findings(id) ON DELETE CASCADE
	) COMMENT='差异项与标杆来源的关联' COMMENT='差异项与标杆来源的关联' ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS inspect_baselines(
		type_key    VARCHAR(64) PRIMARY KEY COMMENT '资产类型键（主键）' COMMENT '资产类型键（主键）',
		asset_id    BIGINT NOT NULL COMMENT '标杆资产 ID' COMMENT '标杆资产 ID',
		asset_key   VARCHAR(255) NOT NULL DEFAULT '' COMMENT '标杆资产自然键（快照）' COMMENT '标杆资产自然键（快照）',
		snapshot_id BIGINT NOT NULL COMMENT '标杆快照 ID' COMMENT '标杆快照 ID',
		set_by      VARCHAR(128) NOT NULL DEFAULT '' COMMENT '设置人' COMMENT '设置人',
		set_at      BIGINT NOT NULL COMMENT '设置时间' COMMENT '设置时间'
	) COMMENT='资产类型的巡检标杆' COMMENT='资产类型的巡检标杆' ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS asset_ignored(
		asset_id   BIGINT PRIMARY KEY COMMENT '资产 ID（主键）' COMMENT '资产 ID（主键）',
		reason     LONGTEXT NOT NULL COMMENT '忽略原因' COMMENT '忽略原因',
		ignored_by VARCHAR(128) NOT NULL DEFAULT '' COMMENT '操作人' COMMENT '操作人',
		ignored_at BIGINT NOT NULL COMMENT '忽略时间' COMMENT '忽略时间',
		CONSTRAINT fk_ignored_asset FOREIGN KEY(asset_id) REFERENCES assets(id) ON DELETE CASCADE
	) COMMENT='人工忽略的资产' COMMENT='人工忽略的资产' ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS asset_labels(
		asset_id   BIGINT NOT NULL COMMENT '资产 ID' COMMENT '资产 ID',
		`key`      VARCHAR(128) NOT NULL COMMENT '标签键' COMMENT '标签键',
		value      VARCHAR(512) NOT NULL DEFAULT '' COMMENT '标签值' COMMENT '标签值',
		updated_at BIGINT NOT NULL COMMENT '更新时间' COMMENT '更新时间',
		updated_by VARCHAR(128) NOT NULL DEFAULT '' COMMENT '更新人' COMMENT '更新人',
		PRIMARY KEY(asset_id, `key`),
		KEY idx_asset_labels_kv(`key`, value),
		CONSTRAINT fk_labels_asset FOREIGN KEY(asset_id) REFERENCES assets(id) ON DELETE CASCADE
	) COMMENT='资产标签' COMMENT='资产标签' ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS audit_events(
		id         BIGINT AUTO_INCREMENT PRIMARY KEY COMMENT '自增主键' COMMENT '自增主键',
		time_ms    BIGINT NOT NULL COMMENT '事件时间（epoch ms）' COMMENT '事件时间（epoch ms）',
		user_name  VARCHAR(191) NOT NULL DEFAULT '' COMMENT '操作人' COMMENT '操作人',
		method     VARCHAR(32) NOT NULL DEFAULT '' COMMENT 'HTTP 方法' COMMENT 'HTTP 方法',
		path       VARCHAR(1024) NOT NULL DEFAULT '' COMMENT '请求路径' COMMENT '请求路径',
		status     INT NOT NULL DEFAULT 0 COMMENT 'HTTP 状态码' COMMENT 'HTTP 状态码',
		remote_ip  VARCHAR(64) NOT NULL DEFAULT '' COMMENT '来源 IP' COMMENT '来源 IP',
		source_loc VARCHAR(191) NOT NULL DEFAULT '' COMMENT 'IP 属地' COMMENT 'IP 属地',
		succeeded  TINYINT NOT NULL DEFAULT 0 COMMENT '是否成功（1/0）' COMMENT '是否成功（1/0）',
		category   VARCHAR(128) NOT NULL DEFAULT '' COMMENT '分类（asset / ops / security …）' COMMENT '分类（asset / ops / security …）',
		action     VARCHAR(191) NOT NULL DEFAULT '' COMMENT '动作标识' COMMENT '动作标识',
		detail     LONGTEXT NOT NULL COMMENT '详情' COMMENT '详情',
		request_id VARCHAR(64) NOT NULL DEFAULT '' COMMENT '请求关联 ID' COMMENT '请求关联 ID',
		seq        BIGINT NOT NULL DEFAULT 0 COMMENT '链上序号（0 = 启用链之前的历史行）' COMMENT '链上序号（0 = 启用链之前的历史行）',
		prev_hash  VARCHAR(64) NOT NULL DEFAULT '' COMMENT '前一条哈希' COMMENT '前一条哈希',
		hash       VARCHAR(64) NOT NULL DEFAULT '' COMMENT '本条哈希' COMMENT '本条哈希',
		key_fp     VARCHAR(64) NOT NULL DEFAULT '' COMMENT '链密钥指纹' COMMENT '链密钥指纹',
		KEY idx_audit_events_time(time_ms),
		KEY idx_audit_events_user(user_name, time_ms),
		KEY idx_audit_events_request(request_id),
		KEY idx_audit_events_seq(seq)
	) COMMENT='审计事件（seq + prev_hash + hash 组成哈希链）' COMMENT='审计事件（seq + prev_hash + hash 组成哈希链）' ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS audit_chain(
		id              INT PRIMARY KEY COMMENT '固定为 1（单行）' COMMENT '固定为 1（单行）',
		seq             BIGINT NOT NULL DEFAULT 0 COMMENT '当前链头序号' COMMENT '当前链头序号',
		hash            VARCHAR(64) NOT NULL DEFAULT '' COMMENT '当前链头哈希' COMMENT '当前链头哈希',
		key_fingerprint VARCHAR(64) NOT NULL DEFAULT '' COMMENT '链密钥指纹' COMMENT '链密钥指纹',
		updated_ms      BIGINT NOT NULL DEFAULT 0 COMMENT '更新时间' COMMENT '更新时间',
		CONSTRAINT chk_audit_chain_id CHECK(id = 1)
	) COMMENT='审计链头（单行，FOR UPDATE 串行追加）' COMMENT='审计链头（单行，FOR UPDATE 串行追加）' ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS audit_pruned(
		from_seq  BIGINT NOT NULL COMMENT '裁剪区间起始 seq' COMMENT '裁剪区间起始 seq',
		to_seq    BIGINT NOT NULL COMMENT '裁剪区间结束 seq' COMMENT '裁剪区间结束 seq',
		head_hash VARCHAR(64) NOT NULL DEFAULT '' COMMENT '区间末条哈希（校验锚点）' COMMENT '区间末条哈希（校验锚点）',
		`rows` BIGINT NOT NULL DEFAULT 0 COMMENT '被裁行数' COMMENT '被裁行数',
		pruned_ms BIGINT NOT NULL DEFAULT 0 COMMENT '裁剪时间' COMMENT '裁剪时间',
		mac       VARCHAR(64) NOT NULL DEFAULT '' COMMENT '裁剪记录的 HMAC' COMMENT '裁剪记录的 HMAC',
		KEY idx_audit_pruned_range(from_seq, to_seq)
	) COMMENT='合法裁剪区间与 HMAC 锚点' COMMENT='合法裁剪区间与 HMAC 锚点' ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS alert_acks(
		ack_key       VARCHAR(512) PRIMARY KEY COMMENT '处置键 rule|host|instance|startsAt' COMMENT '处置键 rule|host|instance|startsAt',
		rule          VARCHAR(512) NOT NULL DEFAULT '' COMMENT '规则名' COMMENT '规则名',
		host          VARCHAR(255) NOT NULL DEFAULT '' COMMENT '主机' COMMENT '主机',
		instance      VARCHAR(1024) NOT NULL DEFAULT '' COMMENT '实例' COMMENT '实例',
		starts_at     BIGINT NOT NULL DEFAULT 0 COMMENT '告警触发时间' COMMENT '告警触发时间',
		status        VARCHAR(32) NOT NULL DEFAULT 'pending' COMMENT 'pending / ack / closed' COMMENT 'pending / ack / closed',
		user_name     VARCHAR(128) NOT NULL DEFAULT '' COMMENT '最近操作人' COMMENT '最近操作人',
		assignee      VARCHAR(128) NOT NULL DEFAULT '' COMMENT '当前处理人' COMMENT '当前处理人',
		time_ms       BIGINT NOT NULL DEFAULT 0 COMMENT '最近操作时间' COMMENT '最近操作时间',
		ack_time_ms   BIGINT NOT NULL DEFAULT 0 COMMENT '认领时间' COMMENT '认领时间',
		close_time_ms BIGINT NOT NULL DEFAULT 0 COMMENT '关闭时间' COMMENT '关闭时间',
		close_reason  LONGTEXT NOT NULL COMMENT '关闭原因' COMMENT '关闭原因',
		comments      LONGTEXT NOT NULL COMMENT '评论 JSON' COMMENT '评论 JSON',
		KEY idx_alert_acks_time(time_ms),
		KEY idx_alert_acks_status(status, time_ms)
	) COMMENT='告警处置（认领 / 指派 / 关闭 / 评论）' COMMENT='告警处置（认领 / 指派 / 关闭 / 评论）' ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS ops_tasks(
		id         VARCHAR(64) PRIMARY KEY COMMENT '任务 ID（ops-N / ob-N）' COMMENT '任务 ID（ops-N / ob-N）',
		batch_id   VARCHAR(64) NOT NULL DEFAULT '' COMMENT '批次 ID' COMMENT '批次 ID',
		node       VARCHAR(255) NOT NULL DEFAULT '' COMMENT '目标节点' COMMENT '目标节点',
		kind       VARCHAR(64) NOT NULL DEFAULT '' COMMENT '动作标识' COMMENT '动作标识',
		state      VARCHAR(32) NOT NULL DEFAULT '' COMMENT 'queued / running / succeeded / failed / canceled / expired' COMMENT 'queued / running / succeeded / failed / canceled / expired',
		created_at BIGINT NOT NULL DEFAULT 0 COMMENT '创建时间' COMMENT '创建时间',
		done_at    BIGINT NOT NULL DEFAULT 0 COMMENT '结束时间' COMMENT '结束时间',
		task_json  LONGTEXT NOT NULL COMMENT '完整任务 JSON（参数 / 结果 / 操作人）' COMMENT '完整任务 JSON（参数 / 结果 / 操作人）',
		KEY idx_ops_tasks_created(created_at),
		KEY idx_ops_tasks_state(state),
		KEY idx_ops_tasks_pick(node, kind, state, done_at)
	) COMMENT='下行操作任务' COMMENT='下行操作任务' ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS ops_meta(
		`key` VARCHAR(64) PRIMARY KEY COMMENT '元数据键（目前只有 seq）' COMMENT '元数据键（目前只有 seq）',
		value LONGTEXT NOT NULL COMMENT '元数据值' COMMENT '元数据值'
	) COMMENT='下行任务元数据（序号等）' COMMENT='下行任务元数据（序号等）' ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS ops_caps(
		node  VARCHAR(255) PRIMARY KEY COMMENT '节点名（主键）' COMMENT '节点名（主键）',
		kinds LONGTEXT NOT NULL COMMENT '该节点可执行动作（逗号分隔）' COMMENT '该节点可执行动作（逗号分隔）'
	) COMMENT='节点声明的可执行动作' COMMENT='节点声明的可执行动作' ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS schema_version(
		id      INT PRIMARY KEY COMMENT '固定为 1' COMMENT '固定为 1',
		version INT NOT NULL COMMENT '资产库结构版本' COMMENT '资产库结构版本'
	) COMMENT='资产库结构版本' COMMENT='资产库结构版本' ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

