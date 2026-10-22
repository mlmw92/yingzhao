# 多数据库支持设计（PostgreSQL / 瀚高 / 达梦 / 金仓）

- 日期：2026-10-21
- 状态：实施中
- 前置：`2026-10-21-mysql-state-backend-design.md`（状态全部入库）
- 目标：状态库不再绑定 MySQL 8；同一套 Server 可按 `database.driver` 连接
  PostgreSQL、瀚高（HighGo）、达梦（DM）、金仓（KingbaseES）

---

## 1. 兼容性判断

| 目标库 | 协议/语法族 | Go 驱动 | 结论 |
|---|---|---|---|
| MySQL 8+ | MySQL | `go-sql-driver/mysql` | 已有 |
| PostgreSQL 12+ | PostgreSQL | `lib/pq`（已在依赖中） | 直接支持 |
| 瀚高 HighGo | PostgreSQL 兼容 | `lib/pq` | 按 PostgreSQL 方言支持；差异见 §7 |
| 金仓 KingbaseES | PostgreSQL 兼容 | `lib/pq` | 同上 |
| 达梦 DM8 | Oracle 风格 | `gitee.com/chunanyong/dm` | **驱动当前环境不可获取**（离线、无网络）；代码按方言预留，默认构建不含驱动，见 §8 |

## 2. 配置

```yaml
database:
  driver: mysql          # mysql | postgres | highgo | kingbase | dm
  dsn: "..."             # 各驱动自己的 DSN 格式
  maxOpenConns: 20
  maxIdleConns: 5
  connMaxLifetimeSeconds: 1800
  masterKeyFile: /etc/monitor-server/master.key
```

- `postgres` / `highgo` / `kingbase` 共用 PostgreSQL 方言；`highgo` / `kingbase` 只是
  更明确的别名（日志与错误提示里显示实际驱动名）。
- `mysql` 保持现有 DSN 归一化（补 `charset`、`clientFoundRows`）。
- `postgres` 的 DSN 原样交给 `lib/pq`（支持 URL 与 key=value 两种写法）。

## 3. 方言层

`internal/server/sqlx` 从「`mysql bool` + 正则改写」升级为显式 `Dialect`：

```go
type Dialect interface {
    Name() string        // mysql / postgres / dm
    Driver() string      // 传给 sql.Open 的驱动名
    Bind(string) string  // ? -> $n（PostgreSQL）
    Rewrite(string) string
    Upsert(cols, sets string) string
    UpsertNothing(col string) string
    FromDual() string
}
```

- `Rewrite`：保留字标识符按方言加引号（MySQL 反引号、PG/DM 双引号）；
  MySQL 下删除 `COLLATE NOCASE`。
- `Bind`：PostgreSQL 把 `?` 换成 `$1..$n`；扫描时跳过字符串字面量与双引号标识符。
- `Upsert`：MySQL `ON DUPLICATE KEY UPDATE`；PostgreSQL `ON CONFLICT ... DO UPDATE SET`；
  达梦走 `MERGE`（见 §8）。
- 所有执行入口（`DB`/`Tx`）统一做 `Bind(Rewrite(sql))`，调用点不再感知方言。

## 4. Schema 与迁移

### 4.1 迁移目录

```
internal/server/migrate/migrations/mysql/0001_*.sql ...
internal/server/migrate/migrations/postgres/0001_*.sql ...
internal/server/migrate/migrations/dm/0001_*.sql ...
```

`migrate.Load(dialect)` 只读对应目录；`schema_migrations` 的校验和按方言各算各的。
现有 MySQL 文件内容不变，仅移动目录——已部署实例的校验和不受影响。

### 4.2 资产台账

`asset/schema_mysql.go` 的 25 张表按方言各一份（`schema_postgres.go`、`schema_dm.go`）。
类型映射：

| 逻辑类型 | MySQL | PostgreSQL | DM |
|---|---|---|---|
| 自增主键 | `BIGINT AUTO_INCREMENT` | `BIGSERIAL` | `BIGINT IDENTITY(1,1)` |
| 长文本 | `LONGTEXT` | `TEXT` | `CLOB` |
| JSON | `JSON` | `JSONB` | `CLOB` |
| 布尔 | `TINYINT` | `SMALLINT` | `TINYINT` |
| 表尾 | `ENGINE=InnoDB DEFAULT CHARSET=utf8mb4` | 无 | 无 |
| 内联索引 | `KEY idx(...)` | 独立 `CREATE INDEX IF NOT EXISTS` | 独立 `CREATE INDEX` |

