# AI 模型配置与辅助运维（设计件）

- 日期：2026-10-08
- 批次：30
- 状态：**设计待评审（D1~D10）**
- 前置：无（新增领域）
- 关联：`2026-09-30-ops-platform-capability-map.md`（拟新增「AI 辅助运维」域）；定界依据 `2026-09-30-ops-platform-evolution-design.md:19`

---

## 1. 起因

用户 2026-10-08 提出：接入大模型、增加「AI 模型配置」功能，借助 AI 模型辅助运维。

这是一条**新领域**，不是既有能力的扩展：平台此前的"智能"全是本地统计与规则（见 §2.1），**从未调用过任何外部模型**。

## 2. 现状与依据（调研结论）

### 2.1 现有「智能分析」是纯统计 + 规则，不调外部服务

| 文件 | 做什么 | 算法 |
|---|---|---|
| `internal/server/analysis/baseline.go` | 动态基线 | 中位数 + MAD 稳健区间，末尾**连续 ≥3 点**越界才算异常 |
| `internal/server/analysis/forecast.go` | 容量预测 | 最小二乘线性回归 + R² 拟合度门槛；百分比指标算"何时打满"，速率类只报增长 |
| `internal/server/analysis/risk.go` | 风险优先级 | 规则加权评分，同类证据只取最重两条 |
| `internal/server/analysis/correlation.go` | 根因关联 | 规则化关联（主机离线 × 中间件 × 拨测 × 安全事件），只读线索 |
| `internal/server/analysis/analyzer.go` | 编排 + 5 分钟缓存 | 数据源是 `storage.QueryRange`（PromQL 区间查询） |

**判据**：`analysis` 包的导入只有 `math`/`sort`/`time` 与内部包，**没有 `net/http`**——即"智能"完全在本地算。
入口：`GET /api/v1/analysis/summary`（权限点 `nodes:read`，`api/analysis_api.go:13`）；前端 `web/src/components/intelligence/IntelligenceView.vue`。

**结论**：AI 接入是**新增能力**，不是改造既有智能分析；两者要能**并存且不混淆**（一个是平台自己算的结论，一个是模型给的建议）。

### 2.2 可直接复用的四套范式

| 要复用的 | 现成在哪 | 已经处理好的细节 |
|---|---|---|
| 外呼 HTTP | `internal/server/alert/notifier.go`（`postJSON` L577） | 超时 10s、响应体 `io.LimitReader(...,4096)`、状态码 ≥300 判失败、**业务码校验**（钉钉/企微/飞书 HTTP 200 也可能失败）、`redactURLError`（L357，剥掉含 token 的 URL）、TLS 强制校验（**未设** `InsecureSkipVerify`） |
| 密钥脱敏 | `internal/server/api/notify.go` | GET **把密钥置空**（`handleNotifyGet:12`）；PUT **空值沿用旧值**（`handleNotifyPut:26`）；前端 placeholder「不修改请留空」 |
| 配置热改 | `internal/server/notify/manager.go` | 独立 YAML + **原子写**（`Save:67`），保存即生效、不需重启 |
| 审计脱敏 | `internal/server/audit/store.go:sanitizeObject`（L457） | 键名含 `password/secret/token/credential` 的值统一替换为 `<redacted>` |

### 2.3 定界：必须做成「可选能力、默认关闭」

`2026-09-30-ops-platform-evolution-design.md:19`：

> **不引入需外网或重量级依赖**：默认只借开源**设计思想**；代码级复用仅限 Apache-2.0/MIT 集合

`internal/server/config/config.go:376`：

> 外部后端是可选能力，**默认值必须保持"离线单机可用"**

**注意口径**：项目里**没有一条禁止运行时外呼**的定界——通知（钉钉/飞书/企微/邮件/Webhook）、服务拨测、Web 升级、GeoIP 上传都是既有的外呼/外部交互能力。所以接大模型**不违反定界**；真正的约束是"**离线部署的默认真空不因此改变**"。

