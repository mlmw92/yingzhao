package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nebula/monitor/internal/model"
	"github.com/nebula/monitor/internal/server/ai"
	"github.com/nebula/monitor/internal/server/analysis"
	"github.com/nebula/monitor/internal/server/asset"
	"github.com/nebula/monitor/internal/server/auth"
)

// AI 诊断：把平台**已有**的数据组装成上下文交给模型，返回排查建议。
// 设计件：docs/superpowers/specs/2026-10-08-ai-assistant-design.md
//
// 三条硬规则：
//  1. **上下文在服务端组装**——前端拼意味着它可以塞任意内容，范围校验也会失守；
//  2. **范围判定在取数之前**（不是先取全量再过滤）——与报告章节那次同一条道理：
//     `ai:invoke` 不能成为越权读取的旁路；
//  3. **只给建议**：不改任何状态（与既有「智能分析与预测」的边界一致）。
//
// 主机诊断**直接用 analysis 的结论**而不自己查指标：那套结论（MAD 基线、容量预测、风险评分、
// 覆盖度）是平台已有且有用例覆盖的产物；另查一遍只会得到"两份说法"。

type aiDiagnoseRequest struct {
	Kind string `json:"kind"` // node | asset | alert
	ID   string `json:"id"`   // node=主机名；asset=资产 ID；alert=活跃告警 ID
}

type aiDiagnoseResponse struct {
	Advice    string `json:"advice"`
	Model     string `json:"model"`
	Tokens    int    `json:"totalTokens"`
	ElapsedMs int64  `json:"elapsedMs"`
	Truncated bool   `json:"truncated"`
	// Sensitive 表示本次**真的**发了高级别内容（属性值等）——界面要显眼提示。
	Sensitive bool `json:"sensitive"`
	// NotSent 是"因敏感度被整段丢弃"的段标题：让用户知道模型没看到什么，
	// 否则他会奇怪"为什么它没提到我的 IP"。
	NotSent []string `json:"notSent,omitempty"`
}

func (a *API) handleAIDiagnose(w http.ResponseWriter, r *http.Request) {
	if a.aiMgr == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "AI 配置未初始化"})
		return
	}
	cfg := a.aiMgr.Get()
	if !cfg.Ready() {
		// 明确说"未配置"并指路，而不是发一个必然失败的请求。
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "AI 未配置或未启用，请先在「系统设置 → AI 助手」里配置模型",
		})
		return
	}
	var req aiDiagnoseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体解析失败: " + err.Error()})
		return
	}

	ctx, err := a.buildDiagnosticContext(Principal(r), req, cfg.AllowSensitive)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	ctx.Actor = AuthenticatedUser(r)

	messages, truncated := ai.BuildMessages(ctx, cfg.AllowSensitive)
	client := ai.NewClient(time.Duration(cfg.TimeoutSeconds) * time.Second)
	res, err := client.Chat(cfg, messages)
	if err != nil {
		a.recordAI(r, "ai:diagnose", fmt.Sprintf("失败（%s %s）：%s", ctx.Kind, ctx.Subject, err.Error()))
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	// 审计：谁、对什么对象、发往哪个模型、发了多少、**是否含高级别内容**。
	// 数据出境这件事，事后唯一能回答的就是这条记录。
	a.recordAI(r, "ai:diagnose", fmt.Sprintf(
		"对象=%s/%s 模型=%s 上下文=%d字符 截断=%v 含敏感内容=%v 耗时=%dms token=%d",
		ctx.Kind, ctx.Subject, res.Model, len(messages[1].Content), truncated,
		ctx.Sensitive, res.Elapsed.Milliseconds(), res.TotalTokens))

	// notSent 回答"模型没看到什么"，两种情况都要说：
	//   - 因未开启敏感内容而**根本没取**的（第一道防线，正常情况）；
	//   - 取到了但在组装时被过滤的（第二道防线，正常不该发生，留作兜底）。
	notSent := append([]string{}, ctx.WithheldTitles...)
	if !cfg.AllowSensitive {
		notSent = append(notSent, ai.HighLevelTitles(ctx)...)
	}
	writeJSON(w, http.StatusOK, aiDiagnoseResponse{
		Advice: res.Content, Model: res.Model, Tokens: res.TotalTokens,
		ElapsedMs: res.Elapsed.Milliseconds(), Truncated: truncated,
		Sensitive: ctx.Sensitive, NotSent: notSent,
	})
}

