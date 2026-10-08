# 文档索引

本目录的文档索引。**先说清哪一份是权威**，再按用途分类列出全部文件，最后是目录约定与归档说明。

---

## 一、先看这三份

| 想知道什么 | 看这份 |
|---|---|
| 某个功能**有没有做**、做到什么程度 | `superpowers/specs/2026-09-30-ops-platform-capability-map.md`——**功能全景与实现状态对照表**（14 个领域 × 已实现/部分实现/未实现 × 优先级 × 复杂度）。**行级状态是权威**，域表数字跟随它 |
| 这次升级**要注意什么** | `releases/release-notes-v1.30.0.md`——面向运维；含「**刻意不做**」与「**风险后置**」两张清单、以及两条**运维动作**（备份审计链密钥、把日志锚点送往异地） |
| 系统**怎么用、怎么配** | 仓库根目录 `README.md`（部署/升级/配置/API 全量）+ 本目录 `guides/configuration-cookbook.md`（可复制的配置模板） |

> **读文档前的一条规矩**：本仓库的文档里，**设计件**写的是"当时打算怎么做 + 实施记录 + 实机结果"，
> **不代表现状**；**状态以全景表的行级状态为准**。凡被移入 `archive/` 的，一律**不要当现状读**。

---

## 二、目录约定（新增文档该放哪）

| 目录 | 放什么 | 命名约定 |
|---|---|---|
| `adr/` | **架构决策记录**——一事一议、短、**不改**（要改就再写一条） | `NNNN-主题.md` |
| `superpowers/specs/` | **设计件**——功能设计 + 实施记录 + 实机验证结果 | `YYYY-MM-DD-主题-design.md` |
| `superpowers/plans/` | **实施计划**——批次拆解与执行顺序 | `YYYY-MM-DD-主题.md` |
| `designs/` | **早期设计件**（成文时未按 `YYYY-MM-DD` 命名，如 C2 集中日志、A1 高可用） | 主题名 |
| `guides/` | **现役的横切文档**：权限矩阵、RBAC、配置手册 | 主题名 |
| `releases/` | **发布说明** | `release-notes-v<版本>.md` |
| `notes/` | **排查与修复记录**（方法可复用的那种） | 主题名 |
| `prototypes/` | **静态原型**（设计参考稿，**不是可运行页面**） | 主题名 |
| `testing/` | 测试计划、验证矩阵、审查结论 | `YYYY-MM-DD-*.md` 或主题名 |
| `system-audit/` | 系统巡检问题清单 | — |
| `agents/` | 给 AI 协作者的说明（领域术语、issue 流程） | — |
| `archive/` | **已失效 / 一次性**记录，保留原文件名 | — |
| **本目录根** | **只有 `README.md`（本索引）**——新文档一律进上面某个目录 | — |

**判断落点**：功能设计 → `superpowers/specs/`（带日期）；一次决策 → `adr/`；测试与验证 → `testing/`；
现役的权限/配置类横切文档 → `guides/`；发布说明 → `releases/`；排查记录 → `notes/`；
被后续改动取代的旧文档 → `archive/`（**加横幅说明它为什么失效**，不要静默删掉——决策轨迹也是资产）。

---

## 三、架构决策记录（ADR）

| 文件 | 决策 |
|---|---|
| `adr/0001-cmdb-relational-store.md` | 资产与配置用**内嵌关系库**（SQLite 单文件、纯 Go 驱动）持久化 |
| `adr/0002-log-backend-abstraction.md` | 日志存储**抽成接口** `logstore.LogStore`，并随第二个适配器（VictoriaLogs）一起落地 |
| `adr/0003-container-management-downlink.md` | 容器管理操作**由 Agent 执行**，复用上报响应下发指令（不新增长连接与入站端口） |

---

## 四、设计件（`superpowers/specs/`）

### 平台总体

| 文件 | 说明 |
|---|---|
| `2026-09-30-ops-platform-capability-map.md` | **功能全景与实现状态对照表（权威）**：96 个功能点 × 状态 × 优先级 × 复杂度 + 各域的「定界」与「仍缺」 |
| `2026-09-30-ops-platform-evolution-design.md` | 一体化运维平台总体设计（在既有监控平台上演进的路线） |
| `2026-09-30-ops-platform-open-source-research.md` | 开源项目调研对比（选型依据） |

### 资产与配置

