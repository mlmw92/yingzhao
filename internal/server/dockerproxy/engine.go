package dockerproxy

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nebula/monitor/internal/model"
)

// Docker Engine API 的查询与写操作实现（「容器与工作负载 → Docker」管理面）。
//
// 本文件是 ADR-0006 之后 Server 侧的直连实现；逻辑与投影规则从原先的 Agent 侧实现
// （internal/agent/collector/docker_query.go）整体搬过来，取舍逐条保留。
//
// **脱敏原则与 container.describe 完全一致：白名单投影，不是原始 JSON。**
// 把 Docker 的 inspect 原样透出，只要漏一个字段就是一次凭据泄露——`Config.Env` 里常见
// DB 密码 / Token / License Key，`Config.Labels` 里常见 compose 注入的凭据。
// 因此环境变量**只给变量名**，标签只给白名单键，其余一律不出现。

// 结果类型标识。
//
// 它们不再是「下行动作」（ADR-0006 之后 Docker 走 Server 直连，不经过 ops 目录），
// 但仍写进 ContainerQueryResult.Kind：前端与排障日志靠它区分"这是哪张表"。
const (
	KindInfo       = "docker.info"
	KindContainers = "docker.containers"
	KindInspect    = "docker.inspect"
	KindLogs       = "docker.logs"
	KindImages     = "docker.images"
	KindStart      = "docker.start"
	KindStop       = "docker.stop"
	KindRestart    = "docker.restart"
	KindRemove     = "docker.rm"
	KindImageRm    = "docker.image.rm"
)

// 各类列表的行上限。够覆盖一次排障，同时把响应体压在几十 KB 量级。
const (
	maxContainers = 500
	maxImages     = 500
	// maxLogBytes 是日志正文的字节预算。原先的取值依据是"回执要搭在下一轮上报的请求体里"，
	// 现在直连没有那个约束了，但上限本身仍然要留：日志是**业务数据**，
	// 平台不该变成一条把任意业务数据搬走的通道（与 container.logs 同一取舍）。
	maxLogBytes = 96 << 10
	// maxLogLineRunes 是单行的最大字符数：一行几十万字符的日志（如未换行的 JSON）
	// 会把整份响应顶爆，宁可截断并显式说明。
	maxLogLineRunes = 500
)

// labelWhitelist 是 inspect 里允许透出的标签键（前缀匹配）。
//
// 只放"描述来源与构建信息"的键：这些能帮人定位"这个容器是谁起的、什么版本"，
// 而不像自定义标签那样常被塞进凭据。
var labelWhitelist = []string{
	"com.docker.compose.project",
	"com.docker.compose.service",
	"com.docker.compose.version",
	"com.docker.compose.config-hash",
	"org.opencontainers.image.",
	"maintainer",
}

func newResult(kind, endpoint, namespace string, columns []string) *model.ContainerQueryResult {
	return &model.ContainerQueryResult{
		Kind: kind, Cluster: endpoint, Namespace: namespace,
		Columns: columns, Rows: make([][]string, 0, 16),
		FetchedAt: model.NowMillis(),
	}
}

// addRow 追加一行。类型定义在 model，所以这里只能是自由函数而不是方法。
func addRow(r *model.ContainerQueryResult, row ...string) { r.Rows = append(r.Rows, row) }

// ---- 引擎概览 ----

type infoResp struct {
	ServerVersion     string `json:"ServerVersion"`
	APIVersion        string `json:"ApiVersion"`
	OperatingSystem   string `json:"OperatingSystem"`
	KernelVersion     string `json:"KernelVersion"`
	Driver            string `json:"Driver"`
	Containers        int    `json:"Containers"`
	ContainersRunning int    `json:"ContainersRunning"`
	ContainersPaused  int    `json:"ContainersPaused"`
	ContainersStopped int    `json:"ContainersStopped"`
	Images            int    `json:"Images"`
	NCPU              int    `json:"NCPU"`
	MemTotal          int64  `json:"MemTotal"`
	DockerRootDir     string `json:"DockerRootDir"`
}

