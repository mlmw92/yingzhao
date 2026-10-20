package api

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/nebula/monitor/internal/model"
	"github.com/nebula/monitor/internal/server/ai"
	"github.com/nebula/monitor/internal/server/logstore"
)

// apiToolbox 把平台已有的只读数据暴露给模型。
// 所有范围收窄都在这里完成——模型看到的参数不可信。
type apiToolbox struct {
	a     *API
	scope ai.ToolScope
}

func (a *API) newToolbox(scope ai.ToolScope) ai.Toolbox {
	return &apiToolbox{a: a, scope: scope}
}

func (t *apiToolbox) Tools() []ai.Tool {
	return []ai.Tool{
		t.listAlerts(), t.queryMetric(), t.nodeDetail(), t.assetLookup(),
		t.inspectFindings(), t.dialtestStatus(), t.logStats(),
	}
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
