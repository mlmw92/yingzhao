# 容器与工作负载管理（Docker / K8s）部署前提

「容器与工作负载」页面的能力分两条通路：

| 能力 | 执行位置 | 需要的凭据 |
|---|---|---|
| Docker 管理面（列表 / 详情 / 日志 / 镜像 / 启停删） | **Server 直连 Docker daemon** | daemon 可访问（同机 unix socket，或暴露为 `tcp://`） |
| K8s 对象级查询（工作负载 / Pod / 事件） | **Server 直连 apiserver** | `k8s-live.yaml` 里的集群凭据（只读） |
| K8s 写操作（重启工作负载 / 扩缩容 / 删除 Pod） | **Agent 本地执行** | Agent 的 kubeconfig + `guards.ops.containerWrite` |

---

## 1. Docker 管理面

- 端点清单来自 Agent 上报（`dockerInstances` / `DockerEndpoints`），Server 不另配。
- **同机部署**：unix socket（`unix:///var/run/docker.sock`）即可。
- **跨机部署**：Server 连不上别的机器上的 unix socket，需要把 daemon 暴露为
  `tcp://<host>:2375`（务必配合防火墙或 TLS，daemon 端口等于 root 权限）。
- 写操作（启停删）的权限点是 `docker:write`（仅超管）；删除容器**不删卷**。

> 实测（2026-10-23）：同机 unix socket 下，列表 / 详情 / 日志 / 镜像与
> stop / start / restart / delete 全部正常。

---

## 2. K8s 排障写操作

三个动作：`container.restart`（滚动重启）、`container.scale`（改副本数）、
`container.delete`（删 Pod，控制器会重建）。它们**走 Agent 下行通道**：
集群凭据只存 Agent 本地、从不上报；本机护栏决定这台机器同不同意被改。

### 2.1 Agent 显式同意

```yaml
# /etc/monitor-agent/agent.yaml
guards:
  ops:
    containerWrite: true
```

重启 Agent 后，节点上报的能力清单里会出现
`container.restart / container.scale / container.delete`；
未开启时平台不会向该节点下发这三类任务（能力协商如实显示"不支持"）。

### 2.2 kubeconfig 权限（最小集）

只读采集用的 `k8s-readonly.kubeconfig` **不够**，写操作会以 403 失败。
按命名空间授权的最小 Role：

```yaml
apiVersion: v1
kind: ServiceAccount
metadata: {name: yingzhao-ops, namespace: <业务命名空间>}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata: {name: yingzhao-ops, namespace: <业务命名空间>}
rules:
- apiGroups: ["apps"]
  resources: ["deployments", "statefulsets", "daemonsets"]
  verbs: ["get", "list", "patch", "update"]
# 扩缩容走 /scale 子资源，**必须单独授权**，否则 container.scale 报 403
- apiGroups: ["apps"]
  resources: ["deployments/scale", "statefulsets/scale"]
  verbs: ["get", "patch", "update"]
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get", "list", "delete"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata: {name: yingzhao-ops, namespace: <业务命名空间>}
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: Role, name: yingzhao-ops}
subjects: [{kind: ServiceAccount, name: yingzhao-ops, namespace: <业务命名空间>}]
```

kubeconfig 指向该 ServiceAccount 的 token：

```yaml
apiVersion: v1
kind: Config
clusters:
- name: k3s
  cluster:
    server: "https://<apiserver>:6443"
    certificate-authority: /var/lib/rancher/k3s/server/tls/server-ca.crt
contexts:
- name: k3s
  context: {cluster: k3s, user: yingzhao-ops, namespace: <业务命名空间>}
current-context: k3s
users:
- name: yingzhao-ops
  user: {token: "<kubectl -n <ns> create token yingzhao-ops 的输出>"}
```

> `insecure-skip-tls-verify: true` 在 Agent 的客户端里**不生效**，必须用
> `certificate-authority` 指向集群 CA，否则报
> `x509: certificate signed by unknown authority`。

### 2.3 只读采集的额外权限

Agent 采集集群清单还需要集群级读取。内置 `view` ClusterRole 不含 `nodes`，
再补一条即可：

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata: {name: yingzhao-nodes}
rules:
- apiGroups: [""]
  resources: ["nodes"]
  verbs: ["get", "list"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata: {name: yingzhao-nodes}
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: ClusterRole, name: yingzhao-nodes}
subjects: [{kind: ServiceAccount, name: yingzhao-ops, namespace: <业务命名空间>}]
```

### 2.4 平台侧权限

三个动作除入口的 `ops:exec` 外，还要求 `container:write`（**仅超管**）。
审批开启时它们属于写类动作，会先进入双人复核。

---

## 3. 实测记录（2026-10-23，dev-server / k3s v1.36）

- `container.scale`：1 → 2 副本成功，任务状态 `succeeded` 并带结构化结果；
- `container.restart`：Deployment 打上 `kubectl.kubernetes.io/restartedAt` 注解；
- `container.delete`：目标 Pod 删除后由控制器重建；
- **首次扩容失败（403）暴露了 `/scale` 子资源权限要求**，补上后通过；
- 验证后已恢复 Agent 配置并删除测试命名空间。
