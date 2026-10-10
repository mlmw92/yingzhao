package collector

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/nebula/monitor/internal/model"
)

// NacosCollector 采集 Nacos 实例存活状态，走控制台健康检查接口：
//   - GET {addr}{contextPath}/v1/console/health/readiness → 200 即就绪
//
// Nacos 的丰富指标需开启 2.x actuator prometheus（可后续经 exporter 模式扩展），
// 此处先保证「存活监控 + 告警」这一核心诉求。
type NacosCollector struct {
	node      string
	instances []model.NacosInstanceConfig
}

// NewNacosCollector 创建 NacosCollector。
func NewNacosCollector(node string, instances []model.NacosInstanceConfig) *NacosCollector {
	return &NacosCollector{node: node, instances: instances}
}

// Collect 采集所有 Nacos 实例指标（等价于 CollectCtx(context.Background())）。
func (c *NacosCollector) Collect() ([]model.Metric, []model.NacosInstance) {
	return c.CollectCtx(context.Background())
}

// CollectCtx 采集所有 Nacos 实例指标；ctx 取消或超时后停止采集剩余实例。
func (c *NacosCollector) CollectCtx(ctx context.Context) ([]model.Metric, []model.NacosInstance) {
	if len(c.instances) == 0 {
		return nil, nil
	}
	now := model.NowMillis()
	var metrics []model.Metric
	var instances []model.NacosInstance

	for _, cfg := range c.instances {
		if err := ctx.Err(); err != nil {
			slog.Warn("Nacos 采集被中断，跳过剩余实例", "err", err)
			break
		}
		m, ni := c.collectOne(ctx, cfg, now)
		metrics = append(metrics, m...)
		instances = append(instances, ni)
	}
	return metrics, instances
}

func (c *NacosCollector) collectOne(ctx context.Context, cfg model.NacosInstanceConfig, now int64) ([]model.Metric, model.NacosInstance) {
	cp := cfg.ContextPath
	if cp == "" {
		cp = "/nacos"
	}
	if !strings.HasPrefix(cp, "/") {
		cp = "/" + cp
	}
	cp = strings.TrimSuffix(cp, "/")

	ni := model.NacosInstance{
		Instance: urlHostPort(cfg.Addr),
		Name:     cfg.Name,
		Node:     c.node,
		Group:    cfg.Name,
		Up:       false,
	}
	up := 0.0
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://"+cfg.Addr+cp+"/v1/console/health/readiness", nil)
	if err == nil {
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				up = 1
			}
		} else {
			slog.Warn("Nacos 健康检查失败", "addr", cfg.Addr, "err", err)
		}
	} else {
		slog.Warn("Nacos 请求构造失败", "addr", cfg.Addr, "err", err)
	}
	ni.Up = up == 1

	metric := model.Metric{
		Node: c.node, Name: "nacos_instance_up", Value: up, Timestamp: now,
		Labels: map[string]string{
			"node":     c.node,
			"instance": ni.Instance,
			"name":     cfg.Name,
			"group":    cfg.Name,
			"role":     "server",
		},
	}
	metrics := []model.Metric{metric}
	// 存活之外再抓 Nacos 2.x 的指标端点（连接数/配置数/服务数/JVM 堆）。
	// 端点需在 Nacos 配置里开启（nacos.prometheus.metrics.enabled=true 与
	// management.endpoints.web.exposure.include=prometheus），未开启则静默跳过，
	// 不影响存活监控。
	if up == 1 {
		metrics = append(metrics, c.fetchNacosMetrics(ctx, cfg, cp, now, metric.Labels)...)
	}
	return metrics, ni
}

// fetchNacosMetrics 抓取 Nacos 2.x 的 /actuator/prometheus 端点，并把关键指标映射成
// 平台指标：客户端长连接、配置数、服务数、订阅者数与 JVM 堆/线程。
// heap 类指标按内存池多条样本求和得出；端点未开启或返回非 200 时静默跳过。
func (c *NacosCollector) fetchNacosMetrics(ctx context.Context, cfg model.NacosInstanceConfig, cp string, now int64, labels map[string]string) []model.Metric {
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+cfg.Addr+cp+"/actuator/prometheus", nil)
	if err != nil {
		return nil
	}
	resp, err := client.Do(req)
	if err != nil {
		slog.Warn("Nacos 指标抓取失败（如需连接数/配置数等详情指标，请在 Nacos 配置开启 prometheus 端点）", "addr", cfg.Addr, "err", err)
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil
	}

	mk := func(name string, val float64) model.Metric {
		return model.Metric{Node: c.node, Name: name, Labels: labels, Value: val, Timestamp: now}
	}
	monitor := map[string]float64{} // nacos_monitor{name="..."} → 值
	var heapUsed, heapMax float64
	var out []model.Metric
	sc := bufio.NewScanner(bytes.NewReader(body))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, " ")
		if idx <= 0 {
			continue
		}
		raw := line[:idx]
		val, _ := parseFloatOK(line[idx+1:])
		switch {
		case strings.HasPrefix(raw, "nacos_monitor{"):
			inner := raw[len("nacos_monitor{") : len(raw)-1]
			for _, kv := range strings.Split(inner, ",") {
				if strings.HasPrefix(kv, `name="`) {
					monitor[strings.Trim(strings.TrimPrefix(kv, `name="`), `"`)] = val
				}
			}
		case raw == "jvm_threads_live_threads":
			out = append(out, mk("nacos_jvm_threads_live", val))
		case strings.HasPrefix(raw, "jvm_memory_used_bytes{") && strings.Contains(raw, `area="heap"`):
			heapUsed += val
		case strings.HasPrefix(raw, "jvm_memory_max_bytes{") && strings.Contains(raw, `area="heap"`):
			heapMax += val
		}
	}
	if v, ok := monitor["longConnection"]; ok {
		out = append(out, mk("nacos_long_connection", v))
	}
	if v, ok := monitor["configCount"]; ok {
		out = append(out, mk("nacos_config_count", v))
	}
	if v, ok := monitor["serviceCount"]; ok {
		out = append(out, mk("nacos_naming_service_count", v))
	}
	if v, ok := monitor["subscriberCount"]; ok {
		out = append(out, mk("nacos_subscriber_count", v))
	}
	if heapUsed > 0 {
		out = append(out, mk("nacos_jvm_heap_used_bytes", heapUsed))
	}
	if heapMax > 0 {
		out = append(out, mk("nacos_jvm_heap_max_bytes", heapMax))
		if heapUsed > 0 {
			out = append(out, mk("nacos_jvm_heap_used_percent", round2(heapUsed/heapMax*100)))
		}
	}
	return out
}
