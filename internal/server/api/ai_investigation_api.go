package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/mlmw92/yingzhao/internal/model"
	"github.com/mlmw92/yingzhao/internal/server/ai"
	"github.com/mlmw92/yingzhao/internal/server/approval"
	"github.com/mlmw92/yingzhao/internal/server/audit"
	"github.com/mlmw92/yingzhao/internal/server/ops"
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

// POST /api/v1/ai/investigations/retry
//
// 重跑一条「未完成 / 失败」的调查。
//
// 只接受**仍然活跃**的告警：告警已恢复时「现在该怎么排查」这个问题本身就不存在了。
// 模型调用放在后台：单次调查最长 120 秒，占住 HTTP 连接会让网关先超时；
// 因此先同步落一条 running 记录再返回 202，前端轮询 GET 即可看到进展。
func (a *API) handleAIRetryInvestigation(w http.ResponseWriter, r *http.Request) {
	if a.aiInv == nil || a.aiMgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "AI 调查未启用"})
		return
	}
	var req struct {
		AlertID string `json:"alertId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.AlertID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少 alertId"})
		return
	}
	cfg := a.aiMgr.Get().Normalize()
	if !cfg.Enabled || !cfg.Ready() {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "AI 未配置或未启用，请先在「系统设置 → AI 助手」里配置模型",
		})
		return
	}
	ref, ok, err := a.activeAlertRef(req.AlertID)
	if err != nil {
		// 查询失败不能伪装成「告警已恢复」：那会让值班的人以为问题自己好了。
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "告警查询失败，请稍后重试"})
		return
	}
	if !ok {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "该告警已恢复，无法重新调查"})
		return
	}
	// 同一告警不叠加调查：既浪费 token，也会让「哪份结论是最新的」变得含糊。
	if prev, ok := a.aiInv.ByAlert(req.AlertID); ok && prev.State == "running" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "该告警已有调查在进行中"})
		return
	}

	id := a.newInvestigation(ref)
	actor := AuthenticatedUser(r)
	go a.runInvestigation(context.Background(), id, ref, "ai:investigate:retry", actor)
	a.recordAI(r, "ai:investigate:retry", fmt.Sprintf("告警 %s 节点 %s 重新调查（%s）", ref.ID, ref.Node, id))
	writeJSON(w, http.StatusAccepted, map[string]any{
		"message": "已开始重新调查", "alertId": ref.ID, "investigationId": id,
	})
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
		out = append(out, toAlertRef(e))
	}
	return out
}

func toAlertRef(e model.AlertEvent) ai.AlertRef {
	return ai.AlertRef{
		ID: alertEventID(e), RuleID: e.RuleID, RuleName: e.RuleName,
		Node: e.Node, Message: e.Message, StartsAt: e.StartsAt,
	}
}

// activeAlertRef 按稳定 ID 找一条仍在活跃的告警。
// 查询失败时把错误交给调用方：这里分不清「没有这条告警」和「存储读不到」。
func (a *API) activeAlertRef(id string) (ai.AlertRef, bool, error) {
	if a.alerts == nil || id == "" {
		return ai.AlertRef{}, false, nil
	}
	events, err := activeAlerts(a.alerts)
	if err != nil {
		return ai.AlertRef{}, false, err
	}
	for _, e := range events {
		if alertEventID(e) == id {
			return toAlertRef(e), true, nil
		}
	}
	return ai.AlertRef{}, false, nil
}

// RunAutoInvestigation 跑一次自动调查并落存储（含审计）。
func (a *API) RunAutoInvestigation(ctx context.Context, ref ai.AlertRef) {
	if a.aiMgr == nil || a.aiInv == nil {
		return
	}
	a.runInvestigation(ctx, a.newInvestigation(ref), ref, "ai:auto_investigate", "auto")
}

// newInvestigation 先落一条 running 记录再跑：
// 前端发起重跑后立刻轮询时，必须看到「调查中」，而不是上一轮的旧结论。
func (a *API) newInvestigation(ref ai.AlertRef) string {
	inv := ai.Investigation{
		ID: "inv-" + strconv.FormatInt(time.Now().UnixNano(), 36), AlertID: ref.ID,
		RuleID: ref.RuleID, RuleName: ref.RuleName, Node: ref.Node,
		State: "running", StartedAt: time.Now().UnixMilli(),
	}
	_ = a.aiInv.Add(inv)
	return inv.ID
}

func (a *API) runInvestigation(ctx context.Context, id string, ref ai.AlertRef, action, actor string) {
	cfg := a.aiMgr.Get()
	// panic 也要把记录落到终态，否则会永远停在「调查中」
	defer func() {
		if p := recover(); p != nil {
			a.aiInv.Update(id, func(it *ai.Investigation) {
				it.State = "failed"
				it.Error = fmt.Sprintf("调查异常终止: %v", p)
			})
			a.recordInvestigation(action, actor, fmt.Sprintf("告警 %s 调查 panic: %v", ref.ID, p))
		}
	}()

	system := "你是运维平台的诊断助手。只根据工具返回的数据作答；不要猜测，也不要执行任何写操作。" +
		"若结论指向一个可执行的处置动作，可调用 propose_action 记录建议。" +
		"只有工具结果里有明确依据时才建议；最多 3 条；不要建议文件分发。" +
		"建议不会自动执行，需人工提交并由另一位同事批准。"
	user := fmt.Sprintf("告警：%s（规则 %s，节点 %s）。请先调用工具核实，再给出结论与建议。",
		ref.Message, ref.RuleName, ref.Node)
	// 建议先累积到本次运行的局部切片，随最终 Update 一并落库。
	// 每次建议都调 aiInv.Update 会无条件写 EndedAt，让还在跑的调查显示成「已结束」。
	var suggestions []ai.Suggestion
	toolbox := a.newToolbox(ai.ToolScope{
		Node: ref.Node, AllowSensitive: cfg.AllowSensitive,
		Propose: func(s ai.Suggestion) error {
			suggestions = append(suggestions, s)
			return nil
		},
	})
	out, err := ai.Run(ctx, cfg, system, user, toolbox, ai.Budget{})
	a.aiInv.Update(id, func(it *ai.Investigation) {
		it.Steps = out.Steps
		it.Suggestions = suggestions
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
	a.recordInvestigation(action, actor, fmt.Sprintf("告警 %s 节点 %s：%s，工具调用 %d 次",
		ref.ID, ref.Node, out.Stopped, len(out.Steps)))
}

// recordInvestigation 写一条调查审计。
// 自动调查没有请求上下文（actor=auto）；手动重跑记真实用户。
func (a *API) recordInvestigation(action, actor, detail string) {
	if a.audit == nil {
		return
	}
	_ = a.audit.Record(audit.Event{
		User: actor, Method: "SYSTEM", Path: "/api/v1/ai/investigations",
		Status: 200, Succeeded: true,
		Category: "ai", Action: action, Detail: detail,
	})
}

// POST /api/v1/ai/investigations/{id}/suggestions/{sid}/submit
//
// 把一条 AI 建议转成审批单。**永远**创建审批单，不看 approval.Config.Enabled：
// 那个开关回答的是「人提交的动作要不要复核」，而模型产出的动作没有免复核的选项。
func (a *API) handleAISubmitSuggestion(w http.ResponseWriter, r *http.Request) {
	if a.aiInv == nil || a.approval == nil || a.ops == nil || a.nodeMgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "AI 建议或审批通道未启用"})
		return
	}
	// 先判权限再看资源：没有执行权限的人不该通过错误码差异探测建议是否存在。
	if !a.checkPerm(w, r, "ops:exec") {
		return
	}

	inv, ok := a.aiInv.ByID(r.PathValue("id"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "调查不存在"})
		return
	}
	var sug *ai.Suggestion
	for i := range inv.Suggestions {
		if inv.Suggestions[i].ID == r.PathValue("sid") {
			sug = &inv.Suggestions[i]
			break
		}
	}
	if sug == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "建议不存在"})
		return
	}
	if !a.nodeInScope(Principal(r), sug.Node) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "节点不存在"})
		return
	}

	// 不信任落库值：动作可能已下架，参数规则也可能已收紧。
	action, params, err := ops.Validate(sug.Kind, sug.Params)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "建议已失效：" + err.Error()})
		return
	}
	if action.ReadOnly || sug.Kind == ops.KindFilePush {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "该动作不支持从建议提交"})
		return
	}
	if action.Perm != "" && !a.checkPerm(w, r, action.Perm) {
		return
	}
	nd, ok := a.nodeMgr.GetNode(sug.Node)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "节点不存在"})
		return
	}
	if nd.Status != "online" {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": fmt.Sprintf("节点 %s 当前不在线（状态 %s），无法提交", sug.Node, nd.Status),
		})
		return
	}
	// 能力协商：与人工提交同一条判定。节点没放行的动作在提交时就拒绝，
	// 不把注定失败的单子推给审批人。
	if err := a.ops.CheckSupport(sug.Node, action); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}

	// 幂等：同一调查的同一条建议只能有一张未结束的审批单。
	// 面板刷新后本地「已提交」状态会丢，用户再点一次不能变成两张单子——
	// 两张都批准就是同一个动作执行两次。
	for _, prev := range a.approval.List(approval.Filter{State: approval.StatePending, Limit: 500}) {
		if prev.Extra["investigationId"] == inv.ID && prev.Extra["suggestionId"] == sug.ID {
			writeJSON(w, http.StatusAccepted, map[string]any{
				"pending":   true,
				"duplicate": true,
				"approval":  a.approvalView(r, prev),
				"message":   "该建议已提交过审批（审批单 " + prev.ID + "），请等待另一位同事批准",
			})
			return
		}
	}

	saved := a.submitApproval(r, &approval.Request{
		Kind: approval.EntryOpsWrite, OpsKind: sug.Kind, Node: sug.Node, Params: params,
		// 模型给的理由要送到审批人眼前：他判断的是「要不要批准这个动作」，理由就是依据。
		Reason: sug.Reason,
		Title:  fmt.Sprintf("AI 建议：%s（写操作） @ %s", action.Title, sug.Node),
		Extra: map[string]string{
			"source": "ai", "investigationId": inv.ID, "suggestionId": sug.ID,
		},
	})
	a.recordAI(r, "ai:suggestion:submit", fmt.Sprintf(
		"调查 %s 建议 %s：%s @ %s → 审批单 %s", inv.ID, sug.ID, action.Title, sug.Node, saved.ID))
	writeJSON(w, http.StatusAccepted, map[string]any{
		"pending":  true,
		"approval": a.approvalView(r, saved),
		"message":  "已提交审批：需另一位同事批准后才会执行",
	})
}
