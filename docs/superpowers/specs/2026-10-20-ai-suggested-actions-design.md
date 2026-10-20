# AI 建议处置动作并接入审批设计

- 日期：2026-10-20
- 状态：待评审
- 前置：阶段 1（值班台与模块开关）、阶段 2（AI 只读工具层与自动调查）、高风险操作双人复核（2026-10-07）
- 归属：产品改造阶段 3 ——「AI 建议写动作 → 接审批单」

---

## 1. 起因与依据

阶段 2 划定的边界是：**诊断不需要人参与，解决问题才需要人参与**。现在 AI 能查、能下结论，但结论里的「建议重启 nginx」只能由人再打开操作页手动做一遍——中间那一步没有接上。

既有能力已经齐了两块：

1. **审批状态机**（`internal/server/approval`）已经完整：不可自批、30 分钟超时失效、批准后自动执行、审批链审计。
2. **参数化动作目录**（`internal/server/ops/catalog.go`）是唯一动作白名单，参数校验只有 `ops.Validate` 一处实现。

缺的只是：**AI 的建议如何安全地进入这条链**。

阶段 2 设计件的不做清单写着「写操作工具：写动作必须走审批与护栏；模型不能直接改变系统状态」。本设计**不放开**这条，而是把「模型提议 → 人确认 → 审批 → 执行」拆成四段，每段都有独立的责任主体。

---

## 2. 现状（均已核实）

| 事实 | 证据 |
|---|---|
| 工具层 7 个只读工具；`executeTool` 硬拦 `ReadOnly=false` 的工具 | `internal/server/ai/agent.go`、`internal/server/api/ai_tools.go` |
| 动作参数校验唯一实现是 `ops.Validate`（必填 / 正则 / 未知参数拒绝） | `internal/server/ops/catalog.go:277` |
| `approvalGate` 按 `Config.Enabled` 与 `requireFor` 判定；关闭时**直接下发** | `internal/server/api/approval_api.go:63` |
| `approval.Store.Create` 接受完整动作；批准后 `executeApproved` 以 `req.Requester` 建 ops 任务 | `internal/server/api/approval_api.go:568` |
| `approval.Request` 有 `Extra map[string]string`，可用于来源标记，不需要改 schema | `internal/server/approval/store.go:108` |
| 调查存储是 JSON 文件；`Investigation` 目前没有建议字段 | `internal/server/ai/investigations.go` |
| 调查是并发执行的（全局并发 2），任何包级共享状态都会串台 | `internal/server/ai/investigator.go:47` |
| 前端 `AIInvestigationPanel` 只展示结论与步骤，没有操作入口 | `web/src/components/alert/AIInvestigationPanel.vue` |

---

## 3. 设计

### 3.1 三条硬规则

1. **模型只记录建议**：不创建审批单，不执行动作。
2. **人点提交才产生审批单**：提交人就是审批单的申请人，`Operator` 记他，不记 AI。
3. **AI 来源的审批单无条件走审批**：不受 `approval.enabled` 与 `requireFor` 影响。即使全局审批开关关闭，模型产出的动作也不能直连执行。

第 3 条是本设计与 `approvalGate` 刻意不同的地方：`approvalGate` 回答「这个入口现在要不要人审」，而 AI 建议回答「模型的话能不能直接变成动作」。后者的答案永远是「不能」。

### 3.2 建议的产生：`propose_action` 工具

新增第 8 个工具：

```
kind   string  动作标识（动作目录里的取值，如 svc.restart）
params object  动作参数（键值均为字符串）
reason string  建议理由（一句话，会展示给审批人）
```

服务端校验顺序（任何一步失败都返回可读错误，模型可在预算内重试）：

1. `kind` 在动作目录中存在；
2. 动作 `ReadOnly == false`（只读动作没有「处置」意义，直接执行即可）；
3. 动作不在排除集里；
4. `params` 通过 `ops.Validate`（必填、正则、未知参数拒绝）；
5. **目标节点声明支持该动作**（`ops.Service.CheckSupport`，与人工提交同一条能力协商）——
   提前到建议阶段，避免记录一条注定执行失败的建议；
