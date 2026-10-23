package api

// K8s 排障写操作（重启工作负载 / 扩缩容 / 删除 Pod）。
//
// 2026-10-23 决定：**Agent 只负责采集**，这三个动作从「Agent 异步下行」改为
// 「Server 直连 apiserver」——与 Docker 管理面同一形态（ADR-0006），点一次就是一次
// 真实请求，失败当场可见。写权限由 k8s-live.yaml 里的凭据决定。
//
// 平台侧的三道闸不变：
//   - 权限点 `container:write`（仅超管）；
//   - 界面二次确认（前端）；
//   - 审计（每次写操作一条）。
// 开启高风险审批时，它们仍是写类动作，会先进入双人复核（见 approvalGate）。

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mlmw92/yingzhao/internal/model"
	"github.com/mlmw92/yingzhao/internal/server/approval"
	"github.com/mlmw92/yingzhao/internal/server/audit"
)

// k8sWriteParams 是一次 K8s 写操作的参数（已通过白名单校验）。
type k8sWriteParams struct {
	Cluster   string
	Namespace string
	Kind      string
	Name      string
	Replicas  int
	Reason    string
}

// parseK8sWriteParams 校验并归一化参数。
//
// 与 Agent 时期同一套白名单（model 里的 Ops*Pattern）：这些值会被拼进 apiserver 的路径，
// 放斜杠进去就等于开了一条改变请求路径的口子。
func (a *API) parseK8sWriteParams(w http.ResponseWriter, r *http.Request, needKind, needReplicas bool) (k8sWriteParams, bool) {
	q := r.URL.Query()
	p := k8sWriteParams{
		Cluster:   strings.TrimSpace(r.PathValue("cluster")),
		Namespace: strings.TrimSpace(q.Get("namespace")),
		Kind:      strings.TrimSpace(q.Get("kind")),
		Name:      strings.TrimSpace(q.Get("name")),
		Reason:    strings.TrimSpace(q.Get("reason")),
	}
	bad := func(msg string) (k8sWriteParams, bool) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
		return k8sWriteParams{}, false
	}
	if !model.OpsClusterPattern.MatchString(p.Cluster) {
		return bad("集群标识不合法")
	}
	// 写动作必须指定命名空间：工作负载与 Pod 名只在命名空间内唯一。
	if !model.OpsNamespacePattern.MatchString(p.Namespace) {
		return bad("命名空间不合法（写操作必须指定）")
	}
	if needKind && !model.OpsWorkloadKindPattern.MatchString(p.Kind) {
		return bad("工作负载类型不合法（Deployment / StatefulSet / DaemonSet）")
	}
	if !model.OpsObjectNamePattern.MatchString(p.Name) {
		return bad("名称不合法")
	}
	if needReplicas {
		n, err := strconv.Atoi(strings.TrimSpace(q.Get("replicas")))
		if err != nil || n < 0 || n > model.OpsMaxReplicas {
			return bad("副本数必须是 0 到 " + strconv.Itoa(model.OpsMaxReplicas) + " 之间的整数")
		}
		p.Replicas = n
	}
	return p, true
}

// k8sWrite 是三个写接口的共同实现：审批 → 执行 → 审计 → 响应。
func (a *API) k8sWrite(w http.ResponseWriter, r *http.Request, kind, title string, p k8sWriteParams,
	exec func() (*model.ContainerQueryResult, error)) {
	if a.k8sLive == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{
			"error": "K8s 直连未启用（缺少 k8s-live.yaml 配置）",
		})
		return
	}
	// 审批：写类动作在开启审批时先登记审批单，不执行。
	params := map[string]string{
		"cluster": p.Cluster, "namespace": p.Namespace, "name": p.Name,
	}
	if p.Kind != "" {
		params["kind"] = p.Kind
	}
	if kind == model.OpsKindContainerScale {
		params["replicas"] = strconv.Itoa(p.Replicas)
	}
	if a.approvalGate(w, r, approval.EntryOpsWrite, kind, false, &approval.Request{
		Params: params, Title: title, Reason: p.Reason,
	}) {
		return
	}

	res, err := exec()
	a.recordK8sWrite(r, kind, title, err)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]interface{}{
			"error": err.Error(), "cluster": p.Cluster, "live": false,
		})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// recordK8sWrite 为一次 K8s 写操作留痕。
