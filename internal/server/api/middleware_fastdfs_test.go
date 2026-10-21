package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mlmw92/yingzhao/internal/model"
	"github.com/mlmw92/yingzhao/internal/server/instancereg"
)

// TestFastDFSInstances_DeclarativeShape 锁定 FastDFS 实例接口的声明式形状。
//
// 改造前 FastDFS 手写了一份与通用 handler 逐行相同的聚合逻辑，返回扁平字段
// （totalSpaceMB 等）。声明化后与其它类型一致：指标走 metrics 数组，
// 字节到 MB 的换算由声明里的 Scale 决定，而不是写死在 handler 里。
func TestFastDFSInstances_DeclarativeShape(t *testing.T) {
	a := scopeTestAPI(t)
	a.alerts = &alertStoreStub{}
	now := time.Now().UnixMilli()
	labels := map[string]string{"node": "web-01", "instance": "10.0.0.10:22122"}
	a.store = &seriesStore{series: map[string][]model.Series{
		"fastdfs_up": {{
			Labels: map[string]string{
				"node": "web-01", "instance": "10.0.0.10:22122",
				"role": "tracker", "group": "g1",
			},
			Points: []model.Point{{Timestamp: now, Value: 1}},
		}},
		"fastdfs_total_space": {{
			Labels: labels,
			Points: []model.Point{{Timestamp: now, Value: 1048576}}, // 1 MB（字节）
		}},
		"fastdfs_storage_online_count": {{
			Labels: labels,
			Points: []model.Point{{Timestamp: now, Value: 3}},
		}},
	}}

	instancereg.Default.SetFastDFS("web-01", []model.FastDFSInstance{{
		Node: "web-01", Instance: "10.0.0.10:22122", Name: "dev-fastdfs",
		Role: "tracker", Group: "g1", Up: true,
	}})
	t.Cleanup(func() { instancereg.Default.SetFastDFS("web-01", nil) })

	rec := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, reqWith(globalPrincipal("middleware:read"), http.MethodGet, "/api/v1/middleware/fastdfs/instances", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d（body=%s）", rec.Code, rec.Body.String())
	}

	var resp struct {
		Type      string `json:"type"`
		Instances []struct {
			Node     string `json:"node"`
			Instance string `json:"instance"`
			Name     string `json:"name"`
			Role     string `json:"role"`
			Group    string `json:"group"`
			Up       bool   `json:"up"`
			Metrics  []struct {
				Key   string  `json:"key"`
				Label string  `json:"label"`
				Value float64 `json:"value"`
				Unit  string  `json:"unit"`
			} `json:"metrics"`
		} `json:"instances"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应解析失败：%v（body=%s）", err, rec.Body.String())
	}

	if resp.Type != "fastdfs" {
		t.Errorf("type = %q，want fastdfs", resp.Type)
	}
	if len(resp.Instances) != 1 {
		t.Fatalf("实例数 = %d，want 1（body=%s）", len(resp.Instances), rec.Body.String())
	}
	got := resp.Instances[0]
	if got.Role != "tracker" || got.Group != "g1" {
		t.Errorf("元信息 = role:%q group:%q，want tracker/g1", got.Role, got.Group)
	}
	// 名称以注册表为准：指标 labels 里没有 name，不能用 group 兜底后覆盖真名
	if got.Name != "dev-fastdfs" {
		t.Errorf("name = %q，want dev-fastdfs（注册表里的名称被 group 兜底覆盖了）", got.Name)
	}
	if !got.Up {
		t.Errorf("up = false，want true（fastdfs_up 值为 1）")
	}

	metrics := map[string]struct {
		value float64
		unit  string
	}{}
	for _, m := range got.Metrics {
		metrics[m.Key] = struct {
			value float64
			unit  string
		}{m.Value, m.Unit}
	}
	space, ok := metrics["fastdfs_total_space"]
	if !ok {
		t.Fatalf("缺少 fastdfs_total_space 指标（body=%s）", rec.Body.String())
	}
	if space.value != 1 {
		t.Errorf("fastdfs_total_space = %v，want 1（1048576 字节应换算成 1 MB）", space.value)
	}
	if space.unit != "MB" {
		t.Errorf("fastdfs_total_space unit = %q，want MB", space.unit)
	}
	if online, ok := metrics["fastdfs_storage_online_count"]; !ok || online.value != 3 {
		t.Errorf("fastdfs_storage_online_count = %+v（存在=%v），want 3", online, ok)
	}
}
