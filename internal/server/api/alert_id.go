package api

import "github.com/mlmw92/yingzhao/internal/model"

// alertEventID 返回事件的稳定标识。
//
// 触发时的事件有真实 ID，但**从时序库恢复的事件没有**（ID 不落库）。
// 详情页与 AI 调查都靠它关联，所以按「规则|节点|实例」派生——
// 这与告警引擎的 firingKey 同一口径，两处必须用同一个函数，避免各算一套。
func alertEventID(e model.AlertEvent) string {
	if e.ID != "" {
		return e.ID
	}
	return e.RuleID + "|" + e.Node + "|" + e.Instance
}

// withStableIDs 为恢复的事件补上派生 ID（原地修改切片元素）。
func withStableIDs(events []model.AlertEvent) []model.AlertEvent {
	for i := range events {
		if events[i].ID == "" {
			events[i].ID = alertEventID(events[i])
		}
	}
	return events
}