因此形态只能是：**默认关闭、配好才可用、离线包与离线部署不受影响**。

### 2.4 既有边界：只读辅助，不自动处置

`README.md:243`：

> 本能力仅做数据分析和人工决策辅助，**不执行自动化处置**，也不会改变告警规则、主机或中间件状态。

且「自动化处置与自愈」已由用户 **2026-09-26 决定暂缓**（`README.md:416`）。AI 必须落在同一条边界内：**只给建议，不动任何状态**。

## 3. 本功能与平台其它功能的根本差异：数据出境

平台此前所有能力，数据**从不离开部署环境**（通知只发告警标题、拨测只发探测包、报告只落本地文件）。接大模型意味着把运维数据**发到第三方**（除非用户自建本地模型）——这是**新风险面**，必须在设计里正面处理。

**关键**：数据一旦发出，就**脱离了平台的全部权限与范围校验**。这与报告章节那次是同一个道理（"报告是下载物，`report:export` 不能成为读取三域数据的旁路"）——那次我们让**缺权限的章节在收集阶段就断掉**，这里同理：**组装上下文时就要按调用者的范围与权限裁剪，不能先组装再过滤**。

### 3.1 敏感度分级

| 级别 | 数据 | 出境建议 |
|---|---|---|
| 低 | 主机名、类型、分组、标签、指标名 | 默认允许 |
| 中 | 指标统计值、告警标题/级别/时间、智能分析结论、关系与变更历史 | 默认允许 |
| **高** | **日志正文、属性值（可能含连接串/口令）、命令输出、环境变量、IP/内网地址** | **默认不发**，需显式开启 |

### 3.2 五条硬规则

1. **默认关闭**——不配置、不启用就完全不存在这条外呼路径；
2. **调用前明确告知发往哪里**——配置页显示 baseURL；界面提示"本次会把 X 类数据发送到 Y"；
3. **权限点单列** `ai:invoke`——调用 ≠ 看配置，前者是数据出境；
4. **每次调用写审计**——谁、对哪个对象、发往哪个模型、发了多少字符、用了多少 token、**是否含高级别数据**；
5. **数据分级**——默认只发低 + 中；高级别需显式开关，且该事实进审计。

## 4. 功能全景（候选能力）

| # | 能力 | 价值 | 成本 | 出境风险 | 期次 |
|---|---|---|---|---|---|
| 1 | **AI 模型配置**（provider / baseURL / key / model / 参数 / 连通测试） | 地基 | S | 无（不调用） | **第一期** |
| 2 | **AI 诊断**（对主机 / 资产 / 告警生成排查建议） | 高 | M | 中 | **第一期** |
| 3 | AI 告警摘要（把风暴收敛的结论写成人话） | 中 | S | 中 | 第二期 |
| 4 | AI 报告摘要（巡检报告加一段摘要） | 中 | S | 中 | 第二期 |
| 5 | 日志归因（检索结果 → 归因结论） | 高 | M | **高** | 第二期（需显式勾选） |
| 6 | 自然语言查询（→ PromQL / 日志检索式） | 中 | L | 低 | 第三期（生成的查询可能很重，必须人工确认后执行） |
| 7 | AI 规则生成（自然语言 → 告警规则草稿） | 中 | M | 低 | 第三期 |

## 5. 第一期方案

### 5.1 模型配置

**存储**：独立文件 `ai.yaml`（照 `notify.yaml` 的先例：独立文件 + 原子写 + 保存即生效）。

