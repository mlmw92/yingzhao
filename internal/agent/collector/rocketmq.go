package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/nebula/monitor/internal/model"
)

// RocketMQCollector 采集 RocketMQ 实例指标，支持 HTTP API 直连与 exporter 双模式。
type RocketMQCollector struct {
	node      string
	instances []model.RocketMQInstanceConfig
}

// NewRocketMQCollector 创建 RocketMQCollector。
func NewRocketMQCollector(node string, instances []model.RocketMQInstanceConfig) *RocketMQCollector {
	return &RocketMQCollector{node: node, instances: instances}
}

// Collect 采集所有 RocketMQ 实例指标（等价于 CollectCtx(context.Background())）。
func (c *RocketMQCollector) Collect() ([]model.Metric, []model.RocketMQInstance) {
	return c.CollectCtx(context.Background())
}

// CollectCtx 采集所有 RocketMQ 实例指标；ctx 取消或超时后停止采集剩余实例。
func (c *RocketMQCollector) CollectCtx(ctx context.Context) ([]model.Metric, []model.RocketMQInstance) {
	if len(c.instances) == 0 {
		return nil, nil
	}
	now := model.NowMillis()
	var metrics []model.Metric
	var instances []model.RocketMQInstance

	for _, cfg := range c.instances {
		if err := ctx.Err(); err != nil {
			slog.Warn("RocketMQ 采集被中断，跳过剩余实例", "err", err)
			break
		}
		if cfg.ExporterURL != "" {
			m, ri := c.collectExporter(ctx, cfg, now)
			metrics = append(metrics, m...)
			instances = append(instances, ri)
			continue
		}
		m, ri := c.collectHTTP(ctx, cfg, now)
		metrics = append(metrics, m...)
		instances = append(instances, ri)
	}
	return metrics, instances
}

// collectHTTP 通过 RocketMQ HTTP API 采集。
func (c *RocketMQCollector) collectHTTP(ctx context.Context, cfg model.RocketMQInstanceConfig, now int64) ([]model.Metric, model.RocketMQInstance) {
	client := &http.Client{Timeout: 5 * time.Second}
	baseURL := "http://" + cfg.Addr

	labels := map[string]string{
		"node":     c.node,
		"instance": normalizeRemoteAddr(cfg.Addr, ""),
		"group":    cfg.Name,
		"name":     cfg.Name,
		"role":     "nameserver",
	}
	mk := func(name string, val float64) model.Metric {
		return model.Metric{Node: c.node, Name: name, Labels: labels, Value: val, Timestamp: now}
	}

	var out []model.Metric
	up := 1.0

	// 1. 集群信息
	clusterInfo, err := c.getRocketMQJSON(ctx, client, baseURL+"/rocketmq/httpapi/cluster/list.query")
	if err != nil {
		hint := "请确认 NameServer 已开启 HTTP API（RocketMQ 5.x 需启动参数 -Drocketmq.httpapi.enabled=true 或环境变量 ROCKETMQ_HTTPAPI_ENABLED=true；RocketMQ 4.x 无此 HTTP API，请改用 exporterURL 走 rocketmq-exporter 模式）"
		if strings.Contains(err.Error(), "EOF") {
			slog.Warn("RocketMQ 集群信息获取失败(连接被关闭, HTTP API 可能未开启)", "addr", cfg.Addr, "err", err, "hint", hint)
		} else {
			slog.Warn("RocketMQ 集群信息获取失败", "addr", cfg.Addr, "err", err, "hint", hint)
		}
		up = 0
	}
	out = append(out, mk("rocketmq_instance_up", up))

	if up == 0 {
		return out, model.RocketMQInstance{
			Instance: normalizeRemoteAddr(cfg.Addr, ""), Name: cfg.Name, Node: c.node,
			Group: cfg.Name, Role: "nameserver", Up: false,
		}
	}

	// 解析集群信息
	brokerCount := 0.0
	if data, ok := clusterInfo["data"].(map[string]interface{}); ok {
		if brokers, ok := data["brokerServer"].(map[string]interface{}); ok {
			brokerCount = float64(len(brokers))
		}
	}
	out = append(out, mk("rocketmq_broker_count", brokerCount))

	// 2. Topic 列表
	topicList, _ := c.getRocketMQJSON(ctx, client, baseURL+"/rocketmq/httpapi/topic/list.query")
	topicCount := 0.0
	if data, ok := topicList["data"].(map[string]interface{}); ok {
		if topics, ok := data["topicList"].([]interface{}); ok {
			topicCount = float64(len(topics))
		}
	}
	out = append(out, mk("rocketmq_topic_count", topicCount))

	// 3. Consumer Group 列表
	groupList, _ := c.getRocketMQJSON(ctx, client, baseURL+"/rocketmq/httpapi/consumerGroup/list.query")
	groupCount := 0.0
	if data, ok := groupList["data"].(map[string]interface{}); ok {
		if groups, ok := data["groupList"].([]interface{}); ok {
			groupCount = float64(len(groups))
		}
	}
	out = append(out, mk("rocketmq_consumer_group_count", groupCount))

	// 4. Broker TPS/QPS（从集群 stats 接口获取）
	stats, _ := c.getRocketMQJSON(ctx, client, baseURL+"/rocketmq/httpapi/cluster/stats.query")
	if data, ok := stats["data"].(map[string]interface{}); ok {
		out = append(out, mk("rocketmq_broker_tps", parseFloat(fmt.Sprintf("%v", data["brokerTps"]))))
		out = append(out, mk("rocketmq_producer_tps", parseFloat(fmt.Sprintf("%v", data["producerTps"]))))
		out = append(out, mk("rocketmq_consumer_tps", parseFloat(fmt.Sprintf("%v", data["consumerTps"]))))
	}

	// 5. 消息积压（从 Consumer Group stats 聚合）
	totalAccumulation := 0.0
	maxLag := 0.0
	if data, ok := groupList["data"].(map[string]interface{}); ok {
		if groups, ok := data["groupList"].([]interface{}); ok {
			for _, g := range groups {
				groupName, _ := g.(string)
				if groupName == "" {
					continue
				}
				groupStats, _ := c.getRocketMQJSON(ctx, client, baseURL+"/rocketmq/httpapi/consumer/stats.query?group="+groupName)
				if sd, ok := groupStats["data"].(map[string]interface{}); ok {
					diff := parseFloat(fmt.Sprintf("%v", sd["consumeDiff"]))
					totalAccumulation += diff
					if diff > maxLag {
						maxLag = diff
					}
				}
			}
		}
	}
	out = append(out, mk("rocketmq_message_accumulation", totalAccumulation))
	out = append(out, mk("rocketmq_consumer_lag", maxLag))

	version := ""
	if v, ok := clusterInfo["data"].(map[string]interface{}); ok {
		if vv, ok := v["rocketMQVersion"].(string); ok {
			version = vv
		}
	}

	ri := model.RocketMQInstance{
		Instance: normalizeRemoteAddr(cfg.Addr, ""),
		Name:     cfg.Name,
		Node:     c.node,
		Group:    cfg.Name,
		Role:     "nameserver",
		Version:  version,
		Up:       true,
	}
	return out, ri
}

