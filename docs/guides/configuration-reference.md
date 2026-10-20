# 配置说明

> 本文件由仓库根目录 `README.md` 的「配置说明」章节下沉而来（2026-10-16）：逐字段的配置参考属于
> **查阅型**内容，放在 `guides/` 与既有约定一致（该目录专放现役的横切文档：权限矩阵、RBAC、配置手册）。
> 上手流程与部署步骤见 [README](../../README.md)；可直接套用的模板见 [配置模板手册](configuration-cookbook.md)；
> 权限点含义见 [权限矩阵](permission-matrix.md)。


## Server（`server.yaml`）

| 字段 | 说明 |
|------|------|
| `mode` | `standalone` |
| `listen` | HTTP 监听地址，如 `:8080` |
| `tsdb.backend` | 时序库后端：`victoriametrics`(默认) / `mimir` / `cortex` / `thanos` / `prometheus` / `custom` |
| `tsdb.addr` | 时序库基址，如 `http://10.0.0.10:8428` |
| `tsdb.queryAddr` | 可选：查询基址（与写入端口不同时，如 Thanos/Cortex） |
| `alert` | 告警引擎（enabled / rulesFile / evalInterval） |
| `notify` | 邮件 / Webhook / 钉钉 / 飞书 / 企业微信渠道（**仅首次初始化用**；运行时以独立 `notifyFile` 为准，可在 Web 后台「通知配置」页修改） |
| `notifyFile` | 通知配置独立文件路径（默认 `/etc/monitor-server/notify.yaml`）；Web 端修改写入此处并热加载 |
| `agentAuth.enabled` | 启用 Agent 接入授权（默认 false） |
| `agentAuth.secret` | 授权密钥；启用且留空时启动自动生成 |
| `agentBinDir` | Agent 二进制分发目录（自带 CDN） |
| `agentScriptPath` | Agent 安装脚本路径（`/install/agent-install.sh`） |
| `dialtestFile` | 拨测任务配置文件（默认 `/etc/monitor-server/dialtest.yaml`） |
| `auth.password` | Web 登录密码。存储为**国密 SM3 加盐哈希**（形如 `sm3:<salt>:<hash>`）；旧明文配置在 Server 启动时自动迁移为哈希并写回，无需手动处理。可在 Web 端「系统设置 → 修改密码」更改。 |
| `reportDir` | 报告存储目录（默认 `/var/lib/monitor-server/reports`） |
| `assetStoreFile` | 资产台账库（内嵌 SQLite 单文件，默认 `/var/lib/monitor-server/assets.db`）。**审计事件与告警处置也在这个库里**（同库单连接） |
| `securityStoreFile` | 安全事件 / 基线持久化文件（默认 `/var/lib/monitor-server/security_store.json`） |
| `retentionFile` | 数据保留策略文件（默认 `/etc/monitor-server/retention.yaml`，Web 端可改、保存即热生效） |
| `auditKeyFile` | **审计防篡改链的 HMAC 密钥文件**（默认 `/etc/monitor-server/audit.key`，0600，首次启动自动生成）。**必须纳入备份**：丢了它，历史审计全部无法校验。**不要把它放进 `server.yaml`**，也不通过任何接口取（接口只返回指纹） |
| `logDir` / `logBackend` | 集中日志落盘目录（默认 `/var/lib/monitor-server/logs`）与后端选择：`local`(默认，自研分片) / `victorialogs`（外部，配 `logVictoriaLogs.addr`） |
| `logMaxBytesPerDay` / `logUploadRateBps` / `logMaxBodyBytes` | 集中日志的三个上限：单来源每日写入上限（默认 1 GiB）、单节点上行速率上限（默认 1 MiB/s）、单次上行请求体上限（默认 4 MiB）。**三个都必须有值**，否则"开了日志把盘写满"只是时间问题 |

> **Web 通知配置**：登录后在「通知配置」页可视化配置邮件 / Webhook / 钉钉 / 飞书 / 企业微信，支持多收件人、多群（多 Webhook 地址）、@ 成员；保存即写入独立 `notifyFile` 并热加载，无需重启。敏感字段（SMTP 密码、机器人加签密钥）读取时脱敏，留空表示不修改。

### 时序库后端

| `tsdb.backend` | 写入路径 | 说明 |
|------|------|------|
| `victoriametrics`（默认） | `/api/v1/write` | 单二进制，推荐 |
| `mimir` | `/api/v1/push` | Grafana Mimir |
| `cortex` | `/api/v1/push` | Cortex（写入 `:9009` / 查询 `:8080`） |
| `thanos` | `/api/v1/receive` | Thanos Receive（写入 `:19291` / 查询 `:9090`） |
| `prometheus` | `/api/v1/receive` | 经 remote_write receiver |
| `custom` | 由 `tsdb.writePath` 指定 | 任意 PromQL 时序库 |

> 非 PromQL 时序库（InfluxDB/TimescaleDB）不在支持范围。

## Agent（`agent.yaml`）

