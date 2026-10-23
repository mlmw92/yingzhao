package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/mlmw92/yingzhao/internal/model"
	"github.com/mlmw92/yingzhao/internal/server/approval"
)

// 2026-10-23：Docker 写操作接入双人复核（与 K8s 写操作同一形态）。
//
// 这里钉住两件事：
//  1. 开启审批时，写接口返回 202 + pending，且**不直连 daemon**；
//  2. 批准后由 Server 按审批单里的参数直连执行，打到的正是 daemon 的对应接口。

// dockerFakeDaemon 起一个假 daemon，记录收到的请求。
func dockerFakeDaemon(t *testing.T) (*httptest.Server, func() (string, string)) {
	t.Helper()
	var mu sync.Mutex
	var method, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		method, path = r.Method, r.URL.Path
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	return srv, func() (string, string) {
		mu.Lock()
		defer mu.Unlock()
		return method, path
	}
}

// registerDockerEndpoint 把假 daemon 注册成 web-01 上的端点。
func registerDockerEndpoint(t *testing.T, srv *httptest.Server) {
	t.Helper()
	setDockerEndpoints(t, "web-01", []model.DockerEndpoint{
		{
			Node: "web-01", Name: "local-docker", Reachable: true,
			Addr: "tcp://" + strings.TrimPrefix(srv.URL, "http://"),
		},
	})
}

// 开启审批时：写接口回 202 + pending，且 daemon 一次都没被调用。
func TestDockerWrite_RequiresApproval(t *testing.T) {
	srv, gotReq := dockerFakeDaemon(t)
	registerDockerEndpoint(t, srv)

	a, _, st, _ := approvalTestAPI(t)
	mux := newRoutesMux(a)
	requester := approvalPrincipal("ops1", true, "docker:write", "approval:read")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, approvalReq(requester, http.MethodPost,
		"/api/v1/container/docker/containers/web-1/stop?node=web-01&name=local-docker&timeout=20&reason=排障", ""))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("需要审批时应 202，实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	if body["pending"] != true {
		t.Fatalf("响应应标明 pending，实际 %v", body)
	}
	if method, path := gotReq(); method != "" || path != "" {
		t.Fatalf("进入审批后不该直连 daemon，实际 %s %s", method, path)
	}

	list := st.List(approval.Filter{State: approval.StatePending})
	if len(list) != 1 {
		t.Fatalf("应产生 1 条待审批单，实际 %d 条", len(list))
	}
	req := list[0]
	if req.Kind != approval.EntryDockerWrite {
		t.Fatalf("审批单入口 = %q，want %q", req.Kind, approval.EntryDockerWrite)
	}
	if req.Node != "web-01" {
		t.Fatalf("审批单目标节点 = %q，want web-01", req.Node)
	}
	if req.Reason != "排障" {
		t.Fatalf("审批单理由 = %q，want 排障", req.Reason)
	}
	// 批准后要靠这些参数原样执行：少一个就执行不出来。
	want := map[string]string{
		"action": "stop", "node": "web-01", "endpoint": "local-docker",
		"id": "web-1", "timeout": "20",
	}
	for k, v := range want {
		if req.Params[k] != v {
			t.Fatalf("审批单参数 %s = %q，want %q（全部：%v）", k, req.Params[k], v, req.Params)
		}
	}
}

// 批准 Docker 写操作需要动作本身的 docker:write：只会点批准的橡皮图章批不动。
func TestDockerApproval_ApproverNeedsDockerWrite(t *testing.T) {
	srv, _ := dockerFakeDaemon(t)
	registerDockerEndpoint(t, srv)

	a, _, _, _ := approvalTestAPI(t)
	mux := newRoutesMux(a)
	requester := approvalPrincipal("ops1", true, "docker:write", "approval:read")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, approvalReq(requester, http.MethodDelete,
		"/api/v1/container/docker/containers/web-1?node=web-01&name=local-docker", ""))
	id, _ := decodeBody(t, rec)["approval"].(map[string]any)["id"].(string)
	if id == "" {
		t.Fatalf("应返回审批单号：%s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, approvalReq(approvalPrincipal("ops2", true, "approval:approve"),
		http.MethodPost, "/api/v1/approvals/"+id+"/approve", `{}`))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("缺少 docker:write 的审批人应 403，实际 %d：%s", rec.Code, rec.Body.String())
	}
}

// 批准后：Server 直连 daemon 执行，审批单落成已执行。
func TestDockerApprovedWriteExecutes(t *testing.T) {
	srv, gotReq := dockerFakeDaemon(t)
	registerDockerEndpoint(t, srv)

	a, _, st, _ := approvalTestAPI(t)
	mux := newRoutesMux(a)
	requester := approvalPrincipal("ops1", true, "docker:write", "approval:read")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, approvalReq(requester, http.MethodPost,
		"/api/v1/container/docker/containers/web-1/stop?node=web-01&name=local-docker&timeout=20", ""))
	id, _ := decodeBody(t, rec)["approval"].(map[string]any)["id"].(string)
	if id == "" {
		t.Fatalf("应返回审批单号：%s", rec.Body.String())
	}

	// 批准人需要 approval:approve + 动作本身的 docker:write（橡皮图章不能批自己做不到的事）。
	approver := approvalPrincipal("ops2", true, "approval:approve", "docker:write")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, approvalReq(approver, http.MethodPost, "/api/v1/approvals/"+id+"/approve", `{}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("批准应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if body := decodeBody(t, rec); body["error"] != nil {
		t.Fatalf("批准后执行失败：%v", body["error"])
	}

	method, path := gotReq()
	if method != http.MethodPost || path != "/containers/web-1/stop" {
		t.Fatalf("请求不对: %s %s", method, path)
	}
	if got, ok := st.Get(id); !ok || got.State != approval.StateExecuted {
		t.Fatalf("审批单应落成已执行，实际 %+v", got)
	}
}
