# AI 建议处置动作并接入审批 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 AI 在调查结论里记录结构化的处置建议，并由人一键提交为既有审批单；模型永远不能直接执行或创建审批。

**Architecture:** 新增第 8 个工具 `propose_action`（只记录、不执行），建议随 `Investigation` 落库；新增一个提交接口把建议转成 `approval.Request`。提交时重新校验动作、参数、节点范围与在线状态，权限沿用 `ai:invoke` + `ops:exec` + 动作自带 `Perm`。

**Tech Stack:** Go 1.22（`net/http` ServeMux 路径参数）、Vue 3 `<script setup>`、Element Plus、Vitest、Go testing + httptest。

**Spec:** `docs/superpowers/specs/2026-10-20-ai-suggested-actions-design.md`

## Global Constraints

- 模型只能记录建议；创建审批单必须由人点击。
- AI 来源的审批单无条件走审批，**不受** `approval.Config.Enabled` 影响。
- 建议只接受写类 ops 动作（`ReadOnly == false`），排除 `file.push`。
- 建议的目标节点固定为调查范围节点，模型不能指定。
- 每条调查最多 3 条建议，完全相同的建议去重。
- 提交时重新校验，不信任落库的参数值。
- 不新增权限点，不修改 `internal/server/approval` 的状态机。
- 旧调查记录没有 `suggestions` 字段时必须能正常反序列化。
- 所有提交信息末尾附一行：`Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>`。
- 提交与构建分开执行：先确认 `git commit` 成功，再做任何构建。

## Review Focus

- **模型重复建议同一条动作**：必须被去重拒绝，否则审批人看到三条一模一样的单子。
- **审批开关关闭时提交建议**：必须仍然生成待审批单，不能直连执行。
- **提交前参数已失效**（动作被移除、参数不再匹配）：必须 400，而不是把坏参数推给审批人。
- **提交前节点已离线**：必须 409，与 `handleOpsCreate` 同口径。
- **两条并发调查**：建议必须各归各的调查，不能串台。

---

### Task 1: `Suggestion` 类型、`ToolScope.Propose` 与 `propose_action` 工具

**Files:**
- Modify: `internal/server/ai/tools.go`
- Modify: `internal/server/ai/investigations.go`
- Modify: `internal/server/api/ai_tools.go`
- Test: `internal/server/api/ai_tools_test.go`
- Test: `internal/server/ai/investigations_test.go`

**Interfaces:**
- Consumes: `ops.Lookup(kind) (Action, bool)`、`ops.Validate(kind, params) (Action, map[string]string, error)`（既有）。
- Produces:
  - `ai.Suggestion{ID, Kind, Title string; Params map[string]string; Node, Reason string}`
  - `ai.ToolScope.Propose func(Suggestion) error`
  - `ai.Investigation.Suggestions []Suggestion`
  - 工具名 `propose_action`

- [ ] **Step 1: Write the failing tests**

在 `internal/server/api/ai_tools_test.go` 末尾追加（并在 import 里补 `"github.com/mlmw92/yingzhao/internal/server/ops"`）：