// QueryInfo 查询引擎概览。
func QueryInfo(ctx context.Context, ep Endpoint) (*model.ContainerQueryResult, error) {
	var info infoResp
	if err := GetJSON(ctx, ep, "/info", &info); err != nil {
		return nil, err
	}
	res := newResult(KindInfo, ep.Name, "", []string{"项目", "值"})
	addRow(res, "引擎版本", fallback(info.ServerVersion, "—"))
	addRow(res, "API 版本", fallback(info.APIVersion, "—"))
	addRow(res, "操作系统", fallback(info.OperatingSystem, "—"))
	addRow(res, "内核版本", fallback(info.KernelVersion, "—"))
	addRow(res, "存储驱动", fallback(info.Driver, "—"))
	addRow(res, "数据目录", fallback(info.DockerRootDir, "—"))
	addRow(res, "CPU 核数", strconv.Itoa(info.NCPU))
	addRow(res, "内存总量", humanBytes(info.MemTotal))
	addRow(res, "容器总数", strconv.Itoa(info.Containers))
	addRow(res, "运行中", strconv.Itoa(info.ContainersRunning))
	addRow(res, "已暂停", strconv.Itoa(info.ContainersPaused))
	addRow(res, "已停止", strconv.Itoa(info.ContainersStopped))
	addRow(res, "镜像数", strconv.Itoa(info.Images))
	res.Total = len(res.Rows)
	return res, nil
}

// ---- 容器列表 ----

type portItem struct {
	IP          string `json:"IP"`
	PrivatePort int    `json:"PrivatePort"`
	PublicPort  int    `json:"PublicPort"`
	Type        string `json:"Type"`
}

type containerListItem struct {
	ID      string     `json:"Id"`
	Names   []string   `json:"Names"`
	Image   string     `json:"Image"`
	State   string     `json:"State"`
	Status  string     `json:"Status"`
	Created int64      `json:"Created"`
	Ports   []portItem `json:"Ports"`
}

// QueryContainers 列出容器。all 为 true 时包含已停止的。
func QueryContainers(ctx context.Context, ep Endpoint, all bool) (*model.ContainerQueryResult, error) {
	var list []containerListItem
	path := "/containers/json?all=" + strconv.FormatBool(all)
	if err := GetJSON(ctx, ep, path, &list); err != nil {
		return nil, err
	}
	res := newResult(KindContainers, ep.Name, "",
		[]string{"名称", "状态", "镜像", "端口", "创建时间", "ID"})
	res.Total = len(list)
	for _, it := range list {
		if len(res.Rows) >= maxContainers {
			res.Truncated = true
			break
		}
		addRow(res,
			strings.TrimPrefix(firstOr(it.Names, ""), "/"),
			fallback(it.Status, fallback(it.State, "—")),
			fallback(it.Image, "—"),
			fallback(formatPorts(it.Ports), "—"),
			formatUnixSeconds(it.Created),
			shortID(it.ID),
		)
	}
	if !all {
		res.Notice = "只显示运行中的容器（包含已停止 = false）"
	}
	return res, nil
}

// ---- 容器详情（白名单投影）----

type inspectResp struct {
	ID      string `json:"Id"`
	Name    string `json:"Name"`
	Created string `json:"Created"`
	Image   string `json:"Image"`
	Path    string `json:"Path"`
	Args    []string `json:"Args"`
	State   struct {
		Status       string `json:"Status"`
		Running      bool   `json:"Running"`
		Paused       bool   `json:"Paused"`
		Restarting   bool   `json:"Restarting"`
		OOMKilled    bool   `json:"OOMKilled"`
		Dead         bool   `json:"Dead"`
		Pid          int    `json:"Pid"`
		ExitCode     int    `json:"ExitCode"`
		StartedAt    string `json:"StartedAt"`
		FinishedAt   string `json:"FinishedAt"`
		RestartCount int    `json:"RestartCount"`
		Health       *struct {
			Status        string `json:"Status"`
			FailingStreak int    `json:"FailingStreak"`
		} `json:"Health"`
	} `json:"State"`
	Config struct {
		Image      string            `json:"Image"`
		Hostname   string            `json:"Hostname"`
		User       string            `json:"User"`
		WorkingDir string            `json:"WorkingDir"`
		Env        []string          `json:"Env"`
		Labels     map[string]string `json:"Labels"`
		Entrypoint []string          `json:"Entrypoint"`
		Cmd        []string          `json:"Cmd"`
	} `json:"Config"`
	HostConfig struct {
		NetworkMode   string   `json:"NetworkMode"`
		Binds         []string `json:"Binds"`
		Privileged    bool     `json:"Privileged"`
		RestartPolicy struct {
			Name              string `json:"Name"`
			MaximumRetryCount int    `json:"MaximumRetryCount"`
		} `json:"RestartPolicy"`
	} `json:"HostConfig"`
	Mounts []struct {
		Type        string `json:"Type"`
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
		Mode        string `json:"Mode"`
		RW          bool   `json:"RW"`
	} `json:"Mounts"`
	NetworkSettings struct {
		Ports    map[string][]struct {
			HostIP   string `json:"HostIp"`
			HostPort string `json:"HostPort"`
		} `json:"Ports"`
		Networks map[string]struct {
			IPAddress  string `json:"IPAddress"`
			MacAddress string `json:"MacAddress"`
		} `json:"Networks"`
	} `json:"NetworkSettings"`
}

