package storage

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestQueryPromQLForwardsExpression(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query().Get("query")
		json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data": map[string]any{
				"resultType": "vector",
				"result": []any{map[string]any{
					"metric": map[string]string{"node": "n1"},
					"value":  []any{1700000000, "91.2"},
				}},
			},
		})
	}))
	defer srv.Close()

	s := New(srv.URL, 5, 5)
	series, err := s.QueryPromQL(`cpu_usage > 85`)
	if err != nil {
		t.Fatalf("QueryPromQL: %v", err)
	}
	if got != `cpu_usage > 85` {
		t.Fatalf("转发的表达式 = %q", got)
	}
	if len(series) != 1 || series[0].Labels["node"] != "n1" || series[0].Points[0].Value != 91.2 {
		t.Fatalf("解析结果不符: %+v", series)
	}
}
