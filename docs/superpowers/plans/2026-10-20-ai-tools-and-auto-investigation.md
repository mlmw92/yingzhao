# AI 只读工具层与告警自动调查 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 给模型 7 个只读工具与一个带预算的 Agent 循环，并在 critical 告警触发时自动调查，把结论落到告警详情页。

**Architecture:** `ai` 包新增 Tool/Toolbox 抽象、协议适配（native/json/auto）、Agent 循环与调查存储；工具实现放在 `api` 层（它持有各数据源），通过工厂按节点范围注入。自动调查器轮询活跃 critical 告警，去重后并发受限地跑 Agent。

**Tech Stack:** Go（标准库 + 现有 OpenAI 兼容客户端）、Vue 3 + Element Plus、Vitest、httptest 假模型服务。

**Spec:** `docs/superpowers/specs/2026-10-20-ai-tools-and-auto-investigation-design.md`

## Global Constraints

- 所有工具**只读**；不提供任何写操作。
- 工具在服务端按 `ToolScope.Node` 强制收窄；模型传入越界节点必须返回错误，不得返回数据。
- `log_stats` 只返回来源与条数，**永不返回日志正文**。
- 预算：4 轮 / 8 次工具调用 / 8000 tokens / 30 秒；全局并发 2。
- 自动调查仅 `critical`；同一 `RuleID|Node` 30 分钟去重。
- `autoInvestigate` 默认 `false`；不开启时整条链路不启动。
- 工具执行失败不终止调查，错误作为工具结果回填给模型。
- 中文注释；注释只写「为什么」。

## Review Focus

- **越界读取**：模型在参数里填了另一个节点，工具必须拒绝而不是返回数据。
- **日志正文泄漏**：`log_stats` 的任何字段都不能包含日志行内容。
- **一次失败废掉调查**：模型返回空内容或单个工具报错时，调查要继续或给出可读结论，不能整体失败。
- **费用失控**：轮次/工具/tokens/并发/去重任一失效都会造成持续计费。
- **未开启也发数据**：`autoInvestigate=false` 或 `ai.enabled=false` 时不得有任何出网请求。

---

## Task 1: 工具抽象与 7 个只读工具

**Files:**
- Create: `internal/server/ai/tools.go`
- Create: `internal/server/api/ai_tools.go`
- Create: `internal/server/api/ai_tools_test.go`

**Interfaces:**
- Produces: `ai.Tool`、`ai.ToolScope`、`ai.Toolbox`；`(*API).newToolbox(scope ai.ToolScope) ai.Toolbox`。
- Consumes: `a.store`、`a.alerts`、`a.assets`、`a.dialtest`、`a.logs`、`a.nodeMgr`、`a.analysis`、`a.nodeInScope`。

- [ ] **Step 1: 写失败测试**

```go
package api

import (
	"context"
	"strings"
	"testing"

	"github.com/mlmw92/yingzhao/internal/model"
	"github.com/mlmw92/yingzhao/internal/server/ai"
	"github.com/mlmw92/yingzhao/internal/server/logstore"
)

func toolByName(t *testing.T, tb interface{ Tools() []ai.Tool }, name string) ai.Tool {
	t.Helper()
	for _, tool := range tb.Tools() {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("缺少工具 %s", name)
	return ai.Tool{}
}

func TestToolScopeRejectsOtherNode(t *testing.T) {
	a := scopeTestAPI(t)
	a.alerts = &alertStoreStub{}
	tb := a.newToolbox(ai.ToolScope{Node: "web-01"})

	tool := toolByName(t, tb, "asset_lookup")
	out, err := tool.Handler(context.Background(), map[string]any{"node": "db-01"})
	if err == nil {
		t.Fatalf("越界节点必须报错，实际返回 %q", out)
	}
}

func TestLogStatsNeverReturnsBody(t *testing.T) {
	a := scopeTestAPI(t)
	a.logs = &fakeLogStore{}
	tb := a.newToolbox(ai.ToolScope{Node: "web-01"})

	tool := toolByName(t, tb, "log_stats")
	out, err := tool.Handler(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("log_stats: %v", err)
	}
	if strings.Contains(out, "hunter2") || strings.Contains(out, "SECRET") {
		t.Fatalf("log_stats 不得返回日志正文: %q", out)
	}
	if !strings.Contains(out, "app") {
		t.Fatalf("应包含来源名: %q", out)
	}
}

func TestSevenToolsRegistered(t *testing.T) {
	a := scopeTestAPI(t)
	tb := a.newToolbox(ai.ToolScope{Node: "web-01"})
	want := []string{"list_alerts", "query_metric", "node_detail", "asset_lookup", "inspect_findings", "dialtest_status", "log_stats"}
	for _, name := range want {
		toolByName(t, tb, name)
	}
}
```

`fakeLogStore` 需要实现 `logstore.LogStore`（其余方法返回零值）：

```go
type fakeLogStore struct {
	lines []model.LogLine
}

func (f *fakeLogStore) Append(model.LogBatch) (int, int, string, error) { return 0, 0, "", nil }
func (f *fakeLogStore) Query(q model.LogQuery, _ logstore.Cursor) (model.LogQueryResult, error) {
	return model.LogQueryResult{Lines: []model.LogHit{{Source: "app", Node: "web-01", Text: "SECRET password=hunter2"}}}, nil
}
func (f *fakeLogStore) Backend() string                 { return "fake" }
func (f *fakeLogStore) Sources() []string               { return []string{"app"} }
func (f *fakeLogStore) FieldNames(string) []string      { return nil }
func (f *fakeLogStore) Capability() logstore.Capability { return logstore.Capability{} }
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/server/api/ -run 'TestTool|TestLogStats|TestSevenTools' -v`
Expected: FAIL（`ai.Tool` / `newToolbox` 未定义）。

