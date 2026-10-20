package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/nebula/monitor/internal/server/uicfg"
)

func uiTestAPI(t *testing.T) *API {
	t.Helper()
	a := scopeTestAPI(t)
	m, err := uicfg.New(filepath.Join(t.TempDir(), "ui.yaml"),
		uicfg.UIConfig{Name: "英招", Subtitle: "运维平台"})
	if err != nil {
		t.Fatalf("uicfg.New: %v", err)
	}
	a.uiMgr = m
	return a
}

func TestUIPutPartialUpdateKeepsBrand(t *testing.T) {
	a := uiTestAPI(t)
	rec := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, reqWith(globalPrincipal("system:config"), http.MethodPut,
		"/api/v1/ui/settings", `{"disabled":["assets"]}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d（body=%s）", rec.Code, rec.Body.String())
	}
	cfg := a.uiMgr.Get()
	if cfg.Name != "英招" || cfg.Subtitle != "运维平台" {
		t.Fatalf("部分更新不应清空品牌字段: %+v", cfg)
	}
	if len(cfg.Disabled) != 1 || cfg.Disabled[0] != "assets" {
		t.Fatalf("disabled 未保存: %+v", cfg.Disabled)
	}
}

func TestUIPutRejectsLockedModule(t *testing.T) {
	a := uiTestAPI(t)
	rec := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, reqWith(globalPrincipal("system:config"), http.MethodPut,
		"/api/v1/ui/settings", `{"disabled":["alerts"]}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d，want 400（body=%s）", rec.Code, rec.Body.String())
	}
}
