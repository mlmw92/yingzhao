package k8sproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/nebula/monitor/internal/model"
)

// ---- 结果类型标识 ----
//
// 它们不再是「下行动作」（ADR-0006 之后这条通路不经过 ops 目录），但仍写进
// ContainerQueryResult.Kind：前端与排障日志靠它区分"这是哪张表"。
// 取值刻意沿用旧的 container.* 字面量——前端按它做缓存键，改名字只会换来无谓的改动。
const (
	KindWorkloads = "container.workloads"
	KindPods      = "container.pods"
	KindEvents    = "container.events"
	KindObject    = "container.describe"
	KindLogs      = "container.logs"
)

// ---- 连接 ----

// conn 按集群名取连接（凭据来自 k8s-live.yaml，由运维在 Server 上手工维护）。
func (m *Manager) conn(cluster string) (*conn, error) {
	c, ok := m.Get(cluster)
	if !ok {
		return nil, fmt.Errorf("集群 %q 未在 %s 中配置", cluster, m.path)
	}
	return buildConn(c)
}

// getJSON 向 apiserver 发起 GET 并解码 JSON。
func getJSON(ctx context.Context, conn *conn, path string, out interface{}) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, conn.server+path, nil)
	if err != nil {
		return err
	}
	if conn.token != "" {
		req.Header.Set("Authorization", "Bearer "+conn.token)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := conn.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("apiserver 返回 %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("解析 apiserver 响应失败：%w", err)
	}
	return nil
}

// ---- apiserver 返回体里的容器状态 ----
//
// 与 Agent 侧采集用的同名类型形状一致，这不是巧合：**状态的真相在容器级状态里，不在 Pod 的
// phase 里**（镜像拉不动时 phase 仍是 Pending，容器反复崩溃时 phase 仍是 Running）。
// 两边共用一个形状，才能让"有效状态"的判定只有 podStatus 一处实现。

type k8sContainerStatus struct {
	Name         string `json:"name"`
	Ready        bool   `json:"ready"`
	RestartCount int32  `json:"restartCount"`
	State        struct {
		Waiting    k8sContainerWaiting     `json:"waiting"`
		Terminated *k8sContainerTerminated `json:"terminated"`
	} `json:"state"`
}

type k8sContainerWaiting struct {
	Reason string `json:"reason"`
}

type k8sContainerTerminated struct {
	Reason   string `json:"reason"`
	ExitCode int    `json:"exitCode"`
}

// podStatus 推断 Pod 的**有效状态**，语义对齐 `kubectl get pods` 的 STATUS 列：
// 容器等待原因优先（ImagePullBackOff / CrashLoopBackOff / RunContainerError ...），
// 其次是容器终止原因（Error / OOMKilled / Completed），最后才回退到 phase。
//
// 为什么必须这样：只报 phase 会让崩溃中的 Pod 在界面上显示成"运行中"——
// 那是这个页面最不该给出的一个错误结论。
func podStatus(phase string, cs []k8sContainerStatus) string {
	for _, c := range cs {
		if r := strings.TrimSpace(c.State.Waiting.Reason); r != "" {
			return r
		}
	}
	for _, c := range cs {
		t := c.State.Terminated
		if t == nil {
			continue
		}
		if r := strings.TrimSpace(t.Reason); r != "" {
			return r
		}
		if t.ExitCode != 0 {
			return "Error"
		}
		return "Completed"
	}
	return phase
}

// zhPodStatus 把**有效状态**翻成中文，只用于展示（Pod 列表的状态列、Pod 详情的状态行）。
//
// 为什么不直接在 podStatus 里返回中文：它的返回值是**判定用的值**，不是文案——
// Agent 侧（internal/agent/collector/k8s.go）就是靠 `status == "Running"` 数"运行中"、
// 靠容器级原因算"异常"的。在那一层翻译，统计口径会静默失效。
//
// 词表与工作负载表的既有用词对齐（就绪 / 已完成 / 运行中 / 已停止），不另造一套；
// **未收录的原因原样返回**——宁可中英混排，也不猜一个可能译错的中文（错译比原文更难排查）。
func zhPodStatus(s string) string {
	if zh, ok := podStatusZh[s]; ok {
		return zh
	}
	return s
}

