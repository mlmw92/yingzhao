package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/nebula/monitor/internal/server/ai"
)

func TestAIInvestigationsByAlert(t *testing.T) {
	a := scopeTestAPI(t)
	store := ai.NewInvestigationStore(filepath.Join(t.TempDir(), "inv.json"), 10, time.Hour)
	_ = store.Add(ai.Investigation{ID: "inv-1", AlertID: "e1", State: "done", Answer: "ok"})
	a.SetInvestigationStore(store)

	rec := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, reqWith(globalPrincipal("ai:read"), http.MethodGet,
		"/api/v1/ai/investigations?alertId=e1", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d（body=%s）", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	inv, ok := body["investigation"].(map[string]any)
	if !ok || inv["answer"] != "ok" {
		t.Fatalf("investigation = %v", body["investigation"])
	}
}

func TestAIInvestigationsMissingAlertID(t *testing.T) {
	a := scopeTestAPI(t)
	a.SetInvestigationStore(ai.NewInvestigationStore(filepath.Join(t.TempDir(), "inv.json"), 10, time.Hour))
	rec := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, reqWith(globalPrincipal("ai:read"), http.MethodGet,
		"/api/v1/ai/investigations", ""))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d，want 400", rec.Code)
	}
}
