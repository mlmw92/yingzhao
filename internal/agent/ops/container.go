package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/mlmw92/yingzhao/internal/model"
)

// 容器 **写** 动作（K8s 排障三件：重启工作负载 / 扩缩容 / 删除 Pod）。
//
// 链路：Server 提交任务 → 随 Agent 上报响应下发 → 这里执行 → 回执随下一轮上报送回。
// 因此延迟至少一个上报周期。**只读查询不走这条链路**：2026-10-10 重规划（ADR-0006）后，
// K8s 对象级查询由 Server 侧的 k8sproxy 直连 apiserver 提供实时清单——
// ADR-0004 早就写过理由：「异步下发的延迟等于一个上报周期，不满足管理面的要求」。
//
// 写操作之所以仍留在 Agent 侧：它要的是**逐机护栏**（这台机器同不同意被改）与
// **凭据边界**（集群 kubeconfig 只存 Agent 本地、从不上报）。

// maxContainerJSONBytes 是结构化结果的大小上限。
//
// 回执要搭在下一轮上报的请求体里，撑爆它的表现是"这台机器突然不上报了"，
// 比"表格少几行"严重得多——所以宁可整条失败并说清原因。
const maxContainerJSONBytes = 256 << 10

// K8sQuerier 是容器**写操作**的本机实现，由 internal/agent/collector 提供。
//
// 定义成接口而不是直接依赖 collector：ops 包不需要知道 kubeconfig 怎么解析、apiserver 怎么连——
// 与凭据相关的东西只该待在一个地方。测试里也就能用一个假实现跑通护栏与分派。
type K8sQuerier interface {
	// 写操作：只有 guards.ops.containerWrite 放行时才会被调用。
	QueryRestartWorkload(ctx context.Context, cluster, namespace, kind, name string) (*model.ContainerQueryResult, error)
	QueryScaleWorkload(ctx context.Context, cluster, namespace, kind, name string, replicas int) (*model.ContainerQueryResult, error)
	QueryDeletePod(ctx context.Context, cluster, namespace, name string) (*model.ContainerQueryResult, error)
}

// containerWriteKinds 是容器类**写**动作清单。
//
// 能力协商按它决定"要不要声明容器写能力"，而写能力只有 guards.ops.containerWrite
// 显式开启时才声明（与只读总开关无关：那是两次不同的同意）。
var containerWriteKinds = []string{
	model.OpsKindContainerRestart,
	model.OpsKindContainerScale,
	model.OpsKindContainerDelete,
}

// containerWriteKind 判断某个动作是不是容器类写动作（护栏分叉的依据）。
func containerWriteKind(kind string) bool {
	for _, k := range containerWriteKinds {
		if k == kind {
			return true
		}
	}
	return false
}

// SetK8sQuerier 注入容器写操作的实现。
//
// 未注入时容器写动作**既不声明也不执行**：让能力协商如实显示"该节点不支持"，
// 好过接受任务后在执行期才失败——后者在界面上表现为一条永远不成功的任务。
func (e *Executor) SetK8sQuerier(q K8sQuerier) { e.k8s = q }

