# 数据库部署指南

Server 的所有状态（配置、协作、资产、审计、节点、安全）都在一个关系库里，
**数据库是硬依赖**：连不上或没配置，Server 会直接拒绝启动。

本文给出建库/建账号 SQL、连接配置、升级顺序与备份清单。

---

## 1. 支持矩阵

| `database.driver` | 数据库 | 版本要求 | 驱动 |
|---|---|---|---|
| `mysql`（默认） | MySQL | 8.0+ | `go-sql-driver/mysql` |
| `postgres` | PostgreSQL | 12+ | `lib/pq` |
| `highgo` | 瀚高 HighGo | V9+ | `lib/pq`（PostgreSQL 协议） |
| `kingbase` | 金仓 KingbaseES | V8R6+ | `lib/pq`（PostgreSQL 协议） |
| `dm` | 达梦 DM8 | 8.1.1.126+ | `gitee.com/chunanyong/dm` |

瀚高与金仓走 PostgreSQL 协议，使用同一套方言与表结构；达梦使用 `MERGE` 做 upsert。
**同一套数据库上只能运行一个 Server 实例**（两个实例会重复执行告警评估与拨测）。

---

## 2. 建库与建账号

Server 启动时会自动建表（含索引与注释），因此账号需要 **DDL 权限**。

### MySQL 8.0+

```sql
CREATE DATABASE yingzhao DEFAULT CHARSET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
CREATE USER 'yingzhao'@'%' IDENTIFIED BY '<password>';
GRANT ALL PRIVILEGES ON yingzhao.* TO 'yingzhao'@'%';
FLUSH PRIVILEGES;
```

### PostgreSQL / 瀚高 / 金仓

```sql
CREATE USER yingzhao WITH PASSWORD '<password>';
CREATE DATABASE yingzhao OWNER yingzhao;
```

账号是库的 owner 即可建表。瀚高/金仓若使用 Oracle 兼容模式，请关闭
（会改变空串与 NULL 语义），并按 §7 的清单验收。

### 达梦 DM8

达梦的「用户」即 schema。用 SYSDBA 建用户并授予 `RESOURCE` 角色：

```sql
CREATE USER YINGZHAO IDENTIFIED BY "<password>";
GRANT RESOURCE, PUBLIC TO YINGZHAO;
GRANT CREATE TABLE, CREATE VIEW, CREATE INDEX TO YINGZHAO;
```

建库时建议 `UNICODE_FLAG=1`（UTF-8）。不要用带双引号的小写对象名——
Server 的 DDL 对 `key`/`schema`/`rows` 统一加双引号，其余标识符按达梦默认大写处理。

---

## 3. 连接配置

```yaml
database:
  driver: mysql          # mysql | postgres | highgo | kingbase | dm
  dsn: "..."             # 各驱动自己的 DSN 格式
  maxOpenConns: 20
  maxIdleConns: 5
  connMaxLifetimeSeconds: 1800
  masterKeyFile: /etc/monitor-server/master.key
```

DSN 示例：

```yaml
# MySQL
dsn: "yingzhao:<password>@tcp(127.0.0.1:3306)/yingzhao?charset=utf8mb4"

# PostgreSQL / 瀚高 / 金仓（key=value 或 URL 均可）
dsn: "host=127.0.0.1 port=5432 user=yingzhao password=<password> dbname=yingzhao sslmode=disable"

# 达梦
dsn: "dm://YINGZHAO:<password>@127.0.0.1:5236?schema=YINGZHAO"
```

- 密码含特殊字符时，MySQL 用 URL 转义；达梦在 DSN 末尾加 `&escapeProcess=true`。
- 日志与错误信息里的 DSN 会自动脱敏（只显示主机/库），不会打印密码。

安装脚本可直接生成这段配置：

```bash
sudo bash deploy/install-server.sh --yes \
  --tsdb-addr http://127.0.0.1:8428 --log-addr http://127.0.0.1:9428 \
  --db-driver postgres \
  --db-dsn 'host=127.0.0.1 port=5432 user=yingzhao password=... dbname=yingzhao sslmode=disable'
```

---

## 4. 首次启动与升级

**首次启动**：Server 自动建表、建索引、写表/列注释，并播种内置资产类型与告警规则。
不需要手工执行任何 SQL。

**升级顺序**（单实例）：

```bash
# 1. 备份（见 §5）
# 2. 先用新二进制把库升到最新结构（不启动服务）
sudo /usr/local/bin/monitor-server.new migrate -config /etc/monitor-server/server.yaml
# 3. 停服、替换二进制、启动
sudo systemctl stop monitor-server
sudo install -m 0755 /usr/local/bin/monitor-server.new /usr/local/bin/monitor-server
sudo systemctl start monitor-server
```

- 迁移由数据库侧命名锁保护（MySQL `GET_LOCK`、PostgreSQL advisory lock、达梦控制表行锁），
  重复执行是幂等的；已应用迁移的 SQL 被改动会直接报错。
