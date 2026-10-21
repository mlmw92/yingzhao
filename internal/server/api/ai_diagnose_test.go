package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mlmw92/yingzhao/internal/model"
	"github.com/mlmw92/yingzhao/internal/server/ai"
	"github.com/mlmw92/yingzhao/internal/server/asset"
	"github.com/mlmw92/yingzhao/internal/server/config"
)

// stubAlerts 是活跃告警的最小替身（诊断只需要 Active()）。
type stubAlerts struct{ events []model.AlertEvent }

func (s stubAlerts) Recent(int) []model.AlertEvent { return s.events }
func (s stubAlerts) Active() []model.AlertEvent    { return s.events }

func alertOf(id, node string) model.AlertEvent {
	return model.AlertEvent{
		ID: id, RuleID: "rule-" + id, RuleName: "CPU 使用率过高", Node: node,
		Metric: "cpu_usage", Value: 95.5, Operator: ">", Threshold: 90,
		Severity: "warning", State: model.AlertStateFiring, Message: "持续 10 分钟高于阈值",
		StartsAt: 1759900000000,
	}
}

// captureEndpoint 起一个假端点并捕获请求体（断言"到底发了什么"）。
func captureEndpoint(t *testing.T) (*httptest.Server, *string) {
	t.Helper()
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = string(b)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   "fake-model",
			"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": "先看磁盘 inode"}}},
			"usage":   map[string]int{"total_tokens": 7},
		})
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func diagnoseTestAPI(t *testing.T) (*API, *ai.Manager, *asset.Service) {
	t.Helper()
	a, svc := assetTestAPI(t)
	m, err := ai.New(filepath.Join(t.TempDir(), "ai.yaml"), config.AIConfig{})
	if err != nil {
		t.Fatalf("ai.New: %v", err)
	}
	a.SetAIManager(m)
	return a, m, svc
}

// 未配置时**不发请求**并明确指路（否则用户会收到一个看不懂的上游错误）。
func TestAIDiagnoseNotConfiguredSendsNothing(t *testing.T) {
	a, _, _ := diagnoseTestAPI(t)
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit = true }))
	defer srv.Close()

	rec := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, reqWith(globalPrincipal("ai:invoke"), http.MethodPost, "/api/v1/ai/diagnose",
		`{"kind":"node","id":"web-01"}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("未配置应 400，实际 %d（%s）", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "AI 助手") {
		t.Fatalf("错误信息应指路到配置页：%s", rec.Body.String())
	}
	if hit {
		t.Fatal("未配置时不应发出任何请求")
	}
}

// 范围判定必须先于取数：受限身份不能借 ai:invoke 读到范围外的对象。
func TestAIDiagnoseScopeEnforced(t *testing.T) {
	a, m, _ := diagnoseTestAPI(t)
	srv, body := captureEndpoint(t)
	if err := m.Save(config.AIConfig{Enabled: true, BaseURL: srv.URL, Model: "m"}); err != nil {
		t.Fatal(err)
	}
	a.alerts = stubAlerts{events: []model.AlertEvent{alertOf("a1", "web-01"), alertOf("a2", "db-01")}}

	// g1 的受限身份诊断 db-01（g2）上的告警 → 拒绝，且不取任何数据
	rec := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, reqWith(restrictedPrincipal([]string{"ai:invoke", "nodes:read"}, "g1"),
		http.MethodPost, "/api/v1/ai/diagnose", `{"kind":"alert","id":"a2"}`))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "资源范围") {
		t.Fatalf("范围外告警应被拒，实际 %d（%s）", rec.Code, rec.Body.String())
	}
	if *body != "" {
		t.Fatal("范围判定失败时不应发出请求")
	}

	// 同一身份诊断 g1 上的告警 → 通过
	rec = httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, reqWith(restrictedPrincipal([]string{"ai:invoke", "nodes:read"}, "g1"),
		http.MethodPost, "/api/v1/ai/diagnose", `{"kind":"alert","id":"a1"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("范围内告警应可诊断，实际 %d（%s）", rec.Code, rec.Body.String())
	}
	if !strings.Contains(*body, "web-01") {
		t.Fatalf("上下文应含该主机信息：%s", *body)
	}
	// 范围外的主机名**不得**出现在上下文里
	if strings.Contains(*body, "db-01") {
		t.Fatalf("上下文泄露了范围外主机：%s", *body)
	}
}

