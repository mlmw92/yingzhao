# 资产 / 容器 / 日志报表化视图设计件

- 批次：28（待排期；spool-drain = 批次 27 之后的首个新功能批次）
- 日期：2026-10-08
- 对应全景表：`2026-09-30-ops-platform-capability-map.md` §3.12「资产/容器/日志的报表化视图」（**未实现** / P2 / M / 归属 D1）
- 状态：**已评审通过（2026-10-08，D1~D8 全部按建议执行）**——D1 扩展报告章节、D2 三章全做、D3 路线 A（定时按保存者快照）、D4 缺权限章节跳过并标注、D5 容器接受快照口径、D6 日志不做按天扫描、D7 本期仅 HTML、D8 章节开关默认全开可显式关闭。

## 1. 起因

全景表 §3.12 那一行只有一句：`资产/容器/日志的报表化视图 | 未实现 | 依赖 D2/D3 数据源 | P2 | M | D2/D3 | D1`。

它说的是：**D2（资产与配置）与 D3（容器与可观测）的数据已经齐了，但只能"一屏一屏地看"，没有"一份东西能交出去"**。具体缺口有三处：

1. **报告里没有这三域**。现有巡检报告（`internal/server/report/report.go`）覆盖主机、中间件、安全、智能分析，但**没有任何资产、容器、日志章节**——`report` 包内 grep `asset|container|log` 只命中 `docker_containers_*` 指标与 `log/slog`。
2. **报告拿不走按范围的口径**。报告生成**始终覆盖全部节点**（`report.go:443` 调 `nodeMgr.ListNodes()`，无分组/标签过滤），而平台其余位置早已按资源范围收窄。给一个只负责某几个分组的团队出报告，现在做不到。
3. **三个域各自的聚合散在各处**，没有一份把它们拼起来的视图（资产看 `/assets/summary`、容器看 `/middleware/k8s/instances`、日志看 `/logs/backend`）。

## 2. 现状（有依据）

### 2.1 报告子系统能承载什么（**可复用度最高**）

| 事实 | 依据 |
|---|---|
| 章节是**硬编码**的固定结构，不是可插拔切片 | `internal/server/report/report.go:140-154`（`reportData` 固定字段）+ `report.go:438` 的 `collectData` 逐个显式调用各 collector |
| 模板是 `go:embed` 的单文件 HTML，章节 = 一个 `<div class="section">` | `report.go:25-26`、`template.html`（`<h2>` 锚点见 `:118/137/165/182/196/227/277/311/346`） |
| 图表是**自包含内联 SVG**，无外部 JS 依赖 | `internal/server/report/charts.go`（`lineChart` 等） |
| 取数只有三类：时序库 PromQL、节点管理器、安全库 | `report.go:460-472`（`storage.QueryRange`）、`:443/481`（`nodeMgr`）、`:719`（`secStore`） |
| **周期化调度 + 历史 + 保留 + 下载**已齐备 | `report/schedule.go:30-41`（配置与运行状态同文件）、`report.go:296-297`（`{type}-{YYYYMMDD-HHMMSS}.html`）、`:415-434`（`history.json`，内存留 50 条）、`retention/retention.go:42`（`ReportsDays` 默认 180 天） |
| 接口与权限面 | `POST /api/v1/report/generate`（`report:export`）、`GET /report/download`（`report:export`）、`GET /report/history`（`report:read`）、`GET/PUT /report/schedule` + `POST /report/schedule/run`（读 `report:read`，写与跑 `report:export`）——`api/middleware_api.go:1511-1544`、`api/report_schedule_api.go` |
| 前端整份 HTML 塞进 iframe 预览 | `web/src/components/ReportView.vue:109-116`、`:225-233` |
| **报告生成不接资源范围** | `Generate(rt ReportType)` 只收类型（`report.go:280`）；`handleReportGenerate` 只解析 `type`（`middleware_api.go:1512-1514`） |
| **调度配置里没有保存者身份** | `report/schedule.go:30-41` 的 `ScheduleConfig` 只有 `Enabled/Type/IntervalHours` + 运行状态；对比配置巡检的 `asset/inspect_schedule.go` 有 `Filter` 快照与 `actor` 留痕（`InspectScheduleActor = "schedule"`，`:40`） |

