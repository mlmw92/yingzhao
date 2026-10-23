-- yingzhao 状态库最终表结构（postgres）

CREATE TABLE IF NOT EXISTS config_documents (
  namespace   VARCHAR(64)  NOT NULL,
  version     BIGINT       NOT NULL DEFAULT 1,
  payload     TEXT         NOT NULL,
  updated_at  BIGINT       NOT NULL,
  updated_by  VARCHAR(128) NOT NULL DEFAULT '',
  PRIMARY KEY (namespace)
);
COMMENT ON TABLE config_documents IS '整份读写的配置文档（JSON 信封；敏感内容 AES-GCM 加密）';
COMMENT ON COLUMN config_documents.namespace IS '配置命名空间（notify / ui / alert_rules …）';
COMMENT ON COLUMN config_documents.version IS '乐观锁版本号，每次写入 +1';
COMMENT ON COLUMN config_documents.payload IS 'JSON 信封：format / encoding / nonce / data';
COMMENT ON COLUMN config_documents.updated_at IS '最后写入时间（epoch ms）';
COMMENT ON COLUMN config_documents.updated_by IS '最后写入人（预留）';

CREATE TABLE IF NOT EXISTS nodes(
  hostname     VARCHAR(255) NOT NULL,
  mode         VARCHAR(32)  NOT NULL DEFAULT '',
  display_name VARCHAR(255) NOT NULL DEFAULT '',
  ip           VARCHAR(64)  NOT NULL DEFAULT '',
  os           VARCHAR(128) NOT NULL DEFAULT '',
  arch         VARCHAR(64)  NOT NULL DEFAULT '',
  group_name   VARCHAR(128) NOT NULL DEFAULT '',
  labels       TEXT         NULL,
  version      VARCHAR(64)  NOT NULL DEFAULT '',
  host_info    TEXT         NULL,
  log_sources  TEXT         NULL,
  status       VARCHAR(32)  NOT NULL DEFAULT '',
  last_seen    BIGINT       NOT NULL DEFAULT 0,
  created_at   BIGINT       NOT NULL DEFAULT 0,
  updated_at   BIGINT       NOT NULL DEFAULT 0,
  PRIMARY KEY (hostname)
);
CREATE INDEX IF NOT EXISTS idx_nodes_group ON nodes(group_name);
COMMENT ON TABLE nodes IS '节点注册信息与最近心跳';
COMMENT ON COLUMN nodes.hostname IS '主机名（主键，Agent 上报原样）';
COMMENT ON COLUMN nodes.mode IS '运行模式 collect / edge / hub';
COMMENT ON COLUMN nodes.display_name IS '界面显示名（人工设置，不改 hostname）';
COMMENT ON COLUMN nodes.ip IS '最近上报 IP';
COMMENT ON COLUMN nodes.os IS '操作系统';
COMMENT ON COLUMN nodes.arch IS 'CPU 架构';
COMMENT ON COLUMN nodes.group_name IS '所属分组（Server 端管理）';
COMMENT ON COLUMN nodes.labels IS '标签 JSON';
COMMENT ON COLUMN nodes.version IS 'Agent 版本';
COMMENT ON COLUMN nodes.host_info IS '主机硬件信息 JSON（注册级字段）';
COMMENT ON COLUMN nodes.log_sources IS '日志来源 JSON';
COMMENT ON COLUMN nodes.status IS 'online / offline';
COMMENT ON COLUMN nodes.last_seen IS '最近心跳时间（epoch ms）';
COMMENT ON COLUMN nodes.created_at IS '首次注册时间';
COMMENT ON COLUMN nodes.updated_at IS '最近写入时间';

CREATE TABLE IF NOT EXISTS node_groups(
  name        VARCHAR(128) NOT NULL,
  description VARCHAR(255) NOT NULL DEFAULT '',
  created_at  BIGINT       NOT NULL DEFAULT 0,
  PRIMARY KEY (name)
);
COMMENT ON TABLE node_groups IS '节点分组';
COMMENT ON COLUMN node_groups.name IS '分组名（主键）';
COMMENT ON COLUMN node_groups.description IS '分组说明';
COMMENT ON COLUMN node_groups.created_at IS '创建时间';

