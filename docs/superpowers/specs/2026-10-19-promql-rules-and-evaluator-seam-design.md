# 告警规则 PromQL 类型与评估器接缝（设计）

- 日期：2026-10-19
- 状态：待评审
- 一句话：**给告警引擎加一种 `promql` 规则类型，让新增指标告警只改配置；同时把周期性评估抽成可注册的评估器接口，为后续规则类型扩展留出接缝。**

---

## 1. 目标与非目标

### 目标

1. 用户写一条 PromQL 表达式即可定义告警，不再需要写 Go 代码并重新发版。
2. 把周期性评估（阈值 / 主机离线 / 服务离线 / 主从切换 / 集群故障 / PromQL）收敛到统一的评估器接口；以后新增规则类型 = 新增一个文件 + 注册，不改 `engine.go` 的分发。
3. 现有五种规则类型的行为**保持不变**。
4. 前端支持选择 PromQL 规则、填写表达式、保存前试运行预览。

### 非目标

- 不改现有阈值规则、场景规则的语义与字段。
- 不做 PromQL 模板的自动播种（避免升级时凭空多出规则）。
- 不做表达式语法高亮、自动补全、历史记录。
- 不把安全事件规则塞进评估器接口（见 §3.2）。

---

## 2. 现状（已核实）

| 事实 | 证据 |
|---|---|
| 规则类型是 `AlertRule.Type` 上的字符串，空串等价于 `threshold` | `internal/model/metric.go:782-793` |
| 引擎按类型 switch 分发；未知类型回退到阈值评估 | `internal/server/alert/engine.go:366-379` |
| 五种周期性评估函数都挂在 `*Engine` 上、直接读引擎内部状态 | `engine.go:522`（阈值）、`:571`（离线）、`:625`（服务离线）、`:677`（主从）、`:733`（集群故障） |
| 安全事件是队列驱动：`IngestSecurityEvents` 入队，评估循环消费 | `engine.go:206-228`、`:231-270` |
| 告警去重键为 `规则ID|节点|instance` | `engine.go:1899-1901`（`firingKey`） |
| 规则校验集中在 `ValidateRule`，按类型分支 | `internal/server/alert/rules.go:558-621` |
| 存储层已有发送原始 PromQL 的私有方法 `queryExpr` | `internal/server/storage/querier.go:135-151` |
| 存储抽象接口没有暴露原始表达式查询 | `internal/server/storage/vm.go:109-127` |
| 规则创建/更新接口调用 `ValidateRule` 后直接落库 | `internal/server/api/query.go:1327-1360` |
| 前端规则类型下拉与阈值表单按 `form.type` 分支渲染 | `web/src/components/RuleModal.vue:30-37`、`:62-136` |

---

## 3. 设计

### 3.1 评估器接缝

新增 `internal/server/alert/evaluators.go`：

```go
// evaluator 是一种周期性规则类型的评估器。
type evaluator interface {
    Evaluate(c *evalContext)
}

// evalContext 是评估器可用的引擎能力（Engine 持锁时构造）。
// 只暴露评估需要的动作，不把 *Engine 交给评估器。
type evalContext struct {
    e     *Engine
    rule  model.AlertRule
    nodes []model.Node
    now   int64
}

func (c *evalContext) Fire(node, instance string, value float64, msg ...string)
func (c *evalContext) Resolve(node, instance string, value float64, msg ...string)
func (c *evalContext) State(key string) *ruleState
func (c *evalContext) MaybeEscalate(node, instance string, value float64, st *ruleState)
// 指标与集群读取：封装现有 latestMetricSamplesIn / latestRoleSamples / LoadClusterMembers，
// 评估器不直接接触 store。
func (c *evalContext) LatestSamples(node, metric string, labels map[string]string) []metricSample
func (c *evalContext) LatestSamplesIn(node, metric string, labels map[string]string, lookback time.Duration) []metricSample
func (c *evalContext) RoleSamples(node, metric string, labels map[string]string) []roleSample
func (c *evalContext) ClusterMembers(service string, nodeNames []string) []ClusterMember
func (c *evalContext) QueryPromQL(expr string) ([]model.Series, error)
```