### 2.2 三域可取的数据（**注意哪些有时间维度**）

| 域 | 现成聚合 | 时间维度 | 依据 |
|---|---|---|---|
| **资产** | `GET /api/v1/assets/summary`（`assets:read`）→ `Stats{Total, Missing, NoOwner, Conflict, Changes, Ignored}`；`GET /api/v1/assets/link-stats`（`assets:read`）→ 按 `FromType×Kind×ToType×Source` 分组计数 + `assetsWithoutLinks` | **有**：`asset_changes.at` 支持窗口统计（`Stats.Changes` 就是 7 天窗口，`asset/store.go:1416-1420`）；`snapshots.taken_at` 支持快照对比 | `api/asset_api.go:460-477`、`:664-695`；`asset/service.go:136-148`；`asset/store.go:83-95`（表列） |
| **容器** | `GET /api/v1/container/k8s/clusters`（`container:read`）→ 集群清单 + `up`；`GET /api/v1/middleware/k8s/instances`（`middleware:read`）→ `nodesTotal/Ready`、`podsTotal/Running/Pending/Failed/Abnormal`、工作负载健康 | **无**：两个接口都是 `QueryAllLatest` **即时查询**（当前快照）；台账里 `pod/workload/service` 与 Docker 容器资产有 `last_seen` 与 `asset_changes`，但那是"资产视角"的增删，不是容器指标历史 | `api/container_api.go:28-80`；`api/middleware_api.go:1548+`；`asset/model.go:80-103/115`（`EphemeralTypes()={pod,workload}`） |
| **日志** | `GET /api/v1/logs/backend`（`logs:read`）→ `capabilities{fullTextIndex,fieldIndex,notes,scanBudget}` + `storage{sources,nodes,oldestDay,newestDay,bytes}` | **一半**：分片按天（`root/<source>/<date>/<node>.log`）但**只报首尾日期与字节合计，没有每日行数/来源数**，也没有任何聚合端点；**可做周期对比的只有"日志告警指标"**（`<来源>_log_<模式>_total`，来自时序库） | `api/logs_api.go:69-98`；`logstore/backend.go:86-112`；`logstore/capability.go:44-104`、`:116`（VL 后端不报字节与跨度） |

### 2.3 两个不能作为载体的展示层（**别再考虑它们**）

- **自定义仪表盘**：`dashboard.Panel` 恒等于「一个指标 + 一个 range 查询」（`internal/server/dashboard/store.go:32-41`，数据源写死在 `web/src/components/dashboard/PanelChart.vue:41-78` 的 `/api/v1/query/range`）。**没有"数据源"抽象**，加资产/容器/日志属结构性改动；且 `PanelChart` 目前把 series 类型硬编码成 `line`（`:65-73`），`chartType` 存了不生效。
- **数据大屏**：`screen.go` 只有配置接口（`system:config`），板块在 `ScreenView.vue` 里写死（5 个 Tab），`DefaultScreenConfig` 的 5 个模块 key 里实际只有 `alerts` 生效。它是**强定制展示壳，不是通用报表容器**。

> 可复用的是 `web/src/charts/echarts.js`（ECharts 5.6 封装 + 主题令牌）——**但报告侧不需要它**（报告用自包含 SVG，离线可看）。

## 3. 范围

**做**：

1. **在现有巡检报告里新增三个章节**：资产与配置、容器与工作负载、日志。
2. **让报告支持按资源范围生成**（手动生成按生成者范围收窄）——这是本批顺带修掉的既有缺口，也是三章节能正确落地的前提。
3. **权限不放大**：生成者缺少某域权限点时，该章节**跳过并标注原因**，不静默输出。

**不做**（明确划线）：

- **不新建独立报表页**、**不改造自定义仪表盘的数据源**、**不改数据大屏**（理由见 §2.3）。
- **不做 PDF**（要排版引擎 + 中文字体，与 P2/M 不符；HTML 可直接打印，`ReportView.vue:240-248` 已有打印）。
- **明细内容不进报告**：资产属性值、容器环境变量/ConfigMap 值、日志正文一律不进（报告是会被下载转发的文件，明细属于"要看就去对应页面看"）。
- **不新建日志按天扫描统计**（要扫分片，成本与"P2/M"不符）；**不为容器新建历史留存**（时序成本高，收益未证明）。
- **不做"上游根因"类分析**（需链路追踪 P3）。