CREATE TABLE IF NOT EXISTS asset_types(
		"key"   VARCHAR(64) PRIMARY KEY,
		title   VARCHAR(255) NOT NULL DEFAULT '',
		builtin SMALLINT NOT NULL DEFAULT 0,
		"schema" TEXT NOT NULL DEFAULT '{}'
	);
COMMENT ON TABLE asset_types IS '资产类型与类型模型';
COMMENT ON COLUMN asset_types."key" IS '类型键（host / redis / mysql …）';
COMMENT ON COLUMN asset_types.title IS '显示名';
COMMENT ON COLUMN asset_types.builtin IS '是否内置类型（1/0）';
COMMENT ON COLUMN asset_types."schema" IS '类型模型 JSON（关注字段、属性元数据）';

CREATE TABLE IF NOT EXISTS assets(
		id          BIGSERIAL PRIMARY KEY,
		type_key    VARCHAR(64) NOT NULL,
		natural_key VARCHAR(255) NOT NULL,
		name        VARCHAR(255) NOT NULL DEFAULT '',
		node        VARCHAR(255) NOT NULL DEFAULT '',
		created_at  BIGINT NOT NULL,
		updated_at  BIGINT NOT NULL,
		CONSTRAINT uk_assets_type_natural UNIQUE(type_key, natural_key),
		CONSTRAINT fk_assets_type FOREIGN KEY(type_key) REFERENCES asset_types("key")
	);
COMMENT ON TABLE assets IS '资产台账主表';
COMMENT ON COLUMN assets.id IS '自增主键';
COMMENT ON COLUMN assets.type_key IS '资产类型键';
COMMENT ON COLUMN assets.natural_key IS '自然键（类型内唯一：hostname / host:port …）';
COMMENT ON COLUMN assets.name IS '显示名';
COMMENT ON COLUMN assets.node IS '归属节点';
COMMENT ON COLUMN assets.created_at IS '创建时间';
COMMENT ON COLUMN assets.updated_at IS '最近更新时间';

CREATE TABLE IF NOT EXISTS asset_attrs(
		asset_id   BIGINT NOT NULL,
		"key"      VARCHAR(128) NOT NULL,
		value      TEXT NOT NULL,
		source     VARCHAR(32) NOT NULL,
		updated_at BIGINT NOT NULL,
		updated_by VARCHAR(128) NOT NULL DEFAULT '',
		PRIMARY KEY(asset_id, "key", source),
		CONSTRAINT fk_attrs_asset FOREIGN KEY(asset_id) REFERENCES assets(id) ON DELETE CASCADE
	);
COMMENT ON TABLE asset_attrs IS '资产属性（采集 / 人工 / 派生）';
COMMENT ON COLUMN asset_attrs.asset_id IS '资产 ID';
COMMENT ON COLUMN asset_attrs."key" IS '属性键';
COMMENT ON COLUMN asset_attrs.value IS '属性值（文本）';
COMMENT ON COLUMN asset_attrs.source IS '来源 discovery / manual / derived';
COMMENT ON COLUMN asset_attrs.updated_at IS '更新时间';
COMMENT ON COLUMN asset_attrs.updated_by IS '更新人';

CREATE TABLE IF NOT EXISTS asset_links(
		id         BIGSERIAL PRIMARY KEY,
		from_id    BIGINT NOT NULL,
		to_id      BIGINT NOT NULL,
		kind       VARCHAR(64) NOT NULL,
		source     VARCHAR(32) NOT NULL DEFAULT 'discovery',
		created_at BIGINT NOT NULL,
		CONSTRAINT uk_links UNIQUE(from_id, to_id, kind),
		CONSTRAINT fk_links_from FOREIGN KEY(from_id) REFERENCES assets(id) ON DELETE CASCADE,
		CONSTRAINT fk_links_to FOREIGN KEY(to_id) REFERENCES assets(id) ON DELETE CASCADE
	);
COMMENT ON TABLE asset_links IS '资产关系';
COMMENT ON COLUMN asset_links.id IS '自增主键';
COMMENT ON COLUMN asset_links.from_id IS '起点资产 ID';
COMMENT ON COLUMN asset_links.to_id IS '终点资产 ID';
COMMENT ON COLUMN asset_links.kind IS '关系类型（runs_on / depends_on …）';
COMMENT ON COLUMN asset_links.source IS '来源 discovery / manual';
COMMENT ON COLUMN asset_links.created_at IS '创建时间';

