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
	"github.com/nebula/monitor/internal/server/approval"
	"github.com/nebula/monitor/internal/server/auth"
	"github.com/nebula/monitor/internal/server/config"
	"github.com/nebula/monitor/internal/server/node"
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

// D2 的决定性判据：全局审批开关关闭时，AI 建议仍然只能进审批。
func TestAISubmitSuggestionCreatesApprovalWhenDisabled(t *testing.T) {
	a, _, st, cfg := approvalTestAPI(t)
	c := cfg.Get()
	c.Enabled = false
	if err := cfg.Set(c); err != nil {
		t.Fatal(err)
	}
	invStore := ai.NewInvestigationStore(filepath.Join(t.TempDir(), "inv.json"), 10, time.Hour)
	a.SetInvestigationStore(invStore)
	_ = invStore.Add(ai.Investigation{
		ID: "inv-1", AlertID: "e1", Node: "web-01", State: "done",
		Suggestions: []ai.Suggestion{{
			ID: "sug-1", Kind: "svc.restart", Title: "重启服务",
			Params: map[string]string{"unit": "nginx.service"}, Node: "web-01",
			Reason: "nginx.service 处于 failed",
		}},
	})

	p := approvalPrincipal("ops1", true, "ai:invoke", "ops:exec")
	rec := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, approvalReq(p, http.MethodPost,
		"/api/v1/ai/investigations/inv-1/suggestions/sug-1/submit", ""))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("code = %d（body=%s）", rec.Code, rec.Body.String())
	}

	body := decodeBody(t, rec)
	ap, _ := body["approval"].(map[string]any)
	id, _ := ap["id"].(string)
	if id == "" {
		t.Fatalf("响应缺少审批单号: %v", body)
	}
	req, ok := st.Get(id)
	if !ok {
		t.Fatalf("审批单 %s 未落库", id)
	}
	if req.Kind != approval.EntryOpsWrite || req.OpsKind != "svc.restart" {
		t.Fatalf("审批单动作不正确: %+v", req)
	}
	if req.Node != "web-01" || req.Params["unit"] != "nginx.service" {
		t.Fatalf("审批单目标或参数不正确: %+v", req)
	}
	if req.Extra["source"] != "ai" || req.Extra["investigationId"] != "inv-1" || req.Extra["suggestionId"] != "sug-1" {
		t.Fatalf("审批单缺少来源标记: %+v", req.Extra)
	}
	if req.Requester != "ops1" {
		t.Fatalf("申请人应为当前用户，实际 %q", req.Requester)
	}
	// 模型给的理由必须到审批人眼前：审批人判断的是「要不要批准这个动作」，理由是他的依据。
	if req.Reason != "nginx.service 处于 failed" {
		t.Fatalf("审批单应带上模型理由，实际 %q", req.Reason)
	}
}

// 幂等：同一调查的同一条建议只能有一张未结束的审批单。
// 面板刷新后本地状态会丢，用户再点一次不能变成两张单子——批准两次就是执行两次。
func TestAISubmitSuggestionIsIdempotent(t *testing.T) {
	a, _, st, _ := approvalTestAPI(t)
	invStore := ai.NewInvestigationStore(filepath.Join(t.TempDir(), "inv.json"), 10, time.Hour)
	a.SetInvestigationStore(invStore)
	_ = invStore.Add(ai.Investigation{
		ID: "inv-1", AlertID: "e1", Node: "web-01", State: "done",
		Suggestions: []ai.Suggestion{{
			ID: "sug-1", Kind: "svc.restart", Params: map[string]string{"unit": "nginx.service"}, Node: "web-01",
		}},
	})

	p := approvalPrincipal("ops1", true, "ai:invoke", "ops:exec")
	target := "/api/v1/ai/investigations/inv-1/suggestions/sug-1/submit"
	first := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(first, approvalReq(p, http.MethodPost, target, ""))
	if first.Code != http.StatusAccepted {
		t.Fatalf("第一次提交 code = %d（%s）", first.Code, first.Body.String())
	}
	firstID, _ := decodeBody(t, first)["approval"].(map[string]any)["id"].(string)

	second := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(second, approvalReq(p, http.MethodPost, target, ""))
	if second.Code != http.StatusAccepted {
		t.Fatalf("第二次提交 code = %d（%s）", second.Code, second.Body.String())
	}
	body := decodeBody(t, second)
	if body["duplicate"] != true {
		t.Fatalf("重复提交应标记 duplicate: %v", body)
	}
	secondID, _ := body["approval"].(map[string]any)["id"].(string)
	if secondID != firstID {
		t.Fatalf("重复提交应返回同一张审批单：%q vs %q", firstID, secondID)
	}
	if got := len(st.List(approval.Filter{State: approval.StatePending, Limit: 500})); got != 1 {
		t.Fatalf("应只有 1 张待审批单，实际 %d", got)
	}
}

