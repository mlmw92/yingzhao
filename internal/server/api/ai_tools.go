package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mlmw92/yingzhao/internal/model"
	"github.com/mlmw92/yingzhao/internal/server/ai"
	"github.com/mlmw92/yingzhao/internal/server/logstore"
	"github.com/mlmw92/yingzhao/internal/server/ops"
)

// apiToolbox 把平台已有的只读数据暴露给模型。
// 所有范围收窄都在这里完成——模型看到的参数不可信。
type apiToolbox struct {
	a     *API
	scope ai.ToolScope
	// 建议计数与去重只在本次调查内有效；工具按顺序执行，无需加锁。
	proposals int
	seen      map[string]struct{}
}

func (a *API) newToolbox(scope ai.ToolScope) ai.Toolbox {
	return &apiToolbox{a: a, scope: scope, seen: map[string]struct{}{}}
}

func (t *apiToolbox) Tools() []ai.Tool {
	tools := []ai.Tool{
		t.listAlerts(), t.queryMetric(), t.nodeDetail(), t.assetLookup(),
		t.inspectFindings(), t.dialtestStatus(), t.logStats(),
	}
	if t.scope.Propose != nil {
		tools = append(tools, t.proposeAction())
	}
	return tools
}

// requireScopeNode 校验参数里的 node 不越过 scope。
// 模型可能被提示注入诱导去查别的机器，这里不信任参数。
func (t *apiToolbox) requireScopeNode(args map[string]any) (string, error) {
	node := t.scope.Node
	if v, ok := args["node"].(string); ok && strings.TrimSpace(v) != "" {
		if v != t.scope.Node {
			return "", fmt.Errorf("无权访问节点 %q：本次调查仅限节点 %q", v, t.scope.Node)
		}
	}
	if node == "" {
		return "", fmt.Errorf("缺少节点范围")
	}
	return node, nil
}

func (t *apiToolbox) listAlerts() ai.Tool {
	return ai.Tool{
		Name: "list_alerts", ReadOnly: true,
		Description: "列出该节点当前的活跃告警",
		Schema:      json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		Handler: func(_ context.Context, _ map[string]any) (string, error) {
			if t.a.alerts == nil {
				return "告警存储未启用", nil
			}
			var lines []string
			for _, e := range t.a.alerts.Active() {
				if e.Node != t.scope.Node {
					continue
				}
				lines = append(lines, fmt.Sprintf("[%v] %s：%s（自 %s）", e.Severity, e.RuleName,
					e.Message, time.UnixMilli(e.StartsAt).Format("2006-01-02 15:04")))
			}
			if len(lines) == 0 {
				return "该节点当前没有活跃告警", nil
			}
			return strings.Join(lines, "\n"), nil
		},
	}
}

func (t *apiToolbox) queryMetric() ai.Tool {
	return ai.Tool{
		Name: "query_metric", ReadOnly: true,
		Description: "查询该节点一个指标最近一段时间的序列（最多 200 点）",
		Schema:      json.RawMessage(`{"type":"object","properties":{"metric":{"type":"string"},"range":{"type":"string","description":"如 1h/6h/24h"},"step":{"type":"string","description":"如 60s/5m"}},"required":["metric"]}`),
		Handler: func(_ context.Context, args map[string]any) (string, error) {
			metric, _ := args["metric"].(string)
			if strings.TrimSpace(metric) == "" {
				return "", fmt.Errorf("缺少 metric 参数")
			}
			rng := parseToolRange(args["range"], time.Hour)
			step := parseToolStep(args["step"], time.Minute)
			end := time.Now()
			series, err := t.a.store.QueryRange(t.scope.Node, metric, nil,
				end.Add(-rng).UnixMilli(), end.UnixMilli(), step.Milliseconds())
			if err != nil {
				return "", fmt.Errorf("查询指标 %q 失败: %w", metric, err)
			}
			var b strings.Builder
			fmt.Fprintf(&b, "指标 %s（最近 %s）：\n", metric, rng)
			for _, s := range series {
				pts := s.Points
				if len(pts) > 200 {
					pts = pts[len(pts)-200:]
				}
				b.WriteString("序列 " + s.Labels["instance"] + ": ")
				for i, p := range pts {
					if i > 0 {
						b.WriteString(",")
					}
					fmt.Fprintf(&b, "%d=%.2f", p.Timestamp/1000, p.Value)
				}
				b.WriteString("\n")
			}
			if len(series) == 0 {
				return "没有数据（指标名可能不对，或该节点未采集此项）", nil
			}
			return b.String(), nil
		},
	}
}

func parseToolRange(v any, def time.Duration) time.Duration {
	s, _ := v.(string)
	if d, err := time.ParseDuration(s); err == nil && d > 0 && d <= 7*24*time.Hour {
		return d
	}
	return def
}

func parseToolStep(v any, def time.Duration) time.Duration {
	s, _ := v.(string)
	if d, err := time.ParseDuration(s); err == nil && d > 0 {
		return d
	}
	return def
}

