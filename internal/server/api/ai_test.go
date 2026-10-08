package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nebula/monitor/internal/server/ai"
	"github.com/nebula/monitor/internal/server/config"
)

func aiTestAPI(t *testing.T) (*API, *ai.Manager) {
	t.Helper()
	a := scopeTestAPI(t)
	m, err := ai.New(filepath.Join(t.TempDir(), "ai.yaml"), config.AIConfig{})
	if err != nil {
		t.Fatalf("ai.New: %v", err)
	}
	a.SetAIManager(m)
	return a, m
}

// GET 不返回明文密钥，但要告诉界面"已经存过密钥"（否则界面无法给出正确的 placeholder）。
func TestAIConfigGetRedactsKey(t *testing.T) {
	a, m := aiTestAPI(t)
	if err := m.Save(config.AIConfig{
		Enabled: true, BaseURL: "https://api.example.com/v1", APIKey: "sk-secret", Model: "m",
	}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, reqWith(globalPrincipal("ai:read"), http.MethodGet, "/api/v1/ai/config", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("状态 = %d（%s）", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "sk-secret") {
		t.Fatal("响应里不得出现明文密钥")
	}
	body := decodeBody(t, rec)
	cfg, _ := body["config"].(map[string]any)
	if cfg == nil || cfg["apiKey"] != "" {
		t.Fatalf("apiKey 应置空：%v", body["config"])
	}
	if body["hasApiKey"] != true {
		t.Fatal("应告诉界面已存过密钥")
	}
	if body["ready"] != true {
		t.Fatal("启用且端点模型齐备时 ready 应为 true")
	}
}

// PUT 空密钥沿用旧值（前端「不修改请留空」），其余字段照常更新。
func TestAIConfigPutKeepsKeyWhenEmpty(t *testing.T) {
	a, m := aiTestAPI(t)
	if err := m.Save(config.AIConfig{
		Enabled: true, BaseURL: "https://a.example.com/v1", APIKey: "sk-old", Model: "m",
	}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, reqWith(globalPrincipal("ai:write"), http.MethodPut, "/api/v1/ai/config",
		`{"enabled":true,"baseUrl":"https://a.example.com/v1","apiKey":"","model":"m2"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("状态 = %d（%s）", rec.Code, rec.Body.String())
	}
	got := m.Get()
	if got.APIKey != "sk-old" {
		t.Fatalf("空密钥应沿用旧值，实际 %q", got.APIKey)
	}
	if got.Model != "m2" {
		t.Fatalf("模型应更新，实际 %q", got.Model)
	}
}

func TestAIConfigPutValidation(t *testing.T) {
	a, _ := aiTestAPI(t)
	for _, c := range []struct{ name, body string }{
		{"缺端点", `{"enabled":true,"model":"m"}`},
		{"缺模型", `{"enabled":true,"baseUrl":"https://a.example.com/v1"}`},
		{"非法 scheme", `{"enabled":true,"baseUrl":"file:///etc/passwd","model":"m"}`},
	} {
		rec := httptest.NewRecorder()
		newRoutesMux(a).ServeHTTP(rec, reqWith(globalPrincipal("ai:write"), http.MethodPut, "/api/v1/ai/config", c.body))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s：应 400，实际 %d（%s）", c.name, rec.Code, rec.Body.String())
		}
	}
	// 关闭时不校验端点：允许"先关掉、再慢慢清理配置"
	rec := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, reqWith(globalPrincipal("ai:write"), http.MethodPut, "/api/v1/ai/config", `{"enabled":false}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("关闭时应允许保存，实际 %d（%s）", rec.Code, rec.Body.String())
	}
}

// 权限：读配置与改配置分开；调用（ai:invoke）更是另一回事。
func TestAIConfigPermissions(t *testing.T) {
	a, _ := aiTestAPI(t)
	rec := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, reqWith(globalPrincipal("ai:read"), http.MethodGet, "/api/v1/ai/config", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("ai:read 应能读，实际 %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, reqWith(globalPrincipal("ai:read"), http.MethodPut, "/api/v1/ai/config", `{}`))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("只有 ai:read 时写应 403，实际 %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, reqWith(globalPrincipal("nodes:read"), http.MethodGet, "/api/v1/ai/config", ""))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("无 ai:read 应 403，实际 %d", rec.Code)
	}
}

func TestAIConfigTest(t *testing.T) {
	a, m := aiTestAPI(t)

	// 未启用：400 且**不发请求**（这里没有端点可发）
	rec := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, reqWith(globalPrincipal("ai:write"), http.MethodPost, "/api/v1/ai/config/test", ""))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("未启用应 400，实际 %d（%s）", rec.Code, rec.Body.String())
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   "fake-model",
			"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": "pong"}}},
			"usage":   map[string]int{"total_tokens": 3},
		})
	}))
	defer srv.Close()
	if err := m.Save(config.AIConfig{Enabled: true, BaseURL: srv.URL, Model: "fake-model"}); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, reqWith(globalPrincipal("ai:write"), http.MethodPost, "/api/v1/ai/config/test", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("应 200，实际 %d（%s）", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	if body["model"] != "fake-model" || body["reply"] != "pong" {
		t.Fatalf("应回显模型与回复：%v", body)
	}

	// 端点不可达 → 502（上游问题，不是本地 500）
	if err := m.Save(config.AIConfig{Enabled: true, BaseURL: "http://127.0.0.1:1/v1", Model: "m"}); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, reqWith(globalPrincipal("ai:write"), http.MethodPost, "/api/v1/ai/config/test", ""))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("上游不可达应 502，实际 %d（%s）", rec.Code, rec.Body.String())
	}
}