```go
func TestProposeActionRecordsValidatedSuggestion(t *testing.T) {
	a := scopeTestAPI(t)
	var got []ai.Suggestion
	tb := a.newToolbox(ai.ToolScope{Node: "web-01", Propose: func(s ai.Suggestion) error {
		got = append(got, s)
		return nil
	}})
	tool := toolByName(t, tb, "propose_action")

	out, err := tool.Handler(context.Background(), map[string]any{
		"kind":   ops.KindSvcRestart,
		"params": map[string]any{"unit": "nginx.service"},
		"reason": "nginx 单元处于 failed 状态",
	})
	if err != nil {
		t.Fatalf("propose_action: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("应记录 1 条建议，实际 %d", len(got))
	}
	s := got[0]
	if s.Node != "web-01" || s.Kind != ops.KindSvcRestart || s.Params["unit"] != "nginx.service" {
		t.Fatalf("建议内容不正确: %+v", s)
	}
	if s.ID == "" || s.Title == "" {
		t.Fatalf("建议缺少 ID 或标题: %+v", s)
	}
	if !strings.Contains(out, "不会自动执行") {
		t.Fatalf("工具回执必须告诉模型建议不会自动执行，实际 %q", out)
	}
}

func TestProposeActionRejects(t *testing.T) {
	a := scopeTestAPI(t)
	cases := []struct {
		name string
		args map[string]any
	}{
		{"未知动作", map[string]any{"kind": "nope"}},
		{"只读动作", map[string]any{"kind": ops.KindNodeDiagnostics}},
		{"文件分发", map[string]any{"kind": ops.KindFilePush,
			"params": map[string]any{"path": "/tmp/x", "fileId": "obf-1"}}},
		{"缺必填参数", map[string]any{"kind": ops.KindSvcRestart, "params": map[string]any{}}},
		{"参数格式错", map[string]any{"kind": ops.KindSvcRestart, "params": map[string]any{"unit": "nginx"}}},
		{"参数不是字符串", map[string]any{"kind": ops.KindSvcRestart, "params": map[string]any{"unit": 1}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got []ai.Suggestion
			tb := a.newToolbox(ai.ToolScope{Node: "web-01", Propose: func(s ai.Suggestion) error {
				got = append(got, s)
				return nil
			}})
			tool := toolByName(t, tb, "propose_action")
			if _, err := tool.Handler(context.Background(), c.args); err == nil {
				t.Fatal("应拒绝该建议")
			}
			if len(got) != 0 {
				t.Fatalf("被拒绝的建议不得记录: %+v", got)
			}
		})
	}
}

func TestProposeActionCapsAndDedupes(t *testing.T) {
	a := scopeTestAPI(t)
	var got []ai.Suggestion
	tb := a.newToolbox(ai.ToolScope{Node: "web-01", Propose: func(s ai.Suggestion) error {
		got = append(got, s)
		return nil
	}})
	tool := toolByName(t, tb, "propose_action")
	call := func(unit string) error {
		_, err := tool.Handler(context.Background(), map[string]any{
			"kind": ops.KindSvcRestart, "params": map[string]any{"unit": unit},
		})
		return err
	}

	if err := call("a.service"); err != nil {
		t.Fatal(err)
	}
	if err := call("a.service"); err == nil {
		t.Fatal("完全相同的建议应被去重")
	}
	for _, u := range []string{"b.service", "c.service"} {
		if err := call(u); err != nil {
			t.Fatal(err)
		}
	}
	if err := call("d.service"); err == nil {
		t.Fatal("第 4 条建议应被上限拒绝")
	}
	if len(got) != 3 {
		t.Fatalf("应记录 3 条建议，实际 %d", len(got))
	}
}

func TestProposeActionAbsentWithoutCallback(t *testing.T) {
	a := scopeTestAPI(t)
	tb := a.newToolbox(ai.ToolScope{Node: "web-01"})
	for _, tool := range tb.Tools() {
		if tool.Name == "propose_action" {
			t.Fatal("没有 Propose 回调时不应暴露 propose_action")
		}
	}
}
```

在 `internal/server/ai/investigations_test.go` 追加（验证建议随调查落盘、重启后可读）：

```go
func TestInvestigationStorePersistsSuggestions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inv.json")
	s := NewInvestigationStore(path, 10, time.Hour)
	_ = s.Add(Investigation{ID: "inv-1", AlertID: "e1", State: "done",
		Suggestions: []Suggestion{{ID: "sug-1", Kind: "svc.restart", Node: "web-01"}}})

	reloaded := NewInvestigationStore(path, 10, time.Hour)
	inv, ok := reloaded.ByAlert("e1")
	if !ok || len(inv.Suggestions) != 1 || inv.Suggestions[0].Kind != "svc.restart" {
		t.Fatalf("建议未落盘: %+v", inv)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/server/api/ -run TestProposeAction -v`
Expected: FAIL，`缺少工具 propose_action`。

Run: `go test ./internal/server/ai/ -run TestInvestigationStorePersistsSuggestions -v`
Expected: 编译失败，`undefined: Suggestion`。

- [ ] **Step 3: Implement the types**

`internal/server/ai/tools.go`：

