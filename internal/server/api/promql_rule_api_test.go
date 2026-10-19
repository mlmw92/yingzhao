package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/nebula/monitor/internal/model"
	"github.com/nebula/monitor/internal/server/alert"
)

// promQLStore 在 seriesStore 基础上注入 PromQL 查询结果：
// "bad(" 模拟表达式语法错误，其余表达式返回一条命中序列。
type promQLStore struct{ seriesStore }

func (s *promQLStore) QueryPromQL(expr string) ([]model.Series, error) {
	if expr == "bad(" {
		return nil, errors.New(`parse error: unexpected "("`)
	}
	return []model.Series{{Labels: map[string]string{"node": "web-01"},
		Points: []model.Point{{Timestamp: 1, Value: 2}}}}, nil
}

// promQLTestAPI 在 scopeTestAPI 基础上补规则存储与可注入的 PromQL 存储。
func promQLTestAPI(t *testing.T) *API {
	t.Helper()
	a := scopeTestAPI(t)
	a.store = &promQLStore{}
	a.rules = alert.NewRulesStore(filepath.Join(t.TempDir(), "rules.yaml"))
	return a
}

func TestPromQLPreview(t *testing.T) {
	a := promQLTestAPI(t)
	rec := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, reqWith(globalPrincipal("alerts:write"), http.MethodPost,
		"/api/v1/rules/promql/preview", `{"expr":"up == 0"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d（body=%s）", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	if body["count"].(float64) != 1 {
		t.Fatalf("count = %v，want 1", body["count"])
	}
}

func TestPromQLPreviewRejectsBadExpr(t *testing.T) {
	a := promQLTestAPI(t)
	rec := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, reqWith(globalPrincipal("alerts:write"), http.MethodPost,
		"/api/v1/rules/promql/preview", `{"expr":"bad("}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d，want 400（body=%s）", rec.Code, rec.Body.String())
	}
}

func TestCreatePromQLRuleValidatesExpression(t *testing.T) {
	a := promQLTestAPI(t)
	rec := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, reqWith(globalPrincipal("alerts:write"), http.MethodPost,
		"/api/v1/rules", `{"name":"p","type":"promql","expr":"bad(","severity":"warning","enabled":true}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d，want 400（body=%s）", rec.Code, rec.Body.String())
	}
}