// QueryInspect 查询单个容器的详情（**白名单投影**）。
func QueryInspect(ctx context.Context, ep Endpoint, id string) (*model.ContainerQueryResult, error) {
	var obj inspectResp
	if err := GetJSON(ctx, ep, "/containers/"+url.PathEscape(id)+"/json", &obj); err != nil {
		return nil, err
	}

	res := newResult(KindInspect, ep.Name, "", []string{"项目", "值"})
	addRow(res, "名称", strings.TrimPrefix(fallback(obj.Name, id), "/"))
	addRow(res, "ID", shortID(obj.ID))
	addRow(res, "镜像", fallback(obj.Config.Image, fallback(obj.Image, "—")))
	addRow(res, "状态", stateText(obj.State.Status, obj.State.Health))
	addRow(res, "启动时间", fallback(obj.State.StartedAt, "—"))
	if obj.State.FinishedAt != "" && !strings.HasPrefix(obj.State.FinishedAt, "0001-") {
		addRow(res, "退出时间", obj.State.FinishedAt)
		addRow(res, "退出码", strconv.Itoa(obj.State.ExitCode))
	}
	addRow(res, "重启次数", strconv.Itoa(obj.State.RestartCount))
	addRow(res, "PID", strconv.Itoa(obj.State.Pid))
	addRow(res, "主机名", fallback(obj.Config.Hostname, "—"))
	addRow(res, "运行用户", fallback(obj.Config.User, "root（未指定）"))
	addRow(res, "工作目录", fallback(obj.Config.WorkingDir, "—"))
	addRow(res, "入口命令", truncate(strings.Join(append(append([]string{}, obj.Config.Entrypoint...), obj.Config.Cmd...), " "), 300))
	addRow(res, "重启策略", restartPolicyText(obj.HostConfig.RestartPolicy.Name, obj.HostConfig.RestartPolicy.MaximumRetryCount))
	// 特权容器是一个安全信号，必须显式透出（"看起来正常"的容器也可能挂在宿主机上）。
	addRow(res, "特权模式", boolText(obj.HostConfig.Privileged))
	addRow(res, "网络模式", fallback(obj.HostConfig.NetworkMode, "—"))
	addRow(res, "端口映射", fallback(inspectPorts(obj.NetworkSettings.Ports), "—"))
	addRow(res, "网络", fallback(inspectNetworks(obj.NetworkSettings.Networks), "—"))
	addRow(res, "挂载", fallback(inspectMounts(obj.Mounts, obj.HostConfig.Binds), "—"))
	addRow(res, "环境变量（仅名称）", fallback(envNames(obj.Config.Env), "—"))
	addRow(res, "标签（白名单）", fallback(whitelistedLabels(obj.Config.Labels), "—"))
	res.Total = len(res.Rows)
	res.Notice = "环境变量只给名称、不给值；标签只给来源类白名单键——详情是白名单投影，不是原始 inspect JSON"
	return res, nil
}

// ---- 容器日志 ----

