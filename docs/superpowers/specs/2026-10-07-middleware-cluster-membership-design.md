# 中间件集群归属（实例 → 集群的 `member_of`）设计件

- 批次：26
- 日期：2026-10-07
- 对应全景表：`2026-09-30-ops-platform-capability-map.md` §3.2 行 104 正文里**最后一条"仍缺"**
  （原文：「中间件集群归属（`Group` 是 Agent 本地别名）」）
- 前序口径：`2026-10-07-asset-relation-auto-source-design.md` 的 **D5**——「本批不做。`Group` 是 Agent
  本地配置别名，跨 Agent 可能同名不同集群；而且『集群』当前没有对应的资产类型——**要做先定集群资产模型**，
  属于另一批」。**本件就是那个"另一批"**（D5 是待办，不是否决）。
- 状态：**设计待评审（D1~D10）**

## 1. 起因

行 104 的三条"仍缺"里，K8s `service → pod` 已在批次 25 落地，剩下这条是**中间件集群归属**。
它要回答的问题是：**这个中间件实例属于哪个集群？**——今天只能看到 Agent 本地配的别名 `Group`
（`redis.go:119` 取 `cfg.Name`），跨 Agent 同名可以指两个不同的集群，因此它**不是身份**。

## 2. 现状（调研结论，逐条有依据）

### 2.1 各中间件"集群身份"的可用性——**这才是本批真正的约束**

| 类型 | 有没有权威集群标识 | 依据 |
|---|---|---|
| **Elasticsearch** | ✅ **有，且已采到**：`GET /` 的 `cluster_name` | `internal/agent/collector/elasticsearch.go:70/93`，写进指标标签 `:110`；模型字段 `model/metric.go:358`。**只是接收侧没映射进实例属性** |
| **MongoDB** | ✅ 有，**读到了但没存**：副本集名 `setName` | `internal/agent/collector/mongo.go:211`（当前只用于角色/复制状态判定） |
| **Kafka** | ✅ 可取，**当前没取**：`clusterId`（AdminClient） | `internal/agent/collector/kafka.go`（`Group` 仍是 `cfg.Name`） |
| **MySQL（Group Replication）** | ✅ 可采，**当前没采**：`group_replication_group_name`（UUID） | `internal/agent/collector/mysql.go`（GR 只有组视图 `mysql_gr_view_member`，`:237`，只用于健康判定） |
| FastDFS | △ 半有：storage group（**来自配置**） | `internal/agent/collector/fastdfs.go:64` |
| redis / postgres / clickhouse / rabbitmq / nacos / zookeeper / rocketmq / docker / nginx | ❌ 没有：只有 `cfg.Name` 别名 | 各采集器一律 `Group: cfg.Name`（`redis.go:119`、`kafka.go:96`、`mongo.go:68`、`nacos.go:68`…） |

**结论**：能做"归属"的只有 **ES / MongoDB / Kafka / MySQL-GR** 四类；其余七类**没有可用的身份**，
按本项目一贯的"宁缺勿错"（D2/D6 的取向、`assets.go:243`），**本批明确不做**，不拿别名冒充集群名。

### 2.2 接收侧的现状

- 实例资产的自然键：`<类型>:<地址>`（`receiver/assets.go:86`，如 `redis:10.0.0.10:7000`）；
- 实例属性今天只有这几个键：`group / role / topology / version / status / image / replicaOf / addrHost / up`
  （`assets.go:67-84`）——**没有集群名字段**，所以即使载荷带了（ES 就是）也会在这一步被丢掉；
- 实例资产的归属节点是**上报主机**（`assets.go:87` `Node: p.Node`），跨机采集的实例也如此。

### 2.3 关系通道（全部就绪，照抄 Pod → 工作负载即可）

- `member_of` 的语义就是"实例属于集群"（`asset/model.go:54`）；
- `LinkDiscovered` 契约：两端须已存在 / 幂等（UPSERT）/ 人工认领不降级 / 被抑制过不重建（`asset/service.go:456-474`）；
- 范本：`receiver/assets.go:404-412`（先落对端资产、再建边；顺序是**正确性**不是优化）。

