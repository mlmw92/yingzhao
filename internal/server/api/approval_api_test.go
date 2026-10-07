package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nebula/monitor/internal/server/approval"
	"github.com/nebula/monitor/internal/server/auth"
	"github.com/nebula/monitor/internal/server/ops"
)

// approvalTestAPI 在 opsTestAPI 的基础上接入审批（临时文件），并**默认开启**审批。
// 夹具：web-01/web-02 → g1，db-01 → g2（沿用 scopeTestAPI）。
func approvalTestAPI(t *testing.T) (*API, *ops.Service, *approval.Store, *approval.ConfigStore) {
	t.Helper()
	a, svc := opsTestAPI(t)
	dir := t.TempDir()
	st := approval.NewStore(filepath.Join(dir, "approvals.json"))
	cfg := approval.NewConfigStore(filepath.Join(dir, "approval.json"))
	c := cfg.Get()
	c.Enabled = true
	c.TTLMinutes = 30
	if err := cfg.Set(c); err != nil {
		t.Fatalf("开启审批失败: %v", err)
	}
	a.SetApproval(st, cfg)
	return a, svc, st, cfg
}

// approvalPrincipal 构造带用户名的授权身份。
//
// **必须同时设 principal 与 authUser 两个 context**：`AuthenticatedUser` 读的是后者，
// 只设 principal 会让申请人/审批人都变成空串——那样"不可自批"会**因为错误的原因**通过
// （两边都是空串，撞上"申请人==审批人"），测出来的是一条假判据。
func approvalPrincipal(user string, global bool, perms ...string) *auth.Principal {
	set := make(map[string]struct{}, len(perms))
	for _, p := range perms {
		set[p] = struct{}{}
	}
	scope := auth.Scope{Mode: auth.ScopeRestricted, Groups: []string{"g1"}}
	if global {
		scope = auth.Scope{Mode: auth.ScopeGlobal}
	}
	return &auth.Principal{Username: user, Permissions: set, Scope: scope}
}

func approvalReq(p *auth.Principal, method, target, body string) *http.Request {
	r := reqWith(p, method, target, body)
	return r.WithContext(context.WithValue(r.Context(), authUserContextKey{}, p.Username))
}

// submit 提交一次写类 ops 动作（默认走审批），返回响应。
func submit(t *testing.T, mux *http.ServeMux, p *auth.Principal, kind, node string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, approvalReq(p, http.MethodPost, "/api/v1/ops/tasks",
		`{"node":"`+node+`","kind":"`+kind+`","params":{"unit":"nginx.service"}}`))
	return rec
}

