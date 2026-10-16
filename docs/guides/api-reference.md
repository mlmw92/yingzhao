# API 参考

> 本文件由仓库根目录 `README.md` 的「API」章节下沉而来（2026-10-16）：接口清单属于**查阅型**内容。
> 部署、升级与配置见 [README](../../README.md)；权限点含义见 [权限矩阵](permission-matrix.md)。


接口统一以 `/api/v1` 为前缀（WebSocket、前端静态资源与 Agent 下发路径除外）。除下表标注「公开」的接口外均需登录会话，请求头携带 `Authorization: Bearer <token>`；标注权限点的接口在服务端额外校验权限，资源范围（节点分组）过滤同样在服务端执行。

**公开白名单**：`POST /api/v1/login`、`POST /api/v1/report`（Agent 上报，走 `X-Agent-Secret`）、`GET /api/v1/agent/check`、`GET /api/v1/ui/settings`（匿名只读）、`GET /healthz`、`GET /readyz`、`/install/*`、`/bin/*`、前端静态资源。

> **健康探针**：`GET /healthz` 为存活探针（仅表明进程仍在服务，不做依赖检查）；`GET /readyz` 为就绪探针（校验时序库可用性与告警评估节拍，未就绪时返回 503 并给出逐项原因）。两者免登录——k8s、systemd 与反向代理通常无法携带登录令牌。
>
> **自监控**：Server 自身指标以 `self_*` 前缀每 30 秒写入时序库，因此可直接用「指标浏览」查看趋势，也能用现有告警规则监控 Server 自身（如 `self_alert_eval_age_seconds` 停摆、`self_tsdb_write_errors_total` 增长）。即时快照见 `GET /api/v1/self/status` 与「系统设置 → 系统自监控」。

## 公开接口

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v1/login` | 登录换取访问令牌 |
| POST | `/api/v1/report` | Agent 指标上报（`X-Agent-Secret` 校验，不受登录令牌影响） |
| GET | `/api/v1/agent/check` | Agent 接入鉴权预检 |
| GET | `/healthz` | 存活探针（进程可服务即 200） |
| GET | `/readyz` | 就绪探针（时序库 / 告警评估节拍，未就绪返回 503 + 逐项原因） |
| GET | `/install/agent-install.sh` | 下发 Agent 安装脚本 |
| GET | `/bin/` | 下发各架构 Agent 二进制 |
| GET | `/` | 前端静态资源（SPA 回退 index.html） |

## 主机与节点

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/nodes` | 节点列表与在线状态 |
| GET | `/api/v1/nodes/latest` | 全节点关键指标聚合 |
| GET | `/api/v1/nodes/{name}` | 节点详情 |
| DELETE | `/api/v1/nodes/{name}` | 移除节点 |
| PUT | `/api/v1/nodes/{name}/group` | 修改节点分组 |
| PUT | `/api/v1/nodes/{name}/display-name` | 设置节点别名 |
| POST | `/api/v1/nodes/{name}/upgrade` | 升级指定节点的 Agent |
| POST | `/api/v1/nodes/upgrade` | 批量升级 Agent |
| GET | `/api/v1/groups` | 节点分组列表 |
| POST | `/api/v1/groups` | 创建节点分组 |
| DELETE | `/api/v1/groups/{name}` | 删除节点分组 |

