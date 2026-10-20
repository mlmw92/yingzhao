package api

import (
	"encoding/json"
	"net/http"
)

// handleUIGet GET /api/v1/ui/settings -> {name, logo}
// 允许匿名访问：登录页与未登录场景也需要展示系统名称与 Logo。
func (a *API) handleUIGet(w http.ResponseWriter, r *http.Request) {
	cfg := a.uiMgr.Get()
	if cfg.Name == "" {
		cfg.Name = "英招"
	}
	writeJSON(w, http.StatusOK, cfg)
}

// handleUIPut PUT /api/v1/ui/settings {name, logo} -> 持久化品牌配置。
// 写操作受登录鉴权保护（AuthMiddleware 对匿名 GET 放行、PUT 仍要求 token）。
func (a *API) handleUIPut(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     *string   `json:"name"`
		Logo     *string   `json:"logo"`
		Footer   *string   `json:"footer"`
		Subtitle *string   `json:"subtitle"`
		Disabled *[]string `json:"disabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
		return
	}
	// 部分更新：未出现的字段保持原值。
	// 此前整体替换导致「只保存模块开关」把名称/Logo/副标题一起清空，
	// 以及保存站点设置会丢副标题（前端发了、后端没接）。
	cfg := a.uiMgr.Get()
	if body.Name != nil {
		cfg.Name = *body.Name
	}
	if body.Logo != nil {
		cfg.Logo = *body.Logo
	}
	if body.Footer != nil {
		cfg.Footer = *body.Footer
	}
	if body.Subtitle != nil {
		cfg.Subtitle = *body.Subtitle
	}
	if body.Disabled != nil {
		cfg.Disabled = *body.Disabled
	}
	if err := a.uiMgr.Save(cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "config": a.uiMgr.Get()})
}
