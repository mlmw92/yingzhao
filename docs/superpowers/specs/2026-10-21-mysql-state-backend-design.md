# MySQL 作为唯一状态后端（取消文件后端）设计

- 日期：2026-10-21
- 状态：待评审
- 前置：A1 Server 高可用设计（`docs/designs/a1-server-ha.md`）、ADR-0002 日志后端抽象
- 决策来源：用户明确要求**不区分单机与高可用**，所有配置与状态一律存 MySQL；范围包含配置、协作状态、资产台账与审计
- 影响：本文替代 A1 §3 选项 B 中「低频配置用共享目录」的部分

---

## 1. 目标与非目标

**目标**

1. **单一状态后端**：Server 不再把配置/协作状态/资产/审计写在本地文件或 SQLite；全部进 MySQL。
2. **Server 无状态**：任意实例重启、被替换都不影响数据；**数据不随 Server 进程走**（备份、迁移、换机都只需搬数据库）。
3. **可迁移**：现有部署首次启动时把文件/SQLite 里的数据一次性导入 MySQL，迁移可重复执行且可回滚（迁移前备份）。
4. **默认可用**：没有 MySQL 就启动失败并给出明确错误，不静默降级成文件模式（避免"以为在用 DB，实际写本地"）。

**非目标**

- 日志不进 MySQL：**统一走 VictoriaLogs**（本阶段即切换；本地日志后端不再作为生产选项）。
- 指标不进 MySQL：仍走 VictoriaMetrics。
- **二进制产物（巡检报告、升级包、备份）保持本地存储**：它们不是"状态"，也不需要跨实例共享。
- **Server 升级流程（apply/rollback）保持现状**：不需要多实例串行化。
- **不做多实例 / 主备切换（阶段 7 搁置）**：2026-10-21 决定不部署多实例，因此
  「实例身份 + 租约选举 + 调度门控 + 跨实例可见性」整块不做。
  **部署约束：同一套数据库上只能运行一个 Server 实例**——两个实例会重复执行告警评估与拨测。
- 不自研数据库高可用：MySQL 的主从/托管由运维负责；平台只要求一个可用 DSN。
- 不做分库分表：单集群数百节点的数据量远未到需要分片的程度。

---

## 2. 状态清单与落表映射

A1 §2.2 盘点过的本地状态，逐类映射：

| 现有载体 | 内容 | 落表 |
|---|---|---|
| `notify.yaml`、`ui.yaml`、`screen.yaml`、`dashboards.yaml`、`alert_pipeline.yaml`、`alert_inhibit.yaml`、`alert_grouping.yaml`、`maintenance.yaml`、`retention.yaml` | 整份读写的配置文档 | `config_documents(namespace, version, payload, updated_at)` |
| `rules.yaml` | 告警规则（按 ID 增删改） | `alert_rules` |
| `users.yaml` | 用户、角色、权限、范围 | `users` / `roles` / `user_roles` |
| `nodes.json` | 节点、分组、最近上报快照 | `nodes` / `node_groups`；快照列单独存 JSON |
| `alert_acks.json` | 认领/关闭/评论 | `alert_acks` / `alert_ack_comments` |
| `approvals.json` | 审批单与审批链 | `approvals` |
| `dialtest.yaml` | 拨测任务与状态 | `dialtest_tasks` |
| `templates.yaml` | 通知模板 | `notify_templates` |
| `ops_tasks.json` | 下行任务 | `ops_tasks` |
| `investigations.json` | AI 调查记录 | `ai_investigations` |
| `inspect_schedule.yaml`、`report_schedule.yaml` | 巡检/报告调度 | `config_documents`（整份） |
| `security_store.json`、`defense_tasks.json` | 安全事件基线、处置任务 | `security_baselines` / `defense_tasks` |
| `assets.db`（SQLite） | 资产台账、关系、快照、巡检结果 | 现有表结构迁到 MySQL（见 §4.3） |
| `audit_events`（SQLite） | 审计事件与哈希链 | `audit_events` / `audit_chain` / `audit_pruned`（见 §4.4） |
| `ai.yaml` | 模型配置（含 API Key） | `config_documents` 的 `ai` 命名空间；密钥字段加密（见 §6） |
| `server.yaml` | 启动配置（监听、DSN、密钥主密钥） | **保留本地文件**：它是引导配置，不能依赖数据库 |
| 地理库 xdb、升级包、备份、巡检报告与附件 | 静态文件/二进制 | **下一阶段解决**：本阶段保持现状（本地/共享存储），不纳入 HA；与 Server 升级流程一起处理。二进制大对象进数据库会把备份、复制与查询都拖慢，不采用 |
| 集中日志分片 | 日志 | 不进 MySQL，走 VictoriaLogs |

---

## 3. 配置文档表

整份读写的配置统一用一张表，避免为 10 多个 store 各写一套 schema：