```go
// ToolScope 是工具可访问的范围。自动调查按告警节点构造；
// 工具实现必须相信这个字段，而不是相信模型传进来的参数。
type ToolScope struct {
	Node           string
	AllowSensitive bool
	// Propose 把一条通过校验的处置建议记入本次调查。为 nil 时不暴露 propose_action。
	Propose func(Suggestion) error
}
```

`internal/server/ai/investigations.go`，在 `Investigation` 之前加：

```go
// Suggestion 是模型提出的一条处置建议。它只是一个记录，不代表任何动作已被提交。
type Suggestion struct {
	ID     string            `json:"id"`
	Kind   string            `json:"kind"`
	Title  string            `json:"title"`
	Params map[string]string `json:"params,omitempty"`
	Node   string            `json:"node"`
	Reason string            `json:"reason,omitempty"`
}
```

并在 `Investigation` 结构体末尾（`EndedAt` 之后）加：

```go
	Suggestions []Suggestion `json:"suggestions,omitempty"`
```

- [ ] **Step 4: Implement the tool**

`internal/server/api/ai_tools.go`：

1. import 增加 `"errors"`、`"github.com/mlmw92/yingzhao/internal/server/ops"`（`sort`/`strings`/`fmt`/`json` 已有）。

2. `apiToolbox` 增加字段：

```go
type apiToolbox struct {
	a     *API
	scope ai.ToolScope
	// 建议计数与去重只在本次调查内有效；工具按顺序执行，无需加锁。
	proposals int
	seen      map[string]struct{}
}
```

3. `newToolbox` 初始化 `seen`：

```go
func (a *API) newToolbox(scope ai.ToolScope) ai.Toolbox {
	return &apiToolbox{a: a, scope: scope, seen: map[string]struct{}{}}
}
```

4. `Tools()` 按回调决定是否暴露：

```go
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
```

5. 新增常量与工具实现：

```go
// maxAISuggestions 是单次调查最多保留的建议数。
// 超过 3 条开始变成噪音：审批人要的是可判断的选项，不是清单。
const maxAISuggestions = 3

func (t *apiToolbox) proposeAction() ai.Tool {
	return ai.Tool{
		Name: "propose_action", ReadOnly: true,
		Description: "记录一条处置建议。只接受写类操作；建议不会自动执行，需人工提交并由另一位同事批准。",
		Schema: json.RawMessage(`{"type":"object","properties":{"kind":{"type":"string","description":"动作标识，如 svc.restart / container.restart"},"params":{"type":"object","additionalProperties":{"type":"string"},"description":"动作参数"},"reason":{"type":"string","description":"建议理由，一句话"}},"required":["kind"]}`),
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
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/server/api/ -run TestProposeAction -v`
Expected: PASS（5 个用例，含子用例）。

- [ ] **Step 6: Commit**

```bash
git add internal/server/ai/tools.go internal/server/ai/investigations.go internal/server/api/ai_tools.go internal/server/api/ai_tools_test.go
git commit -m "feat(ai): 新增 propose_action 工具记录处置建议"
```

---

### Task 2: 调查运行时接入建议与提示词

**Files:**
- Modify: `internal/server/api/ai_investigation_api.go`（`runInvestigation`）
- Test: `internal/server/api/ai_investigation_api_test.go`

**Interfaces:**
- Consumes: Task 1 的 `ai.Suggestion`、`ai.ToolScope.Propose`、`propose_action`。
- Produces: 调查完成时 `Investigation.Suggestions` 已落库。

- [ ] **Step 1: Write the failing test**

在 `internal/server/api/ai_investigation_api_test.go` 追加：

