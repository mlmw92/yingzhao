package ops

import (
	"strings"
	"testing"

	"github.com/nebula/monitor/internal/model"
)

// K8s 排障写动作的目录形态：非只读、归入「容器操作」分组、带 container:write 域权限点。
//
// 与 Docker 写动作同一取向：另开分组（「容器」分组被既有守卫断言为只读），
// 写动作必须在界面上一眼可分（界面顺序即风险顺序）。
func TestCatalog_ContainerWriteActions(t *testing.T) {
	want := map[string][]string{
		model.OpsKindContainerRestart: {"cluster", "namespace", "kind", "name"},
		model.OpsKindContainerScale:   {"cluster", "namespace", "kind", "name", "replicas"},
		model.OpsKindContainerDelete:  {"cluster", "namespace", "name"},
	}
	for kind, params := range want {
		a, ok := Lookup(kind)
		if !ok {
			t.Fatalf("动作目录应登记 %s", kind)
		}
		if a.ReadOnly {
			t.Errorf("%s 必须是写动作", kind)
		}
		if a.Group != "容器操作" {
			t.Errorf("%s 应归入「容器操作」分组，实际 %q", kind, a.Group)
		}
		if a.Perm != PermContainerWrite {
			t.Errorf("%s 应要求 %s，实际 %q", kind, PermContainerWrite, a.Perm)
		}
		got := map[string]bool{}
		for _, p := range a.Params {
			got[p.Name] = true
		}
		for _, name := range params {
			if !got[name] {
				t.Errorf("%s 应声明参数 %s", kind, name)
			}
		}
	}
	// 写动作与只读动作必须落在不同分组。
	for _, a := range Catalog() {
		if a.Group == "容器" && !a.ReadOnly {
			t.Errorf("「容器」分组内不得出现写动作：%s", a.Kind)
		}
		if a.Group == "容器操作" && a.ReadOnly {
			t.Errorf("「容器操作」分组内不得出现只读动作：%s", a.Kind)
		}
	}
}

// 「容器操作」必须排在只读分组之后：界面上先看只读的，再看能改的。
//
// 2026-10-10 重规划后只读分组只剩「节点」「服务」（「容器」随只读动作一起删除），
// 因此这里对着这两个断言——比原来只对一个更严。
func TestCatalog_ContainerWriteGroupComesAfterReadGroups(t *testing.T) {
	rank := map[string]int{}
	for i, g := range groupOrder {
		rank[g] = i
	}
	for _, g := range []string{"节点", "服务"} {
		if rank["容器操作"] <= rank[g] {
			t.Fatalf("「容器操作」应排在只读分组 %s 之后，实际 groupOrder=%v", g, groupOrder)
		}
	}
}

// 写动作的参数必须真的校验：类型 / 副本数 / 名称都在这一层被拒。
func TestValidate_ContainerWriteActions(t *testing.T) {
	cases := []struct {
		name   string
		kind   string
		params map[string]string
		wantOK bool
	}{
		{"重启缺命名空间", model.OpsKindContainerRestart,
			map[string]string{"cluster": "c", "kind": "Deployment", "name": "web"}, false},
		{"重启类型非法", model.OpsKindContainerRestart,
			map[string]string{"cluster": "c", "namespace": "n", "kind": "replicaset", "name": "web"}, false},
		{"重启合法", model.OpsKindContainerRestart,
			map[string]string{"cluster": "c", "namespace": "n", "kind": "Deployment", "name": "web"}, true},
		{"扩缩容 DaemonSet 非法", model.OpsKindContainerScale,
			map[string]string{"cluster": "c", "namespace": "n", "kind": "DaemonSet", "name": "a", "replicas": "2"}, false},
		{"扩缩容副本数为 0 合法", model.OpsKindContainerScale,
			map[string]string{"cluster": "c", "namespace": "n", "kind": "Deployment", "name": "web", "replicas": "0"}, true},
		{"扩缩容副本数非数字", model.OpsKindContainerScale,
			map[string]string{"cluster": "c", "namespace": "n", "kind": "Deployment", "name": "web", "replicas": "x"}, false},
		{"删 Pod 名称含斜杠", model.OpsKindContainerDelete,
			map[string]string{"cluster": "c", "namespace": "n", "name": "a/b"}, false},
		{"删 Pod 合法", model.OpsKindContainerDelete,
			map[string]string{"cluster": "c", "namespace": "n", "name": "web-1"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := Validate(tc.kind, tc.params)
			if tc.wantOK && err != nil {
				t.Fatalf("应通过校验，实际 %v", err)
			}
			if !tc.wantOK && err == nil {
				t.Fatal("应被拒绝，实际通过")
			}
		})
	}
}

// 副本数上限必须真的生效（一次误填 99999 就是把节点资源全占满）。
func TestValidate_ContainerScaleRejectsHugeReplicas(t *testing.T) {
	_, _, err := Validate(model.OpsKindContainerScale, map[string]string{
		"cluster": "c", "namespace": "n", "kind": "Deployment", "name": "web", "replicas": "9999",
	})
	if err == nil {
		t.Fatal("超出上限的副本数应被拒绝")
	}
	if !strings.Contains(err.Error(), "replicas") && !strings.Contains(err.Error(), "副本数") {
		t.Fatalf("错误信息应指明是哪个参数，实际 %v", err)
	}
}