```sql
CREATE TABLE config_documents (
  namespace   VARCHAR(64)  NOT NULL,
  version     BIGINT       NOT NULL DEFAULT 1,
  payload     JSON         NOT NULL,
  updated_at  BIGINT       NOT NULL,   -- epoch ms
  updated_by  VARCHAR(128) NOT NULL DEFAULT '',
  PRIMARY KEY (namespace)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

- 读写语义：`SELECT ... FOR UPDATE` → 修改 → `version = version + 1` → 提交。版本号用于缓存刷新与并发写检测。
- 写入时校验仍由各 store 自己的 `Normalize/Validate` 完成；数据库只负责持久化。
- 不使用 MySQL 的 `JSON` 路径查询：配置是整份读写，不需要在库里检索。

## 4. 类型表

### 4.1 通用约定

- 时间统一 `BIGINT` epoch 毫秒（与现有模型一致，不引入 `DATETIME` 时区问题）。
- 主键用业务 ID（字符串）或 `BIGINT AUTO_INCREMENT`；业务 ID 加唯一索引。
- 所有表 `ENGINE=InnoDB`、`utf8mb4`；金额/计数用 `BIGINT`。
- 软删除只在业务确实需要时使用；默认物理删除。
- 每张表带 `updated_at`，便于排障与增量同步。

### 4.2 协作状态

- `alert_rules(id PK, name, type, expr, metric, operator, threshold, for_seconds, severity, scope, nodes JSON, enabled, silenced, ...)`。
- `users(username PK, display_name, password_hash, status, scope JSON, created_at, updated_at)`；`roles(name PK, permissions JSON, scope_mode, scope_groups JSON, asset_mode, asset_labels JSON)`；`user_roles(username, role_name, PK(username, role_name))`。
- `nodes(hostname PK, group_name, ip, os, arch, mode, status, last_seen, labels JSON, snapshot JSON)`；`node_groups(name PK, title)`。
- `alert_acks(rule_name, node, instance, starts_at, status, assignee, close_reason, updated_at, PK(rule_name,node,instance,starts_at))`；评论单独一张表（自增 ID）。
- `approvals(id PK, kind, ops_kind, node, nodes JSON, params JSON, extra JSON, title, state, requester, requester_ip, reason, created_at, expire_at, decided_by, decided_at, decision, executed_at, task_id, message)`。
- `dialtest_tasks(id PK, ...)`；`notify_templates(name PK, ...)`；`ops_tasks(id PK, node, kind, params JSON, state, operator, operator_ip, reason, created_at, expire_at, ...)`。
- `ai_investigations(id PK, alert_id, rule_id, node, state, stopped, protocol, answer MEDIUMTEXT, steps JSON, suggestions JSON, tokens, rounds, error, started_at, ended_at)`；按 `alert_id` 建索引。

### 4.3 资产台账

把 `internal/server/asset/store.go` 的 SQLite 表结构原样迁到 MySQL：

- `asset_types`、`assets`、`asset_attrs`、`asset_links`、`asset_link_suppressions`、`asset_changes`、`snapshots`、`snapshot_fields`、`asset_seen`、`inspect_runs`、`inspect_run_manifest`、`inspect_run_members`、`inspect_findings`、`inspect_finding_baselines`。
- 方言差异：`INTEGER PRIMARY KEY AUTOINCREMENT` → `BIGINT AUTO_INCREMENT`；`TEXT` → `VARCHAR/TEXT`；部分索引（`WHERE request_id <> ''`）MySQL 不支持 → 改为普通索引；`INSERT OR REPLACE` → `ON DUPLICATE KEY UPDATE`。
- 资产查询已有 SQL，迁移时逐条过一遍方言与执行计划；`asset_changes.request_id` 的索引保留。

### 4.4 审计

表结构沿用现有 SQLite 设计，只做方言迁移：

- `audit_events(id BIGINT AUTO_INCREMENT, time_ms, user_name, method, path, status, remote_ip, source_loc, succeeded, request_id, category, action, detail, seq, prev_hash, hash, key_fp)`，索引：时间、用户、request_id、seq。
- `audit_chain(id=1, seq, hash, key_fingerprint, updated_ms)`。
- `audit_pruned(from_seq, to_seq, head_hash, rows, pruned_at, mac)`：裁剪区间的锚点，保留现有语义。

**并发与完整性（MySQL 特有，必须补）**

1. **链头行锁**：`appendChained` 在事务里先 `SELECT seq, hash, key_fingerprint FROM audit_chain WHERE id=1 FOR UPDATE`。SQLite 靠单写者串行，MySQL 多实例必须显式加锁，否则两个实例会算出同一个 `seq`，链分叉。
2. **首次初始化**：迁移时插入 `audit_chain(id=1, seq=0, hash='', ...)`，避免第一次并发写时都走 `ErrNoRows` 分支。
3. **方言**：`ON CONFLICT ... DO UPDATE` → MySQL `ON DUPLICATE KEY UPDATE`。
4. **追加写权限**：应用连接只授予 `audit_events` 的 `SELECT/INSERT`、`audit_chain` 的 `SELECT/UPDATE`；裁剪需要 `DELETE`，走**独立的维护连接**（见 §5.3），请求路径拿不到删除权限。这样即使应用被攻破，也改不了历史审计。
5. **删除 JSON 降级模式**：现有「台账库打不开时审计退回 JSON 文件」的路径在强制 DB 后不再成立。DB 不可用时 `Record` 返回错误，调用方按现有语义**不阻断业务**，但错误要进自监控；不静默写本地文件。

---

## 5. 迁移、缓存与并发

### 5.1 Schema 迁移

- `schema_migrations(version INT PK, checksum VARCHAR(64), applied_at BIGINT)`。
- 迁移文件按版本号顺序执行，启动时先取 MySQL 命名锁 `GET_LOCK('yingzhao_schema_migrate', 30)`，执行完 `RELEASE_LOCK`；多实例同时启动只有一个在迁移，其余等待。
- 迁移失败**启动失败**并打印失败的版本与 SQL 摘要；不静默跳过。
- 提供 `monitor-server migrate` 子命令（只迁移、不启动服务），便于发布流程先把库升好。
- **管理员引导**：首次迁移把现有 `users.yaml`（或 `server.yaml` 的单用户 `auth` 段）导入 `users`/`roles`；之后运行时只认数据库，`server.yaml` 的 `auth` 段仅用于首次引导。

### 5.2 缓存与刷新

- 启动时把配置文档、规则、用户/角色、节点注册信息等加载进内存；请求路径不查库（保持现有性能特征）。
- 单实例部署下**不需要**跨实例刷新：内存与数据库只有本进程在写，改配置走 Web 接口时同步更新内存。
- **（已取消）** 原计划的 `state_version` 版本行 + 5 秒轮询随阶段 7 一并取消——
  它是为多实例缓存一致性设计的，没有多实例就没有这项需求。

### 5.2.1 高频状态：节点心跳与快照

`nodes` 里有两种数据，写入频率差三个数量级，必须分开：

- **注册信息**（hostname、group、labels、mode）：低频，进 `nodes` 表，写入时 bump `state_version`。
- **心跳/状态/进程与监听快照**：每次上报都变。**不逐次写 MySQL**，保持内存一份；每 30 秒把 `last_seen/status` 批量 upsert 一次，且**不 bump** `state_version`。

这样节点上报不会把数据库和缓存刷新打成热路径。

> **注**：`state_version` 与「按版本号热重载」随阶段 7 一并取消（见 §1 非目标）。
> 单实例部署下内存与数据库只有本进程在写，不存在跨实例的缓存不一致。

### 5.3 写所有权与维护连接

- **单实例部署**：所有写都来自本进程，不存在写冲突。
- 维护类操作（审计裁剪、数据保留清理）与请求路径共用连接池；若将来需要更严格的权限分离，
  可给维护操作单独 DSN（本阶段未做）。
- 连接池：`SetMaxOpenConns` / `SetMaxIdleConns` / `SetConnMaxLifetime` 显式配置；所有查询带 context 超时。

### 5.4 启动与就绪

- 启动时 `Ping` + 迁移 + 加载缓存；失败即退出（非 0），日志给出 DSN 的主机/库名（**不打印密码**）。
- `/readyz` 增加数据库探活；DB 不可用时返回 503，负载均衡据此摘除实例。
- DB 故障期间：读接口可继续用内存缓存（降级但可用）；写接口返回 503；审计写入失败按现有语义记录错误，不阻断业务（但要在自监控里暴露）。

---

## 6. 密钥

配置进 DB 后，以下内容不能明文落库：

- AI API Key、通知渠道口令/签名密钥、登录 token 签名 secret、Agent 接入 secret、审计链密钥。

方案：

- `server.yaml` 增加 `masterKeyFile`（或环境变量 `YINGZHAO_MASTER_KEY`），保存 32 字节主密钥；两台实例部署同一份。
- 敏感字段用 AES-256-GCM 加密后存 `config_documents.payload` 的密文字段（带 `key_id` 前缀，便于轮换）。
- 审计链密钥仍单独管理（现有 `AuditKeyFile` 的取向不变）：可存加密后的副本，但校验时必须能解出；主密钥丢失 = 审计链不可验，需要在文档里写清备份要求。
- 迁移工具导入现有明文配置时，顺带加密；导入后旧文件里的密钥应删除或轮换。

---

## 7. 测试策略

- **契约用例**：配置/协作状态 store 定义接口，同一套断言跑「文件测试替身」与「MySQL 实现」。文件实现**只用于测试**，生产不可选。
- **MySQL 集成**：`TEST_MYSQL_DSN` 存在时运行；CI 用 MySQL 8 服务容器。覆盖迁移、锁、链并发（两个连接同时 append，seq 必须连续且不重）、缓存刷新。
- **迁移测试**：从一份代表性的文件/SQLite 快照导入，逐类比对行数与关键字段；重复执行幂等。
- **回归底线**：现有 API 契约不变；前端不需要改（除错误提示文案）。
- **故障演练**：迁移中途杀进程 → 重启后从断点继续/回滚；DB 断开 → 读降级、写 503、恢复后自动重连。

---

## 8. 部署与发布

- `install-server.sh` 增加 `--db-dsn`（或交互式填写），写入 `server.yaml` 的 `database` 段；不再生成 `users.yaml`、`nodes.json` 等文件。
- 日志：`install-logs.sh` 成为必选步骤；`server.yaml` 移除 `logBackend: local` 选项，只保留 `logVictoriaLogs.addr`。
- 离线包不内置 MySQL；文档要求现场提供 MySQL 8.0+（或托管实例），并给出建库/建账号 SQL。
- 发布流程：先 `monitor-server migrate`，再停服替换二进制并重启（**单实例**，不需要滚动升级）。
- 备份：由 MySQL 侧负责（逻辑备份/快照）；平台文档给出「迁移前备份 + 恢复」步骤。

---

## 9. 实施阶段

| 阶段 | 内容 | 可独立交付 |
|---|---|---|
| 0 | 持久化接口 + MySQL 适配器 + `schema_migrations` + 迁移锁 + 文件测试替身 | 是（纯基础设施） |
| 1 | 配置文档入库（先 notify/ui/screen 试点，再覆盖其余） | 是 |
| 2 | 协作状态入库（rules → users/roles → nodes → acks → approvals → dialtest/templates/ops/investigations） | 是 |
| 3 | 资产台账 SQLite → MySQL | 是 |
| 4 | 审计 SQLite → MySQL（链头行锁、维护连接、裁剪） | 是 |
| 5 | 删除生产路径上的文件后端；保留一次性迁移读取器 | 是 |
| 6 | 日志统一切 VictoriaLogs（移除生产配置里的 `local` 选项；本地实现只留测试替身） | 是 |
| 7 | ~~主备切换与跨实例可见性~~ | **不做**（2026-10-21 决定不部署多实例） |
| — | ~~巡检报告/升级包/备份的共享存储、Server 升级串行化~~ | **不做**（随阶段 7 一并取消） |

### 9.1 完成后的状态

**已具备**

- 结构化状态（配置 / 协作 / 资产 / 审计）在 MySQL：Server 进程重启、换机、重装都不影响数据；
  备份与迁移只需处理一个数据库。
- 日志在 VictoriaLogs：Server 侧不持有日志状态，检索走 VL 的倒排索引。
- 节点注册与心跳在 MySQL：重启后分组、显示名、最近心跳都还在。

**明确的边界**

- **同一套数据库上只能跑一个 Server 实例**：两个实例会重复执行告警评估与拨测，产生重复通知。
  这是放弃阶段 7 的直接代价，部署文档与安装脚本都按单实例写。
- 巡检报告、升级包、备份仍是本地文件——单实例下不存在「备机看不到」的问题，保持现状即可。
- Server 在线升级保持现状（停服替换二进制 + 重启）。

---

## 10. 风险与边界

| 风险 | 缓解 |
|---|---|
| DB 成为硬依赖，DB 故障 = 平台不可写 | 明确写入文档；`/readyz` 摘除；DB 自身做主从/托管；读路径内存降级 |
| 审计链并发分叉 | 链头 `FOR UPDATE` + 首次初始化 + 并发用例 |
| 迁移把现有数据写坏 | 迁移前强制备份；导入幂等；`migrate --dry-run` 先比对行数 |
| 方言差异（JSON、部分索引、upsert、自增） | 只支持 MySQL 8.0+；DDL 集中在迁移文件；集成测试跑真实 MySQL |
| 密钥随库扩散 | 主密钥独立于 DB；敏感字段加密；迁移后轮换旧密钥 |
| 现有单机部署升级成本 | 提供迁移命令与文档；升级前备份；不迁移则拒绝启动并提示 |

**明确不做**：MySQL 自身的高可用、跨地域复制、分库分表、把日志/指标塞进 MySQL。

---

## 11. 待评审的决策点

| # | 问题 | 建议 |
|---|---|---|
| D1 | MySQL 版本 | **8.0+**（JSON、`FOR UPDATE`、窗口函数）；5.7 不承诺 |
| D2 | 单写还是多写 | **单实例单写**（多实例不做，见 §1 非目标） |
| D3 | 审计裁剪 | 保留现有「裁剪区间 + MAC 锚点」语义；裁剪走维护连接 |
| D4 | 密钥主密钥位置 | `server.yaml` 的 `masterKeyFile`（0600，首次启动自动生成） |
| D5 | 迁移时机 | 启动时自动迁移（带命名锁），同时提供 `migrate` 子命令供发布流程使用 |
| D6 | 文件后端 | 生产路径删除；文件实现只作为测试替身保留 |
| D7 | 日志后端 | **已定：统一 VictoriaLogs**。生产配置移除 `local`；本地实现仅保留为契约测试替身 |
| D8 | 二进制产物与 Server 升级 | **已定：下一阶段**。本阶段保持现状，不纳入 HA 验收 |

---

## 12. 阶段 0 实施记录（2026-10-21）

**已落地**

- `internal/server/db`：连接池、DSN 归一化（驱动 `ParseDSN/FormatDSN`）、日志脱敏。
- `internal/server/migrate`：版本化迁移、内嵌 SQL、`schema_migrations`、`GET_LOCK` 命名锁、校验和比对。
- `internal/server/configdoc`：整份配置文档的 `Store` 接口、文件测试替身、MySQL 实现（`FOR UPDATE` 版本冲突）。
- 首条迁移 `0001_config_documents.sql`。
- `server.yaml` 的 `database` 段（阶段 0 仅解析，不连接）。
- `monitor-server migrate` 子命令。

**验证**

- `go test ./...`：38 个包全绿（集成用例在无 DSN 时 SKIP）。
- MySQL 8.0.46 集成：连接、迁移幂等、已应用迁移被修改报错、并发迁移串行化、配置文档文件/MySQL 契约全部通过。
- `migrate` 子命令在全新库上首次应用 1 条、再次 0 条。
- 现有 Server 启动路径未改变：不读 `database.dsn`、不连库、不迁移任何 store。

**实施中的两处修正**

1. `timeout` 是驱动 `mysql.Config` 的专用字段，不在 `Params` 里；DSN 归一化用 `ParseDSN/FormatDSN`，测试按字段断言。
2. MySQL 的 `JSON` 列会规范化空白（`{"a":1}` → `{"a": 1}`）；配置文档契约用例按语义比较，不按字节比较。

**测试环境**：dev-server 上的 MySQL 8.0.46（容器 `mw-mysql`），独立测试库 `yingzhao_test` 与账号；凭据记录在服务器 `/root/.yingzhao-test-mysql.cnf`（0600），不进仓库。集成测试通过 SSH 隧道 `127.0.0.1:13306` 访问。

---

## 13. 阶段 1 实施记录（2026-10-21）

**已落地**

- 新增 `configdoc.Persister`（原始字节读写）与两种实现：`FilePersister`（测试/迁移源）、`DBPersister`（`config_documents` 表，JSON 信封 + 可选 AES-256-GCM 加密）。
- 13 个整份读写的配置 store 全部改走该接口：ui / screen / notify / dashboards / ai / alert_pipeline / alert_inhibit / alert_grouping / maintenance / retention / middleware_view / report_schedule / inspect_schedule。
- 主密钥独立于数据库：`database.masterKeyFile`（0600，首次启动自动生成）。
- `cmd/server/main.go` 启动时连库并执行迁移；数据库成为硬依赖。新增 `cmd/server/state.go` 集中管理命名空间与连接。
- 敏感配置（ai / notify）加密入库；其余为 base64 明文。

**为什么用「原始字节 + 信封」而不是结构化 JSON**

现有 store 各自用 YAML 序列化，直接换成 JSON 结构体要逐字段补 tag，且有丢字段风险。持久化层只搬字节，序列化格式留在 store 内，迁移时也能原样搬运文件内容。

**dev-server 实机（1.36.0）**

- 一次性迁移工具（`dist/migrate-config`，**不入库**）把 `/etc/monitor-server` 与 `/var/lib/monitor-server` 下 9 份现有配置导入 `yingzhao` 库：导入 9、跳过 4（文件不存在）、失败 0。
- 启动日志确认 `db:notify` / `db:ai` / `db:screen` 等从数据库加载；AI 配置（加密）解密正常、`hasApiKey=true`。
- 写路径验证：`PUT /api/v1/ui/settings` 后 `config_documents` 版本 1 → 2，重启后配置仍在。
- 其余配置接口（notify / retention / screen）读取正常。

**未覆盖**

- 其他状态（规则、用户、审批、资产、审计）仍在本阶段之前的存储里；属于阶段 2–4。
- 迁移工具没有写进仓库（测试环境一次性使用），生产迁移路径在阶段 5 统一处理。

---

## 14. 阶段 2 实施记录（2026-10-21）

**已落地**

- 协作状态接入 `configdoc.Persister`：告警规则、拨测任务、审批单、AI 调查记录、用户/角色。
- 文档格式按内容区分：规则/拨测/用户为 YAML，审批单与 AI 调查为 JSON；用户文档加密。
- 数据库模式下告警规则的播种按「规则名」判重，删除的内置规则是否回归依赖本地标记文件（多实例下按实例各自判断，属已知小缺口）。
- 一次性迁移工具新增 5 个命名空间，导入 5、跳过 13、失败 0。

**dev-server 实机（1.37.0）**

- 启动后规则 46 条、用户 1 个、审批单 16 条、拨测任务 1 条，均可正常读取。
- 写路径：新建拨测任务、新建用户、新建/删除规则，重启后数据仍在（`dialtest` 版本 3、`users` 版本 11、`alert_rules` 版本 5）。
- 测试数据已删除（`dbcheck` 用户、`db-roundtrip` 任务、`dbcheck-rule` 规则）。

**已知代价（后续优化）**

- `ai_investigations` 是整份文档（当前约 6 MB），每次追加调查都会重写整篇。调查频率低，测试环境可接受；量级上去后应改成按记录的表。
- 告警确认、下行任务、资产台账、审计仍在 SQLite（`assets.db`），属于阶段 3/4。
- 节点注册信息仍在 `nodes.json` 且每次上报都会写盘，属于阶段 3。

---

## 15. 阶段 3 实施记录（2026-10-21）

**已落地**

- 新增 `internal/server/sqlx`：统一处理 SQLite / MySQL 差异——upsert 子句、`key` 保留字加反引号、`COLLATE NOCASE`、`FROM DUAL`、以及 `IN (子查询 LIMIT)` 的派生表写法。
- `asset.OpenMySQL` 用共享 `*sql.DB` 初始化 24 张表（MySQL 方言），版本记在 `schema_version`。
- 告警处置、下行任务、审计通过 `UseSQL(db, true)` 接入同一连接。
- 审计链头在 MySQL 下 `SELECT ... FOR UPDATE`；裁剪条件改为派生表，避免 MySQL 不支持 `IN (... LIMIT)`。
- 审计校验报告显示 `mode: mysql`。

**dev-server 实机（1.38.0）**

- 一次性迁移工具（`dist/migrate-assets`，不入库）从 `assets.db` 导入 **14694 行**：资产 65、属性 424、关系 64、变更 456、快照 87/397、巡检 21/20/406/44、审计 8485、处置 6127、下行任务 76。
- 启动后资产台账 200、审计链头 seq 3057 连续、处置与下行任务均从 MySQL 读取。
- 接口验证：资产列表 / 摘要、审计列表 / 校验（3074 行）、下行任务列表均 200。
- 修复两处 MySQL 方言问题：`IN (子查询 LIMIT)`（审计裁剪、资产分页）、审计列宽（action 超过 64）。

**未覆盖**

- 阶段 5（节点注册信息与心跳）尚未迁移；`nodes.json` 仍在本地。
- 阶段 6（日志统一 VictoriaLogs）、阶段 7（主备切换）未开始。
- 二进制产物（巡检报告、升级包、备份）与 Server 升级串行化按约定留到下一阶段。

---

## 16. 阶段 5 实施记录（2026-10-21）

**已落地**

- 节点包新增 `Persist` 抽象：`filePersist`（测试/迁移源，整份写，行为与之前一致）与 `mysqlPersist`（生产）。
- **注册信息**（主机名、分组、显示名、标签、模式、版本、主机信息、日志来源）只在真的变化时落库；
  **心跳**（last_seen/status）由 `RunHeartbeatFlusher` 每 30 秒批量刷新，不触碰注册字段。
- `last_seen` 写入用 `GREATEST`，多实例下不会把心跳写回旧值。
- 整份同步：内存即真相，upsert 全部节点/分组后删除不在列表里的行（删除节点后重启不会复活）。
- 新增迁移 `0002_nodes.sql`、`0003_node_groups.sql`。
- 一次性迁移工具（`dist/migrate-nodes`，**不入库**）把既有 `nodes.json` 导入状态库。

**dev-server 实机（1.39.0）**

- 导入节点 4 个、分组 3 个（国内/国外/default），接口读回分组与显示名正确。
- 35 秒窗口内 `last_seen` 前进约 33 秒，确认心跳节流落库生效。
- 注册信息未变化时不再整份重写节点表。

**未覆盖**

- `nodes.json` 不再被读取（迁移后），文件保留作备份。
- 阶段 6（日志统一 VictoriaLogs）、阶段 7（主备切换与跨实例可见性）未开始。

---

## 17. 阶段 6 实施记录（2026-10-21）

**已落地**

- `server.yaml` 删除 `logBackend` / `logDir` / `logMaxBytesPerDay`，只保留 `logVictoriaLogs.addr`。
- 启动时直接构造 VictoriaLogs 客户端；**地址缺失即启动失败**（不再静默关闭日志能力）。
- 本地分片实现（`logstore.Store`）退出生产路径，只作为契约测试的对照实现保留。
- 安装/发布脚本同步：`install-logs.sh` 成为 Server 部署的必选前置；`--log-backend` / `--log-dir` 保留解析但被忽略，升级时会清掉老配置里的这两项。
- 配置契约用例改为验证新形状，并保留「老配置含 logBackend/logDir 仍能解析」的兼容断言。

**dev-server 实机（1.40.0）**

- 启动 VictoriaLogs（`:9428`，`-retentionPeriod=7d`），Server 日志显示
  `集中日志已启用 backend=victorialogs addr=http://127.0.0.1:9428`。