var podStatusZh = map[string]string{
	// phase（podStatus 的兜底来源）
	"Pending":   "等待中",
	"Running":   "运行中",
	"Succeeded": "已完成",
	"Failed":    "已失败",
	"Unknown":   "未知",
	// 容器等待原因：优先级最高，崩溃与镜像拉取失败都落在这一档
	"ContainerCreating":          "创建中",
	"PodInitializing":            "初始化中",
	"ErrImagePull":               "镜像拉取失败",
	"ImagePullBackOff":           "镜像拉取重试",
	"ErrImageNeverPull":          "镜像禁止拉取",
	"ImageInspectError":          "镜像检查失败",
	"InvalidImageName":           "镜像名无效",
	"CreateContainerConfigError": "配置错误",
	"CreateContainerError":       "创建失败",
	"RunContainerError":          "启动失败",
	"CrashLoopBackOff":           "崩溃重启中",
	"ContainerCannotRun":         "无法运行",
	"ContainerStatusUnknown":     "状态未知",
	// 容器终止原因
	"Completed":        "已完成",
	"Error":            "异常退出",
	"OOMKilled":        "内存超限",
	"DeadlineExceeded": "执行超时",
	"StartError":       "启动失败",
	"Evicted":          "已被驱逐",
	"NodeLost":         "节点失联",
	"Shutdown":         "已关闭",
}

// K8s 对象级只读查询（「容器与工作负载 → Kubernetes」的数据通路）。
//
// 2026-10-10 重规划（ADR-0006）：这套查询原先跑在 **Agent** 侧、由统一下行通道的
// container.* 动作调用（指令延迟一个上报周期）。现在整体搬到 **Server** 侧，用
// k8s-live.yaml 里的凭据直连 apiserver——ADR-0004 早就写过理由：
// 「异步下发的延迟等于一个上报周期，不满足管理面的要求」。那 5 个下行动作已随之删除。
//
// **投影规则原样保留，它是安全边界而不是美化**（见 QueryObject 的注释）：
// Pod 的 env[].value、ConfigMap 的 data、注解里的 last-applied-configuration 都可能含明文密钥，
// 所以详情走白名单投影、而不是"脱敏后原样透出"——脱敏规则漏一个字段就是一次凭据泄露。
//
// 三条硬约束（沿用）：
//  1. **只发 GET**，不带任何请求体。
//  2. **行数有界**：所有列表都用 apiserver 的 `limit` + 自己的行上限，超出即标 truncated，
//     绝不"先全拉回来再截断"。
//  3. **详情走白名单投影**，不把对象 JSON 原样透出。

// 各类列表的行上限。取值依据：够覆盖常见集群的一次排障，同时把响应体压在几十 KB 量级。
const (
	containerMaxWorkloads = 300
	containerMaxPods      = 500
	containerMaxEvents    = 200

	// containerMaxLogBytes 是单次日志拉取的字节预算（96 KiB）。
	//
	// 取值不是"随便定大一点"：日志是**业务自己的输出**，可能含敏感内容，
	// 平台不该变成一条把任意业务数据搬走的通道。上限仍然要有——
	// 直连之后没有"撑爆上报体"那层约束了，但"一次拉几百兆日志"从来不是排障需求。
	containerMaxLogBytes = 96 << 10
	// containerLogLineMaxRunes 是单行的最大字符数：一行几十万字符的日志（如未换行的 JSON）
	// 会把整份响应顶爆，宁可截断并显式说明截了几行。
	containerLogLineMaxRunes = 500
)

// ContainerQueryResult 定义在 internal/model——它被前端解析，不只属于本包。
// 这里用别名，让本文件的签名读起来短一些。
type ContainerQueryResult = model.ContainerQueryResult

// ---- apiserver 返回体的最小结构 ----
//
// 只声明用得到的字段：K8s 的响应结构体极大，全量声明既没人维护也容易过时。

type cqMeta struct {
	Name              string `json:"name"`
	Namespace         string `json:"namespace"`
	CreationTimestamp string `json:"creationTimestamp"`
}

type cqListMeta struct {
	// Continue 非空表示还有下一页（我们不会去翻，只用来标记 truncated）。
	Continue string `json:"continue"`
}

