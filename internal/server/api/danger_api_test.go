package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nebula/monitor/internal/server/approval"
	"github.com/nebula/monitor/internal/server/auth"
	"github.com/nebula/monitor/internal/server/ops"
)

// 提交一次 svc.restart（用 unit 区分"危险/普通"，无需上传文件）。
func submitRestart(t *testing.T, mux *http.ServeMux, p *auth.Principal, unit string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, approvalReq(p, http.MethodPost, "/api/v1/ops/tasks",
		`{"node":"web-01","kind":"svc.restart","params":{"unit":"`+unit+`"}}`))
	return rec
}

// **决定性判据**：审批关闭时，危险参数**不硬拦**（仍 201），但必须留下审计。
//
// 不硬拦是刻意的：单管理员部署下硬拦等于把功能锁死，而且硬拦的结果是运维登机器手改，
// 风险反而看不见（"危险"不等于"禁止"）。
func TestDanger_NotBlockedWhenApprovalOff(t *testing.T) {
	a, svc, _, cfg := approvalTestAPI(t)
	mux := newRoutesMux(a)
	svc.Store().SaveCaps("web-01", []string{ops.KindSvcRestart})
	c := cfg.Get()
	c.Enabled = false
	if err := cfg.Set(c); err != nil {
		t.Fatalf("关闭审批失败: %v", err)
	}
	p := approvalPrincipal("ops1", false, "ops:exec")

	rec := submitRestart(t, mux, p, "sshd.service")
	if rec.Code != http.StatusCreated {
		t.Fatalf("审批关闭时危险参数应仍直接下发（201），实际 %d：%s", rec.Code, rec.Body.String())
	}
	events := a.audit.ListFiltered(20, "", "", "ops")
	found := ""
	for _, e := range events {
		if e.Action == "danger-param" {
			found = e.Detail
		}
	}
	if found == "" {
		t.Fatal("危险参数必须留下审计——审批关闭时它是唯一的留痕")
	}
	if !strings.Contains(found, "sshd.service") || !strings.Contains(found, "失联") {
		t.Fatalf("审计应点名参数值与理由，实际 %q", found)
	}
}

// **决定性判据**：审批开启时，危险参数**无条件**进审批——即使它不在 requireFor 里。
//
// 并设**对照组**：同一配置下换个普通单元，应当直接 201——证明是"参数"在起作用，不是"动作"。
func TestDanger_ForcesApprovalRegardlessOfRequireFor(t *testing.T) {
	a, svc, _, cfg := approvalTestAPI(t)
	mux := newRoutesMux(a)
	svc.Store().SaveCaps("web-01", []string{ops.KindSvcRestart})
	// 审批开启，但范围里**不含** ops:write —— 单看配置，写类动作不该被拦
	c := cfg.Get()
	c.Enabled = true
	c.RequireFor = []string{approval.EntryDefense}
	if err := cfg.Set(c); err != nil {
		t.Fatalf("保存配置失败: %v", err)
	}
	p := approvalPrincipal("ops1", false, "ops:exec")

	rec := submitRestart(t, mux, p, "sshd.service")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("危险参数应无条件进审批（202），实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	if body["pending"] != true {
		t.Fatalf("应返回 pending，实际 %v", body)
	}
	// 响应里要把命中的理由带上：操作者得知道为什么这次进了审批
	if !strings.Contains(rec.Body.String(), "失联") {
		t.Fatalf("响应应带上命中理由，实际 %s", rec.Body.String())
	}

	// 对照组：同样配置、同样动作，换个普通单元 → 直接下发
	rec = submitRestart(t, mux, p, "nginx.service")
	if rec.Code != http.StatusCreated {
		t.Fatalf("普通参数不该被拦（201），实际 %d：%s", rec.Code, rec.Body.String())
	}
}

// 危险参数 + 审批关闭 + 普通参数：既不下发也不留 danger 审计的边界（别把普通动作也记成危险）。
func TestDanger_NormalParamLeavesNoDangerAudit(t *testing.T) {
	a, svc, _, cfg := approvalTestAPI(t)
	mux := newRoutesMux(a)
	svc.Store().SaveCaps("web-01", []string{ops.KindSvcRestart})
	c := cfg.Get()
	c.Enabled = true
	c.RequireFor = []string{approval.EntryDefense}
	if err := cfg.Set(c); err != nil {
		t.Fatalf("保存配置失败: %v", err)
	}
	p := approvalPrincipal("ops1", false, "ops:exec")

	submitRestart(t, mux, p, "nginx.service")
	for _, e := range a.audit.ListFiltered(20, "", "", "ops") {
		if e.Action == "danger-param" {
			t.Fatalf("普通参数不该留下危险审计：%s", e.Detail)
		}
	}
}

// 动作目录接口要下发危险规则清单（界面靠它做提交前提示；清单只有一份，由服务端给）。
func TestDanger_ActionsEndpointExposesRules(t *testing.T) {
	a, _, _, _ := approvalTestAPI(t)
	mux := newRoutesMux(a)
	p := approvalPrincipal("ops1", false, "ops:read")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, approvalReq(p, http.MethodGet, "/api/v1/ops/actions", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("动作目录应 200，实际 %d", rec.Code)
	}
	rules, _ := decodeBody(t, rec)["dangerRules"].([]any)
	if len(rules) == 0 {
		t.Fatal("动作目录应带上 dangerRules（否则界面无法在提交前提示）")
	}
	first, _ := rules[0].(map[string]any)
	if first["reason"] == nil || first["reason"] == "" {
		t.Fatalf("每条规则都要带理由，实际 %v", first)
	}
	// 清单值必须带出来：没有值，界面就没法匹配
	vals, _ := first["values"].([]any)
	if len(vals) == 0 {
		t.Fatalf("规则必须带清单值（否则界面无法匹配），实际 %v", first)
	}
}
