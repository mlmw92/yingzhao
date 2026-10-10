package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nebula/monitor/internal/model"
)

// seriesStore 是 storage.Storage 的测试替身：按指标名返回预置序列。
type seriesStore struct {
	series         map[string][]model.Series
	labelsByMetric map[string]map[string]string
}

func (s *seriesStore) Write([]model.Metric) error { return nil }
func (s *seriesStore) QueryRange(string, string, map[string]string, int64, int64, int64) ([]model.Series, error) {
	return nil, nil
}
func (s *seriesStore) QueryLatest(string, string, map[string]string) (*model.Point, error) {
	return nil, nil
}
func (s *seriesStore) QueryInstant(string, string, map[string]string) ([]model.Series, error) {
	return nil, nil
}
func (s *seriesStore) QueryInstantWithLookback(string, string, map[string]string, time.Duration) ([]model.Series, error) {
	return nil, nil
}
func (s *seriesStore) QueryAllLatest(name string, _ map[string]string) ([]model.Series, error) {
	// 未预置序列时返回 nil，既有用例不受影响。
	if s.series == nil {
		return nil, nil
	}
	return s.series[name], nil
}
func (s *seriesStore) Close() error    { return nil }
func (s *seriesStore) Backend() string { return "fake" }

// alertStoreStub 是 AlertStore 的测试替身（scopeTestAPI 不注入告警，总览会读它）。
type alertStoreStub struct{ active []model.AlertEvent }

func (s *alertStoreStub) Recent(int) []model.AlertEvent { return s.active }
func (s *alertStoreStub) Active() []model.AlertEvent    { return s.active }

// TestMiddlewareOverview_WithoutRegistry 未注入注册表时返回内置 15 类。
func TestMiddlewareOverview_WithoutRegistry(t *testing.T) {
	a := scopeTestAPI(t)
	a.store = &seriesStore{}
	a.alerts = &alertStoreStub{}
	rec := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, reqWith(globalPrincipal("middleware:read"), http.MethodGet, "/api/v1/middleware/overview", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d（body=%s）", rec.Code, rec.Body.String())
	}
	var resp struct {
		Types []struct {
			Type string `json:"type"`
		} `json:"types"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应解析失败：%v", err)
	}
	if len(resp.Types) != 15 {
		t.Fatalf("未注入注册表时应只有内置 15 类，got %d", len(resp.Types))
	}
}

// TestMiddlewareViewConfig_Labels 展示类型开关必须带上 key → 展示名。
// 前端此前自己维护这份映射（且引用了一个不存在的对象），渲染期抛错会让整个勾选列表
// 渲染不出来——弹窗只剩说明与按钮，看起来像"没有类型可选"。
func TestMiddlewareViewConfig_Labels(t *testing.T) {
	a := scopeTestAPI(t)
	a.store = &seriesStore{}
	a.alerts = &alertStoreStub{}
	rec := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, reqWith(globalPrincipal("middleware:read"), http.MethodGet, "/api/v1/middleware/view-config", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d（body=%s）", rec.Code, rec.Body.String())
	}
	var resp struct {
		Available []string          `json:"available"`
		Labels    map[string]string `json:"labels"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应解析失败：%v", err)
	}
	if len(resp.Available) != 15 {
		t.Fatalf("available 应为内置 15 类，got %d", len(resp.Available))
	}
	for _, k := range resp.Available {
		if resp.Labels[k] == "" {
			t.Errorf("类型 %s 缺少展示名（弹窗里会只剩 key）", k)
		}
	}
	if resp.Labels["postgres"] != "PostgreSQL" {
		t.Errorf("postgres 的展示名 = %q，want PostgreSQL", resp.Labels["postgres"])
	}
}

// TestMiddlewareTypeInstances_UnknownType 未注册的类型应 404。
func TestMiddlewareTypeInstances_UnknownType(t *testing.T) {
	a := scopeTestAPI(t)
	a.store = &seriesStore{}
	a.alerts = &alertStoreStub{}
	rec := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, reqWith(globalPrincipal("middleware:read"), http.MethodGet, "/api/v1/middleware/no-such-mw/instances", ""))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("code = %d，want 404（body=%s）", rec.Code, rec.Body.String())
	}
}
