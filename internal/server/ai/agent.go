package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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
		// token 预算按**累计 usage** 计，而上下文每轮都在增长：
		// 实机 3 轮调查实测 12.7k–15.3k tokens，8000 会在第 2–3 轮就把调查截断。
		b.MaxTokens = 16000
	}
	if b.Timeout <= 0 {
		// 一次调查要跑多轮「模型→工具→模型」，真实模型单轮就要数秒：
		// 默认值必须大于客户端单次超时，否则第一轮还没回就整体超时。
		b.Timeout = 120 * time.Second
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

// budgetPrompt 把本次调查的预算写进系统提示。
//
// 实机上模型常在信息已经足够后继续调工具，把 8 次预算用满，最后只剩一句
// 「调查未完成」。把上限和收束要求提前告诉它，让它自己权衡「再查一次」还是「下结论」。
func budgetPrompt(system string, b Budget) string {
	var sb strings.Builder
	sb.WriteString(system)
	fmt.Fprintf(&sb, "\n\n本次调查预算：最多 %d 轮对话、%d 次工具调用、%d tokens。", b.MaxRounds, b.MaxToolCalls, b.MaxTokens)
	sb.WriteString("请先说明排查思路，再按需调用工具；每拿到一次工具结果就判断信息是否足够，")
	sb.WriteString("足够就立即给出结论，不要为了用满预算而继续调用。")
	return sb.String()
}

// Run 跑一次带工具的调查。
func Run(ctx context.Context, cfg config.AIConfig, system, user string, toolbox Toolbox, budget Budget) (Outcome, error) {
	budget = normalizeBudget(budget)
	system = budgetPrompt(system, budget)
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
			return client.ChatCtx(ctx, cfg, msgs)
		}
		return client.ChatWithToolsCtx(ctx, cfg, msgs, toolSpecs(tools))
	}

	// finalize 在工具预算用尽时再要一次结论（不带工具）。
	// 模型可能在一轮里批量调用很多工具把预算打满，直接停会把「工具用尽」当答案。
	//
	// 用**干净的纯文本对话**收尾，而不是复用带 tool_calls / role=tool 的历史：
	// 部分推理模型会把工具语法当成待解析内容，只输出思考而不给结论。
	finalize := func() string {
		if out.Tokens >= budget.MaxTokens {
			return ""
		}
		var sb strings.Builder
		sb.WriteString("已获得的工具结果：\n")
		for _, s := range out.Steps {
			// 汇总里每条只留摘要：8 条完整结果叠加会把上下文撑大，
			// 推理模型把输出预算耗在思考上，正文会是空的。
			fmt.Fprintf(&sb, "- %s %v → %s\n", s.Tool, s.Args, truncate(stepContent(s), 600))
		}
		sb.WriteString("\n工具调用次数已达上限。请基于以上信息直接给出结论，不要再请求调用工具。")
		fcfg := cfg
		if fcfg.MaxTokens < 2048 {
			fcfg.MaxTokens = 2048 // 推理模型需要足够 token 才能产出正文
		}
		res, err := client.ChatCtx(ctx, fcfg, []Message{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
			{Role: "user", Content: sb.String()},
		})
		if err != nil {
			slog.Warn("AI 收尾结论生成失败", "err", err)
			return ""
		}
		out.Tokens += res.TotalTokens
		return strings.TrimSpace(res.Content)
	}

	native := mode == "native" || mode == "auto"
	for round := 0; round < budget.MaxRounds; round++ {
		if ctx.Err() != nil {
			out.Stopped = "timeout"
			out.Answer = incompleteAnswer("超时", len(out.Steps), lastContent)
			break
		}
		res, err := callModel(native)
		if err != nil {
			if ctx.Err() != nil {
				// 超时中断的是在途请求：返回未完成结论，而不是把它当配置/网络错误
				out.Stopped = "timeout"
				out.Answer = incompleteAnswer("超时", len(out.Steps), lastContent)
				return out, nil
			}
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
					out.Answer = firstNonEmpty(finalize(),
						incompleteAnswer("工具调用次数用尽", len(out.Steps), lastContent))
					return out, nil
				}
				toolCalls++
				step := executeTool(ctx, tools, tc.Function.Name, parseArgs(tc.Function.Arguments))
				out.Steps = append(out.Steps, step)
				msgs = append(msgs, Message{Role: "tool", ToolCallID: tc.ID, Content: stepContent(step)})
			}
			if out.Tokens >= budget.MaxTokens {
				out.Stopped = "tokens"
				out.Answer = incompleteAnswer("token 预算用尽", len(out.Steps), lastContent)
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
			out.Answer = firstNonEmpty(finalize(),
				incompleteAnswer("工具调用次数用尽", len(out.Steps), lastContent))
			return out, nil
		}
		toolCalls++
		step := executeTool(ctx, tools, env.Tool, env.Args)
		out.Steps = append(out.Steps, step)
		msgs = append(msgs, Message{Role: "assistant", Content: res.Content})
		msgs = append(msgs, Message{Role: "user", Content: "工具结果：\n" + stepContent(step)})
		if out.Tokens >= budget.MaxTokens {
			out.Stopped = "tokens"
			out.Answer = incompleteAnswer("token 预算用尽", len(out.Steps), lastContent)
			return out, nil
		}
	}

	if out.Stopped == "" {
		out.Stopped = "rounds"
	}
	out.Answer = incompleteAnswer("轮次用尽", len(out.Steps), lastContent)
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
	// 只有「明确说不支持工具」才回退：401/403/429 等 4xx 重跑一遍只是重复出境，
	// 不会因为换协议就好。
	status := strings.Contains(s, "返回 400") || strings.Contains(s, "返回 404") || strings.Contains(s, "返回 422")
	if !status {
		return false
	}
	return strings.Contains(s, "tool") || strings.Contains(s, "function") || strings.Contains(s, "unsupported")
}

// incompleteAnswer 在预算耗尽时给出人可读的说明。
// 不能把最后一次工具请求的 JSON 当结论——界面会显示「已完成」却没有内容。
func incompleteAnswer(reason string, steps int, last string) string {
	if _, isEnvelope := parseEnvelope(last); isEnvelope {
		last = ""
	}
	if last != "" {
		return last + "\n\n（调查未完成：" + reason + "）"
	}
	return fmt.Sprintf("调查未完成：%s（已获得 %d 条工具结果，详见步骤）", reason, steps)
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

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…（已截断）"
}
