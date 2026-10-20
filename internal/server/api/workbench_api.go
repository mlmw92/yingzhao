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
	if !p.HasPermission("alerts:read") || a.alerts == nil {
		alertSeg.Available = false
	} else {
		alertSeg.Available = true
		events := filterByNodeScope(a, p, a.unacknowledgedAlerts(a.alerts.Active()),
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
	resp.Todos = append(resp.Todos, alertSeg)

	// 段 2：待我审批（别人提交、仍 pending）
	approvalSeg := workbenchTodo{Kind: "approval", Items: []workbenchTodoItem{}}
	if !p.HasPermission("approval:read") || a.approval == nil {
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