- [ ] **Step 3: 实现工具抽象**

`internal/server/ai/tools.go`：

```go
package ai

import (
	"context"
	"encoding/json"
)

// Tool 是一个只读工具：Description 与 Schema 给模型看，Handler 在服务端执行。
type Tool struct {
	Name        string
	Description string
	Schema      json.RawMessage
	ReadOnly    bool
	Handler     func(ctx context.Context, args map[string]any) (string, error)
}

// ToolScope 是工具可访问的范围。自动调查按告警节点构造；
// 工具实现必须相信这个字段，而不是相信模型传进来的参数。
type ToolScope struct {
	Node           string
	AllowSensitive bool
}

// Toolbox 按范围提供工具。每次调查构造一个实例。
type Toolbox interface {
	Tools() []Tool
}
```

- [ ] **Step 4: 实现 `ai_tools.go`**

```go
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mlmw92/yingzhao/internal/model"
	"github.com/mlmw92/yingzhao/internal/server/ai"
	"github.com/mlmw92/yingzhao/internal/server/logstore"
)

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
		Schema: json.RawMessage(`{"type":"object","properties":{"metric":{"type":"string"},"range":{"type":"string","description":"如 1h/6h/24h"},"step":{"type":"string","description":"如 60s/5m"}},"required":["metric"]}`),
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
					fmt.Fprintf(&b, "%d", p.Timestamp/1000)
					b.WriteString("=")
					fmt.Fprintf(&b, "%.2f", p.Value)
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
		Description: "查询节点对应的资产条目（名称、类型、分组、标签）",
		Schema: json.RawMessage(`{"type":"object","properties":{"node":{"type":"string"}}}`),
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
		Description: "读取该节点上的拨测任务与最近结果",
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
		Schema: json.RawMessage(`{"type":"object","properties":{"source":{"type":"string"},"window":{"type":"string","description":"如 1h/24h"}}}`),
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
```

- [ ] **Step 5: 运行测试**

Run: `go test ./internal/server/api/ -run 'TestTool|TestLogStats|TestSevenTools' -v`
Expected: PASS。

- [ ] **Step 6: 提交**

```bash
git add internal/server/ai/tools.go internal/server/api/ai_tools.go internal/server/api/ai_tools_test.go
git commit -m "feat(ai): 只读工具层与 7 个工具"
```

---

## Task 2: 客户端工具协议与 Agent 循环

**Files:**
- Modify: `internal/server/ai/client.go`
- Create: `internal/server/ai/agent.go`
- Create: `internal/server/ai/agent_test.go`
- Modify: `internal/server/config/config.go`（AIConfig 增加 `ToolMode`）

**Interfaces:**
- Produces: `ai.Run(ctx, cfg, system, user, toolbox, budget) (Outcome, error)`、`ai.Budget`、`ai.Outcome`、`ai.Step`。
- Consumes: `ai.Tool`、`ai.Toolbox`（Task 1）。

- [ ] **Step 1: 写失败测试（假模型服务）**