注册表：

```go
var periodicEvaluators = map[string]evaluator{
    model.RuleTypeThreshold:   thresholdEvaluator{},
    model.RuleTypeNodeOffline: nodeOfflineEvaluator{},
    model.RuleTypeServiceDown: serviceDownEvaluator{},
    model.RuleTypeRoleChange:  roleChangeEvaluator{},
    model.RuleTypeClusterFault: clusterFaultEvaluator{},
    model.RuleTypePromQL:      promQLEvaluator{},
}

// evaluatorFor 对未知/空类型回退到阈值评估，保持现有行为。
func evaluatorFor(t string) evaluator
```

`evaluate()` 的循环保持不变（锁、静默、周期静默、清理），只把分发替换为：

```go
if r.Type == model.RuleTypeSecurityEvent {
    e.evalSecurityEvents(r, nodes, now)
    continue
}
evaluatorFor(r.Type).Evaluate(&evalContext{e: e, rule: r, nodes: nodes, now: now})
```

### 3.2 现有评估器的迁移

| 文件 | 内容 | 迁移方式 |
|---|---|---|
| `eval_threshold.go` | `evalThreshold` | 原样搬入 `thresholdEvaluator` |
| `eval_offline.go` | `evalNodeOffline` | 原样搬入 |
| `eval_service_down.go` | `evalServiceDown` | 原样搬入 |
| `eval_role_change.go` | `evalRoleChange` | 原样搬入 |
| `eval_cluster.go` | `evalClusterFault` | 原样搬入 |
| `eval_security.go` | `evalSecurityEvents` / `emitRuleSecurityEvent` | 仅移动文件，**不进接口** |
| `eval_promql.go` | 新增 | 见 §3.4 |

**为什么安全事件不进接口**：它是事件队列驱动，不是「读当前状态 → 判断」。统一接口会迫使 `evalContext` 暴露 `securityEvents`、`securityRuleEvents`、事件出队等内部状态，等于把耦合换个地方放。两条路径的边界写在 `evaluators.go` 的包注释里。

`ruleState` 暂不拆分：现有类型共用 `aboveSince/firing/firedAt/escalated/lastRepeat/resolvedAt`，PromQL 也只需要这几个通用字段。等出现真正类型专属的状态时，再为评估器引入独立状态存储。

### 3.3 PromQL 规则模型

`model.AlertRule` 新增：

```go
Expr string `json:"expr,omitempty"` // PromQL 表达式（type=promql 时必填）
```

新增规则类型常量：

```go
RuleTypePromQL string = "promql"
```

`ValidateRule` 增加分支：

- `type=promql`：`Expr` 去空白后不能为空；`Metric/Operator/Threshold` 不参与评估（允许留空）。
- 其余字段（`For/Severity/Scope/Nodes/Group/Notify/QuietPeriods/Escalation`）沿用现有校验。
- 结构校验保持纯函数，不查时序库；可执行性校验放在 API 层（§3.6），这样导入规则时仍可离线校验结构。

### 3.4 评估语义与告警身份

**语义**：表达式返回的每条序列就是一条告警实例。

- **序列存在 = 触发；序列消失 = 恢复**。条件写在表达式里（如 `cpu_usage > 85`、`absent(up{job="x"})`）。
- 序列的值不参与判断（值为 0 的序列只要出现在结果里也算触发）；这是 PromQL 的标准用法，文档里写明。
- `For` 语义与阈值规则一致：序列连续存在超过 `For` 才触发。

**归属与范围（混合）**：

- 结果带 `node` 标签（存在且非空）：按该节点归属，并应用规则的 `Group` / `Scope` / `Nodes` 过滤；不在范围内的序列直接忽略。
- 结果不带 `node` 标签（缺失或空串）：视为全局序列，不受节点范围限制。