## 指标查询与浏览

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/query/range?node=&metric=&start=&end=&step=` | 历史范围查询 |
| GET | `/api/v1/query/latest?node=&metric=` | 最新点 |
| GET | `/api/v1/processes?node=` | 进程 TOP |
| GET | `/api/v1/query/listeners?node=` | 端口监听列表 |
| GET | `/api/v1/query/firewall?node=` | 防火墙规则 |
| GET | `/api/v1/query/firewall/status?node=` | 防火墙运行状态 |
| GET | `/api/v1/metrics/catalog` | 指标目录（可采集指标定义） |
| GET | `/api/v1/metrics/active` | 最近有数据上报的指标 |
| GET | `/api/v1/metrics/export?node=&metric=&start=&end=` | 导出指标 CSV |

## 资产与配置巡检

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/assets?type=&node=&keyword=&label=&ignored=&limit=&offset=` | 资产列表（服务端分页；`label=key\|key:value` 按标签筛；`ignored=with\|only` 控制已忽略可见性，默认隐藏） |
| GET | `/api/v1/assets/summary?...` | 台账健康度（总数 / 失联 / 无责任人 / 冲突 / 近 7 天变更） |
| GET | `/api/v1/assets/{id}` | 资产详情（属性双来源 + 生效值 + 派生状态/来源/责任人） |
| GET | `/api/v1/assets/{id}/history?limit=` | 字段级变更历史（含操作人） |
| GET | `/api/v1/assets/{id}/links` | 直接关联（出边 + 入边；范围外对端不返回） |
| GET | `/api/v1/assets/{id}/snapshots?limit=` | 配置快照列表（差异巡检的基线） |
| POST | `/api/v1/assets` | 手工新建资产（`assets:write`，归属节点须在范围内） |
| PUT | `/api/v1/assets/{id}` | 维护人工值 / 恢复采集值（`assets:write`；不接受改归属节点） |
| POST | `/api/v1/assets/{id}/baseline` | 设为该资产类型的期望值（标杆，`assets:write`） |
| DELETE | `/api/v1/assets/{id}/baseline` | 清除该资产类型的期望值（`assets:write`） |
| POST | `/api/v1/assets/{id}/ignore` | 忽略（从台账隐藏，可恢复；采集继续，`assets:write`） |
| POST | `/api/v1/assets/{id}/restore` | 解除忽略（幂等，`assets:write`） |
| POST | `/api/v1/assets/{id}/purge` | 彻底删除（**仅纯人工建档资产**；采集资产 409 并提示改用忽略，`assets:write`） |
| PUT | `/api/v1/assets/{id}/labels` | 维护标签（`labels` 写入/覆盖 + `remove` 删除，`assets:write`） |
| POST | `/api/v1/assets/batch` | 批量维护（`op` = `owner`/`ownerClear`/`labels`/`ignore`/`restore`；逐条结论，单批 ≤ 200 条，`assets:write`） |
| GET | `/api/v1/assets/export?<同列表筛选>` | 导出资产清单 CSV（**服务端不分页**，只含资源范围内的资产；`assets:export`，高风险） |
| POST | `/api/v1/inspect/runs` | 触发一次配置巡检（`inspect:run`；范围同台账筛选） |
| GET | `/api/v1/inspect/runs?limit=` | 巡检记录（`inspect:read`） |
| GET | `/api/v1/inspect/runs/{id}/findings?limit=` | 差异项（按严重级别排序） |
| GET | `/api/v1/inspect/baselines` | 各资产类型当前的期望值来源 |