## 4. 设计

### 4.1 载体与总原则

**载体：扩展巡检报告章节**。理由：周期化调度、历史、保留、下载、打印、iframe 预览**全部现成**（§2.1），而三章节只需在 `reportData` 加三个可选字段 + `collectData` 加三个 collector + `template.html` 加三段；这比新造一个页面（还要重做调度/历史/权限）便宜得多。

**四条总原则**：

1. **只放聚合，不放明细**（见 §3 不做）。
2. **有历史的做周期对比，没有的显式标注「当前快照」**——不允许用"看起来像趋势"的静态数字骗人。
3. **范围与权限两道闸**：数据按生成者可见范围下推；生成者缺权限的章节跳过并写明。
4. **报告头部标注生成口径**（生成者、范围、是否含跳过章节），让这份文件自己能解释自己。

### 4.2 资产与配置章节（`AssetsSection`）

| 内容 | 取数 | 口径 |
|---|---|---|
| 健康度摘要 | `asset.Service.Stats`（与 `/assets/summary` 同一实现） | `Total / Missing / NoOwner / Conflict / Ignored` 五计数 + `Changes`（窗口内变更数） |
| 近 7 天变更 Top | `asset_changes` 按 `field` 分组计数（取 Top N，N=10） | **有周期语义**；只给字段名与计数，不给旧值/新值 |
| 关系总览 | `asset.Service.LinkOverview`（与 `/assets/link-stats` 同一实现） | 按 `来源类型 × 关系种类 × 目标类型 × 边来源` 分组计数 + `assetsWithoutLinks` |
| 配置巡检差异 | `inspect_runs` / `inspect_findings` 最近一次运行 | 按 `level`（added/changed/missing/deviation）计数 + 本次巡检资产数与时间；**不给具体差异条目**（明细在配置巡检页） |

**为什么选这四项**：它们都是**已有的聚合实现**（不新增查询逻辑），且都**不含属性值**。

### 4.3 容器与工作负载章节（`ContainerSection`）

| 内容 | 取数 | 口径 |
|---|---|---|
| 集群清单 | `container/k8s/clusters` 同一实现 | 集群名 / 版本 / `up` / 上报节点 |
| K8s 聚合 | `middleware/k8s/instances` 同一实现 | `nodesTotal/Ready`、`podsTotal/Running/Pending/Failed/Abnormal`、Deployment/StatefulSet/DaemonSet 健康计数 |
| 台账容器资产计数 | `asset` 按类型计数（`pod` / `workload` / `service` / Docker 容器） | 只计数；`pod`/`workload` 是短命对象，**沿用 `EphemeralTypes` 不计入健康度摘要的口径**，但**在报告里单独列出**（报告正是要看清它们的场合） |

**硬要求：本章节整体标注「当前快照（无历史对比）」**——这两个接口都是即时查询，报告只能反映"生成那一刻"的状态。

### 4.4 日志章节（`LogsSection`）

| 内容 | 取数 | 口径 |
|---|---|---|
| 后端能力自述 | `logstore.LogStore.Backend()` + `capabilities` | 如实转述（`backend`、`fullTextIndex`、`scanBudget`），不替后端下结论 |
| 存储概览 | `logstore` 的 `StorageStats` | `sources / nodes / oldestDay / newestDay / bytes`；**VL 后端这些字段为空时要渲染成「该后端不报告」而不是 0**（`capability.go:116`） |
| 日志告警命中 | 时序库：报告窗口内 `<来源>_log_<模式>_total` 的增量 | **有周期语义**；只给模式名与命中数，不给日志正文 |
| 保留策略 | `retention` 的 `logsDays` | 让看报告的人知道"这些日志还能追多久" |

### 4.5 范围收窄与权限（**本期必须一起做**）

这是本批**唯一有架构影响的改动**，分两半：

**(a) 范围收窄**

