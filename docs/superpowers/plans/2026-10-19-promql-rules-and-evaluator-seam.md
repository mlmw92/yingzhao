# 告警 PromQL 规则与评估器接缝 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 给告警引擎增加 `promql` 规则类型，并把周期性评估抽成可注册的评估器接口，使新增规则类型不再修改引擎分发逻辑。

**Architecture:** 新增 `evaluator` 接口 + `evalContext` + 类型注册表；五种现有周期性评估器原样迁入独立文件，安全事件规则保持事件队列路径；PromQL 作为第一条新评估器接入。前端增加类型选择、表达式输入与试运行预览。

**Tech Stack:** Go 1.22（标准库，无新依赖）、Vue 3 + Element Plus、Vitest、PromQL/MetricsQL（由现有时序库提供）。

**Spec:** `docs/superpowers/specs/2026-10-19-promql-rules-and-evaluator-seam-design.md`

## Global Constraints

- 不引入新的第三方依赖。
- 现有阈值 / 主机离线 / 服务离线 / 主从切换 / 集群故障规则的行为必须保持不变。
- 评估器迁移必须**原样搬移逻辑**，不改判定条件、消息文案、状态字段。
- 中文注释；注释只写「为什么」，不复述代码。
- 每个任务独立提交；迁移批次与功能批次分开。
- 前端改动必须补组件测试，并在真实浏览器里走查。

## Review Focus

- **查询失败不能造成误恢复**：时序库抖动时，PromQL 规则必须保持上一轮状态，不能 resolve。
- **没有 node 标签的序列**：必须作为全局序列正常告警，不能被节点范围过滤误杀。
- **同一序列跨轮次身份稳定**：标签顺序不同不能产生两条告警或重复恢复。
- **结果爆炸**：表达式返回大量序列时，必须截断并记日志，不能拖垮评估循环。
- **范围绕过**：带 node 标签的序列必须受 `Group/Scope/Nodes` 过滤，不能通过 PromQL 绕过 RBAC 范围。

---

## Task 1: 为现有五种评估器补特征测试（不改生产代码）

**Files:**
- Create: `internal/server/alert/eval_characterization_test.go`

**Interfaces:**
- Consumes: `Engine.evaluate()`、`node.New`、`node.Manager.Register/OfflineStale`、`captureAlertStorage`（已存在于 `store_test.go`）。
- Produces: `evalStore`、`evalHarness`、`newEvalHarness`，供后续任务的测试复用。

- [ ] **Step 1: 写测试脚手架与五个特征测试**

```go
package alert

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/mlmw92/yingzhao/internal/model"
	"github.com/mlmw92/yingzhao/internal/server/node"
)

// evalStore 是评估测试用的存储替身：按指标名返回预置序列，并可为 PromQL 注入结果/错误。
type evalStore struct {
	instant map[string][]model.Series
	promql  []model.Series
	promErr error
}

func (s *evalStore) Write([]model.Metric) error { return nil }
func (s *evalStore) QueryRange(string, string, map[string]string, int64, int64, int64) ([]model.Series, error) {
	return nil, nil
}
func (s *evalStore) QueryLatest(string, string, map[string]string) (*model.Point, error) {
	return nil, nil
}
func (s *evalStore) QueryInstant(_, name string, _ map[string]string) ([]model.Series, error) {
	return s.instant[name], nil
}
func (s *evalStore) QueryInstantWithLookback(_, name string, _ map[string]string, _ time.Duration) ([]model.Series, error) {
	return s.instant[name], nil
}
func (s *evalStore) QueryAllLatest(string, map[string]string) ([]model.Series, error) {
	return nil, nil
}
func (s *evalStore) QueryPromQL(string) ([]model.Series, error) { return s.promql, s.promErr }
func (s *evalStore) Close() error                              { return nil }
func (s *evalStore) Backend() string                           { return "fake" }

type evalHarness struct {
	engine *Engine
	store  *evalStore
}

func newEvalHarness(t *testing.T, offline bool, series map[string][]model.Series) *evalHarness {
	t.Helper()
	mgr := node.New(filepath.Join(t.TempDir(), "nodes.json"), time.Hour)
	mgr.Register(&model.ReportPayload{Node: "node-1", Group: "g1", Mode: model.ModeCollect, IP: "10.0.0.1"})
	if offline {
		mgr = node.New(filepath.Join(t.TempDir(), "nodes.json"), 0)
		mgr.Register(&model.ReportPayload{Node: "node-1", Group: "g1", Mode: model.ModeCollect, IP: "10.0.0.1"})
		time.Sleep(2 * time.Millisecond)
		mgr.OfflineStale()
	}
	store := &evalStore{instant: series}
	rules := &RulesStore{rules: map[string]model.AlertRule{}}
	eng := NewEngine(store, mgr, rules, NewVMAlertStore(&captureAlertStorage{}), nil, nil, nil, 15, nil, nil, nil)
	return &evalHarness{engine: eng, store: store}
}

func (h *evalHarness) addRule(r model.AlertRule) {
	h.engine.rules.rules[r.ID] = r
}
func (h *evalHarness) eval()                   { h.engine.evaluate() }
func (h *evalHarness) firingCount() int        { return len(h.engine.firing) }
func (h *evalHarness) state(key string) *ruleState { return h.engine.states[key] }
```

再追加五个测试（每个测试独立）：