**布尔统一用 `SMALLINT`/`TINYINT` 而不是 PG 的 `BOOLEAN`**：代码按整数扫描，
换类型会引入驱动差异，且没有收益。

### 4.3 自增主键回读

`LastInsertId()` 在 `lib/pq` 下不支持。新增 `sqlx.InsertID(execer, dialect, sql, args...)`：

- MySQL / DM：`Exec` + `LastInsertId`；
- PostgreSQL：`sql + " RETURNING id"` + `QueryRow`。

调用点只有资产包的 4 处。

### 4.4 迁移锁

- MySQL：`GET_LOCK('yingzhao_schema_migrate', 30)`（现有实现）。
- PostgreSQL：`SELECT pg_advisory_lock($1)` / `pg_advisory_unlock($1)`，键为固定 int64。
- 达梦：`SELECT ... FOR UPDATE` 锁 `schema_migrations` 的控制行（实现随驱动一起提供）。

## 5. 大小写与排序规则

- 依赖大小写不敏感的地方不再使用 `COLLATE NOCASE`：MySQL 与 PG 都改用显式 `LOWER()`。
- 主键/唯一键的语义以各库默认值为准，文档写明：**节点主机名按 DNS 约定大小写不敏感**，
  同一台机器不要用两种大小写上报。

## 6. 测试

- 单元：`Bind` / `Rewrite` / `Upsert` / DSN 脱敏 / 驱动选择，表驱动。
- 集成：`TEST_POSTGRES_DSN` 存在时，把现有 MySQL 集成用例按方言再跑一遍。
  dev-server 的 `mw-postgres`（PostgreSQL 16）经 SSH 隧道 `127.0.0.1:15432` 访问。
- 瀚高 / 金仓：按 PostgreSQL 协议兼容处理；现场验收清单见 §7。
- 达梦：驱动不可用时跳过集成用例。

## 7. 瀚高 / 金仓差异清单（现场验收用）

1. 版本：瀚高 V9 / 金仓 V8R6 及以上，`lib/pq` 可直连。
2. 大小写：默认大小写敏感；建库时不要启用 Oracle 兼容模式（会改变空串与 NULL 语义）。
3. `JSONB`：两者均支持；若不支持则按 `TEXT` 建表（代码只做整份读写）。
4. 自增：`BIGSERIAL` 可用；金仓 Oracle 模式下需改 `IDENTITY`。
5. 迁移锁：`pg_advisory_lock` 可用；不可用时退回控制行 `FOR UPDATE`。

## 8. 达梦（DM8）

- 驱动：`gitee.com/chunanyong/dm v1.8.23` 已加入依赖并注册为 `dm`。
- 方言：`?` 占位符、未加引号的标识符统一大写；只有保留字 `rows` 加双引号。
- upsert：`INSERT ... ON CONFLICT(...)` 由 `sqlx` 改写成 `MERGE INTO ... USING
  (SELECT ... FROM DUAL) ...`（含 `DO NOTHING` 与 `MAX → GREATEST`）。
- schema：`migrations/dm/` 4 条迁移；资产台账 `schema_dm.go`（25 表 + 23 索引，
  索引先查 `USER_INDEXES` 再建）。
- 自增主键：DM 驱动支持 `LastInsertId`，走 `Exec + LastInsertId`。
- **实机验证（2026-10-22，dev-server 上的 DM8 容器）**：
  - 镜像 `zjwmkrnwr2x8y72vh2.xuanyuan.run/yssenn/dameng:dm8_20260427_x86_CentOS7`，
    端口映射 `15236:5236`，`DM_SYSDBA_PWD=<password>`；
  - 六组集成用例全部通过：配置文档契约（含并发创建/版本冲突）、资产台账
    （25 表、MERGE upsert、关系优先级、类型模型）、审计（链初始化/写入/裁剪/校验）、
    下行任务（回填/序号跨重启）、告警处置回填、节点整份同步与心跳补写；
  - Server 完整启动通过：`/readyz` 的 `database` 为 ok，资产日志 `db=dm`。
