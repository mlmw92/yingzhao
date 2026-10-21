package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

type staticToolbox struct{ tools []Tool }

func (s staticToolbox) Tools() []Tool { return s.tools }

func testToolbox() Toolbox {
	return staticToolbox{tools: []Tool{{
		Name: "echo", Description: "echo", ReadOnly: true,
		Schema: json.RawMessage(`{"type":"object"}`),
		Handler: func(_ context.Context, args map[string]any) (string, error) {
			return "echo:" + args["q"].(string), nil
		},
	}}}
}

func TestNormalizeBudgetDefaults(t *testing.T) {
	b := normalizeBudget(Budget{})
	if b.MaxRounds != 4 || b.MaxToolCalls != 8 || b.MaxTokens != 16000 || b.Timeout != 120*time.Second {
		t.Fatalf("默认预算 = %+v", b)
	}
}

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
	// 预算耗尽时不能把最后一次工具请求当成结论：界面会显示「已完成」但只有一段 JSON
	if strings.Contains(out.Answer, `"action":"tool"`) || !strings.Contains(out.Answer, "未完成") {
		t.Fatalf("预算耗尽时应给出可读的未完成说明，实际 %q", out.Answer)
	}
}

func TestRunAutoDoesNotFallbackOnAuthError(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"unauthorized"}}`))
	}))
	t.Cleanup(srv.Close)

	cfg := config.AIConfig{Enabled: true, BaseURL: srv.URL, Model: "m", ToolMode: "auto"}
	if _, err := Run(context.Background(), cfg, "sys", "user", testToolbox(), Budget{}); err == nil {
		t.Fatal("401 应直接失败")
	}
	if calls != 1 {
		t.Fatalf("401 不应触发 JSON 回退（会重复出境同一份数据），实际调用 %d 次", calls)
	}
}

func TestRunAutoFallsBackWhenToolsUnsupported(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"tools are not supported by this model"}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{
			"message": map[string]any{"role": "assistant", "content": "结论：正常"},
		}}})
	}))
	t.Cleanup(srv.Close)

	cfg := config.AIConfig{Enabled: true, BaseURL: srv.URL, Model: "m", ToolMode: "auto"}
	out, err := Run(context.Background(), cfg, "sys", "user", testToolbox(), Budget{})
	if err != nil {
		t.Fatalf("不支持工具时应回退 JSON: %v", err)
	}
	if calls != 2 || out.Protocol != "json" {
		t.Fatalf("应回退到 JSON（调用 2 次），实际 calls=%d protocol=%s", calls, out.Protocol)
	}
}

// 模型可能在一轮里批量调用很多工具，直接把预算打满。
// 此时必须再要一次结论（不带工具），而不是把「工具用尽」当答案。
func TestRunFinalizesWhenToolBudgetExhausted(t *testing.T) {
	var finalOK bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if _, withTools := body["tools"]; withTools {
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{
				"message": map[string]any{
					"role": "assistant",
					"tool_calls": []any{map[string]any{
						"id": "c", "type": "function",
						"function": map[string]any{"name": "echo", "arguments": `{"q":"x"}`},
					}},
				},
			}}})
			return
		}
		// 收尾请求必须是纯文本对话：不能夹带 tool_calls / role=tool，
		// 否则部分推理模型会把这些当成待解析的工具语法而不给结论。
		msgs, _ := body["messages"].([]any)
		for _, m := range msgs {
			mm, _ := m.(map[string]any)
			if mm["role"] == "tool" {
				return
			}
			if _, has := mm["tool_calls"]; has {
				return
			}
		}
		finalOK = true
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{
			"message": map[string]any{"role": "assistant", "content": "最终结论：正常"},
		}}})
	}))
	t.Cleanup(srv.Close)

	cfg := config.AIConfig{Enabled: true, BaseURL: srv.URL, Model: "m", ToolMode: "native"}
	out, err := Run(context.Background(), cfg, "sys", "user", testToolbox(), Budget{MaxRounds: 5, MaxToolCalls: 2, MaxTokens: 8000})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Stopped != "tools" || len(out.Steps) != 2 {
		t.Fatalf("应在 2 次工具后停止: %+v", out)
	}
	if !finalOK {
		t.Fatal("收尾请求应使用干净的纯文本对话")
	}
	if out.Answer != "最终结论：正常" {
		t.Fatalf("预算用尽后应再要一次结论，实际 %q", out.Answer)
	}
}

// 超时必须中断在途模型调用，并给出可读的未完成说明；
// 不能因为 HTTP 请求没绑 ctx 而拖到客户端超时。
func TestRunTimeoutProducesIncompleteAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(500 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{
			"message": map[string]any{"role": "assistant", "content": "late"},
		}}})
	}))
	t.Cleanup(srv.Close)

	cfg := config.AIConfig{Enabled: true, BaseURL: srv.URL, Model: "m", ToolMode: "json"}
	start := time.Now()
	out, err := Run(context.Background(), cfg, "sys", "user", testToolbox(), Budget{Timeout: 50 * time.Millisecond})
	if err != nil {
		t.Fatalf("超时应返回未完成结论而不是错误: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 300*time.Millisecond {
		t.Fatalf("超时未中断在途请求，耗时 %v", elapsed)
	}
	if out.Stopped != "timeout" || !strings.Contains(out.Answer, "未完成") {
		t.Fatalf("超时应给出未完成说明: %+v", out)
	}
}

// 预算必须写进系统提示：实机上模型会在信息够了之后继续调工具，
// 把 8 次预算用满，最后只剩一句「调查未完成」。
func TestRunSystemPromptStatesBudget(t *testing.T) {
	var system string
	srv := fakeChat(t, func(_ int, body map[string]any) map[string]any {
		msgs, _ := body["messages"].([]any)
		if len(msgs) > 0 {
			if m, ok := msgs[0].(map[string]any); ok && m["role"] == "system" {
				system, _ = m["content"].(string)
			}
		}
		return map[string]any{"choices": []any{map[string]any{
			"message": map[string]any{"role": "assistant", "content": "结论：正常"},
		}}}
	})
	cfg := config.AIConfig{Enabled: true, BaseURL: srv.URL, Model: "m", ToolMode: "native"}
	if _, err := Run(context.Background(), cfg, "基础提示", "user", testToolbox(),
		Budget{MaxRounds: 4, MaxToolCalls: 8, MaxTokens: 8000}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, want := range []string{"基础提示", "4 轮", "8 次工具调用", "8000", "足够就立即给出结论"} {
		if !strings.Contains(system, want) {
			t.Fatalf("系统提示缺少 %q：%s", want, system)
		}
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