// **D1 的决定性判据**：只读动作不被拦（否则大家会绕开审批走别的入口）。
func TestApproval_ReadOnlyActionNotGated(t *testing.T) {
	a, svc, _, _ := approvalTestAPI(t)
	mux := newRoutesMux(a)
	svc.Store().SaveCaps("web-01", []string{ops.KindNodeDiagnostics})
	p := approvalPrincipal("ops1", false, "ops:exec", "approval:read")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, approvalReq(p, http.MethodPost, "/api/v1/ops/tasks",
		`{"node":"web-01","kind":"node.diagnostics"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("只读动作应直接下发（201），实际 %d：%s", rec.Code, rec.Body.String())
	}
}

// 写类动作在开启审批后返回 202（**不是 201**），且**不创建任务**。
func TestApproval_WriteActionGatedAndNoTaskYet(t *testing.T) {
	a, svc, _, _ := approvalTestAPI(t)
	mux := newRoutesMux(a)
	svc.Store().SaveCaps("web-01", []string{ops.KindSvcRestart})
	p := approvalPrincipal("ops1", false, "ops:exec", "approval:read")

	rec := submit(t, mux, p, ops.KindSvcRestart, "web-01")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("需要审批时应 202，实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	if body["pending"] != true {
		t.Fatalf("响应应标明 pending，实际 %v", body)
	}
	// 关键：此刻**没有**任何任务被创建——"进了审批"绝不能等同于"已下发"
	if got := len(svc.Store().List(ops.ListFilter{})); got != 0 {
		t.Fatalf("进入审批后不该创建任务，实际 %d 条", got)
	}
}

// **决定性判据（验证矩阵用例 7-07）**：申请人不可批准本人任务。
func TestApproval_SelfApprovalForbidden(t *testing.T) {
	a, svc, _, _ := approvalTestAPI(t)
	mux := newRoutesMux(a)
	svc.Store().SaveCaps("web-01", []string{ops.KindSvcRestart})
	requester := approvalPrincipal("ops1", false, "ops:exec", "approval:read", "approval:approve")

	rec := submit(t, mux, requester, ops.KindSvcRestart, "web-01")
	id, _ := decodeBody(t, rec)["approval"].(map[string]any)["id"].(string)
	if id == "" {
		t.Fatalf("应返回审批单号：%s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, approvalReq(requester, http.MethodPost, "/api/v1/approvals/"+id+"/approve", `{}`))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("自批应 403，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "不能批准自己") {
		t.Fatalf("提示应说清原因，实际 %s", rec.Body.String())
	}
	// 被拒之后：没有任务、审批单仍是待审批
	if got := len(svc.Store().List(ops.ListFilter{})); got != 0 {
		t.Fatalf("自批被拒后不该有任务，实际 %d 条", got)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, approvalReq(requester, http.MethodGet, "/api/v1/approvals/"+id, ""))
	got := decodeBody(t, rec)["approval"].(map[string]any)
	if got["state"] != approval.StatePending {
		t.Fatalf("自批被拒后应仍为 pending，实际 %v", got["state"])
	}
}

// **决定性判据（用例 7-07）**：超时自动失效，且**动作不执行**。
func TestApproval_ExpiredCannotBeApprovedAndNeverRuns(t *testing.T) {
	a, svc, st, _ := approvalTestAPI(t)
	mux := newRoutesMux(a)
	svc.Store().SaveCaps("web-01", []string{ops.KindSvcRestart})
	requester := approvalPrincipal("ops1", false, "ops:exec", "approval:read")
	approver := approvalPrincipal("ops2", false, "ops:exec", "approval:read", "approval:approve")

	rec := submit(t, mux, requester, ops.KindSvcRestart, "web-01")
	id := decodeBody(t, rec)["approval"].(map[string]any)["id"].(string)

	// 把存储的时钟推到有效期之后（用注入时钟，不靠 sleep）
	st.SetNow(func() int64 { return time.Now().Add(31 * time.Minute).UnixMilli() })

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, approvalReq(approver, http.MethodPost, "/api/v1/approvals/"+id+"/approve", `{}`))
	if rec.Code != http.StatusConflict {
		t.Fatalf("超时后批准应 409，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if got := len(svc.Store().List(ops.ListFilter{})); got != 0 {
		t.Fatalf("超时的审批绝不能执行动作，实际创建了 %d 条任务", got)
	}
}

// **决定性判据（用例 7-07）**：审计记录审批链（提交 → 批准 → 落到哪个任务）。
func TestApproval_AuditChain(t *testing.T) {
	a, svc, _, _ := approvalTestAPI(t)
	mux := newRoutesMux(a)
	svc.Store().SaveCaps("web-01", []string{ops.KindSvcRestart})
	requester := approvalPrincipal("ops1", false, "ops:exec", "approval:read")
	approver := approvalPrincipal("ops2", false, "ops:exec", "approval:read", "approval:approve")

	rec := submit(t, mux, requester, ops.KindSvcRestart, "web-01")
	id := decodeBody(t, rec)["approval"].(map[string]any)["id"].(string)

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, approvalReq(approver, http.MethodPost, "/api/v1/approvals/"+id+"/approve", `{"note":"已确认"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("他人批准应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}

	events := a.audit.ListFiltered(50, "", "", "approval")
	acts := map[string]string{}
	for _, e := range events {
		acts[e.Action] = e.Detail
	}
	if _, ok := acts["create:"+approval.EntryOpsWrite]; !ok {
		t.Fatalf("应有提交审批的审计，实际 %v", acts)
	}
	detail, ok := acts["approve"]
	if !ok {
		t.Fatalf("应有批准的审计，实际 %v", acts)
	}
	// 批准那条要能把审批单与 ops 任务号串起来——否则事后只看到"批了"，不知道批的是哪次执行
	if !strings.Contains(detail, id) || !strings.Contains(detail, "已下发任务") {
		t.Fatalf("批准审计应串起审批单与任务号，实际 %q", detail)
	}
}

// 批准后**自动执行**，且 ops 任务里的 Operator 是**申请人**（不是审批人）。
func TestApproval_ApprovedExecutesAsRequester(t *testing.T) {
	a, svc, _, _ := approvalTestAPI(t)
	mux := newRoutesMux(a)
	svc.Store().SaveCaps("web-01", []string{ops.KindSvcRestart})
	requester := approvalPrincipal("ops1", false, "ops:exec", "approval:read")
	approver := approvalPrincipal("ops2", false, "ops:exec", "approval:read", "approval:approve")

	rec := submit(t, mux, requester, ops.KindSvcRestart, "web-01")
	id := decodeBody(t, rec)["approval"].(map[string]any)["id"].(string)

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, approvalReq(approver, http.MethodPost, "/api/v1/approvals/"+id+"/approve", `{}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("批准应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	got := decodeBody(t, rec)["approval"].(map[string]any)
	if got["state"] != approval.StateExecuted {
		t.Fatalf("批准后应立即执行并落成 executed，实际 %v（%v）", got["state"], got["message"])
	}
	tasks := svc.Store().List(ops.ListFilter{})
	if len(tasks) != 1 {
		t.Fatalf("批准后应创建 1 条任务，实际 %d 条", len(tasks))
	}
	// 动作是申请人要做的，审批人只是同意：任务归属必须是申请人
	if tasks[0].Operator != "ops1" {
		t.Fatalf("任务应记在申请人名下，实际 %q", tasks[0].Operator)
	}
	if tasks[0].ID != got["taskId"] {
		t.Fatalf("审批单里的任务号应与实际任务一致，实际 %v vs %s", got["taskId"], tasks[0].ID)
	}
}

// 批准需要**两个**权限：approval:approve + 该动作本身的权限点。
func TestApproval_ApproveNeedsUnderlyingPermission(t *testing.T) {
	a, svc, _, _ := approvalTestAPI(t)
	mux := newRoutesMux(a)
	svc.Store().SaveCaps("web-01", []string{ops.KindSvcRestart})
	requester := approvalPrincipal("ops1", false, "ops:exec", "approval:read")
	// 只有 approval:approve，没有 ops:exec：不该能批准一个自己都做不到的动作
	weak := approvalPrincipal("ops2", false, "approval:read", "approval:approve")

	rec := submit(t, mux, requester, ops.KindSvcRestart, "web-01")
	id := decodeBody(t, rec)["approval"].(map[string]any)["id"].(string)

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, approvalReq(weak, http.MethodPost, "/api/v1/approvals/"+id+"/approve", `{}`))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("缺少动作本身权限应 403，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if got := len(svc.Store().List(ops.ListFilter{})); got != 0 {
		t.Fatalf("被拒的批准不该执行动作，实际 %d 条任务", got)
	}
}

// 关闭开关后行为与今天完全一致（回归）：直接 201，不产生审批单。
func TestApproval_DisabledBehavesAsBefore(t *testing.T) {
	a, svc, st, cfg := approvalTestAPI(t)
	mux := newRoutesMux(a)
	svc.Store().SaveCaps("web-01", []string{ops.KindSvcRestart})
	c := cfg.Get()
	c.Enabled = false
	if err := cfg.Set(c); err != nil {
		t.Fatalf("关闭审批失败: %v", err)
	}
	p := approvalPrincipal("ops1", false, "ops:exec")

	rec := submit(t, mux, p, ops.KindSvcRestart, "web-01")
	if rec.Code != http.StatusCreated {
		t.Fatalf("关闭审批后应直接下发（201），实际 %d：%s", rec.Code, rec.Body.String())
	}
	if n := len(st.List(approval.Filter{})); n != 0 {
		t.Fatalf("关闭时不该产生审批单，实际 %d 条", n)
	}
}

// 批量下发也要拦：只拦单条、放过批量，等于给审批留了一条 200 台的旁路。
func TestApproval_BatchAlsoGated(t *testing.T) {
	a, svc, _, _ := approvalTestAPI(t)
	mux := newRoutesMux(a)
	svc.Store().SaveCaps("web-01", []string{ops.KindSvcRestart})
	svc.Store().SaveCaps("web-02", []string{ops.KindSvcRestart})
	p := approvalPrincipal("ops1", false, "ops:exec", "approval:read")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, approvalReq(p, http.MethodPost, "/api/v1/ops/tasks/batch",
		`{"nodes":["web-01","web-02"],"kind":"svc.restart","params":{"unit":"nginx.service"}}`))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("批量写动作应 202，实际 %d：%s", rec.Code, rec.Body.String())
	}
	got := decodeBody(t, rec)["approval"].(map[string]any)
	nodes, _ := got["nodes"].([]any)
	if len(nodes) != 2 {
		t.Fatalf("审批单应覆盖全部目标节点，实际 %v", got["nodes"])
	}
	if len(svc.Store().List(ops.ListFilter{})) != 0 {
		t.Fatal("进入审批后不该创建任务")
	}
}

// 资源范围：范围外的审批单对这个人等于不存在（列表看不见、直接调也 404）。
func TestApproval_ScopeEnforced(t *testing.T) {
	a, svc, _, _ := approvalTestAPI(t)
	mux := newRoutesMux(a)
	svc.Store().SaveCaps("db-01", []string{ops.KindSvcRestart})
	requester := approvalPrincipal("ops1", true, "ops:exec", "approval:read") // 全局，能提交 db-01

	rec := submit(t, mux, requester, ops.KindSvcRestart, "db-01")
	id := decodeBody(t, rec)["approval"].(map[string]any)["id"].(string)

	// 只管 g1 的审批人：既看不到这条，也批不了
	g1Approver := approvalPrincipal("ops2", false, "ops:exec", "approval:read", "approval:approve")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, approvalReq(g1Approver, http.MethodGet, "/api/v1/approvals?box=pending", ""))
	if n := len(decodeBody(t, rec)["approvals"].([]any)); n != 0 {
		t.Fatalf("范围外节点（db-01→g2）的审批不该出现在 g1 审批人的待办里，实际 %d 条", n)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, approvalReq(g1Approver, http.MethodPost, "/api/v1/approvals/"+id+"/approve", `{}`))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("范围外审批应 404（不泄露存在性），实际 %d：%s", rec.Code, rec.Body.String())
	}
}

// 驳回必须写理由；撤回只有申请人本人。
func TestApproval_RejectNeedsNoteAndCancelOnlyRequester(t *testing.T) {
	a, svc, _, _ := approvalTestAPI(t)
	mux := newRoutesMux(a)
	svc.Store().SaveCaps("web-01", []string{ops.KindSvcRestart})
	requester := approvalPrincipal("ops1", false, "ops:exec", "approval:read")
	approver := approvalPrincipal("ops2", false, "ops:exec", "approval:read", "approval:approve")

	rec := submit(t, mux, requester, ops.KindSvcRestart, "web-01")
	id := decodeBody(t, rec)["approval"].(map[string]any)["id"].(string)

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, approvalReq(approver, http.MethodPost, "/api/v1/approvals/"+id+"/reject", `{}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("驳回不写理由应 400，实际 %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, approvalReq(approver, http.MethodPost, "/api/v1/approvals/"+id+"/cancel", `{}`))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("非申请人撤回应 404（不暴露别人的单子），实际 %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, approvalReq(requester, http.MethodPost, "/api/v1/approvals/"+id+"/cancel", `{}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("申请人撤回应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	got := decodeBody(t, rec)["approval"].(map[string]any)
	if got["state"] != approval.StateCancelled {
		t.Fatalf("撤回应落成 cancelled，实际 %v", got["state"])
	}
}

// 配置：保存即热生效、写审计；不认识的入口必须拒（静默接受会让管理员以为"配上了"）。
func TestApproval_ConfigSave(t *testing.T) {
	a, _, _, _ := approvalTestAPI(t)
	mux := newRoutesMux(a)
	admin := approvalPrincipal("admin", true, "approval:read", "approval:manage")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, approvalReq(admin, http.MethodPut, "/api/v1/approvals/config",
		`{"enabled":true,"requireFor":["ops:write","no-such-entry"],"ttlMinutes":15}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("未知入口应 400，实际 %d：%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, approvalReq(admin, http.MethodPut, "/api/v1/approvals/config",
		`{"enabled":true,"requireFor":["ops:write"],"ttlMinutes":15}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("保存应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	got := decodeBody(t, rec)["config"].(map[string]any)
	if got["ttlMinutes"].(float64) != 15 {
		t.Fatalf("应保存 ttlMinutes=15，实际 %v", got["ttlMinutes"])
	}
	// 开关本身要留痕：事后要能回答"审批是什么时候被谁关掉的"
	events := a.audit.ListFiltered(20, "", "", "approval")
	found := false
	for _, e := range events {
		if e.Action == "config" {
			found = true
		}
	}
	if !found {
		t.Fatal("改配置应写审计")
	}
}

// 改配置要 approval:manage：只有 approval:read 的身份改不了（关掉护栏是另一回事）。
func TestApproval_ConfigNeedsManagePermission(t *testing.T) {
	a, _, _, _ := approvalTestAPI(t)
	mux := newRoutesMux(a)
	p := approvalPrincipal("ops1", false, "approval:read")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, approvalReq(p, http.MethodPut, "/api/v1/approvals/config", `{"enabled":false}`))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("缺少 approval:manage 应 403，实际 %d", rec.Code)
	}
}