func (c *RocketMQCollector) collectExporter(ctx context.Context, cfg model.RocketMQInstanceConfig, now int64) ([]model.Metric, model.RocketMQInstance) {
	client := &http.Client{Timeout: 5 * time.Second}
	body, err := fetchMetrics(ctx, client, cfg.ExporterURL)
	if err != nil {
		slog.Warn("RocketMQ exporter 拉取失败", "target", safeExporterTarget(cfg.ExporterURL), "err", safeExporterError(err))
		return nil, model.RocketMQInstance{
			Instance: normalizeRemoteAddr(cfg.Addr, ""), Name: cfg.Name, Node: c.node,
			Group: cfg.Name, Role: "nameserver", Up: false,
		}
	}
	instance := normalizeRemoteAddr(cfg.Addr, "")
	metrics := parsePrometheusTextWithPrefix(string(body), c.node, instance, "rocketmq_", now)
	// 平台按实例聚合展示、查的是内置模式的指标名，而 exporter 只暴露 topic/group 维度的
	// 细粒度指标 —— 先做一层归并，否则 Broker 数/Topic 数/消费组/消息积压恒为 0。
	metrics = adaptExporterMetrics(metrics, c.node, instance, cfg.Name, now)
	// RocketMQ exporter 原生通常不暴露 rocketmq_instance_up；ExporterURL 可成功拉取
	// 且至少解析到一条 RocketMQ 指标时，补平台统一使用的实例存活指标，否则实例列表
	// 会因缺少 *_instance_up 而显示离线。
	hasUp := false
	for _, m := range metrics {
		if m.Name == "rocketmq_instance_up" {
			hasUp = true
			break
		}
	}
	if len(metrics) > 0 && !hasUp {
		metrics = append(metrics, model.Metric{
			Node: c.node, Name: "rocketmq_instance_up", Value: 1, Timestamp: now,
			Labels: map[string]string{
				"node": c.node, "instance": instance, "name": cfg.Name,
				"group": cfg.Name, "role": "nameserver",
			},
		})
	}
	up := exporterHealth(string(body), len(metrics) > 0, "rocketmq_up", "rocketmq_instance_up")
	ri := model.RocketMQInstance{
		Instance: instance, Name: cfg.Name, Node: c.node,
		Group: cfg.Name, Role: "nameserver", Up: up,
	}
	// apache/rocketmq-exporter 的 broker 运行时指标带这两个标签：
	//   des="V5_3_1"（版本；另有 broker_version="475" 是内部号，不直观）
	//   boottime="1790732323712"（broker 启动时刻，毫秒时间戳）
	// 实例存活指标（可能是自补的那条）不带版本，因此版本与运行时长都从这里取。
	for _, m := range metrics {
		if m.Labels == nil {
			continue
		}
		if ri.Version == "" {
			if v, ok := m.Labels["version"]; ok && v != "" {
				ri.Version = v
			} else if v := normalizeBrokerVersion(m.Labels["des"]); v != "" {
				ri.Version = v
			}
		}
		if ri.Uptime == 0 {
			if bt, ok := parseFloatOK(m.Labels["boottime"]); ok && bt > 0 {
				if sec := (float64(now) - bt) / 1000; sec > 0 {
					ri.Uptime = math.Round(sec)
				}
			}
		}
	}
	if ri.Uptime > 0 {
		metrics = append(metrics, model.Metric{
			Node: c.node, Name: "rocketmq_uptime_seconds", Value: ri.Uptime, Timestamp: now,
			Labels: map[string]string{
				"node": c.node, "instance": instance, "name": cfg.Name,
				"group": cfg.Name, "role": "nameserver",
			},
		})
	}
	return metrics, ri
}