```go
package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mlmw92/yingzhao/internal/server/config"
)

func fakeChat(t *testing.T, handler func(call int, body map[string]any) map[string]any) *httptest.Server {
	t.Helper()
	call := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		call++
		json.NewEncoder(w).Encode(handler(call, body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func testToolbox() Toolbox {
	return staticToolbox{tools: []Tool{{
		Name: "echo", Description: "echo", ReadOnly: true,
		Schema: json.RawMessage(`{"type":"object"}`),
		Handler: func(_ context.Context, args map[string]any) (string, error) {
			return "echo:" + args["q"].(string), nil
		},
	}}}
}

type staticToolbox struct{ tools []Tool }

func (s staticToolbox) Tools() []Tool { return s.tools }

func TestRunNativeToolThenFinal(t *testing.T) {
	srv := fakeChat(t, func(call int, _ map[string]any) map[string]any {
		if call == 1 {
			return map[string]any{"choices": []any{map[string]any{
				"message": map[string]any{
					"role": "assistant",
					"tool_calls": []any{map[string]any{
						"id": "c1", "type": "function",
						"function": map[string]any{"name": "echo", "arguments": `{"q":"hi"}`},
					}},
				},
			}}, "usage": map[string]any{"total_tokens": 10}}
		}
		return map[string]any{"choices": []any{map[string]any{
			"message": map[string]any{"role": "assistant", "content": "结论：正常"},
		}}, "usage": map[string]any{"total_tokens": 5}}
	})
	cfg := config.AIConfig{Enabled: true, BaseURL: srv.URL, Model: "m", ToolMode: "native"}
	out, err := Run(context.Background(), cfg, "sys", "user", testToolbox(), Budget{MaxRounds: 4, MaxToolCalls: 8, MaxTokens: 8000})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Answer != "结论：正常" || len(out.Steps) != 1 || out.Steps[0].Result != "echo:hi" {
		t.Fatalf("outcome = %+v", out)
	}
	if out.Tokens != 15 || out.Stopped != "done" {
		t.Fatalf("预算统计错误: %+v", out)
	}
}

func TestRunJSONProtocol(t *testing.T) {
	srv := fakeChat(t, func(call int, _ map[string]any) map[string]any {
		if call == 1 {
			return map[string]any{"choices": []any{map[string]any{
				"message": map[string]any{"role": "assistant",
					"content": `{"action":"tool","tool":"echo","args":{"q":"hi"}}`},
			}}}
		}
		return map[string]any{"choices": []any{map[string]any{
			"message": map[string]any{"role": "assistant",
				"content": `{"action":"final","answer":"结论：正常"}`},
		}}}
	})
	cfg := config.AIConfig{Enabled: true, BaseURL: srv.URL, Model: "m", ToolMode: "json"}
	out, err := Run(context.Background(), cfg, "sys", "user", testToolbox(), Budget{MaxRounds: 4, MaxToolCalls: 8, MaxTokens: 8000})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Answer != "结论：正常" || len(out.Steps) != 1 {
		t.Fatalf("outcome = %+v", out)
	}
}

func TestRunStopsAtToolBudget(t *testing.T) {
	srv := fakeChat(t, func(int, map[string]any) map[string]any {
		return map[string]any{"choices": []any{map[string]any{
			"message": map[string]any{"role": "assistant", "content": `{"action":"tool","tool":"echo","args":{"q":"x"}}`},
		}}}
	})
	cfg := config.AIConfig{Enabled: true, BaseURL: srv.URL, Model: "m", ToolMode: "json"}
	out, err := Run(context.Background(), cfg, "sys", "user", testToolbox(), Budget{MaxRounds: 10, MaxToolCalls: 2, MaxTokens: 8000})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Stopped != "tools" || len(out.Steps) != 2 {
		t.Fatalf("应在 2 次工具调用后停止: %+v", out)
	}
}

func TestRunToolErrorContinues(t *testing.T) {
	srv := fakeChat(t, func(call int, _ map[string]any) map[string]any {
		if call == 1 {
			return map[string]any{"choices": []any{map[string]any{
				"message": map[string]any{"role": "assistant",
					"content": `{"action":"tool","tool":"nope","args":{}}`},
			}}}
		}
		return map[string]any{"choices": []any{map[string]any{
			"message": map[string]any{"role": "assistant",
				"content": `{"action":"final","answer":"按已有信息给出结论"}`},
		}}}
	})
	cfg := config.AIConfig{Enabled: true, BaseURL: srv.URL, Model: "m", ToolMode: "json"}
	out, err := Run(context.Background(), cfg, "sys", "user", testToolbox(), Budget{})
	if err != nil {
		t.Fatalf("工具报错不应终止调查: %v", err)
	}
	if len(out.Steps) != 1 || out.Steps[0].Err == "" || out.Answer == "" {
		t.Fatalf("工具错误应回填并继续: %+v", out)
	}
}

func TestRunStopsAtTokenBudget(t *testing.T) {
	srv := fakeChat(t, func(int, map[string]any) map[string]any {
		return map[string]any{
			"choices": []any{map[string]any{
				"message": map[string]any{"role": "assistant",
					"content": `{"action":"tool","tool":"echo","args":{"q":"x"}}`},
			}},
			"usage": map[string]any{"total_tokens": 5000},
		}
	})
	cfg := config.AIConfig{Enabled: true, BaseURL: srv.URL, Model: "m", ToolMode: "json"}
	out, err := Run(context.Background(), cfg, "sys", "user", testToolbox(), Budget{MaxRounds: 10, MaxToolCalls: 10, MaxTokens: 8000})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Stopped != "tokens" {
		t.Fatalf("token 超限应停止，实际 %+v", out)
	}
}

func TestRunJSONParseFailureBecomesAnswer(t *testing.T) {
	srv := fakeChat(t, func(int, map[string]any) map[string]any {
		return map[string]any{"choices": []any{map[string]any{
			"message": map[string]any{"role": "assistant", "content": "我无法确定，但建议检查磁盘"},
		}}}
	})
	cfg := config.AIConfig{Enabled: true, BaseURL: srv.URL, Model: "m", ToolMode: "json"}
	out, err := Run(context.Background(), cfg, "sys", "user", testToolbox(), Budget{MaxRounds: 4, MaxToolCalls: 8, MaxTokens: 8000})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Answer != "我无法确定，但建议检查磁盘" || out.Stopped != "done" {
		t.Fatalf("解析失败应按最终结论处理: %+v", out)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/server/ai/ -run TestRun -v`
Expected: FAIL（`Run` / `ToolMode` 未定义）。

- [ ] **Step 3: 扩展客户端**

`client.go` 增加：

```go
// ToolCall 是模型请求调用工具（OpenAI 兼容格式）。
type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolSpec 是发给模型的原生工具声明。
type ToolSpec struct {
	Type     string `json:"type"` // 固定 "function"
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}
```

`Result` 增加 `ToolCalls []ToolCall`。`chatRequest` 增加 `Tools []ToolSpec json:"tools,omitempty"`。`Chat` 改为调用 `ChatWithTools(cfg, messages, nil)`；`ChatWithTools` 是原 `Chat` 的实现加上工具字段，并把「空回答」判定改成「既无 content 又无 tool_calls 才算空」。

- [ ] **Step 4: 实现 Agent**

`agent.go`：

