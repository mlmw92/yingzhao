package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/nebula/monitor/internal/model"
	"github.com/nebula/monitor/internal/server/security"
)

// 夹具：scopeTestAPI 的分组是 web-01/web-02 → g1、db-01 → g2。
func guardRiskAPI(t *testing.T) *API {
	t.Helper()
	a := scopeTestAPI(t)
	a.security = security.New(filepath.Join(t.TempDir(), "security_store.json"))
	return a
}

func riskRow(t *testing.T, rec *httptest.ResponseRecorder, node string) map[string]any {
	t.Helper()
	nodes, _ := decodeBody(t, rec)["nodes"].([]any)
	for _, n := range nodes {
		m, _ := n.(map[string]any)
		if m["node"] == node {
			return m
		}
	}
	return nil
}

// 主流程：风险清单能查出来，字段齐（节点/别名/IP/风险/是否上报过）。
func TestGuardRisks_API_ReturnsRisks(t *testing.T) {
	a := guardRiskAPI(t)
	mux := newRoutesMux(a)
	a.security.SaveGuardRisks("web-01", []model.GuardRisk{{
		Kind: model.GuardRiskPersistence, AllowedDir: "/etc/cron.d", Exposed: "/etc/cron.d",
	}}, true)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, reqWith(globalPrincipal("security:read"), http.MethodGet, "/api/v1/security/guard-risks", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	if body["enabled"] != true {
		t.Fatalf("enabled 应为 true，实际 %v", body["enabled"])
	}
	row := riskRow(t, rec, "web-01")
	if row == nil {
		t.Fatalf("应返回 web-01 的风险，实际 %s", rec.Body.String())
	}
	if row["reported"] != true {
		t.Fatalf("已上报的节点 reported 应为 true，实际 %v", row["reported"])
	}
	risks, _ := row["risks"].([]any)
	if len(risks) != 1 {
		t.Fatalf("应有 1 条风险，实际 %v", row["risks"])
	}
	first, _ := risks[0].(map[string]any)
	if first["allowedDir"] != "/etc/cron.d" || first["kind"] != model.GuardRiskPersistence {
		t.Fatalf("风险字段不全：%v", first)
	}
	// 人话由服务端给（Describe()）：界面与 Agent 日志必须同一句，界面不该自己拼一遍
	want := model.GuardRisk{Kind: model.GuardRiskPersistence, AllowedDir: "/etc/cron.d", Exposed: "/etc/cron.d"}.Describe()
	if first["description"] != want {
		t.Fatalf("description 应与 Describe() 一致\n实际 %v\n期望 %v", first["description"], want)
	}
}

// **决定性**：未上报（旧 Agent）必须与"已检查、无风险"在响应里区分开——
// 把前者显示成后者会让人以为那台机器是安全的。
func TestGuardRisks_API_DistinguishesNotReported(t *testing.T) {
	a := guardRiskAPI(t)
	mux := newRoutesMux(a)
	a.security.SaveGuardRisks("old-01", nil, false)              // 旧 Agent：压根没这个字段
	a.security.SaveGuardRisks("web-01", []model.GuardRisk{}, true) // 新 Agent：报了，空的

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, reqWith(globalPrincipal("security:read"), http.MethodGet, "/api/v1/security/guard-risks", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("应 200，实际 %d", rec.Code)
	}
	old := riskRow(t, rec, "old-01")
	fresh := riskRow(t, rec, "web-01")
	if old == nil || fresh == nil {
		t.Fatalf("两个节点都应在响应里，实际 %s", rec.Body.String())
	}
	if old["reported"] != false {
		t.Fatalf("未上报的节点 reported 应为 false，实际 %v", old["reported"])
	}
	if fresh["reported"] != true {
		t.Fatalf("已上报的节点 reported 应为 true，实际 %v", fresh["reported"])
	}
	// 空清单要序列化成 [] 而不是 null：界面按"已检查、无风险"渲染
	if risks, ok := fresh["risks"].([]any); !ok || risks == nil {
		t.Fatalf("空清单应为 []（不是 null），实际 %#v", fresh["risks"])
	}
}

// 资源范围：只管 g1 的身份看不到 db-01 的风险（风险描述含该机器的目录与单元名，属不该外泄的信息面）。
func TestGuardRisks_API_ScopeEnforced(t *testing.T) {
	a := guardRiskAPI(t)
	mux := newRoutesMux(a)
	a.security.SaveGuardRisks("db-01", []model.GuardRisk{{
		Kind: model.GuardRiskExec, AllowedDir: "/opt/db/bin", Exposed: "/opt/db/bin/db", Unit: "db.service",
	}}, true)
	a.security.SaveGuardRisks("web-01", []model.GuardRisk{}, true)

	p := restrictedPrincipal([]string{"security:read"}, "g1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, reqWith(p, http.MethodGet, "/api/v1/security/guard-risks", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("应 200，实际 %d", rec.Code)
	}
	if riskRow(t, rec, "db-01") != nil {
		t.Fatalf("范围外节点的风险不该出现：%s", rec.Body.String())
	}
	if riskRow(t, rec, "web-01") == nil {
		t.Fatalf("范围内节点应可见：%s", rec.Body.String())
	}
}

// 权限：没有 security:read 一律 403（路由层就该拦住）。
func TestGuardRisks_API_NeedsPermission(t *testing.T) {
	a := guardRiskAPI(t)
	mux := newRoutesMux(a)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, reqWith(restrictedPrincipal([]string{"nodes:read"}, "g1"),
		http.MethodGet, "/api/v1/security/guard-risks", ""))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("无权限应 403，实际 %d", rec.Code)
	}
}