CREATE TABLE IF NOT EXISTS asset_link_suppressions(
		id         BIGSERIAL PRIMARY KEY,
		from_id    BIGINT NOT NULL,
		to_id      BIGINT NOT NULL,
		kind       VARCHAR(64) NOT NULL,
		created_at BIGINT NOT NULL,
		created_by VARCHAR(128) NOT NULL DEFAULT '',
		CONSTRAINT uk_link_supp UNIQUE(from_id, to_id, kind),
		CONSTRAINT fk_supp_from FOREIGN KEY(from_id) REFERENCES assets(id) ON DELETE CASCADE,
		CONSTRAINT fk_supp_to FOREIGN KEY(to_id) REFERENCES assets(id) ON DELETE CASCADE
	);
COMMENT ON TABLE asset_link_suppressions IS '人工删除过的采集关系（阻止采集侧重建）';
COMMENT ON COLUMN asset_link_suppressions.id IS '自增主键';
COMMENT ON COLUMN asset_link_suppressions.from_id IS '起点资产 ID';
COMMENT ON COLUMN asset_link_suppressions.to_id IS '终点资产 ID';
COMMENT ON COLUMN asset_link_suppressions.kind IS '关系类型';
COMMENT ON COLUMN asset_link_suppressions.created_at IS '抑制时间';
COMMENT ON COLUMN asset_link_suppressions.created_by IS '操作人';

CREATE TABLE IF NOT EXISTS asset_changes(
		id         BIGSERIAL PRIMARY KEY,
		asset_id   BIGINT NOT NULL,
		field      VARCHAR(128) NOT NULL,
		old_value  TEXT NOT NULL,
		new_value  TEXT NOT NULL,
		source     VARCHAR(32) NOT NULL DEFAULT '',
		actor      VARCHAR(128) NOT NULL DEFAULT '',
		kind       VARCHAR(64) NOT NULL,
		at         BIGINT NOT NULL,
		request_id VARCHAR(64) NOT NULL DEFAULT '',
		CONSTRAINT fk_changes_asset FOREIGN KEY(asset_id) REFERENCES assets(id) ON DELETE CASCADE
	);
COMMENT ON TABLE asset_changes IS '资产字段级变更记录';
COMMENT ON COLUMN asset_changes.id IS '自增主键';
COMMENT ON COLUMN asset_changes.asset_id IS '资产 ID';
COMMENT ON COLUMN asset_changes.field IS '变更字段';
COMMENT ON COLUMN asset_changes.old_value IS '旧值';
COMMENT ON COLUMN asset_changes.new_value IS '新值';
COMMENT ON COLUMN asset_changes.source IS '变更来源';
COMMENT ON COLUMN asset_changes.actor IS '操作人 / 采集器';
COMMENT ON COLUMN asset_changes.kind IS '变更类型（create / update / delete …）';
COMMENT ON COLUMN asset_changes.at IS '变更时间（epoch ms）';
COMMENT ON COLUMN asset_changes.request_id IS '关联请求 ID（与审计对齐）';

CREATE TABLE IF NOT EXISTS snapshots(
		id       BIGSERIAL PRIMARY KEY,
		asset_id BIGINT NOT NULL,
		taken_at BIGINT NOT NULL,
		CONSTRAINT fk_snapshots_asset FOREIGN KEY(asset_id) REFERENCES assets(id) ON DELETE CASCADE
	);
COMMENT ON TABLE snapshots IS '配置快照头';
COMMENT ON COLUMN snapshots.id IS '自增主键';
COMMENT ON COLUMN snapshots.asset_id IS '资产 ID';
COMMENT ON COLUMN snapshots.taken_at IS '快照时间';

CREATE TABLE IF NOT EXISTS snapshot_fields(
		snapshot_id BIGINT NOT NULL,
		"key"       VARCHAR(128) NOT NULL,
		value       TEXT NOT NULL,
		PRIMARY KEY(snapshot_id, "key"),
		CONSTRAINT fk_sfields_snapshot FOREIGN KEY(snapshot_id) REFERENCES snapshots(id) ON DELETE CASCADE
	);