type cqWorkload struct {
	Metadata cqMeta `json:"metadata"`
	Spec     struct {
		Replicas *int32 `json:"replicas"`
	} `json:"spec"`
	Status struct {
		Replicas               int32 `json:"replicas"`
		ReadyReplicas          int32 `json:"readyReplicas"`
		AvailableReplicas      int32 `json:"availableReplicas"`
		NumberReady            int32 `json:"numberReady"`
		NumberScheduled        int32 `json:"numberScheduled"`
		DesiredNumberScheduled int32 `json:"desiredNumberScheduled"`
		Succeeded              int32 `json:"succeeded"`
		Failed                 int32 `json:"failed"`
		Active                 int32 `json:"active"`
	} `json:"status"`
}

type cqWorkloadList struct {
	Metadata cqListMeta   `json:"metadata"`
	Items    []cqWorkload `json:"items"`
}

type cqPod struct {
	Metadata cqMeta `json:"metadata"`
	Spec     struct {
		NodeName string `json:"nodeName"`
	} `json:"spec"`
	Status struct {
		Phase     string `json:"phase"`
		PodIP     string `json:"podIP"`
		StartTime string `json:"startTime"`
		// 容器级状态（本包定义的 k8sContainerStatus）：ready/restartCount 是排障第一眼要看的，
		// state 里的 waiting/terminated reason 才是"这个 Pod 到底怎么了"的答案。
		ContainerStatuses []k8sContainerStatus `json:"containerStatuses"`
	} `json:"status"`
}

type cqPodList struct {
	Metadata cqListMeta `json:"metadata"`
	Items    []cqPod    `json:"items"`
}

