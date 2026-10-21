package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mlmw92/yingzhao/internal/server/config"
)

// maxRespBytes 是响应体读取上限。
//
// 比通知的 4 KiB 大得多（大模型的回答本来就长），但仍然要有上限：
// 一个恶意/异常端点返回无限流会把内存吃光。
const maxRespBytes = 256 << 10

// Message 是一条对话消息（OpenAI 兼容格式）。
type Message struct {
	Role       string     `json:"role"` // system | user | assistant | tool
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall 是模型请求调用工具（OpenAI 兼容格式）。
type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
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

// Result 是一次调用的结果。
type Result struct {
	Content      string        // 模型回答正文
	Model        string        // 端点回显的模型名（可能与我们请求的不同）
	TotalTokens  int           // 用量（端点没回则为 0）
	Elapsed      time.Duration // 端到端耗时
	FinishReason string        // stop / length / …
	ToolCalls    []ToolCall    // 模型请求调用的工具（原生协议）
}

// Client 是 OpenAI 兼容的对话客户端：只做一件事——把 messages 发出去、把回答取回来。
//
// **不重试**：重试等于把同一份运维数据再出境一次。失败就是失败，由调用方决定要不要再点一次
// （与 `alert/notifier.go` 同一取向）。
type Client struct {
	http *http.Client
}

// NewClient 创建客户端。超时按配置传入（LLM 比 webhook 慢得多，默认 60s）。
func NewClient(timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &Client{http: &http.Client{Timeout: timeout}}
}

// chatRequest 是 OpenAI 兼容的请求体（只放我们真正会用的字段）。
type chatRequest struct {
	Model       string     `json:"model"`
	Messages    []Message  `json:"messages"`
	Temperature float64    `json:"temperature"`
	MaxTokens   int        `json:"max_tokens"`
	Stream      bool       `json:"stream"`
	Tools       []ToolSpec `json:"tools,omitempty"`
}

// chatResponse 是响应体（只解我们需要的部分）。
type chatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message      Message `json:"message"`
		FinishReason string  `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// Chat 发起一次对话补全。
//
// 三类失败要分得开（现场才知道该改哪一头）：
//   - **配置问题**：未启用 / 端点或模型为空 → 直接返回，不发请求；
//   - **传输问题**：连不上、超时、TLS → 错误里带**脱敏后的**端点（密钥不进日志）；
//   - **服务端问题**：HTTP ≥300 或响应体里带 error → 带上状态码与端点给的消息。
func (c *Client) Chat(cfg config.AIConfig, messages []Message) (*Result, error) {
	return c.chat(context.Background(), cfg, messages, nil)
}

// ChatCtx 与 Chat 相同，但请求绑定调用方的 ctx（超时可中断在途请求）。
func (c *Client) ChatCtx(ctx context.Context, cfg config.AIConfig, messages []Message) (*Result, error) {
	return c.chat(ctx, cfg, messages, nil)
}

// ChatWithTools 发起一次可能带工具声明的对话补全。
// tools 为空时与 Chat 等价。
func (c *Client) ChatWithTools(cfg config.AIConfig, messages []Message, tools []ToolSpec) (*Result, error) {
	return c.chat(context.Background(), cfg, messages, tools)
}

// ChatWithToolsCtx 同 ChatWithTools，但请求绑定 ctx。
func (c *Client) ChatWithToolsCtx(ctx context.Context, cfg config.AIConfig, messages []Message, tools []ToolSpec) (*Result, error) {
	return c.chat(ctx, cfg, messages, tools)
}

func (c *Client) chat(ctx context.Context, cfg config.AIConfig, messages []Message, tools []ToolSpec) (*Result, error) {
	cfg = cfg.Normalize()
	if !cfg.Ready() {
		return nil, errors.New("AI 未配置或未启用（需要启用、端点与模型）")
	}
	body, err := json.Marshal(chatRequest{
		Model:       cfg.Model,
		Messages:    messages,
		Temperature: cfg.Temperature,
		MaxTokens:   cfg.MaxTokens,
		Tools:       tools,
	})
	if err != nil {
		return nil, fmt.Errorf("构造请求失败: %w", err)
	}

	endpoint := strings.TrimRight(cfg.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("构造请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}

	started := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("调用 AI 端点失败（%s）：%s", RedactEndpoint(endpoint), redactTransportErr(err))
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxRespBytes))
	if err != nil {
		return nil, fmt.Errorf("读取 AI 响应失败（%s）：%w", RedactEndpoint(endpoint), err)
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("AI 端点返回 %d（%s）：%s", resp.StatusCode, RedactEndpoint(endpoint), snippet(raw))
	}

	var out chatResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		// 有些自建端点失败时返回纯文本：把片段带上，比"解析失败"有用得多。
		return nil, fmt.Errorf("AI 响应不是合法 JSON（%s）：%s", RedactEndpoint(endpoint), snippet(raw))
	}
	if out.Error != nil && out.Error.Message != "" {
		// HTTP 200 但业务失败（部分网关这么干），不能当成功。
		return nil, fmt.Errorf("AI 端点报错：%s", out.Error.Message)
	}
	if len(out.Choices) == 0 ||
		(strings.TrimSpace(out.Choices[0].Message.Content) == "" && len(out.Choices[0].Message.ToolCalls) == 0) {
		return nil, fmt.Errorf("AI 端点返回了空回答（%s）", RedactEndpoint(endpoint))
	}

	return &Result{
		Content:      out.Choices[0].Message.Content,
		Model:        firstNonEmpty(out.Model, cfg.Model),
		TotalTokens:  out.Usage.TotalTokens,
		FinishReason: out.Choices[0].FinishReason,
		Elapsed:      time.Since(started),
		ToolCalls:    out.Choices[0].Message.ToolCalls,
	}, nil
}

// TestConnection 做一次最小连通性测试（MaxTokens 压到 16，只验证"能通、能解析"）。
func (c *Client) TestConnection(cfg config.AIConfig) (*Result, error) {
	probe := cfg.Normalize()
	probe.MaxTokens = 16
	return c.Chat(probe, []Message{{Role: "user", Content: "ping"}})
}

// RedactEndpoint 剥掉端点里的查询串与凭据，只留 scheme://host/path。
//
// 有些 OpenAI 兼容网关把 key 放在 query（`?api_key=...`），
// 直接把它拼进错误信息就等于把密钥写进日志与界面。
func RedactEndpoint(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil {
		return endpoint
	}
	u.RawQuery = ""
	u.Fragment = ""
	u.User = nil
	return u.String()
}

// redactTransportErr 把传输层错误里的完整 URL 换成脱敏端点。
func redactTransportErr(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Sprintf("%s: %v", RedactEndpoint(ue.URL), ue.Err)
	}
	return err.Error()
}

// snippet 截断响应片段用于报错（并压掉换行，避免日志被撑开）。
func snippet(b []byte) string {
	s := strings.Join(strings.Fields(string(b)), " ")
	if len(s) > 300 {
		return s[:300] + "…"
	}
	if s == "" {
		return "(空响应)"
	}
	return s
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
