package ops

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/nebula/monitor/internal/agent/config"
	"github.com/nebula/monitor/internal/model"
)

// 容器**写**动作在 Agent 侧的三道关：实现是否就位、本机护栏、参数本地复校。
//
// 只读查询**不在这条链路上**：2026-10-10 重规划（ADR-0006）后它们改由 Server 直连
// （k8sproxy / dockerproxy），Agent 既不该声明也不该执行——下面有专门的回归用例钉住这点。

type fakeQuerier struct {
	calls  int
	last   string
	result *model.ContainerQueryResult
	err    error
}

func (f *fakeQuerier) record(what string) (*model.ContainerQueryResult, error) {
	f.calls++
	f.last = what
	return f.result, f.err
}

func (f *fakeQuerier) QueryRestartWorkload(_ context.Context, cluster, ns, kind, name string) (*model.ContainerQueryResult, error) {
	return f.record("restart|" + cluster + "|" + ns + "|" + kind + "|" + name)
}

func (f *fakeQuerier) QueryScaleWorkload(_ context.Context, cluster, ns, kind, name string, replicas int) (*model.ContainerQueryResult, error) {
	return f.record("scale|" + cluster + "|" + ns + "|" + kind + "|" + name + "|" + strconv.Itoa(replicas))
}

func (f *fakeQuerier) QueryDeletePod(_ context.Context, cluster, ns, name string) (*model.ContainerQueryResult, error) {
	return f.record("delete|" + cluster + "|" + ns + "|" + name)
}