type cqEvent struct {
	Metadata cqMeta `json:"metadata"`
	Type     string `json:"type"`
	Reason   string `json:"reason"`
	Message  string `json:"message"`
	Count    int32  `json:"count"`
	// 三个时间字段都可能为空：不同来源（kubelet / controller）填的不是同一个。
	LastTimestamp  string `json:"lastTimestamp"`
	EventTime      string `json:"eventTime"`
	FirstTimestamp string `json:"firstTimestamp"`
	InvolvedObject struct {
		Kind      string `json:"kind"`
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"involvedObject"`
}

type cqEventList struct {
	Metadata cqListMeta `json:"metadata"`
	Items    []cqEvent  `json:"items"`
}

func newContainerResult(kind, cluster, namespace string, columns []string) *ContainerQueryResult {
	return &ContainerQueryResult{
		Kind: kind, Cluster: cluster, Namespace: namespace,
		Columns: columns, Rows: make([][]string, 0, 16),
		FetchedAt: model.NowMillis(),
	}
}

// addRow 追加一行。类型定义在 model，所以这里只能是自由函数而不是方法。
func addRow(r *ContainerQueryResult, row ...string) { r.Rows = append(r.Rows, row) }

// QueryWorkloads 列工作负载与副本就绪情况。
func (m *Manager) QueryWorkloads(ctx context.Context, cluster, namespace string) (*ContainerQueryResult, error) {
	conn, err := m.conn(cluster)
	if err != nil {
		return nil, err
	}
	defer conn.client.CloseIdleConnections()

	res := newContainerResult(KindWorkloads, cluster, namespace,
		[]string{"类型", "命名空间", "名称", "期望", "就绪", "可用", "状态"})

	appsBase := "/apis/apps/v1"
	batchBase := "/apis/batch/v1"
	if namespace != "" {
		esc := url.PathEscape(namespace)
		appsBase = "/apis/apps/v1/namespaces/" + esc
		batchBase = "/apis/batch/v1/namespaces/" + esc
	}

	targets := []struct {
		label string
		base  string
		path  string
		limit int
	}{
		{"Deployment", appsBase, "/deployments", containerMaxWorkloads},
		{"StatefulSet", appsBase, "/statefulsets", containerMaxWorkloads},
		{"DaemonSet", appsBase, "/daemonsets", containerMaxWorkloads},
		{"Job", batchBase, "/jobs", containerMaxWorkloads},
	}
	for _, t := range targets {
		if err := m.appendWorkloads(ctx, conn, res, t.label, t.base+t.path, t.limit); err != nil {
			// 单个类型拉不到（例如没有 batch 权限）不该让整次查询失败——其余类型的结果仍然有用。
			res.Notice = appendNotice(res.Notice, fmt.Sprintf("%s 读取失败：%v", t.label, err))
		}
	}
	return res, nil
}

func (m *Manager) appendWorkloads(ctx context.Context, conn *conn, res *ContainerQueryResult, label, path string, limit int) error {
	q := url.Values{}
	q.Set("limit", strconv.Itoa(limit))
	var list cqWorkloadList
	if err := getJSON(ctx, conn, path+"?"+q.Encode(), &list); err != nil {
		return err
	}
	for _, it := range list.Items {
		res.Total++
		if len(res.Rows) >= limit {
			res.Truncated = true
			continue
		}
		desired, ready, extra, state := workloadReplicas(label, it)
		addRow(res, label, it.Metadata.Namespace, it.Metadata.Name, desired, ready, extra, state)
	}
	if list.Metadata.Continue != "" {
		res.Truncated = true
	}
	return nil
}

// workloadReplicas 把四种工作负载的"期望/就绪/可用/结论"归一成四列。
//
// 它们的字段名并不统一（Deployment 是 spec.replicas，DaemonSet 是 desiredNumberScheduled/
// numberReady，Job 是 active/succeeded/failed），这里做一次归一，前端就不必知道对象是哪种类型。
//
// 结论按类型分别判定：把 Job 的"成功 3/失败 1"套成 Deployment 的"副本不足"是错的——
// Job 跑完就该退出，它不是"没就绪"。
func workloadReplicas(label string, it cqWorkload) (desired, ready, extra, state string) {
	switch label {
	case "DaemonSet":
		desired = itoa32(it.Status.DesiredNumberScheduled)
		ready = itoa32(it.Status.NumberReady)
		extra = itoa32(it.Status.NumberScheduled)
		switch {
		case desired == "0":
			state = "无匹配节点"
		case desired == ready:
			state = "就绪"
		default:
			state = "副本不足"
		}
	case "Job":
		desired = "—"
		ready = itoa32(it.Status.Active)
		extra = fmt.Sprintf("成功 %s / 失败 %s", itoa32(it.Status.Succeeded), itoa32(it.Status.Failed))
		switch {
		case it.Status.Failed > 0:
			state = "有失败"
		case it.Status.Active > 0:
			state = "运行中"
		default:
			state = "已完成"
		}
	default:
		n := int32(0)
		if it.Spec.Replicas != nil {
			n = *it.Spec.Replicas
		}
		desired = itoa32(n)
		ready = itoa32(it.Status.ReadyReplicas)
		extra = itoa32(it.Status.AvailableReplicas)
		switch {
		case n == 0:
			state = "已停止"
		case it.Status.ReadyReplicas == n:
			state = "就绪"
		default:
			state = "副本不足"
		}
	}
	return desired, ready, extra, state
}

// QueryPods 列 Pod 的状态、重启次数与所在节点。
func (m *Manager) QueryPods(ctx context.Context, cluster, namespace string) (*ContainerQueryResult, error) {
	conn, err := m.conn(cluster)
	if err != nil {
		return nil, err
	}
	defer conn.client.CloseIdleConnections()

	res := newContainerResult(KindPods, cluster, namespace,
		[]string{"命名空间", "名称", "状态", "就绪", "重启", "节点", "Pod IP"})

	path := "/api/v1/pods"
	if namespace != "" {
		path = "/api/v1/namespaces/" + url.PathEscape(namespace) + "/pods"
	}
	q := url.Values{}
	q.Set("limit", strconv.Itoa(containerMaxPods))
	var list cqPodList
	if err := getJSON(ctx, conn, path+"?"+q.Encode(), &list); err != nil {
		return nil, err
	}
	for _, it := range list.Items {
		res.Total++
		if len(res.Rows) >= containerMaxPods {
			res.Truncated = true
			continue
		}
		ready, restarts := 0, int32(0)
		for _, cs := range it.Status.ContainerStatuses {
			if cs.Ready {
				ready++
			}
			restarts += cs.RestartCount
		}
		// 状态列用**有效状态**（对齐 kubectl 的 STATUS），而不是裸 phase：
		// 只报 phase 会把 ImagePullBackOff 显示成 Pending、把 CrashLoopBackOff
		// 显示成 Running——后者看起来是健康的，正好把要排的故障藏起来。
		// 判定用英文值，展示再经 zhPodStatus 翻中文（见该函数的注释）。
		addRow(res, it.Metadata.Namespace, it.Metadata.Name,
			zhPodStatus(fallback(podStatus(it.Status.Phase, it.Status.ContainerStatuses), "Unknown")),
			fmt.Sprintf("%d/%d", ready, len(it.Status.ContainerStatuses)),
			itoa32(restarts),
			fallback(it.Spec.NodeName, "—"),
			fallback(it.Status.PodIP, "—"))
	}
	if list.Metadata.Continue != "" {
		res.Truncated = true
	}
	return res, nil
}

// QueryEvents 列事件，按时间倒序取最近若干条。
func (m *Manager) QueryEvents(ctx context.Context, cluster, namespace, name string) (*ContainerQueryResult, error) {
	conn, err := m.conn(cluster)
	if err != nil {
		return nil, err
	}
	defer conn.client.CloseIdleConnections()

	res := newContainerResult(KindEvents, cluster, namespace,
		[]string{"时间", "类型", "对象", "原因", "次数", "内容"})

	path := "/api/v1/events"
	if namespace != "" {
		path = "/api/v1/namespaces/" + url.PathEscape(namespace) + "/events"
	}
	// fieldSelector 在服务端过滤，比拉回来再筛省一个数量级——事件在一个吵闹的集群里非常多。
	q := url.Values{}
	q.Set("limit", strconv.Itoa(containerMaxEvents))
	if name != "" {
		q.Set("fieldSelector", "involvedObject.name="+name)
	}
	var list cqEventList
	if err := getJSON(ctx, conn, path+"?"+q.Encode(), &list); err != nil {
		return nil, err
	}

	items := list.Items
	sort.SliceStable(items, func(i, j int) bool {
		return eventTime(items[i]) > eventTime(items[j])
	})
	for _, it := range items {
		res.Total++
		if len(res.Rows) >= containerMaxEvents {
			res.Truncated = true
			continue
		}
		obj := it.InvolvedObject.Kind + "/" + it.InvolvedObject.Name
		addRow(res, formatK8sTime(eventTime(it)), fallback(it.Type, "—"), obj,
			fallback(it.Reason, "—"), itoa32(it.Count), truncateLine(it.Message, 300))
	}
	if list.Metadata.Continue != "" {
		res.Truncated = true
	}
	return res, nil
}

func eventTime(e cqEvent) string {
	for _, t := range []string{e.LastTimestamp, e.EventTime, e.FirstTimestamp} {
		if t != "" {
			return t
		}
	}
	return ""
}

// QueryLogs 拉取某个 Pod 的最近日志。
//
// 与列表类查询的差别：apiserver 的 `/log` 子资源返回**纯文本**（不是 JSON），且没有分页，
// 边界完全靠 tailLines / sinceSeconds 两个参数（上层已按上限校过，这里再兜一次字节预算）。
//
// 刻意不提供"全量日志下载"：那等于给平台开了一条把任意业务数据搬走的通道。
// 要看全量应该上机器或走日志后端。
func (m *Manager) QueryLogs(ctx context.Context, cluster, namespace, pod, container string, tailLines, sinceSeconds int) (*ContainerQueryResult, error) {
	conn, err := m.conn(cluster)
	if err != nil {
		return nil, err
	}
	defer conn.client.CloseIdleConnections()

	res := newContainerResult(KindLogs, cluster, namespace, []string{"日志"})

	q := url.Values{}
	q.Set("tailLines", strconv.Itoa(tailLines))
	if sinceSeconds > 0 {
		q.Set("sinceSeconds", strconv.Itoa(sinceSeconds))
	}
	if container != "" {
		q.Set("container", container)
	}
	path := "/api/v1/namespaces/" + url.PathEscape(namespace) + "/pods/" + url.PathEscape(pod) + "/log?" + q.Encode()
	text, overflow, err := getText(ctx, conn, path, containerMaxLogBytes)
	if err != nil {
		return nil, err
	}
	if overflow {
		// 字节预算先于行数用尽：丢掉可能被切断的半行，并把截断显式说出来。
		if idx := strings.LastIndexByte(text, '\n'); idx >= 0 {
			text = text[:idx]
		} else {
			text = ""
		}
	}

	clipped := 0
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		short := truncateLine(line, containerLogLineMaxRunes)
		if short != line {
			clipped++
		}
		addRow(res, short)
	}
	res.Total = len(res.Rows)
	// 行数打满即视为"更早的还有"：apiserver 只保证返回最后 N 行，不告诉我们上面还有多少。
	if overflow || res.Total >= tailLines {
		res.Truncated = true
	}

	scope := fmt.Sprintf("Pod %s/%s", namespace, pod)
	if container != "" {
		scope += " · 容器 " + container
	} else {
		scope += " · 第一个容器"
	}
	notice := fmt.Sprintf("%s · 最近 %d 行", scope, tailLines)
	if sinceSeconds > 0 {
		notice += fmt.Sprintf("（时间窗 %d 秒）", sinceSeconds)
	}
	if res.Truncated {
		notice += "；已达上限，更早的日志未拉取"
	}
	if clipped > 0 {
		notice += fmt.Sprintf("；%d 行因过长被截断", clipped)
	}
	res.Notice = notice
	return res, nil
}

// getText 向 apiserver 发起 GET 并返回原始文本（Pod 日志不是 JSON，不能走 getJSON）。
//
// overflow 表示正文超过 maxBytes 被截断——调用方据此把"日志比预算更长"如实标出来，
// 而不是让使用者以为"日志就到这里了"。
func getText(ctx context.Context, conn *conn, path string, maxBytes int64) (string, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, conn.server+path, nil)
	if err != nil {
		return "", false, err
	}
	if conn.token != "" {
		req.Header.Set("Authorization", "Bearer "+conn.token)
	}
	// `/log` 子资源虽然返回**纯文本**，它的内容协商却只认 json / yaml / protobuf：
	// 送 `Accept: text/plain` 会被 406 顶回来（实测 k3s v1.36.2，报
	// "only the following media types are accepted: application/json, ..."）。
	// `*/*` 交给服务端挑默认序列化器，正文仍是原始日志文本。
	req.Header.Set("Accept", "*/*")
	resp, err := conn.client.Do(req)
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", false, fmt.Errorf("apiserver 返回 %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return "", false, err
	}
	if int64(len(raw)) > maxBytes {
		return string(raw[:maxBytes]), true, nil
	}
	return string(raw), false, nil
}

