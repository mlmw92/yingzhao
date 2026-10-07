package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/nebula/monitor/internal/model"
	"github.com/nebula/monitor/internal/server/approval"
	"github.com/nebula/monitor/internal/server/audit"
	"github.com/nebula/monitor/internal/server/auth"
	"github.com/nebula/monitor/internal/server/ops"
	"github.com/nebula/monitor/internal/version"
)

// 高风险操作双人复核的接入层。
//
// 设计件：docs/superpowers/specs/2026-10-07-high-risk-approval-design.md
//
// 本文件只做三件事：**判定要不要审批**（needsApproval / approvalGate）、
// **把高风险动作登记成审批单**（submitApproval）、**批准后按原样执行**（executeApproved）。
// 状态机与不可自批在 `internal/server/approval` 里，本层不重复实现——判定写两遍迟早会分叉。

// SetApproval 注入审批单存储与配置（可选；不注入时审批接口返回 503，且不拦截任何动作）。
func (a *API) SetApproval(store *approval.Store, cfg *approval.ConfigStore) {
	a.approval = store
	a.approvalCfg = cfg
}

// approvalConfig 返回当前配置；未注入时返回默认（**关闭**）。
func (a *API) approvalConfig() approval.Config {
	if a.approvalCfg == nil {
		return approval.DefaultConfig()
	}
	return a.approvalCfg.Get()
}

// needsApproval 判定一次动作是否需要人审。**唯一判定处**——五处入口都走它。
//
// entry 是入口标识（见 approval.Entry*），opsKind 只对 ops 入口有意义（用于支持
// 配置里精确到某个动作的 `ops:<动作名>`），readOnly 只对 ops/处置入口有意义。
func (a *API) needsApproval(entry, opsKind string, readOnly bool) bool {
	if a.approval == nil {
		return false
	}
	cfg := a.approvalConfig()
	if opsKind != "" && entry == approval.EntryOpsWrite {
		return cfg.RequiresApproval(opsKind, readOnly)
	}
	return cfg.RequiresApproval(entry, readOnly)
}

// approvalGate 是五处入口共用的拦截点：需要审批就登记审批单、写 **202** 并返回 true
// （调用方见到 true 必须立刻 return，**不要**再执行原动作）。
//
// 返回 202 而不是 201 是**正确性的一部分**：界面据此说"已提交审批"而不是"已下发"。
// 把"进了审批"显示成"已下发"，操作者会以为指令已经在跑了——那正是双人复核要防的误判。
func (a *API) approvalGate(w http.ResponseWriter, r *http.Request, entry, opsKind string, readOnly bool, req *approval.Request) bool {
	if !a.needsApproval(entry, opsKind, readOnly) {
		return false
	}
	req.Kind = entry
	if entry == approval.EntryOpsWrite {
		req.OpsKind = opsKind
	}
	saved := a.submitApproval(r, req)
	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"pending":  true,
		"approval": a.approvalView(r, saved),
		"message":  "已提交审批：本动作**尚未下发**，需另一位具备相应权限的同事批准后才会执行",
	})
	return true
}

// submitApproval 登记一条待审批单（补齐申请人、来源 IP、有效期）并写审计。
func (a *API) submitApproval(r *http.Request, req *approval.Request) *approval.Request {
	req.Requester = AuthenticatedUser(r)
	req.RequesterIP = audit.ClientIP(r)
	if req.Title == "" {
		req.Title = req.Kind
	}
	saved := a.approval.Create(req, a.approvalConfig().TTL())
	if a.audit != nil {
		_ = a.audit.Record(audit.Event{
			User:      req.Requester,
			Method:    r.Method,
			Path:      r.URL.Path,
			Status:    http.StatusAccepted,
			RemoteIP:  req.RequesterIP,
			Succeeded: true,
			RequestID: RequestID(r),
			Category:  "approval",
			Action:    "create:" + req.Kind,
			Detail: fmt.Sprintf("提交审批 %s（%s）：%s%s",
				saved.ID, req.Title, targetsText(saved), reasonText(req.Reason)),
		})
	}
	slog.Info("已提交审批", "id", saved.ID, "kind", req.Kind, "requester", req.Requester, "targets", strings.Join(saved.Targets(), ","))
	return saved
}

func targetsText(req *approval.Request) string {
	ts := req.Targets()
	if len(ts) == 0 {
		return "系统级动作"
	}
	if len(ts) > 5 {
		return fmt.Sprintf("%d 个节点（%s 等）", len(ts), strings.Join(ts[:5], ","))
	}
	return strings.Join(ts, ",")
}

func reasonText(reason string) string {
	if reason == "" {
		return ""
	}
	return "，理由：" + reason
}