func sampleResult(kind string) *model.ContainerQueryResult {
	return &model.ContainerQueryResult{
		Kind: kind, Cluster: "prod-k8s", Namespace: "default",
		Columns: []string{"名称", "状态"},
		Rows:    [][]string{{"web-1", "Running"}},
		Total:   1,
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// 没有注入实现（本机没配集群）时，容器写动作既不该被声明，也不该被执行。
func TestContainerWrite_NotDeclaredWithoutQuerier(t *testing.T) {
	e := newTestExecutor(config.OpsGuards{ContainerWrite: true})
	for _, k := range containerWriteKinds {
		if contains(e.Supported(), k) {
			t.Fatalf("未注入实现时不应声明 %s，实际 %v", k, e.Supported())
		}
	}
	res := e.Execute(model.OpsCommand{ID: "t1", Kind: model.OpsKindContainerRestart,
		Params: map[string]string{"cluster": "c", "namespace": "n", "kind": "Deployment", "name": "web"}})
	if res.State != model.OpsStateFailed || !strings.Contains(res.Message, "未启用容器写操作") {
		t.Fatalf("未注入实现时应明确失败，实际 %+v", res)
	}
}

// 写动作只认 guards.ops.containerWrite（默认关闭）：未开启时既不声明也不执行，
// 且**一次都不能碰 apiserver**。只读总开关放行不代表写放行——那是两次不同的同意。
func TestContainerWrite_GuardDefaultsOff(t *testing.T) {
	f := &fakeQuerier{result: sampleResult(model.OpsKindContainerRestart)}
	e := newTestExecutor(config.OpsGuards{})
	e.SetK8sQuerier(f)

	for _, k := range containerWriteKinds {
		if contains(e.Supported(), k) {
			t.Fatalf("containerWrite 默认关闭时不应声明写动作 %s，实际 %v", k, e.Supported())
		}
	}
	res := e.Execute(model.OpsCommand{ID: "t1", Kind: model.OpsKindContainerRestart,
		Params: map[string]string{"cluster": "c", "namespace": "default", "kind": "Deployment", "name": "web"}})
	if res.State != model.OpsStateFailed || !strings.Contains(res.Message, "containerWrite") {
		t.Fatalf("写护栏关闭时应被拒绝，实际 %+v", res)
	}
	if f.calls != 0 {
		t.Fatalf("护栏拒绝时不应调用实现，实际调用 %d 次", f.calls)
	}
}

// 回归守卫（ADR-0006）：已被 Server 直连取代的只读动作**不得**再被声明或执行。
//
// 这类退化不会报错：能力清单里多一项，中心就会去下发，而 Agent 侧已无实现——
// 结果是任务永远停在"已下发"。所以宁可让这条用例红。
func TestContainerWrite_RemovedReadKindsAreNotSupported(t *testing.T) {
	f := &fakeQuerier{result: sampleResult("container.pods")}
	e := newTestExecutor(config.OpsGuards{})
	e.SetK8sQuerier(f)

	removed := []string{
		"container.workloads", "container.pods", "container.describe",
		"container.events", "container.logs",
	}
	for _, k := range removed {
		if contains(e.Supported(), k) {
			t.Errorf("只读动作 %s 已改由 Server 直连，不该出现在能力清单里", k)
		}
		res := e.Execute(model.OpsCommand{ID: "t-" + k, Kind: k,
			Params: map[string]string{"cluster": "c", "namespace": "n"}})
		if res.State != model.OpsStateFailed || !strings.Contains(res.Message, "不支持该动作") {
			t.Errorf("%s 应被明确回绝，实际 %+v", k, res)
		}
	}
	if f.calls != 0 {
		t.Fatalf("已移除的动作不该被执行，实际调用 %d 次", f.calls)
	}
}

// 写护栏开启后：声明写动作，参数如实传到实现。
func TestContainerWrite_AllowedPassesArgs(t *testing.T) {
	f := &fakeQuerier{result: sampleResult(model.OpsKindContainerScale)}
	e := newTestExecutor(config.OpsGuards{ContainerWrite: true})
	e.SetK8sQuerier(f)

	for _, k := range containerWriteKinds {
		if !contains(e.Supported(), k) {
			t.Fatalf("写护栏开启后应声明 %s，实际 %v", k, e.Supported())
		}
	}
	if res := e.Execute(model.OpsCommand{ID: "t1", Kind: model.OpsKindContainerRestart,
		Params: map[string]string{"cluster": "prod-k8s", "namespace": "default", "kind": "Deployment", "name": "web"}}); res.State != model.OpsStateSucceeded {
		t.Fatalf("应成功，实际 %+v", res)
	}
	if f.last != "restart|prod-k8s|default|Deployment|web" {
		t.Fatalf("参数不符：%q", f.last)
	}
	if res := e.Execute(model.OpsCommand{ID: "t2", Kind: model.OpsKindContainerScale,
		Params: map[string]string{"cluster": "c", "namespace": "n", "kind": "StatefulSet", "name": "db", "replicas": "0"}}); res.State != model.OpsStateSucceeded {
		t.Fatalf("应成功（0 副本是合法取值：等于让服务下线），实际 %+v", res)
	}
	if f.last != "scale|c|n|StatefulSet|db|0" {
		t.Fatalf("副本数应原样下发：%q", f.last)
	}
	if res := e.Execute(model.OpsCommand{ID: "t3", Kind: model.OpsKindContainerDelete,
		Params: map[string]string{"cluster": "c", "namespace": "n", "name": "web-1"}}); res.State != model.OpsStateSucceeded {
		t.Fatalf("应成功，实际 %+v", res)
	}
	if f.last != "delete|c|n|web-1" {
		t.Fatalf("参数不符：%q", f.last)
	}
}

// 写动作的参数复校同样在调用之前完成；命名空间是必填（同名对象可能存在于别的命名空间）。
func TestContainerWrite_LocalParamValidationRunsBeforeQuery(t *testing.T) {
	cases := []struct {
		name   string
		kind   string
		params map[string]string
		wantIn string
	}{
		{"缺命名空间", model.OpsKindContainerRestart,
			map[string]string{"cluster": "c", "kind": "Deployment", "name": "web"}, "必须指定命名空间"},
		{"集群标识含空格", model.OpsKindContainerRestart,
			map[string]string{"cluster": "prod k8s", "namespace": "n", "kind": "Deployment", "name": "web"}, "集群标识不合法"},
		{"命名空间路径穿越", model.OpsKindContainerRestart,
			map[string]string{"cluster": "c", "namespace": "../../etc", "kind": "Deployment", "name": "web"}, "命名空间不合法"},
		{"类型不在白名单", model.OpsKindContainerRestart,
			map[string]string{"cluster": "c", "namespace": "n", "kind": "ReplicaSet", "name": "web"}, "工作负载类型不合法"},
		{"类型为选项注入", model.OpsKindContainerRestart,
			map[string]string{"cluster": "c", "namespace": "n", "kind": "--force", "name": "web"}, "工作负载类型不合法"},
		{"名称路径穿越", model.OpsKindContainerDelete,
			map[string]string{"cluster": "c", "namespace": "n", "name": "../../secrets"}, "Pod 名不合法"},
		{"DaemonSet 不支持扩缩容", model.OpsKindContainerScale,
			map[string]string{"cluster": "c", "namespace": "n", "kind": "DaemonSet", "name": "agent", "replicas": "2"}, "不支持扩缩容"},
		{"副本数非数字", model.OpsKindContainerScale,
			map[string]string{"cluster": "c", "namespace": "n", "kind": "Deployment", "name": "web", "replicas": "many"}, "副本数不合法"},
		{"副本数超上限", model.OpsKindContainerScale,
			map[string]string{"cluster": "c", "namespace": "n", "kind": "Deployment", "name": "web", "replicas": "9999"}, "副本数不合法"},
		{"副本数留空", model.OpsKindContainerScale,
			map[string]string{"cluster": "c", "namespace": "n", "kind": "Deployment", "name": "web"}, "副本数不合法"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeQuerier{result: sampleResult(tc.kind)}
			e := newTestExecutor(config.OpsGuards{ContainerWrite: true})
			e.SetK8sQuerier(f)

			res := e.Execute(model.OpsCommand{ID: "t1", Kind: tc.kind, Params: tc.params})
			if res.State != model.OpsStateFailed || !strings.Contains(res.Message, tc.wantIn) {
				t.Fatalf("应本地拒绝（含 %q），实际 %+v", tc.wantIn, res)
			}
			if f.calls != 0 {
				t.Fatalf("参数不合法时不应调用实现，实际调用 %d 次", f.calls)
			}
		})
	}
}

// 写失败要把 apiserver 的原话带回来（forbidden / not found 这类结论只有它知道）。
func TestContainerWrite_APIServerErrorSurfaces(t *testing.T) {
	f := &fakeQuerier{err: errors.New(`apiserver 返回 403：{"message":"deployments.apps \"web\" is forbidden"}`)}
	e := newTestExecutor(config.OpsGuards{ContainerWrite: true})
	e.SetK8sQuerier(f)

	res := e.Execute(model.OpsCommand{ID: "t1", Kind: model.OpsKindContainerRestart,
		Params: map[string]string{"cluster": "c", "namespace": "n", "kind": "Deployment", "name": "web"}})
	if res.State != model.OpsStateFailed || !strings.Contains(res.Message, "forbidden") {
		t.Fatalf("应把 apiserver 的说明带回来，实际 %+v", res)
	}
}

// 成功路径：Data 给一行摘要、JSON 给结构化载荷，且 JSON 能被反序列化回协议类型。
func TestContainerWrite_SuccessCarriesStructuredPayload(t *testing.T) {
	f := &fakeQuerier{result: sampleResult(model.OpsKindContainerRestart)}
	f.result.Truncated = true
	f.result.Total = 812

	e := newTestExecutor(config.OpsGuards{ContainerWrite: true})
	e.SetK8sQuerier(f)

	res := e.Execute(model.OpsCommand{ID: "t1", Kind: model.OpsKindContainerRestart,
		Params: map[string]string{"cluster": "prod-k8s", "namespace": "default", "kind": "Deployment", "name": "web"}})

	if res.State != model.OpsStateSucceeded {
		t.Fatalf("应成功，实际 %+v", res)
	}
	if !strings.Contains(res.Message, "已截断") {
		t.Fatalf("截断必须说出来，实际 %q", res.Message)
	}
	if res.Data["概览"] == "" {
		t.Fatalf("Data 应给一行摘要，实际 %+v", res.Data)
	}
	var got model.ContainerQueryResult
	if err := json.Unmarshal([]byte(res.JSON), &got); err != nil {
		t.Fatalf("JSON 载荷应可反序列化: %v / %s", err, res.JSON)
	}
	if got.Total != 812 || !got.Truncated || len(got.Rows) != 1 || got.Columns[0] != "名称" {
		t.Fatalf("结构化载荷内容不符，实际 %+v", got)
	}
}

// containerWriteKinds 自检：清单里的每一项都要被判为写动作，且只有这三项。
func TestContainerWrite_KindsAreConsistent(t *testing.T) {
	if len(containerWriteKinds) != 3 {
		t.Fatalf("容器写动作应为 3 项（重启 / 扩缩容 / 删 Pod），实际 %v", containerWriteKinds)
	}
	for _, k := range containerWriteKinds {
		if !containerWriteKind(k) {
			t.Fatalf("写清单自检失败：%s", k)
		}
		if strings.TrimSpace(k) == "" {
			t.Fatal("写清单里出现空 kind")
		}
	}
	if containerWriteKind(model.OpsKindSvcRestart) {
		t.Fatal("svc.restart 不是容器写动作，不应被判为真")
	}
}
