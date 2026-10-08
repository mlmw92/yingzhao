package ai

import (
	"fmt"
	"strings"
)

// 敏感度分级（设计件 §3.1）。分级的价值在**默认值**：不打开 AllowSensitive 时，
// 高级别内容在 API 层**根本不会被取出来**——不是"先取出来再过滤"。
const (
	LevelLow  = "low"  // 主机名、类型、分组、标签、指标名
	LevelMid  = "mid"  // 指标统计值、告警标题与级别、智能分析结论、资产名与标签、变更历史
	LevelHigh = "high" // 属性值（可能含连接串/口令）、日志正文、命令输出
)

// Section 是上下文里的一段：标题 + 逐行文本 + 敏感度。
type Section struct {
	Title string
	Lines []string
	Level string
}

// DiagnosticContext 是一次诊断的输入（由 API 层从各域取数后填好）。
//
// **本包不认识 node / asset / alert**：依赖方向要保持单向——API 层持有各 provider，由它取数；
// ai 包只负责"把已经拿到的数据变成一次请求"。这与 report 包不依赖 auth 是同一条理由。
type DiagnosticContext struct {
	Kind     string // node | asset | alert
	Subject  string // 人读的对象名
	Actor    string
	Sections []Section
	// Sensitive 由 API 层设置：本次**实际**取到了高级别内容（进审计用）。
	Sensitive bool
	// WithheldTitles 是"本来属于这个对象、但因未开启 AllowSensitive 而**根本没取**"的段标题。
	//
	// 与 HighLevelTitles 的区别很重要：后者是"取到了又被过滤"（第二道防线，正常不该发生），
	// 前者是"压根没取"（第一道防线）。界面必须说清"模型没看到什么"，
	// 否则用户会奇怪"为什么它没提到我的 IP"。
	WithheldTitles []string
}

// MaxContextChars 是上下文预算（设计件 D10）。
//
// 不设上限是双输：费用按 token 走，而"把一个月的指标全塞进去"对回答质量也没帮助。
const MaxContextChars = 24000

// systemPrompt 是固定的系统提示词。
//
// 三条约束都对着具体风险：① "只依据收到的数据"——模型很擅长编一个像模像样的排查步骤；
// ② "不执行处置"——平台既有边界就是只读辅助；③ "样本不足要如实说"——把"没数据"读成"没问题"
// 是监控类产品最典型的误导（智能分析的 Coverage 就是为这件事存在的）。
const systemPrompt = `你是 Linux 与中间件运维助手。你会收到一台主机（或一个资产、一条告警）的现状数据。

请给出排查建议，遵守以下规则：
1. **只依据收到的数据**下结论；数据里没有的东西，明确说"数据未提供"，不要猜；
2. 建议要具体可执行（看哪个指标、查哪类日志、检查哪个配置），不要给"加强监控"这类空话；
3. **不要建议执行任何写操作或自动化处置**——你只给建议，平台不会自动执行；
4. 如果数据显示"样本不足"或"无法预测"，要如实说出来，**不要把无数据当成正常**；
5. 用中文回答，markdown 小标题组织，控制在 400 字以内。`

// BuildMessages 组装对话消息，返回 (messages, 是否因预算被截断)。
//
// allowSensitive=false 时**高级别段整段丢弃**：宁可让模型少知道一点，也不能把可能含口令的
// 属性值发出去。这是第二道防线——第一道在 API 层（不打开时压根不取）。
func BuildMessages(ctx DiagnosticContext, allowSensitive bool) ([]Message, bool) {
	var b strings.Builder
	fmt.Fprintf(&b, "对象类型：%s\n对象：%s\n", ctx.Kind, ctx.Subject)
	if ctx.Actor != "" {
		fmt.Fprintf(&b, "发起人：%s\n", ctx.Actor)
	}

	truncated := false
	for _, s := range ctx.Sections {
		if s.Level == LevelHigh && !allowSensitive {
			continue
		}
		block := renderSection(s)
		if b.Len()+len(block) > MaxContextChars {
			// 截断要**说出来**：模型与用户都该知道上下文不完整，
			// 否则一个基于半份数据的结论看起来和完整数据的一样可信。
			truncated = true
			break
		}
		b.WriteString(block)
	}
	if truncated {
		b.WriteString("\n（注意：以上数据因长度限制被截断，可能不完整。）\n")
	}

	return []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: b.String()},
	}, truncated
}

// renderSection 把一段渲染成 markdown 列表。
func renderSection(s Section) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n## %s\n", s.Title)
	for _, line := range s.Lines {
		fmt.Fprintf(&b, "- %s\n", line)
	}
	return b.String()
}

// HighLevelTitles 返回上下文里所有高级别段的标题（界面据此如实说明"哪些没发出去"）。
func HighLevelTitles(ctx DiagnosticContext) []string {
	out := make([]string, 0, 2)
	for _, s := range ctx.Sections {
		if s.Level == LevelHigh {
			out = append(out, s.Title)
		}
	}
	return out
}