- 制造一条 404 → 约 11 秒后出现在 VictoriaLogs（`_stream={node=...,source=nginx_access}`）。
- 平台接口：`/api/v1/logs` 返回该行、关键词检索命中、`/api/v1/logs/backend` 报告
  `fullTextIndex: true`、`/api/v1/system/retention` 的 `logsBackend=victorialogs`。

**未覆盖**

- 历史本地分片不会迁入 VL（换后端是换存储）；旧目录保留作备份，可人工清理。
- 阶段 7（主备切换与跨实例可见性）未开始。

---

## 18. 决定：不做多实例（2026-10-21）

**结论**：阶段 7（实例身份 + 租约选举 + 调度门控 + 跨实例可见性）**不做**；
随它一并取消的还有「巡检报告/升级包/备份的共享存储」与「Server 升级串行化」——
这两项的唯一动机就是多实例。

**理由**：不部署多实例。评估下来，主备切换的复杂度（选举、调度门控、缓存一致性、
跨实例广播）与它带来的收益（单机故障时自动接管）不成比例。

**仍然保留的收益**：阶段 0–6 的价值不依赖多实例——

- 状态在 MySQL：Server 重启、换机、重装都不丢数据，备份与迁移只需处理一个数据库；
- 日志在 VictoriaLogs：检索走倒排索引，不再受本地扫描预算限制；
- 节点注册与心跳入库：重启后分组、显示名、最近心跳都还在。