```go
package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mlmw92/yingzhao/internal/server/config"
)

type Step struct {
	Tool   string
	Args   map[string]any
	Result string
	Err    string
}

type Budget struct {
	MaxRounds    int
	MaxToolCalls int
	MaxTokens    int
	Timeout      time.Duration
}

type Outcome struct {
	Answer   string
	Steps    []Step
	Tokens   int
	Rounds   int
	Stopped  string
	Protocol string
}

func normalizeBudget(b Budget) Budget {
	if b.MaxRounds <= 0 { b.MaxRounds = 4 }
	if b.MaxToolCalls <= 0 { b.MaxToolCalls = 8 }
	if b.MaxTokens <= 0 { b.MaxTokens = 8000 }
	if b.Timeout <= 0 { b.Timeout = 30 * time.Second }
	return b
}

func toolSpecs(tools []Tool) []ToolSpec {
	out := make([]ToolSpec, 0, len(tools))
	for _, t := range tools {
		var spec ToolSpec
		spec.Type = "function"
		spec.Function.Name = t.Name
		spec.Function.Description = t.Description
		spec.Function.Parameters = t.Schema
		out = append(out, spec)
	}
	return out
}

func findTool(tools []Tool, name string) (Tool, bool) {
	for _, t := range tools {
		if t.Name == name {
			return t, true
		}
	}
	return Tool{}, false
}

// jsonToolPrompt 把工具清单写进系统提示（JSON 协议用）。
func jsonToolPrompt(tools []Tool, system string) string {
	var b strings.Builder
	b.WriteString(system)
	b.WriteString("\n\n可调用工具（只读）：\n")
	for _, t := range tools {
		fmt.Fprintf(&b, "- %s：%s；参数 JSON Schema：%s\n", t.Name, t.Description, string(t.Schema))
	}
	b.WriteString("\n每次只输出一个 JSON：\n")
	b.WriteString(`{"action":"tool","tool":"工具名","args":{...}}` + "\n")
	b.WriteString(`{"action":"final","answer":"结论"}` + "\n")
	b.WriteString("不要输出 JSON 之外的任何文字。\n")
	return b.String()
}

type jsonEnvelope struct {
	Action string         `json:"action"`
	Tool   string         `json:"tool"`
	Args   map[string]any `json:"args"`
	Answer string         `json:"answer"`
}

// Run 跑一次带工具的调查。
func Run(ctx context.Context, cfg config.AIConfig, system, user string, toolbox Toolbox, budget Budget) (Outcome, error) {
	budget = normalizeBudget(budget)
	cfg = cfg.Normalize()
	tools := toolbox.Tools()
	mode := strings.ToLower(strings.TrimSpace(cfg.ToolMode))
	if mode == "" { mode = "auto" }

	ctx, cancel := context.WithTimeout(ctx, budget.Timeout)
	defer cancel()

	out := Outcome{Protocol: mode, Steps: []Step{}}
	msgs := []Message{{Role: "system", Content: system}, {Role: "user", Content: user}}
	client := NewClient(time.Duration(cfg.TimeoutSeconds) * time.Second)
	toolCalls := 0
	lastContent := ""

	callModel := func(native bool) (*Result, error) {
		if !native {
			sys := jsonToolPrompt(tools, system)
			msgs[0] = Message{Role: "system", Content: sys}
			return client.Chat(cfg, msgs)
		}
		return client.ChatWithTools(cfg, msgs, toolSpecs(tools))
	}

	native := mode == "native" || mode == "auto"
	for round := 0; round < budget.MaxRounds; round++ {
		if ctx.Err() != nil {
			out.Stopped = "timeout"
			break
		}
		res, err := callModel(native)
		if err != nil {
			if native && mode == "auto" && round == 0 && looksLikeToolUnsupported(err) {
				native = false
				out.Protocol = "json"
				continue
			}
			if len(out.Steps) == 0 {
				return out, err
			}
			out.Answer = "调查中断：模型调用失败。已获得的工具结果见步骤列表。"
			out.Stopped = "model_error"
			return out, nil
		}
		out.Tokens += res.TotalTokens
		out.Rounds = round + 1
		if res.Content != "" {
			lastContent = res.Content
		}

		if native {
			if len(res.ToolCalls) == 0 {
				out.Answer = res.Content
				out.Stopped = "done"
				return out, nil
			}
			msgs = append(msgs, Message{Role: "assistant", Content: res.Content, ToolCalls: res.ToolCalls})
			for _, tc := range res.ToolCalls {
				if toolCalls >= budget.MaxToolCalls {
					out.Stopped = "tools"
					out.Answer = lastContent
					return out, nil
				}
				toolCalls++
				step := executeTool(ctx, tools, tc.Function.Name, parseArgs(tc.Function.Arguments))
				out.Steps = append(out.Steps, step)
				msgs = append(msgs, Message{Role: "tool", ToolCallID: tc.ID, Content: stepContent(step)})
			}
			if out.Tokens >= budget.MaxTokens {
				out.Stopped = "tokens"
				out.Answer = lastContent
				return out, nil
			}
			continue
		}

		// JSON 协议
		env, ok := parseEnvelope(res.Content)
		if !ok || env.Action != "tool" {
			out.Answer = res.Content // 解析失败按结论处理
			out.Stopped = "done"
			return out, nil
		}
		if toolCalls >= budget.MaxToolCalls {
			out.Stopped = "tools"
			out.Answer = lastContent
			return out, nil
		}
		toolCalls++
		step := executeTool(ctx, tools, env.Tool, env.Args)
		out.Steps = append(out.Steps, step)
		msgs = append(msgs, Message{Role: "assistant", Content: res.Content})
		msgs = append(msgs, Message{Role: "user", Content: "工具结果：\n" + stepContent(step)})
		if out.Tokens >= budget.MaxTokens {
			out.Stopped = "tokens"
			out.Answer = lastContent
			return out, nil
		}
	}

	if out.Stopped == "" {
		out.Stopped = "rounds"
	}
	out.Answer = lastContent
	return out, nil
}

func parseArgs(raw string) map[string]any {
	out := map[string]any{}
	if strings.TrimSpace(raw) == "" {
		return out
	}
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}

func parseEnvelope(s string) (jsonEnvelope, bool) {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end <= start {
		return jsonEnvelope{}, false
	}
	var env jsonEnvelope
	if err := json.Unmarshal([]byte(s[start:end+1]), &env); err != nil {
		return jsonEnvelope{}, false
	}
	return env, true
}

func looksLikeToolUnsupported(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "返回 4") ||
		strings.Contains(s, "tool") && strings.Contains(s, "unsupported")
}

func executeTool(ctx context.Context, tools []Tool, name string, args map[string]any) Step {
	t, ok := findTool(tools, name)
	if !ok {
		return Step{Tool: name, Args: args, Err: "未知工具"}
	}
	if !t.ReadOnly {
		return Step{Tool: name, Args: args, Err: "非只读工具不允许执行"}
	}
	res, err := t.Handler(ctx, args)
	if err != nil {
		return Step{Tool: name, Args: args, Err: err.Error()}
	}
	if len(res) > 4000 {
		res = res[:4000] + "…（已截断）"
	}
	return Step{Tool: name, Args: args, Result: res}
}

func stepContent(s Step) string {
	if s.Err != "" {
		return "错误：" + s.Err
	}
	return s.Result
}
```