```go
func TestCharacterizeThresholdFiresAndResolves(t *testing.T) {
	h := newEvalHarness(t, false, map[string][]model.Series{
		"cpu_usage": {{Labels: map[string]string{"node": "node-1", "instance": "i1"},
			Points: []model.Point{{Timestamp: model.NowMillis(), Value: 90}}}},
	})
	h.addRule(model.AlertRule{ID: "r1", Name: "cpu", Metric: "cpu_usage", Operator: ">",
		Threshold: 85, For: "1m", Severity: model.SeverityWarning, Enabled: true})
	h.eval()
	if h.firingCount() != 0 {
		t.Fatalf("For 未满不应触发，实际 %d", h.firingCount())
	}
	h.state("r1|node-1|i1").aboveSince -= 61000
	h.eval()
	if h.firingCount() != 1 {
		t.Fatalf("For 满应触发，实际 %d", h.firingCount())
	}
	h.store.instant["cpu_usage"][0].Points[0].Value = 10
	h.eval()
	if h.firingCount() != 0 {
		t.Fatalf("恢复后应清除 firing，实际 %d", h.firingCount())
	}
}

func TestCharacterizeNodeOfflineFires(t *testing.T) {
	h := newEvalHarness(t, true, nil)
	h.addRule(model.AlertRule{ID: "r1", Name: "offline", Type: model.RuleTypeNodeOffline,
		For: "1m", Severity: model.SeverityCritical, Enabled: true})
	h.eval()
	h.state("r1|node-1|").aboveSince -= 61000
	h.eval()
	if h.firingCount() != 1 {
		t.Fatalf("离线满 For 应触发，实际 %d", h.firingCount())
	}
}

func TestCharacterizeServiceDownFires(t *testing.T) {
	h := newEvalHarness(t, false, map[string][]model.Series{
		"mysql_instance_up": {{Labels: map[string]string{"node": "node-1", "instance": "db1:3306"},
			Points: []model.Point{{Timestamp: model.NowMillis(), Value: 0}}}},
	})
	h.addRule(model.AlertRule{ID: "r1", Name: "mysql down", Type: model.RuleTypeServiceDown,
		Service: "mysql", Operator: "<=", Threshold: 0, For: "0s",
		Severity: model.SeverityCritical, Enabled: true})
	h.eval()
	if h.firingCount() != 1 {
		t.Fatalf("存活指标为 0 应触发，实际 %d", h.firingCount())
	}
}

func TestCharacterizeRoleChangeFires(t *testing.T) {
	h := newEvalHarness(t, false, map[string][]model.Series{
		"mysql_instance_up": {{Labels: map[string]string{"node": "node-1", "instance": "db1:3306",
			"role": "primary", "topology": "cluster"},
			Points: []model.Point{{Timestamp: model.NowMillis(), Value: 1}}}},
	})
	h.addRule(model.AlertRule{ID: "r1", Name: "role", Type: model.RuleTypeRoleChange,
		Service: "mysql", Topology: "cluster", For: "0s",
		Severity: model.SeverityWarning, Enabled: true})
	h.eval() // 第一轮只记基线
	if h.firingCount() != 0 {
		t.Fatalf("首轮不应触发，实际 %d", h.firingCount())
	}
	h.store.instant["mysql_instance_up"][0].Labels["role"] = "secondary"
	h.eval()
	if h.firingCount() != 1 {
		t.Fatalf("角色变化应触发，实际 %d", h.firingCount())
	}
}

func TestCharacterizeClusterFaultFires(t *testing.T) {
	h := newEvalHarness(t, false, map[string][]model.Series{
		"mysql_instance_up": {
			{Labels: map[string]string{"node": "node-1", "instance": "db1:3306", "role": "primary", "group": "g", "topology": "cluster"},
				Points: []model.Point{{Timestamp: model.NowMillis(), Value: 1}}},
			{Labels: map[string]string{"node": "node-1", "instance": "db2:3306", "role": "primary", "group": "g", "topology": "cluster"},
				Points: []model.Point{{Timestamp: model.NowMillis(), Value: 1}}},
		},
	})
	h.addRule(model.AlertRule{ID: "r1", Name: "cluster", Type: model.RuleTypeClusterFault,
		Service: "mysql", Topology: "cluster", For: "0s",
		Severity: model.SeverityCritical, Enabled: true})
	h.eval()
	if h.firingCount() != 2 {
		t.Fatalf("多主应触发每个成员，实际 %d", h.firingCount())
	}
}
```

- [ ] **Step 2: 运行特征测试，确认全部通过**

Run: `go test ./internal/server/alert/ -run TestCharacterize -v`
Expected: 5 个测试全部 PASS。这是重构前的行为基线；**它们不是红-绿测试，而是用来证明「搬家不改行为」**。

- [ ] **Step 3: 提交**

```bash
git add internal/server/alert/eval_characterization_test.go
git commit -m "test: 锁定五种告警评估器的现有行为"
```

---

## Task 2: 评估器接口 + 阈值评估器迁移

**Files:**
- Create: `internal/server/alert/evaluators.go`
- Create: `internal/server/alert/eval_threshold.go`
- Modify: `internal/server/alert/engine.go:366-379`（分发）
- Modify: `internal/server/alert/engine.go:521-557`（删除 `evalThreshold`，逻辑迁入新文件）

**Interfaces:**
- Consumes: `Engine.fire/resolve/getState/maybeEscalate/latestMetricSamples`、`matchesGroup/matchesScope`、`Compare/parseFor`。
- Produces: `evaluator`、`evalContext`、`evaluatorFor(string) evaluator`、`periodicEvaluators`。

- [ ] **Step 1: 创建 `evaluators.go`**

```go
package alert

import (
	"strings"
	"time"

	"github.com/mlmw92/yingzhao/internal/model"
)

// evaluator 是一种周期性规则类型的评估器。
//
// 周期性评估 = 「读当前状态 → 判断 → 触发/恢复」。安全事件规则是事件队列驱动，
// 不在这里注册（见 engine.go 的 evalSecurityEvents）。
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

func (c *evalContext) Fire(node, instance string, value float64, msg ...string) {
	c.e.fire(c.rule, node, instance, value, c.now, msg...)
}

func (c *evalContext) Resolve(node, instance string, value float64, msg ...string) {
	c.e.resolve(c.rule, node, instance, value, c.now, msg...)
}

func (c *evalContext) State(key string) *ruleState { return c.e.getState(key) }

func (c *evalContext) MaybeEscalate(node, instance string, value float64, st *ruleState) {
	c.e.maybeEscalate(c.rule, node, instance, value, st, c.now)
}

func (c *evalContext) LatestSamples(node, metric string, labels map[string]string) []metricSample {
	return c.e.latestMetricSamples(node, metric, labels)
}

func (c *evalContext) LatestSamplesIn(node, metric string, labels map[string]string, lookback time.Duration) []metricSample {
	return c.e.latestMetricSamplesIn(node, metric, labels, lookback)
}

func (c *evalContext) RoleSamples(node, metric string, labels map[string]string) []roleSample {
	return latestRoleSamples(c.e.store, node, metric, labels)
}

func (c *evalContext) ClusterMembers(service string, nodeNames []string) []ClusterMember {
	return LoadClusterMembers(c.e.store, service, nodeNames)
}

func (c *evalContext) QueryPromQL(expr string) ([]model.Series, error) {
	return c.e.store.QueryPromQL(expr)
}

// StatesWithPrefix 返回本规则相关的状态项（key 以 prefix 开头）。
// PromQL 评估器用它找出「上一轮 firing、本轮未出现」的序列。
func (c *evalContext) StatesWithPrefix(prefix string) map[string]*ruleState {
	out := map[string]*ruleState{}
	for k, st := range c.e.states {
		if strings.HasPrefix(k, prefix) {
			out[k] = st
		}
	}
	return out
}

var periodicEvaluators = map[string]evaluator{
	model.RuleTypeThreshold:    thresholdEvaluator{},
	model.RuleTypeNodeOffline:  nodeOfflineEvaluator{},
	model.RuleTypeServiceDown:  serviceDownEvaluator{},
	model.RuleTypeRoleChange:   roleChangeEvaluator{},
	model.RuleTypeClusterFault: clusterFaultEvaluator{},
	model.RuleTypePromQL:       promQLEvaluator{},
}

// evaluatorFor 对未知/空类型回退到阈值评估，保持现有行为。
func evaluatorFor(t string) evaluator {
	if ev, ok := periodicEvaluators[t]; ok {
		return ev
	}
	return periodicEvaluators[model.RuleTypeThreshold]
}
```