// buildDiagnosticContext 按对象类型取数并组装上下文。
//
// allowSensitive 从配置透传下来：**不打开时高级别内容根本不取**（第一道防线），
// BuildMessages 里的过滤是第二道。
func (a *API) buildDiagnosticContext(p *auth.Principal, req aiDiagnoseRequest, allowSensitive bool) (ai.DiagnosticContext, error) {
	switch strings.TrimSpace(req.Kind) {
	case "node":
		return a.diagnoseContextNode(p, strings.TrimSpace(req.ID))
	case "asset":
		return a.diagnoseContextAsset(p, strings.TrimSpace(req.ID), allowSensitive)
	case "alert":
		return a.diagnoseContextAlert(p, strings.TrimSpace(req.ID))
	default:
		return ai.DiagnosticContext{}, fmt.Errorf("不支持的对象类型 %q（可选：node / asset / alert）", req.Kind)
	}
}

// ---- 主机 ----

func (a *API) diagnoseContextNode(p *auth.Principal, name string) (ai.DiagnosticContext, error) {
	if name == "" {
		return ai.DiagnosticContext{}, errors.New("缺少主机名")
	}
	// 范围判定**先于取数**：范围外的主机连名字都不该进入上下文。
	if !a.nodeInScope(p, name) {
		return ai.DiagnosticContext{}, errors.New("主机不在你的资源范围内")
	}
	ctx := ai.DiagnosticContext{Kind: "node", Subject: name}

	if a.analysis != nil {
		if hr, ok := a.analysis.Host(name, 7*24*time.Hour, false); ok {
			ctx.Sections = append(ctx.Sections, analysisSections(hr)...)
		}
	}
	if alerts := a.nodeActiveAlerts(name); len(alerts) > 0 {
		ctx.Sections = append(ctx.Sections, ai.Section{
			Title: "当前活跃告警", Level: ai.LevelMid, Lines: alerts,
		})
	}
	if a.assets != nil {
		if item, ok, err := a.assets.GetHostByName(name); err == nil && ok {
			ctx.Sections = append(ctx.Sections, assetBriefSections(item)...)
		}
	}
	return ctx, nil
}

// analysisSections 把智能分析的主机结论翻译成上下文段。
func analysisSections(hr analysis.HostResult) []ai.Section {
	out := []ai.Section{{
		Title: "主机概况",
		Level: ai.LevelLow,
		Lines: []string{
			fmt.Sprintf("在线：%v；分组：%s", hr.Online, dash(hr.Group)),
			fmt.Sprintf("风险评分：%d（%s）", hr.Score, hr.Severity),
			fmt.Sprintf("风险类型：%s", dash(strings.Join(hr.RiskTypes, "、"))),
		},
	}}

	// 样本不足必须显眼：把"没数据"读成"没问题"是监控类产品最典型的误导。
	if !hr.Coverage.Ready {
		out = append(out, ai.Section{
			Title: "数据覆盖度（样本不足）",
			Level: ai.LevelMid,
			Lines: []string{
				fmt.Sprintf("说明：%s", dash(hr.Coverage.Message)),
				fmt.Sprintf("缺失指标：%s", dash(strings.Join(hr.Coverage.MissingMetrics, "、"))),
			},
		})
	}

	if len(hr.Evidence) > 0 {
		lines := make([]string, 0, len(hr.Evidence))
		for _, e := range hr.Evidence {
			lines = append(lines, fmt.Sprintf("[%s] %s：%s（平台建议：%s）", e.Severity, e.Title, e.Detail, e.Suggestion))
		}
		out = append(out, ai.Section{Title: "风险证据（平台已识别）", Level: ai.LevelMid, Lines: lines})
	}

	if len(hr.Forecasts) > 0 {
		lines := make([]string, 0, len(hr.Forecasts))
		for _, f := range hr.Forecasts {
			s := fmt.Sprintf("%s：当前 %.2f%s，日增 %.2f", f.MetricTitle, f.Latest, f.Unit, f.RatePerDay)
			if f.DaysRemaining > 0 {
				s += fmt.Sprintf("，预计 %.1f 天后打满", f.DaysRemaining)
			}
			if f.Summary != "" {
				s += "（" + f.Summary + "）"
			}
			if f.Reason != "" {
				s += "（不可预测原因：" + f.Reason + "）"
			}
			lines = append(lines, s)
		}
		out = append(out, ai.Section{Title: "容量预测", Level: ai.LevelMid, Lines: lines})
	}

	anomalies := make([]string, 0, len(hr.Baselines))
	for _, b := range hr.Baselines {
		if !b.IsAnomalous {
			continue
		}
		anomalies = append(anomalies, fmt.Sprintf("%s：当前 %.2f 偏离正常区间 [%.2f, %.2f]（中位数 %.2f，偏离分 %.1f）",
			b.Metric, b.Latest, b.Lower, b.Upper, b.Median, b.DeviationScore))
	}
	if len(anomalies) > 0 {
		out = append(out, ai.Section{Title: "持续异常（基线偏离）", Level: ai.LevelMid, Lines: anomalies})
	}
	return out
}