// approvalView 是审批单的对外形状。
func (a *API) approvalView(r *http.Request, req *approval.Request) map[string]interface{} {
	now := time.Now().UnixMilli()
	st := req.State
	// 与存储层的呈现一致：超时但还没被扫到的，界面上也应当是"已失效"，
	// 否则会有人对着一条早就过期的单子点批准。
	if st == approval.StatePending && req.Expired(now) {
		st = approval.StateExpired
	}
	mine := false
	if p := Principal(r); p != nil && p.Username == req.Requester {
		mine = true
	}
	return map[string]interface{}{
		"id": req.ID, "kind": req.Kind, "opsKind": req.OpsKind, "title": req.Title,
		"node": req.Node, "nodes": req.Nodes, "params": req.Params, "extra": req.Extra,
		"state": st, "terminal": approval.Terminal(st),
		"requester": req.Requester, "requesterIP": req.RequesterIP, "reason": req.Reason,
		"createdAt": req.CreatedAt, "expireAt": req.ExpireAt,
		"decidedBy": req.DecidedBy, "decidedAt": req.DecidedAt, "decision": req.Decision,
		"executedAt": req.ExecutedAt, "taskId": req.TaskID, "message": req.Message,
		// mine 让界面直接知道"这条不能由我批"（自己提交的），而不是点下去才被拒
		"mine": mine,
	}
}

// approvalReady 确认审批能力已注入。
func (a *API) approvalReady(w http.ResponseWriter) bool {
	if a.approval == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "审批功能未启用"})
		return false
	}
	return true
}

// approvalInScope 判断该身份能否看到/审批这条审批单。
//
// 规则：**目标节点必须全部**在该身份的资源范围内；系统级动作（升级，无目标节点）
// 要求全局范围。批量只要有一个节点越界就整体不可见——否则会出现
// "审批人批准了一批自己只能看见一台的动作"。
func (a *API) approvalInScope(p *auth.Principal, req *approval.Request) bool {
	if p == nil || p.Scope.IsGlobal() {
		return true
	}
	targets := req.Targets()
	if len(targets) == 0 {
		return false
	}
	for _, n := range targets {
		if !a.nodeInScope(p, n) {
			return false
		}
	}
	return true
}

// handleApprovalList 列出审批单。box=pending 为待我审批（排除自己提交的）、mine 为我提交的。
// GET /api/v1/approvals
func (a *API) handleApprovalList(w http.ResponseWriter, r *http.Request) {
	if !a.approvalReady(w) {
		return
	}
	// 顺手把超时的落成 expired：读取时呈现是一回事，落盘的那次必须有人做，
	// 否则重启后一条三小时前的 pending 会重新变成"待审批"。
	a.approval.ExpireOverdue()

	p := Principal(r)
	me := AuthenticatedUser(r)
	box := r.URL.Query().Get("box")
	f := approval.Filter{State: strings.TrimSpace(r.URL.Query().Get("state"))}
	switch box {
	case "mine":
		f.Requester = me
	default: // pending 或未指定：待我审批
		f.ExcludeRequester = me
		if f.State == "" {
			f.State = approval.StatePending
		}
	}

	out := make([]map[string]interface{}, 0)
	for _, it := range a.approval.List(f) {
		if !a.approvalInScope(p, it) {
			continue
		}
		out = append(out, a.approvalView(r, it))
	}
	cfg := a.approvalConfig()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"approvals": out,
		"enabled":   cfg.Enabled,
		"config":    cfg,
	})
}

// handleApprovalGet 返回一条审批单详情。
// GET /api/v1/approvals/{id}
func (a *API) handleApprovalGet(w http.ResponseWriter, r *http.Request) {
	if !a.approvalReady(w) {
		return
	}
	req, ok := a.approval.Get(r.PathValue("id"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "审批单不存在"})
		return
	}
	// 范围外的单子对这个人等于不存在：用 404 而不是 403，与列表口径一致
	// （403 会告诉受限用户"有这么一条单子"，等于泄露了范围外的动作内容）。
	if !a.approvalInScope(Principal(r), req) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "审批单不存在"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"approval": a.approvalView(r, req)})
}