## 中间件监控

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/middleware/overview` | 健康度总览（十类组件实例数 / 在线数 / 告警数聚合） |
| GET | `/api/v1/middleware/redis/instances` | Redis 实例列表（含拓扑角色识别） |
| GET | `/api/v1/middleware/mysql/instances` | MySQL 实例列表（含 Group Replication 角色） |
| GET | `/api/v1/middleware/postgres/instances` | PostgreSQL 实例列表（含复制角色与延迟） |
| GET | `/api/v1/middleware/nginx/instances` | Nginx 实例列表 |
| GET | `/api/v1/middleware/kafka/instances` | Kafka 实例列表 |
| GET | `/api/v1/middleware/docker/containers` | Docker 容器列表 |
| GET | `/api/v1/middleware/rocketmq/instances` | RocketMQ 实例列表 |
| GET | `/api/v1/middleware/k8s/instances` | Kubernetes 集群列表（集群聚合 + Node / 异常 Pod 明细） |
| GET | `/api/v1/middleware/mongodb/instances` | MongoDB 实例列表（含副本集角色） |
| GET | `/api/v1/middleware/fastdfs/instances` | FastDFS 实例列表 |
| GET | `/api/v1/middleware/rabbitmq/instances` | RabbitMQ 实例列表 |
| GET | `/api/v1/middleware/elasticsearch/instances` | Elasticsearch 实例列表（集群状态/分片） |
| GET | `/api/v1/middleware/clickhouse/instances` | ClickHouse 实例列表（连接/查询/合并） |
| GET | `/api/v1/middleware/nacos/instances` | Nacos 实例列表（存活） |
| GET | `/api/v1/middleware/zookeeper/instances` | ZooKeeper 实例列表（角色/延迟/znode） |
| GET | `/api/v1/middleware/view-config` | 中间件监控页面展示开关查询 |
| PUT | `/api/v1/middleware/view-config` | 保存展示开关（`system:config`） |
| GET | `/api/v1/middleware/nginx/access/summary` | Nginx 访问日志汇总（总请求 / 速率 / 状态码分布 / Top URI / Top IP） |
| GET | `/api/v1/middleware/nginx/access/geo?scope=cn\|world` | 请求来源地理分布（热力点 / 部署点 / 动线） |
| POST | `/api/v1/logs` | 集中日志上行（Agent → Server，走 `X-Agent-Secret`，非浏览器接口） |
| GET | `/api/v1/status` | 对外状态页数据（**免登录**；仅返回已勾选「对外发布」的拨测任务概览） |
| GET | `/api/v1/logs?from&to&q\|regex&nodes&sources&limit&cursor` | 集中日志检索（`logs:read` + 节点分组范围；游标翻页，命中上限时返回 `truncated`；响应含 `assets` 来源→资产映射） |
| GET | `/api/v1/logs/fields?sources` | 结构化字段候选（只列服务端见过的名字） |
| GET | `/api/v1/logs/rule-template?source&pattern&node` | 由日志模式生成告警规则模板（`logs:read`；指标名 `<来源>_log_<模式>_total` 由服务端拼，与 Agent 产出同源） |
| GET | `/api/v1/alerts/impact?node&instance&depth` | 告警影响面：命中资产 + 近期变更 + 波及范围（`assets:read`；标签寻址，范围外不出现） |
| GET | `/api/v1/assets/{id}/topology?depth&limit` | 资产关系图：以该资产为中心的 N 跳邻域（`assets:read`；跳数 ≤3、节点 ≤500，截断显式回传） |
| GET / PUT | `/api/v1/report/schedule` | 报告周期化调度（读 `report:read`，改 `report:export`；配置与运行状态同文件） |
| POST | `/api/v1/report/schedule/run` | 立即生成一次报告（`report:export`；生成失败返回 502） |

## 智能分析

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/analysis/summary?windowHours=24\|168\|720&refresh=true\|false` | 分析摘要与按风险排序的主机结论（动态基线、容量预测、风险证据、根因关联线索） |
| GET | `/api/v1/analysis/hosts/{name}?windowHours=24\|168\|720&refresh=true\|false` | 指定主机的分析详情；受节点分组访问范围限制 |