// QueryLogs 拉取某个容器的最近日志。
//
// 刻意不提供"全量日志下载"：那等于给平台开了一条把任意业务数据搬走的通道
// （与 container.logs 同一取舍）。
func QueryLogs(ctx context.Context, ep Endpoint, id string, tailLines, sinceSeconds int) (*model.ContainerQueryResult, error) {
	q := url.Values{}
	q.Set("stdout", "true")
	q.Set("stderr", "true")
	q.Set("tail", strconv.Itoa(tailLines))
	if sinceSeconds > 0 {
		// Docker 的 since 收 Unix 时间戳（秒）或 RFC3339；用时间戳，避免时区歧义。
		q.Set("since", strconv.FormatInt(time.Now().Add(-time.Duration(sinceSeconds)*time.Second).Unix(), 10))
	}
	raw, err := GetRaw(ctx, ep, "/containers/"+url.PathEscape(id)+"/logs?"+q.Encode())
	if err != nil {
		return nil, err
	}

	res := newResult(KindLogs, ep.Name, "", []string{"日志"})
	overflow := false
	text := string(demuxLogs(raw))
	if len(text) > maxLogBytes {
		text = text[:maxLogBytes]
		overflow = true
	}
	if overflow {
		// 字节预算先于行数用尽：丢掉可能被切断的半行，并把截断显式说出来。
		if idx := strings.LastIndexByte(text, '\n'); idx >= 0 {
			text = text[:idx]
		} else {
			text = ""
		}
	}
	text = strings.TrimRight(text, "\n")
	if text != "" {
		for _, line := range strings.Split(text, "\n") {
			addRow(res, truncate(strings.TrimRight(line, "\r"), maxLogLineRunes))
		}
	}
	res.Total = len(res.Rows)
	// 行数打满即视为"更早的还有"：Docker 只按 tail 截取，不告诉我们上面还有多少。
	if overflow || len(res.Rows) >= tailLines {
		res.Truncated = true
	}
	notice := fmt.Sprintf("最近 %d 行", tailLines)
	if sinceSeconds > 0 {
		notice += fmt.Sprintf("，时间窗 %d 秒", sinceSeconds)
	}
	if res.Truncated {
		notice += "；更早的日志未拉取（平台不提供全量下载）"
	}
	res.Notice = notice
	return res, nil
}

// ---- 镜像列表 ----

type imageListItem struct {
	ID          string   `json:"Id"`
	RepoTags    []string `json:"RepoTags"`
	RepoDigests []string `json:"RepoDigests"`
	Created     int64    `json:"Created"`
	Size        int64    `json:"Size"`
}

// QueryImages 列出镜像。
func QueryImages(ctx context.Context, ep Endpoint) (*model.ContainerQueryResult, error) {
	var list []imageListItem
	if err := GetJSON(ctx, ep, "/images/json", &list); err != nil {
		return nil, err
	}
	res := newResult(KindImages, ep.Name, "", []string{"仓库标签", "镜像 ID", "大小", "创建时间"})
	res.Total = len(list)
	dangling := 0
	for _, it := range list {
		if len(res.Rows) >= maxImages {
			res.Truncated = true
			break
		}
		tag := imageTag(it.RepoTags)
		if tag == "<none>:<none>" {
			dangling++
		}
		addRow(res, tag, shortID(it.ID), humanBytes(it.Size), formatUnixSeconds(it.Created))
	}
	if dangling > 0 {
		res.Notice = fmt.Sprintf("其中 %d 个是悬空镜像（无标签）", dangling)
	}
	return res, nil
}

// ---- 写操作 ----

func writeResult(kind, endpoint, msg string) *model.ContainerQueryResult {
	res := newResult(kind, endpoint, "", []string{"结果"})
	addRow(res, msg)
	res.Total = 1
	return res
}

// containerLifecycle 是 start / stop / restart 的共同实现。
func containerLifecycle(ctx context.Context, ep Endpoint, kind, id, action string, timeout int, label string) (*model.ContainerQueryResult, error) {
	path := "/containers/" + url.PathEscape(id) + "/" + action
	if timeout > 0 {
		path += "?t=" + strconv.Itoa(timeout)
	}
	if err := Write(ctx, ep, http.MethodPost, path); err != nil {
		return nil, err
	}
	return writeResult(kind, ep.Name, fmt.Sprintf("%s 容器 %s", label, id)), nil
}

