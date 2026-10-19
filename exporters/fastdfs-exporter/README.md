# fastdfs-exporter

把 FastDFS 的集群状态暴露为 Prometheus 指标，供平台按 `exporterURL` 抓取。

数据来自 FastDFS 自带的 `fdfs_monitor`，支持两种运行形态：

- **local**：exporter 与 FastDFS 同机，直接执行宿主机的 `fdfs_monitor`（裸机部署）。
- **docker**：FastDFS 跑在容器里，通过 `docker exec <container> fdfs_monitor ...` 采集。

指标名与平台的采集契约（`internal/agent/collector/fastdfs.go`）一致，单位统一为字节。

## 指标

| 指标 | 说明 |
|---|---|
| `fastdfs_up` | 采集成功为 1；`fdfs_monitor` 失败或输出无法识别时为 0 |
| `fastdfs_group_count` | group 数 |
| `fastdfs_storage_count` | storage 节点总数 |
| `fastdfs_storage_online_count` | 在线 storage 数 |
| `fastdfs_storage_offline_count` | 离线 storage 数 |
| `fastdfs_total_space` / `fastdfs_free_space` / `fastdfs_used_space` | 总/空闲/已用空间（字节） |
| `fastdfs_trunk_free_space` | trunk 空闲空间（字节） |

`fdfs_monitor` 不提供磁盘 IO 与网络累计值，因此本 exporter **刻意不输出**这两类指标——上报 0 会让人误以为「没有流量」。

## 启动参数

| 参数 | 默认值 | 说明 |
|---|---|---|
| `-listen` | `127.0.0.1:9110` | 监听地址 |
| `-mode` | `local` | `local` / `docker` |
| `-client-conf` | `/etc/fdfs/client.conf` | client.conf 路径（docker 模式下是容器内路径） |
| `-container` | `mw-fastdfs` | docker 模式下的容器名 |
| `-monitor-bin` | `fdfs_monitor` | 可执行文件名或绝对路径 |
| `-cache` | `10s` | 采集结果缓存时长（避免每次抓取都起进程） |
| `-timeout` | `15s` | 单次采集超时 |

## 裸机部署

```bash
sudo install -m 0755 fastdfs-exporter /usr/local/bin/fastdfs-exporter

sudo tee /etc/systemd/system/fastdfs-exporter.service >/dev/null <<'EOF'
[Unit]
Description=FastDFS exporter (yingzhao)
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/fastdfs-exporter -mode local -client-conf /etc/fdfs/client.conf
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable --now fastdfs-exporter
curl -s http://127.0.0.1:9110/metrics
```

## 容器部署

FastDFS 在容器里时，宿主机通常没有 `fdfs_monitor`，用 docker 模式：

```ini
[Unit]
Description=FastDFS exporter (yingzhao)
After=docker.service
Requires=docker.service

[Service]
Type=simple
ExecStart=/usr/local/bin/fastdfs-exporter -mode docker -container mw-fastdfs -client-conf /etc/fdfs/client.conf
Restart=always
RestartSec=5
User=root

[Install]
WantedBy=multi-user.target
```

> docker 模式需要读取 Docker socket 的权限（root 或 docker 组）。

## 接入平台

在 `/etc/monitor-agent/agent.yaml` 的 storage 实例上填 `exporterURL`：

```yaml
fastdfsInstances:
  - name: "storage-1"
    role: "storage"
    addr: "10.0.0.21:23000"
    group: "group1"
    exporterURL: "http://127.0.0.1:9110/metrics"
```

**只在一个实例上配置**：exporter 返回的是整个 group 的总量，挂到多个实例上会让首页 KPI 按实例重复累加。

## 构建

```bash
go build -o fastdfs-exporter ./exporters/fastdfs-exporter
```

交叉编译已接入 `build/cross-compile.sh`，产物在 `dist/artifacts/bin/exporters/linux/<arch>/`。
