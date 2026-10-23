package k8sproxy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 2026-10-23：K8s 写操作从「Agent 异步下行」改为「Server 直连 apiserver」。
// 这组用例钉住三个请求的形状——apiserver 对方法、路径与 Content-Type 都很挑。

func TestRestartWorkloadPatchesTemplateAnnotation(t *testing.T) {
	var gotMethod, gotPath, gotCT, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotCT = r.Method, r.URL.Path, r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = w.Write([]byte(`{"metadata":{"name":"web"}}`))
	}))
	defer srv.Close()

	m := New(writeLiveConfig(t, srv.URL))
	res, err := m.RestartWorkload(context.Background(), "k3s-dev", "default", "Deployment", "web")
	if err != nil {
		t.Fatalf("RestartWorkload: %v", err)
	}
	if gotMethod != http.MethodPatch {
		t.Fatalf("method = %s，want PATCH", gotMethod)
	}
	if gotPath != "/apis/apps/v1/namespaces/default/deployments/web" {
		t.Fatalf("path = %s", gotPath)
	}
	if gotCT != "application/merge-patch+json" {
		t.Fatalf("Content-Type = %q", gotCT)
	}
	if !strings.Contains(gotBody, "kubectl.kubernetes.io/restartedAt") {
		t.Fatalf("补丁里没有 restartedAt 注解: %s", gotBody)
	}
	if res.Kind != "container.restart" {
		t.Fatalf("result kind = %q", res.Kind)
	}
}

func TestScaleWorkloadPatchesScaleSubresource(t *testing.T) {
	var gotMethod, gotPath, gotCT, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotCT = r.Method, r.URL.Path, r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = w.Write([]byte(`{"spec":{"replicas":3}}`))
	}))
	defer srv.Close()

	m := New(writeLiveConfig(t, srv.URL))
	res, err := m.ScaleWorkload(context.Background(), "k3s-dev", "default", "StatefulSet", "db", 3)
	if err != nil {
		t.Fatalf("ScaleWorkload: %v", err)
	}
	if gotMethod != http.MethodPatch {
		t.Fatalf("method = %s，want PATCH", gotMethod)
	}
	// 扩缩容必须走 /scale 子资源，而不是改整个对象。
	if gotPath != "/apis/apps/v1/namespaces/default/statefulsets/db/scale" {
		t.Fatalf("path = %s", gotPath)
	}
	if gotCT != "application/merge-patch+json" {
		t.Fatalf("Content-Type = %q", gotCT)
	}
	var body struct {
		Spec struct {
			Replicas *int `json:"replicas"`
		} `json:"spec"`
	}
	if err := json.Unmarshal([]byte(gotBody), &body); err != nil || body.Spec.Replicas == nil || *body.Spec.Replicas != 3 {
		t.Fatalf("补丁体不对: %s (%v)", gotBody, err)
	}
	if res.Kind != "container.scale" {
		t.Fatalf("result kind = %q", res.Kind)
	}
}

func TestDeletePodDeletes(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		_, _ = w.Write([]byte(`{"kind":"Status","status":"Success"}`))
	}))
	defer srv.Close()

	m := New(writeLiveConfig(t, srv.URL))
	res, err := m.DeletePod(context.Background(), "k3s-dev", "default", "web-1")
	if err != nil {
		t.Fatalf("DeletePod: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Fatalf("method = %s，want DELETE", gotMethod)
	}
	if gotPath != "/api/v1/namespaces/default/pods/web-1" {
		t.Fatalf("path = %s", gotPath)
	}
	if res.Kind != "container.delete" {
		t.Fatalf("result kind = %q", res.Kind)
	}
}

// DaemonSet 不支持扩缩容（副本数由节点数决定），必须当场拒绝而不是发一个注定失败的请求。
func TestScaleWorkloadRejectsDaemonSet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("不该发出请求: %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()

	m := New(writeLiveConfig(t, srv.URL))
	if _, err := m.ScaleWorkload(context.Background(), "k3s-dev", "default", "DaemonSet", "agent", 2); err == nil {
		t.Fatal("DaemonSet 扩缩容应报错")
	}
}

// apiserver 返回 403 时要带出原始信息：排障时"权限不够"和"参数写错"必须能分清。
func TestWriteSurfacesAPIServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"deployments.apps \"web\" is forbidden"}`))
	}))
	defer srv.Close()

	m := New(writeLiveConfig(t, srv.URL))
	_, err := m.RestartWorkload(context.Background(), "k3s-dev", "default", "Deployment", "web")
	if err == nil || !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "forbidden") {
		t.Fatalf("错误信息不对: %v", err)
	}
}
