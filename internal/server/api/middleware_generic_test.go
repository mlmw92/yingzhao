package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nebula/monitor/internal/model"
)

// TestGenericMWInstances_NewestSampleWinsMetadata 锁定「同一实例的多条样本里，
// 元信息（group/role/version）跟随最新样本，旧样本不得覆盖」。
//
// 触发场景：改了实例分组后，旧序列在时序库的即时查询回看窗口内仍然存在；
// 若旧样本在遍历顺序上靠后，就会把界面上的分组改回旧值。
func TestGenericMWInstances_NewestSampleWinsMetadata(t *testing.T) {
	a := scopeTestAPI(t)
	a.store = &seriesStore{series: map[string][]model.Series{
		"demo_up": {
			// 新样本在前
			{Labels: map[string]string{"node": "web-01", "instance": "i1", "group": "new-group"},
				Points: []model.Point{{Timestamp: 2000, Value: 1}}},
			// 旧样本在后：不得把 group 改回 old-group，也不得把在线改成离线
			{Labels: map[string]string{"node": "web-01", "instance": "i1", "group": "old-group"},
				Points: []model.Point{{Timestamp: 1000, Value: 0}}},
		},
	}}

	spec := mwGenericSpec{Type: "demo", Label: "Demo", UpMetric: "demo_up",
		Registry: func() []mwGenericRegistryRef { return nil }}
	rec := httptest.NewRecorder()
	a.handleGenericMWInstances(spec)(rec, reqWith(globalPrincipal("middleware:read"), http.MethodGet, "/", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d（body=%s）", rec.Code, rec.Body.String())
	}

	var resp struct {
		Instances []struct {
			Group string `json:"group"`
			Up    bool   `json:"up"`
		} `json:"instances"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应解析失败：%v", err)
	}
	if len(resp.Instances) != 1 {
		t.Fatalf("实例数 = %d，want 1", len(resp.Instances))
	}
	if got := resp.Instances[0].Group; got != "new-group" {
		t.Errorf("group = %q，want new-group（旧样本覆盖了最新样本的元信息）", got)
	}
	if !resp.Instances[0].Up {
		t.Errorf("up = false，want true（旧样本的 up=0 覆盖了最新样本）")
	}
}
