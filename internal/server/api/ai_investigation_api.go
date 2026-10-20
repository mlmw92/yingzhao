package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/nebula/monitor/internal/model"
	"github.com/nebula/monitor/internal/server/ai"
	"github.com/nebula/monitor/internal/server/audit"
)

// SetInvestigationStore 注入 AI 调查结果存储（未注入时查询返回空）。
func (a *API) SetInvestigationStore(s *ai.InvestigationStore) { a.aiInv = s }

// GET /api/v1/ai/investigations?alertId=xxx
func (a *API) handleAIInvestigations(w http.ResponseWriter, r *http.Request) {
	if a.aiInv == nil {
		writeJSON(w, http.StatusOK, map[string]any{"investigation": nil})
		return
	}
	alertID := r.URL.Query().Get("alertId")
	if alertID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少 alertId"})
		return
	}
	inv, ok := a.aiInv.ByAlert(alertID)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"investigation": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"investigation": inv})
}

// CriticalAlertsForAI 把活跃 critical 告警投影给 ai 包（窄类型，避免依赖倒置）。
func (a *API) CriticalAlertsForAI() ai.AlertSource { return apiAlertSource{a: a} }

type apiAlertSource struct{ a *API }

func (s apiAlertSource) ActiveCritical() []ai.AlertRef {
	if s.a.alerts == nil {
		return nil
	}
	var out []ai.AlertRef
	for _, e := range s.a.alerts.Active() {
		if e.Severity != model.SeverityCritical || e.Node == "" {
			continue
		}
		out = append(out, ai.AlertRef{
			ID: e.ID, RuleID: e.RuleID, RuleName: e.RuleName,
			Node: e.Node, Message: e.Message, StartsAt: e.StartsAt,
		})
	}
	return out
}

// RunAutoInvestigation 跑一次自动调查并落存储（含审计）。
func (a *API) RunAutoInvestigation(ctx context.Context, ref ai.AlertRef) {
	if a.aiMgr == nil || a.aiInv == nil {
		return
	}
	cfg := a.aiMgr.Get()
	inv := ai.Investigation{
		ID: "inv-" + strconv.FormatInt(time.Now().UnixNano(), 36), AlertID: ref.ID,
		RuleID: ref.RuleID, RuleName: ref.RuleName, Node: ref.Node,
		State: "running", StartedAt: time.Now().UnixMilli(),
	}
	_ = a.aiInv.Add(inv)

	system := "你是运维平台的诊断助手。只根据工具返回的数据作答；不要猜测，也不要执行任何写操作。"
	user := fmt.Sprintf("告警：%s（规则 %s，节点 %s）。请先调用工具核实，再给出结论与建议。",
		ref.Message, ref.RuleName, ref.Node)
	toolbox := a.newToolbox(ai.ToolScope{Node: ref.Node, AllowSensitive: cfg.AllowSensitive})
	out, err := ai.Run(ctx, cfg, system, user, toolbox, ai.Budget{})
	a.aiInv.Update(inv.ID, func(it *ai.Investigation) {
		it.Steps = out.Steps
		it.Tokens = out.Tokens
		it.Rounds = out.Rounds
		it.Stopped = out.Stopped
		it.Protocol = out.Protocol
		it.Answer = out.Answer
		if err != nil {
			it.State = "failed"
			it.Error = err.Error()
		} else {
			it.State = "done"
		}
	})
	a.recordAIAuto("ai:auto_investigate", fmt.Sprintf("告警 %s 节点 %s：%s，工具调用 %d 次",
		ref.ID, ref.Node, out.Stopped, len(out.Steps)))
}

// recordAIAuto 写一条自动调查审计（没有请求上下文，actor 记为 auto）。
func (a *API) recordAIAuto(action, detail string) {
	if a.audit == nil {
		return
	}
	_ = a.audit.Record(audit.Event{
		User: "auto", Method: "SYSTEM", Path: "/api/v1/ai/investigations",
		Status: 200, Succeeded: true,
		Category: "ai", Action: action, Detail: detail,
	})
}