| 字段 | 说明 |
|------|------|
| `mode` | 运行模式：`collect`(默认,采集) \| `edge`(网闸区A边界代理) \| `hub`(网闸区B边界代理)；详见「网闸代理部署」 |
| `serverURL` | Server 接收地址（collect 模式）；Edge 模式填 Hub 的 HTTPS 地址；Hub 模式填真实 Server 地址 |
| `node` | 节点名（默认 hostname） |
| `group` | 分组 |
| `secret` | 接入授权密钥（与 Server 一致） |
| `interval` | 采集间隔（秒）；代理模式下用于自监控指标上报周期 |
| `proxy` | 代理模式配置（mode=edge/hub 时生效），见下表 |
| `collectors` | 采集项开关（cpu / memory / disk / network / process / load / redis / mysql / postgres / nginx / nginxLog / kafka / docker / rocketmq / k8s / mongodb / fastdfs / port / security）；`security` 默认开启，关闭时设 `false` |
| `redisInstances` | Redis 实例连接配置列表（数组，密码仅存本地不上报） |
| `mysqlInstances` | MySQL 实例连接配置列表（数组，密码仅存本地不上报） |
| `postgresInstances` | PostgreSQL 实例连接配置列表（数组，密码仅存本地不上报） |
| `mongoInstances` | MongoDB 实例连接配置列表（数组，密码仅存本地不上报） |
| `fastdfsInstances` | FastDFS 实例连接配置列表（数组，无密码） |
| `nginxInstances` | Nginx 实例连接配置列表（数组） |
| `kafkaInstances` | Kafka 实例连接配置列表（数组） |
| `dockerInstances` | Docker 实例连接配置列表（数组） |
| `rocketmqInstances` | RocketMQ 实例连接配置列表（数组） |
| `k8sInstances` | Kubernetes 集群连接配置列表（数组，kubeconfig/token 仅存本地不上报） |
| `portChecks` | TCP 端口存活检测列表（数组），如 `["80","443","3306"]`，开启 `collectors.port` 后生效 |
| `security` | 安全采集配置段（开启 `collectors.security` 后生效），详见「安全监测中心配置」章节 |

**代理模式配置（`proxy` 字段，mode=edge/hub 时生效）**

| 字段 | 模式 | 说明 |
|------|------|------|
| `listen` | edge/hub | 监听地址（Edge 默认 `:18080` 本地汇聚口；Hub 默认 `:8443` TLS 监听口） |
| `hubAddr` | edge | Hub 地址 `host:port`（如 `10.0.0.2:8443`），Edge 主动拨出 TLS 隧道至此 |
| `serverURL` | hub | 真实 Server 地址（如 `http://127.0.0.1:8080`），Hub 转发请求至此 |
| `tlsCert` | edge/hub | TLS 证书文件路径（mTLS 双向校验） |
| `tlsKey` | edge/hub | TLS 私钥文件路径 |
| `tlsCa` | edge/hub | CA 证书文件路径（用于校验对端证书） |
| `bufferSize` | edge | 断连期间内存缓冲条数，默认 1000；满时丢弃最旧请求并计数 |
| `poolSize` | edge | 到 Hub 的并发隧道连接数，默认 2 |

### 通过命令一键配置（推荐）

安装 Agent 后，在任何被监控节点上执行以下命令，按交互引导填写实例即可（无需手编 yaml）：

```bash
# 在线方式（从 Server CDN 拉取脚本）
curl -fsSL http://<server>:8080/install/agent-install.sh | bash -s -- redis
curl -fsSL http://<server>:8080/install/agent-install.sh | bash -s -- mysql
curl -fsSL http://<server>:8080/install/agent-install.sh | bash -s -- postgres
curl -fsSL http://<server>:8080/install/agent-install.sh | bash -s -- nginx
curl -fsSL http://<server>:8080/install/agent-install.sh | bash -s -- kafka
curl -fsSL http://<server>:8080/install/agent-install.sh | bash -s -- rocketmq
curl -fsSL http://<server>:8080/install/agent-install.sh | bash -s -- k8s
curl -fsSL http://<server>:8080/install/agent-install.sh | bash -s -- mongodb
curl -fsSL http://<server>:8080/install/agent-install.sh | bash -s -- fastdfs

# 或直接调用本机已安装的脚本
bash /etc/monitor-agent/agent-install.sh redis
bash /etc/monitor-agent/agent-install.sh mysql
bash /etc/monitor-agent/agent-install.sh postgres
bash /etc/monitor-agent/agent-install.sh nginx
bash /etc/monitor-agent/agent-install.sh kafka
bash /etc/monitor-agent/agent-install.sh rocketmq
bash /etc/monitor-agent/agent-install.sh k8s
bash /etc/monitor-agent/agent-install.sh mongodb
bash /etc/monitor-agent/agent-install.sh fastdfs
```

RabbitMQ / Elasticsearch / ClickHouse / Nacos / ZooKeeper 五类暂无向导子命令，
请在 `/etc/monitor-agent/agent.yaml` 中手动添加对应实例段（示例见下文「手动编辑 YAML」），
开启对应的 `collectors` 开关并重启 Agent。

向导会引导你逐个填写对应中间件的实例信息（别名、地址、账号/密码、拓扑类型等），完成后自动写入 `agent.yaml`、开启对应的 `collectors` 开关并重启 Agent。**密码仅存本机，不上报 Server。**

