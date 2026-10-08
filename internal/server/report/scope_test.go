package report

import (
	"strings"
	"testing"

	"github.com/nebula/monitor/internal/server/asset"
)

// 报告范围与章节（设计件 docs/superpowers/specs/2026-10-08-reporting-views-design.md）。
//
// 范围快照本身的**三态与交集**由 asset 包的用例覆盖（report 直接复用它的实现，不另写一份）；
// 这里只钉报告自己的语义：
//  1. 节点过滤（scopeCovers）；
//  2. 章节的合法性与权限要求；
//  3. 调度侧折算——定时按保存者、手动按点击者，解析不到与交集为空都**不生成**（fail-closed）；
//  4. 报告头部能解释自己。

func scopeAllForTest() asset.ScopeSnapshot {
	return asset.ScopeSnapshot{NodeMode: asset.ScopeModeAll, AssetMode: asset.ScopeModeAll}
}

func TestScopeCovers(t *testing.T) {
	if !scopeCovers(scopeAllForTest(), "any") {
		t.Fatal("all 应覆盖任何节点")
	}
	lim := asset.ScopeSnapshot{
		NodeMode: asset.ScopeModeLimited, Nodes: []string{"web-01"}, AssetMode: asset.ScopeModeAll,
	}
	if !scopeCovers(lim, "web-01") || scopeCovers(lim, "web-02") {
		t.Fatal("limited 只覆盖清单内节点")
	}
}

// 章节的合法性与权限要求：只有三域章节带额外权限点。
func TestSectionsAndPermissions(t *testing.T) {
	for _, id := range allSections {
		if !ValidSection(id) {
			t.Fatalf("内置章节应合法：%s", id)
		}
	}
	if ValidSection("nope") {
		t.Fatal("未知章节不应合法")
	}
	if SectionPermission(SectionLogs) != "logs:read" ||
		SectionPermission(SectionAssets) != "assets:read" ||
		SectionPermission(SectionContainer) != "container:read" {
		t.Fatal("三域章节必须带权限要求")
	}
	if SectionPermission(SectionHosts) != "" {
		t.Fatal("既有章节本批不额外要求权限")
	}
}