// ---- 单对象详情 ----

// QueryObject 返回单个对象的详情（白名单投影）。
//
// 刻意**不做**"把对象 JSON 脱敏后原样透出"：那样只要漏一个字段就是一次凭据泄露，
// 而 Pod 的 `env[].value`、ConfigMap 的 `data`、注解里的 `last-applied-configuration`
// 都可能含明文密钥。这里改成按资源类型逐项投影——只取排障真正需要的字段，
// 敏感内容压根不进返回体。
func (m *Manager) QueryObject(ctx context.Context, cluster, namespace, resource, name string) (*ContainerQueryResult, error) {
	conn, err := m.conn(cluster)
	if err != nil {
		return nil, err
	}
	defer conn.client.CloseIdleConnections()

	res := newContainerResult(KindObject, cluster, namespace,
		[]string{"字段", "值"})
	addRow(res, "资源", resource+"/"+name)

	path, ok := objectPath(resource, namespace, name)
	if !ok {
		return nil, fmt.Errorf("不支持的资源类型 %q", resource)
	}
	var raw map[string]any
	if err := getJSON(ctx, conn, path, &raw); err != nil {
		return nil, err
	}

	switch resource {
	case "pods":
		projectPod(raw, res)
	case "deployments", "statefulsets", "daemonsets":
		projectWorkload(raw, res)
	case "jobs":
		projectJob(raw, res)
	case "services":
		projectService(raw, res)
	case "configmaps":
		projectConfigMap(raw, res)
	}
	return res, nil
}

