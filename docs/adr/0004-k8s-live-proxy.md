# K8s 实时清单由 Server 侧凭据代理（修订 ADR-0003 的凭据边界）

Status: accepted

平台定位升级为**运维平台**（不只监控）后，资源清单需要「点开即得」的实时态——ADR-0003 的异步下发延迟等于一个上报周期（默认 15s），不满足管理面的要求。决定：允许把 K8s 凭据配置在 Server 侧（独立文件 `<DataDir>/k8s-live.yaml`，权限 0600，由运维手工维护，**不经任何 API 传输或回显**），Server 提供只读 apiserver 代理（白名单：pods / deployments / events / nodes，支持 namespace 过滤），权限门沿用 `container:read`。

## Considered Options

- **维持 ADR-0003 纯异步作为唯一通路**：清单实时性不足（延迟=上报周期），管理面不可用 → 与运维平台定位冲突，拒绝（作为唯一通路）。
- **Server 直连 apiserver（本 ADR，选定）**：凭据上移到 Server；代理只读白名单；RBAC 沿用 `container:read`。
- **集成 KubeSphere / Rancher**：重量级平台（自身需资源与运维），与「内网离线 + 轻量单二进制」冲突（同 ADR-0003 的结论）→ 拒绝。

## Consequences

- **凭据边界修订**：K8s 凭据现在可能存在于两处——Agent 本地（ADR-0003 异步通路）与 Server（本 ADR 实时通路）。两侧都是 0600 文件、运维手工维护；Server 侧凭据不经任何 API 传输或回显。
- **指标通路不变**：apiserver 没有时序数据（metrics-server 仅内存 1-2 分钟窗口），历史趋势与告警只能靠采集入库。本 ADR 不改变指标通路。
- **两条通路并存**：网闸 / Server 不可达 apiserver 的场景走异步任务；Server→apiserver 可达的场景用实时代理。前端按通路分区展示（上报数据 = 历史/趋势；live 接口 = 当前态）。
- **实时代理只读**：白名单不含任何写动词；写操作（重启/扩缩容/终端）后续单独走审批，ADR-0003 的四道护栏原则沿用。
- **配置即时生效**：凭据文件每次查询时读取，修改无需重启；文件不存在时实时接口返回 501（不静默空数据），其余功能不受影响。
