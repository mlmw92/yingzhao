package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// K8s 写动作的权限点是 container:write，**不能被 container:read 或入口的 ops:exec 顶替**。
//
// 用户 2026-10-10 拍板：三项写操作（重启工作负载 / 扩缩容 / 删除 Pod）全都只给超管。
// 这是"两道门独立"的第一道：即使目标机器 guards.ops.containerWrite 放行了，
// 没有 container:write 的人依然下不了这条动作。
func TestRoutes_ContainerWriteNeedsContainerWritePermission(t *testing.T) {
	a, _ := opsTestAPI(t)
	mux := newRoutesMux(a)
	body := `{"node":"web-01","kind":"container.delete","params":{"cluster":"k3s-dev","namespace":"default","name":"web-1"}}`

	// 1) 只有 ops:exec → 403，且说明缺哪个权限点
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, reqWith(globalPrincipal("ops:exec"), http.MethodPost, "/api/v1/ops/tasks", body))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("缺 container:write 应 403，code = %d，body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "container:write") {
		t.Fatalf("应说明缺哪个权限点，实际 %s", rec.Body.String())
	}

	// 2) container:read 同样不够——"能看"与"能改"是两个权限点
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, reqWith(globalPrincipal("ops:exec", "container:read"), http.MethodPost, "/api/v1/ops/tasks", body))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("container:read 不得顶替 container:write，code = %d，body = %s", rec.Code, rec.Body.String())
	}

	// 3) 有 container:write 时权限门放行（这里没有 Agent 声明该动作，因此落到 409"节点不支持"，
	//    而**不是** 403——两者混起来就分不清"没权限"和"机器没放行"）
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, reqWith(globalPrincipal("ops:exec", "container:write"), http.MethodPost, "/api/v1/ops/tasks", body))
	if rec.Code == http.StatusForbidden {
		t.Fatalf("有 container:write 不应被权限门拦下，body = %s", rec.Body.String())
	}
}

// 只读的 container.* 动作只要求 container:read，**不要求** container:write——
// 否则"能看"会顺带把"能改"的门槛拉高到不可用。
func TestRoutes_ContainerReadDoesNotNeedWritePermission(t *testing.T) {
	a, _ := opsTestAPI(t)
	mux := newRoutesMux(a)
	body := `{"node":"web-01","kind":"container.pods","params":{"cluster":"k3s-dev"}}`

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, reqWith(globalPrincipal("ops:exec", "container:read"), http.MethodPost, "/api/v1/ops/tasks", body))
	if rec.Code == http.StatusForbidden {
		t.Fatalf("只读动作不该要求 container:write，body = %s", rec.Body.String())
	}
}
