package api

import (
	"encoding/json"
	"net/http"
	"strings"
)

// promQLPreviewLimit 是试运行最多回传的序列数。
// 试运行是给人看的：前几条足以判断表达式对不对，全量返回只会让弹窗卡住。
const promQLPreviewLimit = 20

type promQLPreviewItem struct {
	Labels map[string]string `json:"labels"`
	Value  float64           `json:"value"`
}

type promQLPreviewResp struct {
	Count     int                 `json:"count"`
	Truncated bool                `json:"truncated"`
	Series    []promQLPreviewItem `json:"series"`
}

// validatePromQL 用一次真实查询校验表达式可执行性。
// 不引入 PromQL 解析器依赖：表达式由后端方言（VictoriaMetrics 的 MetricsQL 超集）
// 自己回答，与运行时评估走同一条路径，不会出现「校验通过但评估失败」。
func (a *API) validatePromQL(expr string) error {
	_, err := a.store.QueryPromQL(expr)
	return err
}

func (a *API) handlePromQLPreview(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Expr string `json:"expr"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.Expr) == "" {
		http.Error(w, "表达式不能为空", http.StatusBadRequest)
		return
	}
	series, err := a.store.QueryPromQL(body.Expr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	limit := len(series)
	if limit > promQLPreviewLimit {
		limit = promQLPreviewLimit
	}
	resp := promQLPreviewResp{
		Count:     len(series),
		Truncated: len(series) > promQLPreviewLimit,
		Series:    make([]promQLPreviewItem, 0, limit),
	}
	for _, s := range series[:limit] {
		v := 0.0
		if len(s.Points) > 0 {
			v = s.Points[len(s.Points)-1].Value
		}
		resp.Series = append(resp.Series, promQLPreviewItem{Labels: s.Labels, Value: v})
	}
	writeJSON(w, 200, resp)
}
