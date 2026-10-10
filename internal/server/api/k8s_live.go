package api

// K8s 实时清单（ADR-0004）：Server 侧凭据的 apiserver 只读代理。
//
// 与 container_api.go 的异步下行走同一个权限门（container:read），但语义是
// 「点开即得」——资源清单的实时态由这条通路提供；指标（时序）仍走 Agent
// 采集入库，两者在页面上分区展示、互不混写。

import (
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/nebula/monitor/internal/server/k8sproxy"
)

// SetK8sLive 注入 K8s 实时代理管理器（未注入时接口返回 501，不静默空数据）。
func (a *API) SetK8sLive(m *k8sproxy.Manager) { a.k8sLive = m }

// k8sLiveResources 是只读白名单：资源名 → apiserver 路径。
// 刻意不做通用代理（路径只能出自这张表），写动词天然被挡在门外。
var k8sLiveResources = map[string]string{
	"pods":        "/api/v1/pods",
	"deployments": "/apis/apps/v1/deployments",
	"events":      "/api/v1/events",
	"nodes":       "/api/v1/nodes",
}

// k8sLivePath 组出最终查询路径；带命名空间时在最后一段前插入 namespaces/{ns}。
func k8sLivePath(resource, namespace string) string {
	base, ok := k8sLiveResources[resource]
	if !ok {
		return ""
	}
	if namespace == "" || resource == "nodes" {
		return base
	}
	i := strings.LastIndex(base, "/")
	return base[:i] + "/namespaces/" + url.PathEscape(namespace) + base[i:]
}

// GET /api/v1/container/k8s/live-configs —— 已配置的集群名列表（实时视图的集群选择器）。
// 与 handleContainerClusters 的区别：数据源是 Server 侧凭据文件，不是 Agent 上报；
// 两者名字可能重叠（同一个集群两边都配了），前端以调用哪条通路为准。
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

// GET /api/v1/container/k8s/live/{cluster}/{resource}?namespace=...
//
// 只读白名单代理：响应体原样透传 apiserver 的 JSON。apiserver 的错误语义
// （403=SA 权限不足、401=token 失效）对排障有价值，用 502 包一层并带上原文。
func (a *API) handleK8sLiveQuery(w http.ResponseWriter, r *http.Request) {
	if a.k8sLive == nil {
		http.Error(w, "K8s 实时代理未启用（缺少 k8sLive 配置文件）", http.StatusNotImplemented)
		return
	}
	cluster := r.PathValue("cluster")
	resource := r.PathValue("resource")
	path := k8sLivePath(resource, r.URL.Query().Get("namespace"))
	if path == "" {
		http.Error(w, "不支持的资源类型（只读白名单：pods/deployments/events/nodes）", http.StatusBadRequest)
		return
	}
	body, err := a.k8sLive.GetJSON(r.Context(), cluster, path)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]interface{}{
			"error":   err.Error(),
			"cluster": cluster,
			"live":    false,
		})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-K8s-Live", "true")
	_, _ = w.Write(body)
}
