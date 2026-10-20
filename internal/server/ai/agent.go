package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nebula/monitor/internal/server/config"
)

// Step 是一次工具调用及其结果（会持久化给前端展示，因此带 JSON 标签）。
type Step struct {
	Tool   string         `json:"tool"`
	Args   map[string]any `json:"args,omitempty"`
	Result string         `json:"result,omitempty"`
	Err    string         `json:"err,omitempty"`
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
	if b.MaxRounds <= 0 {
		b.MaxRounds = 4
	}
	if b.MaxToolCalls <= 0 {
		b.MaxToolCalls = 8
	}
	if b.MaxTokens <= 0 {
		b.MaxTokens = 8000
	}
	if b.Timeout <= 0 {
		b.Timeout = 30 * time.Second
	}
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
	if mode == "" {
		mode = "auto"
	}

	ctx, cancel := context.WithTimeout(ctx, budget.Timeout)
	defer cancel()

	out := Outcome{Protocol: mode, Steps: []Step{}}
	msgs := []Message{{Role: "system", Content: system}, {Role: "user", Content: user}}
	client := NewClient(time.Duration(cfg.TimeoutSeconds) * time.Second)
	toolCalls := 0
	lastContent := ""

	callModel := func(native bool) (*Result, error) {
		if !native {
			msgs[0] = Message{Role: "system", Content: jsonToolPrompt(tools, system)}
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
		if !ok {
			out.Answer = res.Content // 解析失败按结论处理
			out.Stopped = "done"
			return out, nil
		}
		if env.Action == "final" {
			out.Answer = firstNonEmpty(env.Answer, res.Content)
			out.Stopped = "done"
			return out, nil
		}
		if env.Action != "tool" {
			out.Answer = res.Content
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
		(strings.Contains(s, "tool") && strings.Contains(s, "unsupported"))
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