| 文件 | 说明 |
|---|---|
| `2026-09-30-asset-cmdb-design.md` | 资产与配置模块**主设计件**（台账 / 属性 / 变更历史 / 巡检 / 标签 / 批量 / 导出） |
| `2026-10-07-asset-business-scope-design.md` | 资源范围增加**业务标签**维度（与节点分组取交集）；顺带关掉一条提权路径 |
| `2026-10-07-asset-ci-model-design.md` | **配置项模型页**（类型模型可读可写，巡检字段范围收敛到一处） |
| `2026-10-07-asset-relation-auto-source-design.md` | 资产关系的**自动来源**（副本→主库；修掉 `runs_on` 宿主误判） |
| `2026-10-07-asset-topology-overview-design.md` | **关系视图**（全库总览聚合 + 筛选下钻） |
| `2026-10-07-k8s-service-relation-design.md` | K8s `service → pod` 关系（补采集 Service / EndpointSlice，`exposes` 的第一个自动来源） |
| `2026-10-07-middleware-cluster-membership-design.md` | 中间件实例 → 集群的 `member_of`（只对**有权威集群标识**的类型建边） |
| `2026-10-07-inspect-schedule-design.md` | **配置巡检周期化**（范围只能由服务端按身份折算） |

### 容器与集中日志

| 文件 | 说明 |
|---|---|
| `2026-09-30-container-observability-design.md` | 容器与可观测**主设计件**（只读管理面 + 日志拉取 + Pod 日志进检索） |
| `2026-10-04-container-inventory-design.md` | 容器与工作负载**清单上报**（K8s Pod / Workload / Service） |
| `2026-10-07-log-backend-capability-design.md` | 日志检索能力与**后端选型可见化**（"我在哪一种后端下、能查多大"） |

### 权限、审计与高风险操作

| 文件 | 说明 |
|---|---|
| `2026-09-27-scope-and-server-reliability-design.md` | 节点分组**资源范围**落到指标读取链路 + Server HTTP 生命周期加固（对应 v1.28.0） |
| `2026-10-05-audit-ack-persistence-design.md` | **审计与告警处置入库**（批次 18；含"变更历史 ↔ 审计按请求 id 关联"的批次 20） |
| `2026-10-07-high-risk-approval-design.md` | **高风险操作双人复核**（审批状态机；默认关闭；申请人不可自批） |
| `2026-10-07-high-risk-command-design.md` | **高危命令拦截**（危险参数提交前判定 + `sudo` 高危命令事后发现） |
| `2026-10-08-audit-tamper-evident-design.md` | **审计防篡改（可检测）**：链式 HMAC + 裁剪归因 + 日志锚点 + 校验接口。**含"它不是不可篡改"的完整边界说明** |
| `2026-10-07-compliance-matrix-export-design.md` | 合规矩阵导出（安全基线 × 节点，CSV） |

### 下行操作与护栏

| 文件 | 说明 |
|---|---|
| `2026-10-05-ops-tasks-persistence-design.md` | 操作任务（`ops_tasks`）**入库**（批次 19） |
| `2026-10-07-spool-drain-budget-design.md` | 补传排空预算动态化 + 413 自愈 + 排空可见性（Agent 侧） |
| `2026-10-08-guard-risk-report-design.md` | **护栏组合风险**上报与展示（"两个护栏配在一起等于任意代码执行"） |

### 实施计划（`superpowers/plans/`）

| 文件 | 说明 |
|---|---|
| `2026-09-27-resource-scope-and-alerts.md` | 节点分组权限边界修复的实施计划（批次拆解） |
| `2026-09-27-server-lifecycle.md` | Server HTTP 生命周期加固的实施计划 |

> 计划是**执行期**文档：做完之后它描述的是"当时怎么排的"，**状态仍以全景表为准**。

---

## 五、发布说明

| 文件 | 覆盖 |
|---|---|
| `releases/release-notes-v1.27.0.md` | v1.27 全仓代码审查安全修复 |
| `releases/release-notes-v1.28.0.md` | v1.28 节点分组资源范围 + Server 生命周期加固 |
| **`releases/release-notes-v1.30.0.md`** | **v1.29.0 ~ v1.30.56**（跨度大，按主题组织；**含刻意不做 / 风险后置 / 运维动作**） |

---

## 六、现役的横切文档

### `guides/`（**权限与配置以它们为准**）

| 文件 | 说明 |
|---|---|
| `guides/permission-matrix.md` | **权限矩阵**：路由 → 权限点 → 资源范围（**权限相关的现状以它为准**） |
| `guides/role-permission-management.md` | RBAC 设计文档（角色模型、资源范围、迁移策略） |
| `guides/configuration-cookbook.md` | **配置模板手册**：可复制的告警事件管道、集中日志等配置模板 |