// canApprove 校验审批权：既要 approval:approve，**也要**该动作本身的权限点。
func (a *API) canApprove(w http.ResponseWriter, r *http.Request, req *approval.Request) bool {
	if !a.checkPerm(w, r, approval.PermApprove) {
		return false
	}
	perm := approval.UnderlyingPerm(req.Kind)
	if perm == "" {
		return true
	}
	p := Principal(r)
	if p == nil {
		// 未启用登录认证时 Principal 为空：与 checkPerm 的既有语义一致（放行）。
		return true
	}
	if !p.HasPermission(perm) {
		RecordPermissionDenied(a.audit, r, p.Username, perm)
		writeJSON(w, http.StatusForbidden, map[string]string{
			"error":      "批准该动作还需要它本身的权限（" + perm + "）：只会点批准的角色不能批准自己都做不到的事",
			"permission": perm,
		})
		return false
	}
	return true
}

// mapApprovalErr 把存储层的错误翻成状态码与可操作的提示。
func (a *API) mapApprovalErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, approval.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	case errors.Is(err, approval.ErrSelfApproval):
		// 这是本能力最核心的规则，被拒本身值得留痕（事后要能回答"谁试过自己批自己"）
		if p := Principal(r); p != nil {
			_ = a.audit.Record(audit.Event{
				User: p.Username, Method: r.Method, Path: r.URL.Path,
				Status: http.StatusForbidden, RemoteIP: audit.ClientIP(r),
				Succeeded: false, RequestID: RequestID(r),
				Category: "approval", Action: "deny-self-approval",
				Detail: "尝试批准自己提交的申请，已拒绝",
			})
		}
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
	case errors.Is(err, approval.ErrNotRequester):
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
	case errors.Is(err, approval.ErrExpired):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	case errors.Is(err, approval.ErrNotPending):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
}

// handleApprovalApprove 批准并**立即执行**。
// POST /api/v1/approvals/{id}/approve
func (a *API) handleApprovalApprove(w http.ResponseWriter, r *http.Request) {
	if !a.approvalReady(w) {
		return
	}
	id := r.PathValue("id")
	req, ok := a.approval.Get(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "审批单不存在"})
		return
	}
	if !a.approvalInScope(Principal(r), req) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "审批单不存在"})
		return
	}
	if !a.canApprove(w, r, req) {
		return
	}
	var body struct {
		Note string `json:"note"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body) // 允许空体（批准可以不写说明）

	approver := AuthenticatedUser(r)
	decided, err := a.approval.Decide(id, approver, true, strings.TrimSpace(body.Note))
	if err != nil {
		a.mapApprovalErr(w, r, err)
		return
	}
	// 批准即执行：审批单自带完整动作，不需要申请人再点一次
	//（否则"批了但没人执行"会变成新的糊涂账）。
	taskID, execErr := a.executeApproved(r, decided)
	final, markErr := a.approval.MarkExecuted(id, taskID, execErr)
	if markErr != nil {
		slog.Warn("审批单执行结果落盘失败", "id", id, "err", markErr)
		final = decided
	}
	if a.audit != nil {
		detail := fmt.Sprintf("批准 %s（%s）：%s%s", id, decided.Title, targetsText(decided), reasonText(decided.Reason))
		if execErr != nil {
			detail += "；执行失败：" + execErr.Error()
		} else if taskID != "" {
			detail += "；已下发任务 " + taskID
		}
		_ = a.audit.Record(audit.Event{
			User: approver, Method: r.Method, Path: r.URL.Path,
			Status: http.StatusOK, RemoteIP: audit.ClientIP(r),
			Succeeded: execErr == nil, RequestID: RequestID(r),
			Category: "approval", Action: "approve",
			Detail: detail,
		})
	}
	resp := map[string]interface{}{"approval": a.approvalView(r, final)}
	if execErr != nil {
		// 批准成功了但动作没发出去：必须说清楚，否则界面显示"已批准"而机器上什么都没发生
		resp["error"] = "审批已通过，但动作下发失败：" + execErr.Error()
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleApprovalReject 驳回（必须写理由——事后回看"为什么不同意"比"谁不同意"更有用）。
// POST /api/v1/approvals/{id}/reject
func (a *API) handleApprovalReject(w http.ResponseWriter, r *http.Request) {
	if !a.approvalReady(w) {
		return
	}
	id := r.PathValue("id")
	req, ok := a.approval.Get(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "审批单不存在"})
		return
	}
	if !a.approvalInScope(Principal(r), req) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "审批单不存在"})
		return
	}
	if !a.canApprove(w, r, req) {
		return
	}
	var body struct {
		Note string `json:"note"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	note := strings.TrimSpace(body.Note)
	if note == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "驳回必须填写理由"})
		return
	}
	approver := AuthenticatedUser(r)
	decided, err := a.approval.Decide(id, approver, false, note)
	if err != nil {
		a.mapApprovalErr(w, r, err)
		return
	}
	if a.audit != nil {
		_ = a.audit.Record(audit.Event{
			User: approver, Method: r.Method, Path: r.URL.Path,
			Status: http.StatusOK, RemoteIP: audit.ClientIP(r),
			Succeeded: true, RequestID: RequestID(r),
			Category: "approval", Action: "reject",
			Detail: fmt.Sprintf("驳回 %s（%s）：%s", id, decided.Title, note),
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"approval": a.approvalView(r, decided)})
}

