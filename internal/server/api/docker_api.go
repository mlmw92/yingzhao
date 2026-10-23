package api

// Docker 管理面（「容器与工作负载 → Docker」）。
//
// 数据通路：**Server 直连 Docker daemon**（ADR-0006 修订 ADR-0005）。原先的实现把全部
// 能力压进 Agent 异步下行（kind=docker.*，延迟一个上报周期），已被取代——ADR-0004 早已
// 为 K8s 定下「点开即得」这条原则，Docker 没有理由例外。
//
// 端点清单来自 Agent 上报（instancereg.DockerEndpoints），运维不需要在 Server 上另配一份。
// 因此本文件的接口都是**同步**的：点一次就是一次真实请求，失败当场可见。
//
// **地址边界**：daemon 地址随上报离开了 Agent 本机（这是「管理面要实时」的代价），
// 但只在 Server 内部使用——任何响应体都不得包含它（见 handleDockerEndpoints 的注释）。
//
// 权限：只读用 docker:read（运维管理员），写操作用 docker:write（**仅超管**）。
// 写操作没有「本机护栏」这一道了——护栏原本在 Agent 侧，直连之后不存在那个执行点；
// 剩下的三道是：权限点、界面二次确认、审计。

import (
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/mlmw92/yingzhao/internal/model"
	"github.com/mlmw92/yingzhao/internal/server/audit"
	"github.com/mlmw92/yingzhao/internal/server/dockerproxy"
)

// 参数上限。取值的依据是「一次排障够用」而不是「尽可能多给」：
// 日志是业务数据，平台不该变成一条把任意业务数据搬走的通道。
//
// 日志的行数与时间窗直接复用 model 里的协议常量（OpsLog*），不在这里另定一套：
// 同一件事两处定义，迟早会漂移成"界面允许 500 行、后端只认 200"。
const (
	// dockerMaxStopTimeout 是 stop / restart 的宽限期上限（秒）。
	//
	// 与动作通道时期的 model.OpsDockerMaxTimeoutSeconds（300 秒）刻意不同：
	// 那时上限受"回执要搭在下一轮上报的请求体里"约束，现在直连没有那个约束了，
	// 但**客户端超时**成了新约束——dockerproxy.writeTimeout 是 60 秒，
	// 宽限期必须留在那个预算里，否则会出现"daemon 还在等容器退出、Server 已经报超时"
	// 的假失败（人看到失败会再点一次，第二次撞 304 更困惑）。
	dockerMaxStopTimeout = 30

	// dockerDefaultStopTimeout 是不传 timeout 时的宽限期（秒）。
	//
	// 不能用 0：Docker **API** 在缺少 t 参数时按 0 处理（立即 SIGKILL），
	// 而 CLI 默认发 t=10——平台直接透传 0 会让"停止容器"变成"强杀"，
	// 正在处理请求的服务来不及优雅退出。跟随 CLI 的 10 秒。
	dockerDefaultStopTimeout = 10
)

// GET /api/v1/container/docker/endpoints
//
// 返回可管理的 Docker 端点清单，供界面做端点选择器。
//
// **不含 daemon 地址**：地址只在 Server 内部用于直连。这里只给定位二元组 (node, name)
// 与两个提示位：
//   - reachable：Agent 最近一次采集时 daemon 是否可达；
//   - unixSocket：地址是 unix socket。它是**静态**判断（看 scheme），不发探测请求——
//     列表页不该对每台机器发起连接。真实可达性由具体查询给出（直连后是同步的，反馈即时）。
//
// unixSocket 之所以要单独提示：Server 与 daemon 不同机时 unix socket 连不上，
// 而这是部署形态问题、不是故障——先说出来，用户才不会以为是平台坏了。
func (a *API) handleDockerEndpoints(w http.ResponseWriter, r *http.Request) {
	p := Principal(r)

	type item struct {
		Node       string `json:"node"`
		Name       string `json:"name"`
		Reachable  bool   `json:"reachable"`
		UnixSocket bool   `json:"unixSocket"`
	}

	out := make([]item, 0)
	for _, ep := range dockerproxy.Endpoints() {
		// 范围外的端点整条不下发：只给个名字也算暴露了"范围外有这么一台资产"。
		if !a.nodeInScope(p, ep.Node) {
			continue
		}
		out = append(out, item{
			Node:       ep.Node,
			Name:       ep.Name,
			Reachable:  ep.Reachable,
			UnixSocket: strings.HasPrefix(strings.TrimSpace(ep.Addr), "unix://"),
		})
	}
	// 稳定排序：选择器顺序每次刷新都变会让人点错。
	sort.Slice(out, func(i, j int) bool {
		if out[i].Node != out[j].Node {
			return out[i].Node < out[j].Node
		}
		return out[i].Name < out[j].Name
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{"endpoints": out})
}

// dockerEndpoint 解析 (node, name) 并做数据范围校验。
//
// 失败一律 404 + 同一句话：范围外与不存在对使用者的区别是权限问题，
// 但对**探测者**的区别是"这台机器上有这么个端点"——后者不该被套出来。
func (a *API) dockerEndpoint(w http.ResponseWriter, r *http.Request) (dockerproxy.Endpoint, bool) {
	node := strings.TrimSpace(r.URL.Query().Get("node"))
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if node == "" || name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "缺少 node / name 参数（Docker 端点由这两个值共同定位）",
		})
		return dockerproxy.Endpoint{}, false
	}
	ep, ok := dockerproxy.Resolve(node, name)
	if !ok || !a.nodeInScope(Principal(r), ep.Node) {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "Docker 端点不存在，或不在你的数据范围内",
		})
		return dockerproxy.Endpoint{}, false
	}
	return ep, true
}