**新的硬约束**：**同一套数据库上只能运行一个 Server 实例**。两个实例会重复执行
告警评估与拨测，产生重复通知。安装脚本、部署文档与运维手册都按单实例写；
如果将来要做多实例，从本文件 §5.2 / §5.3 与 A1 设计件的批次 D/E 重新起步。

---

## 19. 阶段 2–6 代码审查与修复（2026-10-21）

**范围**：`650c31a..51fd81d`（阶段 2–6 的 9 个提交，39 个文件，+1817/−441）。
**方式**：独立审查者读净 diff 与本文档，逐条回溯源码；MySQL 语义在 8.0 方言规则下推演，
关键结论再用真实 MySQL（dev-server 的 `yingzhao_test` 库，SSH 隧道 `127.0.0.1:13306`）补集成用例验证。

### 19.1 已修复（每条都有先失败、后通过的用例）

| # | 问题 | 后果 | 修复 |
|---|---|---|---|
| C1 | `loadOpsFromDB` 用裸 `*sql.DB` 查 `ops_meta.key`，`key` 是保留字且错误被吞 | 每次重启 seq 归零，新任务 ID 与旧任务撞车并被 upsert 覆盖 | SQL 加反引号；只把 `ErrNoRows` 当「无序号」，其余错误返回 |
| C2 | 审计裁剪的 `INSERT/SELECT` 未引用 `rows`（MySQL 8 保留字） | 裁剪事务 1064，审计永远删不掉；裁剪锚点读不到 | `rows` 加反引号；`sqlx` 保留字表补 `rows` |
| C3 | 节点注册变化检测包含 `DiskUsed`/`OnlineUsers` | 每轮上报（15s）都触发全表 `SaveMeta`，且持写锁做事务 | 只比较注册级字段；易变值仍更新内存快照 |
| C4 | `install-server.sh` 不生成 `database` 段 | 新装 Server 启动即退出 | 新增 `--db-dsn`（交互/非交互），生成 `database` 块；升级时缺块自动补 |
| — | **审查中额外发现**：`SELECT/UPDATE asset_types ... schema` 未引用 `schema`（MySQL 8 保留字） | 资产类型读取与保存全部 1064 | `sqlx` 保留字表补 `schema`（`schema_version` 不受影响），并加表驱动用例 |
| I1 | ack/ops 回填在 `s.mysql` 赋值前执行 | 升级部署永远退回本地 JSON（阶段 3/5 实机验证因先跑迁移工具而掩盖） | 回填显式接收方言参数 |
| I2 | `LIKE ... ESCAPE '\'` 在 MySQL 下是未闭合字符串 | 中间件告警「影响面」接口 500 | 转义字符改为 `!`，两套方言通用 |
| I4 | 规则/拨测/审批/调查把「读失败」当「空库」 | 下一次保存用空集合覆盖库里的原文档 | 记录 `loadErr`、禁止写入；`main` 检查后拒绝启动 |
| I5 | 播种标记在本机文件，且路径随阶段 2 变化 | 重启/换机后用户删掉的内置规则复活 | 标记改存 `alert_rules_seed` 配置文档；「全新安装」以规则文档是否存在为准 |
| I6 | 版本冲突不重读、不重试 | 一次外部写入后该 namespace 永久只读（用户看到保存成功） | 冲突时重读版本重试一次；`isConflictError` 补 1205 |
| I7 | 资产库初始化失败时 ack/ops/audit 静默退回本地文件 | 状态写本地、换机即丢，且只有一条日志 | 数据库改为硬依赖，失败即退出 |
| I9 | `UPDATE` 的 `RowsAffected==0` 被当成「行不存在」 | 内容未变的保存返回 404 | DSN 启用 `clientFoundRows=true` |
| I10 | `/readyz` 不探数据库 | DB 挂掉仍返回 200 | 注入 `DBProbe`，2 秒超时 Ping，失败 503 |
| I12 | `audit_chain` 首次初始化未实现 | 并发写入者可能同时算出 seq=1（丢审计/死锁） | 切库时幂等插入链头行 |
| I13 | 裁剪与 `Record` 未互斥 | MySQL 连接池下可能删掉刚提交的审计行并漏记缺口 | 裁剪持与 `Record` 相同的写锁 |
| I15 | 心跳只 `UPDATE` | 首次注册写库失败后该节点永久缺席 | 改为 upsert 补行；退出时等心跳协程结束后再关库 |
| I16 | 阶段 6 的脚本/文案不干净 | 升级路径因缺 `logVictoriaLogs.addr` 起不来；卸载脚本仍教人改回 `local` | 升级时缺地址自动补；卸载/接口文案同步；VL 包改为必需（`SKIP_VICTORIALOGS=1` 显式跳过）；删除已失效的 `build/verify-central-logs.sh` |
| I3 | 大小写折叠查找依赖库默认 collation | DBA 用 `_bin` 时同一台机器被判成两台 | MySQL 下改用显式 `LOWER()`，不依赖排序规则 |
| I11 | 配置文档读写无超时 | DB 黑洞时保存请求永久挂起 | 加 10 秒超时 context |
| M8 | 播种逐条 `Create` | 启动时 N 次整份序列化 + N 次数据库往返 | 内存批量构造后一次写入；同毫秒 ID 加序号防碰撞 |
| M9 | 启动日志固定 `migrated=true` | 无法判断本次是否真的迁移 | 回传并打印实际应用条数 |
| M3 | 审批/调查的格式标签写成 yaml | 排障时按 yaml 手工解码会失败 | 新增 `plainJSON` 持久化工厂 |
| M4 | 审计写失败被静默丢弃 | DB 故障时审计缺失无人知晓 | 记录错误日志（每分钟限一条） |