// objectPath 把「资源类型 + 命名空间 + 名字」映射成 apiserver 路径。
// 返回 ok=false 表示不在白名单里——**路径是拼出来的，所以白名单必须在拼之前生效**。
func objectPath(resource, namespace, name string) (string, bool) {
	esc := url.PathEscape(namespace)
	escName := url.PathEscape(name)
	switch resource {
	case "pods":
		return "/api/v1/namespaces/" + esc + "/pods/" + escName, true
	case "services":
		return "/api/v1/namespaces/" + esc + "/services/" + escName, true
	case "configmaps":
		return "/api/v1/namespaces/" + esc + "/configmaps/" + escName, true
	case "deployments":
		return "/apis/apps/v1/namespaces/" + esc + "/deployments/" + escName, true
	case "statefulsets":
		return "/apis/apps/v1/namespaces/" + esc + "/statefulsets/" + escName, true
	case "daemonsets":
		return "/apis/apps/v1/namespaces/" + esc + "/daemonsets/" + escName, true
	case "jobs":
		return "/apis/batch/v1/namespaces/" + esc + "/jobs/" + escName, true
	}
	return "", false
}

func projectPod(raw map[string]any, res *ContainerQueryResult) {
	meta, _ := raw["metadata"].(map[string]any)
	spec, _ := raw["spec"].(map[string]any)
	status, _ := raw["status"].(map[string]any)

	addRow(res, "命名空间", str(meta["namespace"]))
	addRow(res, "创建时间", formatK8sTime(str(meta["creationTimestamp"])))
	addRow(res, "标签", joinMap(meta["labels"]))
	addRow(res, "节点", str(spec["nodeName"]))
	cstatus := rawContainerStatuses(status["containerStatuses"])
	addRow(res, "Pod IP", str(status["podIP"]))
	// 状态给**有效状态**（对齐 kubectl 的 STATUS）：只给 phase 时，
	// CrashLoopBackOff 的 Pod 会显示成 Running —— 看起来是健康的。
	// 与 Pod 列表用同一套中文词表：同一个 Pod 在两个入口的说法必须一致。
	addRow(res, "状态", zhPodStatus(fallback(podStatus(str(status["phase"]), cstatus), "—")))
	addRow(res, "QoS", str(status["qosClass"]))

	// 只列容器名与镜像：env 的**值**可能含明文密钥，一律不下发。
	for _, field := range []string{"containers", "initContainers"} {
		list, _ := spec[field].([]any)
		for _, it := range list {
			c, _ := it.(map[string]any)
			addRow(res, field, str(c["name"])+" → "+str(c["image"]))
		}
	}
	// 容器级原因：这才是"这个 Pod 为什么起不来"的答案（镜像拉不动 / 反复崩溃 / OOM）。
	// 光有容器名和镜像不够——排查时第一眼要看的就是这里。
	for _, c := range cstatus {
		addRow(res, "容器状态", containerStatusText(c))
	}
	appendEnvNames(res, spec, "containers", "容器环境变量名")
	appendConditions(res, status)
	res.Notice = appendNotice(res.Notice, "环境变量只返回变量名，不返回值；密钥类字段不下发")
}