## 告警

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/alerts?state=active` | 告警事件（含 `suppressed` / `suppressedBy` 抑制状态） |
| GET | `/api/v1/alerts/stats` | 告警统计看板 |
| GET | `/api/v1/alerts/acks` | 告警处置记录（状态 / 处理人 / 关闭原因 / 评论） |
| POST | `/api/v1/alerts/ack` | 认领告警（可同时指派处理人与评论） |
| POST | `/api/v1/alerts/close` | 关闭告警（可记录原因） |
| POST | `/api/v1/alerts/reopen` | 重新打开告警，回到待处理 |
| POST | `/api/v1/alerts/comment` | 追加处置评论 |
| POST | `/api/v1/alerts/test` | 触发一条测试告警 |
| GET | `/api/v1/rules` | 规则列表 |
| POST | `/api/v1/rules` | 新建规则 |
| PUT | `/api/v1/rules/{id}` | 更新规则 |
| DELETE | `/api/v1/rules/{id}` | 删除规则 |
| POST | `/api/v1/rules/{id}/toggle` | 启用 / 停用规则 |
| POST | `/api/v1/rules/{id}/toggle-silence` | 静音 / 取消静音规则 |
| GET | `/api/v1/rules/export` | 导出规则 |
| POST | `/api/v1/rules/import` | 导入规则 |
| GET | `/api/v1/rules/templates` | 规则模板列表（65 条，按中间件分组，带触发条件与阈值依据） |
| GET | `/api/v1/inhibit` | 抑制规则查询 |
| PUT | `/api/v1/inhibit` | 抑制规则全量更新（热生效） |
| GET | `/api/v1/grouping` | 告警分组配置查询 |
| PUT | `/api/v1/grouping` | 告警分组配置更新（热生效） |
| GET | `/api/v1/alert-pipeline` | 事件管道配置查询 |
| PUT | `/api/v1/alert-pipeline` | 事件管道配置保存（热生效） |
| POST | `/api/v1/alert-pipeline/preview` | 按指定配置试算管道效果（不落盘） |

> 事件管道的**场景化示例模板**与可用变量（内置标签、事件字段、`{{ts .StartsAt}}` 时间格式化）
> 见 `docs/guides/configuration-cookbook.md`；页面上「站点与品牌 → 告警事件管道」也内置了
> 「示例模板」下拉，可先预览再一键追加（追加不覆盖已有规则）。

| GET | `/api/v1/maintenance` | 维护窗口查询 |
| PUT | `/api/v1/maintenance` | 维护窗口设置 |
| GET | `/api/v1/notify` | 通知配置查询 |
| PUT | `/api/v1/notify` | 通知配置保存（热生效） |
| POST | `/api/v1/notify/test` | 发送测试通知 |

## 告警规则的指标从哪来（指标字典）

规则表单里的指标下拉来自**服务端指标字典** `internal/server/metrics/`（`GET /api/v1/metrics/catalog`
的 `alertGroups`），不是前端硬编码的一张短表——后者曾只覆盖 7 组指标，导致 MySQL/Nginx/Kafka/ES/拨测证书
一条都选不到，而界面上看不出「少了什么」。

- **两条守卫固定住这个不变量**：目录里出现的每个指标名，必须能在 `internal/agent/collector`、
  `internal/server/receiver`、`internal/server/dialtest` 里找到产出方（名字写错的症状是静默失效：
  指标浏览查不到数据、规则永不触发，都不报错）；每条规则模板的运算符方向必须与指标的
  **变差方向**（`WorseWhen`）一致（给「证书剩余天数」配 `>` 是这类模板最经典的错）。
- **两类指标有标记**：「不常设阈值」（累计计数器 / 容量 / 运行时长）与「按维度命名」
  （`mongodb_opcounters_query`、`nginx_access_requests_by_status`、`<来源>_log_<模式>_total`，
  需要把占位替换成实际值）。手输不在字典里的名字（exporter 透传名等）是允许的，但表单会给出
  橙色提示——那是"规则永不触发"最隐蔽的来源。
- **阈值怎么填**：`docs/guides/configuration-cookbook.md` §三 给出了各中间件的建议阈值、方向与理由，
  规则表单的「新建规则」下拉里也能直接套用这 65 条模板（自动创建的只有其中 20 条跨环境通用的）。

## 节点操作（统一下行通道）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/ops/actions?node=&nodes=a,b,c` | 动作目录；带 `node`/`nodes` 时附带这些节点**本机放行**的动作清单（`ops:read`） |
| GET | `/api/v1/ops/tasks?node=&state=&kind=&batchId=&limit=` | 操作任务列表（按资源范围过滤，`ops:read`） |
| GET | `/api/v1/ops/tasks/{id}` | 任务详情（含执行输出，`ops:read`） |
| POST | `/api/v1/ops/tasks` | 下发一条操作任务 `{node,kind,params,reason}`（`ops:exec`，**高风险**） |
| POST | `/api/v1/ops/tasks/batch` | 批量下发 `{nodes,kind,params,reason}`，逐节点返回结论；最多 200 台（`ops:exec`） |
| POST | `/api/v1/ops/tasks/cancel` | 取消 `{ids}` 或 `{batchId}`；**仅排队中可撤**，已下发的逐条报"无法取消"（`ops:exec`） |
| DELETE | `/api/v1/ops/tasks/{id}` | 删除一条**已结束**的任务记录（写审计，`ops:exec`） |