## 3. 范围

**做**：ES / MongoDB / Kafka / MySQL-GR 四类的"实例 → 集群"自动 `member_of`（含集群资产类型与身份口径）。

**不做**（划线）：

- 其余七类中间件（没有权威身份，不拿 `Group` 冒充）；
- **"集群健康度"这类新指标**（本批只做资产与关系，不顺手扩指标面）；
- **改 `Group` 的语义**（它是界面上的实例别名，改了会动到中间件页的分组与筛选）；
- **Redis Cluster / Sentinel 的拓扑建模**（cluster 本身没有名字，`sentinel_master_of` 是另一条线）。

## 4. 设计

### 4.1 集群身份

- 统一字段：**`ClusterName`**——ES 已有这个字段（`model/metric.go:358`），本批把它补到
  MongoDB / Kafka / MySQL 三类实例上（**同一个名字，同一种含义**）；
- 集群资产自然键：**`<类型>:<集群名>`**（如 `elasticsearch:prod-logs`、`mongodb:rs0`、
  `kafka:AbC123…`、`mysql:<group-uuid>`）。与实例键 `<类型>:<地址>` 同形不同命名空间
  （集群名不可能长得像地址；真要撞上（集群名叫 `10.0.0.10:7000`）属于人为构造，不做特判）；
- **集群名来自被采集方**（`cluster_name` / `setName` / `clusterId` / `group_replication_group_name`），
  **不是 `cfg.Name`**——这正是本条与 D5 里被否掉的做法之间的区别。

### 4.2 新增资产类型

`middleware-cluster`（标题「中间件集群」），加进 `BuiltinTypes()`；**不加入 `EphemeralTypes()`**
（集群是稳定对象，成员全消失就是真失联——与 Pod/Workload 相反、与 `service` 同一取向）。

### 4.3 归属节点：**落空串**，并接受它的后果（本批最需要拍板的一条）

集群**跨主机**（三台 MySQL 的 GR 组、多个 Mongo 节点），没有单一归属节点。三个选项：

| 选项 | 后果 |
|---|---|
| (a) 取上报主机 | **会在多个 Agent 之间来回覆盖**（每个成员上报都会 Apply 一次），受限用户看到的归属会抖动——比没有更糟 |
| (b) **落空串** | 与"Pod 落在未注册节点上"**完全同一取向**（`assets.go:378`：「该节点没有对应主机资产时落空串 → 仅全局范围可见：不假装它属于某个分组」）。**代价：受限用户看不到集群资产** |
| (c) 按成员可见（改范围模型） | 最正确，但要动资源范围的判定口径，是另一个量级 |

**建议 (b)**，并做两件事把代价压住：
1. **实例上照样写 `cluster` 属性**——受限用户看实例详情就能知道它属于哪个集群，信息不丢；
2. 在设计件里写明"集群资产对受限用户不可见"，而不是让它变成一个"看不见却也不知道为什么"的现象。

### 4.4 关系与顺序

`member_of`：**实例 → 集群**（与 Pod → 工作负载同义）。顺序（`LinkDiscovered` 要求两端已存在）：

```
1. 集群资产（从本轮实例清单里抽出有 ClusterName 的集群集合去重）
2. 实例资产（照旧）
3. instance --member_of--> cluster
```

集群资产的 `lastSeen` 由**任一成员的上报**推进（每个成员上报都会 Apply 一次）；成员全部消失后
转 `missing`，**不物理删除**（既有约定）。

### 4.5 Agent 侧要补的三处采集

| 类型 | 补什么 | 位置 |
|---|---|---|
| MongoDB | `setName` 写进载荷的 `ClusterName` | `mongo.go:211`（**已经读到了**，只差放对地方） |
| Kafka | `describeCluster().clusterId()` | `kafka.go`（AdminClient 已在用） |
| MySQL | `SELECT @@group_replication_group_name`（**GR 未启用时为空 → 不建集群**） | `mysql.go`（与现有 GR 组视图同处） |
| Elasticsearch | **不用改**（已采） | — |

