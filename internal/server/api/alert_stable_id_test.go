package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mlmw92/yingzhao/internal/model"
)

// 从时序库恢复的事件没有 ID（ID 只在触发时生成、未落库）。
// 告警详情与 AI 调查都靠它关联：必须给出稳定派生 ID，否则面板永远查不到记录。
func TestAlertsRestoredEventGetsStableID(t *testing.T) {
	a := scopeTestAPI(t)
	a.alerts = &alertStoreStub{active: []model.AlertEvent{{
		RuleID: "r1", RuleName: "cpu", Node: "web-01", Instance: "i1",
		Severity: model.SeverityCritical, State: model.AlertStateFiring,
	}}}
	rec := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, reqWith(globalPrincipal("alerts:read"), http.MethodGet,
		"/api/v1/alerts?state=active", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d（body=%s）", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	alerts := body["alerts"].([]any)
	if len(alerts) != 1 {
		t.Fatalf("告警数 = %d", len(alerts))
	}
	id, _ := alerts[0].(map[string]any)["id"].(string)
	if id == "" {
		t.Fatal("恢复的告警必须有稳定 ID（供详情页关联 AI 调查）")
	}
	if id != "r1|web-01|i1" {
		t.Fatalf("id = %q，want r1|web-01|i1", id)
	}

	// AI 自动调查用同一个派生 ID 落库，避免两处各算一套
	refs := a.CriticalAlertsForAI().ActiveCritical()
	if len(refs) != 1 || refs[0].ID != id {
		t.Fatalf("AI 告警投影 ID 不一致: %+v", refs)
	}
}
