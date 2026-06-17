# vmcompute.dll API 参考手册

> Windows Host Compute Service (HCS) 底层 API 完整参考
> 源码来源: [github.com/microsoft/hcsshim](https://github.com/microsoft/hcsshim)

---

## 1. 架构总览

```
┌──────────────────────────────────────────────────────────┐
│  应用层 (你的 Go 代码)                                     │
│  hcsshim.CreateContainer(id, &ContainerConfig{})          │
│  container.Start()                                        │
│  container.CreateProcess(&ProcessConfig{})                │
└───────────────────┬──────────────────────────────────────┘
                    │
┌───────────────────▼──────────────────────────────────────┐
│  hcsshim 公开 API 层                                      │
│  container.go / interface.go                              │
│  薄封装，转发给 internal/hcs                               │
└───────────────────┬──────────────────────────────────────┘
                    │
┌───────────────────▼──────────────────────────────────────┐
│  internal/hcs  核心层                                      │
│  system.go       → ComputeSystem 状态管理                  │
│  process.go      → Process 状态管理                        │
│  errors.go       → 错误码映射                              │
│  序列化 JSON → 调用 vmcompute 包                           │
└───────────────────┬──────────────────────────────────────┘
                    │
┌───────────────────▼──────────────────────────────────────┐
│  internal/vmcompute  syscall 层                           │
│  vmcompute.go      → //sys 函数声明                        │
│  zsyscall_windows.go → 实际 LoadLibrary + 调用             │
│                                                          │
│  modvmcompute = windows.NewLazySystemDLL("vmcompute.dll") │
└───────────────────┬──────────────────────────────────────┘
                    │
┌───────────────────▼──────────────────────────────────────┐
│  vmcompute.dll  (Windows 系统服务)                         │
│  用户态接口 → 内核态 HCS 服务                               │
└──────────────────────────────────────────────────────────┘
```

---

## 2. DLL 函数清单

`vmcompute.dll` 导出的所有函数，以及对应的 Go syscall 声明。

### 2.1 Compute System（计算系统 = 容器 或 Utility VM）

| C 函数 | Go 声明 | 说明 |
|--------|---------|------|
| `HcsCreateComputeSystem` | `hcsCreateComputeSystem(id, configuration, identity, computeSystem, result)` | 创建计算系统（不启动） |
| `HcsStartComputeSystem` | `hcsStartComputeSystem(computeSystem, options, result)` | 启动计算系统 |
| `HcsShutdownComputeSystem` | `hcsShutdownComputeSystem(computeSystem, options, result)` | 优雅关闭 |
| `HcsTerminateComputeSystem` | `hcsTerminateComputeSystem(computeSystem, options, result)` | 强制终止 |
| `HcsCloseComputeSystem` | `hcsCloseComputeSystem(computeSystem)` | 关闭句柄（不终止） |
| `HcsPauseComputeSystem` | `hcsPauseComputeSystem(computeSystem, options, result)` | 暂停 |
| `HcsResumeComputeSystem` | `hcsResumeComputeSystem(computeSystem, options, result)` | 恢复 |
| `HcsSaveComputeSystem` | `hcsSaveComputeSystem(computeSystem, options, result)` | 保存状态 |
| `HcsOpenComputeSystem` | `hcsOpenComputeSystem(id, computeSystem, result)` | 打开已有计算系统 |
| `HcsEnumerateComputeSystems` | `hcsEnumerateComputeSystems(query, computeSystems, result)` | 列出所有计算系统 |
| `HcsGetComputeSystemProperties` | `hcsGetComputeSystemProperties(computeSystem, propertyQuery, properties, result)` | 获取属性 |
| `HcsModifyComputeSystem` | `hcsModifyComputeSystem(computeSystem, configuration, result)` | 修改配置 |
| `HcsRegisterComputeSystemCallback` | `hcsRegisterComputeSystemCallback(computeSystem, callback, context, callbackHandle)` | 注册事件回调 |
| `HcsUnregisterComputeSystemCallback` | `hcsUnregisterComputeSystemCallback(callbackHandle)` | 取消回调 |

### 2.2 Process（容器内进程）

| C 函数 | Go 声明 | 说明 |
|--------|---------|------|
| `HcsCreateProcess` | `hcsCreateProcess(computeSystem, processParameters, processInformation, process, result)` | 在容器内创建进程 |
| `HcsOpenProcess` | `hcsOpenProcess(computeSystem, pid, process, result)` | 打开已有进程 |
| `HcsCloseProcess` | `hcsCloseProcess(process)` | 关闭进程句柄 |
| `HcsTerminateProcess` | `hcsTerminateProcess(process, result)` | 终止进程 |
| `HcsSignalProcess` | `hcsSignalProcess(process, options, result)` | 发送信号（如 Ctrl+C） |
| `HcsGetProcessInfo` | `hcsGetProcessInfo(process, processInformation, result)` | 获取进程信息 |
| `HcsGetProcessProperties` | `hcsGetProcessProperties(process, processProperties, result)` | 获取进程属性 |
| `HcsModifyProcess` | `hcsModifyProcess(process, settings, result)` | 修改进程设置 |
| `HcsRegisterProcessCallback` | `hcsRegisterProcessCallback(process, callback, context, callbackHandle)` | 注册进程回调 |
| `HcsUnregisterProcessCallback` | `hcsUnregisterProcessCallback(callbackHandle)` | 取消进程回调 |

### 2.3 Service（HCS 服务本身）

| C 函数 | Go 声明 | 说明 |
|--------|---------|------|
| `HcsGetServiceProperties` | `hcsGetServiceProperties(propertyQuery, properties, result)` | 获取 HCS 服务属性 |
| `HcsModifyServiceSettings` | `hcsModifyServiceSettings(settings, result)` | 修改 HCS 服务设置 |

---

## 3. 核心数据结构

### 3.1 Handle 类型

```go
// vmcompute.go 中定义

type HcsSystem  syscall.Handle  // 计算系统句柄
type HcsProcess syscall.Handle  // 进程句柄
type HcsCallback syscall.Handle // 回调句柄

// 进程创建时返回的信息
type HcsProcessInformation struct {
    ProcessId uint32         // 容器内 PID
    _         uint32         // reserved padding
    StdInput  syscall.Handle // stdin 管道句柄
    StdOutput syscall.Handle // stdout 管道句柄
    StdError  syscall.Handle // stderr 管道句柄
}
```

### 3.2 ContainerConfig（创建容器时传入的 JSON）

**文件**: `internal/hcs/schema1/schema1.go`

这是 `HcsCreateComputeSystem` 的 `configuration` 参数反序列化后的结构。

```go
type ContainerConfig struct {
    // === 必填字段 ===
    SystemType  string  // 固定为 "Container"
    Name        string  // 容器名称（Docker 用容器 ID）
    Layers      []Layer // 存储层列表（镜像层）

    // === 隔离模式 ===
    HvPartition bool       // false = 进程隔离(Argon), true = Hyper-V 隔离(Xenon)
    HvRuntime   *HvRuntime // Hyper-V 容器专用配置（当 HvPartition=true 时必填）

    // === 资源限制 ===
    ProcessorCount      uint32 // CPU 核数
    ProcessorWeight     uint64 // CPU 相对权重 (1-10000, 0=默认)
    ProcessorMaximum    int64  // CPU 最大百分比 (1-10000, 0=无限制)
    MemoryMaximumInMB   int64  // 最大内存 (MB)
    StorageIOPSMaximum  uint64 // 最大存储 IOPS
    StorageBandwidthMaximum uint64 // 最大存储带宽 (bytes/s)
    StorageSandboxSize  uint64 // 系统盘扩展大小 (bytes)

    // === 网络 ===
    EndpointList               []string // 网络 endpoint 列表
    NetworkSharedContainerName string   // 共享网络栈的容器名
    AllowUnqualifiedDNSQuery   bool     // 允许非限定 DNS 查询
    DNSSearchList              string   // DNS 搜索列表（逗号分隔）

    // === 挂载 ===
    MappedDirectories  []MappedDir         // 目录挂载
    MappedPipes        []MappedPipe        // 命名管道挂载
    MappedVirtualDisks []MappedVirtualDisk // 虚拟磁盘挂载

    // === 其他 ===
    Owner                       string // 管理平台标识
    VolumePath                  string // Windows 卷路径（进程隔离容器的 scratch 空间）
    LayerFolderPath             string // 层文件夹路径
    HostName                    string // 主机名
    Credentials                 string // 凭据信息
    ContainerType               string // "Linux" = Linux 容器 on Windows
    IgnoreFlushesDuringBoot     bool   // 启动时忽略 flush（优化）
    Servicing                   bool   // 是否为维护容器
    TerminateOnLastHandleClosed bool   // 最后一个句柄关闭时终止容器
    AssignedDevices             []AssignedDevice // 直接分配的设备
}
```

### 3.3 Layer（镜像层）

```go
type Layer struct {
    ID   string // 层的 GUID
    Path string // 层在宿主机上的路径
}
```

Windows 容器使用分层存储。每层是一个只读目录，通过 union 文件系统叠加。

典型路径:
```
C:\ProgramData\Docker\windowsfilter\<layer-id>\
C:\ProgramData\Microsoft\Windows\Containers\BaseImages\<layer-id>\
```

### 3.4 HvRuntime（Hyper-V 容器运行时配置）

```go
type HvRuntime struct {
    ImagePath           string // Utility VM 镜像路径（必填）
    SkipTemplate        bool   // 跳过模板
    LinuxInitrdFile     string // Linux 容器的 initrd 路径
    LinuxKernelFile     string // Linux 容器的 kernel 路径
    LinuxBootParameters string // Linux 启动参数
    BootSource          string // "Vhd" = 从 VHD 启动
    WritableBootSource  bool   // 可写启动源
}
```

`ImagePath` 的典型值:
```
C:\ProgramData\Microsoft\Windows\Containers\BaseImages\<guid>\UtilityVM
```

### 3.5 MappedDir（目录挂载）

```go
type MappedDir struct {
    HostPath          string // 宿主机路径
    ContainerPath     string // 容器内路径
    ReadOnly          bool   // 是否只读
    BandwidthMaximum  uint64 // 带宽限制
    IOPSMaximum       uint64 // IOPS 限制
    CreateInUtilityVM bool   // 是否在 UVM 中创建
    LinuxMetadata     bool   // Linux 元数据支持 (RS4+)
}
```

### 3.6 MappedPipe（命名管道挂载）

```go
type MappedPipe struct {
    HostPath          string // 宿主机管道路径
    ContainerPipeName string // 容器内管道名
}
```

### 3.7 MappedVirtualDisk（虚拟磁盘挂载）

```go
type MappedVirtualDisk struct {
    HostPath          string // VHD 文件路径
    ContainerPath     string // 容器内挂载点
    CreateInUtilityVM bool   // 是否在 UVM 中创建
    ReadOnly          bool   // 是否只读
    Cache             string // 缓存策略: "" | "Disabled" | "Enabled" | "Private" | "PrivateAllowSharing"
    AttachOnly        bool   // 仅附加（不挂载）
}
```

### 3.8 ProcessConfig（创建进程时传入的 JSON）

```go
type ProcessConfig struct {
    ApplicationName   string            // 应用程序名
    CommandLine       string            // 命令行（如 "cmd /c echo hello"）
    CommandArgs       []string          // 参数数组（Linux 容器用）
    User              string            // 运行用户
    WorkingDirectory  string            // 工作目录
    Environment       map[string]string // 环境变量
    EmulateConsole    bool              // 模拟控制台
    CreateStdInPipe   bool              // 创建 stdin 管道
    CreateStdOutPipe  bool              // 创建 stdout 管道
    CreateStdErrPipe  bool              // 创建 stderr 管道
    ConsoleSize       [2]uint           // 控制台大小 [行, 列]
    CreateInUtilityVm bool              // 在 UVM 中创建（Linux 容器用）
    OCISpecification  *json.RawMessage  // OCI 规范（Linux 容器用）
}
```

### 3.9 ComputeSystemQuery（查询计算系统）

```go
type ComputeSystemQuery struct {
    IDs    []string `json:"Ids,omitempty"`    // 按 ID 过滤
    Types  []string `json:",omitempty"`       // 按类型过滤
    Names  []string `json:",omitempty"`       // 按名称过滤
    Owners []string `json:",omitempty"`       // 按所有者过滤
}
```

---

## 4. 典型调用流程

### 4.1 创建并运行进程隔离容器

```go
// 1. 准备配置 JSON
config := &ContainerConfig{
    SystemType:     "Container",
    Name:           "my-container",
    HvPartition:    false,  // 进程隔离
    ProcessorCount: 2,
    MemoryMaximumInMB: 1024,
    Layers: []Layer{
        {ID: "base-layer-id", Path: `C:\ProgramData\Docker\windowsfilter\base-layer`},
    },
    MappedDirectories: []MappedDir{
        {HostPath: `C:\data`, ContainerPath: `C:\mount`, ReadOnly: false},
    },
}

// 2. 创建（不启动）
container, err := hcsshim.CreateContainer("my-container", config)

// 3. 启动
err = container.Start()

// 4. 创建进程
proc, err := container.CreateProcess(&ProcessConfig{
    CommandLine:      "cmd /c echo hello",
    CreateStdInPipe:  true,
    CreateStdOutPipe: true,
    CreateStdErrPipe: true,
})

// 5. 读写 stdio
stdin, stdout, stderr, _ := proc.Stdio()

// 6. 等待退出
proc.Wait()
exitCode, _ := proc.ExitCode()

// 7. 清理
container.Shutdown()
container.Terminate()
container.Close()
```

### 4.2 创建并运行 Hyper-V 隔离容器

```go
config := &ContainerConfig{
    SystemType:     "Container",
    Name:           "my-hyperv-container",
    HvPartition:    true,  // Hyper-V 隔离
    ProcessorCount: 2,
    MemoryMaximumInMB: 2048,
    Layers: []Layer{
        {ID: "base-layer-id", Path: `C:\ProgramData\Docker\windowsfilter\base-layer`},
    },
    HvRuntime: &HvRuntime{
        ImagePath: `C:\ProgramData\Microsoft\Windows\Containers\BaseImages\<guid>\UtilityVM`,
    },
    MappedDirectories: []MappedDir{
        {HostPath: `C:\data`, ContainerPath: `C:\mount`},
    },
}

// 后续流程与进程隔离容器完全相同
container, _ := hcsshim.CreateContainer("my-hyperv-container", config)
container.Start()
// ...
```

### 4.3 进程隔离 vs Hyper-V 隔离的配置差异

```diff
  config := &ContainerConfig{
      SystemType:  "Container",
      Name:        "my-container",
-     HvPartition: false,
+     HvPartition: true,
      Layers:      layers,
+     HvRuntime: &HvRuntime{
+         ImagePath: `C:\...\UtilityVM`,
+     },
  }
```

**只需要改两个地方：**
1. `HvPartition` 设为 `true`
2. 加上 `HvRuntime.ImagePath`

---

## 5. 底层 syscall 映射

### 5.1 Go //sys 声明语法

hcsshim 使用 `mkwinsyscall` 工具从 `//sys` 注释生成 syscall 代码：

```go
// sys hcsCreateComputeSystem(id string, configuration string, identity syscall.Handle,
//     computeSystem *HcsSystem, result **uint16) (hr error) = vmcompute.HcsCreateComputeSystem?
```

等价于：

```go
// LoadLibrary
modvmcompute = windows.NewLazySystemDLL("vmcompute.dll")

// GetProcAddress
procHcsCreateComputeSystem = modvmcompute.NewProc("HcsCreateComputeSystem")

// 调用
ret, _, _ := procHcsCreateComputeSystem.Call(
    uintptr(unsafe.Pointer(idStr)),
    uintptr(unsafe.Pointer(configStr)),
    uintptr(identity),
    uintptr(unsafe.Pointer(&computeSystem)),
    uintptr(unsafe.Pointer(&resultStr)),
)
```

### 5.2 完整 syscall 映射表

| Go 函数 | DLL 导出函数 | 参数 |
|---------|-------------|------|
| `hcsCreateComputeSystem` | `HcsCreateComputeSystem` | `(id, config, identity, *handle, *result)` |
| `hcsStartComputeSystem` | `HcsStartComputeSystem` | `(handle, options, *result)` |
| `hcsShutdownComputeSystem` | `HcsShutdownComputeSystem` | `(handle, options, *result)` |
| `hcsTerminateComputeSystem` | `HcsTerminateComputeSystem` | `(handle, options, *result)` |
| `hcsCloseComputeSystem` | `HcsCloseComputeSystem` | `(handle)` |
| `hcsPauseComputeSystem` | `HcsPauseComputeSystem` | `(handle, options, *result)` |
| `hcsResumeComputeSystem` | `HcsResumeComputeSystem` | `(handle, options, *result)` |
| `hcsSaveComputeSystem` | `HcsSaveComputeSystem` | `(handle, options, *result)` |
| `hcsOpenComputeSystem` | `HcsOpenComputeSystem` | `(id, *handle, *result)` |
| `hcsEnumerateComputeSystems` | `HcsEnumerateComputeSystems` | `(query, *systems, *result)` |
| `hcsGetComputeSystemProperties` | `HcsGetComputeSystemProperties` | `(handle, query, *props, *result)` |
| `hcsModifyComputeSystem` | `HcsModifyComputeSystem` | `(handle, config, *result)` |
| `hcsCreateProcess` | `HcsCreateProcess` | `(handle, params, *info, *proc, *result)` |
| `hcsOpenProcess` | `HcsOpenProcess` | `(handle, pid, *proc, *result)` |
| `hcsCloseProcess` | `HcsCloseProcess` | `(handle)` |
| `hcsTerminateProcess` | `HcsTerminateProcess` | `(handle, *result)` |
| `hcsSignalProcess` | `HcsSignalProcess` | `(handle, options, *result)` |
| `hcsGetProcessInfo` | `HcsGetProcessInfo` | `(handle, *info, *result)` |
| `hcsGetProcessProperties` | `HcsGetProcessProperties` | `(handle, *props, *result)` |
| `hcsModifyProcess` | `HcsModifyProcess` | `(handle, settings, *result)` |
| `hcsRegisterComputeSystemCallback` | `HcsRegisterComputeSystemCallback` | `(handle, cb, ctx, *cbHandle)` |
| `hcsUnregisterComputeSystemCallback` | `HcsUnregisterComputeSystemCallback` | `(cbHandle)` |
| `hcsRegisterProcessCallback` | `HcsRegisterProcessCallback` | `(handle, cb, ctx, *cbHandle)` |
| `hcsUnregisterProcessCallback` | `HcsUnregisterProcessCallback` | `(cbHandle)` |
| `hcsGetServiceProperties` | `HcsGetServiceProperties` | `(query, *props, *result)` |
| `hcsModifyServiceSettings` | `HcsModifyServiceSettings` | `(settings, *result)` |

---

## 6. 错误处理

### 6.1 特殊错误码

```go
// 异步操作挂起（正常情况，表示操作正在后台完成）
errVmcomputeOperationPending = syscall.Errno(0xC0370103)

// 计算系统不存在
ErrComputeSystemDoesNotExist = syscall.Errno(0xc037010e)

// 元素未找到
ErrElementNotFound = syscall.Errno(0x490)

// 不支持的操作
ErrNotSupported = syscall.Errno(0x32)

// 无效数据
ErrInvalidData = syscall.Errno(0xd)

// 已停止
ErrVmcomputeAlreadyStopped = syscall.Errno(0xc0370110)
```

### 6.2 result 参数

大多数 HCS 函数的最后一个参数是 `result **uint16`，返回一个 JSON 字符串，包含操作的详细结果或错误信息。hcsshim 内部会：

1. 检查返回的 HRESULT
2. 如果失败，解析 result JSON 获取详细错误
3. 包装为 `HcsError` 返回

```go
type HcsError struct {
    Op      string        // 操作名
    Err     error         // 原始错误
    Events  []ErrorEvent  // HCS 事件详情
}
```

---

## 7. JSON 配置示例

### 7.1 进程隔离容器（最简配置）

```json
{
    "SystemType": "Container",
    "Name": "test-container",
    "HvPartition": false,
    "Layers": [
        {
            "Id": "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
            "Path": "C:\\ProgramData\\Docker\\windowsfilter\\xxxxxxxx"
        }
    ],
    "ProcessorCount": 2,
    "MemoryMaximumInMB": 1024
}
```

### 7.2 Hyper-V 隔离容器

```json
{
    "SystemType": "Container",
    "Name": "test-hyperv-container",
    "HvPartition": true,
    "HvRuntime": {
        "ImagePath": "C:\\ProgramData\\Microsoft\\Windows\\Containers\\BaseImages\\xxxxxxxx\\UtilityVM",
        "SkipTemplate": true
    },
    "Layers": [
        {
            "Id": "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
            "Path": "C:\\ProgramData\\Docker\\windowsfilter\\xxxxxxxx"
        }
    ],
    "ProcessorCount": 2,
    "MemoryMaximumInMB": 2048,
    "MappedDirectories": [
        {
            "HostPath": "C:\\Users\\test\\data",
            "ContainerPath": "C:\\mount",
            "ReadOnly": false
        }
    ],
    "TerminateOnLastHandleClosed": true
}
```

### 7.3 进程创建配置

```json
{
    "CommandLine": "cmd /c dir C:\\mount",
    "WorkingDirectory": "C:\\",
    "CreateStdInPipe": true,
    "CreateStdOutPipe": true,
    "CreateStdErrPipe": true,
    "ConsoleSize": [25, 80],
    "Environment": {
        "PATH": "C:\\Windows\\system32;C:\\Windows"
    }
}
```

---

## 8. 两种隔离模式对比

| 特性 | 进程隔离 (Argon) | Hyper-V 隔离 (Xenon) |
|------|-----------------|----------------------|
| `HvPartition` | `false` | `true` |
| `HvRuntime` | 不需要 | **必填** |
| 内核 | 共享宿主内核 | 独立内核（在 Utility VM 中） |
| 启动速度 | 毫秒级 | 秒级 |
| 内存开销 | 几 MB | 几百 MB（每个 VM） |
| 安全性 | 较低（共享内核） | 高（VM 级隔离） |
| 内核版本 | 必须与宿主一致 | 可以不同 |
| 需要 Hyper-V | 不需要 | 需要 |
| Windows 版本要求 | Pro/Enterprise/Server | Pro/Enterprise/Server |

---

## 9. 资源限制参数速查

| 参数 | 类型 | 说明 | 默认值 |
|------|------|------|--------|
| `ProcessorCount` | uint32 | vCPU 核数 | 宿主核数 |
| `ProcessorWeight` | uint64 | CPU 相对权重 (1-10000) | 5000 (正常) |
| `ProcessorMaximum` | int64 | CPU 最大百分比 ×100 (1-10000) | 0 (无限制) |
| `MemoryMaximumInMB` | int64 | 最大内存 (MB) | 1024 |
| `StorageIOPSMaximum` | uint64 | 最大存储 IOPS | 0 (无限制) |
| `StorageBandwidthMaximum` | uint64 | 最大存储带宽 (bytes/s) | 0 (无限制) |
| `StorageSandboxSize` | uint64 | 系统盘扩展大小 (bytes) | 默认大小 |

---

## 10. 文件索引

| 文件 | 作用 |
|------|------|
| `hcsshim/container.go` | 公开 API: `CreateContainer`, `OpenContainer`, `GetContainers` |
| `hcsshim/interface.go` | 接口定义: `Container`, `Process` |
| `internal/vmcompute/vmcompute.go` | syscall 声明 (`//sys`) |
| `internal/vmcompute/zsyscall_windows.go` | 自动生成的 syscall 实现 |
| `internal/hcs/system.go` | `System` 结构体，ComputeSystem 的完整生命周期 |
| `internal/hcs/process.go` | `Process` 结构体，进程管理 |
| `internal/hcs/errors.go` | 错误码定义 |
| `internal/hcs/schema1/schema1.go` | V1 Schema: `ContainerConfig`, `ProcessConfig`, `Layer` 等 |
| `internal/hcs/schema2/compute_system.go` | V2 Schema: `ComputeSystem` |
| `internal/hcs/schema2/container.go` | V2 Schema: `Container` |
| `internal/hcsoci/hcsdoc_wcow.go` | WCOW 容器文档组装逻辑（`HvPartition` 分支在这里） |
| `internal/uvm/create.go` | Utility VM 创建入口 |
| `internal/uvm/create_wcow.go` | WCOW Utility VM 创建 |

---

*文档生成自 hcsshim v0.14.1 源码，对应 HCS API v1/v2*