// nodeActiveAlerts 取该主机的活跃告警（告警存储没有按节点查询，全量后按范围口径过滤）。
func (a *API) nodeActiveAlerts(node string) []string {
	if a.alerts == nil {
		return nil
	}
	var out []string
	for _, e := range a.alerts.Active() {
		if e.Node != node {
			continue
		}
		out = append(out, fmt.Sprintf("[%v] %s：%s（自 %s）",
			e.Severity, e.RuleName, dash(e.Message), time.UnixMilli(e.StartsAt).Format("2006-01-02 15:04")))
	}
	return out
}

// ---- 资产 ----

func (a *API) diagnoseContextAsset(p *auth.Principal, idStr string, allowSensitive bool) (ai.DiagnosticContext, error) {
	if a.assets == nil {
		return ai.DiagnosticContext{}, errors.New("资产台账未启用")
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return ai.DiagnosticContext{}, errors.New("资产 ID 不合法")
	}
	item, ok, err := a.assets.GetByID(id)
	if err != nil || !ok {
		return ai.DiagnosticContext{}, errors.New("资产不存在")
	}
	// 拿到对象后用**纯判定**（节点分组 + 业务标签两维都要过），不写响应。
	if !a.assetVisible(p, item) {
		return ai.DiagnosticContext{}, errors.New("资产不在你的资源范围内")
	}

	ctx := ai.DiagnosticContext{Kind: "asset", Subject: item.Name}
	ctx.Sections = append(ctx.Sections, assetBriefSections(item)...)

	if recs, err := a.assets.History(asset.Ref{TypeKey: item.TypeKey, NaturalKey: item.NaturalKey}, 10); err == nil && len(recs) > 0 {
		lines := make([]string, 0, len(recs))
		for _, rec := range recs {
			lines = append(lines, fmt.Sprintf("%s %s：%s → %s（来源 %s）",
				time.UnixMilli(rec.At).Format("01-02 15:04"), rec.Field, dash(rec.Old), dash(rec.New), rec.Source))
		}
		ctx.Sections = append(ctx.Sections, ai.Section{Title: "最近变更", Level: ai.LevelMid, Lines: lines})
	}

	// 属性值属于高级别（可能有连接串/口令）：**不打开就根本不取**（第一道防线）。
	if allowSensitive {
		keys := sortedAttrKeys(item)
		lines := make([]string, 0, len(keys))
		for _, k := range keys {
			v, _ := item.Value(k) // 生效值（人工优先）
			lines = append(lines, fmt.Sprintf("%s = %s", k, v))
		}
		if len(lines) > 0 {
			ctx.Sections = append(ctx.Sections, ai.Section{Title: "属性值（敏感）", Level: ai.LevelHigh, Lines: lines})
			ctx.Sensitive = true
		}
	} else {
		// 没取也要**说出来**：否则用户会奇怪"为什么模型没提到我的连接串"。
		ctx.WithheldTitles = []string{"属性值（未开启「允许发送敏感内容」）"}
	}
	return ctx, nil
}

// ---- 告警 ----

