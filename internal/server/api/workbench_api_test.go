package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nebula/monitor/internal/model"
)

// alertStoreErrStub 模拟时序库查询失败。
type alertStoreErrStub struct{ alertStoreStub }

func (s *alertStoreErrStub) ActiveWithError() ([]model.AlertEvent, error) {
	return nil, errors.New("tsdb down")
}

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

// 未启用登录时没有 principal：不能把它当成「无权限」，否则默认部署上值班台永久失灵。
func TestWorkbenchSummary_NilPrincipalIsUnrestricted(t *testing.T) {
	a := scopeTestAPI(t)
	a.alerts = &alertStoreStub{active: []model.AlertEvent{{
		ID: "e1", RuleID: "r1", RuleName: "cpu", Node: "web-01",
		Severity: model.SeverityWarning, State: model.AlertStateFiring,
	}}}
	rec := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, reqWith(nil, http.MethodGet, "/api/v1/workbench/summary", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d（body=%s）", rec.Code, rec.Body.String())
	}
	todos := decodeBody(t, rec)["todos"].([]any)
	alert := todos[0].(map[string]any)
	if alert["available"] != true || alert["count"].(float64) != 1 {
		t.Fatalf("无 principal 时应视为不受限，实际 %v", alert)
	}
}

// 只有 approval:read 的身份（如安全管理员）不能被整条路由挡住。
func TestWorkbenchSummary_ApprovalOnlyUserNotBlocked(t *testing.T) {
	a := scopeTestAPI(t)
	a.alerts = &alertStoreStub{}
	rec := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, reqWith(globalPrincipal("approval:read"), http.MethodGet,
		"/api/v1/workbench/summary", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d，want 200（body=%s）", rec.Code, rec.Body.String())
	}
	todos := decodeBody(t, rec)["todos"].([]any)
	alert := todos[0].(map[string]any)
	if alert["available"] != false {
		t.Fatalf("无 alerts:read 时告警段应 available=false，实际 %v", alert)
	}
}

// 查询失败必须进 errors，不能把「查不到」显示成 0 条待办。
func TestWorkbenchSummary_AlertQueryErrorReported(t *testing.T) {
	a := scopeTestAPI(t)
	a.alerts = &alertStoreErrStub{}
	rec := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, reqWith(globalPrincipal("alerts:read"), http.MethodGet,
		"/api/v1/workbench/summary", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d（body=%s）", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	if errs, _ := body["errors"].([]any); len(errs) == 0 {
		t.Fatalf("查询失败必须出现在 errors 里，实际 %v", body)
	}
}