// Start 启动容器。
func Start(ctx context.Context, ep Endpoint, id string) (*model.ContainerQueryResult, error) {
	return containerLifecycle(ctx, ep, KindStart, id, "start", 0, "已启动")
}

// Stop 停止容器（timeout 为 SIGKILL 前的宽限期，0 表示用 daemon 默认值）。
func Stop(ctx context.Context, ep Endpoint, id string, timeout int) (*model.ContainerQueryResult, error) {
	return containerLifecycle(ctx, ep, KindStop, id, "stop", timeout, "已停止")
}

// Restart 重启容器（timeout 同上）。
func Restart(ctx context.Context, ep Endpoint, id string, timeout int) (*model.ContainerQueryResult, error) {
	return containerLifecycle(ctx, ep, KindRestart, id, "restart", timeout, "已重启")
}

// RemoveContainer 删除容器。
//
// **刻意不带 `v=1`**：卷里是数据，删掉不可逆；而"顺手删容器"的人多半没打算连数据一起删。
// 要看或删卷请上机器用 docker CLI——那是另一件需要单独同意的事。
func RemoveContainer(ctx context.Context, ep Endpoint, id string, force bool) (*model.ContainerQueryResult, error) {
	path := "/containers/" + url.PathEscape(id) + "?force=" + strconv.FormatBool(force)
	if err := Write(ctx, ep, http.MethodDelete, path); err != nil {
		return nil, err
	}
	return writeResult(KindRemove, ep.Name, fmt.Sprintf("已删除容器 %s（未删卷）", id)), nil
}

// RemoveImage 删除镜像（force=false 时被引用的镜像会报错而不是被强行删掉）。
func RemoveImage(ctx context.Context, ep Endpoint, id string, force bool) (*model.ContainerQueryResult, error) {
	path := "/images/" + url.PathEscape(id) + "?force=" + strconv.FormatBool(force)
	if err := Write(ctx, ep, http.MethodDelete, path); err != nil {
		return nil, err
	}
	return writeResult(KindImageRm, ep.Name, fmt.Sprintf("已删除镜像 %s", id)), nil
}

// ---- 投影与格式化小工具 ----

// demuxLogs 去掉 Docker 的多路复用帧头。
//
// 容器**未分配 TTY** 时（绝大多数服务型容器），`/logs` 返回的是 8 字节帧头的多路复用流：
// `[stream(1) 0 0 0 size(4, big-endian)] + payload`。不去帧头，正文里会混进二进制垃圾。
// 分配了 TTY 时是原始流、没有帧头——因此只在**整段都能按帧解析**时才去帧头，
// 否则原样返回：误判会把正文前 8 字节吃掉，那比多几个二进制字节更糟。
func demuxLogs(b []byte) []byte {
	if len(b) < 8 {
		return b
	}
	out := make([]byte, 0, len(b))
	i := 0
	for i+8 <= len(b) {
		if b[i] > 2 || b[i+1] != 0 || b[i+2] != 0 || b[i+3] != 0 {
			return b
		}
		size := int(b[i+4])<<24 | int(b[i+5])<<16 | int(b[i+6])<<8 | int(b[i+7])
		if size < 0 || i+8+size > len(b) {
			return b
		}
		out = append(out, b[i+8:i+8+size]...)
		i += 8 + size
	}
	if i != len(b) {
		return b
	}
	return out
}

// stateText 组合状态与健康检查结果：只看 Status 会把"健康检查失败"的容器显示成健康的。
func stateText(status string, health *struct {
	Status        string `json:"Status"`
	FailingStreak int    `json:"FailingStreak"`
}) string {
	s := fallback(status, "—")
	if health == nil {
		return s
	}
	s += "（健康检查：" + fallback(health.Status, "未知")
	if health.FailingStreak > 0 {
		s += fmt.Sprintf("，连续失败 %d 次", health.FailingStreak)
	}
	return s + "）"
}

func restartPolicyText(name string, maxRetry int) string {
	name = fallback(name, "no")
	if name == "on-failure" && maxRetry > 0 {
		return fmt.Sprintf("%s（最多 %d 次）", name, maxRetry)
	}
	return name
}