## 5. 决策点（D1~D10）

| # | 问题 | 建议 | 理由 |
|---|---|---|---|
| D1 | 范围 | **只做 ES / MongoDB / Kafka / MySQL-GR**；其余七类明确不做 | 那七类只有 `cfg.Name` 别名，拿它当身份就是 D5 否掉的那件事 |
| D2 | 集群身份 | 统一字段 `ClusterName`（**来自被采集方**）；自然键 `<类型>:<集群名>` | 一个名字一种含义；不新造概念 |
| D3 | 资产类型 | 新增内置 `middleware-cluster`，**不计入短命对象** | 集群是稳定对象，"失联"是真信号 |
| D4 | **归属节点** | **落空串**（+ 实例上保留 `cluster` 属性作为补偿） | 见 §4.3：取上报主机会抖动，改范围模型是另一个量级；空串与 Pod 的先例同口径 |
| D5 | 关系种类 | `member_of`（实例 → 集群） | 语义正合，与 Pod → 工作负载一致 |
| D6 | 顺序 | 集群资产 → 实例资产 → 建边 | `LinkDiscovered` 要求两端已存在 |
| D7 | 信息不足时 | **不建集群、不建边**（GR 未启用、单机 Mongo、Kafka 取不到 clusterId 都算） | 宁缺勿错 |
| D8 | 同一实例属于多个集群 | 本批**不处理**（一个实例最多一个 `ClusterName`）；真遇到时以"最后上报"为准并**在文档里记明** | 现在没有这样的真实场景，先不为它设计 |
| D9 | 升级面 | **两端都要升**（三处采集在 Agent） | 与批次 25 同 |
| D10 | 前端 | 只补中文名（类型标题、关系种类标签已是「归属」）；中间件页**不改** | 集群资产会自然出现在台账与关系视图里 |

## 6. 批次拆分、用例与实机验证

### 6.1 三笔

1. **Agent 采集**：MongoDB `setName` → `ClusterName`；Kafka `clusterId`；MySQL GR group name；
   `model/metric.go` 三处实例结构补 `ClusterName` 字段。用例：取到/取不到（GR 关、单机 Mongo）两条路径。
2. **Server 建资产与边**：`asset/model.go` 新增 `TypeMiddlewareCluster` + `MiddlewareClusterNaturalKey`；
   `receiver/assets.go` 里新增"抽集群 → 落资产 → 建边"三步 + 实例属性 `cluster`。
   用例：有 `ClusterName` 才建（无则不建、不造占位）、同一集群多成员只落一份资产、幂等、空节点、
   实例属性带上 `cluster`。
3. **前端与文档**：类型标题与标签（`AssetListView` / `AssetTopologyView` 的映射）；文档对账。

### 6.2 实机验证计划（dev-server）——**先说清能验到什么**

这台机器的中间件测试实例是 **MySQL / PostgreSQL / MongoDB / Redis / Nginx**（见
`reference_devserver_mw_test.md`），**没有 Elasticsearch 与 Kafka**；MySQL 的 **Group Replication 三节点
当前是停着的**（为了给 k3s 让内存），MongoDB 大概率是**单机**。

所以实机分成两段：

1. **先验"不猜"（不需要造数据，最有价值的一段）**：升级后台账里**不应出现任何 `middleware-cluster` 资产、
   也不应有 `member_of` 边**——因为 GR 停着（group name 为空）、Mongo 是单机（`setName` 为空）。
   若这里冒出来集群资产，说明我们**拿别名或猜的值当了身份**，那是本批最该被挡住的事。
2. **再造一个权威来源验通路**：把 MySQL GR 三节点拉回来（`sudo docker start mysql-gr-1/2/3`，
   约 1.45G 内存，**要与 k3s 抢**，验完停回）——三节点起来后应看到
   **1 个 `mysql:<group-uuid>` 集群资产 + 3 条 `member_of` 边**，且三个实例属性里都有 `cluster`。
   若内存不够，退而求其次：给测试用的 MongoDB 起一个**单节点副本集**（`--replSet rs0` + `rs.initiate()`），
   验同一套判据。

