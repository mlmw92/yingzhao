package asset

// dmSchemaStatements 是达梦 DM8 版建表语句。
//
// 与 MySQL 版的差异：
//   - 自增主键用 BIGINT IDENTITY(1,1)；
//   - LONGTEXT/TEXT → CLOB，TINYINT 保留；
//   - 内联 KEY 索引改成独立的 CREATE INDEX（见 dmIndexStatements）；
//   - 没有 ENGINE/CHARSET；
//   - 未加引号的标识符会被统一大写，因此 key/schema 不加引号（DM 中非保留），
//     只有保留字 rows 加双引号，且所有引用处都由 sqlx 统一加引号。
var dmSchemaStatements = []string{
	`CREATE TABLE IF NOT EXISTS asset_types(
		key     VARCHAR(64) PRIMARY KEY,
		title   VARCHAR(255) NOT NULL DEFAULT '',
		builtin TINYINT NOT NULL DEFAULT 0,
		schema  CLOB NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS assets(
		id          BIGINT IDENTITY(1,1) PRIMARY KEY,
		type_key    VARCHAR(64) NOT NULL,
		natural_key VARCHAR(255) NOT NULL,
		name        VARCHAR(255) NOT NULL DEFAULT '',
		node        VARCHAR(255) NOT NULL DEFAULT '',
		created_at  BIGINT NOT NULL,
		updated_at  BIGINT NOT NULL,
		CONSTRAINT uk_assets_type_natural UNIQUE(type_key, natural_key),
		CONSTRAINT fk_assets_type FOREIGN KEY(type_key) REFERENCES asset_types(key)
	)`,
	`CREATE TABLE IF NOT EXISTS asset_attrs(
		asset_id   BIGINT NOT NULL,
		key        VARCHAR(128) NOT NULL,
		value      CLOB NOT NULL,
		source     VARCHAR(32) NOT NULL,
		updated_at BIGINT NOT NULL,
		updated_by VARCHAR(128) NOT NULL DEFAULT '',
		PRIMARY KEY(asset_id, key, source),
		CONSTRAINT fk_attrs_asset FOREIGN KEY(asset_id) REFERENCES assets(id) ON DELETE CASCADE
	)`,
	`CREATE TABLE IF NOT EXISTS asset_links(
		id         BIGINT IDENTITY(1,1) PRIMARY KEY,
		from_id    BIGINT NOT NULL,
		to_id      BIGINT NOT NULL,
		kind       VARCHAR(64) NOT NULL,
		source     VARCHAR(32) NOT NULL DEFAULT 'discovery',
		created_at BIGINT NOT NULL,
		CONSTRAINT uk_links UNIQUE(from_id, to_id, kind),
		CONSTRAINT fk_links_from FOREIGN KEY(from_id) REFERENCES assets(id) ON DELETE CASCADE,
		CONSTRAINT fk_links_to FOREIGN KEY(to_id) REFERENCES assets(id) ON DELETE CASCADE
	)`,
	`CREATE TABLE IF NOT EXISTS asset_link_suppressions(
		id         BIGINT IDENTITY(1,1) PRIMARY KEY,
		from_id    BIGINT NOT NULL,
		to_id      BIGINT NOT NULL,
		kind       VARCHAR(64) NOT NULL,
		created_at BIGINT NOT NULL,
		created_by VARCHAR(128) NOT NULL DEFAULT '',
		CONSTRAINT uk_link_supp UNIQUE(from_id, to_id, kind),
		CONSTRAINT fk_supp_from FOREIGN KEY(from_id) REFERENCES assets(id) ON DELETE CASCADE,
		CONSTRAINT fk_supp_to FOREIGN KEY(to_id) REFERENCES assets(id) ON DELETE CASCADE
	)`,
	`CREATE TABLE IF NOT EXISTS asset_changes(
		id         BIGINT IDENTITY(1,1) PRIMARY KEY,
		asset_id   BIGINT NOT NULL,
		field      VARCHAR(128) NOT NULL,
		old_value  CLOB NOT NULL,
		new_value  CLOB NOT NULL,
		source     VARCHAR(32) NOT NULL DEFAULT '',
		actor      VARCHAR(128) NOT NULL DEFAULT '',
		kind       VARCHAR(64) NOT NULL,
		at         BIGINT NOT NULL,
		request_id VARCHAR(64) NOT NULL DEFAULT '',
		CONSTRAINT fk_changes_asset FOREIGN KEY(asset_id) REFERENCES assets(id) ON DELETE CASCADE
	)`,
	`CREATE TABLE IF NOT EXISTS snapshots(
		id       BIGINT IDENTITY(1,1) PRIMARY KEY,
		asset_id BIGINT NOT NULL,
		taken_at BIGINT NOT NULL,
		CONSTRAINT fk_snapshots_asset FOREIGN KEY(asset_id) REFERENCES assets(id) ON DELETE CASCADE
	)`,
	`CREATE TABLE IF NOT EXISTS snapshot_fields(
		snapshot_id BIGINT NOT NULL,
		key         VARCHAR(128) NOT NULL,
		value       CLOB NOT NULL,
		PRIMARY KEY(snapshot_id, key),
		CONSTRAINT fk_sfields_snapshot FOREIGN KEY(snapshot_id) REFERENCES snapshots(id) ON DELETE CASCADE
	)`,
	`CREATE TABLE IF NOT EXISTS asset_seen(
		asset_id     BIGINT PRIMARY KEY,
		last_seen_at BIGINT NOT NULL,
		CONSTRAINT fk_seen_asset FOREIGN KEY(asset_id) REFERENCES assets(id) ON DELETE CASCADE
	)`,
	`CREATE TABLE IF NOT EXISTS inspect_runs(
		id         BIGINT IDENTITY(1,1) PRIMARY KEY,
		scope      VARCHAR(255) NOT NULL DEFAULT '',
		actor      VARCHAR(128) NOT NULL DEFAULT '',
		started_at BIGINT NOT NULL,
		assets     INT NOT NULL DEFAULT 0,
		baselined  INT NOT NULL DEFAULT 0,
		findings   INT NOT NULL DEFAULT 0,
		truncated  INT NOT NULL DEFAULT 0
	)`,
	`CREATE TABLE IF NOT EXISTS inspect_run_manifest(
		run_id BIGINT PRIMARY KEY,
		CONSTRAINT fk_manifest_run FOREIGN KEY(run_id) REFERENCES inspect_runs(id) ON DELETE CASCADE
	)`,
	`CREATE TABLE IF NOT EXISTS inspect_run_members(
		run_id      BIGINT NOT NULL,
		asset_id    BIGINT NOT NULL,
		node_at_run VARCHAR(255) NOT NULL DEFAULT '',
		baselined   INT NOT NULL DEFAULT 0,
		findings    INT NOT NULL DEFAULT 0,
		PRIMARY KEY(run_id, asset_id),
		CONSTRAINT fk_members_run FOREIGN KEY(run_id) REFERENCES inspect_runs(id) ON DELETE CASCADE
	)`,
	`CREATE TABLE IF NOT EXISTS inspect_findings(
		id         BIGINT IDENTITY(1,1) PRIMARY KEY,
		run_id     BIGINT NOT NULL,
		asset_id   BIGINT NOT NULL,
		asset_type VARCHAR(64) NOT NULL DEFAULT '',
		asset_key  VARCHAR(255) NOT NULL DEFAULT '',
		asset_name VARCHAR(255) NOT NULL DEFAULT '',
		node       VARCHAR(255) NOT NULL DEFAULT '',
		field      VARCHAR(128) NOT NULL DEFAULT '',
		kind       VARCHAR(64) NOT NULL,
		level      VARCHAR(32) NOT NULL DEFAULT '',
		expected   CLOB NOT NULL,
		actual     CLOB NOT NULL,
		at         BIGINT NOT NULL,
		CONSTRAINT fk_findings_run FOREIGN KEY(run_id) REFERENCES inspect_runs(id) ON DELETE CASCADE
	)`,
	`CREATE TABLE IF NOT EXISTS inspect_finding_baselines(
		finding_id BIGINT PRIMARY KEY,
		asset_id   BIGINT NOT NULL,
		CONSTRAINT fk_fbase_finding FOREIGN KEY(finding_id) REFERENCES inspect_findings(id) ON DELETE CASCADE
	)`,
	`CREATE TABLE IF NOT EXISTS inspect_baselines(
		type_key    VARCHAR(64) PRIMARY KEY,
		asset_id    BIGINT NOT NULL,
		asset_key   VARCHAR(255) NOT NULL DEFAULT '',
		snapshot_id BIGINT NOT NULL,
		set_by      VARCHAR(128) NOT NULL DEFAULT '',
		set_at      BIGINT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS asset_ignored(
		asset_id   BIGINT PRIMARY KEY,
		reason     CLOB NOT NULL,
		ignored_by VARCHAR(128) NOT NULL DEFAULT '',
		ignored_at BIGINT NOT NULL,
		CONSTRAINT fk_ignored_asset FOREIGN KEY(asset_id) REFERENCES assets(id) ON DELETE CASCADE
	)`,
	`CREATE TABLE IF NOT EXISTS asset_labels(
		asset_id   BIGINT NOT NULL,
		key        VARCHAR(128) NOT NULL,
		value      VARCHAR(512) NOT NULL DEFAULT '',
		updated_at BIGINT NOT NULL,
		updated_by VARCHAR(128) NOT NULL DEFAULT '',
		PRIMARY KEY(asset_id, key),
		CONSTRAINT fk_labels_asset FOREIGN KEY(asset_id) REFERENCES assets(id) ON DELETE CASCADE
	)`,
	`CREATE TABLE IF NOT EXISTS audit_events(
		id         BIGINT IDENTITY(1,1) PRIMARY KEY,
		time_ms    BIGINT NOT NULL,
		user_name  VARCHAR(191) NOT NULL DEFAULT '',
		method     VARCHAR(32) NOT NULL DEFAULT '',
		path       VARCHAR(1024) NOT NULL DEFAULT '',
		status     INT NOT NULL DEFAULT 0,
		remote_ip  VARCHAR(64) NOT NULL DEFAULT '',
		source_loc VARCHAR(191) NOT NULL DEFAULT '',
		succeeded  TINYINT NOT NULL DEFAULT 0,
		category   VARCHAR(128) NOT NULL DEFAULT '',
		action     VARCHAR(191) NOT NULL DEFAULT '',
		detail     CLOB NOT NULL,
		request_id VARCHAR(64) NOT NULL DEFAULT '',
		seq        BIGINT NOT NULL DEFAULT 0,
		prev_hash  VARCHAR(64) NOT NULL DEFAULT '',
		hash       VARCHAR(64) NOT NULL DEFAULT '',
		key_fp     VARCHAR(64) NOT NULL DEFAULT ''
	)`,
	`CREATE TABLE IF NOT EXISTS audit_chain(
		id              INT PRIMARY KEY,
		seq             BIGINT NOT NULL DEFAULT 0,
		hash            VARCHAR(64) NOT NULL DEFAULT '',
		key_fingerprint VARCHAR(64) NOT NULL DEFAULT '',
		updated_ms      BIGINT NOT NULL DEFAULT 0,
		CONSTRAINT chk_audit_chain_id CHECK(id = 1)
	)`,
	`CREATE TABLE IF NOT EXISTS audit_pruned(
		from_seq  BIGINT NOT NULL,
		to_seq    BIGINT NOT NULL,
		head_hash VARCHAR(64) NOT NULL DEFAULT '',
		"rows"    BIGINT NOT NULL DEFAULT 0,
		pruned_ms BIGINT NOT NULL DEFAULT 0,
		mac       VARCHAR(64) NOT NULL DEFAULT ''
	)`,
	`CREATE TABLE IF NOT EXISTS alert_acks(
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
		close_reason  CLOB NOT NULL,
		comments      CLOB NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS ops_tasks(
		id         VARCHAR(64) PRIMARY KEY,
		batch_id   VARCHAR(64) NOT NULL DEFAULT '',
		node       VARCHAR(255) NOT NULL DEFAULT '',
		kind       VARCHAR(64) NOT NULL DEFAULT '',
		state      VARCHAR(32) NOT NULL DEFAULT '',
		created_at BIGINT NOT NULL DEFAULT 0,
		done_at    BIGINT NOT NULL DEFAULT 0,
		task_json  CLOB NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS ops_meta(
		key   VARCHAR(64) PRIMARY KEY,
		value CLOB NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS ops_caps(
		node  VARCHAR(255) PRIMARY KEY,
		kinds CLOB NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS schema_version(
		id      INT PRIMARY KEY,
		version INT NOT NULL
	)`,
}

