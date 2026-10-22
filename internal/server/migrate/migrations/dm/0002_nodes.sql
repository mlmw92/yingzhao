CREATE TABLE IF NOT EXISTS nodes(
  hostname     VARCHAR(255) NOT NULL,
  mode         VARCHAR(32)  NOT NULL DEFAULT '',
  display_name VARCHAR(255) NOT NULL DEFAULT '',
  ip           VARCHAR(64)  NOT NULL DEFAULT '',
  os           VARCHAR(128) NOT NULL DEFAULT '',
  arch         VARCHAR(64)  NOT NULL DEFAULT '',
  group_name   VARCHAR(128) NOT NULL DEFAULT '',
  labels       CLOB         NULL,
  version      VARCHAR(64)  NOT NULL DEFAULT '',
  host_info    CLOB         NULL,
  log_sources  CLOB         NULL,
  status       VARCHAR(32)  NOT NULL DEFAULT '',
  last_seen    BIGINT       NOT NULL DEFAULT 0,
  created_at   BIGINT       NOT NULL DEFAULT 0,
  updated_at   BIGINT       NOT NULL DEFAULT 0,
  PRIMARY KEY (hostname)
)