COMMENT ON TABLE snapshot_fields IS '配置快照字段';
COMMENT ON COLUMN snapshot_fields.snapshot_id IS '快照 ID';
COMMENT ON COLUMN snapshot_fields."key" IS '字段名';
COMMENT ON COLUMN snapshot_fields.value IS '字段值';

CREATE TABLE IF NOT EXISTS asset_seen(
		asset_id     BIGINT PRIMARY KEY,
		last_seen_at BIGINT NOT NULL,
		CONSTRAINT fk_seen_asset FOREIGN KEY(asset_id) REFERENCES assets(id) ON DELETE CASCADE
	);
COMMENT ON TABLE asset_seen IS '资产最近一次采集上报时间';
COMMENT ON COLUMN asset_seen.asset_id IS '资产 ID（主键）';
COMMENT ON COLUMN asset_seen.last_seen_at IS '最近一次采集上报时间';

CREATE TABLE IF NOT EXISTS inspect_runs(
		id         BIGSERIAL PRIMARY KEY,
		scope      VARCHAR(255) NOT NULL DEFAULT '',
		actor      VARCHAR(128) NOT NULL DEFAULT '',
		started_at BIGINT NOT NULL,
		assets     INT NOT NULL DEFAULT 0,
		baselined  INT NOT NULL DEFAULT 0,
		findings   INT NOT NULL DEFAULT 0,
		truncated  INT NOT NULL DEFAULT 0
	);
COMMENT ON TABLE inspect_runs IS '巡检批次';
COMMENT ON COLUMN inspect_runs.id IS '自增主键';
COMMENT ON COLUMN inspect_runs.scope IS '巡检范围';
COMMENT ON COLUMN inspect_runs.actor IS '触发人 / 调度器';
COMMENT ON COLUMN inspect_runs.started_at IS '开始时间';
COMMENT ON COLUMN inspect_runs.assets IS '巡检资产数';
COMMENT ON COLUMN inspect_runs.baselined IS '命中标杆数';
COMMENT ON COLUMN inspect_runs.findings IS '差异数';
COMMENT ON COLUMN inspect_runs.truncated IS '是否因上限截断（1/0）';

CREATE TABLE IF NOT EXISTS inspect_run_manifest(
		run_id BIGINT PRIMARY KEY,
		CONSTRAINT fk_manifest_run FOREIGN KEY(run_id) REFERENCES inspect_runs(id) ON DELETE CASCADE
	);
COMMENT ON TABLE inspect_run_manifest IS '巡检批次清单标记';
COMMENT ON COLUMN inspect_run_manifest.run_id IS '巡检批次 ID（主键）';

CREATE TABLE IF NOT EXISTS inspect_run_members(
		run_id      BIGINT NOT NULL,
		asset_id    BIGINT NOT NULL,
		node_at_run VARCHAR(255) NOT NULL DEFAULT '',
		baselined   INT NOT NULL DEFAULT 0,
		findings    INT NOT NULL DEFAULT 0,
		PRIMARY KEY(run_id, asset_id),
		CONSTRAINT fk_members_run FOREIGN KEY(run_id) REFERENCES inspect_runs(id) ON DELETE CASCADE
	);
COMMENT ON TABLE inspect_run_members IS '巡检批次包含的资产';
COMMENT ON COLUMN inspect_run_members.run_id IS '巡检批次 ID';
COMMENT ON COLUMN inspect_run_members.asset_id IS '资产 ID';
COMMENT ON COLUMN inspect_run_members.node_at_run IS '巡检时的归属节点';
COMMENT ON COLUMN inspect_run_members.baselined IS '该资产是否命中标杆（1/0）';
COMMENT ON COLUMN inspect_run_members.findings IS '该资产的差异数';

CREATE TABLE IF NOT EXISTS inspect_findings(
		id         BIGSERIAL PRIMARY KEY,
		run_id     BIGINT NOT NULL,
		asset_id   BIGINT NOT NULL,
		asset_type VARCHAR(64) NOT NULL DEFAULT '',
		asset_key  VARCHAR(255) NOT NULL DEFAULT '',
		asset_name VARCHAR(255) NOT NULL DEFAULT '',
		node       VARCHAR(255) NOT NULL DEFAULT '',
		field      VARCHAR(128) NOT NULL DEFAULT '',
		kind       VARCHAR(64) NOT NULL,
		"level"    VARCHAR(32) NOT NULL DEFAULT '',
		expected   TEXT NOT NULL,
		actual     TEXT NOT NULL,
		at         BIGINT NOT NULL,
		CONSTRAINT fk_findings_run FOREIGN KEY(run_id) REFERENCES inspect_runs(id) ON DELETE CASCADE
	);