```go
type AIConfig struct {
    Enabled        bool    `yaml:"enabled" json:"enabled"`
    BaseURL        string  `yaml:"baseUrl" json:"baseUrl"`   // OpenAI 兼容端点，如 https://api.deepseek.com/v1
    APIKey         string  `yaml:"apiKey" json:"apiKey"`     // 敏感：只写不读
    Model          string  `yaml:"model" json:"model"`
    TimeoutSeconds int     `yaml:"timeoutSeconds" json:"timeoutSeconds"` // 默认 60（LLM 比 webhook 慢）
    MaxTokens      int     `yaml:"maxTokens" json:"maxTokens"`           // 默认 1024
    Temperature    float64 `yaml:"temperature" json:"temperature"`       // 默认 0.2（运维要稳定，不要发散）
    AllowSensitive bool    `yaml:"allowSensitive" json:"allowSensitive"` // 允许发高级别数据，默认 false
    IncludeLogs    bool    `yaml:"includeLogs" json:"includeLogs"`       // 诊断是否带日志片段，默认 false
}
```

**接口**：

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| GET | `/api/v1/ai/config` | `ai:read` | **不返回 apiKey**（置空，照 `handleNotifyGet`） |
| PUT | `/api/v1/ai/config` | `ai:write` | 空 apiKey **沿用旧值**；**写审计** |
| POST | `/api/v1/ai/config/test` | `ai:write` | 连通性测试（发一条最小请求）；**写审计** |

> 与既有"配置类 API 不写审计"（notify/ui/screen/retention 都没写）**有意不同**：本配置决定**数据往哪里流**，属于安全相关配置，必须留痕。

**前端**：`SettingsView.vue` 的 `panes` 新增一项「AI 助手」（`?tab=ai` 直达），表单 + 启用开关 + 「测试连接」按钮；密钥字段 `type=password` + 「不修改请留空」；页面上明确显示"数据将发送到：`{baseURL}`"。

### 5.2 AI 诊断（第一期的唯一应用）

**入口**（三处共用同一后端）：主机详情页、资产详情抽屉、告警详情。

**接口**：`POST /api/v1/ai/diagnose`（`ai:invoke`），body：

```json
{ "kind": "node|asset|alert", "id": "…", "includeLogs": false }
```

**上下文组装在服务端**（不让前端拼）：前端拼意味着可以塞任意内容，且范围校验会失守。

| 对象 | 上下文内容 | 级别 |
|---|---|---|
| 主机 | 当前指标摘要（CPU/内存/磁盘/网络现值 + 近 24h 趋势）+ 智能分析结论（`analysis.HostResult`）+ 活跃告警 + 关联中间件实例 + 拨测状态 | 低/中 |
| 资产 | 类型、标签、关系、近 N 条变更历史 + 属性（**按敏感度过滤**：`ip`/`addrHost` 等归高级别） | 低/中 |
| 告警 | 规则、级别、时间、labels + 同主机其它活跃告警 + 智能分析的关联结论 | 低/中 |
| 日志（可选） | 检索片段，**先脱敏**（疑似口令/连接串的行剔除） | **高**，需 `AllowSensitive` + 请求显式开启 |

**范围校验**：除 `ai:invoke` 外，还要过 `nodes:read` / `assets:read` 的**资源范围**判定——**不能成为越权读取的旁路**。

**提示词**：固定 system prompt——"你是运维助手，**只依据给定数据**给排查建议；不要编造未给出的指标；不确定就明确说不确定；不给自动化处置方案" + 上下文。

**输出**：markdown 建议，前端展示；**不落库**（见 D8）。

### 5.3 失败与降级

- 未配置/未启用 → 界面入口显示「未配置 AI 模型」并给配置页链接（**不静默**）；
- 调用失败 → 明确错误（超时 / 401 / 429 / 业务码），**不重试**（与 notify 一致；重试 = 重复出境）；
- 超时默认 60s；响应体限流取 256 KiB（LLM 响应比 webhook 大得多）；
- URL 错误用 `redactURLError` 脱敏后再记日志（**密钥不能进日志**）。

### 5.4 权限点

新增三个：`ai:read`（看配置）、`ai:write`（改配置与测试）、`ai:invoke`（发起调用）。