### 19.2 新增测试

- `internal/server/sqlx/db_test.go`：保留字改写（`key`/`rows`/`schema`/`schema_version`）、upsert、`FROM DUAL`。
- `ops` / `alert` / `audit` 的 MySQL 集成用例：JSON 回填、序号跨重启、裁剪与校验、处置回填。
- `asset`：实例地址查找、`ESCAPE` 转义、类型模型重复保存、大小写折叠查找。
- `node`：易变主机信息不触发注册写；心跳补写缺失节点。
- `configdoc`：版本冲突自动重试。
- `api`：`/readyz` 数据库探活。
- 规则：读失败禁止写入、播种记录跨重启、播种只写一次。

### 19.3 已知缺口（未在本次处理）

- ~~**安全事件/防护任务与审批配置仍是本地文件**~~（`security_store.json`、`defense_tasks.json`、`approval.json`）
  → **已迁移（阶段 7）**：三者改存状态库文档 `security` / `defense_tasks` / `approval_config`；
  旧文件在首次启动时一次性导入并改名 `.bak-migrated`；读失败一律拒绝启动（审批开关
  不再可能静默回到默认关闭）。
- ~~**资产表结构没有版本化 `ALTER` 路径**~~ → **已补（阶段 8）**：`assetMigrations`
  按版本升序执行，存量库升级不再只跑 `CREATE TABLE IF NOT EXISTS`（那对已有表是 no-op）。
  新增列时：`schemaVersion` 加 1、三种方言改建表语句、同时追加一条同版本的 `assetMigration`。