- [ ] **Step 5: 配置增加 `ToolMode`**

`AIConfig` 增加：

```go
	// ToolMode 控制工具调用协议：auto（先原生、失败回退 JSON）| native | json。
	ToolMode string `yaml:"toolMode" json:"toolMode"`
```

`Normalize` 中空值归一为 `auto`。

- [ ] **Step 6: 运行测试**

Run: `go test ./internal/server/ai/ -count=1`
Expected: PASS（含原有 client/manager 用例）。

- [ ] **Step 7: 提交**

```bash
git add internal/server/ai internal/server/config/config.go
git commit -m "feat(ai): 工具调用客户端与带预算的 Agent 循环"
```

---

## Task 3: 调查存储与查询接口

**Files:**
- Create: `internal/server/ai/investigations.go`
- Create: `internal/server/ai/investigations_test.go`
- Create: `internal/server/api/ai_investigation_api.go`
- Create: `internal/server/api/ai_investigation_api_test.go`
- Modify: `internal/server/api/query.go`（路由）

**Interfaces:**
- Produces: `ai.Investigation`、`ai.InvestigationStore`（`Add`、`Update`、`ByAlert`、`List`）。
- Consumes: `ai.Step`。

- [ ] **Step 1: 写失败测试**

```go
package ai

import (
	"path/filepath"
	"testing"
)

func TestInvestigationStoreByAlertAndPrune(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inv.json")
	s := NewInvestigationStore(path, 500, 30*24*time.Hour)
	inv := Investigation{ID: "inv-1", AlertID: "a1", Node: "n1", State: "done", Answer: "ok"}
	if err := s.Add(inv); err != nil {
		t.Fatalf("Add: %v", err)
	}
	got, ok := s.ByAlert("a1")
	if !ok || got.Answer != "ok" {
		t.Fatalf("ByAlert = %+v, %v", got, ok)
	}
	s.Update("inv-1", func(i *Investigation) { i.State = "failed"; i.Error = "x" })
	got, _ = s.ByAlert("a1")
	if got.State != "failed" || got.Error != "x" {
		t.Fatalf("Update 未生效: %+v", got)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/server/ai/ -run TestInvestigation -v`
Expected: FAIL。

- [ ] **Step 3: 实现存储**

```go
package ai

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Investigation struct {
	ID        string `json:"id"`
	AlertID   string `json:"alertId"`
	RuleID    string `json:"ruleId"`
	RuleName  string `json:"ruleName"`
	Node      string `json:"node"`
	State     string `json:"state"` // running|done|failed|skipped
	Stopped   string `json:"stopped,omitempty"`
	Protocol  string `json:"protocol,omitempty"`
	Answer    string `json:"answer,omitempty"`
	Steps     []Step `json:"steps,omitempty"`
	Tokens    int    `json:"tokens,omitempty"`
	Rounds    int    `json:"rounds,omitempty"`
	Error     string `json:"error,omitempty"`
	StartedAt int64  `json:"startedAt"`
	EndedAt   int64  `json:"endedAt,omitempty"`
}

type InvestigationStore struct {
	mu      sync.Mutex
	path    string
	max     int
	keepFor time.Duration
	items   []Investigation
}

func NewInvestigationStore(path string, max int, keepFor time.Duration) *InvestigationStore {
	s := &InvestigationStore{path: path, max: max, keepFor: keepFor}
	s.load()
	return s
}

func (s *InvestigationStore) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	_ = json.Unmarshal(data, &s.items)
}

func (s *InvestigationStore) persistLocked() {
	_ = os.MkdirAll(filepath.Dir(s.path), 0o755)
	data, _ := json.MarshalIndent(s.items, "", "  ")
	_ = os.WriteFile(s.path, data, 0o644)
}

func (s *InvestigationStore) pruneLocked() {
	cutoff := time.Now().Add(-s.keepFor).UnixMilli()
	out := s.items[:0]
	for _, it := range s.items {
		if it.StartedAt >= cutoff {
			out = append(out, it)
		}
	}
	s.items = out
	if s.max > 0 && len(s.items) > s.max {
		s.items = s.items[len(s.items)-s.max:]
	}
}

func (s *InvestigationStore) Add(inv Investigation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = append(s.items, inv)
	s.pruneLocked()
	s.persistLocked()
	return nil
}

func (s *InvestigationStore) Update(id string, fn func(*Investigation)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.items {
		if s.items[i].ID == id {
			fn(&s.items[i])
			s.items[i].EndedAt = time.Now().UnixMilli()
			break
		}
	}
	s.persistLocked()
}

func (s *InvestigationStore) ByAlert(alertID string) (Investigation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.items) - 1; i >= 0; i-- {
		if s.items[i].AlertID == alertID {
			return s.items[i], true
		}
	}
	return Investigation{}, false
}

func (s *InvestigationStore) List(limit int) []Investigation {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 || limit > len(s.items) {
		limit = len(s.items)
	}
	out := make([]Investigation, limit)
	copy(out, s.items[len(s.items)-limit:])
	return out
}
```