`ai:invoke` **单列**的理由：调用 = 数据出境，与"看配置"不是一个量级的动作。

## 6. 决策点

| # | 决策 | 建议 | 理由 / 影响 |
|---|---|---|---|
| **D1** | 第一期做哪些能力 | **配置 + AI 诊断** | 配置是地基；诊断是"配好立刻有用"的最小闭环。其余见 §4 分期 |
| **D2** | 协议 | **OpenAI 兼容**（`POST {baseURL}/chat/completions`） | 一个适配器覆盖 DeepSeek / 通义 / 智谱 / Moonshot **与本地 vLLM / Ollama**——离线环境也能用本地模型；不写多套适配器 |
| **D3** | 默认状态 | **默认关闭** | 与「离线单机可用」一致；离线部署与离线包不受影响 |
| **D4** | 数据边界 | **分级**：低/中默认发；高（日志正文/属性值/命令输出）默认不发，需显式开启 | 见 §3；高级别开启这一事实进审计 |
| **D5** | 密钥存储 | **明文 `ai.yaml` + API 脱敏**（照 notify） | 平台既有取向是密钥明文落盘、只写不读。**不引入加密库**（会新增重量级依赖，与 §2.3 定界冲突） |
| **D6** | 内置角色是否默认给权限 | **`ai:read` + `ai:invoke` 给运维管理员；`ai:write` 只给超管** | 与既有取向一致——运维是排障的人，`logs:read`/`container:read`/`ops:exec` 都给了他；但**改配置决定数据流向哪里**，是平台里唯一"改一下就会让数据离开部署环境"的配置，且**改回来也收不回已发出的数据**，因此**用**与**配**分离。这条也是被既有测试 `TestEveryCatalogKeyIsGrantedBySomeRole` 逼出来的：它要求每个权限点都有非超管内置角色覆盖，否则会出现"功能装了却没人看得到"（`ops:read`/`ops:exec` 就这样漏掉过一次） |
| **D7** | 是否支持流式输出 | **第一期不做**（一次性返回） | 简化前端与错误处理；流式另批 |
| **D8** | 是否保存 AI 结论 | **不落库**，只写审计摘要（对象 / 模型 / token / 耗时 / 是否含高级别数据） | 结论是"线索"；落库会与平台自己算的智能分析结论混淆，且模型输出不可复现 |
| **D9** | 多轮对话 | **第一期不做**（单轮诊断） | 多轮要维护会话状态与上下文预算，价值增量不明 |
| **D10** | 上下文预算 | 单次请求上限（建议 24K 字符），超限**截断并明确告知** | 避免把一个月指标全塞进去（费用与隐私双输） |

## 7. 明确不做

- **不自动处置**：AI 只给建议，不改告警规则、不下发动作、不改任何状态（与 §2.4 既有边界一致）；
- **不让 AI 改配置**：模型输出的"建议修改"只是文本，要改由人去改；
- **不做 RAG / 向量库**：第一期靠**结构化上下文**——平台自己的数据本来就有结构（指标、告警、资产、关系），不需要检索增强；
- **不做多轮对话记忆**；
- **不引入新依赖**：HTTP 用标准库 `net/http`（照 `notifier.go`），不引任何 LLM SDK。

## 8. 实施批次

- **30a 配置地基**：`config.AIConfig` + `ai/manager.go`（独立 YAML + 原子写 + 热生效）+ `GET/PUT /api/v1/ai/config` + `POST /api/v1/ai/config/test` + 权限点（`ai:read`/`ai:write`）+ 审计 + 前端设置页「AI 助手」子页
- **30b AI 诊断**：`ai/context.go`（三域上下文组装 + 敏感度过滤 + 预算截断）+ `ai/client.go`（OpenAI 兼容调用，复用 notifier 的超时/限流/脱敏做法）+ `POST /api/v1/ai/diagnose` + 三处前端入口（主机 / 资产 / 告警）
- **30c 收口与验证**：范围校验用例（受限身份不能借 `ai:invoke` 读到范围外对象）、脱敏用例（密钥不进日志与响应）、离线默认关闭用例、`dist/ai-e2e.py` + 实机验证