// normalizeBrokerVersion 把 exporter 的 des 标签归一成可读版本号：
// "V5_3_1" → "5.3.1"。非该形态的值原样返回（为空则返回空串）。
func normalizeBrokerVersion(des string) string {
	s := strings.TrimSpace(des)
	if s == "" {
		return ""
	}
	s = strings.TrimPrefix(strings.TrimPrefix(s, "V"), "v")
	return strings.ReplaceAll(s, "_", ".")
}

// adaptExporterMetrics 把 rocketmq-exporter 的 topic/group 维度指标归并成平台实例级指标。
//
// exporter 暴露的是细粒度序列：rocketmq_producer_offset 每个 topic 一条、
// rocketmq_group_diff 每个 group×topic 一条（实测：没有消费组订阅关系时连这些都不出现）；
// 而平台按实例聚合展示，查的又是内置模式的指标名（rocketmq_topic_count /
// rocketmq_message_accumulation ...）。不做这层归并，页面上的 Broker 数、Topic 数、
// 消费组、消息积压会恒为 0——造再多数据也显示不出来。
func adaptExporterMetrics(raw []model.Metric, node, instance, name string, now int64) []model.Metric {
	// 一条都没解析到时不要凭空产出指标：后续的存活判定依据是「是否解析到指标」，
	// 否则「拉取成功但内容为空/无关」会被误判成在线。
	if len(raw) == 0 {
		return raw
	}
	labels := map[string]string{
		"node": node, "instance": instance, "name": name,
		"group": name, "role": "nameserver",
	}
	topics := map[string]struct{}{}
	brokers := map[string]struct{}{}
	groups := map[string]struct{}{}
	var accumulation, maxLag, consumerTps, putTps float64

	out := make([]model.Metric, 0, len(raw)+8)
	for _, m := range raw {
		switch m.Name {
		case "rocketmq_producer_offset":
			if t := m.Labels["topic"]; t != "" {
				topics[t] = struct{}{}
			}
			if b := m.Labels["broker"]; b != "" {
				brokers[b] = struct{}{}
			}
		case "rocketmq_group_diff":
			accumulation += m.Value
			if m.Value > maxLag {
				maxLag = m.Value
			}
			if g := m.Labels["group"]; g != "" {
				groups[g] = struct{}{}
			}
		case "rocketmq_consumer_tps":
			consumerTps += m.Value
			// 归并后的实例级同名指标必须唯一：细粒度序列若一并保留，同实例会落在
			// 多条序列上，聚合时相互覆盖、取值不确定。
			continue
		case "rocketmq_brokeruntime_put_tps":
			putTps = m.Value
		}
		out = append(out, m)
	}

	mk := func(metric string, val float64) model.Metric {
		return model.Metric{Node: node, Name: metric, Labels: labels, Value: val, Timestamp: now}
	}
	out = append(out,
		mk("rocketmq_broker_count", float64(len(brokers))),
		mk("rocketmq_topic_count", float64(len(topics))),
		mk("rocketmq_consumer_group_count", float64(len(groups))),
		mk("rocketmq_message_accumulation", accumulation),
		mk("rocketmq_consumer_lag", maxLag),
		mk("rocketmq_consumer_tps", consumerTps),
	)
	if putTps != 0 {
		out = append(out, mk("rocketmq_producer_tps", putTps))
	}
	return out
}

// getRocketMQJSON 发送 GET 请求并解析 JSON。
func (c *RocketMQCollector) getRocketMQJSON(ctx context.Context, client *http.Client, url string) (map[string]interface{}, error) {
	body, err := fetchMetrics(ctx, client, url)
	if err != nil {
		return nil, err
	}
	var out map[string]interface{}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// formatRocketMQAddr 格式化 RocketMQ 地址。
func formatRocketMQAddr(addr string) string {
	return strings.TrimPrefix(strings.TrimPrefix(addr, "http://"), "https://")
}
