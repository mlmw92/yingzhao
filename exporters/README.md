# exporters

平台配套的中间件 exporter 源码。每个 exporter 是**独立的可执行程序**，不编译进 Agent，也不随 Server 升级；由用户按需部署在需要采集中间件的节点上。

## 为什么单独放

中间件的指标契约由平台定义，但社区 exporter 的命名并不统一（仅 FastDFS 就有三套以上命名，且没有一个和平台契约对得上）。把 exporter 收在这里，可以：

- 保证指标名、单位与平台的采集契约一致，不存在「装了 exporter 页面还是空」的漂移；
- 支持裸机与容器两种部署形态（在 exporter 内部处理，不进 Agent）；
- 按需部署：没有中间件的节点不需要装任何东西。

平台仍然支持 `exporterURL` 指向任意第三方 exporter；本目录只是提供一套「开箱即用」的实现。

## 目录约定

```
exporters/
├── README.md
└── <middleware>-exporter/
    ├── main.go          # 可执行程序
    ├── main_test.go     # 解析与指标契约的用例
    └── README.md        # 部署方式、配置项、指标清单
```

新增一个 exporter 时：

1. 指标名遵循平台采集器的契约（见对应 `internal/agent/collector/<mw>.go`）；
2. 只依赖标准库，保证离线包可交叉编译；
3. 在 `build/cross-compile.sh` 里加一条交叉编译，并在 `build/release.sh` 的 full 包里带上产物；
4. 补 `README.md`，写清裸机与容器两种启动方式。

## 现有 exporter

| 目录 | 用途 |
|---|---|
| [fastdfs-exporter](fastdfs-exporter/) | FastDFS 空间 / Storage 状态，支持本机 `fdfs_monitor` 与 `docker exec` 两种模式 |
