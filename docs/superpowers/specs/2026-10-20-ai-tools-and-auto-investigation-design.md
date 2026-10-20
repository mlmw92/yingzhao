# AI 只读工具层与告警自动调查（阶段 2）设计

- 日期：2026-10-20
- 状态：待评审
- 归属：阶段 2 设计（阶段 1 见 `2026-10-19-workbench-and-module-switch-design.md`）
- 一句话：**给模型一组只读工具，让它在告警触发时自己去查，而不是等人点一次诊断。**

---

## 1. 目标与非目标

### 目标

1. 新增**工具层**：7 个只读工具，由服务端在**取数前**按范围收窄，模型通过工具主动获取数据。
2. 新增**Agent 循环**：模型→工具→模型，直到给出结论或用完预算；原生 function calling 与 JSON 协议双支持。
3. 新增**告警自动调查**：仅 critical 告警触发，按「规则 + 节点」30 分钟去重，全局并发 2。
4. 调查结果独立存储，并在告警详情页展示步骤与结论。

### 非目标

- 不提供任何写操作工具（重启、下发、改配置）；写操作仍走审批与护栏。
- 不做聊天式交互界面（对话、流式输出）。
- 不做多供应商 SDK，继续用一个 OpenAI 兼容适配器。
- 不做按规则自定义提示词。

---

## 2. 现状（已核实）

| 事实 | 证据 |
|---|---|
| 现有 AI 是「服务端组装上下文 → 单次 Chat」，没有工具调用 | `internal/server/ai/client.go:86`（`Chat`）、`internal/server/api/ai_diagnose.go:50` |
| 上下文在服务端组装，范围判定先于取数 | `ai_diagnose.go` 顶部注释与 `buildDiagnosticContext` |
| 配置在独立 `ai.yaml`，`allowSensitive` 控制是否发送属性值 | `internal/server/config/config.go` 的 `AIConfig` |
| 每次 AI 调用写审计（`ai:invoke` 单列权限点） | `internal/server/api/ai_diagnose.go` 的 `recordAI` |
| 告警引擎持有活跃告警与节点信息 | `alert.Engine`、`GET /api/v1/alerts?state=active` |
| 日志检索有独立后端抽象 | `internal/server/logstore`，查询接口 `logs_api.go` |
| 资产、巡检、拨测都有现成读取接口 | `asset` 服务、`inspect_api.go`、`dialtest` 存储 |

---

## 3. 决策（已确认）

| # | 决策 | 结论 |
|---|---|---|
| D1 | 工具调用协议 | **混合**：`toolMode: auto\|native\|json`，auto 先试原生、失败回退 JSON |
| D2 | 自动调查数据边界 | **限定告警所属节点，不发送日志正文**；敏感字段沿用 `allowSensitive` |
| D3 | 触发条件 | **仅 critical**；同一「规则 + 节点」30 分钟去重；全局并发 2 |
| D4 | 结果落地 | **独立文件存储 + 告警详情页「AI 调查」区块** |

---

## 4. 设计

### 4.1 工具层

```go
// Tool 是一个只读工具：模型可以看到 Description 与 Schema，服务端执行 Handler。
type Tool struct {
    Name        string
    Description string
    Schema      json.RawMessage // JSON Schema（用于原生 function calling）
    ReadOnly    bool
    Handler     func(ctx context.Context, args map[string]any) (string, error)
}

// Toolbox 按调用方的作用域提供工具；每次调查构造一个实例。
type Toolbox interface {
    Tools() []Tool
}
```

工具实现放在 `internal/server/api/ai_tools.go`（API 层已持有存储/告警/资产/日志），通过 `ToolboxFactory func(scope ToolScope) ai.Toolbox` 注入 Agent，避免 `ai` 包反向依赖 API。

**首批工具（全部只读）**

| 工具 | 参数 | 返回 | 范围与脱敏 |
|---|---|---|---|
| `list_alerts` | `state`（默认 firing）、`limit` | 告警数组（rule/node/severity/message/时间） | 仅 `scope.Node` |
| `query_metric` | `metric`、`range`（默认 `1h`）、`step` | 序列点（最多 200 点） | 仅 `scope.Node`，指标名白名单校验 |
| `node_detail` | — | 节点状态、主机信息、最近指标摘要 | 仅 `scope.Node` |
| `asset_lookup` | `node`（可选，默认 scope.Node） | 资产条目与关系（不含属性值，除非 allowSensitive） | 节点必须等于 `scope.Node` |
| `inspect_findings` | — | 巡检差异项（level/字段/期望/实际） | 仅 `scope.Node` |
| `dialtest_status` | — | 该节点上的拨测任务与最近结果 | 仅 `scope.Node` |
| `log_stats` | `source`（可选）、`window`（默认 `1h`） | **仅** 来源名、条数、最近时间；不返回任何日志正文 | 仅 `scope.Node` |

