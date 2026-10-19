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

// TestNginxInstances_DeclarativeShape 锁定 Nginx 声明化后的形状。
//
// 与其它类型相比，Nginx 多了两件事：实例地址优先展示节点 IP（nodeIp），
// 版本只存在于注册表。声明化后这两点必须仍然成立。
func TestNginxInstances_DeclarativeShape(t *testing.T) {
	a := scopeTestAPI(t)
	a.alerts = &alertStoreStub{}
	now := time.Now().UnixMilli()
	a.store = &seriesStore{series: map[string][]model.Series{
		"nginx_instance_up": {{
			Labels: map[string]string{"node": "web-01", "instance": "127.0.0.1:80", "group": "edge"},
			Points: []model.Point{{Timestamp: now, Value: 1}},
		}},
		"nginx_active_connections": {{
			Labels: map[string]string{"node": "web-01", "instance": "127.0.0.1:80"},
			Points: []model.Point{{Timestamp: now, Value: 3}},
		}},
	}}
	instancereg.Default.SetNginx("web-01", []model.NginxInstance{{
		Node: "web-01", Instance: "127.0.0.1:80", Name: "edge-nginx",
		Group: "edge", Version: "1.24.0", Up: true,
	}})
	t.Cleanup(func() { instancereg.Default.SetNginx("web-01", nil) })

	rec := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, reqWith(globalPrincipal("middleware:read"), http.MethodGet, "/api/v1/middleware/nginx/instances", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d（body=%s）", rec.Code, rec.Body.String())
	}

	var resp struct {
		Type      string `json:"type"`
		Instances []struct {
			Name    string `json:"name"`
			Version string `json:"version"`
			NodeIP  string `json:"nodeIp"`
			Up      bool   `json:"up"`
			Metrics []struct {
				Key   string  `json:"key"`
				Value float64 `json:"value"`
			} `json:"metrics"`
		} `json:"instances"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应解析失败：%v（body=%s）", err, rec.Body.String())
	}
	if resp.Type != "nginx" {
		t.Errorf("type = %q，want nginx", resp.Type)
	}
	if len(resp.Instances) != 1 {
		t.Fatalf("实例数 = %d，want 1（body=%s）", len(resp.Instances), rec.Body.String())
	}
	got := resp.Instances[0]
	if got.NodeIP != "10.0.0.1" {
		t.Errorf("nodeIp = %q，want 10.0.0.1（节点地址）", got.NodeIP)
	}
	if got.Version != "1.24.0" {
		t.Errorf("version = %q，want 1.24.0（来自注册表）", got.Version)
	}
	if !got.Up {
		t.Errorf("up = false，want true")
	}
	var active float64
	found := false
	for _, m := range got.Metrics {
		if m.Key == "nginx_active_connections" {
			active, found = m.Value, true
		}
	}
	if !found || active != 3 {
		t.Errorf("nginx_active_connections = %v（存在=%v），want 3", active, found)
	}
}
