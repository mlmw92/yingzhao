package alert

import "github.com/nebula/monitor/internal/model"

// 安全事件规则是事件队列驱动（IngestSecurityEvents 入队、评估循环消费），
// 与周期性评估器是两条路径，因此不实现 evaluator 接口。

// evalSecurityEvents 按安全事件告警规则匹配并触发事件型告警。调用方须持有 e.mu。
func (e *Engine) evalSecurityEvents(r model.AlertRule, nodes []model.Node, now int64) {
	nodeSet := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		if matchesGroup(r.Group, n.Group) && matchesScope(r.Scope, r.Nodes, n.Hostname) {
			nodeSet[n.Hostname] = true
		}
	}
	for node, events := range e.securityEvents {
		if !nodeSet[node] {
			continue
		}
		for i := 0; i < len(events); {
			ev := events[i]
			if r.Category != "" && r.Category != ev.Category {
				i++
				continue
			}
			if !shouldAlertSecurityEvent(ev) {
				// 已过滤的 SSH 审计噪声不应长期滞留在待告警队列。
				events = append(events[:i], events[i+1:]...)
				continue
			}
			key := r.ID + "|" + ev.Node + "|" + ev.ID
			if _, seen := e.securityRuleEvents[key]; seen {
				// 已按该规则处理过，从事件队列移除，避免无限累积。
				events = append(events[:i], events[i+1:]...)
				continue
			}
			e.securityRuleEvents[key] = now
			msg := ev.Message
			if msg == "" {
				msg = "节点 " + ev.Node + " 发生安全事件：" + securityCategoryName(ev.Category)
			}
			e.emitRuleSecurityEvent(r, ev, now, msg)
			// 触发后从队列移除，避免重复处理与内存无限增长。
			events = append(events[:i], events[i+1:]...)
		}
		e.securityEvents[node] = events
	}
}

// shouldAlertSecurityEvent 过滤高频 SSH 审计噪声：登录失败、无效用户和认证中断只作安全日志保留；
// SSH 登录成功及其他安全事件仍可由规则告警，暴力破解则由聚合类别单独触发。
func shouldAlertSecurityEvent(ev model.SecurityEvent) bool {
	if ev.Category != model.SecurityCatSSHAudit {
		return true
	}
	switch ev.Detail["result"] {
	case "failed", "invalid", "closed":
		return false
	default:
		return true
	}
}

func (e *Engine) emitRuleSecurityEvent(r model.AlertRule, source model.SecurityEvent, now int64, message string) {
	ev := model.AlertEvent{
		ID:        "security-rule-" + source.ID,
		RuleID:    r.ID,
		RuleName:  r.Name,
		RuleType:  r.Type,
		Node:      source.Node,
		NodeIP:    source.NodeIP,
		Instance:  source.ID,
		Metric:    "security_event",
		Value:     1,
		Operator:  "==",
		Threshold: 1,
		Severity:  r.Severity,
		State:     model.AlertStateFiring,
		Message:   message,
		StartsAt:  now,
		Notify:    append([]string(nil), r.Notify...),
	}
	suppressed, by := e.computeSuppressedLocked(ev)
	ev.Suppressed = suppressed
	ev.SuppressedBy = by
	e.trackFiringLocked(ev, suppressed)
	e.refreshSuppressedTargetsLocked(ev)
	e.alerts.Add(ev)
	if e.broadcaster != nil {
		e.broadcaster.BroadcastAlert(ev)
	}
	if !suppressed {
		if e.grouper != nil {
			e.grouper.Add(ev)
		} else {
			e.notify(ev)
		}
	}
}