// handleApprovalCancel 申请人撤回。
// POST /api/v1/approvals/{id}/cancel
func (a *API) handleApprovalCancel(w http.ResponseWriter, r *http.Request) {
	if !a.approvalReady(w) {
		return
	}
	id := r.PathValue("id")
	req, ok := a.approval.Get(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "审批单不存在"})
		return
	}
	actor := AuthenticatedUser(r)
	// 撤回只对自己提交的：别人的单子连存在性都不该暴露
	if req.Requester != actor {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "审批单不存在"})
		return
	}
	cancelled, err := a.approval.Cancel(id, actor)
	if err != nil {
		a.mapApprovalErr(w, r, err)
		return
	}
	if a.audit != nil {
		_ = a.audit.Record(audit.Event{
			User: actor, Method: r.Method, Path: r.URL.Path,
			Status: http.StatusOK, RemoteIP: audit.ClientIP(r),
			Succeeded: true, RequestID: RequestID(r),
			Category: "approval", Action: "cancel",
			Detail: fmt.Sprintf("撤回 %s（%s）", id, cancelled.Title),
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"approval": a.approvalView(r, cancelled)})
}

// handleApprovalConfig 读取审批配置。
// GET /api/v1/approvals/config
func (a *API) handleApprovalConfig(w http.ResponseWriter, r *http.Request) {
	if a.approvalCfg == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"config": approval.DefaultConfig(), "entries": approvalEntryViews(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"config": a.approvalCfg.Get(), "entries": approvalEntryViews(),
	})
}

// handleApprovalConfigSave 保存审批配置（保存即热生效）。
// PUT /api/v1/approvals/config
func (a *API) handleApprovalConfigSave(w http.ResponseWriter, r *http.Request) {
	if a.approvalCfg == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "审批功能未启用"})
		return
	}
	var cfg approval.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体不是合法 JSON"})
		return
	}
	// 不认识的入口一律拒：静默接受会让管理员以为"配上了"，实际那条根本没被拦。
	if bad := unknownEntries(cfg.RequireFor); len(bad) > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "不认识的入口：" + strings.Join(bad, "、") + "（可用：内置入口或 ops:<动作名>）",
		})
		return
	}
	if err := a.approvalCfg.Set(cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "保存失败：" + err.Error()})
		return
	}
	saved := a.approvalCfg.Get()
	if a.audit != nil {
		// 开关本身要留痕：事后要能回答"审批是什么时候被谁关掉的"
		_ = a.audit.Record(audit.Event{
			User: AuthenticatedUser(r), Method: r.Method, Path: r.URL.Path,
			Status: http.StatusOK, RemoteIP: audit.ClientIP(r),
			Succeeded: true, RequestID: RequestID(r),
			Category: "approval", Action: "config",
			Detail: fmt.Sprintf("审批配置：enabled=%v，范围=%v，有效期=%d 分钟",
				saved.Enabled, saved.RequireFor, saved.TTLMinutes),
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"config": saved})
}

// unknownEntries 找出配置里不认识的入口。`ops:<动作名>` 是允许的手写形态（要能在动作目录里查到）。
func unknownEntries(entries []string) []string {
	known := map[string]bool{}
	for _, e := range approval.KnownEntries() {
		known[e] = true
	}
	var bad []string
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" || known[e] {
			continue
		}
		if kind, ok := strings.CutPrefix(e, "ops:"); ok && kind != "write" {
			if _, found := ops.Lookup(kind); found {
				continue
			}
		}
		bad = append(bad, e)
	}
	return bad
}

func approvalEntryViews() []map[string]string {
	out := make([]map[string]string, 0, len(approval.KnownEntries()))
	for _, e := range approval.KnownEntries() {
		out = append(out, map[string]string{
			"value": e, "label": approval.EntryLabel(e), "permission": approval.UnderlyingPerm(e),
		})
	}
	return out
}

