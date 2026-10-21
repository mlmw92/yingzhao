package api

// K8s 对象级只读查询（「容器与工作负载 → Kubernetes」的数据通路）。
//
// 2026-10-10 重规划（ADR-0006）：这条通路从「Agent 异步下行」整体改为「Server 直连 apiserver」。
// 原先的形态是 5 个下行动作（container.workloads / pods / describe / events / logs）走
// ADR-0003 的统一下行通道，延迟一个上报周期；现在用 k8s-live.yaml 里的凭据同步查询——
// ADR-0004 早就写过理由：「异步下发的延迟等于一个上报周期，不满足管理面的要求」。
// 那 5 个下行动作已删除，本文件是它们唯一的替代。
//
// 查询与投影都在 k8sproxy 里（投影是**安全边界**，见其注释）；本文件只做参数校验与错误映射。
// 权限点仍是 container:read。写操作（重启工作负载 / 扩缩容 / 删除 Pod）**不走这里**：
// 它们要的是逐机护栏与 Agent 本地的集群凭据，仍在统一下行通道上。

import (
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/mlmw92/yingzhao/internal/model"
	"github.com/mlmw92/yingzhao/internal/server/k8sproxy"
)

// SetK8sLive 注入 K8s 直连查询管理器（未注入时接口返回 501，不静默空数据）。
func (a *API) SetK8sLive(m *k8sproxy.Manager) { a.k8sLive = m }

// k8sLiveCaps 是能力白名单：路径里的那一段只能是这几个值。
//
// 与"任意资源名透传"的写法相比，这张表把可查询的东西钉死了——新增一种查询必须改这里，
// 而不是在 URL 里换个资源名就能试出来。
var k8sLiveCaps = map[string]bool{
	"workloads": true,
	"pods":      true,
	"events":    true,
	"object":    true,
	"logs":      true,
}

// GET /api/v1/container/k8s/live-configs —— 已配置的集群名列表（集群选择器）。
// 数据源是 Server 侧凭据文件（k8s-live.yaml），不是 Agent 上报；
// 与 handleContainerClusters 的区别正在这里，两者名字可能重叠，前端以调用哪条通路为准。
func (a *API) handleK8sLiveConfigs(w http.ResponseWriter, r *http.Request) {
	if a.k8sLive == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"clusters": []string{}})
		return
	}
	names := make([]string, 0)
	for _, c := range a.k8sLive.List() {
		names = append(names, c.Name)
	}
	sort.Strings(names)
	writeJSON(w, http.StatusOK, map[string]interface{}{"clusters": names})
}

// GET /api/v1/container/k8s/live/{cluster}/{cap}?namespace=&name=&resource=&container=&tailLines=&sinceSeconds=
//
// 五条能力：
//
//	workloads  工作负载（Deployment / StatefulSet / DaemonSet / Job）与副本就绪情况
//	pods       Pod 状态、重启次数、所在节点
//	events     事件（可按对象名过滤）
//	object     单个对象的详情（**白名单投影**，见 k8sproxy.QueryObject）
//	logs       某个 Pod 的最近日志
//
// 参数一律先校验再拼路径：这些值会被拼进 apiserver 的 URL，不限制字符集就可能出现
// `namespaces/../..` 或把 `?` `#` 塞进去改变请求语义。校验用的是 model 里那批共用正则。
func (a *API) handleK8sLiveQuery(w http.ResponseWriter, r *http.Request) {
	if a.k8sLive == nil {
		http.Error(w, "K8s 直连查询未启用（缺少 k8s-live.yaml 配置文件）", http.StatusNotImplemented)
		return
	}
	cluster := strings.TrimSpace(r.PathValue("cluster"))
	capability := strings.TrimSpace(r.PathValue("cap"))
	if cluster == "" || !k8sLiveCaps[capability] {
		http.Error(w, "不支持的能力（白名单：workloads/pods/events/object/logs）", http.StatusBadRequest)
		return
	}

	q := r.URL.Query()
	namespace := strings.TrimSpace(q.Get("namespace"))
	name := strings.TrimSpace(q.Get("name"))

	// 命名空间可以留空（= 全部命名空间），但非空时必须是合法形态。
	if namespace != "" && !model.OpsNamespacePattern.MatchString(namespace) {
		http.Error(w, "命名空间不合法", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	var (
		res *model.ContainerQueryResult
		err error
	)
	switch capability {
	case "workloads":
		res, err = a.k8sLive.QueryWorkloads(ctx, cluster, namespace)
	case "pods":
		res, err = a.k8sLive.QueryPods(ctx, cluster, namespace)
	case "events":
		if name != "" && !model.OpsObjectNamePattern.MatchString(name) {
			http.Error(w, "对象名不合法", http.StatusBadRequest)
			return
		}
		res, err = a.k8sLive.QueryEvents(ctx, cluster, namespace, name)
	case "object":
		resource := strings.TrimSpace(q.Get("resource"))
		if !model.OpsContainerResourcePattern.MatchString(resource) {
			http.Error(w, "资源类型不合法（白名单：pods/deployments/statefulsets/daemonsets/jobs/services/configmaps）", http.StatusBadRequest)
			return
		}
		// 对象详情必须带命名空间：对象名只在命名空间内唯一，用"全部命名空间"去猜等于随机取一个。
		if namespace == "" || !model.OpsObjectNamePattern.MatchString(name) {
			http.Error(w, "对象详情必须指定合法的 namespace 与 name", http.StatusBadRequest)
			return
		}
		res, err = a.k8sLive.QueryObject(ctx, cluster, namespace, resource, name)
	case "logs":
		if namespace == "" || !model.OpsObjectNamePattern.MatchString(name) {
			http.Error(w, "拉取日志必须指定合法的 namespace 与 Pod 名", http.StatusBadRequest)
			return
		}
		container := strings.TrimSpace(q.Get("container"))
		if container != "" && !model.OpsNamespacePattern.MatchString(container) {
			http.Error(w, "容器名不合法", http.StatusBadRequest)
			return
		}
		tail, ok := k8sLiveIntParam(w, r, "tailLines", model.OpsLogDefaultTailLines, 1, model.OpsLogMaxTailLines)
		if !ok {
			return
		}
		since, ok := k8sLiveIntParam(w, r, "sinceSeconds", 0, 0, model.OpsLogMaxSinceSeconds)
		if !ok {
			return
		}
		res, err = a.k8sLive.QueryLogs(ctx, cluster, namespace, name, container, tail, since)
	}

	if err != nil {
		// apiserver 的错误语义（403=凭据权限不足、401=token 失效、404=对象不在）
		// 对排障有价值，原样带上并用 502 包一层：这不是"请求写错了"，而是"上游没给"。
		writeJSON(w, http.StatusBadGateway, map[string]interface{}{
			"error":   err.Error(),
			"cluster": cluster,
			"live":    false,
		})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// k8sLiveIntParam 读一个整数查询参数并做上下限校验。
// 越界**报错而不是静默夹取**：用户要 5000 行、平台悄悄给 500 行，
// 会让人以为"日志里没有那条错误"——这是最坏的一类误判。显式拒绝才能让上限被看见。
func k8sLiveIntParam(w http.ResponseWriter, r *http.Request, key string, def, min, max int) (int, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return def, true
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		http.Error(w, key+" 必须是整数", http.StatusBadRequest)
		return 0, false
	}
	if v < min || v > max {
		http.Error(w, key+" 超出允许范围（"+strconv.Itoa(min)+" ~ "+strconv.Itoa(max)+"）", http.StatusBadRequest)
		return 0, false
	}
	return v, true
}
