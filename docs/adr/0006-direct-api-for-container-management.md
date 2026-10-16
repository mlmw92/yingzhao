# 容器与工作负载的读改走 Server 直连，Docker 读写都直连 daemon

Status: accepted

日期：2026-10-10（取代 `0005-docker-management-channel.md`）

「容器与工作负载」是**管理面**，它给出的清单要能直接支撑「看一眼再动手」。ADR-0004 已经为 K8s 定下这条原则——「异步下发的延迟等于一个上报周期，**不满足管理面的要求**」——并为此在 Server 侧建了 `k8sproxy`。ADR-0005 却把 Docker 判成「不存在 Server 直连等价通路」，于是把它的全部能力（含只读列表）压进 Agent 异步下行。

**这个判断不成立**：`model.DockerInstanceConfig.Addr` 从一开始就允许 `tcp://host:2375`（网络可达），与 K8s 的 apiserver 地址没有本质差别；而 ADR-0005 拒绝 Server 直连的理由是「那等于把『能起容器』的能力搬到中心」，可它同时接受了 Server 侧持有 K8s 集群凭据——后者的权限大得多。同一条原则、两套结论，不一致的是 ADR-0005。

决定：

1. **Docker 的读与写都改为 Server 直连 daemon**（新增 `internal/server/dockerproxy/`）。端点清单**复用 Agent 上报**（新增 `ReportPayload.DockerEndpoints`，含 daemon 地址）——运维不必在 Server 上另配一份，两份配置必然会对不上。
2. **K8s 的读只留一条通路**：删除 5 个只读下行动作（`container.workloads` / `pods` / `describe` / `events` / `logs`），全部改由 `k8sproxy` 同步查询。K8s 的**写**仍走异步下行（要逐机护栏与 Agent 本地的集群凭据）。
3. **`guards.ops.docker{read,write}` 与 `guards.ops.container` 一并删除**：直连之后 Agent 侧没有执行点，本机护栏无处安放。留着它们是**空开关**——运维会以为自己挡住了一件其实挡不住的事，那比没有更坏。
4. **Docker 写操作的门控从四道减为三道**：`docker:write`（仅超管）+ 界面二次确认 + 审计。

## Considered Options

- **维持 ADR-0005（Docker 全走异步下行）**：与 ADR-0004 的原则自相矛盾，且用户 2026-10-10 明确要求「调接口」→ 拒绝。
- **只把 Docker 读改直连、写仍走异步（对称 K8s）**：能保住逐机护栏，但写操作仍要等一个上报周期，而「停一个跑飞的容器」恰恰是最需要立刻生效的动作。用户明确选了「读写都直连」→ 拒绝。
- **Docker 端点由运维在 Server 上另配一份（对称 `k8s-live.yaml`）**：daemon 地址不是凭据，另配一份只会让运维维护两处、且必然对不上 → 拒绝，改为复用上报。
- **Docker 读写都 Server 直连 + 端点复用上报 + 删掉本机护栏（本 ADR，选定）**。

## Consequences

- **Server 侧因此持有「能启停容器」的能力**：这是本决策的代价，写在明处——中心被攻破即全网容器可被启停。用户 2026-10-10 知情并选择了实时性。剩下的门控是权限点（仅超管）+ 二次确认 + 审计。
- **daemon 地址离开 Agent 本机**（随上报进入 Server）：代价是地址不再只存在于被监控机器上。边界是**它只在 Server 内部用于直连、不进任何响应体**——`GET /container/docker/endpoints` 只返回 node / name / reachable / unixSocket。
- **跨机器的 unix socket 连不上**：这是**部署形态**问题、不是故障。Server 必须给出可执行的建议（把 daemon 暴露为 `tcp://` 或前面挂 socket 代理），而不是把 `connect: no such file or directory` 原样抛给用户。
- **15 个动作被删除**，动作目录里不再有「容器」「Docker」「Docker 操作」三个分组；新增守卫（`catalog_removed_actions_test.go`）防止它们回来——同一个页面两条数据通路（一条实时、一条延迟一个上报周期）而使用者分不出来，这种退化不会报错。
- **Agent 侧少了一整块执行能力**：`internal/agent/ops/docker.go`、`internal/agent/collector/docker_query.go` 删除，`k8s_query.go` 瘦身为 `k8s_common.go`。旧 Agent 仍会上报 `docker.*` 能力，Server 侧已不认这些动作，**不会因此出错**（只是能力清单里多几条用不上的）。
- **投影仍在服务端**：Docker 的 inspect 与 K8s 的对象详情都是**白名单投影**而非原始 JSON。这一条与通路无关，但必须跟着搬——下放给浏览器就等于数据已经到过浏览器了。
- **指标通路不受影响**：中间件监控的 K8s / Docker Tab 仍读 Agent 采集入库的时序数据（ADR-0004 早已说明 apiserver 与 daemon 本身没有时序数据）。