- ~~**数据库调用尚未全面带 context 超时**~~ → **已补（阶段 8）**：`sqlx.WithTimeout`（10 秒）
  统一加在所有事务边界（`BeginTx`）与关键 `ExecContext` 上——审计写入/裁剪、
  资产写事务、告警处置、下行任务、节点心跳、安全存储。
- **`sqlx` 仍是正则改写**：只认识列出的保留字，且绕过 `sqlx` 的裸 `*sql.DB` 调用不会被覆盖。
  多数据库支持会把它替换为显式 Dialect 接口（见 §20）。

---

## 20. 阶段 7：安全事件 / 防护任务 / 审批配置入库（2026-10-22）

**动机**：`security_store.json`、`defense_tasks.json`、`approval.json` 是本文件 §2 落表清单里
唯一还在本机文件的三项。其中审批配置最危险——文件丢失会让审批开关回到默认**关闭**，
高风险操作变成无需审批即可执行（fail-open），而且没有任何地方会提示这件事。

**落地**

| 旧文件 | 状态库文档（命名空间） |
|---|---|
| `security_store.json`（安全事件 / 基线 / 防护状态 / 护栏风险） | `security` |
| `defense_tasks.json`（防护任务与节点能力） | `defense_tasks` |
| `approval.json`（审批开关与范围） | `approval_config` |