> 该命令仅修改配置并重启 Agent，不安装/覆盖二进制。可多次执行以更新实例列表（会覆盖对应中间件的 `xxxInstances` 段）；不同中间件子命令对应的配置段相互独立。

### 手动编辑 YAML（高级）

也可直接编辑 `/etc/monitor-agent/agent.yaml`，字段说明如下：

```yaml
serverURL: "http://10.0.0.1:8080"
node: "web-01"
group: "default"
interval: 15

collectors:
  cpu: true
  memory: true
  disk: true
  network: true
  process: true
  load: true
  redis: true                    # ← 总开关，必须开启

redisInstances:
  # 1) 单机（无需指定 db，监控命令为实例级，与具体库无关）
  - name: "redis-standalone"
    addr: "127.0.0.1:6379"
    password: "yourpassword"     # 仅存本地，不上报 Server
    topology: "standalone"

  # 2) 主从（master / slave 各配一条 standalone，分别填各自地址）
  - name: "redis-master"
    addr: "127.0.0.1:6379"
    password: "yourpassword"
    topology: "standalone"
  - name: "redis-slave"
    addr: "127.0.0.1:6380"
    password: "yourpassword"
    topology: "standalone"

  # 3) 哨兵（addr 填任一哨兵节点，sentinelName 填 master 名，Agent 自动发现并采集 master）
  - name: "redis-sentinel"
    addr: "127.0.0.1:26379"
    password: "yourpassword"
    topology: "sentinel"
    sentinelName: "mymaster"

  # 4) 集群（addr 填任一集群节点，Agent 自动遍历全部 master 并采集）
  - name: "redis-cluster"
    addr: "127.0.0.1:7000"
    password: "yourpassword"
    topology: "cluster"

  # 5) Prometheus exporter 模式（exporterURL 填 /metrics，不走直连）
  - name: "redis-exporter"
    addr: "127.0.0.1:6379"
    password: ""
    topology: "standalone"
    exporterURL: "http://127.0.0.1:9121/metrics"
```

**Redis 字段说明**

| 字段 | 必填 | 说明 |
|------|------|------|
| `name` | 是 | 实例别名，Web 展示用 |
| `addr` | 是 | 地址 `host:port`（直连为 Redis 地址；sentinel 为哨兵地址；cluster 为任一节点；exporter 为实例地址） |
| `password` | 否 | 认证密码，`json:"-"` 标记，**仅存 Agent 本地，绝不通过网络上报**，Web 端不可见；存储为**国密 SM4 加密**（形如 `enc:<base64>`），旧明文配置保持兼容、按明文使用，无需手动处理 |
| `db` | 否 | 预留字段，当前采集为实例级指标，与具体 DB 无关，**无需填写** |
| `topology` | 是 | `standalone` \| `replication` \| `sentinel` \| `cluster` |
| `sentinelName` | 哨兵必填 | sentinel 模式监控的 master 名称 |
| `exporterURL` | exporter 必填 | Prometheus exporter 的 `/metrics` URL；**一旦填写即走 exporter 拉取模式，忽略直连** |
### 采集高可用：Agent 冗余部署（避免单点故障）

中间件采集依赖部署在某台机器上的 Agent。**如果只装了一台 Agent，它所在的服务器宕机或进程退出，则该 Agent 负责采集的所有 Redis 实例（含整个集群）监控数据全部中断**——Redis 本身照常运行，只是监控出现盲区（前端显示"未采集"/离线，该 Agent 节点被 Server 标为 offline）。

Redis 集群入口节点的单点问题已由 Agent 内置的入口故障转移解决（`collectCluster` 在配置入口不可达时，自动改用上次成功发现的其他存活节点作为入口）。但 **Agent 这台机器本身挂了没有任何内部机制能补**，需要冗余部署来消除：

**方案：双 Agent 同 `node` 名兜底**

1. 在另一台**独立**的机器上安装 Agent（建议与 Redis 节点分离，避免同机共损）；
2. 两台 Agent 的 `agent.yaml` 使用**完全相同的 `node` 名**（如都叫 `redis-monitor`），并配置**相同的 `redisInstances`**（含同一个集群入口）；
3. 两台 Agent 各自独立采集、独立上报。指标按 `node|instance` 聚合，前端把这台 node 下的 Redis 实例合并显示为一份，**不会翻倍**；
4. 任一 Agent 存活，Server 看到的都是同一个 node，数据不断；一台宕机，另一台无缝兜底。

> 关键点：**两个 Agent 的 `node` 名必须相同**。若配成不同名，同一 Redis 集群会在前端出现两份（两个 node 下各一份），造成重复展示。

**进阶：keepalived VIP**
如需更"生产级"的单主漂移，可在两台机器上用 keepalived 漂一个 VIP，两台 Agent 的 `node` 均配置为 VIP 对应的主机名，平时一主一备，主机宕机后 VIP 漂到备机，Server 视角节点不变（本质仍是上面的同 `node` 名方案，只是用 VIP 保证同一时刻只有一个主在写，避免双写）。

---

**MySQL 配置示例**