> 说明：`nodeOfflineEvaluator` 等类型在本任务尚未定义，会编译失败——这是预期的红。

- [ ] **Step 2: 创建 `eval_threshold.go`，把 `engine.go:521-557` 的函数体原样搬入**

```go
package alert

// thresholdEvaluator 传统阈值规则评估：逐节点 × 实例，指标与阈值比较，持续 For 后触发。
type thresholdEvaluator struct{}

func (thresholdEvaluator) Evaluate(c *evalContext) {
	r := c.rule
	for _, n := range c.nodes {
		if !matchesGroup(r.Group, n.Group) {
			continue
		}
		if !matchesScope(r.Scope, r.Nodes, n.Hostname) {
			continue
		}
		for _, sample := range c.LatestSamples(n.Hostname, r.Metric, nil) {
			key := r.ID + "|" + n.Hostname + "|" + sample.instance
			st := c.State(key)
			cond := Compare(r.Operator, sample.value, r.Threshold)
			if cond {
				if st.aboveSince == 0 {
					st.aboveSince = c.now
				}
				forSec := parseFor(r.For)
				if !st.firing && c.now-st.aboveSince >= forSec*1000 {
					st.firing = true
					st.firedAt = c.now
					st.escalated = false
					st.lastRepeat = 0
					c.Fire(n.Hostname, sample.instance, sample.value)
				} else {
					c.MaybeEscalate(n.Hostname, sample.instance, sample.value, st)
				}
			} else {
				if st.firing {
					st.firing = false
					c.Resolve(n.Hostname, sample.instance, sample.value)
				}
				st.aboveSince = 0
			}
		}
	}
}
```

- [ ] **Step 3: 修改 `evaluate()` 的分发**

把 `engine.go:366-379` 的 switch 替换为：

```go
		if r.Type == model.RuleTypeSecurityEvent {
			e.evalSecurityEvents(r, nodes, now)
			continue
		}
		evaluatorFor(r.Type).Evaluate(&evalContext{e: e, rule: r, nodes: nodes, now: now})
```

同时删除 `engine.go` 中的旧 `evalThreshold` 函数（逻辑已在 `eval_threshold.go`）。

- [ ] **Step 4: 为其余四种评估器补最小占位，保证编译通过**

在各自文件创建前，先在 `evaluators.go` 末尾临时加入以下占位（Task 3 会替换为真实实现）：

```go
type nodeOfflineEvaluator struct{}
func (nodeOfflineEvaluator) Evaluate(c *evalContext) { c.e.evalNodeOffline(c.rule, c.nodes, c.now) }
type serviceDownEvaluator struct{}
func (serviceDownEvaluator) Evaluate(c *evalContext) { c.e.evalServiceDown(c.rule, c.nodes, c.now) }
type roleChangeEvaluator struct{}
func (roleChangeEvaluator) Evaluate(c *evalContext) { c.e.evalRoleChange(c.rule, c.nodes, c.now) }
type clusterFaultEvaluator struct{}
func (clusterFaultEvaluator) Evaluate(c *evalContext) { c.e.evalClusterFault(c.rule, c.nodes, c.now) }
type promQLEvaluator struct{}
func (promQLEvaluator) Evaluate(c *evalContext) { /* Task 6 实现 */ }
```

- [ ] **Step 5: 运行全量 alert 测试**

Run: `go test ./internal/server/alert/ -count=1`
Expected: PASS（特征测试与既有测试全绿）。

- [ ] **Step 6: 提交**

```bash
git add internal/server/alert/evaluators.go internal/server/alert/eval_threshold.go internal/server/alert/engine.go
git commit -m "refactor(alert): 引入周期性评估器接口，迁移阈值评估"
```

---

## Task 3: 迁移其余四种评估器 + 安全事件文件拆分

**Files:**
- Create: `internal/server/alert/eval_offline.go`
- Create: `internal/server/alert/eval_service_down.go`
- Create: `internal/server/alert/eval_role_change.go`
- Create: `internal/server/alert/eval_cluster.go`
- Create: `internal/server/alert/eval_security.go`
- Modify: `internal/server/alert/evaluators.go`（删除 Task 2 的占位）
- Modify: `internal/server/alert/engine.go`（删除已迁移的函数）

**Interfaces:**
- Consumes: `evalContext` 的 `LatestSamplesIn/RoleSamples/ClusterMembers/Fire/Resolve/State/MaybeEscalate`。
- Produces: `nodeOfflineEvaluator`、`serviceDownEvaluator`、`roleChangeEvaluator`、`clusterFaultEvaluator` 的真实实现。

- [ ] **Step 1: 逐个搬移函数体**

按下表把 `engine.go` 中的函数体**原样**搬入新文件，只改接收者与调用方式：

| 新文件 | 类型 | 原函数 | 调用方式 |
|---|---|---|---|
| `eval_offline.go` | `nodeOfflineEvaluator` | `evalNodeOffline`（engine.go:569-604） | `e.getState` → `c.State`；`e.fire` → `c.Fire`；`e.resolve` → `c.Resolve`；`e.maybeEscalate` → `c.MaybeEscalate`；`now` → `c.now`；`nodes` → `c.nodes` |
| `eval_service_down.go` | `serviceDownEvaluator` | `evalServiceDown`（engine.go:623-673） | `e.latestMetricSamplesIn` → `c.LatestSamplesIn`；其余同上 |
| `eval_role_change.go` | `roleChangeEvaluator` | `evalRoleChange`（engine.go:675-726） | `latestRoleSamples(e.store, ...)` → `c.RoleSamples(...)`；其余同上 |
| `eval_cluster.go` | `clusterFaultEvaluator` | `evalClusterFault`（engine.go:728-796） | `LoadClusterMembers(e.store, ...)` → `c.ClusterMembers(...)`；其余同上 |

示例（`eval_offline.go` 的骨架；把 `engine.go:569-604` 的函数体整体复制进来，只按上表替换 5 处调用）：