func TestAISubmitSuggestionRejections(t *testing.T) {
	a, _, _, _ := approvalTestAPI(t)
	invStore := ai.NewInvestigationStore(filepath.Join(t.TempDir(), "inv.json"), 10, time.Hour)
	a.SetInvestigationStore(invStore)
	_ = invStore.Add(ai.Investigation{
		ID: "inv-1", AlertID: "e1", Node: "web-01", State: "done",
		Suggestions: []ai.Suggestion{
			{ID: "sug-1", Kind: "svc.restart", Params: map[string]string{"unit": "nginx.service"}, Node: "web-01"},
			{ID: "sug-out", Kind: "svc.restart", Params: map[string]string{"unit": "nginx.service"}, Node: "db-01"},
			{ID: "sug-bad", Kind: "svc.restart", Params: map[string]string{"unit": "nginx"}, Node: "web-01"},
			{ID: "sug-ro", Kind: "node.diagnostics", Node: "web-01"},
			{ID: "sug-file", Kind: "file.push", Params: map[string]string{"path": "/tmp/x", "fileId": "obf-1"}, Node: "web-01"},
		},
	})

	check := func(name string, p *auth.Principal, target string, want int) {
		t.Helper()
		rec := httptest.NewRecorder()
		newRoutesMux(a).ServeHTTP(rec, approvalReq(p, http.MethodPost, target, ""))
		if rec.Code != want {
			t.Fatalf("%s：code = %d，want %d（body=%s）", name, rec.Code, want, rec.Body.String())
		}
	}
	base := "/api/v1/ai/investigations/inv-1/suggestions/"

	check("无 ai:invoke", approvalPrincipal("ops1", true, "ops:exec"), base+"sug-1/submit", http.StatusForbidden)
	check("无 ops:exec", approvalPrincipal("ops1", true, "ai:invoke"), base+"sug-1/submit", http.StatusForbidden)
	check("调查不存在", approvalPrincipal("ops1", true, "ai:invoke", "ops:exec"),
		"/api/v1/ai/investigations/nope/suggestions/sug-1/submit", http.StatusNotFound)
	check("建议不存在", approvalPrincipal("ops1", true, "ai:invoke", "ops:exec"),
		base+"nope/submit", http.StatusNotFound)
	check("参数失效", approvalPrincipal("ops1", true, "ai:invoke", "ops:exec"),
		base+"sug-bad/submit", http.StatusBadRequest)
	check("只读动作", approvalPrincipal("ops1", true, "ai:invoke", "ops:exec"),
		base+"sug-ro/submit", http.StatusBadRequest)
	check("文件分发", approvalPrincipal("ops1", true, "ai:invoke", "ops:exec"),
		base+"sug-file/submit", http.StatusBadRequest)
	// 范围：db-01 属于 g2，ops1 只被授予 g1
	check("范围外节点", approvalPrincipal("ops1", false, "ai:invoke", "ops:exec"),
		base+"sug-out/submit", http.StatusNotFound)
}

// 提交前节点离线：必须 409，不能把注定过期的动作推给审批人。
func TestAISubmitSuggestionOfflineNode(t *testing.T) {
	a, _, _, _ := approvalTestAPI(t)
	mgr := node.New(filepath.Join(t.TempDir(), "nodes.json"), time.Millisecond)
	mgr.Register(&model.ReportPayload{Node: "web-01", Group: "g1", Mode: model.ModeCollect})
	time.Sleep(5 * time.Millisecond)
	mgr.OfflineStale()
	a.nodeMgr = mgr

	invStore := ai.NewInvestigationStore(filepath.Join(t.TempDir(), "inv.json"), 10, time.Hour)
	a.SetInvestigationStore(invStore)
	_ = invStore.Add(ai.Investigation{
		ID: "inv-1", AlertID: "e1", Node: "web-01", State: "done",
		Suggestions: []ai.Suggestion{{
			ID: "sug-1", Kind: "svc.restart", Params: map[string]string{"unit": "nginx.service"}, Node: "web-01",
		}},
	})

	rec := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, approvalReq(
		approvalPrincipal("ops1", true, "ai:invoke", "ops:exec"),
		http.MethodPost, "/api/v1/ai/investigations/inv-1/suggestions/sug-1/submit", ""))
	if rec.Code != http.StatusConflict {
		t.Fatalf("code = %d，want 409（body=%s）", rec.Code, rec.Body.String())
	}
}