```yaml
collectors:
  mysql: true                     # ← 总开关，必须开启

mysqlInstances:
  - name: "mysql-master"
    addr: "127.0.0.1:3306"
    user: "monitor"
    password: "yourpassword"      # 仅存本地，不上报 Server
    topology: "standalone"        # standalone | replication | cluster

  - name: "mysql-cluster"         # Group Replication / InnoDB Cluster 多节点，name 相同即归为一组
    addr: "127.0.0.1:3306"
    user: "monitor"
    password: "yourpassword"
    topology: "cluster"

  - name: "mysql-exporter"
    addr: "127.0.0.1:3306"
    user: "monitor"
    password: ""
    topology: "standalone"
    exporterURL: "http://127.0.0.1:9104/metrics"
```

**MySQL 字段说明**

| 字段 | 必填 | 说明 |
|------|------|------|
| `name` | 是 | 实例别名 |
| `addr` | 是 | MySQL 地址 `host:port` |
| `user` | 是 | 采集账号（建议授予 `PROCESS`、`REPLICATION CLIENT` 权限） |
| `password` | 否 | 密码，`json:"-"` 标记，仅存本地不上报 |
| `topology` | 是 | `standalone` \| `replication` \| `cluster`（cluster 指 MySQL Group Replication / InnoDB Cluster，多节点多主；同集群各节点取相同 `name` 即在前端「实例拓扑」以集群分组展示；agent-install.sh 向导暂仅提供 standalone / replication，配置 cluster 需手动编辑 agent.yaml） |
| `exporterURL` | exporter 选填 | 填写后走 mysqld_exporter 拉取模式 |

---

**PostgreSQL 配置示例**

```yaml
collectors:
  postgres: true

postgresInstances:
  - name: "pg-primary"
    addr: "127.0.0.1:5432"
    database: "postgres"
    user: "monitor"
    password: "yourpassword"      # 仅存本地，不上报 Server
    sslMode: "disable"            # disable | require | verify-ca | verify-full
    topology: "standalone"        # standalone | replication

  - name: "pg-exporter"
    addr: "127.0.0.1:5432"
    database: "postgres"
    user: "monitor"
    password: ""
    sslMode: "disable"
    topology: "standalone"
    exporterURL: "http://127.0.0.1:9187/metrics"
```

**PostgreSQL 字段说明**

| 字段 | 必填 | 说明 |
|------|------|------|
| `name` | 是 | 实例别名 |
| `addr` | 是 | PostgreSQL 地址 `host:port` |
| `database` | 是 | 连接的数据库名 |
| `user` | 是 | 采集账号 |
| `password` | 否 | 密码，`json:"-"` 标记，仅存本地不上报 |
| `sslMode` | 否 | SSL 模式，默认 `disable` |
| `topology` | 是 | `standalone` \| `replication` |
| `exporterURL` | exporter 选填 | 填写后走 postgres_exporter 拉取模式 |

---

**Nginx 配置示例**

```yaml
collectors:
  nginx: true
  nginxLog: true   # 访问日志采集开关，用于数据大屏 Nginx 分析板块

nginxInstances:
  - name: "nginx-01"
    addr: "127.0.0.1:80"
    statusPath: "/nginx_status"   # stub_status 路径，默认 /nginx_status
    accessLog: "/var/log/nginx/access.log"   # access.log 路径；填写后采集访问日志
    logFormat: "combined_timed"   # 日志格式：combined（默认）| combined_timed（含 $request_time）

  - name: "nginx-vts"
    addr: "127.0.0.1:80"
    statusPath: "/nginx_status"
    exporterURL: "http://127.0.0.1:9913/metrics"   # VTS exporter
```

**Nginx 字段说明**

| 字段 | 必填 | 说明 |
|------|------|------|
| `name` | 是 | 实例别名 |
| `addr` | 是 | Nginx 监听地址 `host:port` |
| `statusPath` | 否 | `stub_status` 路径，默认 `/nginx_status` |
| `exporterURL` | exporter 选填 | 填写后走 nginx-vts-exporter 拉取模式 |
| `accessLog` | 否 | access.log 文件路径；填写后按行增量采集访问日志，用于数据大屏 Nginx 分析板块（请求来源地理分布、状态码分布、Top URI/Top IP）。留空则不采集 |
| `logFormat` | 否 | access.log 格式：`combined`（默认，对应 Nginx 默认 log_format）或 `combined_timed`（追加 `$request_time`，可统计平均响应时间）。需与实际日志格式一致，否则解析失败会跳过该行 |

**Nginx access.log 配置说明**

大屏的请求来源地理分布、状态码分布、Top URI/Top IP 等分析依赖 access log 解析，需同时满足以下条件：

1. 在 Agent 配置的 `collectors` 中开启 `nginxLog: true`，否则不会创建访问日志采集器，仅配置 `accessLog` 路径不会生效。
2. Nginx 的 `log_format` 与 Agent 的 `logFormat` 保持一致。启用响应时间统计时，Nginx 侧可配置：

```nginx
http {
    log_format combined_timed '$remote_addr - - [$time_local] "$request" $status $body_bytes_sent $request_time';
    server {
        access_log /var/log/nginx/access.log combined_timed;
    }
}
```