- [ ] **Step 4: 查询接口**

`ai_investigation_api.go`：

```go
package api

import (
	"net/http"
)

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
```

`API` 增加字段 `aiInv *ai.InvestigationStore` 与 setter（字段加在 `internal/server/api/query.go:98` 的 `alerts AlertStore` 附近）：

```go
func (a *API) SetInvestigationStore(s *ai.InvestigationStore) { a.aiInv = s }
```

路由（`query.go`，AI 路由旁）：

```go
	mux.HandleFunc("GET /api/v1/ai/investigations", a.permit(a.handleAIInvestigations, "ai:read"))
```

- [ ] **Step 5: 运行测试**

Run: `go test ./internal/server/ai/ ./internal/server/api/ -count=1`
Expected: PASS。

- [ ] **Step 6: 提交**

```bash
git add internal/server/ai/investigations.go internal/server/ai/investigations_test.go internal/server/api/ai_investigation_api.go internal/server/api/ai_investigation_api_test.go internal/server/api/query.go
git commit -m "feat(ai): 调查结果存储与查询接口"
```

---

## Task 4: 自动调查器与接线

**Files:**
- Create: `internal/server/ai/investigator.go`
- Create: `internal/server/ai/investigator_test.go`
- Modify: `internal/server/config/config.go`（`AutoInvestigate`）
- Modify: `cmd/server/main.go`（构造与启动）
- Modify: `internal/server/api/ai_diagnose.go`（`AlertSource` 适配 + 审计）

**Interfaces:**
- Produces: `ai.AlertSource`、`ai.AlertRef`、`ai.Investigator`（`Run(ctx)`）、`(*API).criticalAlertsForAI()`。
- Consumes: `ai.Run`、`InvestigationStore`、`ai.Manager.Get`、`newToolbox`。

- [ ] **Step 1: 写失败测试**

```go
package ai

import (
	"context"
	"testing"
	"time"

	"github.com/mlmw92/yingzhao/internal/server/config"
)

type fakeSource struct{ refs []AlertRef }

func (f fakeSource) ActiveCritical() []AlertRef { return f.refs }

func TestInvestigatorDedupesByRuleAndNode(t *testing.T) {
	cfg := config.AIConfig{Enabled: true, BaseURL: "http://127.0.0.1:1", Model: "m", AutoInvestigate: true}
	ran := 0
	inv := NewInvestigator(
		func() config.AIConfig { return cfg },
		fakeSource{refs: []AlertRef{{ID: "e1", RuleID: "r1", Node: "n1"}}},
		nil,
		func(context.Context, AlertRef) { ran++ },
	)
	inv.now = func() time.Time { return time.Unix(1000, 0) }
	inv.sweep(context.Background())
	inv.sweep(context.Background())
	if ran != 1 {
		t.Fatalf("同一规则+节点 30 分钟内应只调查一次，实际 %d", ran)
	}
}

func TestInvestigatorDisabledDoesNothing(t *testing.T) {
	cfg := config.AIConfig{Enabled: true, AutoInvestigate: false}
	ran := 0
	inv := NewInvestigator(
		func() config.AIConfig { return cfg },
		fakeSource{refs: []AlertRef{{ID: "e1", RuleID: "r1", Node: "n1"}}},
		nil,
		func(context.Context, AlertRef) { ran++ },
	)
	inv.sweep(context.Background())
	if ran != 0 {
		t.Fatal("未开启 autoInvestigate 时不得发起调查")
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/server/ai/ -run TestInvestigator -v`
Expected: FAIL（`Investigator` 未定义）。

- [ ] **Step 3: 实现调查器**

```go
package ai

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/mlmw92/yingzhao/internal/server/config"
)

type AlertRef struct {
	ID       string
	RuleID   string
	RuleName string
	Node     string
	Message  string
	StartsAt int64
}

type AlertSource interface {
	ActiveCritical() []AlertRef
}

const investigateCooldown = 30 * time.Minute

type Investigator struct {
	cfg    func() config.AIConfig
	src    AlertSource
	store  *InvestigationStore
	runOne func(context.Context, AlertRef) // 测试注入；生产由 Run 内部构造

	mu     sync.Mutex
	last   map[string]time.Time
	sem    chan struct{}
	now    func() time.Time
}

func NewInvestigator(cfg func() config.AIConfig, src AlertSource, store *InvestigationStore,
	run func(context.Context, AlertRef)) *Investigator {
	return &Investigator{
		cfg: cfg, src: src, store: store, runOne: run,
		last: map[string]time.Time{}, sem: make(chan struct{}, 2), now: time.Now,
	}
}

func (i *Investigator) Run(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			i.sweep(ctx)
		}
	}
}

func (i *Investigator) sweep(ctx context.Context) {
	cfg := i.cfg().Normalize()
	if !cfg.Enabled || !cfg.Ready() || !cfg.AutoInvestigate {
		return
	}
	for _, ref := range i.src.ActiveCritical() {
		key := ref.RuleID + "|" + ref.Node
		if !i.claim(key) {
			continue
		}
		select {
		case i.sem <- struct{}{}:
			go func(r AlertRef) {
				defer func() { <-i.sem }()
				i.runOne(ctx, r)
			}(ref)
		default:
			slog.Info("AI 自动调查跳过：并发已满", "rule", ref.RuleID, "node", ref.Node)
		}
	}
}

func (i *Investigator) claim(key string) bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	now := i.now()
	if t, ok := i.last[key]; ok && now.Sub(t) < investigateCooldown {
		return false
	}
	i.last[key] = now
	return true
}
```