// rawContainerStatuses 把原始对象里的 containerStatuses 转成共用类型。
// 这里走一次 JSON 往返，而不是手写字段提取：目的是让"有效状态"始终只有
// podStatus（k8s.go）一处实现。手写提取等于把判定逻辑复制一遍，
// 采集页与查询页迟早会给出互相矛盾的结论。
func rawContainerStatuses(v any) []k8sContainerStatus {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var out []k8sContainerStatus
	if err := json.Unmarshal(b, &out); err != nil {
		return nil
	}
	return out
}

// containerStatusText 一行说清一个容器的处境：状态（含原因）与重启次数。
func containerStatusText(c k8sContainerStatus) string {
	state := "Running"
	if r := strings.TrimSpace(c.State.Waiting.Reason); r != "" {
		state = r
	} else if t := c.State.Terminated; t != nil {
		switch {
		case strings.TrimSpace(t.Reason) != "":
			state = t.Reason
		case t.ExitCode != 0:
			state = "Error"
		default:
			state = "Completed"
		}
	}
	s := c.Name + ": " + state
	if c.RestartCount > 0 {
		s += fmt.Sprintf("（重启 %d 次）", c.RestartCount)
	}
	return s
}

func projectWorkload(raw map[string]any, res *ContainerQueryResult) {
	meta, _ := raw["metadata"].(map[string]any)
	spec, _ := raw["spec"].(map[string]any)
	status, _ := raw["status"].(map[string]any)

	addRow(res, "命名空间", str(meta["namespace"]))
	addRow(res, "创建时间", formatK8sTime(str(meta["creationTimestamp"])))
	addRow(res, "标签", joinMap(meta["labels"]))
	addRow(res, "期望副本", str(spec["replicas"]))
	addRow(res, "就绪副本", str(status["readyReplicas"]))
	addRow(res, "可用副本", str(status["availableReplicas"]))
	addRow(res, "更新副本", str(status["updatedReplicas"]))
	tmpl, _ := spec["template"].(map[string]any)
	tmplSpec, _ := tmpl["spec"].(map[string]any)
	for _, field := range []string{"containers", "initContainers"} {
		list, _ := tmplSpec[field].([]any)
		for _, it := range list {
			c, _ := it.(map[string]any)
			addRow(res, field, str(c["name"])+" → "+str(c["image"]))
		}
	}
	appendConditions(res, status)
}

