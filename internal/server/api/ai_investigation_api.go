package api

import (
	"net/http"

	"github.com/nebula/monitor/internal/server/ai"
)

// SetInvestigationStore 注入 AI 调查结果存储（未注入时查询返回空）。
func (a *API) SetInvestigationStore(s *ai.InvestigationStore) { a.aiInv = s }

// GET /api/v1/ai/investigations?alertId=xxx
func (a *API) handleAIInvestigations(w http.ResponseWriter, r *http.Request) {
	if a.aiInv == nil {
		writeJSON(w, http.StatusOK, map[string]any{"investigation": nil})
		return
	}
	alertID := r.URL.Query().Get("alertId")
	if alertID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少 alertId"})
		return
	}
	inv, ok := a.aiInv.ByAlert(alertID)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"investigation": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"investigation": inv})
}
