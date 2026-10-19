package alert

// roleChangeEvaluator 数据库主从切换检测：对比各实例上一次评估记录的 role（来自 *_instance_up 的 role 标签），
// 发现变化即触发事件型告警（下一轮无变化则自动恢复），重复切换会重复触发。无 agent 改动（复用 TSDB 已有 role 标签）。
type roleChangeEvaluator struct{}

func (roleChangeEvaluator) Evaluate(c *evalContext) {
	r := c.rule
	metric := serviceMetric(r.Service)
	for _, n := range c.nodes {
		if !matchesGroup(r.Group, n.Group) {
			continue
		}
		if !matchesScope(r.Scope, r.Nodes, n.Hostname) {
			continue
		}
		for _, sample := range c.RoleSamples(n.Hostname, metric, upLabelsFor(r.Service)) {
			key := r.ID + "|" + n.Hostname + "|" + sample.instance
			st := c.State(key)
			// 仅在 topology 匹配时才判定为主从切换关注对象（未指定则全部）
			if r.Topology != "" && r.Topology != sample.topology {
				st.lastRole = sample.role
				continue
			}
			prev := st.lastRole
			st.lastRole = sample.role
			if prev == "" {
				// 首次采集仅记录基线，不触发
				continue
			}
			if prev != sample.role {
				// 事件型：每隔冷却窗口（默认 For，0 表示每次都触发）触发一次
				cooldownMs := parseFor(r.For) * 1000
				if c.now-st.aboveSince < cooldownMs {
					continue
				}
				st.aboveSince = c.now
				st.firing = true
				st.firedAt = c.now
				st.escalated = false
				st.lastRepeat = 0
				msg := "节点 " + n.Hostname + " 的 " + r.Service + " 实例 " + instanceLabel(sample.instance) +
					" 发生角色切换：" + roleText(prev) + " -> " + roleText(sample.role) + "（架构/拓扑=" + sample.topology + "）"
				c.Fire(n.Hostname, sample.instance, 0, msg)
				// 事件型：下一轮无变化即视为恢复（仅当仍为 firing 且无再次切换）
				st.resolvedAt = 0
			} else {
				// 角色未变：若之前触发过（firing），则自动恢复
				if st.firing {
					st.firing = false
					msg := "节点 " + n.Hostname + " 的 " + r.Service + " 实例 " + instanceLabel(sample.instance) + " 角色已稳定为 " + roleText(sample.role)
					c.Resolve(n.Hostname, sample.instance, 0, msg)
				}
			}
		}
	}
}
