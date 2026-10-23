package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 2026-10-23：K8s 的读与写动作都改由 Server 直连（Agent 只负责采集），
// 因此下行动作目录里**不该**再有 container.*。
//
// 这条用例守的是"没人能创建一条永远不会被执行的下行任务"：目录里若重新登记了
// container.*，这里拿到的会是 403 / 201，而不是 400「未知动作」。
// 写权限点（container:write，仅超管）的校验现在在直连接口上，见 k8s_write_api_test.go。
func TestOpsTasks_RejectsContainerKinds(t *testing.T) {
	a, _ := opsTestAPI(t)
	mux := newRoutesMux(a)

	for _, kind := range []string{"container.delete", "container.restart", "container.scale", "container.pods"} {
		body := `{"node":"web-01","kind":"` + kind + `","params":{"cluster":"k3s-dev","namespace":"default","name":"web-1"}}`
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, reqWith(globalPrincipal("ops:exec", "container:write"), http.MethodPost, "/api/v1/ops/tasks", body))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s 不在下行目录里，应 400，实际 %d：%s", kind, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "未知动作") {
			t.Fatalf("%s 的拒绝理由应是未知动作，实际 %s", kind, rec.Body.String())
		}
	}
}