**未覆盖边界（不得视为通过）**：Elasticsearch 与 Kafka 在本机没有实例，**只能靠用例覆盖**；
集群资产对受限用户不可见（D4 的已知代价）；同一实例属于多个集群（D8 不处理）；
浏览器实点。

## 7. 待评审

D1~D10 若都按建议，则按 §6.1 的三笔开工；有异议的挑出来。

## 8. 实施记录（2026-10-07，Server 与 Agent 1.30.48）

D1~D10 按建议执行，**但 D1 的范围在实施中按代码事实收窄了一格**（见下）。

**① Agent 采集**：`model/metric.go` 给 `MongoDBInstance` / `MySQLInstance` 补 `ClusterName`
（ES 的 `ElasticsearchInstance.ClusterName` 早就有，不用改）；`mongo.go` 把已经读到的 `hello.setName`
落到 `ClusterName`（standalone 与 mongos 都为空 → 不建集群）；`mysql.go` 的 `grInfo` 补 `GroupName`，
在 `queryGroupReplicationInfo` 里多查一条 `SELECT @@group_replication_group_name`，非 cluster 拓扑时
`gr` 是零值 → 天然为空。

**⚠️ Kafka 本批不做**（D1 的收窄）：`sarama v1.43` 的 `ClusterAdmin.DescribeCluster()` 返回的是
**brokers + controllerID**（`admin.go:299`），**没有集群 ID**——集群 ID 只在 `MetadataResponse` 里
（`metadata_response_test.go` 可证），要拿它得对某个已连接的 broker 手发 `GetMetadata` 并**指定协议版本**
（v3+ 才有该字段）。而这台机器**没有 Kafka 实例可验**，等于要上线一段无法验证的取数代码。
按本项目一贯的"宁缺勿错"，**不做**：等有真实 Kafka 可验时再补，届时 `ClusterName` 字段一并加回。

**② Server 建资产与边**：`asset/model.go` 新增 `TypeMiddlewareCluster`（内置类型「中间件集群」）、
`MiddlewareClusterNaturalKey(kind, name)` = `<类型>:<集群名>`，**不进 `EphemeralTypes`**（成员全消失是
真失联）；`receiver/assets.go` 的 `applyAssets` 里：从本轮实例清单里抽出有 `ClusterName` 的集群集合
（`clusterPlan`）→ **先落集群资产**（归属节点落**空串**，D4）→ 落实例（属性里多一个 `cluster`）→
建 `member_of` 边（复用 `instancePlan.clusterRef`，与 `linkInstance` 同一轮）。

**③ 前端与文档**：`AssetListView.vue` 的 `PEER_TYPE_LABELS` 与 `AssetTopologyView.vue` 的
`TYPE_TITLES` 补 `middleware-cluster`；`AssetListView.vue:577` 那句补上 `member_of` 的第二个来源
（容器 → 工作负载、**实例 → 集群**）。

**测试**：`go test ./...` 全绿；新增 2 条（`receiver/mw_cluster_test.go`）：**只有权威集群名才建集群**
（MySQL GR 两个成员 → 一份资产两条边；Mongo 副本集 → 一份；**单机 Mongo、redis 的本地别名都不建**，
这是本批最该被挡住的事）、幂等（连报两轮边数不变）、归属节点落空串、实例上带 `cluster` 属性且
`group`（本地别名）**保持原样**、集群类型不进短命对象。前端**未新增用例**（只加了两处标签映射与一句文案，
上一批那条"关系总览里新类型显示中文名"的用例覆盖的是同一处机制）。

**未覆盖（不得视为通过）**：**Elasticsearch 与 Kafka 本机都没有实例**——ES 的链路只有用例与映射覆盖，
Kafka 直接不做；集群资产对受限用户不可见（D4 的已知代价，靠实例上的 `cluster` 属性补偿）；
同一实例属于多个集群（D8 不处理）；浏览器实点。

**待办**：出包（**Server 与 Agent 同升**）后在 dev-server 按 §6.2 跑实机核对——第一段"不猜"是重点。