3. 日志文件的读取权限：Agent 进程需能读取该文件（如运行用户为 root，或将该用户加入日志目录所属组）。
4. 修改配置后重启 Agent 生效：`systemctl restart monitor-agent`。首次采集从文件末尾开始增量跟踪，历史行不计入统计。

---

**Kafka 配置示例**

```yaml
collectors:
  kafka: true

kafkaInstances:
  - name: "kafka-cluster"
    addr: "127.0.0.1:9092"        # 任一 Broker 地址
    version: "2.8.0"              # 用于展示的版本号
    # exporterURL: "http://127.0.0.1:9308/metrics"  # 可选 JMX exporter
```

**Kafka 字段说明**

| 字段 | 必填 | 说明 |
|------|------|------|
| `name` | 是 | 实例别名（集群名） |
| `addr` | 是 | 任一 Broker 地址 `host:port` |
| `version` | 否 | Kafka 版本号，仅展示用 |
| `exporterURL` | exporter 选填 | 填写后走 kafka-exporter 拉取模式 |

---

**Docker 配置示例**

```yaml
collectors:
  docker: true

dockerInstances:
  - name: "local-docker"
    addr: "unix:///var/run/docker.sock"   # 本地 Unix Socket
    # 远程 Docker 可填 tcp://10.0.0.1:2375（需开启 TCP 监听）
```

**Docker 字段说明**

| 字段 | 必填 | 说明 |
|------|------|------|
| `name` | 是 | 实例别名 |
| `addr` | 是 | Docker Daemon 地址，本地用 `unix:///var/run/docker.sock`；远程用 `tcp://host:2375` |

---

**RocketMQ 采集模式说明**

Agent 支持两种采集模式，但**强烈建议使用 exporter 模式**：

- **exporter 模式（推荐，必选用于 RocketMQ 5.x）**：Agent 通过 `exporterURL` 拉取一个 RocketMQ Prometheus Exporter 暴露的 `/metrics`，兼容 RocketMQ 4.x / 5.x，无需 NameServer 开启任何 HTTP 接口。
- **HTTP API 直连模式（不推荐，5.x 不可用）**：Agent 直接向 `addr`（NameServer）发起 HTTP GET 到 `/rocketmq/httpapi/...` 端点。但标准 RocketMQ 的 NameServer（9876）与 Proxy（默认 8080）均只跑二进制 `Remoting` 协议、**不提供标准 HTTP 管理 API**，直连会得到 `EOF` 报错且拿不到数据。该模式仅在你的环境通过反向代理/定制把 `/rocketmq/httpapi/` 以真正 HTTP 形式暴露时才可用。

> 一句话：**RocketMQ 5.x 请务必配置 `exporterURL` 走 exporter 模式**；只填 `addr` 不填 `exporterURL` 在 5.x 上必然失败（日志报 `RocketMQ 集群信息获取失败 ... EOF`）。

**RocketMQ 配置示例（exporter 模式，推荐）**

```yaml
collectors:
  rocketmq: true

rocketmqInstances:
  - name: "rocketmq-cluster"
    addr: "127.0.0.1:9876"                       # NameServer 地址（仍必填，用于标识）
    exporterURL: "http://127.0.0.1:5557/metrics" # 指向 RocketMQ exporter 的 /metrics
```

**用户侧操作步骤（exporter 模式）**

1. 部署一个 RocketMQ Prometheus Exporter（与 Agent 同机或网络可达），把 NameServer 地址指过去：
   ```bash
   docker run -d --name rocketmq-exporter -p 5557:5557 \
     -e ROCKETMQ_NAMESRV_ADDR=127.0.0.1:9876 \
     masteryourtech/rocketmq-exporter   # 或 apache/rocketmq-exporter
   ```
   > 若使用独立部署的 NameServer 集群，把 `ROCKETMQ_NAMESRV_ADDR` 改为 `ns1:9876;ns2:9876`。
2. 在 Agent 的 `agent.yaml` 中按上面的示例填好 `exporterURL`（同时保留 `addr`）。
3. 重启 Agent（必须）：
   ```bash
   systemctl restart monitor-agent   # 或离线包：/etc/monitor-agent/monitor-agent restart
   ```
4. 查看采集日志确认无报错、且不再出现 `EOF`：
   ```bash
   journalctl -u monitor-agent -f | grep -i rocketmq
   ```
5. Web 端「中间件监控 → RocketMQ」Tab 查看概览卡片、实例列表与详情抽屉趋势图。

**RocketMQ 字段说明**

| 字段 | 必填 | 说明 |
|------|------|------|
| `name` | 是 | 实例别名（集群名） |
| `addr` | 是 | NameServer 地址 `host:port`（用于实例标识；5.x 下不用于 HTTP 直连） |
| `exporterURL` | **5.x 必填** | 指向 RocketMQ exporter 的 `/metrics` 地址，填写后走 exporter 拉取模式 |

---

**Kubernetes 配置示例**