6. 本次调查的建议数 < 3，且与已有建议不完全重复。

**排除集**：`file.push`。`fileId` 只能来自一次上传，模型既看不到也造不出；允许它只会产生注定失败的建议。

工具成功时返回给模型：

```
已记录建议：重启服务 nginx.service @ web-01
（不会自动执行；需人工提交，并由另一位同事批准）
```

这句回执是**提示词的一部分**：让模型知道建议没有产生任何实际动作，避免它在后续轮次里重复提交或声称「已处理」。

### 3.3 数据模型

```go
// Suggestion 是模型提出的一条处置建议。它只是一个记录，不代表任何动作已被提交。
type Suggestion struct {
	ID     string            `json:"id"`
	Kind   string            `json:"kind"`
	Title  string            `json:"title"`            // 动作标题（来自动作目录）
	Params map[string]string `json:"params,omitempty"` // 已按目录规格归一化
	Node   string            `json:"node"`             // 固定为调查范围节点
	Reason string            `json:"reason,omitempty"` // 模型给的理由，截断 300 字
}
```

挂在 `ai.Investigation.Suggestions []Suggestion`，最多 3 条。旧记录没有该字段，反序列化为空切片，向后兼容。

### 3.4 工具如何写回调查

`ai.ToolScope` 增加一个回调：

```go
type ToolScope struct {
	Node           string
	AllowSensitive bool
	// Propose 把一条通过校验的建议记入本次调查。为 nil 时工具不可用。
	Propose func(Suggestion) error
}
```

`apiToolbox` 的工具闭包调用它，由 `runInvestigation` 注入一个只写当前调查的回调。

两点实现约束：

1. **回调只累积到本次运行的局部切片**，随最终那次 `Update` 一并落库。不要在每次建议时调 `aiInv.Update`：`Update` 会无条件写 `EndedAt`，一条还在跑的调查会提前显示成「已结束」。
2. `Propose == nil` 时 `Tools()` **不返回** `propose_action`。让模型看到却调不动，只会浪费一次工具预算。

工具自身的 `ReadOnly` 标为 `true`：这个字段在本仓库的语义是「不改变被监控系统的状态」，记录一条建议不改变任何远端状态。真正的写动作只发生在审批通过之后。

**为什么用回调而不是包级状态**：调查是并发的（全局并发 2），包级变量会让两条调查的建议互相串台；回调天然绑定到单次运行。

### 3.5 提交接口

```
POST /api/v1/ai/investigations/{id}/suggestions/{sid}/submit
```

| 项 | 取值 |
|---|---|
| 权限 | `ai:invoke` + `ops:exec` + 动作自带的 `Perm`（如 `container:write`） |
| 请求体 | 无（动作与参数全部来自已落库的建议） |
| 成功 | `202` + `{pending:true, approval:{...}, message:"已提交审批：需另一位同事批准后才会执行"}` |
| 失败 | 调查/建议不存在 404；节点不在范围 404；节点离线 409；参数复验失败 400；审批存储未注入 503 |

提交时**重新校验**，不信任落库值：

1. 调查存在，且 `Node` 在当前用户的资源范围内；
2. 建议存在，`Kind` 仍在动作目录中、仍是写动作、仍不在排除集；
3. `ops.Validate` 重跑一遍参数；
4. 节点存在且在线（离线节点的指令必然等到过期，与 `handleOpsCreate` 同口径）；
5. 节点声明支持该动作（`ops.Service.CheckSupport`，与人工提交同一条能力协商）——不把注定失败的单子推给审批人。

审批单字段：