```go
// 调查运行时必须把工具记录的建议写进调查记录，并把「建议不会自动执行」写进系统提示。
func TestInvestigationRecordsSuggestions(t *testing.T) {
	a, m := aiTestAPI(t)
	store := ai.NewInvestigationStore(filepath.Join(t.TempDir(), "inv.json"), 10, time.Hour)
	a.SetInvestigationStore(store)
	a.alerts = &alertStoreStub{}

	var systemPrompt, toolResult string
	call := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		msgs, _ := body["messages"].([]any)
		if len(msgs) > 0 {
			if m0, ok := msgs[0].(map[string]any); ok && m0["role"] == "system" {
				systemPrompt, _ = m0["content"].(string)
			}
			for _, raw := range msgs {
				if mm, ok := raw.(map[string]any); ok && mm["role"] == "tool" {
					toolResult, _ = mm["content"].(string)
				}
			}
		}
		call++
		if call == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{
				"message": map[string]any{
					"role": "assistant",
					"tool_calls": []any{map[string]any{
						"id": "c1", "type": "function",
						"function": map[string]any{"name": "propose_action",
							"arguments": `{"kind":"svc.restart","params":{"unit":"nginx.service"},"reason":"单元 failed"}`},
					}},
				},
			}}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{
			"message": map[string]any{"role": "assistant", "content": "结论：建议重启 nginx"},
		}}})
	}))
	defer srv.Close()
	if err := m.Save(config.AIConfig{Enabled: true, BaseURL: srv.URL, Model: "m", ToolMode: "native"}); err != nil {
		t.Fatal(err)
	}

	a.RunAutoInvestigation(context.Background(), ai.AlertRef{
		ID: "e1", RuleID: "r1", RuleName: "CPU 高", Node: "web-01", Message: "CPU > 90%",
	})

	inv, ok := store.ByAlert("e1")
	if !ok || inv.State != "done" {
		t.Fatalf("调查未完成: %+v", inv)
	}
	if len(inv.Suggestions) != 1 {
		t.Fatalf("应记录 1 条建议，实际 %+v", inv.Suggestions)
	}
	s := inv.Suggestions[0]
	if s.Kind != "svc.restart" || s.Node != "web-01" || s.Params["unit"] != "nginx.service" {
		t.Fatalf("建议内容不正确: %+v", s)
	}
	if !strings.Contains(systemPrompt, "propose_action") || !strings.Contains(systemPrompt, "不会自动执行") {
		t.Fatalf("系统提示应说明建议能力与边界: %q", systemPrompt)
	}
	if !strings.Contains(toolResult, "不会自动执行") {
		t.Fatalf("工具回执应告诉模型建议不会自动执行: %q", toolResult)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/api/ -run TestInvestigationRecordsSuggestions -v`
Expected: FAIL，`应记录 1 条建议，实际 []`。

- [ ] **Step 3: Implement**

`internal/server/api/ai_investigation_api.go` 的 `runInvestigation`：

1. 系统提示改为：

```go
	system := "你是运维平台的诊断助手。只根据工具返回的数据作答；不要猜测，也不要执行任何写操作。" +
		"若结论指向一个可执行的处置动作，可调用 propose_action 记录建议。" +
		"只有工具结果里有明确依据时才建议；最多 3 条；不要建议文件分发。" +
		"建议不会自动执行，需人工提交并由另一位同事批准。"
```

2. 构造 toolbox 时注入回调，并在最终 `Update` 写入：

```go
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
		// …其余字段保持不变
	})
```

**不要**在每次建议时调用 `aiInv.Update`：它会无条件写 `EndedAt`，让还在跑的调查显示成「已结束」。

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/server/api/ -run 'TestInvestigationRecordsSuggestions|TestAIRetryInvestigation' -v`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/server/api/ai_investigation_api.go internal/server/api/ai_investigation_api_test.go
git commit -m "feat(ai): 调查结果记录处置建议"
```

---

### Task 3: 提交建议生成审批单

**Files:**
- Modify: `internal/server/ai/investigations.go`（新增 `ByID`）
- Modify: `internal/server/api/ai_investigation_api.go`（新增 handler）
- Modify: `internal/server/api/query.go`（注册路由）
- Test: `internal/server/ai/investigations_test.go`
- Test: `internal/server/api/ai_investigation_api_test.go`

**Interfaces:**
- Consumes: `approval.Store`、`approval.Request`、`a.submitApproval`、`a.approvalView`、`a.nodeInScope`、`a.checkPerm`、`ops.Validate`（均既有）。
- Produces:
  - `InvestigationStore.ByID(id string) (Investigation, bool)`
  - `POST /api/v1/ai/investigations/{id}/suggestions/{sid}/submit`

- [ ] **Step 1: Write the failing store test**

在 `internal/server/ai/investigations_test.go` 追加：