// dmIndexStatements 是 MySQL 内联 KEY 的对应物。
// 资产结构初始化每次启动都会执行，所以先查 USER_INDEXES 再建，避免重复创建报错。
var dmIndexStatements = []struct{ name, ddl string }{
	{"idx_assets_node", `CREATE INDEX idx_assets_node ON assets(node)`},
	{"idx_assets_type", `CREATE INDEX idx_assets_type ON assets(type_key)`},
	{"idx_links_from", `CREATE INDEX idx_links_from ON asset_links(from_id)`},
	{"idx_links_to", `CREATE INDEX idx_links_to ON asset_links(to_id)`},
	{"idx_link_supp_from", `CREATE INDEX idx_link_supp_from ON asset_link_suppressions(from_id)`},
	{"idx_link_supp_to", `CREATE INDEX idx_link_supp_to ON asset_link_suppressions(to_id)`},
	{"idx_changes_asset", `CREATE INDEX idx_changes_asset ON asset_changes(asset_id, at)`},
	{"idx_changes_request", `CREATE INDEX idx_changes_request ON asset_changes(request_id)`},
	{"idx_snapshots_asset", `CREATE INDEX idx_snapshots_asset ON snapshots(asset_id, taken_at)`},
	{"idx_inspect_runs_at", `CREATE INDEX idx_inspect_runs_at ON inspect_runs(started_at)`},
	{"idx_inspect_members_asset", `CREATE INDEX idx_inspect_members_asset ON inspect_run_members(asset_id, run_id)`},
	{"idx_inspect_findings_run", `CREATE INDEX idx_inspect_findings_run ON inspect_findings(run_id, level, kind)`},
	{"idx_asset_labels_kv", `CREATE INDEX idx_asset_labels_kv ON asset_labels(key, value)`},
	{"idx_audit_events_time", `CREATE INDEX idx_audit_events_time ON audit_events(time_ms)`},
	{"idx_audit_events_user", `CREATE INDEX idx_audit_events_user ON audit_events(user_name, time_ms)`},
	{"idx_audit_events_request", `CREATE INDEX idx_audit_events_request ON audit_events(request_id)`},
	{"idx_audit_events_seq", `CREATE INDEX idx_audit_events_seq ON audit_events(seq)`},
	{"idx_audit_pruned_range", `CREATE INDEX idx_audit_pruned_range ON audit_pruned(from_seq, to_seq)`},
	{"idx_alert_acks_time", `CREATE INDEX idx_alert_acks_time ON alert_acks(time_ms)`},
	{"idx_alert_acks_status", `CREATE INDEX idx_alert_acks_status ON alert_acks(status, time_ms)`},
	{"idx_ops_tasks_created", `CREATE INDEX idx_ops_tasks_created ON ops_tasks(created_at)`},
	{"idx_ops_tasks_state", `CREATE INDEX idx_ops_tasks_state ON ops_tasks(state)`},
	{"idx_ops_tasks_pick", `CREATE INDEX idx_ops_tasks_pick ON ops_tasks(node, kind, state, done_at)`},
}
