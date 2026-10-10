package collector

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nebula/monitor/internal/model"
)

// newRocketMQExporterServer 启动一个返回指定 exposition 文本的假 exporter。
func newRocketMQExporterServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func collectRocketMQExporter(t *testing.T, addr, exporterURL string) ([]model.Metric, []model.RocketMQInstance) {
	t.Helper()
	c := NewRocketMQCollector("node-a", []model.RocketMQInstanceConfig{{
		Name: "dev-rocketmq", Addr: addr, ExporterURL: exporterURL,
	}})
	return c.CollectCtx(context.Background())
}

// RocketMQ exporter 原生不暴露 rocketmq_instance_up，成功拉到 RocketMQ 指标后
// 必须补出该存活指标，否则平台实例列表会一直显示离线（本次修复的回归守卫）。
func TestRocketMQCollectorExporterSynthesizesInstanceUp(t *testing.T) {
	srv := newRocketMQExporterServer(t, "rocketmq_broker_tps{cluster=\"DefaultCluster\"} 2\n")
	metrics, instances := collectRocketMQExporter(t, "127.0.0.1:9876", srv.URL+"/metrics")

	if len(instances) != 1 || !instances[0].Up {
		t.Fatalf("实例应在线，实际 %+v", instances)
	}
	if instances[0].Name != "dev-rocketmq" || instances[0].Group != "dev-rocketmq" || instances[0].Role != "nameserver" {
		t.Fatalf("实例元信息不符：%+v", instances[0])
	}

	var up *model.Metric
	for i := range metrics {
		if metrics[i].Name == "rocketmq_instance_up" {
			up = &metrics[i]
		}
	}
	if up == nil {
		t.Fatalf("缺少 rocketmq_instance_up 指标：%+v", metrics)
	}
	if up.Value != 1 {
		t.Fatalf("存活指标应为 1，实际 %v", up.Value)
	}
	// instance 标签经归一化（回环地址替换为本机真实 IP），与实例元信息保持一致即可。
	for key, want := range map[string]string{
		"node": "node-a", "instance": instances[0].Instance, "name": "dev-rocketmq",
		"group": "dev-rocketmq", "role": "nameserver",
	} {
		if got := up.Labels[key]; got != want {
			t.Errorf("标签 %s = %q，期望 %q", key, got, want)
		}
	}
	if !strings.HasSuffix(up.Labels["instance"], ":9876") {
		t.Errorf("instance 标签应保留端口，实际 %q", up.Labels["instance"])
	}
}

// exporter 自带 rocketmq_instance_up 时不得重复追加。
func TestRocketMQCollectorExporterKeepsNativeInstanceUp(t *testing.T) {
	srv := newRocketMQExporterServer(t, "rocketmq_instance_up{version=\"5.3.1\"} 0\n")
	metrics, instances := collectRocketMQExporter(t, "127.0.0.1:9876", srv.URL)

	count := 0
	for _, m := range metrics {
		if m.Name == "rocketmq_instance_up" {
			count++
			if m.Value != 0 {
				t.Errorf("原生指标值应保留为 0，实际 %v", m.Value)
			}
		}
	}
	if count != 1 {
		t.Fatalf("rocketmq_instance_up 应恰好 1 条，实际 %d：%+v", count, metrics)
	}
	if len(instances) != 1 || instances[0].Up || instances[0].Version != "5.3.1" {
		t.Fatalf("显式 up=0 应判离线并保留版本：%+v", instances)
	}
}

// 拉取成功但没有 RocketMQ 指标（空响应、只有注释、只有无关指标）时不得伪造在线。
func TestRocketMQCollectorExporterEmptyExpositionStaysOffline(t *testing.T) {
	bodies := map[string]string{
		"空响应":  "",
		"仅注释":  "# HELP rocketmq_x example\n# TYPE rocketmq_x gauge\n",
		"无关指标": "process_cpu_seconds_total 0.5\n",
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			srv := newRocketMQExporterServer(t, body)
			metrics, instances := collectRocketMQExporter(t, "127.0.0.1:9876", srv.URL)

			if len(instances) != 1 || instances[0].Up {
				t.Fatalf("应判离线，实际 %+v", instances)
			}
			for _, m := range metrics {
				if m.Name == "rocketmq_instance_up" {
					t.Fatalf("不应产生存活指标：%+v", m)
				}
			}
		})
	}
}

