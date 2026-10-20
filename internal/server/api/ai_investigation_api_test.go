package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nebula/monitor/internal/model"
	"github.com/nebula/monitor/internal/server/ai"
	"github.com/nebula/monitor/internal/server/config"
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

func retryReq(perm, body string) *http.Request {
	return reqWith(globalPrincipal(perm), http.MethodPost, "/api/v1/ai/investigations/retry", body)
}

// 重跑：立刻落一条 running 记录，模型调用在后台跑，HTTP 请求不等 120 秒。
func TestAIRetryInvestigationStartsNewRun(t *testing.T) {
	a, m := aiTestAPI(t)
	store := ai.NewInvestigationStore(filepath.Join(t.TempDir(), "inv.json"), 10, time.Hour)
	a.SetInvestigationStore(store)
	a.alerts = &alertStoreStub{active: []model.AlertEvent{{
		ID: "e1", RuleID: "r1", RuleName: "CPU 高", Node: "web-01",
		Severity: model.SeverityCritical, Message: "CPU > 90%", StartsAt: time.Now().UnixMilli(),
	}}}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{
			"message": map[string]any{"role": "assistant", "content": "结论：CPU 高"},
		}}})
	}))
	defer srv.Close()
	if err := m.Save(config.AIConfig{Enabled: true, BaseURL: srv.URL, Model: "m", ToolMode: "native"}); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, retryReq("ai:invoke", `{"alertId":"e1"}`))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("code = %d（body=%s）", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	if body["alertId"] != "e1" {
		t.Fatalf("响应应带回 alertId：%v", body)
	}

	// 后台 goroutine 完成前记录必须是 running（否则前端会立刻读到一个旧结论）
	inv, ok := store.ByAlert("e1")
	if !ok || inv.State != "running" {
		t.Fatalf("应立即落一条 running 记录，实际 %+v（存在=%v）", inv, ok)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		inv, _ = store.ByAlert("e1")
		if inv.State != "running" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if inv.State != "done" || inv.Answer != "结论：CPU 高" {
		t.Fatalf("后台调查应完成，实际 %+v", inv)
	}
}

func TestAIRetryInvestigationRejections(t *testing.T) {
	a, m := aiTestAPI(t)
	store := ai.NewInvestigationStore(filepath.Join(t.TempDir(), "inv.json"), 10, time.Hour)
	a.SetInvestigationStore(store)
	a.alerts = &alertStoreStub{active: []model.AlertEvent{{
		ID: "e1", RuleID: "r1", RuleName: "CPU 高", Node: "web-01",
		Severity: model.SeverityCritical, StartsAt: time.Now().UnixMilli(),
	}}}

	check := func(name string, req *http.Request, want int) {
		t.Helper()
		rec := httptest.NewRecorder()
		newRoutesMux(a).ServeHTTP(rec, req)
		if rec.Code != want {
			t.Fatalf("%s：code = %d，want %d（body=%s）", name, rec.Code, want, rec.Body.String())
		}
	}

	check("缺少 alertId", retryReq("ai:invoke", `{}`), http.StatusBadRequest)
	check("无 ai:invoke", retryReq("ai:read", `{"alertId":"e1"}`), http.StatusForbidden)
	check("AI 未配置", retryReq("ai:invoke", `{"alertId":"e1"}`), http.StatusConflict)
	if _, ok := store.ByAlert("e1"); ok {
		t.Fatal("AI 未配置时不应创建调查记录")
	}

	if err := m.Save(config.AIConfig{Enabled: true, BaseURL: "http://127.0.0.1:1/v1", Model: "m"}); err != nil {
		t.Fatal(err)
	}
	check("告警已恢复", retryReq("ai:invoke", `{"alertId":"gone"}`), http.StatusConflict)

	_ = store.Add(ai.Investigation{ID: "inv-x", AlertID: "e1", State: "running"})
	check("已有调查在跑", retryReq("ai:invoke", `{"alertId":"e1"}`), http.StatusConflict)
}

type errAlertStore struct{}

func (errAlertStore) Recent(int) []model.AlertEvent { return nil }
func (errAlertStore) Active() []model.AlertEvent    { return nil }
func (errAlertStore) ActiveWithError() ([]model.AlertEvent, error) {
	return nil, errors.New("时序库不可用")
}

// 告警查询失败不能伪装成「告警已恢复」：那会让值班的人以为问题自己好了。
func TestAIRetryInvestigationAlertQueryError(t *testing.T) {
	a, m := aiTestAPI(t)
	a.SetInvestigationStore(ai.NewInvestigationStore(filepath.Join(t.TempDir(), "inv.json"), 10, time.Hour))
	a.alerts = errAlertStore{}
	if err := m.Save(config.AIConfig{Enabled: true, BaseURL: "http://127.0.0.1:1/v1", Model: "m"}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, retryReq("ai:invoke", `{"alertId":"e1"}`))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d，want 503（body=%s）", rec.Code, rec.Body.String())
	}
}

// 调查运行时必须把工具记录的建议写进调查记录，并把「建议不会自动执行」写进系统提示。
func TestInvestigationRecordsSuggestions(t *testing.T) {
	a, m := aiTestAPI(t)
	store := ai.NewInvestigationStore(filepath.Join(t.TempDir(), "inv.json"), 10, time.Hour)
	a.SetInvestigationStore(store)
	a.alerts = &alertStoreStub{}

	var systemPrompt, toolResult string
	call := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		msgs, _ := body["messages"].([]any)
		if len(msgs) > 0 {
			if m0, ok := msgs[0].(map[string]any); ok && m0["role"] == "system" {
				systemPrompt, _ = m0["content"].(string)
			}
			for _, raw := range msgs {
				if mm, ok := raw.(map[string]any); ok && mm["role"] == "tool" {
					toolResult, _ = mm["content"].(string)
				}
			}
		}
		call++
		if call == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{
				"message": map[string]any{
					"role": "assistant",
					"tool_calls": []any{map[string]any{
						"id": "c1", "type": "function",
						"function": map[string]any{"name": "propose_action",
							"arguments": `{"kind":"svc.restart","params":{"unit":"nginx.service"},"reason":"单元 failed"}`},
					}},
				},
			}}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{
			"message": map[string]any{"role": "assistant", "content": "结论：建议重启 nginx"},
		}}})
	}))
	defer srv.Close()
	if err := m.Save(config.AIConfig{Enabled: true, BaseURL: srv.URL, Model: "m", ToolMode: "native"}); err != nil {
		t.Fatal(err)
	}

	a.RunAutoInvestigation(context.Background(), ai.AlertRef{
		ID: "e1", RuleID: "r1", RuleName: "CPU 高", Node: "web-01", Message: "CPU > 90%",
	})

	inv, ok := store.ByAlert("e1")
	if !ok || inv.State != "done" {
		t.Fatalf("调查未完成: %+v", inv)
	}
	if len(inv.Suggestions) != 1 {
		t.Fatalf("应记录 1 条建议，实际 %+v", inv.Suggestions)
	}
	s := inv.Suggestions[0]
	if s.Kind != "svc.restart" || s.Node != "web-01" || s.Params["unit"] != "nginx.service" {
		t.Fatalf("建议内容不正确: %+v", s)
	}
	if !strings.Contains(systemPrompt, "propose_action") || !strings.Contains(systemPrompt, "不会自动执行") {
		t.Fatalf("系统提示应说明建议能力与边界: %q", systemPrompt)
	}
	if !strings.Contains(toolResult, "不会自动执行") {
		t.Fatalf("工具回执应告诉模型建议不会自动执行: %q", toolResult)
	}
}