> **四道护栏**：① Agent 本机护栏 `guards.ops`（默认只读，写操作需 `write: true` **且**单元在 `units` 清单）；
> ② 能力协商（Agent 只声明本机放行的动作，Server 只下发声明过的）；③ 中心授权 + 参数校验 + 审计
> （`ops:exec` 已入 `HighRiskPermissions`）；④ 首批动作默认只读。
> 指令走「上报响应搭车」（与 defense/upgrade 同构，ADR-0003），**不新增长连接与入站端口**，
> 代价是指令延迟 = 一个采集周期（默认 15s）。节点离线时下发会被直接拒绝（409），
> 因为它不可能被领取。配置与排障见 `docs/guides/configuration-cookbook.md` §四。
>
> **启用双人复核后**：需要审批的写类动作提交返回 **`202 + approvalId`**（**不是 `201`**），
> 表示"已提交审批"而**不是**"已下发"——审批通过后才会真正下发（执行时 `Operator` 记**申请人**，
> 审批链另存）。**批量下发同样走审批**：否则"单条要审批、一次发 200 台反而不需要"，
> 这条护栏就形同虚设。

## 安全中心与审计

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/security/summary` | 安全态势概览 |
| GET | `/api/v1/security/events` | 安全事件列表 |
| GET | `/api/v1/security/baselines` | 安全基线评分 |
| GET | `/api/v1/security/baselines/export` | 导出合规矩阵（节点 × 检查项，CSV）；需 `security:export`，该节点没上报的项**留空 ≠ 未通过** |
| GET | `/api/v1/security/guard-risks` | 各节点**护栏组合风险**（可写目录 + 可重启单元 = 任意代码执行）。`reported=false` 表示该节点**从未上报过**（旧 Agent），与「已检查、无风险」**必须分开看** |
| GET | `/api/v1/security/defense/status` | 全节点入侵防护状态 |
| GET | `/api/v1/security/defense/status/{node}` | 单节点入侵防护状态 |
| POST | `/api/v1/security/defense/{node}/{action}` | 下发防护指令（`enable` / `disable` / `status`） |
| GET | `/api/v1/security/defense/tasks` | 防护任务列表 |
| GET | `/api/v1/security/defense/tasks/{node}` | 按节点查询防护任务 |
| GET | `/api/v1/audit/events` | 管理操作审计记录（支持 `format=csv`，此时额外要求 `audit:export`） |
| GET | `/api/v1/audit/export` | 导出审计记录（CSV） |
| GET | `/api/v1/audit/verify` | **审计完整性校验**（防篡改链）：报出**覆盖区间与条数**、断裂逐条归因、密钥指纹。**未启用/降级模式时 `enabled=false` 且 `ok=false`**——它不会把"没有保护"说成"安全" |

## 审批（高风险操作双人复核）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/approvals` | 审批单列表（可筛「待我审批」/「我的申请」） |
| GET | `/api/v1/approvals/{id}` | 审批单详情（含动作参数、危险参数与理由、审批链） |
| POST | `/api/v1/approvals/{id}/approve` | 批准（`approval:approve`，且**需同时持有该动作本身的权限点**）；批准即自动执行 |
| POST | `/api/v1/approvals/{id}/reject` | 驳回 |
| POST | `/api/v1/approvals/{id}/cancel` | 撤回自己的申请 |
| GET / PUT | `/api/v1/approvals/config` | 审批开关与范围（改配置单设 `approval:manage`：关掉审批等于关掉这条护栏） |

> **没有** `POST /api/v1/approvals`：审批单是**动作提交时自动生成**的——需要审批的动作提交后返回
> **`202 + approvalId`**（而不是 `201`），前端据此提示「已提交审批」。申请人不能批准自己的单子。