func (a *API) diagnoseContextAlert(p *auth.Principal, id string) (ai.DiagnosticContext, error) {
	if a.alerts == nil {
		return ai.DiagnosticContext{}, errors.New("告警未启用")
	}
	if id == "" {
		return ai.DiagnosticContext{}, errors.New("缺少告警 ID")
	}
	var hit *model.AlertEvent
	for _, e := range a.alerts.Active() {
		if e.ID == id {
			ev := e
			hit = &ev
			break
		}
	}
	if hit == nil {
		// 只支持活跃告警：已恢复的告警没有"现在该怎么排查"这个问题。
		return ai.DiagnosticContext{}, errors.New("告警不存在或已恢复（只支持诊断当前活跃的告警）")
	}
	if !a.nodeInScope(p, hit.Node) {
		return ai.DiagnosticContext{}, errors.New("告警不在你的资源范围内")
	}

	ctx := ai.DiagnosticContext{Kind: "alert", Subject: hit.RuleName}
	ctx.Sections = append(ctx.Sections, ai.Section{
		Title: "告警详情",
		Level: ai.LevelMid,
		Lines: []string{
			fmt.Sprintf("规则：%s（%s）", hit.RuleName, hit.RuleID),
			fmt.Sprintf("级别：%v；状态：%v", hit.Severity, hit.State),
			fmt.Sprintf("对象：%s%s", hit.Node, dash2(hit.Instance)),
			fmt.Sprintf("触发：%s %v %v", hit.Metric, hit.Operator, hit.Threshold),
			fmt.Sprintf("当前值：%v", hit.Value),
			fmt.Sprintf("开始时间：%s", time.UnixMilli(hit.StartsAt).Format("2006-01-02 15:04:05")),
			fmt.Sprintf("说明：%s", dash(hit.Message)),
		},
	})
	if len(hit.Labels) > 0 {
		lines := make([]string, 0, len(hit.Labels))
		for k, v := range hit.Labels {
			lines = append(lines, fmt.Sprintf("%s = %s", k, v))
		}
		ctx.Sections = append(ctx.Sections, ai.Section{Title: "告警标签", Level: ai.LevelLow, Lines: lines})
	}

	// 同主机的其它活跃告警：判断"是不是一个更大的故障的一部分"。
	if others := a.otherActiveAlerts(hit.Node, hit.ID); len(others) > 0 {
		ctx.Sections = append(ctx.Sections, ai.Section{
			Title: "同一主机的其它活跃告警", Level: ai.LevelMid, Lines: others,
		})
	}
	// 该主机的分析结论（有就用，没有也不强求）。
	if a.analysis != nil && hit.Node != "" {
		if hr, ok := a.analysis.Host(hit.Node, 7*24*time.Hour, false); ok {
			ctx.Sections = append(ctx.Sections, analysisSections(hr)...)
		}
	}
	return ctx, nil
}

func (a *API) otherActiveAlerts(node, excludeID string) []string {
	if a.alerts == nil || node == "" {
		return nil
	}
	var out []string
	for _, e := range a.alerts.Active() {
		if e.Node != node || e.ID == excludeID {
			continue
		}
		out = append(out, fmt.Sprintf("[%v] %s：%s", e.Severity, e.RuleName, dash(e.Message)))
	}
	return out
}

// ---- 公共小工具 ----

// assetBriefSections 是资产的"概况 + 字段名清单"。
//
// **只给字段名不给值**（值属于高级别，见 diagnoseContextAsset）：字段名本身对诊断有用
// （"这台机器记录了 CPU 核数、内存、磁盘"），而值可能含连接串与口令。
func assetBriefSections(item asset.Asset) []ai.Section {
	lines := []string{
		fmt.Sprintf("类型：%s；自然键：%s", item.TypeKey, item.NaturalKey),
		fmt.Sprintf("归属节点：%s", dash(item.Node)),
		fmt.Sprintf("最近上报：%s", time.UnixMilli(item.SeenAt).Format("2006-01-02 15:04")),
	}
	if len(item.Labels) > 0 {
		kv := make([]string, 0, len(item.Labels))
		for k, v := range item.Labels {
			kv = append(kv, k+"="+v)
		}
		sort.Strings(kv) // map 遍历顺序随机，排序后两次诊断的上下文才可比
		lines = append(lines, "标签："+strings.Join(kv, "、"))
	}
	keys := sortedAttrKeys(item)
	if len(keys) > 0 {
		lines = append(lines, "已记录字段（值未发送）："+strings.Join(keys, "、"))
	}
	return []ai.Section{{Title: "台账信息", Level: ai.LevelMid, Lines: lines}}
}

// sortedAttrKeys 返回资产的字段名（去重、排序）。
//
// `Attrs` 的键是 `key@source` 形态（同一 key 的采集值与人工值并存），因此要去掉来源后缀——
// 交给模型的应该是"这台机器记录了哪些字段"，而不是平台的内部主键形态。
func sortedAttrKeys(item asset.Asset) []string {
	seen := make(map[string]struct{}, len(item.Attrs))
	out := make([]string, 0, len(item.Attrs))
	for mk, at := range item.Attrs {
		k := at.Key
		if k == "" {
			if i := strings.Index(mk, "@"); i > 0 {
				k = mk[:i]
			} else {
				k = mk
			}
		}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "（未提供）"
	}
	return s
}

func dash2(s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	return " / " + s
}
