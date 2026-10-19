package alert

import (
	"log/slog"
	"sort"
	"strings"

	"github.com/nebula/monitor/internal/model"
)

// maxPromQLSeries 是单规则单轮处理的结果上限。
// 上限固定而不做成配置：一条写错的表达式不应该拖垮整轮评估。
const maxPromQLSeries = 200

// promQLEvaluator 按 PromQL 表达式评估告警：
// 表达式返回的每条序列就是一条告警实例，序列存在即触发、消失即恢复。
// 条件写在表达式里（如 cpu_usage > 85），因此不再单独比较阈值。
type promQLEvaluator struct{}

func (promQLEvaluator) Evaluate(c *evalContext) {
	r := c.rule
	series, err := c.QueryPromQL(r.Expr)
	if err != nil {
		// 查询失败保持上一轮状态：既不改 firing，也不 resolve。
		// 否则时序库抖动一次就会误恢复、下一轮又误触发。
		slog.Warn("PromQL 规则评估失败，保持上一轮状态", "rule", r.Name, "err", err)
		return
	}
	if len(series) > maxPromQLSeries {
		// 截断前先排序：保证每轮截掉的都是同一批序列，避免状态随机抖动。
		sort.Slice(series, func(i, j int) bool {
			return seriesIdentity(series[i].Labels) < seriesIdentity(series[j].Labels)
		})
		slog.Warn("PromQL 结果超过上限，已截断", "rule", r.Name,
			"count", len(series), "limit", maxPromQLSeries)
		series = series[:maxPromQLSeries]
	}

	seen := make(map[string]bool, len(series))
	for _, s := range series {
		node := s.Labels["node"]
		if node != "" {
			// 带 node 标签的序列按节点归属，受规则的分组/范围约束；
			// 不带 node 的是集群级序列，不受节点范围限制。
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

	// 本轮未出现的序列 = 已恢复。只扫描本规则的状态键，避免误伤其它规则。
	for key, st := range c.StatesWithPrefix(r.ID + "|") {
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

// seriesIdentity 用排序后的 k=v 串标识一条序列。
// 必须排序：同一序列在不同轮次里标签顺序可能不同，不排序会生成两个身份，
// 表现为一条告警反复触发/恢复。
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

// splitRuleKey 拆解 rule|node|instance 形式的状态键。
func splitRuleKey(key string) (node, instance string) {
	parts := strings.SplitN(key, "|", 3)
	if len(parts) != 3 {
		return "", ""
	}
	return parts[1], parts[2]
}
