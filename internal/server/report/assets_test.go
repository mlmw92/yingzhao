package report

import (
	"strings"
	"testing"

	"github.com/mlmw92/yingzhao/internal/server/asset"
)

// 资产与配置章节（设计件 §4.2）。
//
// 守两件事：
//  1. 取数**必须按范围下推**（节点维度 + 业务标签维度），否则受限身份的资产数字会越权；
//  2. 只放聚合——报告是会被下载转发的文件，明细不进。

// fakeAssets 是台账聚合的替身：报告侧只关心「取到没取到、按什么条件取」。
type fakeAssets struct {
	stats    asset.Stats
	links    asset.LinkOverview
	runs     []asset.InspectRun
	findings []asset.InspectFinding

	count int

	lastFilter asset.ListFilter
	lastScope  asset.ChangeScope
	lastNodes  []string
	lastCount  asset.ListFilter
}

func (f *fakeAssets) Stats(fl asset.ListFilter, since int64) (asset.Stats, error) {
	f.lastFilter = fl
	return f.stats, nil
}

func (f *fakeAssets) LinkOverview(cs asset.ChangeScope) (asset.LinkOverview, error) {
	f.lastScope = cs
	return f.links, nil
}

func (f *fakeAssets) InspectRunsInNodes(limit int, nodes []string) ([]asset.InspectRun, error) {
	f.lastNodes = nodes
	return f.runs, nil
}

func (f *fakeAssets) InspectFindingsInNodes(runID int64, limit int, nodes []string) ([]asset.InspectFinding, bool, error) {
	return f.findings, true, nil
}

func (f *fakeAssets) Count(fl asset.ListFilter) (int, error) {
	f.lastCount = fl
	return f.count, nil
}

func TestCollectAssetsPushesScope(t *testing.T) {
	fake := &fakeAssets{
		stats: asset.Stats{Total: 12, Missing: 1, NoOwner: 3, Conflict: 2, Ignored: 4, Changes: 9},
		links: asset.LinkOverview{
			TotalLinks: 5, AssetsWithoutLinks: 6,
			Groups: []asset.LinkStat{{FromType: "host", Kind: "runs_on", ToType: "middleware-instance", Count: 5}},
		},
		runs: []asset.InspectRun{{ID: 7, Actor: "schedule", Assets: 12, Findings: 3, Baselined: 1}},
		findings: []asset.InspectFinding{
			{Level: asset.FindingCritical}, {Level: asset.FindingWarning}, {Level: asset.FindingWarning},
		},
	}
	g := &Generator{assets: fake}

	// ① 不限范围：两个维度都要是 nil（"不生效"而不是"空清单"）
	sec := g.collectAssets(scopeAllForTest())
	if sec == nil {
		t.Fatal("应产出资产章节")
	}
	if fake.lastFilter.Nodes != nil || fake.lastFilter.LabelSelectors != nil {
		t.Fatalf("all 应折算出 nil（不生效）：%+v", fake.lastFilter)
	}
	if sec.Total != 12 || sec.Changes != 9 || sec.ChangeWindowDays != assetChangeWindowDays {
		t.Fatalf("摘要计数不符：%+v", sec)
	}
	if sec.LinksTotal != 5 || sec.NoLinkAssets != 6 || len(sec.LinkGroups) != 1 {
		t.Fatalf("关系总览不符：%+v", sec)
	}
	if sec.Inspect == nil || sec.Inspect.Findings != 3 || sec.Inspect.ByLevel["warning"] != 2 ||
		sec.Inspect.ByLevel["critical"] != 1 || sec.Inspect.Sampled != 3 {
		t.Fatalf("巡检结论不符：%+v", sec.Inspect)
	}

	// ② 受限范围：节点与业务标签两个维度都要下推（空清单也要下推成非 nil 空切片）
	limited := asset.ScopeSnapshot{
		NodeMode: asset.ScopeModeLimited, Nodes: []string{"web-01"},
		AssetMode: asset.ScopeModeLimited, Labels: []asset.ScopeLabel{{Key: "biz", Value: "pay"}},
	}
	g.collectAssets(limited)
	if len(fake.lastFilter.Nodes) != 1 || fake.lastFilter.Nodes[0] != "web-01" {
		t.Fatalf("节点维度应下推：%+v", fake.lastFilter.Nodes)
	}
	if len(fake.lastFilter.LabelSelectors) != 1 || fake.lastFilter.LabelSelectors[0].Value != "pay" {
		t.Fatalf("业务标签维度应下推：%+v", fake.lastFilter.LabelSelectors)
	}
	if len(fake.lastScope.Nodes) != 1 || len(fake.lastScope.LabelSelectors) != 1 {
		t.Fatalf("关系总览也要两个维度都下推：%+v", fake.lastScope)
	}
	if len(fake.lastNodes) != 1 {
		t.Fatalf("巡检记录按节点收窄：%+v", fake.lastNodes)
	}

	// ③ 受限但一个节点都看不到：下推空清单（恒空），不能退化成"不限"
	g.collectAssets(asset.ScopeSnapshot{NodeMode: asset.ScopeModeLimited, AssetMode: asset.ScopeModeAll})
	if fake.lastFilter.Nodes == nil || len(fake.lastFilter.Nodes) != 0 {
		t.Fatalf("受限且空应下推非 nil 空切片：%#v", fake.lastFilter.Nodes)
	}
}

// 台账未注入（库不可用）→ 不产出章节，而不是产出一份「0 个资产」。
func TestCollectAssetsWithoutService(t *testing.T) {
	g := &Generator{}
	if sec := g.collectAssets(scopeAllForTest()); sec != nil {
		t.Fatalf("未注入台账时不应产出章节：%+v", sec)
	}
}

// 资产章节要渲染出来（含"只给聚合"的口径说明）。
func TestRenderHTMLShowsAssetsSection(t *testing.T) {
	html := renderHTML(reportData{
		Assets: &AssetsSection{
			Total: 12, Missing: 1, Changes: 9, ChangeWindowDays: 7,
			LinksTotal: 5, NoLinkAssets: 6,
			LinkGroups: []AssetLinkGroup{{FromType: "host", Kind: "runs_on", ToType: "pod", Source: "discovered", Count: 5}},
			Inspect:    &AssetInspectInfo{At: 1700000000000, Assets: 12, Findings: 3, ByLevel: map[string]int{"warning": 3}, Sampled: 3},
		},
	})
	for _, want := range []string{"资产与配置", "关系总览", "最近一次配置巡检", "不含属性值与差异明细"} {
		if !strings.Contains(html, want) {
			t.Fatalf("资产章节应包含 %q", want)
		}
	}
}
