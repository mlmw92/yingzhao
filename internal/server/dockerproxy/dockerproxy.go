// Package dockerproxy 提供「容器与工作负载 → Docker」管理面所需的 Server 侧 Docker Engine API 直连。
//
// 这是 ADR-0006 对 ADR-0005 的修订。ADR-0005 判定 Docker「不存在 Server 直连的等价通路」，
// 于是把全部能力（含只读列表）压进 Agent 异步下行。该判定不成立：
// `model.DockerInstanceConfig.Addr` 本来就允许 `tcp://host:2375`（网络可达），
// 与 K8s 的 apiserver 地址没有本质差别；而 ADR-0004 早已为 K8s 定下原则——
// 「异步下发的延迟等于一个上报周期，**不满足管理面的要求**」。
// 同一条原则对 Docker 同样成立，所以管理面改为 Server 直连调接口：点开即得。
//
// 端点清单来自 Agent 上报（instancereg），运维**不需要**在 Server 上另配一份：
// 目标机器配了 dockerInstances，这里就有端点（ADR-0006 选定「复用上报端点」）。
//
// **地址边界**：daemon 地址只在 Server 内部使用。任何 API 响应体都不得包含它——
// 它随上报离开了 Agent 本机（这是「管理面要实时」的代价），但不该再进浏览器。
// 因此本包的对外错误一律经过 dialError / 调用方的 slog，不复述地址取值。
package dockerproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/mlmw92/yingzhao/internal/server/instancereg"
)

const (
	// readTimeout 覆盖一次只读请求（容器 / 镜像列表、inspect、info、日志）。
	readTimeout = 15 * time.Second
	// writeTimeout 必须**大于**写操作自身的耗时上限：`/containers/{id}/stop?t=N` 会阻塞到
	// 容器停下来或等满 N 秒才返回。客户端超时小于 N 就会把"正在停"报成"请求失败"，
	// 而容器其实停掉了——这种假失败比真失败更难查（人会重复点，第二次撞 304）。
	writeTimeout = 60 * time.Second
	// maxRespBytes 是响应体上限：一个畸形或被替换的 daemon 不该能把 Server 内存打满。
	maxRespBytes = 32 << 20
)

// Endpoint 是一条可直连的 Docker 端点，由 Agent 上报的 model.DockerEndpoint 而来。
//
// 不直接复用 model.DockerEndpoint：那个是**上报协议载荷**（带 json tag、会随上行体走），
// 这个是 Server 内部的直连句柄。两者字段眼下相同，但用途不同——协议要能独立演进。
type Endpoint struct {
	Node string
	Name string
	// Addr 是 daemon 地址。**只在 Server 内部流转**，不进任何响应体。
	Addr string
	// Reachable 是 Agent 最近一次采集时该 daemon 是否可达（不是实时探测结果）。
	Reachable bool
}

// Endpoints 返回全部已上报的 Docker 端点。
//
// 不做节点范围过滤：那是 API 层的职责（它才知道当前登录者的数据范围），
// 本包只管"有哪些端点、怎么连上去"。
func Endpoints() []Endpoint {
	reported := instancereg.Default.DockerEndpoints()
	out := make([]Endpoint, 0, len(reported))
	for _, ep := range reported {
		out = append(out, Endpoint{
			Node:      ep.Node,
			Name:      ep.Name,
			Addr:      ep.Addr,
			Reachable: ep.Reachable,
		})
	}
	return out
}

// Resolve 按 (node, name) 定位端点。
//
// 用二元组而不是只用 name：`dockerInstances[].name` 是**每台机器各自起的名**，
// 不同机器上叫同一个名字完全可能（都是 "local"）。只按 name 找会命中错的那台机器，
// 而"在 A 机器上停掉了 B 机器的容器"是这类接口最不能犯的错。
func Resolve(node, name string) (Endpoint, bool) {
	for _, ep := range Endpoints() {
		if ep.Node == node && ep.Name == name {
			return ep, true
		}
	}
	return Endpoint{}, false
}

// client 按地址构造 HTTP 客户端，并返回请求基址。
//
// 两种地址形态（与 Agent 侧采集器同一套约定，见 collector.newDockerHTTPClient）：
//   - `unix:///path/to/docker.sock` —— 自定义 DialContext 走 unix socket。
//     **只在 Server 与 daemon 同一台机器时可用**；远程机器上的 unix socket 连不上，
//     这时 dialError 会给出可执行的提示，而不是一个裸的 "no such file or directory"。
//   - `tcp://host:port` 或 `host:port` —— 走 TCP（daemon 需以 `-H tcp://` 暴露，
//     或前面挂一个 socket 代理）。
func client(addr string, timeout time.Duration) (*http.Client, string, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil, "", fmt.Errorf("该端点没有上报 daemon 地址")
	}
	if strings.HasPrefix(addr, "unix://") {
		socketPath := strings.TrimPrefix(addr, "unix://")
		if socketPath == "" {
			return nil, "", fmt.Errorf("该端点的 unix socket 路径为空")
		}
		tr := &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				d := net.Dialer{Timeout: 5 * time.Second}
				return d.DialContext(ctx, "unix", socketPath)
			},
		}
		return &http.Client{Transport: tr, Timeout: timeout}, "http://docker", nil
	}
	base := addr
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "http://" + base
	}
	return &http.Client{Timeout: timeout}, strings.TrimRight(base, "/"), nil
}

