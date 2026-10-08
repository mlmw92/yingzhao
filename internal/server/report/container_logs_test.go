package report

import (
	"strings"
	"testing"

	"github.com/nebula/monitor/internal/model"
	"github.com/nebula/monitor/internal/server/asset"
	"github.com/nebula/monitor/internal/server/logstore"
)

// 容器与日志章节（设计件 §4.3 / §4.4）。
//
// 守两件事：
//  1. 「没有能力」不等于「产出空章节」——未注入台账/日志后端时章节不渲染，而不是给一份空表；
//  2. 日志的存储统计是**全局口径**，受限身份下不能展示（那会把范围外的规模透出去）。

// fakeLogStore 是日志后端的替身：报告侧只用 Capability 这一项能力。
type fakeLogStore struct{ cap logstore.Capability }

func (f *fakeLogStore) Append(model.LogBatch) (int, int, string, error) { return 0, 0, "", nil }
func (f *fakeLogStore) Query(model.LogQuery, logstore.Cursor) (model.LogQueryResult, error) {
	return model.LogQueryResult{}, nil
}
func (f *fakeLogStore) Backend() string                   { return f.cap.Backend }
func (f *fakeLogStore) Sources() []string                 { return nil }
func (f *fakeLogStore) FieldNames(string) []string        { return nil }
func (f *fakeLogStore) Capability() logstore.Capability   { return f.cap }

// 未注入 store（库/时序不可用）→ 容器章节不渲染。
func TestCollectContainerWithoutStore(t *testing.T) {
	g := &Generator{}
	if sec := g.collectContainer(scopeAllForTest()); sec != nil {
		t.Fatalf("store 缺失时不应产出容器章节：%+v", sec)
	}
}

// 未注入日志后端 → 日志章节不渲染。
func TestCollectLogsWithoutStore(t *testing.T) {
	g := &Generator{}
	if sec := g.collectLogs(scopeAllForTest()); sec != nil {
		t.Fatalf("未接入日志时不应产出日志章节：%+v", sec)
	}
}

// 全量身份：如实转述后端自述的能力与存储统计。
func TestCollectLogsFullScope(t *testing.T) {
	g := &Generator{logs: &fakeLogStore{cap: logstore.Capability{
		Backend: "local", FullTextIndex: false, FieldIndex: false,
		ScanBudget: &logstore.ScanBudget{Bytes: 64 * 1024 * 1024, Lines: 200000},
		Notes:      []string{"本地后端按天分片"},
		Storage:    logstore.StorageStats{Sources: 3, Nodes: 4, OldestDay: "2026-10-01", NewestDay: "2026-10-08", Bytes: 123456},
	}}}
	sec := g.collectLogs(scopeAllForTest())
	if sec == nil {
		t.Fatal("应产出日志章节")
	}
	if sec.Backend != "local" || sec.ScanBudgetMB != 64 || sec.ScanBudgetLines != 200000 {
		t.Fatalf("能力转述不符：%+v", sec)
	}
	if sec.Sources != 3 || sec.Nodes != 4 || sec.Bytes != 123456 || sec.NewestDay != "2026-10-08" {
		t.Fatalf("存储统计不符：%+v", sec)
	}
	if sec.StorageNote != "" {
		t.Fatalf("全量身份不该带「不展示」说明：%q", sec.StorageNote)
	}
}

// 受限身份：只给能力说明，**不展示全局口径的存储统计**（否则会把范围外的规模透出去）。
func TestCollectLogsRestrictedHidesStorage(t *testing.T) {
	g := &Generator{logs: &fakeLogStore{cap: logstore.Capability{
		Backend: "local",
		Storage: logstore.StorageStats{Sources: 3, Nodes: 4, Bytes: 123456},
	}}}
	sec := g.collectLogs(asset.ScopeSnapshot{
		NodeMode: asset.ScopeModeLimited, Nodes: []string{"web-01"}, AssetMode: asset.ScopeModeAll,
	})
	if sec == nil {
		t.Fatal("受限身份仍应有能力说明")
	}
	if sec.Sources != 0 || sec.Nodes != 0 || sec.Bytes != 0 {
		t.Fatalf("受限身份不得展示全局存储统计：%+v", sec)
	}
	if !strings.Contains(sec.StorageNote, "不展示") {
		t.Fatalf("受限身份应说明为什么没有存储统计：%q", sec.StorageNote)
	}
	if sec.Backend != "local" {
		t.Fatalf("能力说明应保留：%+v", sec)
	}
}

// 容器与日志章节要能渲染出来。
func TestRenderHTMLShowsContainerAndLogs(t *testing.T) {
	html := renderHTML(reportData{
		Container: &ContainerSection{
			SnapshotNote: "当前快照：容器指标与集群清单都是即时查询。",
			ClustersTotal: 2, ClustersUp: 1, NodesTotal: 5, NodesReady: 4,
			PodsTotal: 30, PodsRunning: 25, PodsPending: 2, PodsFailed: 1, PodsAbnormal: 5,
			Workloads: []ContainerWorkloadStat{{Kind: "Deployment", Total: 6, Unhealthy: 1}},
			Clusters: []ContainerCluster{
				{Node: "vm-1", Instance: "k3s-dev", Name: "k3s-dev", Version: "v1.30", Up: true},
				{Node: "vm-2", Instance: "k3s-x", Name: "k3s-x", Up: false},
			},
			AssetCounts: map[string]int{"pod": 12, "workload": 3},
		},
		Logs: &LogsSection{
			Backend: "local", ScanBudgetMB: 64, ScanBudgetLines: 200000,
			Sources: 3, Nodes: 4, OldestDay: "2026-10-01", NewestDay: "2026-10-08",
			Notes: []string{"本地后端按天分片"},
		},
	})
	for _, want := range []string{
		"容器与工作负载", "当前快照", "Deployment", "台账中的容器类资产",
		"日志", "单次扫描预算", "本地后端按天分片", "本章不含日志正文",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("报告应包含 %q", want)
		}
	}
}