```go
func TestInvestigationStoreByID(t *testing.T) {
	s := NewInvestigationStore(filepath.Join(t.TempDir(), "inv.json"), 10, time.Hour)
	_ = s.Add(Investigation{ID: "inv-1", AlertID: "e1", State: "done"})
	_ = s.Add(Investigation{ID: "inv-2", AlertID: "e2", State: "done"})

	if inv, ok := s.ByID("inv-2"); !ok || inv.AlertID != "e2" {
		t.Fatalf("ByID = %+v, %v", inv, ok)
	}
	if _, ok := s.ByID("nope"); ok {
		t.Fatal("不存在的 ID 应返回 false")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/server/ai/ -run TestInvestigationStoreByID -v`
Expected: FAIL，`s.ByID undefined`。

- [ ] **Step 3: Implement ByID**

```go
// ByID 按调查 ID 查找。提交建议时需要它：告警 ID 与调查 ID 不是一回事。
func (s *InvestigationStore) ByID(id string) (Investigation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.items) - 1; i >= 0; i-- {
		if s.items[i].ID == id {
			return s.items[i], true
		}
	}
	return Investigation{}, false
}
```

Run: `go test ./internal/server/ai/ -run TestInvestigationStoreByID -v` → PASS。

- [ ] **Step 4: Write the failing API tests**

在 `internal/server/api/ai_investigation_api_test.go` 追加（import 补 `"github.com/mlmw92/yingzhao/internal/server/approval"`、`"github.com/mlmw92/yingzhao/internal/server/auth"`、`"github.com/mlmw92/yingzhao/internal/server/node"`）：

