package alert

// nodeOfflineEvaluator 主机离线检测：直接读取 nodeMgr 内存状态（离线无需指标上报）。
// 状态型：离线持续 For 后触发，恢复（online）即 resolve。
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