// 调度侧的范围折算：定时按保存者、手动按点击者；解析不到与交集为空都**不生成**。
func TestSchedulerScopeNarrowsAndFailsClosed(t *testing.T) {
	byUser := map[string]ScopeResolution{
		"admin":  {Scope: scopeAllForTest(), Skipped: map[string]string{SectionLogs: "缺少 logs:read 权限"}},
		"narrow": {Scope: asset.ScopeSnapshot{NodeMode: asset.ScopeModeLimited, Nodes: []string{"web-01"}, AssetMode: asset.ScopeModeAll}},
		"other":  {Scope: asset.ScopeSnapshot{NodeMode: asset.ScopeModeLimited, Nodes: []string{"web-02"}, AssetMode: asset.ScopeModeAll}},
	}
	resolve := func(user string) (ScopeResolution, bool) {
		r, ok := byUser[user]
		return r, ok
	}

	var got GenerateOptions
	newSched := func(t *testing.T) *Scheduler {
		t.Helper()
		s, _ := newTestScheduler(t, ScheduleConfig{Enabled: true, Type: string(ReportDaily), IntervalHours: 24})
		s.SetScopeResolver(resolve)
		s.generate = func(opts GenerateOptions) (string, error) {
			got = opts
			return "rpt-test", nil
		}
		return s
	}
	all := scopeAllForTest()
	narrow := asset.ScopeSnapshot{NodeMode: asset.ScopeModeLimited, Nodes: []string{"web-01"}, AssetMode: asset.ScopeModeAll}

	// ① 保存者 admin（不限）→ 定时生成不限范围、留痕 schedule，权限跳过随当前身份透传
	s := newSched(t)
	if err := s.Save(ScheduleConfig{Enabled: true, Type: string(ReportDaily), IntervalHours: 24, Scope: &all, SavedBy: "admin"}); err != nil {
		t.Fatal(err)
	}
	if run := s.RunNow("", false); run.Error != "" {
		t.Fatalf("应生成成功：%s", run.Error)
	}
	if got.Scope.NodeMode != asset.ScopeModeAll || got.Actor != ScheduleActor {
		t.Fatalf("定时生成的范围/留痕不符：%+v", got)
	}
	if got.Skipped[SectionLogs] == "" {
		t.Fatalf("当前身份无 logs:read 时应带上跳过原因：%+v", got.Skipped)
	}

	// ② 保存者解析不到（账号被删/停用）→ fail-closed，但推进 LastRunAt 免得每 tick 重试
	gone := newSched(t)
	if err := gone.Save(ScheduleConfig{Enabled: true, Type: string(ReportDaily), IntervalHours: 24, Scope: &all, SavedBy: "ghost"}); err != nil {
		t.Fatal(err)
	}
	run := gone.RunNow("", false)
	if !strings.Contains(run.Error, "已不存在") {
		t.Fatalf("身份解析不到必须明确报出原因：%q", run.Error)
	}
	if run.ReportID != "" {
		t.Fatal("fail-closed 时不得生成报告")
	}
	if gone.Config().LastRunAt == 0 {
		t.Fatal("失败也要推进 LastRunAt（否则每个 tick 重试刷同一条错）")
	}

	// ③ 手动点击者与快照无交集 → 不生成
	manual := newSched(t)
	if err := manual.Save(ScheduleConfig{Enabled: true, Type: string(ReportDaily), IntervalHours: 24, Scope: &narrow, SavedBy: "narrow"}); err != nil {
		t.Fatal(err)
	}
	run = manual.RunNow("other", true)
	if !strings.Contains(run.Error, "收窄为空") {
		t.Fatalf("交集为空应明确报出原因：%q", run.Error)
	}
	if run.ReportID != "" {
		t.Fatal("交集为空时不得生成报告")
	}

	// ④ 手动点击者比快照窄 → 按点击者收窄，留痕是登录名而不是 schedule
	ok := newSched(t)
	if err := ok.Save(ScheduleConfig{Enabled: true, Type: string(ReportDaily), IntervalHours: 24, Scope: &all, SavedBy: "admin"}); err != nil {
		t.Fatal(err)
	}
	if run := ok.RunNow("narrow", true); run.Error != "" {
		t.Fatalf("应生成成功：%s", run.Error)
	}
	if got.Scope.NodeMode != asset.ScopeModeLimited || len(got.Scope.Nodes) != 1 || got.Scope.Nodes[0] != "web-01" {
		t.Fatalf("手动应再按点击者收窄：%+v", got.Scope)
	}
	if got.Actor != "narrow" {
		t.Fatalf("手动留痕应为登录名：%q", got.Actor)
	}
}

// 报告头部要能解释自己：生成口径与「已跳过章节」都要出现在 HTML 里。
func TestRenderHTMLShowsScopeAndSkipped(t *testing.T) {
	html := renderHTML(reportData{
		Period: "2026-10-08 00:00 ~ 2026-10-08 12:00",
		Scope: ScopeInfo{
			Actor:    "alice",
			Desc:     "指定 2 个节点（1 台在范围内）",
			Nodes:    1,
			Sections: []string{SectionHosts},
		},
		Skipped: []SkippedSection{{ID: SectionLogs, Title: SectionTitle(SectionLogs), Reason: "缺少 logs:read 权限"}},
	})
	for _, want := range []string{"生成者", "alice", "覆盖范围", "已跳过章节", "日志", "缺少 logs:read 权限"} {
		if !strings.Contains(html, want) {
			t.Fatalf("报告头部应包含 %q", want)
		}
	}
}

// 章节选择的语义：空 = 全选；非空 = 只含列出的。
func TestGenerateOptionsSelected(t *testing.T) {
	opts := GenerateOptions{Sections: []string{SectionHosts, SectionLogs}}
	if !opts.selected(SectionHosts) || opts.selected(SectionContainer) {
		t.Fatal("selected 语义不符")
	}
	if !(GenerateOptions{}).selected(SectionLogs) {
		t.Fatal("空 Sections 应为全选（默认全开）")
	}
}
