package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/mlmw92/yingzhao/internal/server/ai"
	"github.com/mlmw92/yingzhao/internal/server/audit"
	"github.com/mlmw92/yingzhao/internal/server/config"
)

// AI 助手（大模型）配置接口。设计件：docs/superpowers/specs/2026-10-08-ai-assistant-design.md
//
// 与既有配置类接口（notify / ui / screen / retention **都不写审计**）**有意不同**：
// 本配置决定"运维数据往哪里流"，属于安全相关配置，"谁在什么时候把端点改成了什么"必须能事后回答。
//
// 三条脱敏与校验口径：
//   - GET **不回显 APIKey**（照 handleNotifyGet）；
//   - PUT 空 APIKey **沿用旧值**（照 handleNotifyPut，前端「不修改请留空」）；
//   - 日志与审计里出现的端点一律过 ai.RedactEndpoint（剥掉 query 里的凭据）。

// handleAIGet 返回当前 AI 配置（不含明文密钥）。
func (a *API) handleAIGet(w http.ResponseWriter, r *http.Request) {
	if a.aiMgr == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "AI 配置未初始化"})
		return
	}
	cfg := a.aiMgr.Get()
	c := cfg.Normalize()
	c.APIKey = "" // 脱敏：前端未修改时留空，由 PUT 侧保留原值
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"config": c,
		// ready 单独给出：界面要能区分"没配"与"配了但没启用"，
		// 而不是自己在前端重拼一遍条件（两处判定迟早会不一致）。
		"ready": cfg.Ready(),
		// 是否已保存过密钥：界面用它决定 placeholder 文案（已配置 / 未配置）。
		"hasApiKey": cfg.APIKey != "",
	})
}

// handleAIPut 保存 AI 配置并热生效。空 APIKey 沿用原值，避免误清空。
func (a *API) handleAIPut(w http.ResponseWriter, r *http.Request) {
	if a.aiMgr == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "AI 配置未初始化"})
		return
	}
	var incoming config.AIConfig
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体解析失败: " + err.Error()})
		return
	}
	cur := a.aiMgr.Get()
	if incoming.APIKey == "" {
		incoming.APIKey = cur.APIKey
	}
	if err := validateAIConfig(incoming); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := a.aiMgr.Save(incoming); err != nil {
		slog.Error("保存 AI 配置失败", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "保存失败: " + err.Error()})
		return
	}
	saved := a.aiMgr.Get().Normalize()
	a.recordAI(r, "ai:config:update", fmt.Sprintf(
		"启用=%v 端点=%s 模型=%s 超时=%ds 允许高级别数据=%v",
		saved.Enabled, ai.RedactEndpoint(saved.BaseURL), saved.Model, saved.TimeoutSeconds, saved.AllowSensitive))
	writeJSON(w, http.StatusOK, map[string]string{"message": "已保存并生效"})
}

// handleAITest 用**当前已保存的配置**做一次最小连通性测试。
//
// 刻意不接受请求体里的临时配置：那样"测试通过"与"保存后能用"可能不是同一份配置，
// 而这条链路的意义正是验证真实配置（与通知测试同一取向）。
func (a *API) handleAITest(w http.ResponseWriter, r *http.Request) {
	if a.aiMgr == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "AI 配置未初始化"})
		return
	}
	cfg := a.aiMgr.Get()
	if !cfg.Ready() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "AI 未配置或未启用（需要启用、端点与模型）"})
		return
	}
	client := ai.NewClient(time.Duration(cfg.TimeoutSeconds) * time.Second)
	res, err := client.TestConnection(cfg)
	if err != nil {
		// 失败也记审计：连不上的端点同样是"这次尝试"的事实。
		a.recordAI(r, "ai:config:test", "失败："+err.Error())
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	a.recordAI(r, "ai:config:test", fmt.Sprintf(
		"成功：模型=%s 耗时=%dms token=%d", res.Model, res.Elapsed.Milliseconds(), res.TotalTokens))
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":     "连通正常",
		"model":       res.Model,
		"elapsedMs":   res.Elapsed.Milliseconds(),
		"totalTokens": res.TotalTokens,
		"reply":       replySnippet(res.Content),
	})
}

// validateAIConfig 校验配置。
//
// 只在**启用时**要求端点与模型：允许"先关掉、再慢慢清理配置"，否则关掉之后就没法保存了。
func validateAIConfig(c config.AIConfig) error {
	if !c.Enabled {
		return nil
	}
	if c.BaseURL == "" {
		return errors.New("启用时必须填写端点地址（OpenAI 兼容，如 https://api.deepseek.com/v1）")
	}
	if c.Model == "" {
		return errors.New("启用时必须填写模型名")
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("端点地址必须是 http(s):// 开头的完整地址")
	}
	return nil
}

// recordAI 写一条 AI 相关审计。
func (a *API) recordAI(r *http.Request, action, detail string) {
	if a.audit == nil {
		return
	}
	_ = a.audit.Record(audit.Event{
		User: AuthenticatedUser(r), Method: r.Method, Path: r.URL.Path,
		Status: http.StatusOK, RemoteIP: audit.ClientIP(r), Succeeded: true,
		RequestID: RequestID(r),
		Category:  "ai", Action: action, Detail: detail,
	})
}

// replySnippet 截断测试回复用于界面展示（只用于确认"真的通了"）。
func replySnippet(s string) string {
	r := []rune(s)
	if len(r) > 120 {
		return string(r[:120]) + "…"
	}
	return s
}
