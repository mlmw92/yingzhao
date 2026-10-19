package alert

// clusterFaultEvaluator 集群状态损坏检测：按分组聚合各实例角色与（MySQL）组视图，
// 判定无主 / 多主 / 组视图分裂，并向每个相关成员触发。状态型。
//
// 判定逻辑集中在 ClassifyClusterFault（与页面展示共用同一实现），避免「页面显示正常、
// 告警判定异常」这类同实例两种结论的分叉。
type clusterFaultEvaluator struct{}

func (clusterFaultEvaluator) Evaluate(c *evalContext) {
	r := c.rule
	// 先按规则的分组/范围过滤出可见节点，再统一加载集群成员。
	var nodeNames []string
	for _, n := range c.nodes {
		if !matchesGroup(r.Group, n.Group) {
			continue
		}
		if !matchesScope(r.Scope, r.Nodes, n.Hostname) {
			continue
		}
		nodeNames = append(nodeNames, n.Hostname)
	}

	byGroup := map[string][]ClusterMember{}
	var order []string
	for _, m := range c.ClusterMembers(r.Service, nodeNames) {
		if r.Topology != "" && r.Topology != m.Topology {
			continue
		}
		grp := m.Group
		if grp == "" {
			grp = m.Instance
		}
		if _, ok := byGroup[grp]; !ok {
			order = append(order, grp)
		}
		byGroup[grp] = append(byGroup[grp], m)
	}

	// 对每个集群分组判定是否存在故障，并触发/恢复每个成员节点上的告警。
	for _, grp := range order {
		members := byGroup[grp]
		fault := ClassifyClusterFault(members)
		for _, m := range members {
			// 状态键与 firingKey 保持一致，避免重启后无法恢复集群告警状态。
			key := r.ID + "|" + m.Node + "|" + m.Instance
			st := c.State(key)
			if fault != "" {
				if st.aboveSince == 0 {
					st.aboveSince = c.now
				}
				forSec := parseFor(r.For)
				if !st.firing && c.now-st.aboveSince >= forSec*1000 {
					st.firing = true
					st.firedAt = c.now
					st.escalated = false
					st.lastRepeat = 0
					msg := "集群 " + grp + "（" + r.Service + "）状态损坏：" + fault +
						"（实例 " + instanceLabel(m.Instance) + " 节点 " + m.Node + "）"
					c.Fire(m.Node, m.Instance, m.Value, msg)
				} else {
					c.MaybeEscalate(m.Node, m.Instance, m.Value, st)
				}
			} else {
				if st.firing {
					st.firing = false
					msg := "集群 " + grp + "（" + r.Service + "）已恢复正常"
					c.Resolve(m.Node, m.Instance, m.Value, msg)
				}
				st.aboveSince = 0
			}
		}
	}
}