// writeDockerResult 统一返回一次直连查询的结果。
//
// 失败给 502 并带上原文：daemon 的结论（权限不足、对象不存在、地址形态不对）对排障有价值，
// 平台猜不出来。与 handleK8sLiveQuery 的 502 口径一致。
func (a *API) writeDockerResult(w http.ResponseWriter, ep dockerproxy.Endpoint, res *model.ContainerQueryResult, err error) {
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]interface{}{
			"error":    err.Error(),
			"node":     ep.Node,
			"endpoint": ep.Name,
			"live":     false,
		})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// recordDockerWrite 为一次写操作留痕。
//
// 直连之后 Agent 侧没有执行点了，这条审计是「谁在什么时候停掉了哪个容器」的**唯一**记录，
// 因此它不能省。失败也记：写失败往往比成功更值得回头看（重复点、误操作被拒）。
func (a *API) recordDockerWrite(r *http.Request, ep dockerproxy.Endpoint, action, detail string, err error) {
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
		Category:  "docker",
		Action:    action,
		Detail:    detail + "（节点 " + ep.Node + "，端点 " + ep.Name + "）",
	})
}

// ---- 只读 ----

// GET /api/v1/container/docker/info?node=&name= —— 引擎概览。
func (a *API) handleDockerInfo(w http.ResponseWriter, r *http.Request) {
	ep, ok := a.dockerEndpoint(w, r)
	if !ok {
		return
	}
	res, err := dockerproxy.QueryInfo(r.Context(), ep)
	a.writeDockerResult(w, ep, res, err)
}

// GET /api/v1/container/docker/containers?node=&name=&all= —— 容器列表（实时）。
//
// all 默认 true：管理面默认要能看到已停止的容器，否则"为什么少了一个"要查半天。
//
// 命名带 List 是为了与中间件监控的 handleDockerContainers（按主机聚合的**指标**视图）
// 区分开：两者名字近、语义完全不同（一个是对象清单，一个是聚合计数），
// 而它们的重叠正是 2026-10-10 那次重规划要拆开的东西。
func (a *API) handleDockerContainerList(w http.ResponseWriter, r *http.Request) {
	ep, ok := a.dockerEndpoint(w, r)
	if !ok {
		return
	}
	all := true
	if v := strings.TrimSpace(r.URL.Query().Get("all")); v != "" {
		parsed, err := strconv.ParseBool(v)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "all 参数只接受 true / false"})
			return
		}
		all = parsed
	}
	res, err := dockerproxy.QueryContainers(r.Context(), ep, all)
	a.writeDockerResult(w, ep, res, err)
}

// dockerIDFromPath 取出并校验路径里的容器 / 镜像 ID。
//
// 校验不是形式主义：这个值会被拼进 daemon 的 URL 路径，放斜杠进去就等于开了一条
// 改变请求路径的口子（与 OpsClusterPattern 同一个理由）。model 里的
// OpsDockerIDPattern 就是为此定的，直接复用而不是在这里另写一套。
func dockerIDFromPath(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := strings.TrimSpace(r.PathValue("id"))
	if !model.OpsDockerIDPattern.MatchString(id) {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "容器 / 镜像标识不合法（只允许字母、数字与 . _ -，且不能含路径分隔符）",
		})
		return "", false
	}
	return id, true
}

// GET /api/v1/container/docker/containers/{id}?node=&name= —— 容器详情（白名单投影）。
func (a *API) handleDockerInspect(w http.ResponseWriter, r *http.Request) {
	ep, ok := a.dockerEndpoint(w, r)
	if !ok {
		return
	}
	id, ok := dockerIDFromPath(w, r)
	if !ok {
		return
	}
	res, err := dockerproxy.QueryInspect(r.Context(), ep, id)
	a.writeDockerResult(w, ep, res, err)
}

// GET /api/v1/container/docker/containers/{id}/logs?node=&name=&tail=&since=
func (a *API) handleDockerLogs(w http.ResponseWriter, r *http.Request) {
	ep, ok := a.dockerEndpoint(w, r)
	if !ok {
		return
	}
	id, ok := dockerIDFromPath(w, r)
	if !ok {
		return
	}
	tail, ok := dockerIntParam(w, r, "tail", model.OpsLogDefaultTailLines, 1, model.OpsLogMaxTailLines)
	if !ok {
		return
	}
	since, ok := dockerIntParam(w, r, "since", 0, 0, model.OpsLogMaxSinceSeconds)
	if !ok {
		return
	}
	res, err := dockerproxy.QueryLogs(r.Context(), ep, id, tail, since)
	a.writeDockerResult(w, ep, res, err)
}

