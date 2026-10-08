package api

import (
	"testing"

	"github.com/nebula/monitor/internal/server/asset"
	"github.com/nebula/monitor/internal/server/report"
)

// 报告范围折算（设计件 docs/superpowers/specs/2026-10-08-reporting-views-design.md）。
//
// 这里钉的是「身份 → 范围 + 权限跳过」这一层：
//   - 全局身份两个维度都不限；
//   - 受限身份写 limited + 自己分组的节点；
//   - **缺某域权限就把该章节记进 Skipped**（报告是下载物，不能成为越权旁路）。

func TestReportScopeForGlobalIsUnlimited(t *testing.T) {
	a := scopeTestAPI(t)
	res := a.reportScopeFor(globalPrincipal("report:export"))
	if res.Scope.NodeMode != asset.ScopeModeAll || res.Scope.AssetMode != asset.ScopeModeAll {
		t.Fatalf("全局身份两个维度都应不限制：%+v", res.Scope)
	}
	if len(res.Skipped) != 3 {
		t.Fatalf("只有 report:export 时三域章节都应被跳过：%+v", res.Skipped)
	}
	for _, id := range []string{report.SectionAssets, report.SectionContainer, report.SectionLogs} {
		if res.Skipped[id] == "" {
			t.Fatalf("章节 %s 应带跳过原因：%+v", id, res.Skipped)
		}
	}
}

func TestReportScopeForNarrowsNodesAndSkipsGranted(t *testing.T) {
	a := scopeTestAPI(t)
	// 有 assets:read 与 logs:read，缺 container:read
	res := a.reportScopeFor(restrictedPrincipal([]string{"report:export", "assets:read", "logs:read"}, "g1"))
	if res.Scope.NodeMode != asset.ScopeModeLimited {
		t.Fatalf("受限身份节点维度应为 limited：%+v", res.Scope)
	}
	if len(res.Scope.Nodes) != 2 { // g1 下的 web-01 / web-02
		t.Fatalf("应只含自己分组的节点，实际 %v", res.Scope.Nodes)
	}
	if _, bad := res.Skipped[report.SectionAssets]; bad {
		t.Fatalf("有 assets:read 时不该跳过资产章节：%+v", res.Skipped)
	}
	if _, bad := res.Skipped[report.SectionLogs]; bad {
		t.Fatalf("有 logs:read 时不该跳过日志章节：%+v", res.Skipped)
	}
	if res.Skipped[report.SectionContainer] == "" {
		t.Fatalf("缺 container:read 时应跳过容器章节：%+v", res.Skipped)
	}
}

// 未启用认证（无 authStore）：没有身份与范围的概念，两个维度都不限、也不跳过任何章节。
func TestReportScopeForNoAuthStoreIsUnlimited(t *testing.T) {
	a := &API{}
	res := a.reportScopeFor(nil)
	if res.Scope.NodeMode != asset.ScopeModeAll || res.Scope.AssetMode != asset.ScopeModeAll {
		t.Fatalf("无身份时不应限制：%+v", res.Scope)
	}
	if len(res.Skipped) != 0 {
		t.Fatalf("无身份时不应跳过章节：%+v", res.Skipped)
	}
	if _, ok := a.ReportScopeSnapshot("anyone"); !ok {
		t.Fatal("无 authStore 时折算应成功（与「未启用认证一律放行」同取向）")
	}
}

// 启用认证但身份解析不到 → ok=false（由调度器拒绝生成，不能当成「不限」）。
func TestReportScopeSnapshotUnknownUser(t *testing.T) {
	a := scopeTestAPI(t)
	if _, ok := a.ReportScopeSnapshot("ghost"); ok {
		t.Fatal("解析不到的身份必须返回 ok=false（fail-closed）")
	}
}
