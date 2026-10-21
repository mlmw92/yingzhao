package api

import (
	"fmt"
	"strings"

	"github.com/mlmw92/yingzhao/internal/server/asset"
	"github.com/mlmw92/yingzhao/internal/server/auth"
	"github.com/mlmw92/yingzhao/internal/server/report"
)

// 报告的范围与章节折算（设计件 docs/superpowers/specs/2026-10-08-reporting-views-design.md）。
//
// 报告此前不接资源范围、也没有章节概念。这两件事都必须在**有人**的那一侧算好：
// 报告还要被定时器调用，而定时器没有请求上下文（平台里所有执行入口都靠请求上下文取
// Principal，后台 goroutine 从不写审计）。
//
// 范围本身复用 `asset.ScopeSnapshot`（节点维度 + 业务标签维度）：三态口径在平台里已经定过，
// 报告这边不再另写一份。

// ReportScopeSnapshot 按用户名折算报告范围与权限跳过——「身份 → 报告范围」的唯一出口。
//
// 与周期化巡检的 InspectScopeSnapshot 同构：
//   - 未启用登录认证：没有身份与范围的概念，业务接口一律放行 → 返回「两个维度都不限」且 ok=true；
//   - 启用认证但用户名解析不到（账号被删/停用）→ ok=false，由调度器**拒绝生成**；
//   - 受限身份：受限的维度写 limited + 清单（清单为空即恒不匹配，fail-closed）。
//
// 章节的「无权限跳过」也在这里算：报告是下载物，一旦生成就脱离了原接口的权限校验，
// 不这么做 `report:export` 就成了读取三域数据的旁路。
func (a *API) ReportScopeSnapshot(user string) (report.ScopeResolution, bool) {
	if a.authStore == nil {
		return report.ScopeResolution{Scope: scopeAll()}, true
	}
	// GetPrincipal 返回 nil 表示这个身份已经解析不到（账号被删/被停用）——**不能**当成「不限」。
	p := a.authStore.GetPrincipal(strings.TrimSpace(user))
	if p == nil {
		return report.ScopeResolution{}, false
	}
	return a.reportScopeFor(p), true
}

// reportScopeFor 把已解析的身份折算成报告范围与权限跳过（请求路径与调度路径共用同一实现，
// 避免出现「手动生成按一套口径、定时生成按另一套」）。
func (a *API) reportScopeFor(p *auth.Principal) report.ScopeResolution {
	res := report.ScopeResolution{Scope: scopeAll()}
	if p == nil {
		return res
	}
	if !p.Scope.IsGlobal() {
		res.Scope.NodeMode = asset.ScopeModeLimited
		// 受限但没有任何可见节点 → 空切片（恒不匹配），不是 nil
		res.Scope.Nodes = a.assetAllowedNodes(p)
	}
	if p.Scope.LimitsAssets() {
		res.Scope.AssetMode = asset.ScopeModeLimited
		// Selectors 为 nil（limited 却没配选择器）时保持空清单：IsEmpty 会据此判成恒空。
		if d := auth.ResolveAssetScope(p); d.Selectors != nil {
			for _, sel := range d.Selectors {
				res.Scope.Labels = append(res.Scope.Labels, asset.ScopeLabel{Key: sel.Key, Value: sel.Value})
			}
		}
	}
	skipped := map[string]string{}
	for _, id := range []string{report.SectionAssets, report.SectionContainer, report.SectionLogs} {
		perm := report.SectionPermission(id)
		if perm != "" && !p.HasPermission(perm) {
			skipped[id] = "缺少 " + perm + " 权限"
		}
	}
	if len(skipped) > 0 {
		res.Skipped = skipped
	}
	return res
}

// scopeAll 返回「两个维度都不限」的范围快照。
func scopeAll() asset.ScopeSnapshot {
	return asset.ScopeSnapshot{NodeMode: asset.ScopeModeAll, AssetMode: asset.ScopeModeAll}
}

// normalizeReportSections 校验并去重章节 ID；空表示全选（默认全开）。
//
// 未知 ID 直接 400 而不是静默忽略：静默忽略会让「我勾了日志章节却没出来」变成一个谜。
func normalizeReportSections(in []string) ([]string, error) {
	if len(in) == 0 {
		return nil, nil
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, id := range in {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		if !report.ValidSection(id) {
			return nil, fmt.Errorf("未知的报告章节：%s", id)
		}
		seen[id] = true
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}
