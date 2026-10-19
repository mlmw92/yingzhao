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