**序列标识**：

- 由结果的全部标签按 key 排序拼成 `k=v` 串（含 `__name__`），保证同一序列跨轮次稳定。
- 去重键：`规则ID|node|序列标识`（全局序列的 `node` 为空串）。
- 事件字段：`Instance` = 序列标识；`Metric` = 表达式；`Operator/Threshold` 留空。
- 消息：`PromQL 规则「名称」命中：<序列标签> = <值>`。

**查询失败**：

- 记录日志（含规则名与错误），**保持上一轮状态**，本轮不触发也不恢复。避免时序库抖动造成误恢复、下一轮又误触发。
- 连续失败不自动禁用规则；失败信息在日志中可见。

**结果上限**：

- 单规则单轮最多处理 200 条序列；超出记警告日志并按序列标识排序后截断。截断只影响本轮，不改变已有状态。
- 上限是常量（`maxPromQLSeries = 200`），不做配置项。

**恢复扫描**：

- 每轮记录该规则本轮出现过的序列标识集合；遍历该规则已知的 firing 状态，未出现的 key 执行 `Resolve`。
- 状态键统一由 `规则ID + "|" + node + "|" + 序列标识` 构造，与 `firingKey` 一致。

### 3.5 查询接口与安全边界

`storage.Storage` 新增：

```go
// QueryPromQL 执行一条原始 PromQL 表达式，返回即时查询的向量结果。
// 仅供规则评估与试运行使用；调用方负责表达式来源的可信性。
QueryPromQL(expr string) ([]model.Series, error)
```

- `PromStorage` 实现直接复用现有私有 `queryExpr`，沿用配置的 `queryTimeout`。
- 测试替身（`internal/server/api`、`internal/server/alert` 里的 fake store）补一个可注入结果的实现。
- 表达式只读；不限制语言特性（MetricsQL 超集可用），但限制**结果条数**（§3.4）与**单次查询超时**（存储层已有）。
- 不限制表达式长度以外的内容；建议 API 层限制 body 大小（沿用现有 JSON body 上限）。

### 3.6 API 与试运行

**创建/更新**：

- `POST /api/v1/rules`、`PUT /api/v1/rules/{id}` 在 `ValidateRule` 之后，对 `type=promql` 额外执行一次 `QueryPromQL` 做可执行性校验。
- 查询报错 → 400，返回后端错误原文（如语法错误位置）。
- 时序库不可用时无法保存 PromQL 规则：平台本身依赖时序库，接受这个约束；错误信息里区分「表达式错误」与「后端不可用」。

**试运行**：

- 新增 `POST /api/v1/rules/promql/preview`，权限 `alerts:write`。
- 入参：`{"expr": "..."}`。
- 出参：

```json
{
  "count": 12,
  "truncated": false,
  "series": [
    {"labels": {"node": "web-01", "instance": "10.0.0.1:9100"}, "value": 91.2}
  ]
}
```

- 最多返回前 20 条序列；`truncated` 表示命中数超过 20。
- 只读，不写审计；查询失败返回 400。

### 3.7 前端

**`RuleModal.vue`**：

- 类型下拉增加 `promql` → 「PromQL 规则」。
- 该类型下显示：
  - 表达式 textarea（等宽字体，占位示例 `cpu_usage > 85`，支持多行）。
  - 「试运行」按钮：调用 preview 接口，就地展示命中条数与前几条标签；错误就地显示，不弹全局错误。
  - `For`、级别、作用范围、通知、静默、升级：复用现有控件。
- 阈值规则的「指标/运算符/阈值」区块在 `type=promql` 时不渲染。

**`AlertsView.vue`**：

- 规则列表与告警详情对 `type=promql` 显示表达式（可截断 + tooltip），不显示「指标 运算符 阈值」。
- 告警事件详情显示 `Instance`（序列标签）与表达式。

---

## 4. 测试策略

### 4.1 重构前：特征测试（必须先全绿）