```go
// D2 的决定性判据：全局审批开关关闭时，AI 建议仍然只能进审批。
func TestAISubmitSuggestionCreatesApprovalWhenDisabled(t *testing.T) {
	a, _, st, cfg := approvalTestAPI(t)
	c := cfg.Get()
	c.Enabled = false
	if err := cfg.Set(c); err != nil {
		t.Fatal(err)
	}
	invStore := ai.NewInvestigationStore(filepath.Join(t.TempDir(), "inv.json"), 10, time.Hour)
	a.SetInvestigationStore(invStore)
	_ = invStore.Add(ai.Investigation{
		ID: "inv-1", AlertID: "e1", Node: "web-01", State: "done",
		Suggestions: []ai.Suggestion{{
			ID: "sug-1", Kind: "svc.restart", Title: "重启服务",
			Params: map[string]string{"unit": "nginx.service"}, Node: "web-01",
		}},
	})

	p := approvalPrincipal("ops1", true, "ai:invoke", "ops:exec")
	rec := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, approvalReq(p, http.MethodPost,
		"/api/v1/ai/investigations/inv-1/suggestions/sug-1/submit", ""))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("code = %d（body=%s）", rec.Code, rec.Body.String())
	}

	body := decodeBody(t, rec)
	ap, _ := body["approval"].(map[string]any)
	id, _ := ap["id"].(string)
	if id == "" {
		t.Fatalf("响应缺少审批单号: %v", body)
	}
	req, ok := st.Get(id)
	if !ok {
		t.Fatalf("审批单 %s 未落库", id)
	}
	if req.Kind != approval.EntryOpsWrite || req.OpsKind != "svc.restart" {
		t.Fatalf("审批单动作不正确: %+v", req)
	}
	if req.Node != "web-01" || req.Params["unit"] != "nginx.service" {
		t.Fatalf("审批单目标或参数不正确: %+v", req)
	}
	if req.Extra["source"] != "ai" || req.Extra["investigationId"] != "inv-1" || req.Extra["suggestionId"] != "sug-1" {
		t.Fatalf("审批单缺少来源标记: %+v", req.Extra)
	}
	if req.Requester != "ops1" {
		t.Fatalf("申请人应为当前用户，实际 %q", req.Requester)
	}
}

func TestAISubmitSuggestionRejections(t *testing.T) {
	a, _, _, _ := approvalTestAPI(t)
	invStore := ai.NewInvestigationStore(filepath.Join(t.TempDir(), "inv.json"), 10, time.Hour)
	a.SetInvestigationStore(invStore)
	_ = invStore.Add(ai.Investigation{
		ID: "inv-1", AlertID: "e1", Node: "web-01", State: "done",
		Suggestions: []ai.Suggestion{
			{ID: "sug-1", Kind: "svc.restart", Params: map[string]string{"unit": "nginx.service"}, Node: "web-01"},
			{ID: "sug-out", Kind: "svc.restart", Params: map[string]string{"unit": "nginx.service"}, Node: "db-01"},
			{ID: "sug-bad", Kind: "svc.restart", Params: map[string]string{"unit": "nginx"}, Node: "web-01"},
			{ID: "sug-ro", Kind: "node.diagnostics", Node: "web-01"},
			{ID: "sug-file", Kind: "file.push", Params: map[string]string{"path": "/tmp/x", "fileId": "obf-1"}, Node: "web-01"},
		},
	})

	check := func(name string, p *auth.Principal, target string, want int) {
		t.Helper()
		rec := httptest.NewRecorder()
		newRoutesMux(a).ServeHTTP(rec, approvalReq(p, http.MethodPost, target, ""))
		if rec.Code != want {
			t.Fatalf("%s：code = %d，want %d（body=%s）", name, rec.Code, want, rec.Body.String())
		}
	}
	base := "/api/v1/ai/investigations/inv-1/suggestions/"

	check("无 ai:invoke", approvalPrincipal("ops1", true, "ops:exec"), base+"sug-1/submit", http.StatusForbidden)
	check("无 ops:exec", approvalPrincipal("ops1", true, "ai:invoke"), base+"sug-1/submit", http.StatusForbidden)
	check("调查不存在", approvalPrincipal("ops1", true, "ai:invoke", "ops:exec"),
		"/api/v1/ai/investigations/nope/suggestions/sug-1/submit", http.StatusNotFound)
	check("建议不存在", approvalPrincipal("ops1", true, "ai:invoke", "ops:exec"),
		base+"nope/submit", http.StatusNotFound)
	check("参数失效", approvalPrincipal("ops1", true, "ai:invoke", "ops:exec"),
		base+"sug-bad/submit", http.StatusBadRequest)
	check("只读动作", approvalPrincipal("ops1", true, "ai:invoke", "ops:exec"),
		base+"sug-ro/submit", http.StatusBadRequest)
	check("文件分发", approvalPrincipal("ops1", true, "ai:invoke", "ops:exec"),
		base+"sug-file/submit", http.StatusBadRequest)
	// 范围：db-01 属于 g2，ops1 只被授予 g1
	check("范围外节点", approvalPrincipal("ops1", false, "ai:invoke", "ops:exec"),
		base+"sug-out/submit", http.StatusNotFound)
}

// 提交前节点离线：必须 409，不能把注定过期的动作推给审批人。
func TestAISubmitSuggestionOfflineNode(t *testing.T) {
	a, _, _, _ := approvalTestAPI(t)
	mgr := node.New(filepath.Join(t.TempDir(), "nodes.json"), time.Millisecond)
	mgr.Register(&model.ReportPayload{Node: "web-01", Group: "g1", Mode: model.ModeCollect})
	time.Sleep(5 * time.Millisecond)
	mgr.OfflineStale()
	a.nodeMgr = mgr

	invStore := ai.NewInvestigationStore(filepath.Join(t.TempDir(), "inv.json"), 10, time.Hour)
	a.SetInvestigationStore(invStore)
	_ = invStore.Add(ai.Investigation{
		ID: "inv-1", AlertID: "e1", Node: "web-01", State: "done",
		Suggestions: []ai.Suggestion{{
			ID: "sug-1", Kind: "svc.restart", Params: map[string]string{"unit": "nginx.service"}, Node: "web-01",
		}},
	})

	rec := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, approvalReq(
		approvalPrincipal("ops1", true, "ai:invoke", "ops:exec"),
		http.MethodPost, "/api/v1/ai/investigations/inv-1/suggestions/sug-1/submit", ""))
	if rec.Code != http.StatusConflict {
		t.Fatalf("code = %d，want 409（body=%s）", rec.Code, rec.Body.String())
	}
}
```

- [ ] **Step 5: Run tests to verify they fail**

