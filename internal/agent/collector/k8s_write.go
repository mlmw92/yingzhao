package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/nebula/monitor/internal/model"
)

// K8s 排障写操作（批次 C，见 docs/superpowers/specs/2026-10-10-container-workload-management-design.md §2.2、§4）。
//
// 只走异步下行（ADR-0004 的 Server 侧只读代理**保持只读**、白名单不外扩）：
// 写操作需要逐机护栏与凭据边界。
//
// 刻意**不做**：删除工作负载 / 删除命名空间 / 任意 apply —— 那是不可逆的破坏，
// 或者绕过白名单的任意配置写入。

// workloadResourcePath 把对象 Kind 映射成 apiserver 的资源段。
func workloadResourcePath(kind string) (string, bool) {
	switch kind {
	case "Deployment":
		return "deployments", true
	case "StatefulSet":
		return "statefulsets", true
	case "DaemonSet":
		return "daemonsets", true
	}
	return "", false
}

// doJSON 向 apiserver 发起写请求（PATCH / DELETE）。
//
// 与 getJSON 分开：写请求可能返回 204（无正文），而**失败原因必须原样带回**
// （forbidden / not found / conflict 这类结论只有 apiserver 知道，平台猜不出来，猜错就是让人以为改成功了）。
func (c *K8sCollector) doJSON(ctx context.Context, conn *k8sConn, method, path, contentType, body string, out any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, conn.apiServer+path, rdr)
	if err != nil {
		return err
	}
	if conn.token != "" {
		req.Header.Set("Authorization", "Bearer "+conn.token)
	}
	req.Header.Set("Accept", "application/json")
	if body != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := conn.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		msg := strings.TrimSpace(string(b))
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("apiserver 返回 %d：%s", resp.StatusCode, truncateLine(msg, 300))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}

// k8sWriteResult 把一次写操作的结果包成单行结论（写操作没有表格产出）。
func k8sWriteResult(kind, cluster, namespace, msg string) *ContainerQueryResult {
	res := newContainerResult(kind, cluster, namespace, []string{"结果"})
	addRow(res, msg)
	res.Total = 1
	return res
}

// QueryRestartWorkload 滚动重启一个工作负载。
//
// 做法与 `kubectl rollout restart` 完全一致：给 Pod 模板的注解打一个重启时间戳。
// **刻意不删 Pod**——删 Pod 会让副本数瞬间不足；打注解则由控制器按 maxSurge / maxUnavailable
// 逐个替换，滚动期间服务不中断。这也正是"重启工作负载"与"删除 Pod"该分成两个动作的原因。
func (c *K8sCollector) QueryRestartWorkload(ctx context.Context, cluster, namespace, kind, name string) (*ContainerQueryResult, error) {
	cfg, conn, err := c.queryConn(cluster)
	if err != nil {
		return nil, err
	}
	defer conn.client.CloseIdleConnections()

	resource, ok := workloadResourcePath(kind)
	if !ok {
		return nil, fmt.Errorf("不支持重启的类型：%s", kind)
	}
	// 时间戳必须带时区（RFC3339 UTC）：否则不同节点按本地时区解释同一个注解，
	// 会出现"注解看着没变、控制器也没重启"的静默失败。
	stamp := time.Now().UTC().Format(time.RFC3339)
	body := fmt.Sprintf(`{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":%q}}}}}`, stamp)
	path := "/apis/apps/v1/namespaces/" + url.PathEscape(namespace) + "/" + resource + "/" + url.PathEscape(name)
	if err := c.doJSON(ctx, conn, http.MethodPatch, path, "application/strategic-merge-patch+json", body, nil); err != nil {
		return nil, err
	}
	return k8sWriteResult(model.OpsKindContainerRestart, cfg.Name, namespace,
		fmt.Sprintf("已滚动重启 %s/%s（重启时间戳 %s）", kind, name, stamp)), nil
}

// QueryScaleWorkload 调整工作负载的副本数。
func (c *K8sCollector) QueryScaleWorkload(ctx context.Context, cluster, namespace, kind, name string, replicas int) (*ContainerQueryResult, error) {
	cfg, conn, err := c.queryConn(cluster)
	if err != nil {
		return nil, err
	}
	defer conn.client.CloseIdleConnections()

	resource, ok := workloadResourcePath(kind)
	if !ok {
		return nil, fmt.Errorf("不支持扩缩容的类型：%s", kind)
	}
	// 走 /scale 子资源而不是直接改 spec.replicas：前者是 K8s 为"只改副本数"提供的窄接口，
	// 语义最清楚，也不会因为合并补丁的写法差异误伤 spec 里的其它字段。
	body := fmt.Sprintf(`{"spec":{"replicas":%d}}`, replicas)
	path := "/apis/apps/v1/namespaces/" + url.PathEscape(namespace) + "/" + resource + "/" + url.PathEscape(name) + "/scale"
	if err := c.doJSON(ctx, conn, http.MethodPatch, path, "application/merge-patch+json", body, nil); err != nil {
		return nil, err
	}
	msg := fmt.Sprintf("已把 %s/%s 的副本数调整为 %d", kind, name, replicas)
	if replicas == 0 {
		msg += "（0 副本：该服务已下线）"
	}
	return k8sWriteResult(model.OpsKindContainerScale, cfg.Name, namespace, msg), nil
}

// QueryDeletePod 删除一个 Pod（若它由控制器管理，会被重建）。
func (c *K8sCollector) QueryDeletePod(ctx context.Context, cluster, namespace, name string) (*ContainerQueryResult, error) {
	cfg, conn, err := c.queryConn(cluster)
	if err != nil {
		return nil, err
	}
	defer conn.client.CloseIdleConnections()

	path := "/api/v1/namespaces/" + url.PathEscape(namespace) + "/pods/" + url.PathEscape(name)
	if err := c.doJSON(ctx, conn, http.MethodDelete, path, "", "", nil); err != nil {
		return nil, err
	}
	return k8sWriteResult(model.OpsKindContainerDelete, cfg.Name, namespace,
		fmt.Sprintf("已删除 Pod %s（若它由控制器管理，会被重建）", name)), nil
}