**30a 是地基，先做**。

## 9. 实施记录

### 批次 30a（2026-10-08，未打包）

**已完成**：AI 模型配置地基——独立 `ai.yaml` + 读写/连通测试接口 + 三个权限点 + 审计留痕 + 前端设置页子页。

改动：

- `internal/server/config/config.go`：新增 `AIConfig`（含 `Normalize()` 补齐零值默认、`Ready()` 判定可用）与 `Config.AIFile`，默认路径 `/etc/monitor-server/ai.yaml`。**默认关闭**（`Enabled: false`）。`TimeoutSeconds` 的零值补 60——`http.Client` 的 0 超时是"无限等待"，是本批最危险的一个零值。
- `internal/server/ai/manager.go`（新）：照 `notify.Manager` 的范式（独立文件 + `config.AtomicWrite` + 热生效）；加载与保存都过 `Normalize()`（手写 yaml 漏了超时也要被补上）。
- `internal/server/ai/client.go`（新）：OpenAI 兼容客户端（`POST {BaseURL}/chat/completions`），复用 `alert/notifier.go` 踩过的坑——超时、`io.LimitReader`（256 KiB）、状态码 ≥300 与**响应体里的 error** 分别判定（部分网关 HTTP 200 也算失败）、`RedactEndpoint` 剥掉 query 里的凭据（有些网关用 `?api_key=`）。**不重试**：重试等于把同一份数据再出境一次。
- `internal/server/api/ai.go`（新）：`GET /api/v1/ai/config`（**不回显 apiKey**，另给 `hasApiKey` 让界面能给出正确的 placeholder）、`PUT`（空密钥沿用旧值）、`POST /api/v1/ai/config/test`（用**已保存的配置**测，不接受请求体里的临时配置——否则"测试通过"与"保存后能用"可能不是同一份配置）。**保存与测试都写审计**（category `ai`），与 notify/ui/screen/retention **都不写审计**有意不同：本配置决定数据往哪里流。
- `internal/server/auth/model.go`：权限目录新增「AI 助手」域（`ai:read`/`ai:write`/`ai:invoke`）；`RoleOpsAdmin` 给 `ai:read` + `ai:invoke`。
- `internal/server/api/query.go`、`cmd/server/main.go`：`SetAIManager` 注入（可选能力走 setter，不动已有二十多个位置参数的 `api.New`）。
- 前端：`web/src/components/settings/AISettingsSubView.vue`（新，含"开启前请确认数据去向"提示与敏感内容开关）+ `SettingsView.vue` 注册 `?tab=ai` + `api/http.js` 三个方法。

**D6 在实施中被既有测试修正**：原建议"内置角色都不给"，被 `TestEveryCatalogKeyIsGrantedBySomeRole` 拦下——它要求每个权限点都有**非超管**内置角色覆盖，否则会出现"功能装了却没人看得到"（`ops:read`/`ops:exec` 就这样漏掉过一次）。改为 `ai:read` + `ai:invoke` 给运维管理员（与 `logs:read`/`container:read`/`ops:exec` 同一取舍：运维是排障的人），`ai:write` 登记进 `superAdminOnlyKeys` 并写明理由（它决定数据发往哪里，且**改回来也收不回已发出的数据**）。§6 的 D6 已同步。

**验证**：`go build ./...`、`go test ./...` 全绿；前端 `npm test` **27 文件 / 165 例全过**。新增用例：