```yaml
collectors:
  k8s: true

k8sInstances:
  # 方式①：kubeconfig 文件认证
  - name: "prod-cluster"
    kubeconfig: "/root/.kube/config"   # 仅存本地不上报
    insecureTLS: true                  # 自签名证书跳过校验
    metricsServer: true                # 启用 metrics-server 采集节点 CPU/内存
  # 方式②：apiServer + ServiceAccount Token
  - name: "staging-cluster"
    apiServer: "https://10.0.0.2:6443"
    token: "eyJhbGciOi..."             # 仅存本地不上报
    insecureTLS: true
    metricsServer: false
    #     exporterURL: "http://127.0.0.1:8080/metrics"  # 可选 kube-state-metrics
# includeSystemNamespaces: false   # 台账清单是否上报 kube-system / kube-public / kube-node-lease（默认 false = 排除）
```

> **清单上报**：配了 `k8sInstances`（且不是 exporter 模式）时，Agent 会随上报周期把该集群的
> **Pod 与工作负载清单**一并上报，Server 落成台账资产（类型 `pod` / `workload`）并建立
> `runs_on`（Pod → 所在主机）与 `member_of`（Pod → 所属工作负载）两条关系。
> 单轮上限 1000 个 Pod / 500 个工作负载，超限会截断并在上报体里**显式置位**（不会静默丢弃）。
> 容器与工作负载是**短命对象**，默认不计入台账首页的「总数 / 失联 / 无责任人」——
> 按类型筛选或关键词搜索即可看到。

**MongoDB 配置示例**

```yaml
collectors:
  mongodb: true

mongoInstances:
  - name: "prod-mongo"              # 副本集多成员请使用相同 name 归为同一副本集
    addr: "10.0.0.10:27017"
    user: "monitor"                 # 无认证时留空
    password: "****"                # 仅存本地不上报
    authSource: "admin"             # 认证库
    database: "appdb"               # 采集 dbStats 的库；留空则不采集库存储/对象统计
    topology: "replicaset"          # standalone / replicaset / sharded
    # exporterURL: "http://127.0.0.1:9216/metrics"  # 可选；填了走 exporter，不再直连
```

**MongoDB 字段说明**

| 字段 | 必填 | 说明 |
|------|------|------|
| `mongoInstances[].name` | 是 | 实例别名（replicaset 多成员请使用相同 `name` 归为同一副本集） |
| `mongoInstances[].addr` | 是 | MongoDB 地址 `host:port` |
| `mongoInstances[].user` | 否 | 认证用户名；无认证留空 |
| `mongoInstances[].password` | 否 | 密码，仅存 Agent 本地不上报 |
| `mongoInstances[].authSource` | 否 | 认证库（如 `admin`） |
| `mongoInstances[].database` | 否 | 采集 `dbStats` 的库名；留空则不采集库存储/对象统计 |
| `mongoInstances[].topology` | 否 | `standalone` / `replicaset` / `sharded`；影响角色判定展示 |
| `mongoInstances[].exporterURL` | 否 | mongodb_exporter 的 `/metrics` 地址；填了走 exporter，不再直连 |

> **提示**：MongoDB 直连模式下建议为监控账号授予 `clusterMonitor` 角色（副本集）或 `readAnyDatabase`，以保证 `serverStatus` / `dbStats` / `replSetGetStatus` 可读取。

**FastDFS 配置示例**

```yaml
collectors:
  fastdfs: true

fastdfsInstances:
  - name: "tracker-1"
    role: "tracker"                 # tracker / storage
    addr: "10.0.0.20:22122"
    group: ""                       # tracker 留空
  - name: "storage-1"
    role: "storage"
    addr: "10.0.0.21:23000"
    group: "group1"
    exporterURL: "http://127.0.0.1:9300/metrics"  # 完整指标依赖 fastdfs_exporter
```

**FastDFS 字段说明**

| 字段 | 必填 | 说明 |
|------|------|------|
| `fastdfsInstances[].name` | 是 | 实例别名 |
| `fastdfsInstances[].role` | 是 | `tracker` / `storage` |
| `fastdfsInstances[].addr` | 是 | FastDFS 地址 `host:port`（tracker 22122 / storage 23000） |
| `fastdfsInstances[].group` | 否 | storage 所属 group 名称；tracker 留空 |
| `fastdfsInstances[].exporterURL` | 否 | fastdfs_exporter 的 `/metrics` 地址；完整空间/IO/Storage 状态依赖它，未配置时仅做端口存活探测 |

> **提示**：FastDFS 无标准直连接口，建议部署社区 `fastdfs_exporter` 并填写 `exporterURL` 以获取 Group/Storage 数量、空间使用、磁盘读写、网络收发等完整指标；仅做 TCP 存活探测时只能看到在线/离线状态。exporter 与 FastDFS 本身一样由用户部署，容器化与宿主机部署都可使用。

> **只在一个实例上配置 `exporterURL`**：fastdfs_exporter 返回的是**整个 group** 的总量，不是单个 tracker/storage 的。若同一份 exporter 挂到多个实例上，首页 KPI 会按实例重复累加（总空间正好翻倍）。推荐只挂在 storage 实例上——tracker 本来就没有存储空间。

**exporter 指标契约**

平台按以下指标名读取（单位均为字节，展示时自动换算 MB/GB）。exporter 名称不一致时，需在 exporter 侧做重命名或加一层映射：