// call 发起一次请求，返回响应体（已限长）与状态码。
//
// err 只表示「请求没走通」（拨号失败、超时、读体失败）；非 2xx 不在这里判死——
// 读要报错、写要把 304 单独翻译（见 Write），两者的语义不同，由调用方决定。
func call(ctx context.Context, ep Endpoint, method, path string, timeout time.Duration) ([]byte, int, error) {
	c, base, err := client(ep.Addr, timeout)
	if err != nil {
		return nil, 0, err
	}
	// 不跨请求复用连接：一次查询就是一次点击，保持长连接只会把过期的 socket 留在池里
	// （daemon 重启后第一个请求会撞上它）。
	defer c.CloseIdleConnections()

	req, err := http.NewRequestWithContext(ctx, method, base+path, nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := c.Do(req)
	if err != nil {
		// 原始错误里常含地址取值（dial tcp 10.0.0.1:2375: …），只进日志不进响应。
		slog.Warn("Docker 直连失败", "node", ep.Node, "endpoint", ep.Name, "err", err)
		return nil, 0, dialError(ep)
	}
	defer resp.Body.Close()

	body, rerr := io.ReadAll(io.LimitReader(resp.Body, maxRespBytes))
	if rerr != nil {
		slog.Warn("Docker 响应读取失败", "node", ep.Node, "endpoint", ep.Name, "err", rerr)
		return nil, resp.StatusCode, fmt.Errorf("读取 Docker 响应失败（连接中断或响应过大）")
	}
	return body, resp.StatusCode, nil
}

// dialError 把拨号失败翻译成能指导动作的话，并且**不复述地址取值**。
//
// 最常见的失败是"远程机器上的 unix socket"：原始错误是
// `dial unix /var/run/docker.sock: connect: no such file or directory`，
// 看起来像 daemon 挂了，实际是 Server 根本够不着那台机器的 socket。
func dialError(ep Endpoint) error {
	if strings.HasPrefix(strings.TrimSpace(ep.Addr), "unix://") {
		return fmt.Errorf(
			"无法连接 Docker 端点 %s（节点 %s）：该端点用的是 unix socket，只有 Server 与 daemon 在同一台机器时才能直连。"+
				"请在目标机器上把 daemon 暴露为 tcp://（或前面挂一个 socket 代理），或把 Server 部署到该机器上",
			ep.Name, ep.Node)
	}
	return fmt.Errorf(
		"无法连接 Docker 端点 %s（节点 %s）：daemon 不可达或拒绝连接。"+
			"请确认目标机器的 daemon 已用 tcp 方式暴露，且 Server 到该机器网络可达",
		ep.Name, ep.Node)
}

// GetJSON 发起只读 GET 并把响应体解析到 out。
func GetJSON(ctx context.Context, ep Endpoint, path string, out any) error {
	body, status, err := call(ctx, ep, http.MethodGet, path, readTimeout)
	if err != nil {
		return err
	}
	if status < 200 || status > 299 {
		return statusError(ep, status, body)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("解析 Docker 响应失败：%w", err)
	}
	return nil
}

// GetRaw 发起只读 GET 并返回原始响应体。
//
// 给 `/logs` 用：它返回的是**流**（未分配 TTY 时是带 8 字节帧头的多路复用流），不是 JSON，
// 走不了 GetJSON。
func GetRaw(ctx context.Context, ep Endpoint, path string) ([]byte, error) {
	body, status, err := call(ctx, ep, http.MethodGet, path, readTimeout)
	if err != nil {
		return nil, err
	}
	if status < 200 || status > 299 {
		return nil, statusError(ep, status, body)
	}
	return body, nil
}

// Write 发起一次写请求（POST / DELETE）。
//
// 与只读分开的理由：写请求可能返回 204（无正文）或 200（有正文），
// 而**错误信息要原样带给操作者**——"容器已经在运行"「被引用的镜像删不掉」这类结论
// 只有 daemon 知道，平台猜不出来，也猜错不起（猜错就是让人以为操作成功了）。
func Write(ctx context.Context, ep Endpoint, method, path string) error {
	body, status, err := call(ctx, ep, method, path, writeTimeout)
	if err != nil {
		return err
	}
	if status >= 200 && status <= 299 {
		return nil
	}
	// Docker 用 304 表达"目标已处于该状态"（容器已在运行 / 已停止）。按 RFC 7230，
	// 304 **不允许带正文**，Go 也会把正文丢掉——所以这里不能指望 daemon 的说明，
	// 只能自己把它翻译成人话：原样报一个裸状态码，用户看不懂"304 到底成没成"。
	if status == http.StatusNotModified {
		return fmt.Errorf("目标已处于该状态（docker daemon 返回 304，例如容器已经在运行或已经停止）")
	}
	return statusError(ep, status, body)
}

// statusError 把非 2xx 的响应体做成错误。daemon 的说明是给操作者看的**结论**，原样带上；
// 但地址与端点身份只用一个短前缀，不复述 daemon 地址。
func statusError(ep Endpoint, status int, body []byte) error {
	msg := strings.TrimSpace(string(body))
	if msg == "" {
		msg = http.StatusText(status)
	}
	return fmt.Errorf("docker daemon 返回 %d（端点 %s）：%s", status, ep.Name, truncate(msg, 300))
}

// truncate 按字符截断，避免把多字节汉字切成乱码。
func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