// GET /api/v1/container/docker/images?node=&name= —— 镜像列表。
func (a *API) handleDockerImages(w http.ResponseWriter, r *http.Request) {
	ep, ok := a.dockerEndpoint(w, r)
	if !ok {
		return
	}
	res, err := dockerproxy.QueryImages(r.Context(), ep)
	a.writeDockerResult(w, ep, res, err)
}

// dockerIntParam 读一个整数查询参数并做上下限校验。
//
// 越界**报错而不是静默夹取**：用户要 5000 行日志、平台悄悄给 500 行，
// 会让人以为"日志就这么多"。显式拒绝才能让上限被看见。
func dockerIntParam(w http.ResponseWriter, r *http.Request, key string, def, min, max int) (int, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return def, true
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": key + " 必须是整数"})
		return 0, false
	}
	if v < min || v > max {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": key + " 超出允许范围（" + strconv.Itoa(min) + " ~ " + strconv.Itoa(max) + "）",
		})
		return 0, false
	}
	return v, true
}

// ---- 写操作（docker:write，仅超管）----

// dockerBoolParam 读一个布尔查询参数（缺省为 def）。
func dockerBoolParam(r *http.Request, key string, def bool) (bool, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return def, true
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return false, false
	}
	return v, true
}

// handleDockerContainerStart 启动容器。
func (a *API) handleDockerContainerStart(w http.ResponseWriter, r *http.Request) {
	a.dockerLifecycle(w, r, "start", "启动容器")
}

// handleDockerContainerStop 停止容器。
func (a *API) handleDockerContainerStop(w http.ResponseWriter, r *http.Request) {
	a.dockerLifecycle(w, r, "stop", "停止容器")
}

// handleDockerContainerRestart 重启容器。
func (a *API) handleDockerContainerRestart(w http.ResponseWriter, r *http.Request) {
	a.dockerLifecycle(w, r, "restart", "重启容器")
}

// dockerLifecycle 是 start / stop / restart 的接口层共同实现。
//
// timeout 只对 stop / restart 有意义（SIGKILL 前的宽限期）：start 传了也不该被接受，
// 那会让"我设了 30 秒"变成一个没有效果的参数——参数被静默忽略比报错更坏。
func (a *API) dockerLifecycle(w http.ResponseWriter, r *http.Request, action, label string) {
	ep, ok := a.dockerEndpoint(w, r)
	if !ok {
		return
	}
	id, ok := dockerIDFromPath(w, r)
	if !ok {
		return
	}
	timeout := 0
	if action != "start" {
		if timeout, ok = dockerIntParam(w, r, "timeout", dockerDefaultStopTimeout, 0, dockerMaxStopTimeout); !ok {
			return
		}
	} else if strings.TrimSpace(r.URL.Query().Get("timeout")) != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "启动容器不支持 timeout 参数"})
		return
	}

	var (
		res *model.ContainerQueryResult
		err error
	)
	switch action {
	case "start":
		res, err = dockerproxy.Start(r.Context(), ep, id)
	case "stop":
		res, err = dockerproxy.Stop(r.Context(), ep, id, timeout)
	default:
		res, err = dockerproxy.Restart(r.Context(), ep, id, timeout)
	}

	a.recordDockerWrite(r, ep, action, label+" "+id, err)
	a.writeDockerResult(w, ep, res, err)
}

// DELETE /api/v1/container/docker/containers/{id}?node=&name=&force=
//
// **不删卷**（dockerproxy.RemoveContainer 刻意不带 v=1）：卷里是数据，删掉不可逆。
func (a *API) handleDockerContainerRemove(w http.ResponseWriter, r *http.Request) {
	ep, ok := a.dockerEndpoint(w, r)
	if !ok {
		return
	}
	id, ok := dockerIDFromPath(w, r)
	if !ok {
		return
	}
	force, valid := dockerBoolParam(r, "force", false)
	if !valid {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "force 参数只接受 true / false"})
		return
	}
	res, err := dockerproxy.RemoveContainer(r.Context(), ep, id, force)
	a.recordDockerWrite(r, ep, "rm", "删除容器 "+id+"（未删卷）", err)
	a.writeDockerResult(w, ep, res, err)
}

// DELETE /api/v1/container/docker/images/{id}?node=&name=&force=
func (a *API) handleDockerImageRemove(w http.ResponseWriter, r *http.Request) {
	ep, ok := a.dockerEndpoint(w, r)
	if !ok {
		return
	}
	id, ok := dockerIDFromPath(w, r)
	if !ok {
		return
	}
	force, valid := dockerBoolParam(r, "force", false)
	if !valid {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "force 参数只接受 true / false"})
		return
	}
	res, err := dockerproxy.RemoveImage(r.Context(), ep, id, force)
	a.recordDockerWrite(r, ep, "image.rm", "删除镜像 "+id, err)
	a.writeDockerResult(w, ep, res, err)
}