COMMENT ON TABLE inspect_findings IS '巡检差异项';
COMMENT ON COLUMN inspect_findings.id IS '自增主键';
COMMENT ON COLUMN inspect_findings.run_id IS '巡检批次 ID';
COMMENT ON COLUMN inspect_findings.asset_id IS '资产 ID';
COMMENT ON COLUMN inspect_findings.asset_type IS '资产类型键（快照）';
COMMENT ON COLUMN inspect_findings.asset_key IS '资产自然键（快照）';
COMMENT ON COLUMN inspect_findings.asset_name IS '资产显示名（快照）';
COMMENT ON COLUMN inspect_findings.node IS '归属节点（快照）';
COMMENT ON COLUMN inspect_findings.field IS '差异字段';
COMMENT ON COLUMN inspect_findings.kind IS '差异类型（missing / extra / changed …）';
COMMENT ON COLUMN inspect_findings.level IS '级别（warning / critical …）';
COMMENT ON COLUMN inspect_findings.expected IS '期望值';
COMMENT ON COLUMN inspect_findings.actual IS '实际值';
COMMENT ON COLUMN inspect_findings.at IS '产生时间';

CREATE TABLE IF NOT EXISTS inspect_finding_baselines(
		finding_id BIGINT PRIMARY KEY,
		asset_id   BIGINT NOT NULL,
		CONSTRAINT fk_fbase_finding FOREIGN KEY(finding_id) REFERENCES inspect_findings(id) ON DELETE CASCADE
	);
COMMENT ON TABLE inspect_finding_baselines IS '差异项与标杆来源的关联';
COMMENT ON COLUMN inspect_finding_baselines.finding_id IS '差异项 ID（主键）';
COMMENT ON COLUMN inspect_finding_baselines.asset_id IS '标杆来源资产 ID';

CREATE TABLE IF NOT EXISTS inspect_baselines(
		type_key    VARCHAR(64) PRIMARY KEY,
		asset_id    BIGINT NOT NULL,
		asset_key   VARCHAR(255) NOT NULL DEFAULT '',
		snapshot_id BIGINT NOT NULL,
		set_by      VARCHAR(128) NOT NULL DEFAULT '',
		set_at      BIGINT NOT NULL
	);
COMMENT ON TABLE inspect_baselines IS '资产类型的巡检标杆';
COMMENT ON COLUMN inspect_baselines.type_key IS '资产类型键（主键）';
COMMENT ON COLUMN inspect_baselines.asset_id IS '标杆资产 ID';
COMMENT ON COLUMN inspect_baselines.asset_key IS '标杆资产自然键（快照）';
COMMENT ON COLUMN inspect_baselines.snapshot_id IS '标杆快照 ID';
COMMENT ON COLUMN inspect_baselines.set_by IS '设置人';
COMMENT ON COLUMN inspect_baselines.set_at IS '设置时间';

CREATE TABLE IF NOT EXISTS asset_ignored(
		asset_id   BIGINT PRIMARY KEY,
		reason     TEXT NOT NULL,
		ignored_by VARCHAR(128) NOT NULL DEFAULT '',
		ignored_at BIGINT NOT NULL,
		CONSTRAINT fk_ignored_asset FOREIGN KEY(asset_id) REFERENCES assets(id) ON DELETE CASCADE
	);
COMMENT ON TABLE asset_ignored IS '人工忽略的资产';
COMMENT ON COLUMN asset_ignored.asset_id IS '资产 ID（主键）';
COMMENT ON COLUMN asset_ignored.reason IS '忽略原因';
COMMENT ON COLUMN asset_ignored.ignored_by IS '操作人';
COMMENT ON COLUMN asset_ignored.ignored_at IS '忽略时间';