```go
package alert

type nodeOfflineEvaluator struct{}

func (nodeOfflineEvaluator) Evaluate(c *evalContext) {
	r := c.rule
	for _, n := range c.nodes {
		if !matchesGroup(r.Group, n.Group) {
			continue
		}
		if !matchesScope(r.Scope, r.Nodes, n.Hostname) {
			continue
		}
		key := r.ID + "|" + n.Hostname + "|"
		st := c.State(key)
		offline := n.Status == "offline"
		if offline {
			if st.aboveSince == 0 {
				st.aboveSince = c.now
			}
			forSec := parseFor(r.For)
			if !st.firing && c.now-st.aboveSince >= forSec*1000 {
				st.firing = true
				st.firedAt = c.now
				st.escalated = false
				st.lastRepeat = 0
				c.Fire(n.Hostname, "", 0, "主机 "+n.Hostname+" 已离线（Status=offline，最近心跳 "+formatTS(n.LastSeen)+"）")
			} else {
				c.MaybeEscalate(n.Hostname, "", 0, st)
			}
		} else {
			if st.firing {
				st.firing = false
				c.Resolve(n.Hostname, "", 0, "主机 "+n.Hostname+" 已恢复正常在线")
			}
			st.aboveSince = 0
		}
	}
}
```

- [ ] **Step 2: 把安全事件函数移到 `eval_security.go`**

把 `evalSecurityEvents`（engine.go:231-270）、`emitRuleSecurityEvent`（engine.go:300 起）与 `shouldAlertSecurityEvent`（engine.go:272-284）整体移动到 `eval_security.go`，**保持为 `*Engine` 方法**，逻辑不变。

- [ ] **Step 3: 删除 `evaluators.go` 中的五个占位实现**

保留 `promQLEvaluator` 占位（Task 6 实现），删除其余四个。

- [ ] **Step 4: 运行全量测试**

Run: `go test ./internal/server/alert/ -count=1`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/server/alert/eval_offline.go internal/server/alert/eval_service_down.go \
  internal/server/alert/eval_role_change.go internal/server/alert/eval_cluster.go \
  internal/server/alert/eval_security.go internal/server/alert/evaluators.go \
  internal/server/alert/engine.go
git commit -m "refactor(alert): 迁移其余评估器，安全事件独立成文件"
```

---

## Task 4: PromQL 规则模型与结构校验

**Files:**
- Modify: `internal/model/metric.go:782-827`（规则类型常量与 `AlertRule`）
- Modify: `internal/server/alert/rules.go:558-621`（`ValidateRule`）
- Create: `internal/server/alert/promql_rule_test.go`

**Interfaces:**
- Produces: `model.RuleTypePromQL`、`AlertRule.Expr`。
- Consumes: 现有 `ValidateRule` 调用方（API、导入）。

- [ ] **Step 1: 写失败测试**

```go
package alert

import (
	"testing"

	"github.com/mlmw92/yingzhao/internal/model"
)

func TestValidateRulePromQLRequiresExpr(t *testing.T) {
	base := model.AlertRule{Name: "qps", Type: model.RuleTypePromQL,
		Severity: model.SeverityWarning, Enabled: true}
	if err := ValidateRule(base); err == nil {
		t.Fatal("缺少 Expr 应拒绝")
	}
	base.Expr = "sum(kafka_consumer_lag) > 10000"
	if err := ValidateRule(base); err != nil {
		t.Fatalf("合法表达式不应被拒: %v", err)
	}
}