func (a *API) recordK8sWrite(r *http.Request, action, detail string, err error) {
	if a.audit == nil {
		return
	}
	status := http.StatusOK
	if err != nil {
		status = http.StatusBadGateway
	}
	_ = a.audit.Record(audit.Event{
		User:      AuthenticatedUser(r),
		Method:    r.Method,
		Path:      r.URL.Path,
		Status:    status,
		RemoteIP:  audit.ClientIP(r),
		Succeeded: err == nil,
		RequestID: RequestID(r),
		Category:  "container",
		Action:    action,
		Detail:    detail,
	})
}

// POST /api/v1/container/k8s/live/{cluster}/restart?namespace=&kind=&name=&reason=
func (a *API) handleK8sRestartWorkload(w http.ResponseWriter, r *http.Request) {
	p, ok := a.parseK8sWriteParams(w, r, true, false)
	if !ok {
		return
	}
	title := "重启工作负载 " + p.Kind + "/" + p.Name + " @" + p.Cluster
	a.k8sWrite(w, r, model.OpsKindContainerRestart, title, p, func() (*model.ContainerQueryResult, error) {
		return a.k8sLive.RestartWorkload(r.Context(), p.Cluster, p.Namespace, p.Kind, p.Name)
	})
}

// POST /api/v1/container/k8s/live/{cluster}/scale?namespace=&kind=&name=&replicas=&reason=
func (a *API) handleK8sScaleWorkload(w http.ResponseWriter, r *http.Request) {
	p, ok := a.parseK8sWriteParams(w, r, true, true)
	if !ok {
		return
	}
	title := "调整副本数 " + p.Kind + "/" + p.Name + " → " + strconv.Itoa(p.Replicas) + " @" + p.Cluster
	a.k8sWrite(w, r, model.OpsKindContainerScale, title, p, func() (*model.ContainerQueryResult, error) {
		return a.k8sLive.ScaleWorkload(r.Context(), p.Cluster, p.Namespace, p.Kind, p.Name, p.Replicas)
	})
}

// DELETE /api/v1/container/k8s/live/{cluster}/pod?namespace=&name=&reason=
func (a *API) handleK8sDeletePod(w http.ResponseWriter, r *http.Request) {
	p, ok := a.parseK8sWriteParams(w, r, false, false)
	if !ok {
		return
	}
	title := "删除 Pod " + p.Name + " @" + p.Cluster
	a.k8sWrite(w, r, model.OpsKindContainerDelete, title, p, func() (*model.ContainerQueryResult, error) {
		return a.k8sLive.DeletePod(r.Context(), p.Cluster, p.Namespace, p.Name)
	})
}

// executeApprovedContainer 执行一条已批准（双人复核）的 K8s 写操作。
//
// 与 ops 类审批的区别：不创建下行任务——Server 自己就是执行点。
func (a *API) executeApprovedContainer(req *approval.Request) (string, error) {
	if a.k8sLive == nil {
		return "", errK8sLiveDisabled
	}
	p := req.Params
	replicas, _ := strconv.Atoi(p["replicas"])
	ctx := approvedContext()
	switch req.OpsKind {
	case model.OpsKindContainerRestart:
		_, err := a.k8sLive.RestartWorkload(ctx, p["cluster"], p["namespace"], p["kind"], p["name"])
		return "k8s-" + strconv.FormatInt(time.Now().UnixMilli(), 36), err
	case model.OpsKindContainerScale:
		_, err := a.k8sLive.ScaleWorkload(ctx, p["cluster"], p["namespace"], p["kind"], p["name"], replicas)
		return "k8s-" + strconv.FormatInt(time.Now().UnixMilli(), 36), err
	case model.OpsKindContainerDelete:
		_, err := a.k8sLive.DeletePod(ctx, p["cluster"], p["namespace"], p["name"])
		return "k8s-" + strconv.FormatInt(time.Now().UnixMilli(), 36), err
	}
	return "", errK8sUnknownKind
}

var (
	errK8sLiveDisabled = errors.New("K8s 直连未启用（缺少 k8s-live.yaml 配置）")
	errK8sUnknownKind  = errors.New("未知的容器写动作")
)

// executeApprovedContainer 用后台 context：审批通过后的执行不在某个请求的生命周期里
// （申请人早已关掉页面），k8sproxy 内部有自己的客户端超时。
func approvedContext() context.Context { return context.Background() }