// exporter 不可达或返回非 2xx 时判离线，且不产生任何指标。
func TestRocketMQCollectorExporterRequestFailureIsOffline(t *testing.T) {
	cases := map[string]string{
		"端口不可达": "http://127.0.0.1:1/metrics",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, "rocketmq_instance_up 1\n")
	}))
	t.Cleanup(srv.Close)
	cases["HTTP 500"] = srv.URL

	for name, url := range cases {
		t.Run(name, func(t *testing.T) {
			metrics, instances := collectRocketMQExporter(t, "127.0.0.1:9876", url)
			if len(instances) != 1 || instances[0].Up {
				t.Fatalf("应判离线，实际 %+v", instances)
			}
			if len(metrics) != 0 {
				t.Fatalf("不应产生指标，实际 %+v", metrics)
			}
		})
	}
}

// 只保留 rocketmq_ 前缀指标，并保留原始标签与取值。
func TestRocketMQCollectorExporterFiltersAndPreservesMetrics(t *testing.T) {
	body := "go_goroutines 5\nrocketmq_broker_tps{cluster=\"c1\"} 4.25\nrocketmq_producer_count 2\n"
	srv := newRocketMQExporterServer(t, body)
	metrics, _ := collectRocketMQExporter(t, "127.0.0.1:9876", srv.URL)

	byName := map[string]model.Metric{}
	for _, m := range metrics {
		byName[m.Name] = m
	}
	if _, ok := byName["go_goroutines"]; ok {
		t.Fatalf("非 rocketmq_ 前缀指标应被过滤：%+v", metrics)
	}
	tps, ok := byName["rocketmq_broker_tps"]
	if !ok || tps.Value != 4.25 || tps.Labels["cluster"] != "c1" {
		t.Fatalf("业务指标应保留原始取值与标签：%+v", tps)
	}
	if tps.Node != "node-a" || tps.Labels["node"] != "node-a" || !strings.HasSuffix(tps.Labels["instance"], ":9876") {
		t.Fatalf("业务指标应带节点与实例标签：%+v", tps.Labels)
	}
	if tps.Timestamp <= 0 {
		t.Fatalf("业务指标应有时间戳：%+v", tps)
	}
	if _, ok := byName["rocketmq_producer_count"]; !ok {
		t.Fatalf("应解析全部 rocketmq_ 指标：%+v", metrics)
	}
}