CREATE TABLE IF NOT EXISTS asset_labels(
		asset_id   BIGINT NOT NULL,
		"key"      VARCHAR(128) NOT NULL,
		value      VARCHAR(512) NOT NULL DEFAULT '',
		updated_at BIGINT NOT NULL,
		updated_by VARCHAR(128) NOT NULL DEFAULT '',
		PRIMARY KEY(asset_id, "key"),
		CONSTRAINT fk_labels_asset FOREIGN KEY(asset_id) REFERENCES assets(id) ON DELETE CASCADE
	);
COMMENT ON TABLE asset_labels IS '资产标签';
COMMENT ON COLUMN asset_labels.asset_id IS '资产 ID';
COMMENT ON COLUMN asset_labels."key" IS '标签键';
COMMENT ON COLUMN asset_labels.value IS '标签值';
COMMENT ON COLUMN asset_labels.updated_at IS '更新时间';
COMMENT ON COLUMN asset_labels.updated_by IS '更新人';

CREATE TABLE IF NOT EXISTS audit_events(
		id         BIGSERIAL PRIMARY KEY,
		time_ms    BIGINT NOT NULL,
		user_name  VARCHAR(191) NOT NULL DEFAULT '',
		method     VARCHAR(32) NOT NULL DEFAULT '',
		path       VARCHAR(1024) NOT NULL DEFAULT '',
		status     INT NOT NULL DEFAULT 0,
		remote_ip  VARCHAR(64) NOT NULL DEFAULT '',
		source_loc VARCHAR(191) NOT NULL DEFAULT '',
		succeeded  SMALLINT NOT NULL DEFAULT 0,
		category   VARCHAR(128) NOT NULL DEFAULT '',
		action     VARCHAR(191) NOT NULL DEFAULT '',
		detail     TEXT NOT NULL,
		request_id VARCHAR(64) NOT NULL DEFAULT '',
		seq        BIGINT NOT NULL DEFAULT 0,
		prev_hash  VARCHAR(64) NOT NULL DEFAULT '',
		hash       VARCHAR(64) NOT NULL DEFAULT '',
		key_fp     VARCHAR(64) NOT NULL DEFAULT ''
	);
COMMENT ON TABLE audit_events IS '审计事件（seq + prev_hash + hash 组成哈希链）';
COMMENT ON COLUMN audit_events.id IS '自增主键';
COMMENT ON COLUMN audit_events.time_ms IS '事件时间（epoch ms）';
COMMENT ON COLUMN audit_events.user_name IS '操作人';
COMMENT ON COLUMN audit_events.method IS 'HTTP 方法';
COMMENT ON COLUMN audit_events.path IS '请求路径';
COMMENT ON COLUMN audit_events.status IS 'HTTP 状态码';
COMMENT ON COLUMN audit_events.remote_ip IS '来源 IP';
COMMENT ON COLUMN audit_events.source_loc IS 'IP 属地';
COMMENT ON COLUMN audit_events.succeeded IS '是否成功（1/0）';
COMMENT ON COLUMN audit_events.category IS '分类（asset / ops / security …）';
COMMENT ON COLUMN audit_events.action IS '动作标识';
COMMENT ON COLUMN audit_events.detail IS '详情';
COMMENT ON COLUMN audit_events.request_id IS '请求关联 ID';
COMMENT ON COLUMN audit_events.seq IS '链上序号（0 = 启用链之前的历史行）';
COMMENT ON COLUMN audit_events.prev_hash IS '前一条哈希';
COMMENT ON COLUMN audit_events.hash IS '本条哈希';
COMMENT ON COLUMN audit_events.key_fp IS '链密钥指纹';

CREATE TABLE IF NOT EXISTS audit_chain(
		id              INT PRIMARY KEY,
		seq             BIGINT NOT NULL DEFAULT 0,
		hash            VARCHAR(64) NOT NULL DEFAULT '',
		key_fingerprint VARCHAR(64) NOT NULL DEFAULT '',
		updated_ms      BIGINT NOT NULL DEFAULT 0,
		CONSTRAINT chk_audit_chain_id CHECK(id = 1)
	);