- **手动生成**：`handleReportGenerate` 已有 `Principal`，把调用者可见范围传入 `Generator`，三章节各自下推（资产走 `assetWhere` 同款条件、容器与日志走节点范围）。
- **定时生成**：`ScheduleConfig` 目前**没有保存者字段**（§2.1）。两条路：
  - 路线 A（建议）：**照抄配置巡检**——`ScheduleConfig` 增加范围快照字段，保存时**按保存者折算并覆盖客户端提交值**；定时触发用 `InspectScheduleActor` 同款的 `"schedule"` 留痕，范围 = 保存时的快照 ∩ 保存者当前范围。与 `2026-10-07-inspect-schedule-design.md` 的三条偏离完全同构（**不执行**而不是跑一份空报告、手动额外按点击者收窄、保存时直接覆盖）。
  - 路线 B（更小）：**本期只做手动**，定时报告维持全局，并在界面与报告头部**写明「定时报告覆盖全部节点」**。
- 另：**报告正文的既有章节（主机/中间件/安全）也应一起收窄**——否则会出现"资产章节按范围、主机章节全局"的割裂。这会把本批从"加三节"变成"给报告加范围能力"，是本批的主要工作量。

**(b) 权限不放大**

报告的权限面是 `report:read` / `report:export`，而三章节数据分属 `assets:read` / `container:read` / `logs:read`。**硬规则**：生成时逐章节检查生成者权限，**缺哪个域就跳过哪个章节**，并在该章节位置渲染一行「**无 `logs:read` 权限，本章节已跳过**」。理由：报告是**下载物**，一旦生成就脱离了原接口的权限校验——不这么做，`report:export` 就成了读取三域数据的旁路。

### 4.6 接口与前端

**接口**：不新增路由。`POST /api/v1/report/generate` 请求体增加可选字段：

```
{ "type": "daily", "sections": ["assets","container","logs"], "scope": {...} }
```

- `sections` 省略或为空 = 全选（**默认开启**，符合平台取向）；要少出章节就**显式列出**要保留的 ID（**显式关闭必须真生效**）；未知 ID 直接 400，不静默忽略（静默忽略会让「我勾了日志章节却没出来」变成一个谜）。
- `scope` 由服务端按 Principal 折算并**覆盖**客户端提交值（不接受客户端放宽范围）。

**前端**（`web/src/components/ReportView.vue`）：生成按钮旁加「报告章节」多选（默认全选），历史列表补一列「范围」；报告头部由模板渲染生成者/范围/跳过章节。

**存储**：产物与历史沿用现有机制（`reports/*.html` + `history.json`）；`history.json` 条目增加 `scopeSummary` 与 `skipped` 字段（**新增字段向后兼容**，旧条目缺字段按空处理）。

## 5. 决策点

| # | 决策 | 建议 | 影响 |
|---|---|---|---|
| **D1** | 载体：扩展报告章节 / 新建独立报表页 / 扩仪表盘数据源 | **扩展报告章节** | 另两个选项都要重做调度、历史、权限（§2.3 已论证不可行或成本高） |
| **D2** | 三章节是否本期全做 | **全做**（资产→容器→日志 分三个子批次，见 §6） | 若只做资产章节，容器/日志两行仍留在全景表 |
| **D3** | 范围收窄做到哪一步：路线 A（定时也按保存者快照）/ 路线 B（只做手动，定时全局并写明） | **路线 A**（与配置巡检同构，避免两套语义） | 路线 B 会在"资产章节按范围、主机章节全局"之外再叠加"手动按范围、定时全局"，语义最乱 |
| **D4** | 缺权限章节的处理：跳过并标注 / 直接不生成该章节 / 要求生成者具备全部权限 | **跳过并标注** | 标注能让"报告里没有日志章节"与"这台机器没有日志"区分开 |
| **D5** | 容器域无历史：接受快照口径并显式标注 / 为容器新建历史留存 | **接受快照并标注** | 新建历史要动采集与存储，属另一批 |
| **D6** | 日志域：只做能力自述 + 存储概览 + 日志告警指标 / 另建按天行数统计 | **只做前三项** | 按天统计要扫分片，与 P2/M 不符 |
| **D7** | 是否每章节配一份 CSV 导出（复用 `api/csvexport.go`） | **本期仅 HTML，CSV 另批** | 三份 CSV 各有列口径要定，会把批次拉大 |
| **D8** | 章节开关：生成时可选（默认全开）/ 固定全出 | **可选、默认全开** | 「显式关闭必须真生效」是平台既有取向 |

