package collector

// K8s 侧的公共设施：定位集群配置、建连、把结果包成 ContainerQueryResult。
//
// 2026-10-10 重规划（ADR-0006）之后，K8s 的**只读查询**不再由 Agent 承担——
// 「容器与工作负载」页面改由 Server 侧的 k8sproxy 直连 apiserver 提供实时清单
// （ADR-0004 的原话：「异步下发的延迟等于一个上报周期，不满足管理面的要求」）。
// 因此原先的 k8s_query.go（5 个只读查询 + 一整套投影函数）已删除，
// 本文件只保留**写操作**（重启工作负载 / 扩缩容 / 删除 Pod）需要的部分。
//
// 注意：只读查询里的「白名单投影」取舍**没有丢**，只是换了位置——
// Docker 侧的投影现在在 internal/server/dockerproxy/engine.go，
// 而 K8s 侧改为由 apiserver 原样透传（资源白名单仍然是 pods/deployments/events/nodes，
// 且 `secrets` 从来不在其中）。

import (
	"errors"
	"fmt"
	"strings"

	"github.com/nebula/monitor/internal/model"
)

// ContainerQueryResult 定义在 internal/model——它**是协议载荷**（随 OpsResult.JSON 上行、
// 被前端解析），不只属于本包。这里用别名，让本文件的签名读起来短一些。
type ContainerQueryResult = model.ContainerQueryResult

// K8s 返回本机的 K8s 采集器，供下行动作通道执行容器写操作；**未配置任何集群时返回 nil**。
//
// 返回 nil 而不是一个空采集器是有意的：调用方用它决定"要不要声明容器写能力"，
// 而一台没配 k8sInstances 的机器声明了这项能力，只会让中心下发一条注定失败的任务。
func (c *Collector) K8s() *K8sCollector {
	if c.k8s == nil || len(c.k8s.instances) == 0 {
		return nil
	}
	return c.k8s
}

// queryConn 按集群标识找到配置并建连。
//
// 集群标识优先匹配 agent.yaml 里 k8sInstances[].name；为空时回落到 instance（apiserver 地址）：
// 有些部署不给集群起名，此时上报的 Name 是空的，而前端拿到的也正是空字符串。
func (c *K8sCollector) queryConn(cluster string) (model.K8sInstanceConfig, *k8sConn, error) {
	if len(c.instances) == 0 {
		return model.K8sInstanceConfig{}, nil, errors.New("本机未配置任何 Kubernetes 集群（agent.yaml 的 k8sInstances）")
	}
	cluster = strings.TrimSpace(cluster)
	var picked *model.K8sInstanceConfig
	for i := range c.instances {
		cfg := &c.instances[i]
		if cluster == "" || cfg.Name == cluster {
			picked = cfg
			break
		}
		if cfg.Name == "" && strings.TrimRight(cfg.APIServer, "/") == cluster {
			picked = cfg
			break
		}
	}
	if picked == nil {
		names := make([]string, 0, len(c.instances))
		for _, cfg := range c.instances {
			if cfg.Name != "" {
				names = append(names, cfg.Name)
			} else {
				names = append(names, cfg.APIServer)
			}
		}
		return model.K8sInstanceConfig{}, nil, fmt.Errorf("本机没有名为 %q 的集群（可选：%s）", cluster, strings.Join(names, " / "))
	}
	conn, err := buildK8sConn(*picked)
	if err != nil {
		return *picked, nil, err
	}
	return *picked, conn, nil
}

// newContainerResult 造一张空的结果表。
func newContainerResult(kind, cluster, namespace string, columns []string) *ContainerQueryResult {
	return &ContainerQueryResult{
		Kind: kind, Cluster: cluster, Namespace: namespace,
		Columns: columns, Rows: make([][]string, 0, 16),
		FetchedAt: model.NowMillis(),
	}
}

// addRow 追加一行。类型定义在 model，所以这里只能是自由函数而不是方法。
func addRow(r *ContainerQueryResult, row ...string) { r.Rows = append(r.Rows, row) }

func fallback(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

// truncateLine 按字符（不是字节）截断，避免把多字节汉字切成乱码。
func truncateLine(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