// runContainer 是容器写动作的统一入口：护栏 → 参数复校 → 执行 → 结果封装。
//
// Agent 侧**再校验一次参数**不是重复劳动：Server 校验保证"下发的东西是干净的"，
// 这里保证"即使中心版本不对或被绕过，本机也不会拿着拼出来的路径去改 apiserver 上的对象"。
func (e *Executor) runContainer(cmd model.OpsCommand) model.OpsResult {
	fail := func(msg string) model.OpsResult {
		return model.OpsResult{State: model.OpsStateFailed, Message: msg}
	}

	if e.k8s == nil {
		return fail("该 Agent 未启用容器写操作（未配置 k8sInstances 或凭据未加载）")
	}
	if !containerWriteKind(cmd.Kind) {
		// 兜底：只读动作不该走到这里（它们已改由 Server 直连，见文件头的说明）。
		// 明确回绝而不是"当成写动作执行"，是因为两者要的同意完全不同。
		return fail("本机不支持该动作：" + cmd.Kind)
	}
	if !e.guards.ContainerWrite {
		return fail("本机护栏未放行容器写操作（需在 agent.yaml 的 guards.ops 中设置 containerWrite: true）")
	}

	cluster := strings.TrimSpace(cmd.Params["cluster"])
	if !model.OpsClusterPattern.MatchString(cluster) {
		return fail("集群标识不合法：" + cluster)
	}
	namespace := strings.TrimSpace(cmd.Params["namespace"])
	if namespace != "" && !model.OpsNamespacePattern.MatchString(namespace) {
		return fail("命名空间不合法：" + namespace)
	}
	// 写动作必须指定命名空间：工作负载与 Pod 名只在命名空间内唯一，
	// 用"全部命名空间"去猜等于随机改一个同名的对象。
	if namespace == "" {
		return fail("容器写操作必须指定命名空间")
	}

	ctx, cancel := context.WithTimeout(context.Background(), e.timeout)
	defer cancel()

	var (
		res *model.ContainerQueryResult
		err error
	)
	switch cmd.Kind {
	case model.OpsKindContainerRestart:
		kind := strings.TrimSpace(cmd.Params["kind"])
		if !model.OpsWorkloadKindPattern.MatchString(kind) {
			return fail("工作负载类型不合法：" + kind)
		}
		name := strings.TrimSpace(cmd.Params["name"])
		if !model.OpsObjectNamePattern.MatchString(name) {
			return fail("名称不合法：" + name)
		}
		res, err = e.k8s.QueryRestartWorkload(ctx, cluster, namespace, kind, name)
	case model.OpsKindContainerScale:
		kind := strings.TrimSpace(cmd.Params["kind"])
		if !model.OpsScalableKindPattern.MatchString(kind) {
			return fail("不支持扩缩容的类型：" + kind)
		}
		name := strings.TrimSpace(cmd.Params["name"])
		if !model.OpsObjectNamePattern.MatchString(name) {
			return fail("名称不合法：" + name)
		}
		raw := strings.TrimSpace(cmd.Params["replicas"])
		replicas, ok := boundedCount(raw, 0, model.OpsMaxReplicas)
		// 显式写 0 与"留空"语义不同：副本数必填，留空一律拒绝（boundedCount 的 def 在这里是 0，分不出来）。
		if !ok || raw == "" {
			return fail(fmt.Sprintf("副本数不合法（应为 0-%d 的整数）：%s", model.OpsMaxReplicas, raw))
		}
		res, err = e.k8s.QueryScaleWorkload(ctx, cluster, namespace, kind, name, replicas)
	case model.OpsKindContainerDelete:
		name := strings.TrimSpace(cmd.Params["name"])
		if !model.OpsObjectNamePattern.MatchString(name) {
			return fail("Pod 名不合法：" + name)
		}
		res, err = e.k8s.QueryDeletePod(ctx, cluster, namespace, name)
	default:
		return fail("本机不支持该动作：" + cmd.Kind)
	}
	if err != nil {
		return fail("执行失败：" + err.Error())
	}

	payload, mErr := json.Marshal(res)
	if mErr != nil {
		return fail("结果序列化失败：" + mErr.Error())
	}
	if len(payload) > maxContainerJSONBytes {
		return fail(fmt.Sprintf("结果过大（%d 字节，上限 %d）", len(payload), maxContainerJSONBytes))
	}

	summary := containerSummary(res)
	return model.OpsResult{
		State:   model.OpsStateSucceeded,
		Message: summary,
		// Data 只放一行摘要：结构化内容在 JSON 里，刻意不把同一份数据存两遍。
		Data: map[string]string{"概览": summary},
		JSON: string(payload),
	}
}

// boundedCount 解析可选的正整数参数：留空取默认值，非数字或超界一律拒绝。
//
// 为什么"超界"也拒绝而不是静默夹到上限：静默夹值会让用户以为"我设了 5000"，
// 而实际只有 500——他可能据此以为改动生效了，这是最坏的一类误判。
func boundedCount(raw string, def, max int) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 || n > max {
		return 0, false
	}
	if n == 0 && def > 0 {
		return 0, false // 显式写 0 与"留空"语义不同，行数不能为 0
	}
	return n, true
}

// containerSummary 生成一句给人看的结论（任务列表里直接显示 Message）。
func containerSummary(res *model.ContainerQueryResult) string {
	if res == nil {
		return "执行完成，无结果"
	}
	where := res.Cluster
	if res.Namespace != "" {
		where += " / " + res.Namespace
	}
	s := fmt.Sprintf("%s：返回 %d 条（共 %d 个对象）", where, len(res.Rows), res.Total)
	if res.Truncated {
		// 截断必须说出来：否则使用者会以为"集群里就这么多"。
		s += "，已截断，仅显示前若干条"
	}
	if res.Notice != "" {
		s += "；" + res.Notice
	}
	return s
}