Run: `go test ./internal/server/api/ -run TestAISubmitSuggestion -v`
Expected: FAIL，404（路由不存在）。

- [ ] **Step 6: Implement the handler**

`internal/server/api/ai_investigation_api.go` 追加：

```go
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

	saved := a.submitApproval(r, &approval.Request{
		Kind: approval.EntryOpsWrite, OpsKind: sug.Kind, Node: sug.Node, Params: params,
		Title: fmt.Sprintf("AI 建议：%s（写操作） @ %s", action.Title, sug.Node),
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
```

import 补 `"github.com/mlmw92/yingzhao/internal/server/approval"`、`"github.com/mlmw92/yingzhao/internal/server/ops"`。

- [ ] **Step 7: Register the route**

`internal/server/api/query.go`，紧接 `GET /api/v1/ai/investigations` 之后：

```go
	mux.HandleFunc("POST /api/v1/ai/investigations/{id}/suggestions/{sid}/submit",
		a.permit(a.handleAISubmitSuggestion, "ai:invoke"))
```

- [ ] **Step 8: Run tests to verify they pass**

Run: `go test ./internal/server/api/ -run 'TestAISubmitSuggestion' -v`
Expected: PASS。

- [ ] **Step 9: Commit**

```bash
git add internal/server/ai/investigations.go internal/server/ai/investigations_test.go internal/server/api/ai_investigation_api.go internal/server/api/ai_investigation_api_test.go internal/server/api/query.go
git commit -m "feat(ai): 建议可提交为审批单"
```

---

### Task 4: 前端「建议处置」区

**Files:**
- Modify: `web/src/api/http.js`
- Modify: `web/src/components/alert/AIInvestigationPanel.vue`
- Test: `web/src/components/alert/AIInvestigationPanel.test.js`

**Interfaces:**
- Consumes: Task 3 的提交接口；`Investigation.suggestions[]`。
- Produces: `http.submitAISuggestion(investigationId, suggestionId)`。

- [ ] **Step 1: Write the failing tests**

在 `web/src/components/alert/AIInvestigationPanel.test.js` 追加（mock 对象补 `submitAISuggestion: vi.fn()`）：

```js
  it('展示处置建议，并明确 AI 不会自动执行', async () => {
    http.get.mockResolvedValue({
      investigation: {
        id: 'inv-1', state: 'done', stopped: 'done', answer: '建议重启 nginx',
        suggestions: [{
          id: 'sug-1', kind: 'svc.restart', title: '重启服务',
          params: { unit: 'nginx.service' }, node: 'web-01', reason: '单元处于 failed',
        }],
      },
    })
    const w = mountPanel()
    await flushPromises()
    const text = w.text()
    expect(text).toContain('重启服务')
    expect(text).toContain('nginx.service')
    expect(text).toContain('单元处于 failed')
    expect(text).toContain('AI 只给建议，不会自动执行')
    expect(text).toContain('提交审批')
  })

  it('提交建议后显示审批单号', async () => {
    http.get.mockResolvedValue({
      investigation: {
        id: 'inv-1', state: 'done', answer: '建议重启 nginx',
        suggestions: [{ id: 'sug-1', kind: 'svc.restart', title: '重启服务', params: { unit: 'nginx.service' }, node: 'web-01' }],
      },
    })
    http.submitAISuggestion.mockResolvedValue({ approval: { id: 'ap-9' } })
    const w = mountPanel()
    await flushPromises()

    const btn = w.findAll('button').find((b) => b.text().includes('提交审批'))
    expect(btn).toBeTruthy()
    await btn.trigger('click')
    await flushPromises()

    expect(http.submitAISuggestion).toHaveBeenCalledWith('inv-1', 'sug-1')
    expect(w.text()).toContain('ap-9')
    expect(w.text()).toContain('已提交审批')
  })

  it('没有建议时不显示建议区', async () => {
    http.get.mockResolvedValue({ investigation: { id: 'inv-1', state: 'done', answer: '一切正常' } })
    const w = mountPanel()
    await flushPromises()
    expect(w.text()).not.toContain('建议处置')
  })
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd web && npx vitest run src/components/alert/AIInvestigationPanel.test.js`
Expected: FAIL，找不到「提交审批」/「建议处置」。

