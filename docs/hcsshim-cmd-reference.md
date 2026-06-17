# hcsshim/cmd 目录参考

hcsshim 仓库中 `cmd/` 目录下每个工具的用途说明。
每个子目录都是一个独立的可执行文件（package main）。

## 目录总览

```text
hcsshim/cmd/
├── runhcs/                        ← OCI 运行时（最核心）
├── containerd-shim-runhcs-v1/     ← containerd 的 Windows shim
├── containerd-shim-lcow-v2/       ← containerd 的 Linux 容器 shim
├── wclayer/                       ← 层管理工具
├── gcs/                           ← Guest Compute Service
├── gcs-sidecar/                   ← GCS sidecar
├── gcstools/                      ← GCS 工具集
├── hooks/                         ← 容器钩子
├── ncproxy/                       ← 网络代理
├── shimdiag/                      ← shim 诊断工具
├── jobobject-util/                ← Job Object 工具
├── device-util/                   ← 设备工具
├── mkuvmcim/                      ← UVM CIM 创建工具
└── tar2ext4/                      ← tar 转 ext4 工具
```

## 与 win-sandbox 相关的工具

### runhcs（OCI 运行时）

最核心的工具，实现了 OCI 运行时规范。Docker/containerd 通过它来管理 Windows 容器。

关键文件：

| 文件 | 作用 |
| --- | --- |
| `container.go` | 容器生命周期管理（创建、启动、删除、状态查询） |
| `create.go` | `create` 子命令，从 OCI spec 创建容器 |
| `start.go` | `start` 子命令，启动已创建的容器 |
| `exec.go` | `exec` 子命令，在运行中的容器里执行命令 |
| `delete.go` | `delete` 子命令，删除容器 |
| `kill.go` | `kill` 子命令，终止容器 |
| `list.go` | `list` 子命令，列出所有容器 |
| `state.go` | `state` 子命令，查询容器状态 |
| `vm.go` | Utility VM 管理（Hyper-V 隔离用） |
| `spec.go` | OCI spec 解析 |
| `run.go` | `run` 子命令，创建+启动+执行一步完成 |

核心函数（`container.go:507`）：

```go
func createContainerInHost(c *container, vm *uvm.UtilityVM) error {
    opts := &hcsoci.CreateOptions{
        ID:            c.ID,
        Spec:          c.Spec,
        HostingSystem: vm,  // nil = 进程隔离, 非nil = Hyper-V
    }
    hc, r, err := hcsoci.CreateContainer(context.Background(), opts)
    // ...
}
```

这就是 hcsoci.CreateContainer 的实际用法，Docker 在 Windows 上创建容器最终调的也是这个。

### wclayer（层管理工具）

用于管理 Windows 容器镜像层的命令行工具。

关键文件：

| 文件 | 作用 |
| --- | --- |
| `create.go` | 创建新层 |
| `mount.go` | 挂载层（ActivateLayer → PrepareLayer → GetLayerMountPath） |
| `remove.go` | 删除层 |
| `export.go` | 导出层 |
| `import.go` | 导入层 |
| `makebaselayer.go` | 创建基础层 |

与 win-sandbox 的关系：我们手动调用的 `hcsshim.ActivateLayer`、`PrepareLayer`、`GetLayerMountPath` 就是这个工具的底层 API。

### containerd-shim-runhcs-v1（containerd shim）

containerd 在 Windows 上的 shim 进程。containerd 通过它来调用 hcsshim 管理容器。

关键文件：

| 文件 | 作用 |
| --- | --- |
| `task_hcs.go` | HCS 容器任务管理 |
| `exec_hcs.go` | 在容器中执行命令 |
| `pod.go` | Pod 管理（Kubernetes 场景） |
| `service.go` | shim 服务入口 |

与 win-sandbox 的关系：如果你以后想用 containerd 而不是直接调 hcsshim，这就是中间层。

## 与 win-sandbox 无关的工具

### gcs（Guest Compute Service）

运行在 Utility VM 内部的守护进程，负责：
- 接收宿主机的请求（创建进程、挂载目录等）
- 管理容器在 VM 内部的生命周期
- 通过 vsock 与宿主机通信

不需要关心，这是 Hyper-V 隔离的内部实现。

### gcs-sidecar

GCS 的 sidecar 容器，用于在 UVM 内部运行辅助任务。

### gcstools

GCS 工具集，包含：
- `generichook` — 通用钩子执行器
- `install-drivers` — 驱动安装器

### hooks/wait-paths

容器钩子，等待指定路径出现后执行操作。

### ncproxy（网络代理）

Kubernetes 网络代理，负责：
- 管理 HNS 网络
- 配置容器网络 endpoint
- 与 Kubernetes CNI 插件交互

不需要关心，除非你要做 Kubernetes 集成。

### shimdiag（shim 诊断工具）

诊断 containerd shim 的工具，可以：
- 列出运行中的任务
- 在 shim 中执行命令
- 获取 shim 的堆栈信息

### jobobject-util

Windows Job Object 管理工具。Job Object 是 Windows 的进程组管理机制，容器用它来限制资源。

### device-util

设备管理工具，用于 PCI 设备直通（GPU 等）。

### mkuvmcim

创建 UVM CIM（Container Image Management）文件的工具。CIM 是一种新的容器镜像格式。

### tar2ext4

将 tar 文件转换为 ext4 文件系统的工具。用于 Linux 容器 on Windows (LCOW) 场景。

## 阅读建议

如果你想理解 Windows 容器的完整流程：

```text
1. cmd/runhcs/create.go        ← 容器创建入口
2. cmd/runhcs/container.go     ← 容器生命周期管理
3. cmd/runhcs/vm.go            ← Hyper-V 隔离的 UVM 管理
4. cmd/wclayer/mount.go        ← 层挂载流程
```

如果你想理解 containerd 如何调用 hcsshim：

```text
1. cmd/containerd-shim-runhcs-v1/service.go  ← shim 服务入口
2. cmd/containerd-shim-runhcs-v1/task_hcs.go ← 任务管理
```

---

*基于 hcsshim v0.14.1 源码*