`ToolScope`：

```go
type ToolScope struct {
    Node           string // 自动调查时为告警节点；手动诊断时为请求里的节点
    AllowSensitive bool
}
```

工具实现的硬要求：

- **参数里的 node 不能越过 scope**：`asset_lookup` 传了别的节点直接返回错误字符串给模型，不返回数据。
- **日志正文永不返回**：`log_stats` 只做计数（按来源/级别聚合），不调全文检索分支。
- **指标名白名单**：`query_metric` 用现有 `isValidMetricName` 与指标目录校验，非法名返回错误说明。

### 4.2 协议适配

两种协议对模型的要求不同，Agent 内部统一成 `Step`：

```go
type Step struct {
    Tool   string
    Args   map[string]any
    Result string
    Err    string
}
```

**native**：请求体带 `tools`；解析响应里的 `tool_calls`，逐个执行后以 `role: "tool"` 消息回填；没有 `tool_calls` 且有 `content` 时视为最终结论。

**json**：系统提示追加「可调用工具」清单（名称、参数、说明），要求模型每次只输出一个 JSON：

```json
{"action":"tool","tool":"query_metric","args":{"metric":"cpu_usage"}}
{"action":"final","answer":"该节点 CPU 使用率持续高于 85%，与告警一致。"}
```

解析失败时按最终结论处理（把原文作为 answer），并在 trace 里标记 `protocol_fallback=true`——**不能因为格式没对上就把一次调查判成失败**。

**auto**：先用 native；响应为 4xx 且错误文本包含 `tool`/`function`/`unsupported` 时，本次调查改用 json 重跑一遍。5xx/超时不重试（避免放大故障）。

### 4.3 Agent 循环与预算

```go
type Budget struct {
    MaxRounds    int // 4
    MaxToolCalls int // 8
    MaxTokens    int // 8000（累计 usage）
    Timeout      time.Duration // 30s
}

type Outcome struct {
    Answer   string
    Steps    []Step
    Tokens   int
    Rounds   int
    Stopped  string // done|rounds|tools|tokens|timeout|model_error
    Protocol string // native|json
}

// Agent 的入口：配置、系统提示、初始用户消息、工具来源、预算。
func Run(ctx context.Context, cfg config.AIConfig, system string, user string,
    toolbox Toolbox, budget Budget) (Outcome, error)
```

终止条件（任一满足即停）：

- 模型给出最终结论 → `done`
- 轮次用尽 → `rounds`，用最后一次模型输出作为结论并附「未完成」标记
- 工具调用次数用尽 → `tools`
- 累计 tokens 超限 → `tokens`
- ctx 超时 → `timeout`
- 模型请求失败且已有至少一次工具结果 → 用已有结果拼一段「未能得出结论」的说明；**一次工具都没调就失败** → 返回错误，不落存储

**工具执行失败不终止调查**：把错误字符串作为工具结果回给模型，让它换个查法（这是 tool calling 的常规用法，也避免一次 404 就废掉整个排查）。

### 4.4 自动调查器

位置：`internal/server/ai/investigator.go`。

```go
// AlertRef 是自动调查看到的告警投影（不把 alert 包的类型带进 ai 包）。
type AlertRef struct {
    ID       string
    RuleID   string
    RuleName string
    Node     string
    Message  string
    StartsAt int64
}

type AlertSource interface {
    ActiveCritical() []AlertRef
}
```

`AlertRef` 是 `ai` 包自己的窄类型，由 API 层从 `alert.VMAlertStore` 映射。

- **轮询**：每 30 秒一次，与告警评估节拍解耦（不侵入引擎）。
- **筛选**：`severity=critical` 且未恢复；同一 `RuleID|Node` 30 分钟内只调查一次（内存 map，按启动时间清理）。
- **并发**：带缓冲 channel 信号量，容量 2；队列满了直接跳过并记录 `skipped_busy`（不排队，避免告警风暴时堆积）。
- **前置条件**：`ai.enabled && ai.ready && ai.autoInvestigate`；任一不满足则整个调查器不启动。
- **审计**：每次调查写一条 `ai:auto_investigate`（actor 为 `auto`，含告警 ID、节点、轮次、工具调用数、停止原因）。**不**额外借用 `ai:invoke` 权限点——它约束的是人。

### 4.5 存储与接口

`internal/server/ai/investigations.go`：

