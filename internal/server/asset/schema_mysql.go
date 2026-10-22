package asset

// mySQLSchemaStatements 是 MySQL 版建表语句。
//
// 与 SQLite 版的差异只在类型与少数关键字：自增用 BIGINT AUTO_INCREMENT、
// 主键/索引列用 VARCHAR（TEXT 不能直接做键）、`key`/`schema` 是 MySQL 关键字需要反引号、
// 部分索引（WHERE ...）不被支持，改为普通索引。
var mySQLSchemaStatements = mysqlWithComments(mySQLRawSchemaStatements)

// mySQLRawSchemaStatements 是不带 COMMENT 的建表语句；注释由 mysqlWithComments 注入。
var mySQLRawSchemaStatements = []string{
	`CREATE TABLE IF NOT EXISTS asset_types(
		` + "`key`" + `  VARCHAR(64) PRIMARY KEY,
		title   VARCHAR(255) NOT NULL DEFAULT '',
		builtin TINYINT NOT NULL DEFAULT 0,
		` + "`schema`" + `  LONGTEXT NOT NULL DEFAULT ('{}')
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS assets(
		id          BIGINT AUTO_INCREMENT PRIMARY KEY,
		type_key    VARCHAR(64) NOT NULL,
		natural_key VARCHAR(255) NOT NULL,
		name        VARCHAR(255) NOT NULL DEFAULT '',
		node        VARCHAR(255) NOT NULL DEFAULT '',
		created_at  BIGINT NOT NULL,
		updated_at  BIGINT NOT NULL,
		UNIQUE KEY uk_assets_type_natural(type_key, natural_key),
		KEY idx_assets_node(node),
		KEY idx_assets_type(type_key),
		CONSTRAINT fk_assets_type FOREIGN KEY(type_key) REFERENCES asset_types(` + "`key`" + `)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS asset_attrs(
		asset_id   BIGINT NOT NULL,
		` + "`key`" + `      VARCHAR(128) NOT NULL,
		value      LONGTEXT NOT NULL,
		source     VARCHAR(32) NOT NULL,
		updated_at BIGINT NOT NULL,
		updated_by VARCHAR(128) NOT NULL DEFAULT '',
		PRIMARY KEY(asset_id, ` + "`key`" + `, source),
		CONSTRAINT fk_attrs_asset FOREIGN KEY(asset_id) REFERENCES assets(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS asset_links(
		id         BIGINT AUTO_INCREMENT PRIMARY KEY,
		from_id    BIGINT NOT NULL,
		to_id      BIGINT NOT NULL,
		kind       VARCHAR(64) NOT NULL,
		source     VARCHAR(32) NOT NULL DEFAULT 'discovery',
		created_at BIGINT NOT NULL,
		UNIQUE KEY uk_links(from_id, to_id, kind),
		KEY idx_links_from(from_id),
		KEY idx_links_to(to_id),
		CONSTRAINT fk_links_from FOREIGN KEY(from_id) REFERENCES assets(id) ON DELETE CASCADE,
		CONSTRAINT fk_links_to FOREIGN KEY(to_id) REFERENCES assets(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS asset_link_suppressions(
		id         BIGINT AUTO_INCREMENT PRIMARY KEY,
		from_id    BIGINT NOT NULL,
		to_id      BIGINT NOT NULL,
		kind       VARCHAR(64) NOT NULL,
		created_at BIGINT NOT NULL,
		created_by VARCHAR(128) NOT NULL DEFAULT '',
		UNIQUE KEY uk_link_supp(from_id, to_id, kind),
		KEY idx_link_supp_from(from_id),
		KEY idx_link_supp_to(to_id),
		CONSTRAINT fk_supp_from FOREIGN KEY(from_id) REFERENCES assets(id) ON DELETE CASCADE,
		CONSTRAINT fk_supp_to FOREIGN KEY(to_id) REFERENCES assets(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS asset_changes(
		id         BIGINT AUTO_INCREMENT PRIMARY KEY,
		asset_id   BIGINT NOT NULL,
		field      VARCHAR(128) NOT NULL,
		old_value  LONGTEXT NOT NULL,
		new_value  LONGTEXT NOT NULL,
		source     VARCHAR(32) NOT NULL DEFAULT '',
		actor      VARCHAR(128) NOT NULL DEFAULT '',
		kind       VARCHAR(64) NOT NULL,
		at         BIGINT NOT NULL,
		request_id VARCHAR(64) NOT NULL DEFAULT '',
		KEY idx_changes_asset(asset_id, at),
		KEY idx_changes_request(request_id),
		CONSTRAINT fk_changes_asset FOREIGN KEY(asset_id) REFERENCES assets(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS snapshots(
		id       BIGINT AUTO_INCREMENT PRIMARY KEY,
		asset_id BIGINT NOT NULL,
		taken_at BIGINT NOT NULL,
		KEY idx_snapshots_asset(asset_id, taken_at),
		CONSTRAINT fk_snapshots_asset FOREIGN KEY(asset_id) REFERENCES assets(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS snapshot_fields(
		snapshot_id BIGINT NOT NULL,
		` + "`key`" + `         VARCHAR(128) NOT NULL,
		value       LONGTEXT NOT NULL,
		PRIMARY KEY(snapshot_id, ` + "`key`" + `),
		CONSTRAINT fk_sfields_snapshot FOREIGN KEY(snapshot_id) REFERENCES snapshots(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS asset_seen(
		asset_id     BIGINT PRIMARY KEY,
		last_seen_at BIGINT NOT NULL,
		CONSTRAINT fk_seen_asset FOREIGN KEY(asset_id) REFERENCES assets(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS inspect_runs(
		id         BIGINT AUTO_INCREMENT PRIMARY KEY,
		scope      VARCHAR(255) NOT NULL DEFAULT '',
		actor      VARCHAR(128) NOT NULL DEFAULT '',
		started_at BIGINT NOT NULL,
		assets     INT NOT NULL DEFAULT 0,
		baselined  INT NOT NULL DEFAULT 0,
		findings   INT NOT NULL DEFAULT 0,
		truncated  INT NOT NULL DEFAULT 0,
		KEY idx_inspect_runs_at(started_at)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS inspect_run_manifest(
		run_id BIGINT PRIMARY KEY,
		CONSTRAINT fk_manifest_run FOREIGN KEY(run_id) REFERENCES inspect_runs(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS inspect_run_members(
		run_id      BIGINT NOT NULL,
		asset_id    BIGINT NOT NULL,
		node_at_run VARCHAR(255) NOT NULL DEFAULT '',
		baselined   INT NOT NULL DEFAULT 0,
		findings    INT NOT NULL DEFAULT 0,
		PRIMARY KEY(run_id, asset_id),
		KEY idx_inspect_members_asset(asset_id, run_id),
		CONSTRAINT fk_members_run FOREIGN KEY(run_id) REFERENCES inspect_runs(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS inspect_findings(
		id         BIGINT AUTO_INCREMENT PRIMARY KEY,
		run_id     BIGINT NOT NULL,
		asset_id   BIGINT NOT NULL,
		asset_type VARCHAR(64) NOT NULL DEFAULT '',
		asset_key  VARCHAR(255) NOT NULL DEFAULT '',
		asset_name VARCHAR(255) NOT NULL DEFAULT '',
		node       VARCHAR(255) NOT NULL DEFAULT '',
		field      VARCHAR(128) NOT NULL DEFAULT '',
		kind       VARCHAR(64) NOT NULL,
		level      VARCHAR(32) NOT NULL DEFAULT '',
		expected   LONGTEXT NOT NULL,
		actual     LONGTEXT NOT NULL,
		at         BIGINT NOT NULL,
		KEY idx_inspect_findings_run(run_id, level, kind),
		CONSTRAINT fk_findings_run FOREIGN KEY(run_id) REFERENCES inspect_runs(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS inspect_finding_baselines(
		finding_id BIGINT PRIMARY KEY,
		asset_id   BIGINT NOT NULL,
		CONSTRAINT fk_fbase_finding FOREIGN KEY(finding_id) REFERENCES inspect_findings(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS inspect_baselines(
		type_key    VARCHAR(64) PRIMARY KEY,
		asset_id    BIGINT NOT NULL,
		asset_key   VARCHAR(255) NOT NULL DEFAULT '',
		snapshot_id BIGINT NOT NULL,
		set_by      VARCHAR(128) NOT NULL DEFAULT '',
		set_at      BIGINT NOT NULL
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS asset_ignored(
		asset_id   BIGINT PRIMARY KEY,
		reason     LONGTEXT NOT NULL,
		ignored_by VARCHAR(128) NOT NULL DEFAULT '',
		ignored_at BIGINT NOT NULL,
		CONSTRAINT fk_ignored_asset FOREIGN KEY(asset_id) REFERENCES assets(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS asset_labels(
		asset_id   BIGINT NOT NULL,
		` + "`key`" + `      VARCHAR(128) NOT NULL,
		value      VARCHAR(512) NOT NULL DEFAULT '',
		updated_at BIGINT NOT NULL,
		updated_by VARCHAR(128) NOT NULL DEFAULT '',
		PRIMARY KEY(asset_id, ` + "`key`" + `),
		KEY idx_asset_labels_kv(` + "`key`" + `, value),
		CONSTRAINT fk_labels_asset FOREIGN KEY(asset_id) REFERENCES assets(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS audit_events(
		id         BIGINT AUTO_INCREMENT PRIMARY KEY,
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
		detail     LONGTEXT NOT NULL,
		request_id VARCHAR(64) NOT NULL DEFAULT '',
		seq        BIGINT NOT NULL DEFAULT 0,
		prev_hash  VARCHAR(64) NOT NULL DEFAULT '',
		hash       VARCHAR(64) NOT NULL DEFAULT '',
		key_fp     VARCHAR(64) NOT NULL DEFAULT '',
		KEY idx_audit_events_time(time_ms),
		KEY idx_audit_events_user(user_name, time_ms),
		KEY idx_audit_events_request(request_id),
		KEY idx_audit_events_seq(seq)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS audit_chain(
		id              INT PRIMARY KEY,
		seq             BIGINT NOT NULL DEFAULT 0,
		hash            VARCHAR(64) NOT NULL DEFAULT '',
		key_fingerprint VARCHAR(64) NOT NULL DEFAULT '',
		updated_ms      BIGINT NOT NULL DEFAULT 0,
		CONSTRAINT chk_audit_chain_id CHECK(id = 1)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS audit_pruned(
		from_seq  BIGINT NOT NULL,
		to_seq    BIGINT NOT NULL,
		head_hash VARCHAR(64) NOT NULL DEFAULT '',
		` + "`rows`" + ` BIGINT NOT NULL DEFAULT 0,
		pruned_ms BIGINT NOT NULL DEFAULT 0,
		mac       VARCHAR(64) NOT NULL DEFAULT '',
		KEY idx_audit_pruned_range(from_seq, to_seq)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
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
		close_reason  LONGTEXT NOT NULL,
		comments      LONGTEXT NOT NULL,
		KEY idx_alert_acks_time(time_ms),
		KEY idx_alert_acks_status(status, time_ms)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS ops_tasks(
		id         VARCHAR(64) PRIMARY KEY,
		batch_id   VARCHAR(64) NOT NULL DEFAULT '',
		node       VARCHAR(255) NOT NULL DEFAULT '',
		kind       VARCHAR(64) NOT NULL DEFAULT '',
		state      VARCHAR(32) NOT NULL DEFAULT '',
		created_at BIGINT NOT NULL DEFAULT 0,
		done_at    BIGINT NOT NULL DEFAULT 0,
		task_json  LONGTEXT NOT NULL,
		KEY idx_ops_tasks_created(created_at),
		KEY idx_ops_tasks_state(state),
		KEY idx_ops_tasks_pick(node, kind, state, done_at)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS ops_meta(
		` + "`key`" + ` VARCHAR(64) PRIMARY KEY,
		value LONGTEXT NOT NULL
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS ops_caps(
		node  VARCHAR(255) PRIMARY KEY,
		kinds LONGTEXT NOT NULL
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS schema_version(
		id      INT PRIMARY KEY,
		version INT NOT NULL
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
}