- 迁移失败**不会**带着半套结构启动：进程退出并打印失败版本。
- 资产表的结构升级走版本化 `ALTER`（`assetMigrations`），与核心表迁移分开，
  启动时按 `schema_version` 自动补齐。

---

## 5. 备份清单（务必完整）

| 内容 | 位置 | 丢失后果 |
|---|---|---|
| **数据库** | 整个库 | 配置、资产、审计、节点、安全事件全部丢失 |
| **主密钥** | `/etc/monitor-server/master.key` | AI Key / 通知口令等加密配置**无法解密** |
| **审计链密钥** | `/etc/monitor-server/audit.key` | 历史审计**全部无法校验** |
| `server.yaml` | `/etc/monitor-server/server.yaml` | 启动配置（含 DSN）丢失 |
| 报告 / 升级包 / 下发文件 / 地理库 | `dataDir` 下 `reports/`、`upgrades/`、`ops_files/`、`geoip/` | 历史报告与文件分发内容丢失（非状态，可重建） |

两个密钥文件都**不在数据库里**，必须与数据库备份分开保存：

```bash
# 逻辑备份（示例）
mysqldump --single-transaction --routines yingzhao > yingzhao-$(date +%F).sql
pg_dump -Fc yingzhao > yingzhao-$(date +%F).dump
# 达梦用 dexp 或存储级快照

sudo tar -czf yingzhao-keys-$(date +%F).tar.gz \
  /etc/monitor-server/master.key /etc/monitor-server/audit.key /etc/monitor-server/server.yaml
```

恢复顺序：先恢复库，再恢复两个密钥文件，最后启动 Server。
密钥丢失后重新生成会导致**已加密的配置无法解密**（AI/通知配置需重新填写）。

---

## 6. 数据库故障时的行为

| 数据 | DB 不可用时 |
|---|---|
| 指标、日志、告警评估 | **不受影响**（VictoriaMetrics / VictoriaLogs） |
| 节点注册与心跳 | 内存继续更新，恢复后 30 秒内补写 |
| 安全事件/基线/防护状态 | 内存继续更新，恢复后补写（上限 2000 条事件） |
| 防护任务 / 下行任务 | 下发与回执照常，落库失败记日志 |
| 资产台账 | 连续 3 次写失败后暂停 30 秒，避免拖慢上报 |
| 配置保存 / 审计写入 | 返回错误或记日志，不会假装成功 |

- `/readyz` 会探测数据库，失败返回 503。
- **DB 宕机期间重启 Server 会启动失败**；内存里未落库的状态（最近 30 秒的心跳、
  安全事件）会丢失。
- 恢复后连接池自动重连，下一次 flush 把内存状态写回。

---

## 7. 各库差异与验收清单

| 项目 | MySQL | PostgreSQL / 瀚高 / 金仓 | 达梦 |
|---|---|---|---|
| 标识符引用 | 反引号 | 双引号 | 双引号（仅 `key`/`schema`/`rows`） |
| upsert | `ON DUPLICATE KEY UPDATE` | `ON CONFLICT ... DO UPDATE` | `MERGE INTO` |
| 自增主键 | `AUTO_INCREMENT` | `BIGSERIAL` | `IDENTITY(1,1)` |
| 长文本 | `LONGTEXT` | `TEXT` | `CLOB` |
| 布尔 | `TINYINT` | `SMALLINT` | `TINYINT` |
| 迁移锁 | `GET_LOCK` | `pg_advisory_lock` | 控制表行锁 |
| 大小写折叠 | 显式 `LOWER()` | 显式 `LOWER()` | 显式 `LOWER()` |

**瀚高 / 金仓现场验收**：

1. `lib/pq` 能直连（默认端口、非 Oracle 兼容模式）；
2. 首次启动自动建表成功（25 张资产/审计/协作表 + 3 张核心表）；
3. 节点上报后 `nodes.last_seen` 在 30 秒内前进；
4. 资产台账、审计列表、审批配置页面能正常读写；
5. 重启后数据完整（分组、显示名、审批开关、安全事件）。

**达梦**：已在 DM8 容器上跑通六组集成用例与完整启动，现场仍建议按上面 5 条复核。

---

## 8. 排障

| 现象 | 排查 |
|---|---|
| 启动即退出，日志 `初始化状态库失败` | DSN、账号权限、网络；错误信息里的 DSN 已脱敏 |
| `database.dsn 未配置` | `server.yaml` 缺 `database` 段（老配置升级时安装脚本会自动补） |
| `/readyz` 返回 503 且 `checks.database.ok=false` | 数据库不可达或账号被锁 |
| `迁移失败：已应用迁移的 SQL 被修改` | 不要改已发布的迁移文件；新增一个版本 |
| 中文乱码（MySQL） | 库/表/连接都应为 `utf8mb4` |
| 达梦 `无效的列名` | 标识符大小写不一致：DDL 与 DML 必须同样加引号 |