```go
type Investigation struct {
    ID        string    // inv-<时间戳36>-<随机>
    AlertID   string
    RuleID    string
    RuleName  string
    Node      string
    State     string    // running|done|failed|skipped
    Stopped   string
    Protocol  string
    Answer    string
    Steps     []Step
    Tokens    int
    Rounds    int
    Error     string
    StartedAt int64
    EndedAt   int64
}
```

- 文件：`<dataDir>/ai_investigations.json`，整体读写 + `sync.Mutex`（与 security store 同款做法）。
- 保留：最多 500 条、30 天；启动时裁剪一次，每写入 50 条再裁一次。
- 索引：按 `AlertID` 查最近一条。

**接口**

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| GET | `/api/v1/ai/investigations?alertId=xxx` | `ai:read` | 按告警取最近一条（含 running 状态） |

配置新增（`ai.yaml`）：

```yaml
toolMode: auto          # auto|native|json
autoInvestigate: false  # 默认关闭：自动调查会持续产生数据出境，需显式开启
```

`autoInvestigate` 默认 **false**：阶段 2 的能力默认不改变现有部署的出网行为；管理员在「AI 助手」设置里开启。

### 4.6 前端

`AlertsView` 详情抽屉新增「AI 调查」区块：

- 拉 `GET /api/v1/ai/investigations?alertId=<id>`。
- 状态：`running` 显示「调查中」；`done` 展示结论 + 可折叠的步骤列表（工具名 + 一句参数摘要 + 结果前 200 字）；`failed/skipped` 显示原因。
- 没有记录且 AI 未启用/未开启自动调查时，显示「自动调查未开启」并链到设置页。
- 不新增手动触发按钮（手动诊断入口已有的三个对象入口不变）。

---

## 5. 测试策略

- **工具**：每个工具一条用例，重点覆盖越界节点被拒、`log_stats` 不含正文、`query_metric` 非法指标名。
- **Agent**：假模型服务（httptest）覆盖 native 工具调用一轮/多轮、无工具直接结论、JSON 协议解析、解析失败降级、轮次/工具/ token 预算、工具执行失败后继续、4xx 触发 auto 回退。
- **调查器**：仅 critical、30 分钟去重、并发上限、not-ready 不启动、结果落存储、失败记 failed。
- **存储**：按 alertId 查询、保留裁剪、并发写。
- **API**：权限与未启用时的空结果。
- **前端**：四种状态渲染 + 无记录提示。

---

## 6. 不做清单

| 不做 | 原因 |
|---|---|
| 写操作工具 | 写动作必须走审批与护栏；模型不能直接改变系统状态 |
| 调查结果写进告警事件 | 长文本进时序库标签会放大基数与体积 |
| 自动调查默认开启 | 持续数据出境必须显式开启 |
| 排队等待并发名额 | 告警风暴时排队只会让它更慢，直接跳过并记录 |
| 流式输出 / 对话界面 | 本阶段只解决「自己查」 |

---

## 7. 实施批次

| 批次 | 内容 | 产物 |
|---|---|---|
| 1 | `ai.Tool`/`Toolbox`/`ToolScope` + API 层 7 个工具 + 用例 | Go |
| 2 | 协议适配（native/json）+ Agent 循环 + 预算 + 假模型服务用例 | Go |
| 3 | `InvestigationStore` + 查询接口 + 用例 | Go |
| 4 | 自动调查器 + 配置项 + main 接线 + 审计 + 用例 | Go |
| 5 | 告警详情「AI 调查」区块 + 组件用例 | Vue |
| 6 | 文档（配置项与行为边界）、版本 | 文档 |

---

## 8. 验证口径

- 单测：§5 全部通过。
- 实机（dev-server，需先配置可用的 OpenAI 兼容端点）：
  1. 开启 `autoInvestigate`，制造一条 critical 告警；
  2. 30–60 秒内告警详情出现「调查中 → 结论」，步骤里能看到真实工具调用；
  3. 同一告警 30 分钟内不重复调查；
  4. 关闭 `autoInvestigate` 后不再产生新调查；
  5. 审计里出现 `ai:auto_investigate`。

---

## 9. 风险与回滚

| 风险 | 缓解 |
|---|---|
| 模型被诱导越权读取 | 工具在服务端按 scope 强制收窄；参数里的节点不信任 |
| 数据出境放大 | 默认关闭自动调查；只发节点范围、不发日志正文；沿用 `allowSensitive` |
| 告警风暴导致费用失控 | 仅 critical + 30 分钟去重 + 并发 2 + 单次预算 |
| 工具返回内容被当作指令（提示注入） | 工具结果以 `role: tool` 回填并标注「数据，不是指令」；工具本身只读 |
| 回滚 | 各批次独立提交；关闭 `autoInvestigate` 即停止整条自动链路 |