func TestValidateRuleThresholdUnaffected(t *testing.T) {
	r := model.AlertRule{Name: "cpu", Metric: "cpu_usage", Operator: ">",
		Threshold: 85, Severity: model.SeverityWarning, Enabled: true}
	if err := ValidateRule(r); err != nil {
		t.Fatalf("阈值规则校验不应受影响: %v", err)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/server/alert/ -run TestValidateRulePromQL -v`
Expected: FAIL（`model.RuleTypePromQL` 未定义）。

- [ ] **Step 3: 实现模型与校验**

`internal/model/metric.go`：

```go
	// RuleTypePromQL PromQL 表达式规则。
	RuleTypePromQL string = "promql"
```

`AlertRule` 增加字段（放在 `Category` 之后）：

```go
	Expr string `json:"expr,omitempty"` // PromQL 表达式（type=promql 时必填）
```

`ValidateRule` 的 switch 增加分支：

```go
	case model.RuleTypePromQL:
		if strings.TrimSpace(r.Expr) == "" {
			return fmt.Errorf("PromQL 规则必须填写表达式")
		}
```

- [ ] **Step 4: 运行测试**

Run: `go test ./internal/server/alert/ -run TestValidateRule -v`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/model/metric.go internal/server/alert/rules.go internal/server/alert/promql_rule_test.go
git commit -m "feat(alert): 增加 promql 规则类型与结构校验"
```

---

## Task 5: 存储层暴露 QueryPromQL

**Files:**
- Modify: `internal/server/storage/vm.go:109-127`（接口）
- Modify: `internal/server/storage/querier.go`（实现）
- Modify: `internal/server/selfmon/wrappers.go`（装饰器）
- Modify: 12 个测试替身（见 Step 3）
- Create: `internal/server/storage/promql_test.go`

**Interfaces:**
- Produces: `storage.Storage.QueryPromQL(expr string) ([]model.Series, error)`。

- [ ] **Step 1: 写失败测试（用 httptest 假后端）**

```go
package storage

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestQueryPromQLForwardsExpression(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query().Get("query")
		json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data": map[string]any{
				"resultType": "vector",
				"result": []any{map[string]any{
					"metric": map[string]string{"node": "n1"},
					"value":  []any{1700000000, "91.2"},
				}},
			},
		})
	}))
	defer srv.Close()

	s := New(srv.URL, 5, 5)
	series, err := s.QueryPromQL(`cpu_usage > 85`)
	if err != nil {
		t.Fatalf("QueryPromQL: %v", err)
	}
	if got != `cpu_usage > 85` {
		t.Fatalf("转发的表达式 = %q", got)
	}
	if len(series) != 1 || series[0].Labels["node"] != "n1" || series[0].Points[0].Value != 91.2 {
		t.Fatalf("解析结果不符: %+v", series)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/server/storage/ -run TestQueryPromQL -v`
Expected: FAIL（`QueryPromQL` 未定义）。

- [ ] **Step 3: 实现接口与所有实现**

`vm.go` 的 `Storage` 接口增加：

```go
	// QueryPromQL 执行一条原始 PromQL 表达式，返回即时查询的向量结果。
	// 仅供规则评估与试运行使用；调用方负责表达式来源的可信性。
	QueryPromQL(expr string) ([]model.Series, error)
```

`querier.go` 增加：

```go
func (s *PromStorage) QueryPromQL(expr string) ([]model.Series, error) {
	return s.queryExpr(expr)
}
```

`selfmon/wrappers.go` 增加：

```go
// QueryPromQL 原始表达式查询并计数。
func (s *Storage) QueryPromQL(expr string) ([]model.Series, error) {
	out, err := s.inner.QueryPromQL(expr)
	if s.mon != nil {
		s.mon.AddQuery(err)
	}
	return out, err
}
```

为以下测试替身各加一行空实现（`return nil, nil`）：

```
internal/server/alert/cluster_store_test.go
internal/server/alert/store_test.go
internal/server/api/container_api_test.go
internal/server/api/health_api_test.go
internal/server/api/metric_scope_test.go
internal/server/api/middleware_cluster_test.go
internal/server/api/middleware_registry_test.go
internal/server/api/scope_mw_test.go
internal/server/api/status_page_test.go
internal/server/receiver/assets_test.go
internal/server/receiver/instance_up_test.go
internal/server/selfmon/selfmon_test.go
```

形如：

```go
func (s *X) QueryPromQL(string) ([]model.Series, error) { return nil, nil }
```

- [ ] **Step 4: 运行测试**

Run: `go test ./internal/server/storage/ ./internal/server/selfmon/ -count=1`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/server/storage internal/server/selfmon internal/server/alert internal/server/api internal/server/receiver
git commit -m "feat(storage): 暴露 QueryPromQL 原始表达式查询"
```

---

## Task 6: PromQL 评估器

**Files:**
- Create: `internal/server/alert/eval_promql.go`
- Create: `internal/server/alert/eval_promql_test.go`
- Modify: `internal/server/alert/evaluators.go`（删除 `promQLEvaluator` 占位）

**Interfaces:**
- Consumes: `evalContext.QueryPromQL/Fire/Resolve/State/MaybeEscalate`、`matchesGroup/matchesScope`、`parseFor`。
- Produces: `promQLEvaluator`、`seriesIdentity(map[string]string) string`、`maxPromQLSeries`。

- [ ] **Step 1: 写失败测试**

```go
package alert

import (
	"errors"
	"fmt"
	"testing"

	"github.com/mlmw92/yingzhao/internal/model"
)

func promqlRule() model.AlertRule {
	return model.AlertRule{ID: "r1", Name: "prom", Type: model.RuleTypePromQL,
		Expr: `cpu_usage > 85`, For: "0s", Severity: model.SeverityWarning, Enabled: true}
}

func TestPromQLFiresOnSeriesPresence(t *testing.T) {
	h := newEvalHarness(t, false, nil)
	h.store.promql = []model.Series{{Labels: map[string]string{"node": "node-1", "instance": "i1"},
		Points: []model.Point{{Timestamp: model.NowMillis(), Value: 91}}}}
	h.addRule(promqlRule())
	h.eval()
	if h.firingCount() != 1 {
		t.Fatalf("序列出现应触发，实际 %d", h.firingCount())
	}
}

func TestPromQLResolvesWhenSeriesDisappears(t *testing.T) {
	h := newEvalHarness(t, false, nil)
	h.store.promql = []model.Series{{Labels: map[string]string{"node": "node-1", "instance": "i1"},
		Points: []model.Point{{Timestamp: model.NowMillis(), Value: 91}}}}
	h.addRule(promqlRule())
	h.eval()
	h.store.promql = nil
	h.eval()
	if h.firingCount() != 0 {
		t.Fatalf("序列消失应恢复，实际 %d", h.firingCount())
	}
}

func TestPromQLQueryErrorKeepsPreviousState(t *testing.T) {
	h := newEvalHarness(t, false, nil)
	h.store.promql = []model.Series{{Labels: map[string]string{"node": "node-1", "instance": "i1"},
		Points: []model.Point{{Timestamp: model.NowMillis(), Value: 91}}}}
	h.addRule(promqlRule())
	h.eval()
	h.store.promErr = errors.New("backend down")
	h.eval()
	if h.firingCount() != 1 {
		t.Fatalf("查询失败不应恢复告警，实际 %d", h.firingCount())
	}
}

func TestPromQLGlobalSeriesIgnoresNodeScope(t *testing.T) {
	h := newEvalHarness(t, false, nil)
	h.store.promql = []model.Series{{Labels: map[string]string{"job": "cluster"},
		Points: []model.Point{{Timestamp: model.NowMillis(), Value: 5}}}}
	r := promqlRule()
	r.Scope = "specified"
	r.Nodes = []string{"other-node"}
	h.addRule(r)
	h.eval()
	if h.firingCount() != 1 {
		t.Fatalf("全局序列不受节点范围限制，实际 %d", h.firingCount())
	}
}

func TestPromQLNodeScopedSeriesRespectsScope(t *testing.T) {
	h := newEvalHarness(t, false, nil)
	h.store.promql = []model.Series{{Labels: map[string]string{"node": "node-1", "instance": "i1"},
		Points: []model.Point{{Timestamp: model.NowMillis(), Value: 91}}}}
	r := promqlRule()
	r.Scope = "specified"
	r.Nodes = []string{"other-node"}
	h.addRule(r)
	h.eval()
	if h.firingCount() != 0 {
		t.Fatalf("范围外节点应被过滤，实际 %d", h.firingCount())
	}
}

func TestPromQLSeriesIdentityIsStable(t *testing.T) {
	a := seriesIdentity(map[string]string{"node": "n1", "instance": "i1", "__name__": "cpu_usage"})
	b := seriesIdentity(map[string]string{"__name__": "cpu_usage", "instance": "i1", "node": "n1"})
	if a != b {
		t.Fatalf("标签顺序不应影响身份: %q != %q", a, b)
	}
}

func TestPromQLTruncatesAtLimit(t *testing.T) {
	h := newEvalHarness(t, false, nil)
	for i := 0; i < maxPromQLSeries+5; i++ {
		h.store.promql = append(h.store.promql, model.Series{
			Labels: map[string]string{"node": "node-1", "instance": "i" + itoa(i)},
			Points: []model.Point{{Timestamp: model.NowMillis(), Value: 1}},
		})
	}
	h.addRule(promqlRule())
	h.eval()
	if h.firingCount() != maxPromQLSeries {
		t.Fatalf("应截断到 %d，实际 %d", maxPromQLSeries, h.firingCount())
	}
}

func itoa(i int) string { return fmt.Sprintf("%d", i) }
```

> 测试里 `For: "0s"` 保证第一轮即触发；`time` 导入用于时间戳。

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/server/alert/ -run TestPromQL -v`
Expected: FAIL（`seriesIdentity`/`maxPromQLSeries` 未定义，评估器是空实现）。

- [ ] **Step 3: 实现 `eval_promql.go`**

```go
package alert

import (
	"log/slog"
	"sort"
	"strings"

	"github.com/mlmw92/yingzhao/internal/model"
)

// maxPromQLSeries 是单规则单轮处理的结果上限。
// 上限固定而不做成配置：一条写错的表达式不应该拖垮整轮评估。
const maxPromQLSeries = 200

type promQLEvaluator struct{}

func (promQLEvaluator) Evaluate(c *evalContext) {
	r := c.rule
	series, err := c.QueryPromQL(r.Expr)
	if err != nil {
		// 查询失败保持上一轮状态：既不改 firing，也不 resolve。
		slog.Warn("PromQL 规则评估失败，保持上一轮状态", "rule", r.Name, "err", err)
		return
	}
	if len(series) > maxPromQLSeries {
		slog.Warn("PromQL 结果超过上限，已截断", "rule", r.Name,
			"count", len(series), "limit", maxPromQLSeries)
		sort.Slice(series, func(i, j int) bool {
			return seriesIdentity(series[i].Labels) < seriesIdentity(series[j].Labels)
		})
		series = series[:maxPromQLSeries]
	}

	seen := make(map[string]bool, len(series))
	for _, s := range series {
		node := s.Labels["node"]
		if node != "" {
			n, ok := nodeByName(c.nodes, node)
			if !ok || !matchesGroup(r.Group, n.Group) || !matchesScope(r.Scope, r.Nodes, node) {
				continue
			}
		}
		id := seriesIdentity(s.Labels)
		key := r.ID + "|" + node + "|" + id
		seen[key] = true
		st := c.State(key)
		if st.aboveSince == 0 {
			st.aboveSince = c.now
		}
		value := lastValue(s)
		if !st.firing && c.now-st.aboveSince >= parseFor(r.For)*1000 {
			st.firing = true
			st.firedAt = c.now
			st.escalated = false
			st.lastRepeat = 0
			c.Fire(node, id, value, promQLMessage(r, id, value))
		} else {
			c.MaybeEscalate(node, id, value, st)
		}
	}

	// 本轮未出现的序列 = 已恢复。只扫描本规则的 key，避免误伤其它规则。
	prefix := r.ID + "|"
	for key, st := range c.StatesWithPrefix(prefix) {
		if seen[key] || !st.firing {
			continue
		}
		node, id := splitRuleKey(key)
		st.firing = false
		st.aboveSince = 0
		c.Resolve(node, id, 0, "PromQL 规则「"+r.Name+"」已恢复："+id)
	}
}

func lastValue(s model.Series) float64 {
	if len(s.Points) == 0 {
		return 0
	}
	return s.Points[len(s.Points)-1].Value
}

func promQLMessage(r model.AlertRule, id string, value float64) string {
	return "PromQL 规则「" + r.Name + "」命中：" + id + " = " + formatFloat(value)
}

// seriesIdentity 用排序后的 k=v 串标识一条序列，保证标签顺序不影响告警身份。
func seriesIdentity(labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(labels[k])
	}
	return b.String()
}

func nodeByName(nodes []model.Node, name string) (model.Node, bool) {
	for _, n := range nodes {
		if n.Hostname == name {
			return n, true
		}
	}
	return model.Node{}, false
}

func splitRuleKey(key string) (node, instance string) {
	parts := strings.SplitN(key, "|", 3)
	if len(parts) != 3 {
		return "", ""
	}
	return parts[1], parts[2]
}
```

- [ ] **Step 4: 运行测试**

Run: `go test ./internal/server/alert/ -run TestPromQL -v`
Expected: PASS（7 个用例）。

- [ ] **Step 5: 全量回归**

Run: `go test ./internal/server/alert/ -count=1`
Expected: PASS。

- [ ] **Step 6: 提交**

```bash
git add internal/server/alert/eval_promql.go internal/server/alert/eval_promql_test.go internal/server/alert/evaluators.go
git commit -m "feat(alert): 增加 PromQL 规则评估器"
```

---

## Task 7: API 保存校验与试运行

**Files:**
- Modify: `internal/server/api/query.go:330-331`（路由）
- Modify: `internal/server/api/query.go:1327-1360`（创建/更新）
- Create: `internal/server/api/promql_rule_api.go`
- Create: `internal/server/api/promql_rule_api_test.go`

**Interfaces:**
- Consumes: `storage.Storage.QueryPromQL`、`alert.ValidateRule`、`a.store`、`a.rules`。
- Produces: `POST /api/v1/rules/promql/preview`、`validatePromQL(expr string) error`。

- [ ] **Step 1: 写失败测试**

```go
package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/mlmw92/yingzhao/internal/model"
	"github.com/mlmw92/yingzhao/internal/server/alert"
)

type promQLStore struct{ seriesStore }

func (s *promQLStore) QueryPromQL(expr string) ([]model.Series, error) {
	if expr == "bad(" {
		return nil, errors.New(`parse error: unexpected "("`)
	}
	return []model.Series{{Labels: map[string]string{"node": "web-01"},
		Points: []model.Point{{Timestamp: 1, Value: 2}}}}, nil
}

// promQLTestAPI 在 scopeTestAPI 基础上补规则存储与可注入的 PromQL 存储。
func promQLTestAPI(t *testing.T) *API {
	t.Helper()
	a := scopeTestAPI(t)
	a.store = &promQLStore{}
	a.rules = alert.NewRulesStore(filepath.Join(t.TempDir(), "rules.yaml"))
	return a
}

func TestPromQLPreview(t *testing.T) {
	a := promQLTestAPI(t)
	rec := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, reqWith(globalPrincipal("alerts:write"), http.MethodPost,
		"/api/v1/rules/promql/preview", `{"expr":"up == 0"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d（body=%s）", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	if body["count"].(float64) != 1 {
		t.Fatalf("count = %v，want 1", body["count"])
	}
}

func TestPromQLPreviewRejectsBadExpr(t *testing.T) {
	a := promQLTestAPI(t)
	rec := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, reqWith(globalPrincipal("alerts:write"), http.MethodPost,
		"/api/v1/rules/promql/preview", `{"expr":"bad("}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d，want 400（body=%s）", rec.Code, rec.Body.String())
	}
}

func TestCreatePromQLRuleValidatesExpression(t *testing.T) {
	a := promQLTestAPI(t)
	rec := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, reqWith(globalPrincipal("alerts:write"), http.MethodPost,
		"/api/v1/rules", `{"name":"p","type":"promql","expr":"bad(","severity":"warning","enabled":true}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d，want 400（body=%s）", rec.Code, rec.Body.String())
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/server/api/ -run TestPromQL -v`
Expected: FAIL（路由与处理器不存在）。

- [ ] **Step 3: 实现处理器与路由**

`promql_rule_api.go`：

```go
package api

import (
	"encoding/json"
	"net/http"
	"strings"
)

const promQLPreviewLimit = 20

type promQLPreviewItem struct {
	Labels map[string]string `json:"labels"`
	Value  float64           `json:"value"`
}

type promQLPreviewResp struct {
	Count     int                 `json:"count"`
	Truncated bool                `json:"truncated"`
	Series    []promQLPreviewItem `json:"series"`
}

// validatePromQL 用一次真实查询校验表达式可执行性。
// 不引入 PromQL 解析器依赖：由后端方言（MetricsQL 超集）自己回答。
func (a *API) validatePromQL(expr string) error {
	if strings.TrimSpace(expr) == "" {
		return errEmptyExpr
	}
	_, err := a.store.QueryPromQL(expr)
	return err
}

var errEmptyExpr = &exprError{"表达式不能为空"}

type exprError struct{ msg string }

func (e *exprError) Error() string { return e.msg }

func (a *API) handlePromQLPreview(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Expr string `json:"expr"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.Expr) == "" {
		http.Error(w, "表达式不能为空", http.StatusBadRequest)
		return
	}
	series, err := a.store.QueryPromQL(body.Expr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	limit := len(series)
	if limit > promQLPreviewLimit {
		limit = promQLPreviewLimit
	}
	resp := promQLPreviewResp{
		Count:     len(series),
		Truncated: len(series) > promQLPreviewLimit,
		Series:    make([]promQLPreviewItem, 0, limit),
	}
	for _, s := range series[:limit] {
		v := 0.0
		if len(s.Points) > 0 {
			v = s.Points[len(s.Points)-1].Value
		}
		resp.Series = append(resp.Series, promQLPreviewItem{Labels: s.Labels, Value: v})
	}
	writeJSON(w, 200, resp)
}
```

路由（`query.go` 的规则路由旁）：

```go
	mux.HandleFunc("POST /api/v1/rules/promql/preview", a.permit(a.handlePromQLPreview, "alerts:write"))
```

创建/更新规则时追加校验（`handleRuleCreate` 与 `handleRuleUpdate` 的 `ValidateRule` 之后）：

```go
	if rule.Type == model.RuleTypePromQL {
		if err := a.validatePromQL(rule.Expr); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
```

- [ ] **Step 4: 运行测试**

Run: `go test ./internal/server/api/ -run TestPromQL -v`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/server/api/promql_rule_api.go internal/server/api/promql_rule_api_test.go internal/server/api/query.go
git commit -m "feat(api): PromQL 规则保存校验与试运行接口"
```

---

## Task 8: 前端规则表单

**Files:**
- Modify: `web/src/components/RuleModal.vue`
- Create: `web/src/components/RuleModal.promql.test.js`

**Interfaces:**
- Consumes: `POST /api/v1/rules/promql/preview`。
- Produces: `form.expr`、`runPreview()`。

- [ ] **Step 1: 写失败测试**

```js
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import http from '../api/http'
import RuleModal from './RuleModal.vue'

vi.mock('../api/http', () => ({
  default: { get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() },
}))

beforeEach(() => {
  vi.clearAllMocks()
  http.get.mockResolvedValue({ nodes: [], alertGroups: [] })
  http.post.mockResolvedValue({
    count: 2,
    truncated: false,
    series: [{ labels: { node: 'web-01' }, value: 91 }],
  })
})

describe('RuleModal PromQL', () => {
  it('选择 PromQL 后显示表达式输入与试运行', async () => {
    const w = mount(RuleModal, {
      props: { modelValue: true, rule: null, channels: [], templates: [] },
      global: { plugins: [ElementPlus] },
    })
    await flushPromises()
    // 选择类型（组件内是 el-select，直接改 form 需要通过事件；这里用选项文本定位）
    const vm = w.vm
    vm.$.setupState.form.type = 'promql'
    await vm.$nextTick()
    expect(w.text()).toContain('表达式')
    expect(w.text()).toContain('试运行')
  })

  it('试运行展示命中条数', async () => {
    const w = mount(RuleModal, {
      props: { modelValue: true, rule: null, channels: [], templates: [] },
      global: { plugins: [ElementPlus] },
    })
    await flushPromises()
    const vm = w.vm
    vm.$.setupState.form.type = 'promql'
    vm.$.setupState.form.expr = 'up == 0'
    await vm.$nextTick()
    const btn = w.findAll('button').find((b) => b.text().includes('试运行'))
    await btn.trigger('click')
    await flushPromises()
    expect(http.post).toHaveBeenCalledWith('/api/v1/rules/promql/preview', { expr: 'up == 0' })
    expect(w.text()).toContain('命中 2 条')
  })
})
```

> 若 `w.vm.$.setupState` 在当前 Vue 版本不可用，改为通过 `w.findComponent({ name: 'ElSelect' })` 触发 `update:modelValue`；实现时以实际能驱动表单为准，但断言不变。

- [ ] **Step 2: 运行确认失败**

Run: `cd web && npx vitest run src/components/RuleModal.promql.test.js`
Expected: FAIL（无表达式输入与试运行按钮）。

- [ ] **Step 3: 实现表单**

1. 类型下拉（约第 30 行）增加：

```html
<el-option value="promql" label="PromQL 规则" />
```

2. 在阈值区块之后增加 PromQL 区块：

```html
<template v-else-if="form.type === 'promql'">
  <el-form-item label="表达式" prop="expr">
    <el-input v-model="form.expr" type="textarea" :rows="4" class="promql-input"
      placeholder="cpu_usage > 85" />
  </el-form-item>
  <div class="promql-preview-row">
    <el-button size="small" :loading="previewing" @click="runPreview">试运行</el-button>
    <span v-if="previewResult" class="preview-text">
      命中 {{ previewResult.count }} 条<span v-if="previewResult.truncated">（仅显示前 20 条）</span>
    </span>
  </div>
  <el-alert v-if="previewError" type="error" :closable="false" :title="previewError" />
  <ul v-if="previewResult && previewResult.series.length" class="preview-series">
    <li v-for="(s, i) in previewResult.series" :key="i" class="mono">
      {{ formatLabels(s.labels) }} = {{ s.value }}
    </li>
  </ul>
</template>
```

3. 脚本部分：

```js
// form 增加
expr: '',

// 载入规则
form.expr = r?.expr ?? ''

// formSig 增加
expr: form.expr,

// 校验规则
expr: form.type === 'promql' ? [{ required: true, message: '请填写 PromQL 表达式', trigger: 'blur' }] : [],

// TYPE_HINTS 增加
promql: '用 PromQL 表达式定义告警：结果里的每条序列都会单独触发，序列消失即恢复。',

// 试运行
const previewing = ref(false)
const previewResult = ref(null)
const previewError = ref('')

async function runPreview() {
  previewing.value = true
  previewError.value = ''
  previewResult.value = null
  try {
    previewResult.value = await http.post('/api/v1/rules/promql/preview', { expr: form.expr })
  } catch (e) {
    previewError.value = e?.message || '试运行失败'
  } finally {
    previewing.value = false
  }
}

function formatLabels(labels) {
  return Object.entries(labels || {}).map(([k, v]) => `${k}=${v}`).join(', ')
}
```

4. 保存 payload 增加 `expr: form.expr`（找到提交 `http.post/put` 的对象处）。

5. 切换类型时清理：`clearOtherTypeFields` 中，`t !== 'promql'` 时 `form.expr = ''`。

6. 样式：

```css
.promql-input :deep(textarea) { font-family: var(--mono); }
.promql-preview-row { display: flex; align-items: center; gap: 12px; margin-bottom: 8px; }
.preview-text { color: var(--t2); font-size: var(--fs-sm); }
.preview-series { margin: 8px 0 0; padding-left: 18px; color: var(--t2); font-size: var(--fs-sm); }
```

- [ ] **Step 4: 运行测试**

Run: `cd web && npx vitest run src/components/RuleModal.promql.test.js`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add web/src/components/RuleModal.vue web/src/components/RuleModal.promql.test.js
git commit -m "feat(web): 规则表单支持 PromQL 表达式与试运行"
```

---

## Task 9: 告警页面展示 PromQL 规则

**Files:**
- Modify: `web/src/components/AlertsView.vue:522`、`:1160-1180`、`:1250-1280`
- Create: `web/src/components/AlertsView.promql.test.js`

**Interfaces:**
- Consumes: `rule.type === 'promql'`、`rule.expr`、`event.metric`（存表达式）。

- [ ] **Step 1: 写失败测试**

```js
import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import AlertsView from './AlertsView.vue'

vi.mock('../api/http', () => ({
  default: { get: vi.fn().mockResolvedValue({ alerts: [], rules: [], nodes: [] }), post: vi.fn() },
}))

describe('AlertsView PromQL 展示', () => {
  it('规则条件显示表达式而不是指标/阈值', async () => {
    const w = mount(AlertsView, { global: { plugins: [ElementPlus] } })
    const text = w.vm.$.setupState.conditionText({
      type: 'promql', expr: 'cpu_usage > 85', for: '5m',
    })
    expect(text).toContain('PromQL')
    expect(text).toContain('cpu_usage > 85')
  })
})
```

- [ ] **Step 2: 运行确认失败**

Run: `cd web && npx vitest run src/components/AlertsView.promql.test.js`
Expected: FAIL（`conditionText` 对 promql 走到 default，返回空/错误文案）。

- [ ] **Step 3: 实现展示分支**

`conditionText` 增加：

```js
    case 'promql':
      return 'PromQL：' + (row.expr || '')
```

事件详情（约第 522 行）改为：

```html
<div class="ev-field">
  <span>触发条件</span>
  <span v-if="detail.ruleType === 'promql'" class="mono">{{ detail.metric }}</span>
  <span v-else class="mono">{{ detail.metric }} {{ detail.operator }} {{ detail.threshold }}</span>
</div>
```

**补 `AlertEvent.RuleType`**：在 `internal/model/metric.go` 的 `AlertEvent` 中增加

```go
	RuleType string `json:"ruleType,omitempty"` // 规则类型，供前端区分 PromQL 与阈值规则
```

并在 `engine.go` 的 `fire` 与 `resolve` 构造事件时写入 `RuleType: r.Type`（两处）。

`templateCondition` 增加：

```js
  if (t.type === 'promql') return `PromQL：${t.expr}，持续 ${t.for || '立即'}`
```

- [ ] **Step 4: 运行测试**

Run: `cd web && npx vitest run src/components/AlertsView.promql.test.js`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add web/src/components/AlertsView.vue web/src/components/AlertsView.promql.test.js internal/model/metric.go internal/server/alert/engine.go
git commit -m "feat(web): 告警页面展示 PromQL 规则与表达式"
```

---

## Task 10: 文档与版本

**Files:**
- Modify: `docs/guides/configuration-reference.md`（规则类型章节）
- Modify: `CONTEXT.md`（术语）
- Modify: `VERSION`

- [ ] **Step 1: 文档**

在告警规则说明处增加：

```markdown
### PromQL 规则（type=promql）

用一条 PromQL 表达式定义告警：表达式返回的**每条序列**都会单独触发，序列消失即恢复。

- 条件写在表达式里（如 `cpu_usage > 85`），不需要再填运算符和阈值。
- 结果带 `node` 标签时按节点归属，并受规则的分组/指定主机范围限制；不带 `node` 时按完整标签集合作为全局序列。
- `for` 表示序列连续存在多久才触发；`severity`、通知、静默、升级等字段与阈值规则一致。
- 单条规则单轮最多处理 200 条序列，超出会截断并记录日志。
- 查询失败时保持上一轮状态，不会误恢复。
- 保存规则时会执行一次表达式校验；表单里的「试运行」可预览命中结果。
```

`CONTEXT.md` 术语表增加：

```markdown
| **PromQL 规则** | 以 PromQL 表达式定义触发条件的告警规则（`type=promql`）；表达式结果的每条序列是一条告警实例 |
```

- [ ] **Step 2: 版本号**

`VERSION` 递增一个次版本（新功能）：例如 `1.31.0` → `1.32.0`。

- [ ] **Step 3: 全量验证**

Run: `go test ./... && cd web && npx vitest run`
Expected: 全部通过。

- [ ] **Step 4: 提交**

```bash
git add docs/guides/configuration-reference.md CONTEXT.md VERSION
git commit -m "docs: PromQL 规则说明与版本递增"
```

---

## 验证清单

- [ ] `go test ./...` 全绿（含 Task 1 的特征测试）。
- [ ] `cd web && npx vitest run` 全绿。
- [ ] 实机：新建 PromQL 规则 `cpu_usage > 1` → 试运行有命中 → 保存后一个评估周期内出现告警 → 改为 `cpu_usage > 99999` → 下一周期恢复。
- [ ] 实机：保存 `bad(` → 接口返回 400，前端就地显示错误。
- [ ] 真实浏览器走查规则表单与告警详情（组件测试不能替代）。
