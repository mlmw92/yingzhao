# 容器与工作负载管理（Docker / K8s）部署前提

「容器与工作负载」页面的能力分两条通路：

| 能力 | 执行位置 | 需要的凭据 |
|---|---|---|
| Docker 管理面（列表 / 详情 / 日志 / 镜像 / 启停删） | **Server 直连 Docker daemon** | daemon 可访问（同机 unix socket，或暴露为 `tcp://`） |
| K8s 对象查询与**写操作**（重启工作负载 / 扩缩容 / 删除 Pod） | **Server 直连 apiserver** | `k8s-live.yaml` 里的集群凭据 |

> **Agent 只负责采集**：它上报指标、日志与安全事件，以及 Docker 端点/集群清单；
> 不执行任何容器写操作，也不接触集群写凭据。

---

## 1. Docker 管理面

- 端点清单来自 Agent 上报（`dockerInstances` / `DockerEndpoints`），Server 不另配。
- **同机部署**：unix socket（`unix:///var/run/docker.sock`）即可。
- **跨机部署**：Server 连不上别的机器上的 unix socket，需要把 daemon 暴露为
  `tcp://<host>:2375`（务必配合防火墙或 TLS，daemon 端口等于 root 权限）。
- 写操作（启停删）的权限点是 `docker:write`（仅超管）；删除容器**不删卷**。

> 实测（2026-10-23）：同机 unix socket 下，列表 / 详情 / 日志 / 镜像与
> stop / start / restart / delete 全部正常。停止容器默认给 10 秒宽限期（与 CLI 一致）。

---

## 2. K8s 排障写操作

三个动作：`container.restart`（滚动重启）、`container.scale`（改副本数）、
`container.delete`（删 Pod，控制器会重建）。

**执行点在 Server**：接口同步调用 apiserver，点一次就是一次真实请求，失败当场可见
（2026-10-23 起不再经过 Agent 下行通道）。

### 2.1 集群凭据（`k8s-live.yaml`）

Server 用这份文件里的凭据直连 apiserver。**只读凭据不够**——写操作需要对应权限，
否则 apiserver 返回 403，错误会原样带回界面。

最小权限（按命名空间授权；也可以换成 ClusterRole 覆盖多个命名空间）：

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

`k8s-live.yaml` 的写法（`apiServer` + `kubeconfig` 或 `token`）：

```yaml
clusters:
  - name: prod-k8s
    apiServer: "https://10.0.0.10:6443"
    kubeconfig: "/etc/monitor-server/k8s-prod.kubeconfig"
    insecureTLS: false
```

> `insecureTLS: true` 仅用于自签证书排障；生产建议在 kubeconfig 里用
> `certificate-authority` 指向集群 CA。

### 2.2 平台侧权限与审批

- 三个动作要求 `container:write`（**仅超管**）。
- 开启高风险审批时，它们属于写类动作，会先进入双人复核；批准后由 Server 执行。
- 每次写操作写一条审计（分类 `container`）。

### 2.3 已废弃：Agent 侧开关

早期版本需要 Agent 的 `guards.ops.containerWrite: true` 和可写的 kubeconfig。
**现在不再需要**——Agent 不再执行容器写操作；旧配置里的 `containerWrite` 键会被忽略。
Agent 的 kubeconfig 可以保持只读（它只用于采集集群清单）。

---

## 3. 实测记录（2026-10-23，dev-server / k3s v1.36）

- `container.scale`：1 → 2 副本成功；`container.restart`：Deployment 打上
  `kubectl.kubernetes.io/restartedAt`；`container.delete`：Pod 删除后由控制器重建；
- 首次扩容失败（403）暴露了 `/scale` 子资源权限要求，补上后通过；
- 以上验证后架构调整为 Server 直连，Agent 侧写实现已删除；
  接口层用例覆盖权限、参数白名单与请求形状。
