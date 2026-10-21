package alert

import (
	"strconv"

	"github.com/mlmw92/yingzhao/internal/model"
)

// Compare 根据运算符比较 left 与 right。
func Compare(op string, left, right float64) bool {
	switch op {
	case ">":
		return left > right
	case ">=":
		return left >= right
	case "<":
		return left < right
	case "<=":
		return left <= right
	case "==":
		return left == right
	case "!=":
		return left != right
	default:
		return false
	}
}

// matchesGroup 判断节点是否属于规则作用的分组（空分组表示全部）。
func matchesGroup(ruleGroup, nodeGroup string) bool {
	return ruleGroup == "" || ruleGroup == nodeGroup
}

// matchesScope 判断节点是否在规则的应用范围内。
// scope 为 "specified" 时只允许 nodes 列表中的节点；其他值（含空、"all"）表示全部主机。
func matchesScope(scope string, nodes []string, node string) bool {
	if scope == "specified" {
		for _, n := range nodes {
			if n == node {
				return true
			}
		}
		return false
	}
	return true
}

// conditionMetric 是事件的「触发条件」展示文本：PromQL 规则用表达式，
// 其它类型用指标名。前端详情页直接展示事件的 Metric 字段。
func conditionMetric(r model.AlertRule) string {
	if r.Type == model.RuleTypePromQL {
		return r.Expr
	}
	return r.Metric
}

// triggerMessage 构造告警描述。
func triggerMessage(r model.AlertRule, node string, value float64) string {
	return "节点 " + node + " 指标 " + conditionMetric(r) + " = " + formatFloat(value) +
		" " + r.Operator + " 阈值 " + formatFloat(r.Threshold) + "（规则：" + r.Name + "）"
}

func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', 2, 64)
}