- [ ] **Step 4: 配置增加 `AutoInvestigate`**

`AIConfig` 增加：

```go
	// AutoInvestigate 允许 critical 告警触发自动调查。默认 false：
	// 打开后每次告警都可能把节点数据发给模型，这属于持续数据出境。
	AutoInvestigate bool `yaml:"autoInvestigate" json:"autoInvestigate"`
```

- [ ] **Step 5: 接线**

`main.go` 在 AI 管理器与调查存储构造后：

```go
	aiInvStore := ai.NewInvestigationStore(filepath.Join(cfg.DataDir, "ai_investigations.json"), 500, 30*24*time.Hour)
	rest.SetInvestigationStore(aiInvStore)
	if aiMgr != nil {
		inv := ai.NewInvestigator(aiMgr.Get, rest.CriticalAlertsForAI(), aiInvStore, rest.RunAutoInvestigation)
		go inv.Run(runCtx)
	}
```

`api` 增加两个导出方法（`ai_investigation_api.go`）：

```go
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
	now := time.Now().UnixMilli()
	a.aiInv.Update(inv.ID, func(it *ai.Investigation) {
		it.Steps = out.Steps
		it.Tokens = out.Tokens
		it.Rounds = out.Rounds
		it.Stopped = out.Stopped
		it.Protocol = out.Protocol
		it.Answer = out.Answer
		it.EndedAt = now
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
```

（`ai_investigation_api.go` 需要导入 `context`、`fmt`、`strconv`、`time`、`audit`、`ai`、`model`。

- [ ] **Step 6: 运行测试**

Run: `go test ./internal/server/ai/ ./internal/server/api/ -count=1 && go build ./...`
Expected: PASS。

- [ ] **Step 7: 提交**

```bash
git add internal/server/ai/investigator.go internal/server/ai/investigator_test.go internal/server/config/config.go internal/server/api/ai_diagnose.go internal/server/api/ai_investigation_api.go cmd/server/main.go
git commit -m "feat(ai): critical 告警自动调查"
```

---

## Task 5: 告警详情「AI 调查」区块与设置项

**Files:**
- Create: `web/src/components/alert/AIInvestigationPanel.vue`
- Create: `web/src/components/alert/AIInvestigationPanel.test.js`
- Modify: `web/src/components/AlertsView.vue`（详情抽屉）
- Modify: `web/src/components/settings/AISettingsSubView.vue`（toolMode / autoInvestigate）

**Interfaces:**
- Consumes: `GET /api/v1/ai/investigations?alertId=`。

- [ ] **Step 1: 写失败测试**

```js
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import http from '../../api/http'
import AIInvestigationPanel from './AIInvestigationPanel.vue'

vi.mock('../../api/http', () => ({ default: { get: vi.fn() } }))

beforeEach(() => vi.clearAllMocks())

describe('AIInvestigationPanel', () => {
  it('展示结论与步骤', async () => {
    http.get.mockResolvedValue({
      investigation: {
        state: 'done', answer: 'CPU 持续偏高', protocol: 'native',
        steps: [{ tool: 'query_metric', result: 'cpu=91' }],
      },
    })
    const w = mount(AIInvestigationPanel, { props: { alertId: 'e1' }, global: { plugins: [ElementPlus] } })
    await flushPromises()
    expect(w.text()).toContain('CPU 持续偏高')
    expect(w.text()).toContain('query_metric')
  })

  it('没有记录时说明自动调查未开启', async () => {
    http.get.mockResolvedValue({ investigation: null })
    const w = mount(AIInvestigationPanel, { props: { alertId: 'e1' }, global: { plugins: [ElementPlus] } })
    await flushPromises()
    expect(w.text()).toContain('暂无 AI 调查')
  })
})
```

- [ ] **Step 2: 运行确认失败**

Run: `cd web && ./node_modules/.bin/vitest run src/components/alert/AIInvestigationPanel.test.js`
Expected: FAIL。

- [ ] **Step 3: 实现组件**

