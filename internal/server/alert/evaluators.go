package alert

import (
	"strings"
	"time"

	"github.com/nebula/monitor/internal/model"
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