// envNames 只返回环境变量的**名称**，值一律不出现。
//
// 这是本文件最重要的一行取舍：`Config.Env` 里常见 DB 密码 / Token / License Key，
// 而排障时真正有用的是"它读了哪些变量"（判断配置来源），不是值本身。
func envNames(env []string) string {
	names := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		name = strings.TrimSpace(name)
		if name != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// whitelistedLabels 只透出来源类白名单键，其余键**连键名都不给**。
//
// 为什么连键名都不给：自定义标签的键名本身就可能泄露内部约定（如 `secret-name=prod-db`）。
func whitelistedLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		if !labelAllowed(k) {
			continue
		}
		keys = append(keys, k+"="+truncate(labels[k], 80))
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

func labelAllowed(k string) bool {
	for _, p := range labelWhitelist {
		if strings.HasPrefix(k, p) {
			return true
		}
	}
	return false
}

func inspectMounts(mounts []struct {
	Type        string `json:"Type"`
	Source      string `json:"Source"`
	Destination string `json:"Destination"`
	Mode        string `json:"Mode"`
	RW          bool   `json:"RW"`
}, binds []string) string {
	parts := make([]string, 0, len(mounts)+len(binds))
	for _, m := range mounts {
		parts = append(parts, fmt.Sprintf("%s %s → %s（%s）", m.Type, m.Source, m.Destination, rwText(m.RW)))
	}
	// Binds 与 Mounts 内容重叠，只在 Mounts 为空时兜底（老 API 版本可能只给 Binds）。
	if len(parts) == 0 {
		parts = append(parts, binds...)
	}
	return strings.Join(parts, "；")
}

func inspectPorts(ports map[string][]struct {
	HostIP   string `json:"HostIp"`
	HostPort string `json:"HostPort"`
}) string {
	if len(ports) == 0 {
		return ""
	}
	keys := make([]string, 0, len(ports))
	for k := range ports {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, containerPort := range keys {
		bindings := ports[containerPort]
		if len(bindings) == 0 {
			parts = append(parts, containerPort)
			continue
		}
		for _, b := range bindings {
			parts = append(parts, fmt.Sprintf("%s:%s → %s", fallback(b.HostIP, "0.0.0.0"), b.HostPort, containerPort))
		}
	}
	return strings.Join(parts, ", ")
}

func inspectNetworks(networks map[string]struct {
	IPAddress  string `json:"IPAddress"`
	MacAddress string `json:"MacAddress"`
}) string {
	if len(networks) == 0 {
		return ""
	}
	names := make([]string, 0, len(networks))
	for name := range networks {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, name+"("+fallback(networks[name].IPAddress, "无 IP")+")")
	}
	return strings.Join(parts, ", ")
}

func formatPorts(ports []portItem) string {
	parts := make([]string, 0, len(ports))
	for _, p := range ports {
		if p.PublicPort > 0 {
			parts = append(parts, fmt.Sprintf("%s:%d → %d/%s", fallback(p.IP, "0.0.0.0"), p.PublicPort, p.PrivatePort, p.Type))
			continue
		}
		parts = append(parts, fmt.Sprintf("%d/%s", p.PrivatePort, p.Type))
	}
	return strings.Join(parts, ", ")
}

func imageTag(tags []string) string {
	if len(tags) == 0 {
		return "<none>:<none>"
	}
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		if strings.TrimSpace(t) == "" {
			continue
		}
		out = append(out, t)
	}
	if len(out) == 0 {
		return "<none>:<none>"
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}

// shortID 把 64 位 ID 截成 12 位（与 `docker ps` 的显示口径一致）。
func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func formatUnixSeconds(sec int64) string {
	if sec <= 0 {
		return "—"
	}
	return time.Unix(sec, 0).Format("2006-01-02 15:04:05")
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	v := float64(n)
	for _, u := range units {
		v /= unit
		if v < unit {
			return fmt.Sprintf("%.2f %s", v, u)
		}
	}
	return fmt.Sprintf("%.2f PiB", v/unit)
}

func rwText(rw bool) string {
	if rw {
		return "读写"
	}
	return "只读"
}

func boolText(b bool) string {
	if b {
		return "是"
	}
	return "否"
}

func firstOr(list []string, def string) string {
	if len(list) == 0 {
		return def
	}
	return list[0]
}

func fallback(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}
