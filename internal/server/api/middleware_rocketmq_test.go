package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nebula/monitor/internal/model"
	"github.com/nebula/monitor/internal/server/instancereg"
)

// TestRocketMQInstances_VersionFromRegistry 锁定「版本」列的数据来源。
//
// Agent 采集时把 version 放在实例结构里上报（落进注册表），而 rocketmq_instance_up
// 的 labels 里没有 version。此前的聚合只用指标 labels 建实例，于是「版本」列恒为空。
func TestRocketMQInstances_VersionFromRegistry(t *testing.T) {
	a := scopeTestAPI(t)
	a.alerts = &alertStoreStub{}
	a.store = &seriesStore{series: map[string][]model.Series{
		"rocketmq_instance_up": {{
			Labels: map[string]string{
				"node": "web-01", "instance": "10.0.0.10:9876",
				"role": "nameserver", "group": "dev-rocketmq", "name": "dev-rocketmq",
			},
			Points: []model.Point{{Timestamp: time.Now().UnixMilli(), Value: 1}},
		}},
	}}

	instancereg.Default.SetRocketMQ("web-01", []model.RocketMQInstance{{
		Node: "web-01", Instance: "10.0.0.10:9876", Name: "dev-rocketmq",
		Role: "nameserver", Version: "5.1.4", Group: "dev-rocketmq", Up: true,
	}})
	t.Cleanup(func() { instancereg.Default.SetRocketMQ("web-01", nil) })

	rec := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, reqWith(globalPrincipal("middleware:read"), http.MethodGet, "/api/v1/middleware/rocketmq/instances", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d（body=%s）", rec.Code, rec.Body.String())
	}

	var resp struct {
		Instances []struct {
			Node    string `json:"node"`
			Version string `json:"version"`
			Role    string `json:"role"`
			Up      bool   `json:"up"`
		} `json:"instances"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应解析失败：%v", err)
	}
	if len(resp.Instances) != 1 {
		t.Fatalf("实例数 = %d，want 1（body=%s）", len(resp.Instances), rec.Body.String())
	}
	got := resp.Instances[0]
	if got.Version != "5.1.4" {
		t.Errorf("version = %q，want 5.1.4（注册表里的版本被指标 labels 的空值覆盖了）", got.Version)
	}
	if got.Role != "nameserver" {
		t.Errorf("role = %q，want nameserver", got.Role)
	}
	if !got.Up {
		t.Errorf("up = false，want true（up 指标值为 1）")
	}
}
