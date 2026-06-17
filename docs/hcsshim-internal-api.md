# hcsshim 内部高级 API 参考

hcsshim 仓库中 `internal/` 目录下的高级 API。
这些 API 不能被外部项目直接 import（Go 的 internal 包限制），但理解它们有助于：
- 知道 Docker/containerd 在 Windows 上怎么创建容器
- 以后如果 fork hcsshim 仓库可以直接使用
- 理解底层 API 的正确用法

## API 层级

```text
┌─────────────────────────────────────────────────────────────┐
│  Level 1: hcsoci.CreateContainer                            │
│  输入: OCI Spec                                              │
│  自动处理: 层挂载、配置组装、资源管理、网络                     │
│  位置: internal/hcsoci/create.go                             │
└───────────────────────────┬─────────────────────────────────┘
                            │
┌───────────────────────────▼─────────────────────────────────┐
│  Level 2: layers / uvm                                      │
│  layers.MountWCOWLayers()  → 挂载镜像层                      │
│  uvm.CreateWCOW()          → 创建 Utility VM                 │
│  位置: internal/layers/, internal/uvm/                       │
└───────────────────────────┬─────────────────────────────────┘
                            │
┌───────────────────────────▼─────────────────────────────────┐
│  Level 3: hcsshim.CreateContainer                           │
│  输入: ContainerConfig（手动组装）                            │
│  位置: hcsshim/container.go                                  │
└───────────────────────────┬─────────────────────────────────┘
                            │
┌───────────────────────────▼─────────────────────────────────┐
│  Level 4: vmcompute.dll                                     │
│  直接调 Windows 系统 DLL                                     │
│  位置: internal/vmcompute/vmcompute.go                       │
└─────────────────────────────────────────────────────────────┘
```

## Level 1: hcsoci.CreateContainer

**位置**: `internal/hcsoci/create.go:213`

最高级的 API，一个函数搞定容器创建。

### 函数签名

```go
func CreateContainer(ctx context.Context, createOptions *CreateOptions) (_ cow.Container, _ *resources.Resources, err error)
```

### CreateOptions 结构体

```go
type CreateOptions struct {
    // 容器 ID（可选，不填自动生成 GUID）
    ID string

    // 所有者标识（可选）
    Owner string

    // OCI 规范（必填）
    // 需要包含 Windows.LayerFolders 和 Root.Path
    Spec *specs.Spec

    // HCS Schema 版本（可选，自动选择）
    SchemaVersion *hcsschema.Version

    // Utility VM（Hyper-V 隔离时必填，进程隔离填 nil）
    HostingSystem *uvm.UtilityVM

    // 网络 namespace（可选）
    NetworkNamespace string

    // Windows 容器层（可选，也可通过 Spec.Windows.LayerFolders 指定）
    WCOWLayers layers.WCOWLayers

    // Linux 容器层（可选）
    LCOWLayers *layers.LCOWLayers

    // 调试选项：失败时不释放资源
    DoNotReleaseResourcesOnFailure bool

    // CPU 限制是否按 sandbox 缩放
    ScaleCPULimitsToSandbox bool
}
```

### 自动处理的事情

1. **层挂载**：调用 `layers.MountWCOWLayers` 激活层、准备层、获取 volume path
2. **配置组装**：自动判断 v1/v2 schema，构建 ContainerConfig 或 Container 文档
3. **scratch VHD**：自动创建和管理 scratch 虚拟硬盘
4. **网络配置**：自动创建网络 namespace 和 endpoint
5. **资源管理**：返回 `resources.Resources` 用于清理
6. **错误处理**：失败时自动释放已分配的资源

### 用法示例

```go
// 进程隔离容器
spec := &specs.Spec{
    Version: "1.0.0",
    Windows: &specs.Windows{
        LayerFolders: []string{
            `C:\layers\base`,     // 只读基础层
            `C:\scratch`,         // 可写 scratch 层
        },
    },
    Root: &specs.Root{
        Path: `C:\`,
    },
}

container, resources, err := hcsoci.CreateContainer(ctx, &hcsoci.CreateOptions{
    ID:   "my-container",
    Spec: spec,
    // HostingSystem: nil → 进程隔离
})

// Hyper-V 隔离容器
container, resources, err := hcsoci.CreateContainer(ctx, &hcsoci.CreateOptions{
    ID:            "my-container",
    Spec:          spec,
    HostingSystem: vm,  // Utility VM 实例
})
```

### OCI Spec 关键字段

```go
// 进程隔离
spec := &specs.Spec{
    Version: "1.0.0",
    Windows: &specs.Windows{
        LayerFolders: []string{
            baseLayerPath,   // 基础层
            scratchPath,     // scratch 层（最后一项）
        },
    },
    Root: &specs.Root{
        Path: volumeGUIDPath,  // GetLayerMountPath 返回的值
    },
}

// Hyper-V 隔离
spec := &specs.Spec{
    Version: "1.0.0",
    Windows: &specs.Windows{
        LayerFolders: []string{...},
        HyperV: &specs.WindowsHyperV{
            UtilityVMPath: `C:\...\UtilityVM`,
        },
    },
    Root: &specs.Root{...},
}
```

## Level 2: 层管理 (layers 包)

**位置**: `internal/layers/`

### MountWCOWLayers