| 指标 | 含义 |
|---|---|
| `fastdfs_up` | 实例可达（1/0）；缺失时只要解析到其它 `fastdfs_*` 指标也视为在线 |
| `fastdfs_group_count` | group 数 |
| `fastdfs_storage_count` | storage 节点总数 |
| `fastdfs_storage_online_count` | 在线 storage 数 |
| `fastdfs_storage_offline_count` | 离线 storage 数 |
| `fastdfs_total_space` / `fastdfs_free_space` / `fastdfs_used_space` | 总/空闲/已用空间（字节） |
| `fastdfs_trunk_free_space` | trunk 空闲空间（字节） |
| `fastdfs_disk_read_bytes` / `fastdfs_disk_write_bytes` | 累计磁盘读写（字节） |
| `fastdfs_net_recv_bytes` / `fastdfs_net_sent_bytes` | 累计网络收发（字节） |

> 平台离线包自带 `exporters/fastdfs-exporter`（独立可执行程序，不编译进 Agent、不随 Agent 升级），支持本机 `fdfs_monitor` 与 `docker exec` 两种模式，指标名与上表一致；用法见包内 `exporters/fastdfs-exporter/README.md`。也可以继续使用任意第三方 exporter，但需保证指标名与上表一致。

**Kubernetes 字段说明**

| 字段 | 必填 | 说明 |
|------|------|------|
| `name` | 是 | 集群别名 |
| `apiServer` | 条件 | apiserver 地址 `https://host:6443`；留空则从 kubeconfig 取 |
| `kubeconfig` | 条件 | kubeconfig 文件路径（与 apiServer+token 二选一，仅存本地不上报） |
| `token` | 条件 | ServiceAccount Bearer Token（配 apiServer 使用，仅存本地不上报） |
| `insecureTLS` | 选填 | 跳过 apiserver 证书校验（默认 false） |
| `metricsServer` | 选填 | 启用 metrics-server 采集 Node/Pod CPU/内存使用率（默认 false） |
| `exporterURL` | exporter 选填 | 填写后走 kube-state-metrics /metrics 拉取模式 |

> 认证仅支持 token / client-cert 两种；exec/plugin（如云厂商 IAM 鉴权）不支持，需要时用 token 模式替代。
>
> **兼容标准 K8s 与 k3s**：采集基于标准 apiserver REST API，与发行版无关。k3s 使用时注意三点：
> - kubeconfig 默认在 `/etc/rancher/k3s/k3s.yaml`（非 `/root/.kube/config`），配置 `kubeconfig` 字段填此路径；
> - k3s 默认内置 metrics-server，直接开 `metricsServer: true` 即可采集节点 CPU/内存使用率；
> - k3s kubeconfig 内 server 默认为 `https://127.0.0.1:6443`，若 Agent 与 k3s 不同机，需用 `apiServer` 字段覆盖为节点真实地址。
>
> **k3s 配置示例**：
> ```yaml
> k8sInstances:
>   - name: "k3s-cluster"
>     kubeconfig: "/etc/rancher/k3s/k3s.yaml"
>     insecureTLS: true      # k3s 默认自签名证书
>     metricsServer: true    # k3s 内置 metrics-server
> ```

**RabbitMQ 配置示例**

采集走 rabbitmq_prometheus 插件的 `/metrics`（默认端口 15692，部署 RabbitMQ 后执行
`rabbitmq-plugins enable rabbitmq_prometheus` 即可，无需额外 exporter）。

```yaml
collectors:
  rabbitmq: true

rabbitmqInstances:
  - name: "rabbitmq-01"
    addr: "127.0.0.1:15692"
    # username / password 可选：仅在通过管理端口反代等需要 Basic Auth 的场景填写
```

**Elasticsearch 配置示例**

采集走原生 JSON 接口（`/` 与 `/_cluster/health`，默认端口 9200）。

```yaml
collectors:
  elasticsearch: true

elasticsearchInstances:
  - name: "es-01"
    addr: "http://127.0.0.1:9200"
    username: ""          # 开启安全认证时填写
    password: ""
```

**ClickHouse 配置示例**

采集走 HTTP 端口（默认 8123）执行 SQL（version / system.metrics / uptime），无需 exporter。

```yaml
collectors:
  clickhouse: true

clickhouseInstances:
  - name: "ch-01"
    addr: "127.0.0.1:8123"
    username: "default"
    password: ""
```

**Nacos 配置示例**

采集走控制台健康检查接口（`/nacos/v1/console/health/readiness`，默认端口 8848），
提供存活监控与服务离线告警。

```yaml
collectors:
  nacos: true

nacosInstances:
  - name: "nacos-01"
    addr: "127.0.0.1:8848"
    contextPath: "/nacos"
```

**ZooKeeper 配置示例**

采集走四字命令 `mntr`（TCP 直连，默认端口 2181，无需第三方 exporter）。
若服务端配置了 `4lw.commands.whitelist`，需包含 `mntr`。

```yaml
collectors:
  zookeeper: true

zookeeperInstances:
  - name: "zk-01"
    addr: "127.0.0.1:2181"
```

**部署与验证**

> 以下以 Redis 为例，其余中间件步骤完全一致，仅替换对应的 `collectors` 开关与实例段，并在 Web 端进入对应的中间件 Tab 查看。

