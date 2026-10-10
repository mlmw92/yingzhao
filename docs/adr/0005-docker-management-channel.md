# Docker 管理面只走 Agent 异步下行，且写操作另设本机护栏

Status: proposed

把「容器与工作负载」从「只有 K8s 只读视图」扩为覆盖 K8s 与 Docker 的**管理面**时，Docker 没有等价于 ADR-0004 的 Server 直连通路：Docker socket / daemon 地址（`model.DockerInstanceConfig.Addr`，形如 `unix:///var/run/docker.sock` 或 `tcp://host:2375`）只在 Agent 本机或可达网段，Server 直连要求把 socket 暴露给中心。决定：**Docker 的全部能力（含只读列表）只走 ADR-0003 的统一下行通道**，接受「延迟约一个上报周期」；**不新增 Server 侧 docker 代理**。同时，Docker 写操作（启停 / 重启 / 删除容器、删除镜像）**不复用 `guards.ops.container`，另设 `guards.ops.docker{read,write}`**——与 `file.push` 不复用 `ops.write/units` 同一个取向：「愿意交出集群结构」与「愿意被启停容器」是两件不同的同意。

## Considered Options

- **Docker 也走 Server 直连（对称于 ADR-0004）**：需把 Docker socket 或 daemon 端口暴露/配置到 Server，等于把「能起容器」的能力搬到中心，逐机护栏被架空（中心被攻破即全网容器可被启停）→ 拒绝。
- **Docker 只做指标展示，管理面只覆盖 K8s（维持现状）**：用户 2026-10-10 明确要求容器与工作负载同时管理 K8s 与 Docker → 拒绝。
- **Docker 全部走 Agent 异步下行 + 独立写护栏（本 ADR，选定）**：复用既有协议、协商、回执、超时作废与审计链路，新增的只是动作目录项与本机护栏字段。

## Consequences

- **只读也是异步的**：Docker 容器列表/详情/日志都需要一次下发往返，界面必须显式给出「上次刷新」时间并支持手动刷新，不能假装实时。K8s 侧则保留「只读实时（ADR-0004 代理）+ 写操作异步（ADR-0003）」的分工。
- **能力协商天然生效**：未配 `dockerInstances` 或未放行 `guards.ops.docker` 的机器不声明 `docker.*` 能力，界面显示「该节点未开启」而不是下发注定失败的任务。
- **写操作默认关闭**：`guards.ops.docker.write` 默认 false，写动作在 Agent 侧被拒并回执指向 `agent.yaml`；权限点 `docker:write` 只是四道护栏之一。
- **删容器不删卷**：`docker.rm` 不提供 `-v`（删卷不可逆且常是误操作）；`docker.image.rm` 默认不 `force`。
- **中间件 Docker Tab 与 Docker 管理面分工固化**：指标聚合（主机/容器/镜像计数、按主机拓扑）留在「中间件监控 → Docker」；容器级明细与操作收敛到「容器与工作负载」，见 `docs/superpowers/specs/2026-10-10-container-workload-management-design.md`。
