package alert

// serviceDownEvaluator 中间件/服务离线检测：基于 *_instance_up 指标（值为 0 即离线）。
// 默认阈值 <= 0.5 触发（即 up=0），阈值/运算符可由规则自定义。状态型。
type serviceDownEvaluator struct{}

func (serviceDownEvaluator) Evaluate(c *evalContext) {
	r := c.rule
	metric := serviceMetric(r.Service)
	upLabels := upLabelsFor(r.Service)
	op := r.Operator
	if op == "" {
		op = "<="
	}
	thr := r.Threshold
	if op == "<=" && thr == 0 {
		thr = 0.5
	}
	for _, n := range c.nodes {
		if !matchesGroup(r.Group, n.Group) {
			continue
		}
		if !matchesScope(r.Scope, r.Nodes, n.Hostname) {
			continue
		}
		for _, sample := range latestPerInstance(c.LatestSamplesIn(n.Hostname, metric, upLabels, freshSampleWindow)) {
			key := r.ID + "|" + n.Hostname + "|" + sample.instance
			st := c.State(key)
			cond := Compare(op, sample.value, thr)
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
					msg := "节点 " + n.Hostname + " 的 " + r.Service + " 实例 " + instanceLabel(sample.instance) +
						" 服务离线（" + metric + "=" + formatFloat(sample.value) + "）"
					c.Fire(n.Hostname, sample.instance, sample.value, msg)
				} else {
					c.MaybeEscalate(n.Hostname, sample.instance, sample.value, st)
				}
			} else {
				if st.firing {
					st.firing = false
					msg := "节点 " + n.Hostname + " 的 " + r.Service + " 实例 " + instanceLabel(sample.instance) + " 已恢复正常"
					c.Resolve(n.Hostname, sample.instance, sample.value, msg)
				}
				st.aboveSince = 0
			}
		}
	}
}