## 拨测与巡检报告

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/dialtest/tasks` | 拨测任务列表 |
| POST | `/api/v1/dialtest/tasks` | 创建拨测任务 |
| PUT | `/api/v1/dialtest/tasks/{id}` | 更新拨测任务 |
| DELETE | `/api/v1/dialtest/tasks/{id}` | 删除拨测任务 |
| GET | `/api/v1/dialtest/latest` | 最近拨测结果 |
| POST | `/api/v1/report/generate` | 生成巡检报告 |
| GET | `/api/v1/report/download` | 下载报告 HTML |
| GET | `/api/v1/report/history` | 报告历史列表 |

## 升级、配置与展示

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/version` | Server 版本信息 |
| GET | `/api/v1/self/status` | 自监控快照（进程 / HTTP / 时序库 / 通知 / 告警 / 节点统计，`dashboard:read`） |
| GET | `/api/v1/install-info` | Agent 安装信息（serverURL + 一行命令 + 代理配置模板） |
| GET | `/api/v1/proxy/status` | 代理节点状态（Edge / Hub 自监控指标聚合） |
| POST | `/api/v1/system/upgrade/upload` | 上传升级包（multipart） |
| GET | `/api/v1/system/upgrade/current` | 当前待应用升级包 |
| POST | `/api/v1/system/upgrade/apply` | 立即应用升级并重启 |
| GET | `/api/v1/system/upgrade/history` | 升级历史 |
| GET | `/api/v1/system/upgrade/archive` | 已归档（可切换）版本列表 |
| POST | `/api/v1/system/upgrade/archive/delete` | 删除指定版本的升级包（删除后不可再切换回该版本） |
| POST | `/api/v1/system/upgrade/rollback-to` | 切换到指定归档版本 |
| GET | `/api/v1/system/retention` | 数据保留策略与各类数据现状（`system:config`） |
| PUT | `/api/v1/system/retention` | 保存数据保留策略（热生效） |
| POST | `/api/v1/system/retention/cleanup` | 立即执行一次清理，返回删除数量 |
| GET | `/api/v1/system/geoip` | IP 地理库状态 |
| POST | `/api/v1/system/geoip/upload` | 上传 IP 地理库 |
| POST | `/api/v1/system/geoip/reset` | 重置 IP 地理库 |
| GET | `/api/v1/system/geoip/test?ip=` | 查询指定 IP 的归属地 |
| GET | `/api/v1/ui/settings` | 品牌配置查询（GET 允许匿名只读） |
| PUT | `/api/v1/ui/settings` | 品牌配置保存 |
| GET | `/api/v1/screen/config` | 数据大屏配置查询 |
| PUT | `/api/v1/screen/config` | 数据大屏配置保存 |
| GET | `/api/v1/dashboards` | 自定义仪表盘列表 |
| POST | `/api/v1/dashboards` | 创建仪表盘 |
| GET | `/api/v1/dashboards/{id}` | 仪表盘详情 |
| PUT | `/api/v1/dashboards/{id}` | 更新仪表盘 |
| DELETE | `/api/v1/dashboards/{id}` | 删除仪表盘 |

## 认证与权限（RBAC）

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v1/logout` | 注销当前会话 |
| GET | `/api/v1/auth-info` | 查询是否启用登录认证 |
| POST | `/api/v1/auth/change-password` | 修改当前用户密码 |
| GET | `/api/v1/auth/me` | 当前用户信息（含权限点与资源范围） |
| PUT | `/api/v1/auth/me` | 更新当前用户资料 |
| GET | `/api/v1/users` | 用户列表（`users:manage`） |
| POST | `/api/v1/users` | 创建用户（`users:manage`） |
| GET | `/api/v1/users/{username}` | 用户详情（`users:manage`） |
| PUT | `/api/v1/users/{username}` | 更新用户（`users:manage`） |
| DELETE | `/api/v1/users/{username}` | 删除用户（`users:manage`） |
| POST | `/api/v1/users/{username}/reset-password` | 重置用户密码（`users:manage`） |
| POST | `/api/v1/users/{username}/disable` | 禁用用户（`users:manage`） |
| POST | `/api/v1/users/{username}/enable` | 启用用户（`users:manage`） |
| GET | `/api/v1/roles` | 角色列表（`roles:read`） |
| POST | `/api/v1/roles` | 创建角色（`roles:manage`） |
| GET | `/api/v1/roles/{name}` | 角色详情（`roles:read`） |
| PUT | `/api/v1/roles/{name}` | 更新角色（`roles:manage`） |
| DELETE | `/api/v1/roles/{name}` | 删除角色（`roles:manage`） |
| GET | `/api/v1/permissions/catalog` | 权限点目录（`roles:read`） |

## WebSocket

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/ws?topic=metrics&node=` | 实时指标推送 |
| GET | `/ws?topic=alerts` | 告警广播 |
