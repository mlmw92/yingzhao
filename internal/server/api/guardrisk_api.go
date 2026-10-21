package api

import (
	"net/http"

	"github.com/mlmw92/yingzhao/internal/server/security"
)

// 护栏组合风险的查询接口（能力全景表 §3.4 后第 3 条的既有 TODO）。
//
// 权限：security:read（与安全中心其它数据一致）。**按节点范围过滤**——
// 风险描述里含该机器的目录与单元名，属不该外泄的信息面。

// handleGuardRisks 返回各节点的护栏组合风险状态。
// GET /api/v1/security/guard-risks
func (a *API) handleGuardRisks(w http.ResponseWriter, r *http.Request) {
	if a.security == nil {
		// 未注入 security store：明确区分"未启用"与"启用了但还没有数据"
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"nodes": []interface{}{}, "enabled": false,
		})
		return
	}
	states := a.security.ListGuardRisks()
	states = filterByNodeScope(a, Principal(r), states, func(s security.GuardRiskState) string { return s.Node })

	out := make([]map[string]interface{}, 0, len(states))
	for _, s := range states {
		// 人话由**服务端**给（`Describe()`）：Agent 日志与界面必须是同一句——
		// 这正是把 GuardRisk 搬进 model 的初衷，界面自己拼一遍就白搬了。
		risks := make([]map[string]interface{}, 0, len(s.Risks))
		for _, rk := range s.Risks {
			risks = append(risks, map[string]interface{}{
				"kind": rk.Kind, "allowedDir": rk.AllowedDir, "exposed": rk.Exposed, "unit": rk.Unit,
				"description": rk.Describe(),
			})
		}
		out = append(out, map[string]interface{}{
			"node":        s.Node,
			"displayName": a.nodeDisplayName(s.Node),
			"nodeIp":      a.nodeIP(s.Node),
			// reported=false 表示该节点**从未上报过**这个字段（旧 Agent，压根没检查过）。
			// 界面必须与"已检查、无风险"分开显示：把前者显示成后者会让人以为那台机器是安全的。
			"reported":   s.Reported,
			"reportedAt": s.ReportedAt,
			"risks":      risks,
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"nodes": out, "enabled": true})
}