- **一次性导入**：文档不存在且旧文件存在时导入，随后把旧文件改名 `.bak-migrated`。
  重复启动不会重复导入（判据是文档是否存在）。
- **读失败即拒绝启动**：与规则 / 拨测 / 审批单一致——带着空集合继续跑，
  下一次保存就会用空集合覆盖库里的原文档。审批配置尤其不能静默回到默认值。
- **写入合并**：安全事件文档约 1.5MB，Agent 每次上报都整份重写会成为写热点。
  改为 `markDirty` + `RunFlusher`（10 秒合并一次，进程退出时补一次）；
  文件后端（测试用）保持同步写，既有测试语义不变。

**验证**

- 单测：三个存储的旧文件导入、导入后改名、重新加载读回、合并写入（`Ingest` 不立即落库、
  `Flush` 后落库）。
- dev-server 实机：`security_store.json`（约 1.5MB，2000 事件）、
  `defense_tasks.json`（19 条任务 + 4 条能力）、`approval.json`（`enabled=false`、
  `requireFor=["ops:write"]`）导入后由状态库接管，界面上的安全态势与审批开关与迁移前一致。

---

## 21. 阶段 8：资产表版本化升级与数据库超时（2026-10-22）

**资产表版本化升级**

- `assetMigrations` 是按版本升序执行的升级列表（`asset/schema_comments.go`），
  每条按方言给出 `ALTER` 语句；`migrateSQL` 对 `version > 0` 的存量库依次执行。
- v3 是把「补注释」从一次性特例改成机制的第一个使用者（MySQL 用
  `MODIFY COLUMN ... COMMENT`，新库的 `CREATE TABLE` 已带注释）。
- 约定（写在 `assetMigration` 的注释里）：加列时 `schemaVersion` 加 1、三种方言改建表语句、
  同时追加一条同版本的 `assetMigration`——只改建表语句的话，存量库永远不会加列。
- 回归用例：`TestAssetMySQLUpgradeFromV2` 把注释清空、版本降到 2，重开资产库后
  注释恢复、版本升到 3。

**数据库超时**

- `sqlx.WithTimeout`（10 秒）统一加在所有事务边界与关键单条语句上。
- 覆盖：审计写入（`Record`/`appendChained`）与裁剪、资产写事务（人工属性删除、
  快照、巡检记录）、告警处置写入与清理、下行任务保存与回填、节点整份同步与心跳、
  安全存储写入。
- 效果：DB 网络黑洞或锁等待时，请求与进程退出不再无限挂起。
