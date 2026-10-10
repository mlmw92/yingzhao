package k8sproxy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestParseKubeconfig kubeconfig 解析必须按 current-context 定位 cluster 与 user，
// 不能按顺序取第一个（多集群 kubeconfig 下会连错集群）。
func TestParseKubeconfig(t *testing.T) {
	yaml := `apiVersion: v1
clusters:
- name: c-a
  cluster:
    server: https://a:6443
    certificate-authority-data: Q0FBLUE=
- name: c-b
  cluster:
    server: https://b:6443
users:
- name: u-a
  user:
    token: tk-a
- name: u-b
  user:
    token: tk-b
    client-certificate-data: Q0VSVA==
    client-key-data: S0VZ
contexts:
- name: ctx-a
  context:
    cluster: c-a
    user: u-a
- name: ctx-b
  context:
    cluster: c-b
    user: u-b
current-context: ctx-b
`
	kc, err := parseKubeconfig([]byte(yaml))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if kc.server != "https://b:6443" || kc.token != "tk-b" {
		t.Fatalf("应取 current-context 对应的 cluster/user，实际 server=%s token=%s", kc.server, kc.token)
	}
	if len(kc.caData) != 0 || string(kc.certData) != "CERT" || string(kc.keyData) != "KEY" {
		t.Fatalf("CA/客户端证书解码不符: %+v", kc)
	}
}

// TestListMasksCredentials List 输出不得携带凭据；Get 才返回完整配置供代理使用。
func TestListMasksCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "k8s-live.yaml")
	content := "clusters:\n- name: k1\n  apiServer: https://x:6443\n  token: tk-secret\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	m := New(path)

	list := m.List()
	if len(list) != 1 || list[0].Name != "k1" {
		t.Fatalf("集群清单不符: %+v", list)
	}
	if list[0].Token != "" {
		t.Fatalf("List 结果不应携带 token")
	}
	if !strings.Contains(m.Path(), "k8s-live.yaml") {
		t.Fatalf("Path 应指向配置文件: %s", m.Path())
	}
	c, ok := m.Get("k1")
	if !ok || c.Token != "tk-secret" {
		t.Fatalf("Get 应返回完整凭据供代理使用: %+v", c)
	}
	// 未配置的集群
	if _, ok := m.Get("no-such"); ok {
		t.Fatalf("未配置的集群不应命中")
	}
}
