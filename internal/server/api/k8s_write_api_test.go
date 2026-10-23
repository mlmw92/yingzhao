package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mlmw92/yingzhao/internal/server/k8sproxy"
)

// 2026-10-23：K8s 写操作改由 Server 直连 apiserver（Agent 只负责采集）。
// 这组用例钉住「接口层」的三件事：权限、参数白名单、请求确实打到了 apiserver。

// k8sWriteTestAPI 注入一个指向假 apiserver 的 k8sproxy。
func k8sWriteTestAPI(t *testing.T, server string) *API {
	t.Helper()
	a := scopeTestAPI(t)
	p := filepath.Join(t.TempDir(), "k8s-live.yaml")
	if err := os.WriteFile(p, []byte("clusters:\n  - name: k3s-dev\n    apiServer: \""+server+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.SetK8sLive(k8sproxy.New(p))
	return a
}

func TestK8sRestartRequiresContainerWrite(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("无权限时不该发出请求: %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()

	a := k8sWriteTestAPI(t, srv.URL)
	rec := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, reqWith(globalPrincipal("container:read"), http.MethodPost,
		"/api/v1/container/k8s/live/k3s-dev/restart?namespace=default&kind=Deployment&name=web", ""))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("没有 container:write 应 403，code = %d", rec.Code)
	}
}

func TestK8sRestartCallsAPIServer(t *testing.T) {
	var mu sync.Mutex
	var method, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		method, path = r.Method, r.URL.Path
		mu.Unlock()
		_, _ = w.Write([]byte(`{"metadata":{"name":"web"}}`))
	}))
	defer srv.Close()

	a := k8sWriteTestAPI(t, srv.URL)
	rec := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, reqWith(globalPrincipal("container:write"), http.MethodPost,
		"/api/v1/container/k8s/live/k3s-dev/restart?namespace=default&kind=Deployment&name=web", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d，body = %s", rec.Code, rec.Body.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if method != http.MethodPatch || path != "/apis/apps/v1/namespaces/default/deployments/web" {
		t.Fatalf("请求不对: %s %s", method, path)
	}
}

func TestK8sDeletePodCallsAPIServer(t *testing.T) {
	var method, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		_, _ = w.Write([]byte(`{"status":"Success"}`))
	}))
	defer srv.Close()

	a := k8sWriteTestAPI(t, srv.URL)
	rec := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, reqWith(globalPrincipal("container:write"), http.MethodDelete,
		"/api/v1/container/k8s/live/k3s-dev/pod?namespace=default&name=web-1", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d，body = %s", rec.Code, rec.Body.String())
	}
	if method != http.MethodDelete || path != "/api/v1/namespaces/default/pods/web-1" {
		t.Fatalf("请求不对: %s %s", method, path)
	}
}

// 参数白名单：这些值会被拼进 apiserver 的路径，放斜杠进去等于改请求路径。
func TestK8sWriteRejectsBadParams(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("参数不合法时不该发出请求: %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()

	a := k8sWriteTestAPI(t, srv.URL)
	cases := []struct{ method, target string }{
		{http.MethodPost, "/api/v1/container/k8s/live/k3s-dev/restart?namespace=../x&kind=Deployment&name=web"},
		{http.MethodPost, "/api/v1/container/k8s/live/k3s-dev/restart?namespace=default&kind=Job&name=web"},
		{http.MethodPost, "/api/v1/container/k8s/live/k3s-dev/restart?kind=Deployment&name=web"}, // 缺命名空间
		{http.MethodPost, "/api/v1/container/k8s/live/k3s-dev/scale?namespace=default&kind=Deployment&name=web&replicas=9999"},
		{http.MethodDelete, "/api/v1/container/k8s/live/k3s-dev/pod?namespace=default&name=a/b"},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		newRoutesMux(a).ServeHTTP(rec, reqWith(globalPrincipal("container:write"), tc.method, tc.target, ""))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s 应 400，实际 %d", tc.target, rec.Code)
		}
	}
}

// 未配置 k8s-live.yaml 时明确回 501，而不是假装成功。
func TestK8sWriteWithoutLiveConfig(t *testing.T) {
	a := scopeTestAPI(t) // 未注入 k8sLive
	rec := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, reqWith(globalPrincipal("container:write"), http.MethodPost,
		"/api/v1/container/k8s/live/k3s-dev/restart?namespace=default&kind=Deployment&name=web", ""))
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("code = %d，want 501", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "k8s-live.yaml") {
		t.Fatalf("错误信息应指出缺少 k8s-live.yaml：%s", rec.Body.String())
	}
}