- `ai/client_test.go`（6 例）：**未启用时不发出任何请求**（默认关闭的部署里这条外呼路径根本不该存在）、成功路径与鉴权头、HTTP 错误、HTTP 200 但带 error、非 JSON 与空回答、超时、`RedactEndpoint`、零值默认。
- `ai/manager_test.go`（2 例）：落盘/热生效/跨重启、加载时归一化。
- `api/ai_test.go`（5 例）：脱敏与 `hasApiKey`、空密钥沿用旧值、校验三态（缺端点/缺模型/非法 scheme；关闭时不校验）、权限（`ai:read` 能读不能写）、连通测试成功与端点不可达 502。
- `settings/AISettingsSubView.test.js`（6 例）：默认关闭且写明数据去向、**密钥不回填**、启用缺端点不提交、保存时端点去尾斜杠、测试连接成功/失败展示。

**未做**：30b AI 诊断（服务端上下文组装 + 主机/资产/告警三处入口）、30c 收口与实机验证。

### 批次 30b（2026-10-08，未打包）

**已完成**：AI 诊断后端（三域取数 + prompt 组装 + 调用 + 审计）。**前端入口留到 30c**。

- `internal/server/ai/context.go`（新）：`Section`（标题 + 逐行 + 敏感度）/ `DiagnosticContext` / `BuildMessages`（含 24K 预算截断，**截断会明说**）/ 固定 system prompt（只依据给定数据、不编造、不建议处置、样本不足要如实说）。`ai` 包**不认识** node/asset/alert——取数留在 API 层，依赖方向单向（与 report 包不依赖 auth 是同一条理由）。
- `internal/server/api/ai_diagnose.go`（新）：`POST /api/v1/ai/diagnose`（`ai:invoke`），支持 `kind: node|asset|alert`。
  - **范围判定先于取数**：主机用 `nodeInScope`、资产用 `assetVisible`（节点分组 + 业务标签两维）、告警用其 `Node`；范围外直接 400，**不发请求**。
  - 主机上下文**直接用 `analysis` 的结论**（评分/严重度/风险证据/容量预测/基线偏离/覆盖度），不自己查指标——那套结论已有用例覆盖，另查一遍只会得到"两份说法"。
  - 资产：概况 + **字段名清单** + 最近 10 条变更；值属高级别，见下。
  - 告警：告警详情 + 标签 + 同主机其它活跃告警 + 该主机的分析结论。
- `internal/server/api/query.go`：注册路由。
- **删掉 `IncludeLogs`**：30a 预留的"诊断默认带日志片段"本批不实现，留着就是"配了却不生效"的死开关。日志正文（诊断价值最高、也最敏感）留给「日志归因」那一批，届时加它自己的开关。
- **`notSent` 的两种来源**：因未开启 `AllowSensitive` 而**根本没取**的（第一道防线，`WithheldTitles`）与取到后被过滤的（第二道防线，`HighLevelTitles`）。这是测试抓出来的缺口——只做后者时 `notSent` 恒空，用户无从知道"模型没看到属性值"。

**验证**：`go test ./...` 全绿；前端 `npm test` 27 文件 / 165 例全过。新增 `api/ai_diagnose_test.go`（5 例）：

- **未配置时不发请求**并指路到配置页；
- **范围外拒绝且不发请求**（受限身份诊断 g2 上的告警被拒、诊断 g1 上的通过，且上下文里**不出现** g2 的主机名）；
- **敏感内容默认不发**（`AllowSensitive=false` 时属性值不进上下文但**字段名保留**，`=true` 时进入且响应标记 `sensitive`）；
- 未知类型 / 缺主机名 / 告警不存在；
- 权限（只有 `ai:read` 时 403）。

**未做**：前端三处诊断入口（主机 / 资产 / 告警详情）、`dist/ai-e2e.py` 与实机验证、打包。

### 批次 30c（2026-10-08，已打包 1.30.59）

**已完成**：前端诊断入口（主机 + 告警）+ e2e 脚本 + 打包。