COMMENT ON TABLE audit_chain IS '审计链头（单行，FOR UPDATE 串行追加）';
COMMENT ON COLUMN audit_chain.id IS '固定为 1（单行）';
COMMENT ON COLUMN audit_chain.seq IS '当前链头序号';
COMMENT ON COLUMN audit_chain.hash IS '当前链头哈希';
COMMENT ON COLUMN audit_chain.key_fingerprint IS '链密钥指纹';
COMMENT ON COLUMN audit_chain.updated_ms IS '更新时间';

CREATE TABLE IF NOT EXISTS audit_pruned(
		from_seq  BIGINT NOT NULL,
		to_seq    BIGINT NOT NULL,
		head_hash VARCHAR(64) NOT NULL DEFAULT '',
		"rows"    BIGINT NOT NULL DEFAULT 0,
		pruned_ms BIGINT NOT NULL DEFAULT 0,
		mac       VARCHAR(64) NOT NULL DEFAULT ''
	);
COMMENT ON TABLE audit_pruned IS '合法裁剪区间与 HMAC 锚点';
COMMENT ON COLUMN audit_pruned.from_seq IS '裁剪区间起始 seq';
COMMENT ON COLUMN audit_pruned.to_seq IS '裁剪区间结束 seq';
COMMENT ON COLUMN audit_pruned.head_hash IS '区间末条哈希（校验锚点）';
COMMENT ON COLUMN audit_pruned."rows" IS '被裁行数';
COMMENT ON COLUMN audit_pruned.pruned_ms IS '裁剪时间';
COMMENT ON COLUMN audit_pruned.mac IS '裁剪记录的 HMAC';

CREATE TABLE IF NOT EXISTS alert_acks(
		ack_key       VARCHAR(512) PRIMARY KEY,
		rule          VARCHAR(512) NOT NULL DEFAULT '',
		host          VARCHAR(255) NOT NULL DEFAULT '',
		instance      VARCHAR(1024) NOT NULL DEFAULT '',
		starts_at     BIGINT NOT NULL DEFAULT 0,
		status        VARCHAR(32) NOT NULL DEFAULT 'pending',
		user_name     VARCHAR(128) NOT NULL DEFAULT '',
		assignee      VARCHAR(128) NOT NULL DEFAULT '',
		time_ms       BIGINT NOT NULL DEFAULT 0,
		ack_time_ms   BIGINT NOT NULL DEFAULT 0,
		close_time_ms BIGINT NOT NULL DEFAULT 0,
		close_reason  TEXT NOT NULL,
		comments      TEXT NOT NULL
	);
COMMENT ON TABLE alert_acks IS '告警处置（认领 / 指派 / 关闭 / 评论）';
COMMENT ON COLUMN alert_acks.ack_key IS '处置键 rule|host|instance|startsAt';
COMMENT ON COLUMN alert_acks.rule IS '规则名';
COMMENT ON COLUMN alert_acks.host IS '主机';
COMMENT ON COLUMN alert_acks.instance IS '实例';
COMMENT ON COLUMN alert_acks.starts_at IS '告警触发时间';
COMMENT ON COLUMN alert_acks.status IS 'pending / ack / closed';
COMMENT ON COLUMN alert_acks.user_name IS '最近操作人';
COMMENT ON COLUMN alert_acks.assignee IS '当前处理人';
COMMENT ON COLUMN alert_acks.time_ms IS '最近操作时间';
COMMENT ON COLUMN alert_acks.ack_time_ms IS '认领时间';
COMMENT ON COLUMN alert_acks.close_time_ms IS '关闭时间';
COMMENT ON COLUMN alert_acks.close_reason IS '关闭原因';
COMMENT ON COLUMN alert_acks.comments IS '评论 JSON';

CREATE TABLE IF NOT EXISTS ops_tasks(
		id         VARCHAR(64) PRIMARY KEY,
		batch_id   VARCHAR(64) NOT NULL DEFAULT '',
		node       VARCHAR(255) NOT NULL DEFAULT '',
		kind       VARCHAR(64) NOT NULL DEFAULT '',
		state      VARCHAR(32) NOT NULL DEFAULT '',
		created_at BIGINT NOT NULL DEFAULT 0,
		done_at    BIGINT NOT NULL DEFAULT 0,
		task_json  TEXT NOT NULL
	);