## 6. 实施批次

| 子批次 | 内容 | 依赖 |
|---|---|---|
| **28a** | 报告范围能力（手动 + 定时快照）+ 权限跳过机制 + 报告头部口径 | 无（先做，它是另外两节的地基） |
| **28b** | 资产与配置章节 | 28a |
| **28c** | 容器与工作负载章节 + 日志章节 | 28a |
| **28d** | 前端章节多选 + 历史列补「范围」 | 28b/28c |

每个子批次完成即更新全景表 §3.12 该行与本文档 §8 实施记录；**版本号整批合并递增**（平台既有习惯）。

## 7. 未覆盖边界

- **报告产物是静态文件，范围只锁在"生成时"**：范围小的用户若也持有 `report:read`，能看到范围大的管理员生成的报告全文。**这是现状就存在的问题**（现在的报告本就是全局的），本批不放大也不解决——要解决需给报告产物加"可见范围"并在下载时二次校验，属另一批。
- **大集群成本**：K8s 聚合是即时查询，Pod 数万时单次生成可能变慢；需要限时/限量的实测，本批只有用例覆盖。
- **VL 后端下的日志章节**：`StorageStats` 多项为空（`capability.go:116`），只能渲染"该后端不报告"；真实 VL 环境验证待做。
- **多实例 / HA**：定时生成的去重依赖单实例（A1 暂缓，与配置巡检同一限制）。
- **报告 HTML 体积**：三章节会让单份 HTML 变大（尤其容器章节），需要实测一份日报的字节数。

## 8. 实施记录

### 批次 28a（2026-10-08，未打包）

**已完成**：报告范围能力（手动按生成者 / 定时按保存者快照）+ 章节选择 + 权限跳过 + 报告头部口径。

改动：

- `internal/server/report/report.go`：新增 `ScopeSnapshot` / `GenerateOptions` / `ScopeInfo` / `SkippedSection` 与章节常量；`Generate` 拆出 `GenerateWith`；`collectData` 按范围过滤节点、按章节选择决定收集；`collectSecurity` / `collectMiddleware` 接收范围并在取数处过滤（含 `HasData` 口径）；智能分析 `Hosts` 也按范围收窄；`ReportMeta` 增加 `Scope` / `Sections` / `Skipped`。
- `internal/server/report/schedule.go`：`ScheduleConfig` 增加 `Scope`（范围与章节快照）/ `SavedBy`；`RunNow(by, manual)` 折算范围——**手动按点击者、定时按保存者**，与配置快照取交集（只朝收窄方向变）；身份解析不到或交集恒空都**不生成**并写明原因（fail-closed），失败也推进 `LastRunAt` 免得每 tick 重试。
- `internal/server/report/template.html`：头部渲染生成者 / 覆盖范围 / 已跳过章节（含原因）；主机明细章节改为按是否选中渲染。
- `internal/server/api/report_scope.go`（新）：`ReportScopeSnapshot`（身份 → 范围，供**定时路径**回查）+ `reportScopeFor`（请求路径与调度路径共用同一折算）+ `normalizeReportSections`（未知章节 400，不静默忽略）。
- `internal/server/api/{query,middleware_api,report_schedule_api}.go`：`ReportProvider` 增加 `GenerateWith`、`ReportScheduleProvider.RunNow` 增加触发者；保存调度配置时**覆盖**客户端提交的范围（只认服务端折算结果），章节选择仍取用户意图。
- `cmd/server/main.go`：`reportSched.SetScopeResolver(rest.ReportScopeSnapshot)`（与周期化巡检同一做法）。

测试：`internal/server/report/scope_test.go`（6 例：三态、交集只收窄、章节权限、调度折算与 fail-closed、头部渲染、章节选择）+ `internal/server/api/report_schedule_api_test.go` 新增两例（范围被覆盖、未知章节 400）。

**未做**：28b 资产与配置章节、28c 容器与日志章节、28d 前端章节多选与历史「范围」列。

### 批次 28b / 28c / 28d（2026-10-08，未打包）

