CREATE TABLE IF NOT EXISTS config_documents (
  namespace   VARCHAR(64)  NOT NULL,
  version     BIGINT       NOT NULL DEFAULT 1,
  payload     JSON         NOT NULL,
  updated_at  BIGINT       NOT NULL,
  updated_by  VARCHAR(128) NOT NULL DEFAULT '',
  PRIMARY KEY (namespace)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