func projectJob(raw map[string]any, res *ContainerQueryResult) {
	meta, _ := raw["metadata"].(map[string]any)
	status, _ := raw["status"].(map[string]any)
	addRow(res, "命名空间", str(meta["namespace"]))
	addRow(res, "创建时间", formatK8sTime(str(meta["creationTimestamp"])))
	addRow(res, "活跃", str(status["active"]))
	addRow(res, "成功", str(status["succeeded"]))
	addRow(res, "失败", str(status["failed"]))
	addRow(res, "开始时间", formatK8sTime(str(status["startTime"])))
	addRow(res, "完成时间", formatK8sTime(str(status["completionTime"])))
	appendConditions(res, status)
}

func projectService(raw map[string]any, res *ContainerQueryResult) {
	meta, _ := raw["metadata"].(map[string]any)
	spec, _ := raw["spec"].(map[string]any)
	addRow(res, "命名空间", str(meta["namespace"]))
	addRow(res, "类型", str(spec["type"]))
	addRow(res, "ClusterIP", str(spec["clusterIP"]))
	addRow(res, "选择器", joinMap(spec["selector"]))
	ports, _ := spec["ports"].([]any)
	for _, it := range ports {
		p, _ := it.(map[string]any)
		addRow(res, "端口", fmt.Sprintf("%s:%s/%s", str(p["name"]), str(p["port"]), fallback(str(p["protocol"]), "TCP")))
	}
}

func projectConfigMap(raw map[string]any, res *ContainerQueryResult) {
	meta, _ := raw["metadata"].(map[string]any)
	data, _ := raw["data"].(map[string]any)
	binary, _ := raw["binaryData"].(map[string]any)
	addRow(res, "命名空间", str(meta["namespace"]))
	// 只给键名：ConfigMap 的 value 在生产里经常被拿来放配置口令。
	for k := range data {
		addRow(res, "配置键", k)
	}
	for k := range binary {
		addRow(res, "二进制键", k)
	}
	res.Notice = appendNotice(res.Notice, "ConfigMap 只返回键名，不返回值（内容可能含凭据）")
}

func appendEnvNames(res *ContainerQueryResult, spec map[string]any, field, label string) {
	list, _ := spec[field].([]any)
	for _, it := range list {
		c, _ := it.(map[string]any)
		envs, _ := c["env"].([]any)
		names := make([]string, 0, len(envs))
		for _, e := range envs {
			em, _ := e.(map[string]any)
			if n := str(em["name"]); n != "" {
				names = append(names, n)
			}
		}
		if len(names) > 0 {
			addRow(res, label, str(c["name"])+": "+strings.Join(names, ", "))
		}
	}
}

func appendConditions(res *ContainerQueryResult, status map[string]any) {
	conds, _ := status["conditions"].([]any)
	for _, it := range conds {
		c, _ := it.(map[string]any)
		addRow(res, "条件", fmt.Sprintf("%s=%s %s", str(c["type"]), str(c["status"]), truncateLine(str(c["message"]), 160)))
	}
}

// ---- 小工具 ----

func itoa32(v int32) string { return strconv.FormatInt(int64(v), 10) }

func str(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		// JSON 数字统一解成 float64；整数值不要显示成 3.000000。
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return ""
		}
		return string(b)
	}
}

func fallback(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func joinMap(v any) string {
	m, ok := v.(map[string]any)
	if !ok || len(m) == 0 {
		return "—"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k+"="+str(m[k]))
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

// truncateLine 按字符（不是字节）截断，避免把多字节汉字切成乱码。
func truncateLine(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

// formatK8sTime 把 RFC3339 压成 "2026-10-02 08:30:00"；解析不了就原样返回。
func formatK8sTime(s string) string {
	if s == "" || s == "<nil>" {
		return "—"
	}
	if len(s) >= 19 && s[4] == '-' && s[10] == 'T' {
		return s[:10] + " " + s[11:19]
	}
	return s
}

func appendNotice(cur, add string) string {
	if cur == "" {
		return add
	}
	return cur + "；" + add
}