```vue
<template>
  <div class="ai-inv">
    <div v-if="loading" class="muted">加载中…</div>
    <template v-else-if="inv">
      <div class="inv-state">
        <el-tag size="small" :type="tagType">{{ stateLabel }}</el-tag>
        <span v-if="inv.protocol" class="muted">协议：{{ inv.protocol }}</span>
        <span class="muted">工具调用 {{ (inv.steps || []).length }} 次</span>
      </div>
      <p v-if="inv.answer" class="inv-answer">{{ inv.answer }}</p>
      <p v-if="inv.error" class="inv-error">{{ inv.error }}</p>
      <details v-if="(inv.steps || []).length">
        <summary>调查步骤</summary>
        <div v-for="(s, i) in inv.steps" :key="i" class="inv-step">
          <strong>{{ s.tool }}</strong>
          <span v-if="s.err" class="inv-error">{{ s.err }}</span>
          <pre v-else>{{ s.result }}</pre>
        </div>
      </details>
    </template>
    <div v-else class="muted">暂无 AI 调查（自动调查未开启或该告警未触发调查）</div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import http from '../../api/http'

const props = defineProps({ alertId: { type: String, required: true } })
const inv = ref(null)
const loading = ref(true)

const stateLabel = computed(() => ({
  running: '调查中', done: '已完成', failed: '失败', skipped: '已跳过',
}[inv.value?.state] || inv.value?.state || ''))
const tagType = computed(() => ({
  running: 'warning', done: 'success', failed: 'danger', skipped: 'info',
}[inv.value?.state] || 'info'))

onMounted(async () => {
  try {
    const data = await http.get('/api/v1/ai/investigations?alertId=' + encodeURIComponent(props.alertId))
    inv.value = data.investigation || null
  } catch {
    inv.value = null
  } finally {
    loading.value = false
  }
})
</script>

<style scoped>
.ai-inv { padding: 4px 0; }
.inv-state { display: flex; gap: 12px; align-items: center; margin-bottom: 8px; }
.inv-answer { white-space: pre-wrap; line-height: 1.6; }
.inv-error { color: var(--danger); }
.inv-step { margin: 8px 0; }
.inv-step pre { white-space: pre-wrap; font-size: var(--fs-sm); background: var(--fill-1); padding: 8px; border-radius: 6px; }
.muted { color: var(--t3); }
</style>
```

- [ ] **Step 4: 接入告警详情**

`AlertsView.vue` 的详情抽屉内、趋势图之前加入：

```vue
<div class="ev-chart-title">AI 调查</div>
<AIInvestigationPanel v-if="detail.id" :alert-id="detail.id" />
```

```js
import AIInvestigationPanel from './alert/AIInvestigationPanel.vue'
```

- [ ] **Step 5: AI 设置项**

`AISettingsSubView.vue`：

- `form` 增加 `toolMode: 'auto'`、`autoInvestigate: false`；
- 保存 payload 与 `isDirty` 比较加入这两个字段；
- 界面增加两个控件：

```vue
<el-form-item label="工具调用协议">
  <el-select v-model="form.toolMode" style="width: 220px">
    <el-option value="auto" label="自动（先原生，失败回退 JSON）" />
    <el-option value="native" label="原生 function calling" />
    <el-option value="json" label="JSON 协议" />
  </el-select>
</el-form-item>
<el-form-item label="告警自动调查">
  <el-switch v-model="form.autoInvestigate" />
  <div class="field-hint">仅 critical 告警触发；同一规则+节点 30 分钟一次；会持续产生数据出境。</div>
</el-form-item>
```

- [ ] **Step 6: 运行测试**

Run: `cd web && ./node_modules/.bin/vitest run src/components/alert src/components/settings/AISettingsSubView.test.js src/components/AlertsView`
Expected: PASS。

- [ ] **Step 7: 提交**

```bash
git add web/src/components/alert web/src/components/AlertsView.vue web/src/components/settings/AISettingsSubView.vue
git commit -m "feat(web): 告警详情 AI 调查区块与设置项"
```

---

## Task 6: 文档与版本

**Files:**
- Modify: `docs/guides/configuration-reference.md`
- Modify: `CONTEXT.md`
- Modify: `VERSION`

- [ ] **Step 1: 文档**

在 AI 配置章节补充：

```markdown
### 工具与自动调查（阶段 2）

- `toolMode`：`auto`（先原生 function calling，4xx 不支持时回退 JSON）| `native` | `json`。
- `autoInvestigate`：默认 `false`。开启后仅 **critical** 告警触发自动调查；
  同一「规则 + 节点」30 分钟内只调查一次；全局并发 2；单次最多 4 轮 / 8 次工具调用 / 8000 tokens / 30 秒。
- 工具全部只读，且只访问**告警所属节点**；`log_stats` 只返回来源与条数，不返回日志正文。
- 结果保存在 `ai_investigations.json`（最多 500 条、30 天），在告警详情页「AI 调查」区块展示。
```

`CONTEXT.md` 增加术语：

```markdown
**AI 调查**
由模型通过只读工具核实告警后给出的结论。自动调查仅 critical 触发、按规则+节点去重；结果独立存储，不写入告警事件。
```

- [ ] **Step 2: 版本**

`VERSION` 递增次版本（如 `1.35.0`）。

- [ ] **Step 3: 全量验证**

Run: `go test ./... && cd web && ./node_modules/.bin/vitest run`
Expected: 全部通过。

- [ ] **Step 4: 提交**

```bash
git add docs/guides/configuration-reference.md CONTEXT.md VERSION
git commit -m "docs: AI 工具层与自动调查说明"
```

---

## 验证清单

- [ ] `go test ./...` 全绿；`cd web && ./node_modules/.bin/vitest run` 全绿。
- [ ] 实机：配置可用端点并开启 `autoInvestigate`，制造 critical 告警 → 30–60 秒后详情页出现结论与步骤。
- [ ] 实机：同一告警 30 分钟内不重复调查。
- [ ] 实机：关闭 `autoInvestigate` 后不再产生新调查。
- [ ] 实机：审计出现 `ai:auto_investigate`。
- [ ] 真实浏览器走查告警详情与 AI 设置页。