```go
approval.Request{
	Kind:    approval.EntryOpsWrite,
	OpsKind: sug.Kind,
	Node:    sug.Node,
	Params:  params, // 复验后的归一化值
	Title:   "AI 建议：" + action.Title + "（写操作） @ " + sug.Node,
	Extra: map[string]string{
		"source":          "ai",
		"investigationId": inv.ID,
		"suggestionId":    sug.ID,
	},
}
```

`Requester` 由 `submitApproval` 从请求上下文填入当前用户；批准后 `executeApproved` 以申请人名义建任务，责任链保持完整。

审计：`ai:suggestion:submit`，Detail 含调查号、建议号、动作、节点、审批单号。

### 3.6 前端

`AIInvestigationPanel` 新增「建议处置」区块（有建议时才出现）：

- 每条：动作标题、参数键值、模型理由、**提交审批**按钮；
- 三态：可提交 / 提交中 / 已提交（显示审批单号）；
- 区块顶部固定一句：**「AI 只给建议，不会自动执行；提交后需另一位同事批准」**；
- 节点离线或参数失效时按钮置灰并说明原因（服务端仍会再拦一次）。

### 3.7 系统提示

在现有「只根据工具返回的数据作答」之后追加：

> 若结论指向一个可执行的处置动作，可调用 `propose_action` 记录建议。只有工具结果里有明确依据时才建议；最多 3 条；不要建议文件分发。建议不会自动执行，需人工提交并由另一位同事批准。

---

## 4. 决策点

| # | 问题 | 结论 | 理由 |
|---|---|---|---|
| **D1** | AI 是否自动创建审批单 | **否**，人点提交 | 自动创建等于模型能往审批队列写东西；审批人看到的会是「AI 提的单」，责任链断掉 |
| **D2** | 审批开关关闭时 AI 建议怎么办 | **仍走审批** | 开关回答的是「人提交的动作要不要复核」；模型产出的动作没有「免复核」这个选项 |
| **D3** | 建议范围 | 写类 ops 动作，**排除 `file.push`** | `fileId` 模型无从得知；fail2ban 与升级不是参数化 ops 动作，留给后续 |
| **D4** | 目标节点由谁定 | **调查范围节点**，模型不可指定 | 与只读工具的 `requireScopeNode` 同一条：参数里的节点不可信 |
| **D5** | 每条调查最多几条建议 | **3 条**，完全相同的去重 | 超过 3 条开始变成噪音；审批人要的是可判断的选项，不是清单 |
| **D6** | 提交权限 | `ai:invoke` + `ops:exec` + 动作 `Perm` | 缺 `ops:exec` 的人不能借 AI 绕过自己的权限边界 |
| **D7** | 来源标记 | 审批单 `Extra` 记 `source=ai` / 调查号 / 建议号 | 不改审批单 schema；审批页与审计都能追溯 |
| **D8** | 工具回执对模型说什么 | 明确「已记录，不会自动执行」 | 否则模型会以为动作已发生，在后续轮次里重复建议或声称已处理 |
| **D9** | 手动诊断是否也接建议 | **本批不接** | 手动诊断是单轮对话，没有工具循环与调查记录；接入需要另一套上下文，单独评估 |

---

## 5. 测试策略

- **工具**：未知 kind、只读动作、`file.push`、非法参数、超过 3 条、重复建议、`Propose == nil` 时不可用。
- **Agent**：工具回执包含「不会自动执行」；建议不影响只读工具的执行路径。
- **调查存储**：建议随调查落盘、重启后可读、旧记录无该字段时为空。
- **API**：提交成功生成审批单（`Extra` 正确、标题前缀正确、`Requester` = 当前用户）；无 `ai:invoke` / 无 `ops:exec` / 无动作 `Perm` 各自 403；范围外节点 404；离线节点 409；参数被篡改后 400；审批存储未注入 503；审计一条。
- **审批链**：提交 → 他人批准 → `executeApproved` 建任务且 `Operator` = 提交人；自批被拒（既有状态机，回归）。
- **前端**：建议渲染、提交后显示审批单号、无建议时不出现区块、AI 不会自动执行的提示存在。