- [ ] **Step 3: Add the http method**

`web/src/api/http.js`，在 `retryAIInvestigation` 之后：

```js
  // 把一条 AI 建议提交为审批单（服务端会重新校验动作、参数与节点状态）。
  submitAISuggestion: (investigationId, suggestionId) =>
    api.post(
      '/api/v1/ai/investigations/' + encodeURIComponent(investigationId) +
        '/suggestions/' + encodeURIComponent(suggestionId) + '/submit',
      {},
    ),
```

- [ ] **Step 4: Add the panel section**

`web/src/components/alert/AIInvestigationPanel.vue`，在步骤折叠块之前插入：

```html
      <div v-if="(inv.suggestions || []).length" class="inv-suggestions">
        <div class="sug-title">建议处置</div>
        <p class="sug-hint">AI 只给建议，不会自动执行；提交后需另一位同事批准</p>
        <div v-for="s in inv.suggestions" :key="s.id" class="sug-item">
          <div class="sug-head">
            <strong>{{ s.title }}</strong>
            <span class="muted">@ {{ s.node }}</span>
          </div>
          <div v-if="s.reason" class="sug-reason">{{ s.reason }}</div>
          <div class="sug-params">{{ paramsText(s) }}</div>
          <div class="sug-actions">
            <el-button
              size="small"
              type="primary"
              :loading="submittingId === s.id"
              :disabled="!!submitted[s.id]"
              @click="submitSuggestion(s)"
            >
              {{ submitted[s.id] ? '已提交审批' : '提交审批' }}
            </el-button>
            <span v-if="submitted[s.id]" class="muted">审批单 {{ submitted[s.id] }}</span>
          </div>
        </div>
      </div>
```

script 追加：

```js
const submittingId = ref('')
const submitted = ref({})

function paramsText(s) {
  return Object.entries(s.params || {}).map(([k, v]) => `${k} = ${v}`).join(' · ')
}

async function submitSuggestion(s) {
  submittingId.value = s.id
  try {
    const res = await http.submitAISuggestion(inv.value.id, s.id)
    submitted.value = { ...submitted.value, [s.id]: res.approval?.id || '已提交' }
    ElMessage.success('已提交审批，需另一位同事批准')
  } catch (e) {
    ElMessage.error(e.message || '提交失败')
  } finally {
    submittingId.value = ''
  }
}
```

style 追加：

```css
.inv-suggestions { margin: 12px 0; padding: 12px; border: 1px solid var(--bd); border-radius: var(--r-md); background: var(--fill-1); }
.sug-title { font-weight: 600; margin-bottom: 4px; }
.sug-hint { font-size: var(--fs-sm); color: var(--t3); margin: 0 0 10px; }
.sug-item { padding: 8px 0; border-top: 1px solid var(--bd); }
.sug-head { display: flex; gap: 8px; align-items: baseline; }
.sug-reason { font-size: var(--fs-sm); color: var(--t2); margin-top: 4px; }
.sug-params { font-family: var(--mono); font-size: var(--fs-sm); color: var(--t3); margin-top: 4px; }
.sug-actions { display: flex; gap: 10px; align-items: center; margin-top: 8px; }
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd web && npx vitest run src/components/alert/AIInvestigationPanel.test.js`
Expected: PASS（9 个用例）。

- [ ] **Step 6: Run the full suites**

Run: `go test ./...`
Expected: PASS（无 FAIL）。

Run: `cd web && npx vitest run`
Expected: PASS（全部文件）。

- [ ] **Step 7: Commit**

```bash
git add web/src/api/http.js web/src/components/alert/AIInvestigationPanel.vue web/src/components/alert/AIInvestigationPanel.test.js
git commit -m "feat(web): 调查面板展示处置建议并支持提交审批"
```

---

## 实机验证（部署后）

按 spec §8 的七步执行；重点看：

1. 调查完成后面板出现建议，且带「不会自动执行」提示；
2. 提交后审批中心出现「AI 建议：…」的单子，`Extra` 含调查号；
3. 提交人自批被拒；
4. 另一位账号批准后生成 ops 任务，`Operator` = 提交人；
5. 全局审批关闭时，提交建议仍然只生成待审批单。