func (t *apiToolbox) nodeDetail() ai.Tool {
	return ai.Tool{
		Name: "node_detail", ReadOnly: true,
		Description: "读取该节点的在线状态、分组、风险评分与智能分析结论",
		Schema:      json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		Handler: func(_ context.Context, _ map[string]any) (string, error) {
			if t.a.nodeMgr == nil {
				return "节点管理器未启用", nil
			}
			n, ok := t.a.nodeMgr.GetNode(t.scope.Node)
			if !ok {
				return "节点不存在", nil
			}
			var b strings.Builder
			fmt.Fprintf(&b, "节点 %s：状态=%s，分组=%s，IP=%s，版本=%s\n",
				n.Hostname, n.Status, n.Group, n.IP, n.Version)
			if t.a.analysis != nil {
				if hr, ok := t.a.analysis.Host(t.scope.Node, time.Hour, false); ok {
					fmt.Fprintf(&b, "风险评分=%d（%s）；风险类型=%s\n", hr.Score, hr.Severity,
						strings.Join(hr.RiskTypes, "、"))
					for _, e := range hr.Evidence {
						fmt.Fprintf(&b, "[%s] %s：%s（建议：%s）\n", e.Severity, e.Title, e.Detail, e.Suggestion)
					}
				}
			}
			return b.String(), nil
		},
	}
}

func (t *apiToolbox) assetLookup() ai.Tool {
	return ai.Tool{
		Name: "asset_lookup", ReadOnly: true,
		Description: "查询节点对应的资产条目（名称、类型、节点、标签）",
		Schema:      json.RawMessage(`{"type":"object","properties":{"node":{"type":"string"}}}`),
		Handler: func(_ context.Context, args map[string]any) (string, error) {
			node, err := t.requireScopeNode(args)
			if err != nil {
				return "", err
			}
			if t.a.assets == nil {
				return "资产服务未启用", nil
			}
			item, ok, err := t.a.assets.GetHostByName(node)
			if err != nil {
				return "", err
			}
			if !ok {
				return "该节点没有资产记录", nil
			}
			return fmt.Sprintf("资产 %s：类型=%s，节点=%s，标签=%v", item.Name, item.TypeKey, item.Node, item.Labels), nil
		},
	}
}

func (t *apiToolbox) inspectFindings() ai.Tool {
	return ai.Tool{
		Name: "inspect_findings", ReadOnly: true,
		Description: "读取该节点最近一次配置巡检的差异项",
		Schema:      json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		Handler: func(_ context.Context, _ map[string]any) (string, error) {
			if t.a.assets == nil {
				return "资产服务未启用", nil
			}
			runs, err := t.a.assets.InspectRunsInNodes(1, []string{t.scope.Node})
			if err != nil || len(runs) == 0 {
				return "没有巡检记录", nil
			}
			findings, _, err := t.a.assets.InspectFindingsInNodes(runs[0].ID, 20, []string{t.scope.Node})
			if err != nil {
				return "", err
			}
			if len(findings) == 0 {
				return "最近一次巡检没有差异项", nil
			}
			var b strings.Builder
			for _, f := range findings {
				fmt.Fprintf(&b, "[%s] %s：期望=%s，实际=%s\n", f.Level, f.Field, f.Expected, f.Actual)
			}
			return b.String(), nil
		},
	}
}

func (t *apiToolbox) dialtestStatus() ai.Tool {
	return ai.Tool{
		Name: "dialtest_status", ReadOnly: true,
		Description: "读取目标指向该节点的拨测任务与最近结果",
		Schema:      json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		Handler: func(_ context.Context, _ map[string]any) (string, error) {
			if t.a.dialtest == nil || t.a.nodeMgr == nil {
				return "拨测或节点管理器未启用", nil
			}
			node, ok := t.a.nodeMgr.GetNode(t.scope.Node)
			if !ok {
				return "节点不存在", nil
			}
			last := t.a.dialtest.LastResults()
			var lines []string
			for _, task := range t.a.dialtest.List() {
				// 拨测任务是全局目标（没有 node 字段）：按目标里是否包含本节点 IP/主机名归属
				if node.IP == "" || (!strings.Contains(task.Target, node.IP) && !strings.Contains(task.Target, node.Hostname)) {
					continue
				}
				r, ok := last[task.ID]
				if !ok {
					lines = append(lines, fmt.Sprintf("%s（%s）：暂无结果", task.Name, task.Target))
					continue
				}
				lines = append(lines, fmt.Sprintf("%s（%s）：up=%v，延迟=%.1fms", task.Name, task.Target, r.Up, r.Latency))
			}
			if len(lines) == 0 {
				return "该节点没有直接相关的拨测任务", nil
			}
			sort.Strings(lines)
			return strings.Join(lines, "\n"), nil
		},
	}
}