// executeApproved 按审批单里的动作分派执行，返回 ops 任务号（非 ops 入口为空）。
//
// **执行时用的是申请人记下的参数与身份**，不是审批人的：动作是申请人要做的，
// 审批人只是同意。ops 任务里的 Operator 也因此是申请人——界面上"谁要做的"与
// 审计里"谁批的"是两个问题，不能混成一个字段。
func (a *API) executeApproved(r *http.Request, req *approval.Request) (string, error) {
	switch req.Kind {
	case approval.EntryDefense:
		return a.executeApprovedDefense(req)
	case approval.EntrySystemUpgrade:
		return a.executeApprovedSystemUpgrade(req)
	case approval.EntryAgentUpgrade:
		return a.executeApprovedAgentUpgrade(req)
	default:
		if a.ops == nil {
			return "", errors.New("下行操作通道未启用")
		}
		if req.Node == "" {
			return "", errors.New("审批单缺少目标节点")
		}
		task, err := a.ops.Create(req.Node, req.OpsKind, req.Params, req.Requester, req.RequesterIP, req.Reason)
		if err != nil {
			return "", err
		}
		return task.ID, nil
	}
}

// executeApprovedDefense 执行一条已批准的处置下发。
func (a *API) executeApprovedDefense(req *approval.Request) (string, error) {
	if a.defenseStore == nil {
		return "", errors.New("防护功能未启用")
	}
	action := req.Extra["action"]
	if action == "" {
		return "", errors.New("审批单缺少处置动作")
	}
	nd, ok := a.nodeMgr.GetNode(req.Node)
	if !ok {
		return "", fmt.Errorf("节点 %s 不存在", req.Node)
	}
	if nd.Status != "online" {
		return "", fmt.Errorf("节点 %s 当前不在线，无法下发防护指令", req.Node)
	}
	if !a.defenseStore.GetCap(req.Node) {
		return "", fmt.Errorf("节点 %s 的 Agent 不支持防护指令", req.Node)
	}
	// 白名单取**申请人的**真实来源 IP：被授权"由谁操作"的是申请人，不是审批人。
	ignoreIPs := []string{"127.0.0.1", "::1"}
	if req.RequesterIP != "" {
		ignoreIPs = append(ignoreIPs, normalizeWhitelistIP(req.RequesterIP))
	}
	cmd := model.DefenseCommand{
		ID:        genDefenseTaskID(req.Node),
		Type:      action,
		Node:      req.Node,
		IgnoreIPs: ignoreIPs,
		CreatedAt: time.Now().UnixMilli(),
	}
	task := a.defenseStore.Create(cmd, req.Requester, req.RequesterIP)
	return task.ID, nil
}

// executeApprovedSystemUpgrade 执行已批准的 Server 升级/回滚。
//
// 这里刻意**先返回任务号、后让 Apply 去重启服务**：升级会把本进程重启，回执多半发不回来，
// 若等它返回再落状态，这条审批单会永远停在"已批准"而说不清到底跑没跑。因此由调用方
// 先落"已执行"，本函数只负责发起（发起失败则如实报错）。
func (a *API) executeApprovedSystemUpgrade(req *approval.Request) (string, error) {
	if a.upgrader == nil {
		return "", errors.New("升级功能未启用")
	}
	if v := req.Extra["version"]; v != "" {
		task, err := a.upgrader.RollbackTo(v, req.Requester)
		if err != nil {
			return "", err
		}
		if task != nil {
			return task.ID, nil
		}
		return "", nil
	}
	task, err := a.upgrader.Apply(req.Requester)
	if err != nil {
		return "", err
	}
	if task != nil {
		return task.ID, nil
	}
	return "", nil
}

// executeApprovedAgentUpgrade 执行已批准的 Agent 升级（单个或批量）。
func (a *API) executeApprovedAgentUpgrade(req *approval.Request) (string, error) {
	if a.nodeMgr == nil {
		return "", errors.New("节点管理未启用")
	}
	targets := req.Targets()
	if len(targets) == 0 {
		return "", errors.New("审批单缺少目标节点")
	}
	queued := 0
	for _, name := range targets {
		nd, ok := a.nodeMgr.GetNode(name)
		if !ok {
			continue
		}
		// 执行时**重新取一次** CDN 里的二进制摘要：审批期间 Server 可能刚升过级，
		// 用申请时的旧摘要会把节点升到一个已经不存在的版本。
		binPath := filepath.Join(a.agentBinDir, "agent", "linux", nd.Arch, "agent")
		sha, err := fileSHA256(binPath)
		if err != nil {
			slog.Warn("审批执行：CDN 缺少该架构 agent 二进制", "node", name, "arch", nd.Arch, "err", err)
			continue
		}
		a.nodeMgr.RequestUpgrade(name, sha, version.Version)
		queued++
	}
	if queued == 0 {
		return "", errors.New("没有任何节点被标记升级（节点不存在或 CDN 缺少对应架构的二进制）")
	}
	return fmt.Sprintf("agent-upgrade:%d", queued), nil
}