---

## 6. 不做清单

| 不做 | 原因 |
|---|---|
| AI 自动提交审批 | 见 D1 |
| 建议 `file.push` | `fileId` 模型无从得知 |
| 建议 fail2ban 处置与两种升级 | 不是参数化 ops 动作，接入面另算 |
| 新增权限点 | 复用 `ai:invoke` / `ops:exec` / 动作 `Perm` 已经足够 |
| 修改审批状态机 | 状态机已完整；本批只新增一个「创建审批单」的入口 |
| 手动诊断接建议 | 见 D9 |
| 让模型直接调用 ops 接口 | 模型永远不持有执行能力 |

---

## 7. 风险与回滚

| 风险 | 缓解 |
|---|---|
| 提示注入诱导模型建议危险动作 | 服务端复验参数与范围；提交必须人点；审批仍需第二人 |
| 建议噪音淹没结论 | 最多 3 条 + 去重 + 提示词约束 |
| 模型声称「已处理」 | 工具回执明确「不会自动执行」；界面同样标注 |
| 旧调查记录反序列化失败 | 新字段可选；`suggestions` 为空即不渲染 |
| 回滚 | 关闭 `autoInvestigate` 即停止自动链路；提交接口可单独下线，不影响既有审批 |

---

## 8. 验证口径

**单测**：§5 全部通过；`go test ./...` 与 `npm test` 全绿。

**实机（dev-server）**：

1. 配置可用的 OpenAI 兼容端点，开启 `autoInvestigate`；
2. 制造一条 critical 告警，等待调查完成并出现至少一条建议；
3. 点「提交审批」→ 202，审批中心出现一条「AI 建议：…」的待审批单，`Extra` 含调查号；
4. 提交人自己批准 → 被拒（不可自批）；
5. 另一位有权限的账号批准 → 生成 ops 任务，`Operator` = 提交人；
6. 审计里能看到 `ai:suggestion:submit` 与审批链；
7. 全局审批开关关闭时，AI 建议仍然只能进审批、不能直连执行。

**未覆盖边界（不得视为通过）**：浏览器实点只做人工确认；多实例/HA 未做；不可篡改审计未做（沿用审批设计件 D9）。

---

## 9. 实施与实机验证记录（2026-10-20，Server 1.35.10）

四个 TDD 任务已落地；最终审查修复了三个 Important（重复提交生成两张审批单、重跑后提交状态串调查、模型理由丢失）。

**实机（dev-server）**

- **确定性 e2e**（假 OpenAI 端点，33 项断言全过）：调查产出建议 → 提交 202 → 自批 403 → 他人批准 `executed` 且 ops 任务 `Operator` = 申请人 → 审计链完整 → 审批开关关闭时仍只生成待审批单、不直接建任务。
- **能力协商**（负向，7 项断言全过）：节点未放行 `svc.restart` 时，`propose_action` 返回可读拒绝原因（指向 agent.yaml 的 `guards.ops`），调查仍正常收尾。
- **真实模型**：对 Docker / MySQL 离线告警各跑一次调查，均完成工具循环并给出结论；这两类告警没有能干净映射到现有写动作（Docker 容器不是 K8s 工作负载，MySQL 连接离线无法判定是否该重启），因此未提出建议——符合「只有工具结果里有明确依据时才建议」。

**审查后根据实机观察追加的两处**

1. **调查 token 预算默认 8000 → 16000**：实机 3 轮调查累计 12.7k–15.3k tokens，8000 会在第 2–3 轮截断调查。
2. **能力协商前移到建议与提交阶段**（`ops.Service.CheckSupport`）：节点没放行的动作不再进入建议或审批单，避免审批人批一条注定执行失败的动作。

**未覆盖边界（不得视为通过）**：浏览器实点只做人工确认；多实例/HA 未做；不可篡改审计未做（沿用审批设计件 D9）。