**28b 资产与配置章节**：

- **范围快照收敛到一套**：`report.ScopeSnapshot` 改为 `asset.ScopeSnapshot` 的**类型别名**（节点维度 + 业务标签维度，三态与 `Apply`/`Intersect`/`IsEmpty` 全部复用台账的实现）——报告这边不再另写一份三态逻辑（`model/danger.go` 的目录清单吃过"两端各写一份"的亏）。`Sections`/`Actor`/`Skipped` 移到 `GenerateOptions` 与 `ScheduleConfig.Sections`。
- `internal/server/report/report.go`：新增 `AssetsSection`/`AssetLinkGroup`/`AssetInspectInfo` 与 `collectAssets`；台账依赖抽成窄接口 `assetsReporter`（含 `Count`），`SetAssetService` 注入。
- 四项内容全部复用台账已有聚合：`Stats`（6 个计数）、`LinkOverview`（关系分组 + 无边资产）、`InspectRunsInNodes` + `InspectFindingsInNodes`（最近一次巡检的结论与分级，采样上限 500）。
- **不做「近 7 天变更 Top N」**：需要台账侧新增按字段分组的聚合方法（`asset_changes` 没有对应查询），本批只给 `Stats.Changes` 计数。**Docker 容器计数也不列**：它以 `middleware-instance` 落库、自然键带 `docker:` 前缀，现有筛选没有"自然键前缀"条件——宁缺勿错。

**28c 容器与日志章节**：

- `collectContainer`：与容器页共用同一批 `k8s_*` 指标名与同一个 `instancereg` 注册表（up 指标优先、注册表补全离线集群）；异常 Pod 沿用「`k8s_pods_abnormal` 缺失则退回 pending+failed」的旧 Agent 回退口径；台账容器资产计数按类型（pod / workload / service）。**整章标注「当前快照」**。
- `collectLogs`：如实转述 `LogStore.Capability()`（后端、索引能力、扫描预算、Notes 与存储统计）。**受限身份下不展示存储统计**（它按全局口径统计、无法按范围收窄），只留能力说明。
- 未做：日志告警命中统计（需要规则列表，本批没接）、保留期天数（需要 retention 配置注入）。

**28d 前端**（`web/src/components/ReportView.vue`）：

- 生成卡片加「报告章节」多选（默认全选，手动生成与周期化生成共用同一组选择），生成/保存调度时随请求下发。
- 历史列表加「范围」列（`row.scope`）与「已跳过 N 章」标记。
- 章节回读服务端真实值；空数组表示全选，此时保持界面全选。

**验证**：`go build ./...`、`go test ./...` 全绿；`npx vitest run` 全绿（`ReportView.test.js` 9 例，含新增 3 例：章节随生成下发、历史显示范围与跳过标记、保存调度带章节）。

**已打包 1.30.57**（full 200 MiB / upgrade 80.6 MiB）。打包时本机 jq 损坏、python 不可用，`release.sh` 的 manifest 自校验被跳过，已**手工核对**：6 个二进制齐全、manifest 引用的 8 个 component（含易漏的 `deploy/agent-install.sh`）全在包内、`web/index.html` 在包内、server 二进制内嵌 `1.30.57`。

**已实机验证（2026-10-08，dev-server 1.30.57）**：`dist/report-e2e.py core` **23 项全过**——

- 管理员报告：头部有生成者 / 覆盖范围=全部节点 / 无跳过章节；资产、容器、日志三章都渲染；**含**范围外节点（对照组）。
- 章节选择：只选 `hosts` 时资产与日志章节**正文里不出现**；未知章节 400。
- 历史列表最新一条带 `scope` 字段。
- 受限账号（分组「国外」、缺 `container:read` 与 `logs:read`）：**报告里没有范围外节点**、覆盖范围=指定 N 个节点、缺权限的两章**正文不渲染**只在头部写明原因、有 `assets:read` 的资产章节正常渲染。

**核对中的一个坑（不是缺陷，是既定语义）**：受限测试账号的**角色**也必须设 `restricted`——`ExpandPrincipal` 是「权限并集 + 范围并集」，角色 `global` 会把用户 scope 并成全局，第一版脚本因此得到一份"全局"报告。
