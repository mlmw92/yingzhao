package api

import (
	"net/http"

	"github.com/nebula/monitor/internal/model"
	"github.com/nebula/monitor/internal/server/approval"
)

type workbenchTodoItem struct {
	ID        string         `json:"id"`
	Title     string         `json:"title"`
	Node      string         `json:"node,omitempty"`
	Instance  string         `json:"instance,omitempty"`
	Severity  model.Severity `json:"severity,omitempty"`
	Message   string         `json:"message,omitempty"`
	CreatedAt int64          `json:"createdAt,omitempty"`
}

type workbenchTodo struct {
	Kind      string              `json:"kind"`
	Available bool                `json:"available"`
	Count     int                 `json:"count"`
	Items     []workbenchTodoItem `json:"items"`
}

type workbenchError struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

// handleWorkbenchSummary 聚合「需要人做决定才能推进」的事项。
//
// 三条硬要求：
//   - available 与 count=0 分开：没权限/未启用审批不等于「没人提交」；
//   - 单段失败不拖垮整体，但失败要进 errors，界面不能显示成 0 条；
//   - 范围收窄在服务端做，不依赖前端过滤。
func (a *API) handleWorkbenchSummary(w http.ResponseWriter, r *http.Request) {
	p := Principal(r)
	resp := struct {
		Todos  []workbenchTodo  `json:"todos"`
		Errors []workbenchError `json:"errors"`
	}{Todos: make([]workbenchTodo, 0, 2), Errors: []workbenchError{}}

	// 段 1：待确认告警（活跃且未认领）
	alertSeg := workbenchTodo{Kind: "alert", Items: []workbenchTodoItem{}}
	// 未启用登录时没有 principal：视为不受限（与 filterByNodeScope 同一口径）
	if (p != nil && !p.HasPermission("alerts:read")) || a.alerts == nil {
		alertSeg.Available = false
	} else {
		alertSeg.Available = true
		events, err := activeAlerts(a.alerts)
		if err != nil {
			// 查询失败不能显示成 0 条：界面据 errors 展示失败
			resp.Errors = append(resp.Errors, workbenchError{Kind: "alert", Message: "查询活跃告警失败"})
		} else {
			events = filterByNodeScope(a, p, a.unacknowledgedAlerts(events),
				func(e model.AlertEvent) string { return e.Node })
			alertSeg.Count = len(events)
			for i, e := range events {
				if i >= 10 {
					break
				}
				alertSeg.Items = append(alertSeg.Items, workbenchTodoItem{
					ID: e.ID, Title: e.RuleName, Node: e.Node, Instance: e.Instance,
					Severity: e.Severity, Message: e.Message, CreatedAt: e.StartsAt,
				})
			}
		}
	}
	resp.Todos = append(resp.Todos, alertSeg)

	// 段 2：待我审批（别人提交、仍 pending）
	approvalSeg := workbenchTodo{Kind: "approval", Items: []workbenchTodoItem{}}
	if (p != nil && !p.HasPermission("approval:read")) || a.approval == nil || !a.approvalConfig().Enabled {
		approvalSeg.Available = false
	} else {
		approvalSeg.Available = true
		a.approval.ExpireOverdue()
		me := AuthenticatedUser(r)
		for _, it := range a.approval.List(approval.Filter{State: approval.StatePending, ExcludeRequester: me}) {
			if !a.approvalInScope(p, it) {
				continue
			}
			if approvalSeg.Count < 10 {
				approvalSeg.Items = append(approvalSeg.Items, workbenchTodoItem{
					ID: it.ID, Title: it.Title, Node: it.Node, CreatedAt: it.CreatedAt,
				})
			}
			approvalSeg.Count++
		}
	}
	resp.Todos = append(resp.Todos, approvalSeg)

	writeJSON(w, http.StatusOK, resp)
}

// activeAlertStore 是可选用能力：实现它就能把「查询失败」如实上报。
// 不强行扩展 AlertStore 接口，避免所有测试替身跟着改。
type activeAlertStore interface {
	ActiveWithError() ([]model.AlertEvent, error)
}

func activeAlerts(store AlertStore) ([]model.AlertEvent, error) {
	if s, ok := store.(activeAlertStore); ok {
		return s.ActiveWithError()
	}
	return store.Active(), nil
}