挂载 Windows 容器镜像层，返回 volume path。

```go
func MountWCOWLayers(
    ctx context.Context,
    containerID string,
    vm *uvm.UtilityVM,     // nil = 进程隔离
    wl WCOWLayers,
) (_ *MountedWCOWLayers, _ resources.ResourceCloser, err error)
```

内部流程（从 hcsoci 源码学到）：

```text
1. ensureScratchVHD()     → 确保 scratch 目录和 sandbox.vhdx 存在
2. ActivateLayer(scratch) → 激活 scratch 层
3. PrepareLayer(scratch, [baseLayer]) → 准备 scratch 层
4. GetLayerMountPath(scratch) → 获取 volume GUID path
```

### WCOWLayers 结构

```go
type WCOWLayers interface {
    IsWCOWLayers()
}

// 实际使用的结构
type wcowWCIFSLayers struct {
    scratchLayerData
    layerPaths []string // 层路径列表（从顶到底）
}

type scratchLayerData struct {
    scratchLayerPath string // scratch 目录路径
}
```

### ParseWCOWLayers

从 OCI spec 的 LayerFolders 解析层信息。

```go
func ParseWCOWLayers(rootfs []*types.Mount, layerFolders []string) (WCOWLayers, error)
```

LayerFolders 顺序：`[层N(顶层), 层N-1, ..., 层1(基础), scratch]`

## Level 2: UVM 管理 (uvm 包)

**位置**: `internal/uvm/`

### 创建 Windows Utility VM

```go
// 创建默认选项
opts := uvm.NewDefaultOptionsWCOW("my-uvm", "")

// 自定义配置
opts.MemorySizeInMB = 2048
opts.ProcessorCount = 2
opts.BootFiles = bootFiles  // WCOWBootFiles

// 创建
vm, err := uvm.CreateWCOW(ctx, opts)

// 启动
err = vm.Start(ctx)

// 在 UVM 中创建容器
container, err := vm.CreateContainer(ctx, id, settings)

// 关闭
defer vm.Close()
```

### Options 结构

```go
type Options struct {
    ID                   string  // UVM ID
    Owner                string  // 所有者
    MemorySizeInMB       uint64  // 内存大小
    ProcessorCount       int32   // vCPU 数量
    ProcessorLimit       int32   // CPU 限制百分比
    ProcessorWeight      int32   // CPU 权重
    AllowOvercommit      bool    // 允许内存超额分配
    EnableDeferredCommit bool    // 延迟提交内存
}

type OptionsWCOW struct {
    *Options
    BootFiles *WCOWBootFiles  // UVM 启动文件
    NoDirectMap bool          // 不使用 VSMB 直接映射
}
```

### UVM 与容器的关系

```text
进程隔离（Argon）：
┌────────────┐  ┌────────────┐
│ 容器 A     │  │ 容器 B     │  ← 直接在宿主上
└────────────┘  └────────────┘

Hyper-V 隔离（Xenon）：
┌──────────────────────────────────────┐
│  Utility VM                          │
│  ┌────────────┐  ┌────────────┐     │
│  │ 容器 A     │  │ 容器 B     │     │
│  └────────────┘  └────────────┘     │
└──────────────────────────────────────┘
```

## Level 4: vmcompute.dll

**位置**: `internal/vmcompute/vmcompute.go`

最底层的 syscall 封装，详见 [vmcompute-api-reference.md](vmcompute-api-reference.md)。

```go
// 所有 HCS 操作最终都调这些函数
HcsCreateComputeSystem(id, jsonConfig, ...)
HcsStartComputeSystem(handle, ...)
HcsCreateProcess(handle, processParams, ...)
HcsShutdownComputeSystem(handle, ...)
HcsTerminateComputeSystem(handle, ...)
```

## 关键内部包索引

| 包 | 位置 | 作用 |
| --- | --- | --- |
| `hcsoci` | `internal/hcsoci/` | 最高级容器创建 API |
| `layers` | `internal/layers/` | 层挂载和管理 |
| `uvm` | `internal/uvm/` | Utility VM 管理 |
| `hcs` | `internal/hcs/` | HCS 状态管理 |
| `vmcompute` | `internal/vmcompute/` | DLL syscall |
| `wclayer` | `internal/wclayer/` | 层操作底层 API |
| `resources` | `internal/resources/` | 资源生命周期管理 |
| `oci` | `internal/oci/` | OCI spec 解析 |
| `hcs/schema1` | `internal/hcs/schema1/` | HCS v1 schema 定义 |
| `hcs/schema2` | `internal/hcs/schema2/` | HCS v2 schema 定义 |

## 为什么不能直接 import 这些包

Go 的 `internal` 包限制：只有同一模块内的代码才能 import `internal/` 下的包。

```go
// 可以（hcsshim 模块内部）
import "github.com/Microsoft/hcsshim/internal/hcsoci"

// 不可以（你的外部模块）
import "github.com/Microsoft/hcsshim/internal/hcsoci"  // 编译报错
```

解决方案：

1. 使用公开 API（`hcsshim.CreateContainer`）— 你当前的做法
2. Fork hcsshim 仓库 — 可以直接用 internal 包
3. 使用 OCI 运行时（runhcs）— 通过命令行调用

---

*基于 hcsshim v0.14.1 源码*