在动生产代码之前，为五个现有评估器补通过 `evaluate()` 驱动的行为测试，锁定：

- 阈值：条件成立未满 `For` 不触发、满 `For` 触发、条件消失恢复。
- 主机离线：离线满 `For` 触发、恢复上线后 resolve。
- 服务离线：存活指标为 0 触发。
- 主从切换：role 标签变化触发。
- 集群故障：分类函数返回异常时触发。

这些测试在重构前后都必须通过，用来证明「原样搬家」。

### 4.2 新增测试

- `ValidateRule`：promql 缺 `Expr` 拒绝；其它类型行为不变。
- 引擎：
  - 序列出现触发、消失恢复；
  - `For` 生效；
  - 带 `node` 的序列受 `Scope` 过滤，不带 `node` 的全局序列不受限；
  - 查询失败保持上一轮状态（不 resolve、不重复触发）；
  - 200 条截断；
  - 同一序列两轮之间标识稳定（标签顺序不影响 key）。
- 存储：`QueryPromQL` 原样转发表达式并解析结果（用 httptest 假后端）。
- API：preview 正常/错误；保存非法表达式返回 400；无 `alerts:write` 返回 403。
- 前端：`RuleModal` 切换到 promql 显示表达式与试运行结果；`AlertsView` 对 promql 规则显示表达式。

### 4.3 全量回归

- `go test ./...` 全绿。
- 前端 `vitest run` 全绿。

---

## 5. 不做清单

| 不做 | 原因 |
|---|---|
| 把安全事件规则塞进评估器接口 | 事件驱动与周期评估是两种流程，硬合并会泄漏内部状态（§3.2） |
| PromQL 规则自动播种 | 表达式依赖具体指标与阈值，自动创建容易误报 |
| 表达式语法高亮/补全 | 与本次目标无关 |
| 记录规则最近一次评估错误到界面 | 需要新的规则状态字段与展示；先用日志，后续按需补 |
| 结果条数上限做成配置项 | 先固定 200，等有真实需求再开放 |

---

## 6. 实施批次

| 批次 | 内容 | 产物 |
|---|---|---|
| 1 | 五个现有评估器的特征测试 | Go 测试（不改生产代码） |
| 2 | 评估器接口 + 注册表；五种评估器原样迁移；安全事件移到独立文件 | Go 重构，全量测试绿 |
| 3 | PromQL 规则模型、`QueryPromQL`、`evalPromQL`、引擎测试 | Go + 测试 |
| 4 | API 保存校验 + preview 接口 + 测试 | Go + 测试 |
| 5 | 前端表单、试运行、告警展示 + 组件测试 | Vue + 测试 |
| 6 | 文档（规则类型与示例、CONTEXT 术语）、版本号 | 文档 |

版本号按项目惯例整批递增。

---

## 7. 验证口径

- 单测：§4.2 全部通过；§4.1 特征测试在重构前后均通过。
- 实机（dev-server）：
  1. 新建一条 PromQL 规则（如 `cpu_usage > 1`），试运行显示命中；
  2. 保存后一个评估周期内出现告警，表达式与序列标签正确；
  3. 修改表达式使其不再命中，下一周期告警恢复；
  4. 保存一条语法错误的表达式，接口返回 400 且错误信息可读。
- 界面类必须真实浏览器走一遍。

---

## 8. 风险与回滚

| 风险 | 缓解 |
|---|---|
| 评估器迁移改变现有行为 | 先写特征测试，再原样搬家；迁移批次与功能批次分开 |
| PromQL 表达式拖垮时序库 | 结果条数上限 200；沿用存储层查询超时；试运行与保存校验都走同一超时 |
| 查询失败导致误恢复 | 失败时保持上一轮状态，不 resolve |
| 保存校验依赖时序库可用性 | 接受；错误信息区分表达式错误与后端不可用 |
| 回滚 | 功能按批次提交；评估器迁移与 PromQL 功能分开，可单独回退功能批次 |
