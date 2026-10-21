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

// 这三类与已声明化的中间件同构：注册表补元信息、指标走 metrics 数组。
// 各自锁定一个容易丢的字段：Postgres 的 database、Mongo 的 MB 换算、Kafka 的 broker 计数。

func decodeInstances(t *testing.T, rec *httptest.ResponseRecorder, path string) []struct {
	Node     string  `json:"node"`
	Name     string  `json:"name"`
	Role     string  `json:"role"`
	Topology string  `json:"topology"`
	Version  string  `json:"version"`
	Database string  `json:"database"`
	Uptime   float64 `json:"uptime"`
	Up       bool    `json:"up"`
	Metrics  []struct {
		Key   string  `json:"key"`
		Value float64 `json:"value"`
	} `json:"metrics"`
} {
	t.Helper()
	var resp struct {
		Instances []struct {
			Node     string  `json:"node"`
			Name     string  `json:"name"`
			Role     string  `json:"role"`
			Topology string  `json:"topology"`
			Version  string  `json:"version"`
			Database string  `json:"database"`
			Uptime   float64 `json:"uptime"`
			Up       bool    `json:"up"`
			Metrics  []struct {
				Key   string  `json:"key"`
				Value float64 `json:"value"`
			} `json:"metrics"`
		} `json:"instances"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应解析失败：%v（body=%s）", err, rec.Body.String())
	}
	return resp.Instances
}

func metricValue(t *testing.T, metrics []struct {
	Key   string  `json:"key"`
	Value float64 `json:"value"`
}, key string) (float64, bool) {
	t.Helper()
	for _, m := range metrics {
		if m.Key == key {
			return m.Value, true
		}
	}
	return 0, false
}

func TestPostgresInstances_DeclarativeShape(t *testing.T) {
	a := scopeTestAPI(t)
	a.alerts = &alertStoreStub{}
	now := time.Now().UnixMilli()
	labels := map[string]string{"node": "web-01", "instance": "10.0.0.10:5432"}
	a.store = &seriesStore{series: map[string][]model.Series{
		"postgres_instance_up": {{
			Labels: map[string]string{"node": "web-01", "instance": "10.0.0.10:5432", "role": "master", "topology": "replication"},
			Points: []model.Point{{Timestamp: now, Value: 1}},
		}},
		"postgres_numbackends": {{
			Labels: labels, Points: []model.Point{{Timestamp: now, Value: 7}},
		}},
		"postgres_uptime_seconds": {{
			Labels: labels, Points: []model.Point{{Timestamp: now, Value: 3600}},
		}},
	}}
	instancereg.Default.SetPostgres("web-01", []model.PostgresInstance{{
		Node: "web-01", Instance: "10.0.0.10:5432", Name: "pg-1",
		Role: "master", Topology: "replication", Version: "16.2", Database: "app",
	}})
	t.Cleanup(func() { instancereg.Default.SetPostgres("web-01", nil) })

	rec := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, reqWith(globalPrincipal("middleware:read"), http.MethodGet, "/api/v1/middleware/postgres/instances", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d（body=%s）", rec.Code, rec.Body.String())
	}
	instances := decodeInstances(t, rec, "postgres")
	if len(instances) != 1 {
		t.Fatalf("实例数 = %d，want 1", len(instances))
	}
	got := instances[0]
	if got.Name != "pg-1" || got.Version != "16.2" || got.Database != "app" {
		t.Errorf("元信息 = name:%q version:%q database:%q，want pg-1/16.2/app", got.Name, got.Version, got.Database)
	}
	if got.Topology != "replication" {
		t.Errorf("topology = %q，want replication", got.Topology)
	}
	if got.Uptime != 3600 {
		t.Errorf("uptime = %v，want 3600", got.Uptime)
	}
	if v, ok := metricValue(t, got.Metrics, "postgres_numbackends"); !ok || v != 7 {
		t.Errorf("postgres_numbackends = %v（存在=%v），want 7", v, ok)
	}
}

func TestMongoDBInstances_DeclarativeShape(t *testing.T) {
	a := scopeTestAPI(t)
	a.alerts = &alertStoreStub{}
	now := time.Now().UnixMilli()
	labels := map[string]string{"node": "web-01", "instance": "10.0.0.10:27017"}
	a.store = &seriesStore{series: map[string][]model.Series{
		"mongodb_up": {{
			Labels: map[string]string{"node": "web-01", "instance": "10.0.0.10:27017"},
			Points: []model.Point{{Timestamp: now, Value: 1}},
		}},
		"mongodb_mem_resident_bytes": {{
			Labels: labels, Points: []model.Point{{Timestamp: now, Value: 2 * 1024 * 1024}},
		}},
	}}
	instancereg.Default.SetMongoDB("web-01", []model.MongoDBInstance{{
		Node: "web-01", Instance: "10.0.0.10:27017", Name: "mongo-1",
		Role: "PRIMARY", Topology: "replicaset", Version: "7.0",
	}})
	t.Cleanup(func() { instancereg.Default.SetMongoDB("web-01", nil) })

	rec := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, reqWith(globalPrincipal("middleware:read"), http.MethodGet, "/api/v1/middleware/mongodb/instances", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d（body=%s）", rec.Code, rec.Body.String())
	}
	instances := decodeInstances(t, rec, "mongodb")
	if len(instances) != 1 {
		t.Fatalf("实例数 = %d，want 1", len(instances))
	}
	got := instances[0]
	if got.Role != "PRIMARY" || got.Topology != "replicaset" || got.Version != "7.0" {
		t.Errorf("元信息 = role:%q topology:%q version:%q", got.Role, got.Topology, got.Version)
	}
	// 字节按声明里的 Scale 换算成 MB
	if v, ok := metricValue(t, got.Metrics, "mongodb_mem_resident_bytes"); !ok || v != 2 {
		t.Errorf("mongodb_mem_resident_bytes = %v（存在=%v），want 2（MB）", v, ok)
	}
}

func TestKafkaInstances_DeclarativeShape(t *testing.T) {
	a := scopeTestAPI(t)
	a.alerts = &alertStoreStub{}
	now := time.Now().UnixMilli()
	labels := map[string]string{"node": "web-01", "instance": "10.0.0.10:9092"}
	a.store = &seriesStore{series: map[string][]model.Series{
		"kafka_instance_up": {{
			Labels: map[string]string{"node": "web-01", "instance": "10.0.0.10:9092", "role": "broker"},
			Points: []model.Point{{Timestamp: now, Value: 1}},
		}},
		"kafka_broker_count": {{
			Labels: labels, Points: []model.Point{{Timestamp: now, Value: 3}},
		}},
	}}
	instancereg.Default.SetKafka("web-01", []model.KafkaInstance{{
		Node: "web-01", Instance: "10.0.0.10:9092", Name: "kafka-1",
		Role: "broker", Version: "3.7", Group: "dev-kafka",
	}})
	t.Cleanup(func() { instancereg.Default.SetKafka("web-01", nil) })

	rec := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, reqWith(globalPrincipal("middleware:read"), http.MethodGet, "/api/v1/middleware/kafka/instances", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d（body=%s）", rec.Code, rec.Body.String())
	}
	instances := decodeInstances(t, rec, "kafka")
	if len(instances) != 1 {
		t.Fatalf("实例数 = %d，want 1", len(instances))
	}
	got := instances[0]
	if got.Name != "kafka-1" || got.Version != "3.7" {
		t.Errorf("元信息 = name:%q version:%q，want kafka-1/3.7", got.Name, got.Version)
	}
	if v, ok := metricValue(t, got.Metrics, "kafka_broker_count"); !ok || v != 3 {
		t.Errorf("kafka_broker_count = %v（存在=%v），want 3", v, ok)
	}
}
