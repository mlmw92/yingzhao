package k8sproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/mlmw92/yingzhao/internal/model"
)

// K8s 写操作：Server 直连 apiserver 执行（2026-10-23 决定）。
//
// 此前这三个动作走 Agent 下行通道，理由是"集群凭据只存 Agent 本地 + 逐机护栏"。
// 现在改为 Server 直连，换取**即时反馈**与「Agent 只负责采集」这条边界：
// 写权限由 k8s-live.yaml 里的凭据决定，平台侧仍有三道闸——权限点 `container:write`（仅超管）、
// 界面二次确认、审计（开启审批时另加双人复核）。
//
// 部署前提：k8s-live.yaml 里该集群的凭据必须具备写权限；只读凭据会在 apiserver 侧 403，
// 错误会原样带回（见 write_test 的 TestWriteSurfacesAPIServerError）。

// workloadResource 把动作目录里的 kind 映射到 API 资源名。
//
// 只接受这三种：Job 的"重启"没有标准语义，扩缩容对 DaemonSet 无意义（副本数由节点数决定）。
func workloadResource(kind string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "deployment", "deployments":
		return "deployments", true
	case "statefulset", "statefulsets":
		return "statefulsets", true
	case "daemonset", "daemonsets":
		return "daemonsets", true
	}
	return "", false
}

// doJSON 发起一次写请求并解码响应；非 2xx 时把 apiserver 的正文带进错误。
func doJSON(ctx context.Context, conn *conn, method, path, contentType string, body any, out any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, conn.server+path, reader)
	if err != nil {
		return err
	}
	if conn.token != "" {
		req.Header.Set("Authorization", "Bearer "+conn.token)
	}
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := conn.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("apiserver 返回 %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out != nil && len(raw) > 0 {
		_ = json.Unmarshal(raw, out) // 写操作的结果只用于展示，解析失败不算失败
	}
	return nil
}

// RestartWorkload 滚动重启一个工作负载（等价 kubectl rollout restart）。
//
// 实现方式是给 Pod 模板打 restartedAt 注解——不是删 Pod：控制器会按滚动策略逐个替换，
// 服务不中断。
func (m *Manager) RestartWorkload(ctx context.Context, cluster, namespace, kind, name string) (*ContainerQueryResult, error) {
	resource, ok := workloadResource(kind)
	if !ok {
		return nil, fmt.Errorf("不支持的工作负载类型: %q（可选 Deployment / StatefulSet / DaemonSet）", kind)
	}
	c, err := m.conn(cluster)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	patch := map[string]any{
		"spec": map[string]any{
			"template": map[string]any{
				"metadata": map[string]any{
					"annotations": map[string]string{"kubectl.kubernetes.io/restartedAt": now},
				},
			},
		},
	}
	path := fmt.Sprintf("/apis/apps/v1/namespaces/%s/%s/%s", namespace, resource, name)
	if err := doJSON(ctx, c, http.MethodPatch, path, "application/merge-patch+json", patch, nil); err != nil {
		return nil, err
	}
	res := newContainerResult(model.OpsKindContainerRestart, cluster, namespace, []string{"结果"})
	addRow(res, fmt.Sprintf("已重启 %s/%s（滚动重启，服务不中断）", kind, name))
	return res, nil
}

// ScaleWorkload 调整工作负载副本数（走 /scale 子资源：只改副本数的窄接口）。
func (m *Manager) ScaleWorkload(ctx context.Context, cluster, namespace, kind, name string, replicas int) (*ContainerQueryResult, error) {
	resource, ok := workloadResource(kind)
	if !ok {
		return nil, fmt.Errorf("不支持的工作负载类型: %q（可选 Deployment / StatefulSet）", kind)
	}
	if resource == "daemonsets" {
		return nil, fmt.Errorf("DaemonSet 的副本数由节点数决定，不支持扩缩容")
	}
	if replicas < 0 || replicas > model.OpsMaxReplicas {
		return nil, fmt.Errorf("副本数必须在 0 到 %d 之间", model.OpsMaxReplicas)
	}
	c, err := m.conn(cluster)
	if err != nil {
		return nil, err
	}
	patch := map[string]any{"spec": map[string]any{"replicas": replicas}}
	path := fmt.Sprintf("/apis/apps/v1/namespaces/%s/%s/%s/scale", namespace, resource, name)
	if err := doJSON(ctx, c, http.MethodPatch, path, "application/merge-patch+json", patch, nil); err != nil {
		return nil, err
	}
	res := newContainerResult(model.OpsKindContainerScale, cluster, namespace, []string{"结果"})
	addRow(res, fmt.Sprintf("已把 %s/%s 的副本数调整为 %d", kind, name, replicas))
	return res, nil
}

// DeletePod 删除一个 Pod（控制器会重建它；裸 Pod 上就是一次真实中断）。
func (m *Manager) DeletePod(ctx context.Context, cluster, namespace, name string) (*ContainerQueryResult, error) {
	c, err := m.conn(cluster)
	if err != nil {
		return nil, err
	}
	path := fmt.Sprintf("/api/v1/namespaces/%s/pods/%s", namespace, name)
	if err := doJSON(ctx, c, http.MethodDelete, path, "", nil, nil); err != nil {
		return nil, err
	}
	res := newContainerResult(model.OpsKindContainerDelete, cluster, namespace, []string{"结果"})
	addRow(res, fmt.Sprintf("已删除 Pod %s（控制器会重建它）", name))
	return res, nil
}
