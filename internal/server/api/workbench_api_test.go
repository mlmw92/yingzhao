package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nebula/monitor/internal/model"
)

func TestWorkbenchSummary_SegmentsByPermission(t *testing.T) {
	a := scopeTestAPI(t)
	a.alerts = &alertStoreStub{active: []model.AlertEvent{{
		ID: "e1", RuleID: "r1", RuleName: "cpu", Node: "web-01",
		Severity: model.SeverityWarning, State: model.AlertStateFiring,
	}}}
	// 没有 approval:read 的身份：审批段必须是 available=false，而不是 count=0
	p := globalPrincipal("alerts:read")
	rec := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, reqWith(p, http.MethodGet, "/api/v1/workbench/summary", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d（body=%s）", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	todos := body["todos"].([]any)
	if len(todos) != 2 {
		t.Fatalf("应返回两段待办，实际 %d", len(todos))
	}
	approval := todos[1].(map[string]any)
	if approval["available"] != false {
		t.Fatalf("无权限时审批段必须 available=false，实际 %v", approval)
	}
	alert := todos[0].(map[string]any)
	if alert["count"].(float64) != 1 {
		t.Fatalf("告警段 count = %v，want 1", alert["count"])
	}
}
