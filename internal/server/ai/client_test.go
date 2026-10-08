package ai

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nebula/monitor/internal/server/config"
)

// fakeEndpoint 起一个假的 OpenAI 兼容端点，返回可用配置。
func fakeEndpoint(t *testing.T, h http.HandlerFunc) config.AIConfig {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return config.AIConfig{Enabled: true, BaseURL: srv.URL, Model: "test-model"}.Normalize()
}

func okBody(content string, tokens int) map[string]any {
	return map[string]any{
		"model": "test-model",
		"choices": []map[string]any{{
			"message":       map[string]string{"role": "assistant", "content": content},
			"finish_reason": "stop",
		}},
		"usage": map[string]int{"total_tokens": tokens},
	}
}

func TestChatSuccess(t *testing.T) {
	var gotAuth, gotPath string
	cfg := fakeEndpoint(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(okBody("建议检查磁盘", 42))
	})
	cfg.APIKey = "sk-test"

	res, err := NewClient(5 * time.Second).Chat(cfg, []Message{{Role: "user", Content: "hi"}})
	if err != nil {
		t.Fatalf("应成功：%v", err)
	}
	if res.Content != "建议检查磁盘" || res.TotalTokens != 42 {
		t.Fatalf("结果不符：%+v", res)
	}
	if gotPath != "/chat/completions" {
		t.Fatalf("路径 = %q，应为 OpenAI 兼容的 /chat/completions", gotPath)
	}
	if gotAuth != "Bearer sk-test" {
		t.Fatalf("鉴权头 = %q", gotAuth)
	}
}

// 默认关闭的部署里，这条外呼路径**根本不该存在**：未启用时连一个请求都不能发出去。
func TestChatNotReadySendsNothing(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
	}))
	defer srv.Close()

	_, err := NewClient(time.Second).Chat(
		config.AIConfig{Enabled: false, BaseURL: srv.URL, Model: "m"}, nil)
	if err == nil || !strings.Contains(err.Error(), "未配置或未启用") {
		t.Fatalf("应明确报未启用：%v", err)
	}
	if hit {
		t.Fatal("未启用时不应发出任何请求")
	}
}

func TestChatHTTPError(t *testing.T) {
	cfg := fakeEndpoint(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	})
	_, err := NewClient(5 * time.Second).Chat(cfg, []Message{{Role: "user", Content: "hi"}})
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("应带上状态码：%v", err)
	}
}

// HTTP 200 但响应体里带 error（部分网关这么干）——不能当成功。
func TestChatBusinessError(t *testing.T) {
	cfg := fakeEndpoint(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{"message": "quota exceeded"},
		})
	})
	_, err := NewClient(5 * time.Second).Chat(cfg, []Message{{Role: "user", Content: "hi"}})
	if err == nil || !strings.Contains(err.Error(), "quota exceeded") {
		t.Fatalf("业务错误应被识别：%v", err)
	}
}

func TestChatNonJSONAndEmpty(t *testing.T) {
	cfg := fakeEndpoint(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("gateway timeout"))
	})
	_, err := NewClient(5 * time.Second).Chat(cfg, []Message{{Role: "user", Content: "hi"}})
	if err == nil || !strings.Contains(err.Error(), "gateway timeout") {
		t.Fatalf("非 JSON 响应应把片段带出来：%v", err)
	}

	cfg2 := fakeEndpoint(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{}})
	})
	if _, err := NewClient(5 * time.Second).Chat(cfg2, []Message{{Role: "user", Content: "hi"}}); err == nil {
		t.Fatal("空回答应报错而不是返回空内容")
	}
}

func TestChatTimeout(t *testing.T) {
	cfg := fakeEndpoint(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(okBody("late", 1))
	})
	_, err := NewClient(50 * time.Millisecond).Chat(cfg, []Message{{Role: "user", Content: "hi"}})
	if err == nil || !strings.Contains(err.Error(), "调用 AI 端点失败") {
		t.Fatalf("超时应报传输失败：%v", err)
	}
}

// 端点的 query 里可能带凭据（部分网关用 ?api_key=），错误信息里必须剥掉。
func TestRedactEndpoint(t *testing.T) {
	got := RedactEndpoint("https://gw.example.com/v1/chat/completions?api_key=secret123&x=1")
	if strings.Contains(got, "secret123") || strings.Contains(got, "?") {
		t.Fatalf("query 未剥离：%s", got)
	}
	if !strings.HasPrefix(got, "https://gw.example.com/v1/chat/completions") {
		t.Fatalf("路径应保留：%s", got)
	}
}

// 零值陷阱：超时 0 会被 http.Client 读成"无限等待"。
func TestNormalizeDefaults(t *testing.T) {
	c := config.AIConfig{BaseURL: "https://api.example.com/v1/", Model: " m "}.Normalize()
	if c.TimeoutSeconds != 60 {
		t.Fatalf("超时应补 60，实际 %d", c.TimeoutSeconds)
	}
	if c.MaxTokens != 1024 {
		t.Fatalf("maxTokens 应补 1024，实际 %d", c.MaxTokens)
	}
	if c.BaseURL != "https://api.example.com/v1" {
		t.Fatalf("端点应去尾斜杠，实际 %q", c.BaseURL)
	}
	if c.Model != "m" {
		t.Fatalf("模型名应去空白，实际 %q", c.Model)
	}
	if c.Ready() {
		t.Fatal("未启用时 Ready 应为 false")
	}
	if (config.AIConfig{Enabled: true, BaseURL: "u", Model: "m"}).Normalize().Ready() != true {
		t.Fatal("启用且端点与模型齐备时 Ready 应为 true")
	}
}