1. 编辑 Agent 配置 `agent.yaml` 加入上述对应中间件的配置；
2. 重启 Agent（必须，中间件采集逻辑依赖 1.2.0+ 二进制）：
   ```bash
   systemctl restart monitor-agent   # 或离线包：/etc/monitor-agent/monitor-agent restart
   ```
3. 查看采集日志确认无报错：
   ```bash
   journalctl -u monitor-agent -f | grep -iE 'redis|mysql|postgres|nginx|kafka|docker|rocketmq|k8s'
   ```
4. Web 端左侧菜单「中间件监控」→ 对应中间件 Tab（Redis / MySQL / PostgreSQL / Nginx / Kafka / Docker / RocketMQ / Kubernetes）查看：概览卡片、实例列表、实例详情抽屉（多趋势图）。

> **版本一致性**：中间件监控涉及 Agent（采集）与 Server（实例聚合 + API）两端改动，两端须同时升级到同一版本（≥ 1.2.0），否则对应 Tab 无数据。
> **密码安全**：各中间件 `password` 仅在 Agent 本地用于直连，`json:"-"` 标记，不上报、不入库、Web 不可见；磁盘上以**国密 SM4 加密**形式存储（旧明文配置保持兼容，无需手动处理）。Server 登录密码以**国密 SM3 加盐哈希**存储，被攻破时无法反推原密码。

## 告警规则

告警规则由 Web 端「告警中心 → 告警规则」维护，持久化为规则文件（`server.yaml` 的 `alert.rulesFile`，默认 `/etc/monitor-server/rules.yaml`）。规则类型通过 `type` 字段区分。

### PromQL 规则（`type=promql`）

用一条 PromQL 表达式定义告警：表达式返回的**每条序列**都会单独触发，序列消失即恢复。

- 条件写在表达式里（如 `cpu_usage > 85`），不需要再填运算符和阈值。
- 结果带 `node` 标签时按节点归属，并受规则的分组 / 指定主机范围限制；不带 `node` 时按完整标签集合作为全局序列，不受节点范围限制。
- `for` 表示序列连续存在多久才触发；`severity`、通知、静默、升级等字段与阈值规则一致。
- 单条规则单轮最多处理 200 条序列，超出会截断并记录日志。
- 查询失败时保持上一轮状态，不会误恢复。
- 保存规则时会执行一次表达式校验；表单里的「试运行」可预览命中结果。

```yaml
- name: "消费积压"
  type: promql
  expr: "kafka_consumer_lag > 100"
  for: "5m"
  severity: warning
  scope: all
  enabled: true
```

## UI 配置（`ui.yaml`）

`server.yaml` 的 `uiFile` 指向该文件（默认 `/etc/monitor-server/ui.yaml`）。`name` / `logo` / `footer` / `subtitle` 为品牌配置；`disabled` 是已关闭的模块与页面 key。

- `disabled` 是**关闭清单**：缺省或空 = 全开，升级新增页面自动可见。
- 不可关闭：`overview`（值班台）、`alerts`（告警中心）、`alerting`（整域）、`system`（系统设置）。
- 关闭只隐藏入口并停止自动巡检 / 报告 / 拨测，不注销接口、不删除数据。
- 接口采用部分更新：只提交 `disabled` 不会覆盖品牌字段。

```yaml
name: 英招
subtitle: 运维平台
disabled:
  - assets
  - logs
```

## AI 助手（`ai.yaml`）

`server.yaml` 的 `aiFile` 指向该文件（默认 `/etc/monitor-server/ai.yaml`）。默认关闭；启用后所有调用都会写审计（`ai:invoke` 单列）。

| 字段 | 默认 | 说明 |
|---|---|---|
| `enabled` | `false` | 总开关 |
| `baseUrl` / `model` / `apiKey` | — | OpenAI 兼容端点、模型名、密钥（密钥不写日志、接口不回显） |
| `timeoutSeconds` / `maxTokens` / `temperature` | `60` / `1024` / `0.2` | 单次调用参数 |
| `allowSensitive` | `false` | 允许发送资产属性值等高级别数据 |
| `toolMode` | `auto` | 工具调用协议：`auto`（先原生 function calling，4xx 不支持时回退 JSON）\| `native` \| `json` |
| `autoInvestigate` | `false` | 允许 critical 告警触发自动调查 |

### 工具与自动调查（阶段 2）

- 工具全部**只读**，且只访问**告警所属节点**；模型在参数里传别的节点会被拒绝。
- `log_stats` 只返回来源与条数，**不返回日志正文**。
- `autoInvestigate` 开启后：仅 **critical** 告警触发；同一「规则 + 节点」30 分钟内只调查一次；全局并发 2；单次最多 4 轮 / 8 次工具调用 / 8000 tokens / 120 秒。
- 工具执行失败不会终止调查（错误回填给模型继续排查）；轮次或预算用尽时返回已获得的结论并标记停止原因。
- 结果保存在 `ai_investigations.json`（最多 500 条、保留 30 天），在告警详情页「AI 调查」区块展示。
- 审计动作：`ai:auto_investigate`（actor 为 `auto`）。