// 空配置或已取消的 ctx 不应发起采集。
func TestRocketMQCollectorExporterNoWork(t *testing.T) {
	if metrics, instances := NewRocketMQCollector("node-a", nil).CollectCtx(context.Background()); len(metrics) != 0 || len(instances) != 0 {
		t.Fatalf("空配置应无结果：metrics=%+v instances=%+v", metrics, instances)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := NewRocketMQCollector("node-a", []model.RocketMQInstanceConfig{{
		Name: "dev-rocketmq", Addr: "127.0.0.1:9876", ExporterURL: "http://127.0.0.1:1/metrics",
	}})
	if metrics, instances := c.CollectCtx(ctx); len(metrics) != 0 || len(instances) != 0 {
		t.Fatalf("ctx 已取消应跳过采集：metrics=%+v instances=%+v", metrics, instances)
	}
}

// apache/rocketmq-exporter 把版本放在 des（形如 V5_3_1）、把 broker 启动时刻放在
// boottime（毫秒时间戳）。此前只认 rocketmq_instance_up 的 version 标签，于是实例
// 列表的「版本」列恒为空、「运行时长」恒显示 -。这里锁定这两个字段的取值来源。
func TestRocketMQCollectorExporterVersionAndUptime(t *testing.T) {
	boot := time.Now().Add(-3 * time.Hour).UnixMilli()
	body := fmt.Sprintf(
		"rocketmq_brokeruntime_msg_put_total_today_now{cluster=\"DefaultCluster\",des=\"V5_3_1\",boottime=\"%d\"} 0\n",
		boot,
	)
	srv := newRocketMQExporterServer(t, body)
	metrics, instances := collectRocketMQExporter(t, "127.0.0.1:9876", srv.URL)

	if len(instances) != 1 {
		t.Fatalf("实例数 = %d，want 1", len(instances))
	}
	if got := instances[0].Version; got != "5.3.1" {
		t.Errorf("版本应取 des 并归一化为 5.3.1，got %q", got)
	}
	if got := instances[0].Uptime; got < 10790 || got > 10810 {
		t.Errorf("运行时长应约 10800 秒（boottime 取 3 小时前），got %v", got)
	}

	byName := map[string]model.Metric{}
	for _, m := range metrics {
		byName[m.Name] = m
	}
	up, ok := byName["rocketmq_uptime_seconds"]
	if !ok {
		t.Fatalf("应产出 rocketmq_uptime_seconds 指标：%+v", metrics)
	}
	if up.Labels["name"] != "dev-rocketmq" || !strings.HasSuffix(up.Labels["instance"], ":9876") {
		t.Errorf("uptime 指标标签应与其他 RocketMQ 指标一致：%+v", up.Labels)
	}
}

// exporter 暴露的是 topic/group 维度的细粒度指标，而平台查的是实例级指标名
// （rocketmq_topic_count / rocketmq_message_accumulation ...）。这里用 dev-server 上
// apache/rocketmq-exporter 的实测输出形态锁定归并结果。
func TestRocketMQCollectorExporterAdaptsMetrics(t *testing.T) {
	body := `rocketmq_producer_offset{cluster="DefaultCluster",broker="broker-a",topic="nebula-mw-demo",} 200.0
rocketmq_producer_offset{cluster="DefaultCluster",broker="broker-a",topic="TBW102",} 0.0
rocketmq_group_diff{group="nebula-mw-demo-group",topic="nebula-mw-demo",countOfOnlineConsumers="1",msgModel="1",} 200.0
rocketmq_group_diff{group="nebula-mw-demo-group",topic="other-topic",countOfOnlineConsumers="1",msgModel="1",} 30.0
rocketmq_consumer_tps{cluster="DefaultCluster",broker="broker-a",topic="nebula-mw-demo",group="nebula-mw-demo-group",} 12.5
rocketmq_brokeruntime_put_tps{cluster="DefaultCluster",brokerIP="127.0.0.1:10911",} 42.0
`
	srv := newRocketMQExporterServer(t, body)
	metrics, _ := collectRocketMQExporter(t, "127.0.0.1:9876", srv.URL)

	byName := map[string]model.Metric{}
	count := map[string]int{}
	for _, m := range metrics {
		byName[m.Name] = m
		count[m.Name]++
	}
	want := map[string]float64{
		"rocketmq_broker_count":         1,   // producer_offset 的 broker 去重
		"rocketmq_topic_count":          2,   // producer_offset 的 topic 去重
		"rocketmq_consumer_group_count": 1,   // group_diff 的 group 去重
		"rocketmq_message_accumulation": 230, // group_diff 累加
		"rocketmq_consumer_lag":         200, // group_diff 取最大
		"rocketmq_consumer_tps":         12.5,
		"rocketmq_producer_tps":         42,
	}
	for name, v := range want {
		m, ok := byName[name]
		if !ok {
			t.Errorf("缺少归并后的指标 %s", name)
			continue
		}
		if m.Value != v {
			t.Errorf("%s = %v，want %v", name, m.Value, v)
		}
		if m.Labels["node"] != "node-a" || m.Labels["name"] != "dev-rocketmq" {
			t.Errorf("%s 应带平台统一标签：%+v", name, m.Labels)
		}
	}
	// 细粒度同名序列必须剔除，否则同实例落在多条序列上、聚合时相互覆盖
	if count["rocketmq_consumer_tps"] != 1 {
		t.Errorf("rocketmq_consumer_tps 应只保留归并后的 1 条，实际 %d 条", count["rocketmq_consumer_tps"])
	}
}