- `web/src/components/ai/AiDiagnosePanel.vue`（新）：可复用的「AI 诊断」按钮 + 结果弹窗。**做成组件而不是在三处各写一遍**——上下文组装在服务端，前端只做"传对象标识 + 展示结果"这一件事。结果直接显示原始文本（**不渲染 markdown**：不引依赖，也避免把模型输出的 HTML 当代码执行）。
- 入口**三处**：智能分析页的主机详情抽屉（`IntelligenceView.vue`）、告警详情抽屉（`AlertsView.vue`）、资产详情抽屉（`AssetListView.vue`）。资产那处是后补的（用户 2026-10-08 确认需要）——**组件化的收益在这里体现**：补一处只花了"插一行 + 一个 import"。
- `web/src/api/http.js`：`diagnoseAI`。
- `dist/ai-e2e.py`（新）：**自带假 OpenAI 兼容端点**（照 `build/template-fakes/fakevm` 的先例）——不依赖外网，且能捕获"平台到底把什么发出去了"。

**e2e 判据**（核心不是"接口返回 200"）：

- **未配置时不发请求**：未启用时诊断 400，且假端点一次都没被访问；
- 配置与连通：GET 不回显明文密钥、`hasApiKey` 正确、请求确实带上了鉴权头；
- 主机诊断：上下文含该主机与平台的分析结论段，且**不含其它任何主机名**（本机诊断只带这一台）；
- 敏感内容：默认**不发**属性值但保留字段名、`notSent` 如实告知；打开 `AllowSensitive` 后值进入上下文且响应标记 `sensitive`；
- 范围：受限身份（分组为空）诊断任何主机都被拒，且**被拒时没有发出请求**；
- 审计：`ai:diagnose` 与 `ai:config:update`/`test` 都留痕。

**打包**：`1.30.59`（full 201 MiB / upgrade 81 MiB）。已手工核对：6 个二进制齐全、manifest 引用的 8 个 component（含 `deploy/agent-install.sh`）全在包内、`web/index.html` 在包内、**新增 `web/assets/AiDiagnosePanel-*.js` 与 `.css`**、server 二进制内嵌 `1.30.59`。

**未做**：无（三处入口已齐：主机 / 告警 / 资产）。

**已实机验证（2026-10-08，dev-server 1.30.60）**：`dist/ai-e2e.py core` **33 项全过、0 失败**（脚本自带假 OpenAI 兼容端点，并**捕获实际发出的请求体**）。六组判据：

- **未配置时不发请求**：未启用时诊断 400 并指路到配置页，且假端点一次都没被访问；
- 配置与连通：GET 不回显明文密钥、`hasApiKey` 正确、**请求确实带 `Bearer` 鉴权头**；
- 主机诊断：上下文含该主机与平台的分析结论段，且**不含其它任何主机名**；
- 敏感内容：默认**不发**属性值但保留字段名、`notSent` 如实告知；打开 `AllowSensitive` 后值进入上下文且响应标记 `sensitive`；
- 范围：受限身份（分组为空）诊断任何主机都被拒，且**被拒时没有发出请求**；
- 审计：`ai:diagnose` 与 `ai:config:update` / `test` 都留痕。

**实机验证抓到的真缺陷（已修，1.30.60）**：资产诊断默认不取属性值，但「最近变更」段把变更记录的**旧值与新值**一起发了——而变更的新值就是属性值，等于「不取属性值」这道防线被同一份数据的**另一个出口**绕过了。已改为只发字段名、时间与来源，并补回归用例。**单测没抓到它的原因**：用例里的种子资产走的是**采集来源**（不产生变更记录），那条路径根本没被触发——这类洞只能靠"核对实际发出去的字节"发现。

**两处 e2e 脚本自身的问题（非产品）**：初版断言"带鉴权头"时查的是**请求体**（token 在 header 里，请求体当然没有）；审计接口路径写成了 `/api/v1/audit`（实际是 `/api/v1/audit/events`）。
