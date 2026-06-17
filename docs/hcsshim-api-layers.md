# hcsshim API 分层参考

> hcsshim 库中可用的 API 层级，从高到低

---

## 目录

1. [API 层级总览](#1-api-层级总览)
2. [高级 API: hcsoci.CreateContainer](#2-高级-api-hcsocicreatecontainer)
3. [中级 API: 层管理](#3-中级-api-层管理)
4. [中级 API: UVM 管理](#4-中级-api-uvm-管理)
5. [底层 API: hcsshim.CreateContainer](#5-底层-api-hcsshimcreatecontainer)
6. [最底层: vmcompute.dll](#6-最底层-vmcomputedll)
7. [OCI Spec 构建指南](#7-oci-spec-构建指南)
8. [选择建议](#8-选择建议)

---

## 1. API 层级总览

```
┌─────────────────────────────────────────────────────────────┐
│  Level 1: hcsoci.CreateContainer                            │
│  输入: OCI Spec                                              │
│  自动处理: 层挂载、配置组装、资源管理、网络、scratch VHD       │
│  文件: internal/hcsoci/create.go                             │
└───────────────────────────┬─────────────────────────────────┘
                            │
┌───────────────────────────▼─────────────────────────────────┐
│  Level 2: layers / uvm                                      │
│  layers.MountWCOWLayers()  → 挂载镜像层                      │
│  uvm.CreateWCOW()          → 创建 Utility VM                 │
│  文件: internal/layers/, internal/uvm/                       │
└───────────────────────────┬─────────────────────────────────┘
                            │
┌───────────────────────────▼─────────────────────────────────┐
│  Level 3: hcsshim.CreateContainer                           │
│  输入: ContainerConfig（JSON 结构体）                         │
│  自己处理: 层路径、配置组装、scratch                         │
│  文件: hcsshim/container.go                                  │
└───────────────────────────┬─────────────────────────────────┘
                            │
┌───────────────────────────▼─────────────────────────────────┐
│  Level 4: vmcompute.dll syscall                             │
│  直接调 Windows DLL                                          │
│  文件: internal/vmcompute/vmcompute.go                       │
└─────────────────────────────────────────────────────────────┘
```

---

## 2. 高级 API: hcsoci.CreateContainer

**文件**: `internal/hcsoci/create.go:213`

### 功能

一个函数搞定容器创建。自动处理：
- 镜像层挂载（ActivateLayer → PrepareLayer → GetLayerMountPath）
- HCS 配置组装（自动判断 v1/v2 schema）
- 进程隔离 vs Hyper-V 隔离分支
- scratch VHD 创建和管理
- 网络 namespace 配置
- 资源清理（失败时自动释放）

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

### 用法示例

```go
import (
    "context"
    "github.com/Microsoft/hcsshim/internal/hcsoci"
    specs "github.com/opencontainers/runtime-spec/specs-go"
)

func createSandbox() error {
    ctx := context.Background()

    // 构建 OCI spec
    spec := &specs.Spec{
        Version: "1.0.0",
        Windows: &specs.Windows{
            LayerFolders: []string{
                `C:\ProgramData\Microsoft\Windows\Containers\Layers\layer2`,
                `C:\ProgramData\Microsoft\Windows\Containers\Layers\layer1`,
                `C:\scratch`,
            },
        },
        Root: &specs.Root{
            Path: `\\?\Volume{guid}\`, // 由层挂载生成
        },
    }

    opts := &hcsoci.CreateOptions{
        ID:   "my-sandbox",
        Spec: spec,
        // HostingSystem: nil → 进程隔离
        // HostingSystem: vm  → Hyper-V 隔离
    }

    container, resources, err := hcsoci.CreateContainer(ctx, opts)
    if err != nil {
        return err
    }
    defer resources.ReleaseResources(ctx, resources, nil, true)

    // 启动容器
    err = container.Start()
    // ...
}
```

### 返回值

- `cow.Container` — 容器接口，可调用 `Start()`, `CreateProcess()`, `Shutdown()`, `Terminate()`
- `*resources.Resources` — 已分配的资源，用于清理

---

## 3. 中级 API: 层管理

### 3.1 MountWCOWLayers

**文件**: `internal/layers/wcow_mount.go:30`

```go
func MountWCOWLayers(
    ctx context.Context,
    containerID string,
    vm *uvm.UtilityVM,     // nil = 进程隔离
    wl WCOWLayers,
) (_ *MountedWCOWLayers, _ resources.ResourceCloser, err error)
```

功能：挂载 Windows 容器镜像层，返回挂载后的 volume path。

### 3.2 底层层操作（hcsshim 公开 API）

**文件**: `hcsshim/layer.go`

```go
// 激活一个层
func ActivateLayer(info DriverInfo, id string) error

// 准备层（需要提供父层列表）
func PrepareLayer(info DriverInfo, id string, parentLayerPaths []string) error

// 获取层的挂载路径（volume GUID path）
func GetLayerMountPath(info DriverInfo, id string) (string, error)

// 反激活层
func DeactivateLayer(info DriverInfo, id string) error

// 创建新的空白层
func CreateLayer(info DriverInfo, id string, parent string) error

// 导出层
func ExportLayer(info DriverInfo, id string, path string, parentLayerPaths []string) error

// 导入层
func ImportLayer(info DriverInfo, id string, path string, parentLayerPaths []string) error
```

### 3.3 层操作流程

```
1. CreateLayer()         ← 创建空白层（或已有层跳过）
2. ActivateLayer()       ← 激活层
3. PrepareLayer()        ← 准备层（传入父层路径列表）
4. GetLayerMountPath()   ← 获取 volume GUID path（如 \\?\Volume{xxx}\）
5. 使用中...
6. DeactivateLayer()     ← 使用完毕，反激活
```

### 3.4 WCOWLayers 结构

```go
type WCOWLayers interface {
    IsWCOWLayers()
}

// 实际使用的结构
type wcowWCIFSLayers struct {
    Layers         []string // 层路径列表（从顶到底）
    ScratchVHDPath string   // scratch VHD 路径
}
```

---

## 4. 中级 API: UVM 管理

### 4.1 创建 Windows Utility VM

**文件**: `internal/uvm/create_wcow.go`

```go
// 创建默认选项
opts := uvm.NewDefaultOptionsWCOW("my-uvm", "")

// 自定义配置
opts.MemorySizeInMB = 2048
opts.ProcessorCount = 2
opts.BootFiles = &uvm.WCOWBootFiles{
    // 需要指定 UVM 启动文件路径
}

// 创建 UVM
vm, err := uvm.CreateWCOW(ctx, opts)

// 启动
err = vm.Start(ctx)

// 在 UVM 中创建容器
container, err := vm.CreateContainer(ctx, containerID, settings)

// 关闭
defer vm.Close()
```

### 4.2 OptionsWCOW 结构

```go
type OptionsWCOW struct {
    *Options  // 通用选项

    BootFiles *WCOWBootFiles  // UVM 启动文件

    NoDirectMap             bool // 不使用 VSMB 直接映射
    NoInheritHostTimezone   bool // 不继承宿主时区
    AdditionalRegistryKeys  []hcsschema.RegistryValue
}
```

### 4.3 Options 通用结构

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
    // ...
}
```

### 4.4 UVM 与容器的关系

```
Hyper-V 隔离模式:
┌──────────────────────────────────────┐
│  Utility VM (uvm.UtilityVM)          │
│  ┌────────────┐  ┌────────────┐     │
│  │ 容器 A     │  │ 容器 B     │     │
│  └────────────┘  └────────────┘     │
└──────────────────────────────────────┘

进程隔离模式:
┌────────────┐  ┌────────────┐
│ 容器 A     │  │ 容器 B     │  ← 直接在宿主上，不需要 UVM
└────────────┘  └────────────┘
```

---

## 5. 底层 API: hcsshim.CreateContainer

**文件**: `hcsshim/container.go:75`

### 功能

直接传 ContainerConfig JSON 结构体，不做任何额外处理。

### 函数签名

```go
func CreateContainer(id string, c *ContainerConfig) (Container, error)
```

### ContainerConfig 结构

```go
type ContainerConfig struct {
    // 必填
    SystemType  string  // 固定 "Container"
    Name        string  // 容器名
    Layers      []Layer // 镜像层列表
    HvPartition bool    // false=进程隔离, true=Hyper-V 隔离

    // Hyper-V 隔离时必填
    HvRuntime *HvRuntime

    // 资源限制
    ProcessorCount    uint32
    MemoryMaximumInMB int64

    // 挂载
    MappedDirectories []MappedDir

    // 网络
    EndpointList []string

    // 其他
    TerminateOnLastHandleClosed bool
    // ...更多字段见 vmcompute-api-reference.md
}
```

### 用法示例

```go
import "github.com/Microsoft/hcsshim"

cfg := &hcsshim.ContainerConfig{
    SystemType:     "Container",
    Name:           "my-sandbox",
    HvPartition:    false,
    ProcessorCount: 2,
    MemoryMaximumInMB: 1024,
    Layers: []hcsshim.Layer{
        {ID: "layer-0", Path: `C:\layers\base`},
    },
    TerminateOnLastHandleClosed: true,
}

container, err := hcsshim.CreateContainer("my-sandbox", cfg)
if err != nil {
    return err
}

err = container.Start()
// ...

proc, err := container.CreateProcess(&hcsshim.ProcessConfig{
    CommandLine:      "cmd /c echo hello",
    CreateStdOutPipe: true,
})
// ...
```

### 对比 hcsoci.CreateContainer

| | hcsshim.CreateContainer | hcsoci.CreateContainer |
|---|---|---|
| 输入 | ContainerConfig（手动组装） | OCI Spec |
| 层挂载 | 自己处理 | 自动 |
| scratch VHD | 自己创建 | 自动 |
| 配置组装 | 自己写 | 自动 |
| 网络 | 自己配置 | 自动 |
| 复杂度 | 低（但需要了解细节） | 高（但帮你处理了细节） |

---

## 6. 最底层: vmcompute.dll

**文件**: `internal/vmcompute/vmcompute.go`

直接调 Windows 系统 DLL，详见 [vmcompute-api-reference.md](vmcompute-api-reference.md)。

```go
// 等价于
modvmcompute = windows.NewLazySystemDLL("vmcompute.dll")
procHcsCreateComputeSystem = modvmcompute.NewProc("HcsCreateComputeSystem")
```

---

## 7. OCI Spec 构建指南

如果选择用 `hcsoci.CreateContainer`，需要构建 OCI Spec。

### 最小进程隔离 Spec

```go
spec := &specs.Spec{
    Version: "1.0.0",
    Windows: &specs.Windows{
        LayerFolders: []string{
            `C:\layers\layer2`,  // 顶层（最晚的修改）
            `C:\layers\layer1`,  // 底层（基础镜像）
            `C:\scratch`,        // 可写层（scratch）
        },
        IgnoreFlushesDuringBoot: true,
    },
    Root: &specs.Root{
        Path:   `\\?\Volume{guid}\`, // GetLayerMountPath 返回的值
        Readonly: false,
    },
    Process: &specs.Process{
        CommandLine: "cmd /c echo hello",
        Cwd:        `C:\`,
    },
}
```

### 最小 Hyper-V 隔离 Spec

```go
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

### Spec 中的关键字段

```go
type Spec struct {
    Version  string
    Root     *Root
    Process  *Process
    Windows  *Windows
    Mounts   []Mount
}

type Windows struct {
    LayerFolders            []string       // 层路径列表（必填）
    IgnoreFlushesDuringBoot bool           // 启动优化
    HyperV                  *WindowsHyperV // Hyper-V 配置
    Network                 *Network       // 网络配置
    Resources               *Resources     // 资源限制
}

type Root struct {
    Path     string // volume GUID path
    Readonly bool
}
```

---

## 8. 选择建议

### 方案 A: 用 hcsoci.CreateContainer（推荐）

```
优点: 层管理、scratch、网络全自动化
缺点: 需要学习 OCI Spec 格式
适合: 正式项目，长期维护
```

### 方案 B: 用 hcsshim.CreateContainer

```
优点: 简单直接，不需要 OCI Spec
缺点: 层管理、scratch 需要自己处理
适合: 快速原型，学习理解
```

### 方案 C: 直接调 vmcompute.dll

```
优点: 完全控制
缺点: 重新造轮子
适合: 不推荐
```

### 建议路径

```
1. 先用方案 B 跑通最小可行版本（进程隔离，单层）
2. 理解流程后切换到方案 A（加入 OCI Spec）
3. 最后加上 Hyper-V 隔离支持
```

---

*文档生成自 hcsshim v0.14.1 源码*
