package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nebula/monitor/internal/server/config"
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