// 敏感内容默认不发：属性值只在 AllowSensitive 打开时才进入上下文。
func TestAIDiagnoseSensitiveGate(t *testing.T) {
	a, m, svc := diagnoseTestAPI(t)
	srv, body := captureEndpoint(t)

	// 造一个带"敏感属性值"的资产（值里放一个不可能自然出现的串）
	seedAsset(t, svc, asset.TypeHost, "web-09", "web-09", "web-09", map[string]string{"dbConn": "SENSITIVE-TOKEN-123"})
	item, ok, err := svc.Get(asset.Ref{TypeKey: asset.TypeHost, NaturalKey: "web-09"})
	if err != nil || !ok {
		t.Fatalf("取资产失败：%v", err)
	}

	// 默认（AllowSensitive=false）：值不发，但**字段名**要发（它对诊断有用）
	if err := m.Save(config.AIConfig{Enabled: true, BaseURL: srv.URL, Model: "m"}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, reqWith(globalPrincipal("ai:invoke"), http.MethodPost, "/api/v1/ai/diagnose",
		`{"kind":"asset","id":"`+idStr(item.ID)+`"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("应成功，实际 %d（%s）", rec.Code, rec.Body.String())
	}
	if strings.Contains(*body, "SENSITIVE-TOKEN-123") {
		t.Fatalf("默认不得发送属性值：%s", *body)
	}
	if !strings.Contains(*body, "dbConn") {
		t.Fatalf("字段名应保留（它对诊断有用）：%s", *body)
	}
	resp := decodeBody(t, rec)
	if resp["sensitive"] == true {
		t.Fatal("默认不应标记含敏感内容")
	}
	notSent, _ := resp["notSent"].([]any)
	if len(notSent) == 0 {
		t.Fatal("应如实告知哪些内容没发出去")
	}

	// 打开 AllowSensitive：值进入上下文，并标记 sensitive
	if err := m.Save(config.AIConfig{Enabled: true, BaseURL: srv.URL, Model: "m", AllowSensitive: true}); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, reqWith(globalPrincipal("ai:invoke"), http.MethodPost, "/api/v1/ai/diagnose",
		`{"kind":"asset","id":"`+idStr(item.ID)+`"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("应成功，实际 %d（%s）", rec.Code, rec.Body.String())
	}
	if !strings.Contains(*body, "SENSITIVE-TOKEN-123") {
		t.Fatalf("打开后属性值应进入上下文：%s", *body)
	}
	if decodeBody(t, rec)["sensitive"] != true {
		t.Fatal("含敏感内容时应在响应里标记（界面要显眼提示）")
	}
}

func TestAIDiagnoseBadRequest(t *testing.T) {
	a, m, _ := diagnoseTestAPI(t)
	srv, _ := captureEndpoint(t)
	if err := m.Save(config.AIConfig{Enabled: true, BaseURL: srv.URL, Model: "m"}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ name, body string }{
		{"未知类型", `{"kind":"vm","id":"x"}`},
		{"缺少主机名", `{"kind":"node","id":""}`},
		{"告警不存在", `{"kind":"alert","id":"nope"}`},
	} {
		rec := httptest.NewRecorder()
		newRoutesMux(a).ServeHTTP(rec, reqWith(globalPrincipal("ai:invoke"), http.MethodPost, "/api/v1/ai/diagnose", c.body))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s：应 400，实际 %d（%s）", c.name, rec.Code, rec.Body.String())
		}
	}
}

// 权限：发起调用要 ai:invoke（只有 ai:read 不够——调用会把数据发出去）。
func TestAIDiagnosePermission(t *testing.T) {
	a, _, _ := diagnoseTestAPI(t)
	rec := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, reqWith(globalPrincipal("ai:read"), http.MethodPost, "/api/v1/ai/diagnose",
		`{"kind":"node","id":"web-01"}`))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("只有 ai:read 应 403，实际 %d", rec.Code)
	}
}

func idStr(v int64) string { return strconv.FormatInt(v, 10) }

// 变更历史里的「新值」就是属性值，属于高级别内容——不能借「最近变更」段漏出去。
//
// 这条是实机验证抓出来的：默认不取属性值，值却出现在了上下文里（经由变更记录）。
// 用例守着它，否则这个洞会悄悄回来。
func TestAIDiagnoseHistoryDoesNotLeakValues(t *testing.T) {
	a, m, svc := diagnoseTestAPI(t)
	srv, body := captureEndpoint(t)
	if err := m.Save(config.AIConfig{Enabled: true, BaseURL: srv.URL, Model: "m"}); err != nil {
		t.Fatal(err)
	}
	const secret = "LEAK-CHECK-9527"
	// 写一个人工属性值：会同时产生属性行与一条变更记录
	if _, _, err := svc.Apply(asset.Observation{
		TypeKey: asset.TypeHost, NaturalKey: "web-01", Name: "web-01", Node: "web-01",
		Source: asset.SourceManual, Actor: "tester", Attrs: map[string]string{"dbConn": secret},
	}); err != nil {
		t.Fatalf("写入属性失败：%v", err)
	}
	item, ok, err := svc.Get(asset.Ref{TypeKey: asset.TypeHost, NaturalKey: "web-01"})
	if err != nil || !ok {
		t.Fatalf("取资产失败：%v", err)
	}

	rec := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, reqWith(globalPrincipal("ai:invoke"), http.MethodPost, "/api/v1/ai/diagnose",
		`{"kind":"asset","id":"`+idStr(item.ID)+`"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("应成功，实际 %d（%s）", rec.Code, rec.Body.String())
	}
	if strings.Contains(*body, secret) {
		t.Fatalf("**变更历史不得携带属性值**（值属高级别，默认不发）：%s", *body)
	}
	if !strings.Contains(*body, "dbConn") {
		t.Fatalf("字段名仍应出现（模型要知道动过什么）：%s", *body)
	}
}