### `designs/`（早期设计件，未按日期命名）

| 文件 | 说明 |
|---|---|
| `designs/c2-central-logs.md` | 集中日志设计件（**已落地**；代码注释仍引用它，见 `internal/agent/collector/logcollect.go`） |
| `designs/a1-server-ha.md` | Server 高可用设计件（**暂缓**，非待办；将来评审即可实施） |

### `notes/`（排查与修复记录）

| 文件 | 说明 |
|---|---|
| `notes/debug-security-event-rule-disabled-alerting.md` | 排查记录：安全事件规则禁用后仍持续告警（**排查方法可复用**） |
| `notes/severity-fix-plan.md` | 2026-09 代码审查产出的严重缺陷修复方案（历史修复清单） |

---

## 七、系统巡检

| 文件 | 说明 |
|---|---|
| `system-audit/system-audit.md` | 系统巡检**问题清单**（记录发现的问题与处置）。**它是一份清单，不是现状描述**——问题是否已修要看代码与全景表 |

---

## 八、测试与验证

| 文件 | 说明 |
|---|---|
| `testing/2026-10-06-platform-review-delta.md` | 平台全面审查增量结论（**当前最完整的审查记录**；含日志后端联调清单 §10.4） |
| `testing/2026-10-05-platform-validation-matrix.md` | 功能验证矩阵（逐项验证计划与实测结果） |
| `testing/alert-center-test-plan.md` | 告警中心专项测试文档 |

> **实机核对脚本**不放在本目录：它们在 `dist/`（该目录已 gitignore），形如 `*-e2e.py`，
> 各设计件的「实机验证」小节会引用它们。换人接手时若找不到脚本，去 `dist/` 看。

---

## 九、给 AI 协作者

| 文件 | 说明 |
|---|---|
| `agents/domain.md` | 领域说明 |
| `agents/issue-tracker.md` | issue 流程（GitHub） |

---

## 十、归档（`archive/`）——**别当现状读**

| 文件 | 为什么归档 |
|---|---|
| `archive/refactor-plan.md` | 2026-09-26 的改造路线与批次计划，版本停留在 1.26.1；**正文按当时状态保留、不改写历史**，顶部已加历史横幅 |
| `archive/c1-collector-templates.md` | 采集项模板**已整链路移除**（`1e25239`）；其中的接口、权限点、配置项与安全模型都不再有效 |
| `archive/c1-phase3-kinds.md` | 所属方向已撤；且 `exec` / `file` 取数思路**与现行操作面安全边界冲突**（不提供任意命令 / 脚本执行） |
| `archive/commits-34e0075-to-58c88ee.md` | 一次性提交梳理；依赖的 `git reflog` 90 天恢复窗口早已过期 |
| `archive/nebulaeye_crash_log_20260828.md` | 一次性崩溃现场记录 |
| `archive/2026-08-09-full-test-report.md` | 当时结论（"不满足上线安全门槛"）针对 2026-08 的版本，**不代表现状** |
| `archive/middleware-monitoring-test-report-20260929.md` | 针对 **1.29.0** 的测试报告 |

---

## 十一、原型

| 文件 | 说明 |
|---|---|
| `prototypes/permission-console-mock.html` | 权限控制台的**静态原型**（设计参考稿，不是可运行页面） |

---

## 附：一次「文档滞后」的处置记录（2026-10-08）

全景表 §六 曾记着一条待办：**采集项模板（C1 阶段一~三）已移除，但文档仍按"已交付"描述**。
本次已处置完毕，作为**今后遇到同类问题的样板**：

1. **能改的改**：`CONTEXT.md`（复核后只剩 1 处，全景表原先写的"7 处"**自己也过期了**）、
   `guides/permission-matrix.md`（删掉 6 条不存在的 `/api/v1/middleware/templates*` 路由，
   **以及 1 条同样不存在的通用实例路由** `/api/v1/middleware/{type}/instances`）；
2. **历史文档加横幅**：`archive/refactor-plan.md` 顶部标明"不代表现状"与失效范围，**正文一字不改**；
3. **过时设计件移入 `archive/` 并加横幅**：保留决策轨迹，但明确"其中的接口与安全模型不再有效"；
4. **顺带发现**：`middleware:write` 权限点仍在权限目录里（且仍授予运维管理员），但**已不对应任何路由**——
   排查"为什么某人没有某个入口"时别被它误导。

**通用做法**：**不要静默删除**失效文档；**也不要直接改写历史文档的正文**——
加横幅说明"它为什么失效、现状看哪里"，是既诚实又不丢信息的做法。