- 实现中确认的 DM 差异：
  - `key`/`schema`/`rows` 都按保留字处理，DDL 与 DML 统一加双引号；
  - `MERGE` 的 SET 里目标表只能用别名 `t`（原 SQL 的 `<table>.col` 需替换）；
  - 唯一约束冲突码为 `-6602`，迁移锁用控制表行锁（`SELECT ... FOR UPDATE`）。

复现命令（测试实例）：

```bash
docker run -d --name dm-test -p 15236:5236 \
  --ulimit nofile=65536:65536 --ulimit nproc=65536:65536 \
  -e DM_SYSDBA_PWD=<password> -e DM_SYSAUDITOR_PWD=<auditor-password> \
  -v /var/lib/dm-test:/dmdata \
  zjwmkrnwr2x8y72vh2.xuanyuan.run/yssenn/dameng:dm8_20260427_x86_CentOS7

TEST_DM_DSN='dm://SYSDBA:<password>@127.0.0.1:5236' \
  go test -p 1 ./internal/server/... -run DM
```

## 9. 实施顺序

1. `sqlx.Dialect` + MySQL/PostgreSQL 实现 + 单元测试。
2. `db.Open` 按 `database.driver` 选驱动与方言；DSN 脱敏。
3. `configdoc`、`asset`、`audit`、`ops`、`ack`、`node` 改用方言。
4. 迁移目录按方言拆分 + PostgreSQL SQL。
5. 资产 schema 的 PostgreSQL 版本；`InsertID` 助手。
6. PostgreSQL 集成用例（dev-server 的 PG16）。
7. 安装脚本/文档/配置参考；达梦构建说明。

---

## 10. 实施记录（2026-10-22）

**已落地**

- `internal/server/sqlx` 升级为 `Dialect` 接口（MySQL / PostgreSQL / SQLite），
  执行入口统一做 `Bind(Rewrite(sql))`；`InsertID` 在 PostgreSQL 下走 `RETURNING id`。
- `database.driver` 配置与驱动选择：`mysql` / `postgres` / `highgo` / `kingbase`；
  DSN 脱敏按驱动分别处理（MySQL 解析器 / PostgreSQL URL 与 key=value）。
- 迁移目录按方言拆分：`migrations/mysql/`、`migrations/postgres/`；
  迁移锁 MySQL 用 `GET_LOCK`，PostgreSQL 用 `pg_try_advisory_lock`（30 秒等待）。
- 资产台账新增 `schema_postgres.go`（25 张表 + 23 个索引）；
  `OpenSQL(db, dialect)` 取代写死的 `OpenMySQL`。
- 配置文档、审计、告警处置、下行任务、节点注册与心跳全部走方言层。
- 集成验证：dev-server 的 PostgreSQL 16（`mw-postgres`，SSH 隧道 `127.0.0.1:15432`）上
  跑通配置文档契约、资产台账、审计裁剪与校验、下行任务回填与序号、告警处置回填、节点整份同步与心跳补写。
  MySQL 原有集成用例全部保持通过。

**达梦（DM）**

- 驱动已通过代理拉取并加入依赖（`gitee.com/chunanyong/dm v1.8.23`）。
- 已落地：`dm` 方言、`MERGE` upsert 改写、`migrations/dm/` 4 条迁移、
  `schema_dm.go`（25 表 + 23 索引）、`LastInsertId` 路径、控制表行锁迁移锁。
- **实机验证通过（2026-10-22）**：dev-server 上的 DM8 容器跑通六组集成用例与
  Server 完整启动（详见 §8）。实现中修正了 `schema` 保留字、MERGE 目标表别名、
  `-6602` 唯一约束映射三个只有真实实例才会暴露的问题。

**已知差异**

- PostgreSQL 的 `ON CONFLICT DO UPDATE` 中，未限定的目标列名会被判为 ambiguous
  （`asset_seen.last_seen_at`、`nodes.last_seen` 已改为表名限定）。
- PostgreSQL 没有 `COLLATE NOCASE`；大小写折叠统一用 `LOWER()`。
- JSON 列在 PostgreSQL 用 `TEXT`（代码只做整份读写，不需要 JSONB 的检索能力，
  也避免 `[]byte` 与 JSONB 的编码差异）。
