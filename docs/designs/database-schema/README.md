# 状态库最终表结构（三种方言）

本目录描述**新系统初始化后的最终表结构**，不是迁移过程记录；不包含
`schema_migrations`、`migration_lock` 这类运行时/过程表。

生成来源：

- 核心表（`config_documents`、`nodes`、`node_groups`）的 DDL 维护在
  `internal/server/migrate/migrations/<dialect>/`，文档里只呈现最终形态；
- 其余表（资产台账、巡检、审计、告警处置、下行任务）在
  `internal/server/asset/schema_<dialect>.go`。

| 文件 | 目标库 |
|---|---|
| `mysql.sql` | MySQL 8.0+（当前生产） |
| `postgres.sql` | PostgreSQL 12+ / 瀚高 / 金仓 |
| `dm.sql` | 达梦 DM8 |

修改表结构时改源码，再重新导出；**不要直接改本目录的 SQL**：

```bash
go test -tags schemadoc ./internal/server/asset -run TestDumpSchemaScripts
```

脚本里的表/列说明是**数据库注释**，不是 `--` 注释：

- MySQL：建表语句里的 `COMMENT '...'` / `COMMENT='...'`；
- PostgreSQL / 达梦：建表后的 `COMMENT ON TABLE/COLUMN ... IS '...'`。

自动建表时会一并写入数据库，可用 `SHOW FULL COLUMNS`（MySQL）、
`\d+ 表名`（PG）或 `USER_COL_COMMENTS`（达梦）查看。

## 表清单

**配置与节点**

| 表 | 用途 |
|---|---|
| `config_documents` | 整份读写的配置文档（YAML/JSON 信封，敏感内容 AES-GCM） |
| `nodes` / `node_groups` | 节点注册信息、心跳、分组 |

**资产台账与巡检**

| 表 | 用途 |
|---|---|
| `asset_types` | 资产类型与类型模型（`schema` 列） |
| `assets` / `asset_attrs` | 资产与属性 |
| `asset_links` / `asset_link_suppressions` | 资产关系与人工删除抑制 |
| `asset_changes` | 字段级变更记录 |
| `snapshots` / `snapshot_fields` | 配置快照 |
| `asset_seen` / `asset_ignored` / `asset_labels` | 最近上报、忽略、标签 |
| `inspect_runs` / `inspect_run_manifest` / `inspect_run_members` | 巡检批次 |
| `inspect_findings` / `inspect_finding_baselines` / `inspect_baselines` | 巡检差异与标杆 |
| `schema_version` | 资产库结构版本（运行时校验，不是迁移记录） |

**审计**

| 表 | 用途 |
|---|---|
| `audit_events` | 审计事件（`seq` + `prev_hash` + `hash` 组成哈希链） |
| `audit_chain` | 链头（单行，`FOR UPDATE` 串行追加） |
| `audit_pruned` | 合法裁剪区间与 HMAC 锚点（`rows` 列名在 MySQL/DM 是保留字） |

**协作状态**

| 表 | 用途 |
|---|---|
| `alert_acks` | 告警认领/指派/关闭与评论 |
| `ops_tasks` / `ops_meta` / `ops_caps` | 下行任务、序号、节点能力 |

## 通用约定

- 时间统一 `BIGINT` epoch 毫秒，不引入时区语义。
- 布尔用整数（MySQL `TINYINT`、PG/DM `SMALLINT`/`TINYINT`），避免驱动对 `BOOLEAN` 的差异。
- 自增主键：MySQL `BIGINT AUTO_INCREMENT`、PG `BIGSERIAL`、DM `BIGINT IDENTITY(1,1)`。
- 长文本：MySQL `LONGTEXT`、PG `TEXT`、DM `CLOB`。
- 大 JSON（配置信封、节点快照）只做整份读写：MySQL 用 `JSON`，PG/DM 用 `TEXT`/`CLOB`，
  不依赖 JSON 路径查询，也避免 `[]byte` 与 JSONB/CLOB 的编码差异。

## 达梦特有

- `key`、`schema`、`rows` 按保留字处理：DDL 与 DML 统一加双引号（`"key"`）；
  未加引号的标识符会被 DM 统一大写，两者不能混用。
- upsert 用 `MERGE INTO`（由 `sqlx` 把 `INSERT ... ON CONFLICT` 改写）。
- 索引创建前先查 `USER_INDEXES`（DM 没有 `CREATE INDEX IF NOT EXISTS`）。