COMMENT ON TABLE ops_tasks IS '下行操作任务';
COMMENT ON COLUMN ops_tasks.id IS '任务 ID（ops-N / ob-N）';
COMMENT ON COLUMN ops_tasks.batch_id IS '批次 ID';
COMMENT ON COLUMN ops_tasks.node IS '目标节点';
COMMENT ON COLUMN ops_tasks.kind IS '动作标识';
COMMENT ON COLUMN ops_tasks.state IS 'queued / running / succeeded / failed / canceled / expired';
COMMENT ON COLUMN ops_tasks.created_at IS '创建时间';
COMMENT ON COLUMN ops_tasks.done_at IS '结束时间';
COMMENT ON COLUMN ops_tasks.task_json IS '完整任务 JSON（参数 / 结果 / 操作人）';

CREATE TABLE IF NOT EXISTS ops_meta(
		"key" VARCHAR(64) PRIMARY KEY,
		value TEXT NOT NULL
	);
COMMENT ON TABLE ops_meta IS '下行任务元数据（序号等）';
COMMENT ON COLUMN ops_meta."key" IS '元数据键（目前只有 seq）';
COMMENT ON COLUMN ops_meta.value IS '元数据值';

CREATE TABLE IF NOT EXISTS ops_caps(
		node  VARCHAR(255) PRIMARY KEY,
		kinds TEXT NOT NULL
	);
COMMENT ON TABLE ops_caps IS '节点声明的可执行动作';
COMMENT ON COLUMN ops_caps.node IS '节点名（主键）';
COMMENT ON COLUMN ops_caps.kinds IS '该节点可执行动作（逗号分隔）';

CREATE TABLE IF NOT EXISTS schema_version(
		id      INT PRIMARY KEY,
		version INT NOT NULL
	);
COMMENT ON TABLE schema_version IS '资产库结构版本';
COMMENT ON COLUMN schema_version.id IS '固定为 1';
COMMENT ON COLUMN schema_version.version IS '资产库结构版本';

CREATE INDEX IF NOT EXISTS idx_assets_node ON assets(node);

CREATE INDEX IF NOT EXISTS idx_assets_type ON assets(type_key);

CREATE INDEX IF NOT EXISTS idx_links_from ON asset_links(from_id);

CREATE INDEX IF NOT EXISTS idx_links_to ON asset_links(to_id);

CREATE INDEX IF NOT EXISTS idx_link_supp_from ON asset_link_suppressions(from_id);

CREATE INDEX IF NOT EXISTS idx_link_supp_to ON asset_link_suppressions(to_id);

CREATE INDEX IF NOT EXISTS idx_changes_asset ON asset_changes(asset_id, at);

CREATE INDEX IF NOT EXISTS idx_changes_request ON asset_changes(request_id);

CREATE INDEX IF NOT EXISTS idx_snapshots_asset ON snapshots(asset_id, taken_at);

CREATE INDEX IF NOT EXISTS idx_inspect_runs_at ON inspect_runs(started_at);

CREATE INDEX IF NOT EXISTS idx_inspect_members_asset ON inspect_run_members(asset_id, run_id);

CREATE INDEX IF NOT EXISTS idx_inspect_findings_run ON inspect_findings(run_id, "level", kind);

CREATE INDEX IF NOT EXISTS idx_asset_labels_kv ON asset_labels("key", value);

CREATE INDEX IF NOT EXISTS idx_audit_events_time ON audit_events(time_ms);

CREATE INDEX IF NOT EXISTS idx_audit_events_user ON audit_events(user_name, time_ms);

CREATE INDEX IF NOT EXISTS idx_audit_events_request ON audit_events(request_id);

CREATE INDEX IF NOT EXISTS idx_audit_events_seq ON audit_events(seq);

CREATE INDEX IF NOT EXISTS idx_audit_pruned_range ON audit_pruned(from_seq, to_seq);

CREATE INDEX IF NOT EXISTS idx_alert_acks_time ON alert_acks(time_ms);

CREATE INDEX IF NOT EXISTS idx_alert_acks_status ON alert_acks(status, time_ms);

CREATE INDEX IF NOT EXISTS idx_ops_tasks_created ON ops_tasks(created_at);

CREATE INDEX IF NOT EXISTS idx_ops_tasks_state ON ops_tasks(state);

CREATE INDEX IF NOT EXISTS idx_ops_tasks_pick ON ops_tasks(node, kind, state, done_at);

