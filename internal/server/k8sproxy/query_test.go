package k8sproxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeLiveConfig 写一份只有一个集群的 k8s-live.yaml，指向给定的 server 地址。
func writeLiveConfig(t *testing.T, server string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "k8s-live.yaml")
	raw := "clusters:\n  - name: k3s-dev\n    apiServer: \"" + server + "\"\n"
	if err := os.WriteFile(p, []byte(raw), 0o600); err != nil {
		t.Fatalf("写 k8s-live.yaml 失败: %v", err)
	}
	return p
}

// TestQueryLogsDoesNotAskForTextPlain 钉住 2026-10-10 在真集群上撞到的 406。
//
// apiserver 的 `/log` 子资源虽然返回**纯文本**，内容协商却只认 json / yaml / protobuf：
// 送 `Accept: text/plain` 会被顶回来（实测 k3s v1.36.2）——
//
//	{"message":"only the following media types are accepted: application/json, ...","code":406}
//
// 而 httptest 不做内容协商，这里怎么写都"能过"。所以这条断言是这通路上唯一的防线：
// 退回去不会让任何测试变红，只会在真集群上整页日志拉不出来。
func TestQueryLogsDoesNotAskForTextPlain(t *testing.T) {
	var gotAccept, gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept, gotPath, gotQuery = r.Header.Get("Accept"), r.URL.Path, r.URL.RawQuery
		_, _ = w.Write([]byte("line-1\nline-2\n"))
	}))
	defer srv.Close()

	m := New(writeLiveConfig(t, srv.URL))
	res, err := m.QueryLogs(context.Background(), "k3s-dev", "default", "web-1", "app", 200, 3600)
	if err != nil {
		t.Fatalf("QueryLogs 失败: %v", err)
	}

	if strings.Contains(gotAccept, "text/plain") {
		t.Fatalf("Accept 不能要 text/plain（apiserver 的 /log 会回 406），实际: %q", gotAccept)
	}
	if gotPath != "/api/v1/namespaces/default/pods/web-1/log" {
		t.Fatalf("请求路径不对: %s", gotPath)
	}
	for _, want := range []string{"tailLines=200", "sinceSeconds=3600", "container=app"} {
		if !strings.Contains(gotQuery, want) {
			t.Fatalf("查询参数缺 %s: %s", want, gotQuery)
		}
	}
	// 纯文本正文要按行进表（不能被当成 JSON 解析）。
	if len(res.Rows) != 2 || len(res.Rows[0]) != 1 || res.Rows[0][0] != "line-1" {
		t.Fatalf("纯文本正文没有按行进表: %+v", res.Rows)
	}
}

// TestZhPodStatus 钉住中文词表的两条边界：常见状态有中文，**未收录的原因原样返回**。
// 后半条是刻意的：猜一个可能译错的中文，比留着英文原文更难排查（K8s 的原因值会随版本新增）。
func TestZhPodStatus(t *testing.T) {
	cases := map[string]string{
		"Running":          "运行中",
		"Pending":          "等待中",
		"CrashLoopBackOff": "崩溃重启中",
		"OOMKilled":        "内存超限",
		"Completed":        "已完成",
		"SomeNewK8sReason": "SomeNewK8sReason", // 未收录：原样返回
		"—":                "—",                // 兜底占位符不能被吞掉
	}
	for in, want := range cases {
		if got := zhPodStatus(in); got != want {
			t.Fatalf("zhPodStatus(%q) = %q，期望 %q", in, got, want)
		}
	}
}

// TestQueryPodsShowsChineseEffectiveStatus 钉住 Pod 列表状态列的两件事：
//  1. 用**有效状态**而不是裸 phase：phase=Running 但容器在 CrashLoopBackOff 的 Pod
//     必须显示成"崩溃重启中"——显示成"运行中"是这一页最不该给出的错误结论；
//  2. 这一列是中文（与工作负载表的"就绪 / 已完成"同一套词表），不再是 kubectl 的英文 STATUS。
func TestQueryPodsShowsChineseEffectiveStatus(t *testing.T) {
	body := `{"items":[
	  {"metadata":{"namespace":"default","name":"web-1"},
	   "spec":{"nodeName":"node-1"},
	   "status":{"phase":"Running","podIP":"10.1.2.3","containerStatuses":[
	     {"name":"app","ready":true,"restartCount":6,"state":{}}]}},
	  {"metadata":{"namespace":"default","name":"crash-demo"},
	   "spec":{"nodeName":"node-1"},
	   "status":{"phase":"Running","containerStatuses":[
	     {"name":"app","state":{"waiting":{"reason":"CrashLoopBackOff"}}}]}}
	]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/namespaces/default/pods" {
			t.Errorf("请求路径不对: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	res, err := New(writeLiveConfig(t, srv.URL)).QueryPods(context.Background(), "k3s-dev", "default")
	if err != nil {
		t.Fatalf("QueryPods 失败: %v", err)
	}
	if len(res.Rows) != 2 {
		t.Fatalf("期望 2 行，得到 %d 行: %+v", len(res.Rows), res.Rows)
	}
	// 列序：命名空间 0 / 名称 1 / 状态 2 / 就绪 3 / 重启 4 / 节点 5 / Pod IP 6
	if got := res.Rows[0][2]; got != "运行中" {
		t.Fatalf("健康 Pod 的状态 = %q，期望 运行中", got)
	}
	if got := res.Rows[0][4]; got != "6" {
		t.Fatalf("重启次数 = %q，期望 6", got)
	}
	if got := res.Rows[1][2]; got != "崩溃重启中" {
		t.Fatalf("phase=Running 但容器 CrashLoopBackOff 的 Pod 状态 = %q，期望 崩溃重启中（不能是 运行中）", got)
	}
}
