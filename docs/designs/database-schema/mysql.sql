-- yingzhao 状态库最终表结构（mysql）

-- config_documents：整份读写的配置文档（JSON 信封；敏感内容 AES-GCM 加密）
CREATE TABLE IF NOT EXISTS config_documents (
  namespace   VARCHAR(64)  NOT NULL,  -- 配置命名空间（notify / ui / alert_rules …）
  version     BIGINT       NOT NULL DEFAULT 1,  -- 乐观锁版本号，每次写入 +1
  payload     JSON         NOT NULL,  -- JSON 信封：format / encoding / nonce / data
  updated_at  BIGINT       NOT NULL,  -- 最后写入时间（epoch ms）
  updated_by  VARCHAR(128) NOT NULL DEFAULT '',  -- 最后写入人（预留）
  PRIMARY KEY (namespace)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- nodes：节点注册信息与最近心跳
CREATE TABLE IF NOT EXISTS nodes(
  hostname     VARCHAR(255) NOT NULL,  -- 主机名（主键，Agent 上报原样）
  mode         VARCHAR(32)  NOT NULL DEFAULT '',  -- 运行模式 collect / edge / hub
  display_name VARCHAR(255) NOT NULL DEFAULT '',  -- 界面显示名（人工设置，不改 hostname）
  ip           VARCHAR(64)  NOT NULL DEFAULT '',  -- 最近上报 IP
  os           VARCHAR(128) NOT NULL DEFAULT '',  -- 操作系统
  arch         VARCHAR(64)  NOT NULL DEFAULT '',  -- CPU 架构
  group_name   VARCHAR(128) NOT NULL DEFAULT '',  -- 所属分组（Server 端管理）
  labels       JSON         NULL,  -- 标签 JSON
  version      VARCHAR(64)  NOT NULL DEFAULT '',  -- Agent 版本
  host_info    JSON         NULL,  -- 主机硬件信息 JSON（注册级字段）
  log_sources  JSON         NULL,  -- 日志来源 JSON
  status       VARCHAR(32)  NOT NULL DEFAULT '',  -- online / offline
  last_seen    BIGINT       NOT NULL DEFAULT 0,  -- 最近心跳时间（epoch ms）
  created_at   BIGINT       NOT NULL DEFAULT 0,  -- 首次注册时间
  updated_at   BIGINT       NOT NULL DEFAULT 0,  -- 最近写入时间
  PRIMARY KEY (hostname),
  KEY idx_nodes_group(group_name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- node_groups：节点分组
CREATE TABLE IF NOT EXISTS node_groups(
  name        VARCHAR(128) NOT NULL,  -- 分组名（主键）
  description VARCHAR(255) NOT NULL DEFAULT '',  -- 分组说明
  created_at  BIGINT       NOT NULL DEFAULT 0,  -- 创建时间
  PRIMARY KEY (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- asset_types：资产类型与类型模型
CREATE TABLE IF NOT EXISTS asset_types(
		`key`  VARCHAR(64) PRIMARY KEY,  -- 类型键（host / redis / mysql …）
		title   VARCHAR(255) NOT NULL DEFAULT '',  -- 显示名
		builtin TINYINT NOT NULL DEFAULT 0,  -- 是否内置类型（1/0）
		`schema`  LONGTEXT NOT NULL DEFAULT ('{}')  -- 类型模型 JSON（关注字段、属性元数据）
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- assets：资产台账主表
CREATE TABLE IF NOT EXISTS assets(
		id          BIGINT AUTO_INCREMENT PRIMARY KEY,  -- 自增主键
		type_key    VARCHAR(64) NOT NULL,  -- 资产类型键
		natural_key VARCHAR(255) NOT NULL,  -- 自然键（类型内唯一：hostname / host:port …）
		name        VARCHAR(255) NOT NULL DEFAULT '',  -- 显示名
		node        VARCHAR(255) NOT NULL DEFAULT '',  -- 归属节点
		created_at  BIGINT NOT NULL,  -- 创建时间
		updated_at  BIGINT NOT NULL,  -- 最近更新时间
		UNIQUE KEY uk_assets_type_natural(type_key, natural_key),
		KEY idx_assets_node(node),
		KEY idx_assets_type(type_key),
		CONSTRAINT fk_assets_type FOREIGN KEY(type_key) REFERENCES asset_types(`key`)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- asset_attrs：资产属性（采集 / 人工 / 派生）
CREATE TABLE IF NOT EXISTS asset_attrs(
		asset_id   BIGINT NOT NULL,  -- 资产 ID
		`key`      VARCHAR(128) NOT NULL,  -- 属性键
		value      LONGTEXT NOT NULL,  -- 属性值（文本）
		source     VARCHAR(32) NOT NULL,  -- 来源 discovery / manual / derived
		updated_at BIGINT NOT NULL,  -- 更新时间
		updated_by VARCHAR(128) NOT NULL DEFAULT '',  -- 更新人
		PRIMARY KEY(asset_id, `key`, source),
		CONSTRAINT fk_attrs_asset FOREIGN KEY(asset_id) REFERENCES assets(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- asset_links：资产关系
CREATE TABLE IF NOT EXISTS asset_links(
		id         BIGINT AUTO_INCREMENT PRIMARY KEY,  -- 自增主键
		from_id    BIGINT NOT NULL,  -- 起点资产 ID
		to_id      BIGINT NOT NULL,  -- 终点资产 ID
		kind       VARCHAR(64) NOT NULL,  -- 关系类型（runs_on / depends_on …）
		source     VARCHAR(32) NOT NULL DEFAULT 'discovery',  -- 来源 discovery / manual
		created_at BIGINT NOT NULL,  -- 创建时间
		UNIQUE KEY uk_links(from_id, to_id, kind),
		KEY idx_links_from(from_id),
		KEY idx_links_to(to_id),
		CONSTRAINT fk_links_from FOREIGN KEY(from_id) REFERENCES assets(id) ON DELETE CASCADE,
		CONSTRAINT fk_links_to FOREIGN KEY(to_id) REFERENCES assets(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- asset_link_suppressions：人工删除过的采集关系（阻止采集侧重建）
CREATE TABLE IF NOT EXISTS asset_link_suppressions(
		id         BIGINT AUTO_INCREMENT PRIMARY KEY,  -- 自增主键
		from_id    BIGINT NOT NULL,  -- 起点资产 ID
		to_id      BIGINT NOT NULL,  -- 终点资产 ID
		kind       VARCHAR(64) NOT NULL,  -- 关系类型
		created_at BIGINT NOT NULL,  -- 抑制时间
		created_by VARCHAR(128) NOT NULL DEFAULT '',  -- 操作人
		UNIQUE KEY uk_link_supp(from_id, to_id, kind),
		KEY idx_link_supp_from(from_id),
		KEY idx_link_supp_to(to_id),
		CONSTRAINT fk_supp_from FOREIGN KEY(from_id) REFERENCES assets(id) ON DELETE CASCADE,
		CONSTRAINT fk_supp_to FOREIGN KEY(to_id) REFERENCES assets(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- asset_changes：资产字段级变更记录
CREATE TABLE IF NOT EXISTS asset_changes(
		id         BIGINT AUTO_INCREMENT PRIMARY KEY,  -- 自增主键
		asset_id   BIGINT NOT NULL,  -- 资产 ID
		field      VARCHAR(128) NOT NULL,  -- 变更字段
		old_value  LONGTEXT NOT NULL,  -- 旧值
		new_value  LONGTEXT NOT NULL,  -- 新值
		source     VARCHAR(32) NOT NULL DEFAULT '',  -- 变更来源
		actor      VARCHAR(128) NOT NULL DEFAULT '',  -- 操作人 / 采集器
		kind       VARCHAR(64) NOT NULL,  -- 变更类型（create / update / delete …）
		at         BIGINT NOT NULL,  -- 变更时间（epoch ms）
		request_id VARCHAR(64) NOT NULL DEFAULT '',  -- 关联请求 ID（与审计对齐）
		KEY idx_changes_asset(asset_id, at),
		KEY idx_changes_request(request_id),
		CONSTRAINT fk_changes_asset FOREIGN KEY(asset_id) REFERENCES assets(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- snapshots：配置快照头
CREATE TABLE IF NOT EXISTS snapshots(
		id       BIGINT AUTO_INCREMENT PRIMARY KEY,  -- 自增主键
		asset_id BIGINT NOT NULL,  -- 资产 ID
		taken_at BIGINT NOT NULL,  -- 快照时间
		KEY idx_snapshots_asset(asset_id, taken_at),
		CONSTRAINT fk_snapshots_asset FOREIGN KEY(asset_id) REFERENCES assets(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- snapshot_fields：配置快照字段
CREATE TABLE IF NOT EXISTS snapshot_fields(
		snapshot_id BIGINT NOT NULL,  -- 快照 ID
		`key`         VARCHAR(128) NOT NULL,  -- 字段名
		value       LONGTEXT NOT NULL,  -- 字段值
		PRIMARY KEY(snapshot_id, `key`),
		CONSTRAINT fk_sfields_snapshot FOREIGN KEY(snapshot_id) REFERENCES snapshots(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- asset_seen：资产最近一次采集上报时间
CREATE TABLE IF NOT EXISTS asset_seen(
		asset_id     BIGINT PRIMARY KEY,  -- 资产 ID（主键）
		last_seen_at BIGINT NOT NULL,  -- 最近一次采集上报时间
		CONSTRAINT fk_seen_asset FOREIGN KEY(asset_id) REFERENCES assets(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- inspect_runs：巡检批次
CREATE TABLE IF NOT EXISTS inspect_runs(
		id         BIGINT AUTO_INCREMENT PRIMARY KEY,  -- 自增主键
		scope      VARCHAR(255) NOT NULL DEFAULT '',  -- 巡检范围
		actor      VARCHAR(128) NOT NULL DEFAULT '',  -- 触发人 / 调度器
		started_at BIGINT NOT NULL,  -- 开始时间
		assets     INT NOT NULL DEFAULT 0,  -- 巡检资产数
		baselined  INT NOT NULL DEFAULT 0,  -- 命中标杆数
		findings   INT NOT NULL DEFAULT 0,  -- 差异数
		truncated  INT NOT NULL DEFAULT 0,  -- 是否因上限截断（1/0）
		KEY idx_inspect_runs_at(started_at)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- inspect_run_manifest：巡检批次清单标记
CREATE TABLE IF NOT EXISTS inspect_run_manifest(
		run_id BIGINT PRIMARY KEY,  -- 巡检批次 ID（主键）
		CONSTRAINT fk_manifest_run FOREIGN KEY(run_id) REFERENCES inspect_runs(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- inspect_run_members：巡检批次包含的资产
CREATE TABLE IF NOT EXISTS inspect_run_members(
		run_id      BIGINT NOT NULL,  -- 巡检批次 ID
		asset_id    BIGINT NOT NULL,  -- 资产 ID
		node_at_run VARCHAR(255) NOT NULL DEFAULT '',  -- 巡检时的归属节点
		baselined   INT NOT NULL DEFAULT 0,  -- 该资产是否命中标杆（1/0）
		findings    INT NOT NULL DEFAULT 0,  -- 该资产的差异数
		PRIMARY KEY(run_id, asset_id),
		KEY idx_inspect_members_asset(asset_id, run_id),
		CONSTRAINT fk_members_run FOREIGN KEY(run_id) REFERENCES inspect_runs(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- inspect_findings：巡检差异项
CREATE TABLE IF NOT EXISTS inspect_findings(
		id         BIGINT AUTO_INCREMENT PRIMARY KEY,  -- 自增主键
		run_id     BIGINT NOT NULL,  -- 巡检批次 ID
		asset_id   BIGINT NOT NULL,  -- 资产 ID
		asset_type VARCHAR(64) NOT NULL DEFAULT '',  -- 资产类型键（快照）
		asset_key  VARCHAR(255) NOT NULL DEFAULT '',  -- 资产自然键（快照）
		asset_name VARCHAR(255) NOT NULL DEFAULT '',  -- 资产显示名（快照）
		node       VARCHAR(255) NOT NULL DEFAULT '',  -- 归属节点（快照）
		field      VARCHAR(128) NOT NULL DEFAULT '',  -- 差异字段
		kind       VARCHAR(64) NOT NULL,  -- 差异类型（missing / extra / changed …）
		level      VARCHAR(32) NOT NULL DEFAULT '',  -- 级别（warning / critical …）
		expected   LONGTEXT NOT NULL,  -- 期望值
		actual     LONGTEXT NOT NULL,  -- 实际值
		at         BIGINT NOT NULL,  -- 产生时间
		KEY idx_inspect_findings_run(run_id, level, kind),
		CONSTRAINT fk_findings_run FOREIGN KEY(run_id) REFERENCES inspect_runs(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- inspect_finding_baselines：差异项与标杆来源的关联
CREATE TABLE IF NOT EXISTS inspect_finding_baselines(
		finding_id BIGINT PRIMARY KEY,  -- 差异项 ID（主键）
		asset_id   BIGINT NOT NULL,  -- 标杆来源资产 ID
		CONSTRAINT fk_fbase_finding FOREIGN KEY(finding_id) REFERENCES inspect_findings(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- inspect_baselines：资产类型的巡检标杆
CREATE TABLE IF NOT EXISTS inspect_baselines(
		type_key    VARCHAR(64) PRIMARY KEY,  -- 资产类型键（主键）
		asset_id    BIGINT NOT NULL,  -- 标杆资产 ID
		asset_key   VARCHAR(255) NOT NULL DEFAULT '',  -- 标杆资产自然键（快照）
		snapshot_id BIGINT NOT NULL,  -- 标杆快照 ID
		set_by      VARCHAR(128) NOT NULL DEFAULT '',  -- 设置人
		set_at      BIGINT NOT NULL  -- 设置时间
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- asset_ignored：人工忽略的资产
CREATE TABLE IF NOT EXISTS asset_ignored(
		asset_id   BIGINT PRIMARY KEY,  -- 资产 ID（主键）
		reason     LONGTEXT NOT NULL,  -- 忽略原因
		ignored_by VARCHAR(128) NOT NULL DEFAULT '',  -- 操作人
		ignored_at BIGINT NOT NULL,  -- 忽略时间
		CONSTRAINT fk_ignored_asset FOREIGN KEY(asset_id) REFERENCES assets(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- asset_labels：资产标签
CREATE TABLE IF NOT EXISTS asset_labels(
		asset_id   BIGINT NOT NULL,  -- 资产 ID
		`key`      VARCHAR(128) NOT NULL,  -- 标签键
		value      VARCHAR(512) NOT NULL DEFAULT '',  -- 标签值
		updated_at BIGINT NOT NULL,  -- 更新时间
		updated_by VARCHAR(128) NOT NULL DEFAULT '',  -- 更新人
		PRIMARY KEY(asset_id, `key`),
		KEY idx_asset_labels_kv(`key`, value),
		CONSTRAINT fk_labels_asset FOREIGN KEY(asset_id) REFERENCES assets(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- audit_events：审计事件（seq + prev_hash + hash 组成哈希链）
CREATE TABLE IF NOT EXISTS audit_events(
		id         BIGINT AUTO_INCREMENT PRIMARY KEY,  -- 自增主键
		time_ms    BIGINT NOT NULL,  -- 事件时间（epoch ms）
		user_name  VARCHAR(191) NOT NULL DEFAULT '',  -- 操作人
		method     VARCHAR(32) NOT NULL DEFAULT '',  -- HTTP 方法
		path       VARCHAR(1024) NOT NULL DEFAULT '',  -- 请求路径
		status     INT NOT NULL DEFAULT 0,  -- HTTP 状态码
		remote_ip  VARCHAR(64) NOT NULL DEFAULT '',  -- 来源 IP
		source_loc VARCHAR(191) NOT NULL DEFAULT '',  -- IP 属地
		succeeded  TINYINT NOT NULL DEFAULT 0,  -- 是否成功（1/0）
		category   VARCHAR(128) NOT NULL DEFAULT '',  -- 分类（asset / ops / security …）
		action     VARCHAR(191) NOT NULL DEFAULT '',  -- 动作标识
		detail     LONGTEXT NOT NULL,  -- 详情
		request_id VARCHAR(64) NOT NULL DEFAULT '',  -- 请求关联 ID
		seq        BIGINT NOT NULL DEFAULT 0,  -- 链上序号（0 = 启用链之前的历史行）
		prev_hash  VARCHAR(64) NOT NULL DEFAULT '',  -- 前一条哈希
		hash       VARCHAR(64) NOT NULL DEFAULT '',  -- 本条哈希
		key_fp     VARCHAR(64) NOT NULL DEFAULT '',  -- 链密钥指纹
		KEY idx_audit_events_time(time_ms),
		KEY idx_audit_events_user(user_name, time_ms),
		KEY idx_audit_events_request(request_id),
		KEY idx_audit_events_seq(seq)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- audit_chain：审计链头（单行，FOR UPDATE 串行追加）
CREATE TABLE IF NOT EXISTS audit_chain(
		id              INT PRIMARY KEY,  -- 固定为 1（单行）
		seq             BIGINT NOT NULL DEFAULT 0,  -- 当前链头序号
		hash            VARCHAR(64) NOT NULL DEFAULT '',  -- 当前链头哈希
		key_fingerprint VARCHAR(64) NOT NULL DEFAULT '',  -- 链密钥指纹
		updated_ms      BIGINT NOT NULL DEFAULT 0,  -- 更新时间
		CONSTRAINT chk_audit_chain_id CHECK(id = 1)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- audit_pruned：合法裁剪区间与 HMAC 锚点
CREATE TABLE IF NOT EXISTS audit_pruned(
		from_seq  BIGINT NOT NULL,  -- 裁剪区间起始 seq
		to_seq    BIGINT NOT NULL,  -- 裁剪区间结束 seq
		head_hash VARCHAR(64) NOT NULL DEFAULT '',  -- 区间末条哈希（校验锚点）
		`rows` BIGINT NOT NULL DEFAULT 0,  -- 被裁行数
		pruned_ms BIGINT NOT NULL DEFAULT 0,  -- 裁剪时间
		mac       VARCHAR(64) NOT NULL DEFAULT '',  -- 裁剪记录的 HMAC
		KEY idx_audit_pruned_range(from_seq, to_seq)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- alert_acks：告警处置（认领 / 指派 / 关闭 / 评论）
CREATE TABLE IF NOT EXISTS alert_acks(
		ack_key       VARCHAR(512) PRIMARY KEY,  -- 处置键 rule|host|instance|startsAt
		rule          VARCHAR(512) NOT NULL DEFAULT '',  -- 规则名
		host          VARCHAR(255) NOT NULL DEFAULT '',  -- 主机
		instance      VARCHAR(1024) NOT NULL DEFAULT '',  -- 实例
		starts_at     BIGINT NOT NULL DEFAULT 0,  -- 告警触发时间
		status        VARCHAR(32) NOT NULL DEFAULT 'pending',  -- pending / ack / closed
		user_name     VARCHAR(128) NOT NULL DEFAULT '',  -- 最近操作人
		assignee      VARCHAR(128) NOT NULL DEFAULT '',  -- 当前处理人
		time_ms       BIGINT NOT NULL DEFAULT 0,  -- 最近操作时间
		ack_time_ms   BIGINT NOT NULL DEFAULT 0,  -- 认领时间
		close_time_ms BIGINT NOT NULL DEFAULT 0,  -- 关闭时间
		close_reason  LONGTEXT NOT NULL,  -- 关闭原因
		comments      LONGTEXT NOT NULL,  -- 评论 JSON
		KEY idx_alert_acks_time(time_ms),
		KEY idx_alert_acks_status(status, time_ms)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ops_tasks：下行操作任务
CREATE TABLE IF NOT EXISTS ops_tasks(
		id         VARCHAR(64) PRIMARY KEY,  -- 任务 ID（ops-N / ob-N）
		batch_id   VARCHAR(64) NOT NULL DEFAULT '',  -- 批次 ID
		node       VARCHAR(255) NOT NULL DEFAULT '',  -- 目标节点
		kind       VARCHAR(64) NOT NULL DEFAULT '',  -- 动作标识
		state      VARCHAR(32) NOT NULL DEFAULT '',  -- queued / running / succeeded / failed / canceled / expired
		created_at BIGINT NOT NULL DEFAULT 0,  -- 创建时间
		done_at    BIGINT NOT NULL DEFAULT 0,  -- 结束时间
		task_json  LONGTEXT NOT NULL,  -- 完整任务 JSON（参数 / 结果 / 操作人）
		KEY idx_ops_tasks_created(created_at),
		KEY idx_ops_tasks_state(state),
		KEY idx_ops_tasks_pick(node, kind, state, done_at)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ops_meta：下行任务元数据（序号等）
CREATE TABLE IF NOT EXISTS ops_meta(
		`key` VARCHAR(64) PRIMARY KEY,  -- 元数据键（目前只有 seq）
		value LONGTEXT NOT NULL  -- 元数据值
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ops_caps：节点声明的可执行动作
CREATE TABLE IF NOT EXISTS ops_caps(
		node  VARCHAR(255) PRIMARY KEY,  -- 节点名（主键）
		kinds LONGTEXT NOT NULL  -- 该节点可执行动作（逗号分隔）
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- schema_version：资产库结构版本
CREATE TABLE IF NOT EXISTS schema_version(
		id      INT PRIMARY KEY,  -- 固定为 1
		version INT NOT NULL  -- 资产库结构版本
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