func (t *apiToolbox) logStats() ai.Tool {
	return ai.Tool{
		Name: "log_stats", ReadOnly: true,
		Description: "统计该节点最近的日志来源与条数（只返回统计，不返回日志正文）",
		Schema:      json.RawMessage(`{"type":"object","properties":{"source":{"type":"string"},"window":{"type":"string","description":"如 1h/24h"}}}`),
		Handler: func(_ context.Context, args map[string]any) (string, error) {
			if t.a.logs == nil {
				return "集中日志未启用", nil
			}
			source, _ := args["source"].(string)
			rng := parseToolRange(args["window"], time.Hour)
			now := time.Now()
			q := model.LogQuery{
				From: now.Add(-rng).UnixMilli(), To: now.UnixMilli(),
				Nodes: []string{t.scope.Node}, Limit: 1000,
			}
			if source != "" {
				q.Sources = []string{source}
			}
			res, err := t.a.logs.Query(q, logstore.Cursor{})
			if err != nil {
				return "", err
			}
			var b strings.Builder
			fmt.Fprintf(&b, "最近 %s 共命中 %d 行（统计上限 1000）\n", rng, len(res.Lines))
			counts := map[string]int{}
			for _, l := range res.Lines {
				counts[l.Source]++
			}
			keys := make([]string, 0, len(counts))
			for k := range counts {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Fprintf(&b, "来源 %s：%d 行\n", k, counts[k])
			}
			return b.String(), nil
		},
	}
}

// maxAISuggestions 是单次调查最多保留的建议数。
// 超过 3 条开始变成噪音：审批人要的是可判断的选项，不是清单。
const maxAISuggestions = 3

func (t *apiToolbox) proposeAction() ai.Tool {
	return ai.Tool{
		Name: "propose_action", ReadOnly: true,
		Description: "记录一条处置建议。只接受写类操作；建议不会自动执行，需人工提交并由另一位同事批准。",
		Schema:      json.RawMessage(`{"type":"object","properties":{"kind":{"type":"string","description":"动作标识，如 svc.restart / container.restart"},"params":{"type":"object","additionalProperties":{"type":"string"},"description":"动作参数"},"reason":{"type":"string","description":"建议理由，一句话"}},"required":["kind"]}`),
		Handler: func(_ context.Context, args map[string]any) (string, error) {
			kind, _ := args["kind"].(string)
			kind = strings.TrimSpace(kind)
			action, ok := ops.Lookup(kind)
			if !ok {
				return "", fmt.Errorf("未知动作 %q", kind)
			}
			if action.ReadOnly {
				return "", fmt.Errorf("动作 %s 是只读动作，不需要处置建议", kind)
			}
			if kind == ops.KindFilePush {
				return "", errors.New("不支持建议文件分发：文件引用只能来自一次上传")
			}
			params, err := stringParams(args["params"])
			if err != nil {
				return "", err
			}
			action, norm, err := ops.Validate(kind, params)
			if err != nil {
				return "", err
			}
			// 能力协商前移到建议阶段：目标机器没放行的动作不要提，
			// 否则会记录一条注定执行失败的建议，白占一次人审。
			if t.a.ops != nil {
				if err := t.a.ops.CheckSupport(t.scope.Node, action); err != nil {
					return "", err
				}
			}
			if t.proposals >= maxAISuggestions {
				return "", fmt.Errorf("每条调查最多 %d 条建议", maxAISuggestions)
			}
			key := suggestionKey(kind, norm)
			if _, dup := t.seen[key]; dup {
				return "", errors.New("已经建议过完全相同的动作")
			}
			t.seen[key] = struct{}{}
			t.proposals++

			reason, _ := args["reason"].(string)
			reason = strings.TrimSpace(reason)
			if r := []rune(reason); len(r) > 300 {
				reason = string(r[:300]) + "…"
			}
			s := ai.Suggestion{
				ID: fmt.Sprintf("sug-%d", t.proposals), Kind: kind, Title: action.Title,
				Params: norm, Node: t.scope.Node, Reason: reason,
			}
			if err := t.scope.Propose(s); err != nil {
				return "", err
			}
			return fmt.Sprintf("已记录建议：%s @ %s\n（不会自动执行；需人工提交，并由另一位同事批准）",
				action.Title, t.scope.Node), nil
		},
	}
}

// stringParams 把模型给的 JSON 对象转成动作参数。
// 非字符串值一律拒绝：ops.Validate 只接受字符串，静默转换会让「1」和「"1"」看起来一样。
func stringParams(v any) (map[string]string, error) {
	out := map[string]string{}
	if v == nil {
		return out, nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("params 必须是对象")
	}
	for k, raw := range m {
		s, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("参数 %s 必须是字符串", k)
		}
		out[k] = s
	}
	return out, nil
}

// suggestionKey 用于去重：参数顺序不影响结果。
func suggestionKey(kind string, params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(kind)
	for _, k := range keys {
		b.WriteString("|" + k + "=" + params[k])
	}
	return b.String()
}
