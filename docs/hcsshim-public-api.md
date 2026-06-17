# hcsshim 公开 API 完整参考

hcsshim 包导出的全部公开函数、类型、接口。
外部项目只能使用这些 API，不能 import internal 包。

## 目录

1. [容器操作](#1-容器操作)
2. [层操作](#2-层操作)
3. [进程操作](#3-进程操作)
4. [GUID 操作](#4-guid-操作)
5. [类型定义](#5-类型定义)
6. [接口定义](#6-接口定义)
7. [错误类型](#7-错误类型)

---

## 1. 容器操作

### CreateContainer

创建容器（不启动）。

```go
func CreateContainer(id string, c *ContainerConfig) (Container, error)
```

参数：

- `id` — 容器唯一标识
- `c` — 容器配置（见 ContainerConfig）

返回：

- `Container` — 容器接口，可调用 Start/CreateProcess 等方法
- `error` — 错误信息

示例：

```go
cfg := &hcsshim.ContainerConfig{
    SystemType:  "Container",
    Name:        "my-container",
    HvPartition: false,
    VolumePath:  volumePath,
    Layers:      []hcsshim.Layer{{ID: guid, Path: layerPath}},
}
container, err := hcsshim.CreateContainer("my-id", cfg)
```

### OpenContainer

打开已存在的容器。

```go
func OpenContainer(id string) (Container, error)
```

### GetContainers

查询容器列表。

```go
func GetContainers(q ComputeSystemQuery) ([]ContainerProperties, error)
```

示例：

```go
// 查询所有容器
containers, _ := hcsshim.GetContainers(hcsshim.ComputeSystemQuery{})

// 按 ID 查询
containers, _ := hcsshim.GetContainers(hcsshim.ComputeSystemQuery{
    IDs: []string{"my-container"},
})
```

### Container 接口方法

```go
type Container interface {
    Start() error                                    // 启动容器
    Shutdown() error                                 // 优雅关闭
    Terminate() error                                // 强制终止
    Wait() error                                     // 等待容器退出
    WaitTimeout(time.Duration) error                 // 带超时等待
    Pause() error                                    // 暂停
    Resume() error                                   // 恢复
    HasPendingUpdates() (bool, error)                // 是否有待处理更新
    Statistics() (Statistics, error)                 // 获取统计信息
    ProcessList() ([]ProcessListItem, error)         // 获取进程列表
    MappedVirtualDisks() (map[int]MappedVirtualDiskController, error)
    CreateProcess(c *ProcessConfig) (Process, error) // 创建进程
    OpenProcess(pid int) (Process, error)            // 打开已有进程
    Close() error                                    // 关闭句柄
    Modify(config *ResourceModificationRequestResponse) error // 修改配置
}
```

---

## 2. 层操作

### DriverInfo

层操作的驱动信息。

```go
type DriverInfo struct {
    Flavour int     // 驱动类型（1 = Windows filter driver）
    HomeDir string  // 层存储根目录
}
```

示例：

```go
info := hcsshim.DriverInfo{
    Flavour: 1,
    HomeDir: `C:\ProgramData\Docker\windowsfilter`,
}
```

### ActivateLayer

激活一个层。必须在 PrepareLayer 之前调用。

```go
func ActivateLayer(info DriverInfo, id string) error
```

### DeactivateLayer

反激活一个层。必须在 UnprepareLayer 之后调用。

```go
func DeactivateLayer(info DriverInfo, id string) error
```

### PrepareLayer

准备一个层，使其可以被容器使用。

```go
func PrepareLayer(info DriverInfo, layerId string, parentLayerPaths []string) error
```

参数：

- `layerId` — 层 ID（在 HomeDir 下的目录名）
- `parentLayerPaths` — 父层路径列表（只读层）

### UnprepareLayer

取消层的准备状态。

```go
func UnprepareLayer(info DriverInfo, layerId string) error
```

### GetLayerMountPath

获取层的 volume GUID path。

```go
func GetLayerMountPath(info DriverInfo, id string) (string, error)
```

返回值格式：`\\?\Volume{xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx}`

### CreateLayer

创建新层。

```go
func CreateLayer(info DriverInfo, id, parent string) error
```

### CreateScratchLayer

创建 scratch 层（可写层）。

```go
func CreateScratchLayer(info DriverInfo, layerId, parentId string, parentLayerPaths []string) error
```

### CreateSandboxLayer

创建 sandbox 层。

```go
func CreateSandboxLayer(info DriverInfo, layerId, parentId string, parentLayerPaths []string) error
```

### DestroyLayer

删除层。

```go
func DestroyLayer(info DriverInfo, id string) error
```

### LayerExists

检查层是否存在。

```go
func LayerExists(info DriverInfo, id string) (bool, error)
```

### ExportLayer

导出层到指定目录。

```go
func ExportLayer(info DriverInfo, layerId string, exportFolderPath string, parentLayerPaths []string) error
```

### ImportLayer

从目录导入层。

```go
func ImportLayer(info DriverInfo, layerID string, importFolderPath string, parentLayerPaths []string) error
```

### ExpandSandboxSize

扩展 sandbox 层大小。

```go
func ExpandSandboxSize(info DriverInfo, layerId string, size uint64) error
```

### ExpandScratchSize

扩展 scratch 层大小。

```go
func ExpandScratchSize(info DriverInfo, layerId string, size uint64) error
```

### ProcessBaseLayer

处理基础层。

```go
func ProcessBaseLayer(path string) error
```

### ProcessUtilityVMImage

处理 Utility VM 镜像。

```go
func ProcessUtilityVMImage(path string) error
```

### ConvertToBaseLayer

将目录转换为基础层。

```go
func ConvertToBaseLayer(path string) error
```

### GetSharedBaseImages

获取共享基础镜像信息。

```go
func GetSharedBaseImages() (imageData string, err error)
```

### NewLayerReader

创建层读取器（用于导出）。

```go
func NewLayerReader(info DriverInfo, layerID string, parentLayerPaths []string) (LayerReader, error)
```

### NewLayerWriter

创建层写入器（用于导入）。

```go
func NewLayerWriter(info DriverInfo, layerID string, parentLayerPaths []string) (LayerWriter, error)
```

### 层操作流程

```text
创建 scratch:
1. MkdirAll(scratchPath)
2. CreateScratchLayer(info, scratchID, "", []string{baseLayerPath})

挂载层:
1. ActivateLayer(info, scratchID)
2. PrepareLayer(info, scratchID, []string{baseLayerPath})
3. mountPath, _ := GetLayerMountPath(info, scratchID)
4. // 使用 mountPath 作为 VolumePath 创建容器

卸载层:
1. UnprepareLayer(info, scratchID)
2. DeactivateLayer(info, scratchID)
```

---

## 3. 进程操作

### ProcessConfig

进程创建配置。

```go
type ProcessConfig struct {
    ApplicationName   string            // 应用程序名
    CommandLine       string            // 命令行
    CommandArgs       []string          // 参数数组（Linux 容器用）
    User              string            // 运行用户
    WorkingDirectory  string            // 工作目录
    Environment       map[string]string // 环境变量
    EmulateConsole    bool              // 模拟控制台
    CreateStdInPipe   bool              // 创建 stdin 管道
    CreateStdOutPipe  bool              // 创建 stdout 管道
    CreateStdErrPipe  bool              // 创建 stderr 管道
    ConsoleSize       [2]uint           // 控制台大小 [行, 列]
    CreateInUtilityVm bool              // 在 UVM 中创建
    OCISpecification  *json.RawMessage  // OCI 规范
}
```

### Process 接口

```go
type Process interface {
    Pid() int                                                    // 获取 PID
    Kill() error                                                 // 终止进程
    Wait() error                                                 // 等待退出
    WaitTimeout(time.Duration) error                             // 带超时等待
    ExitCode() (int, error)                                      // 获取退出码
    ResizeConsole(width, height uint16) error                    // 调整控制台大小
    Stdio() (io.WriteCloser, io.ReadCloser, io.ReadCloser, error) // 获取 stdin/stdout/stderr
    CloseStdin() error                                           // 关闭 stdin
    Close() error                                                // 关闭句柄
}
```

### 示例

```go
proc, _ := container.CreateProcess(&hcsshim.ProcessConfig{
    CommandLine:      "cmd /c echo hello",
    CreateStdOutPipe: true,
    CreateStdErrPipe: true,
})
defer proc.Close()

stdin, stdout, stderr, _ := proc.Stdio()
// 读取输出...
proc.Wait()
exitCode, _ := proc.ExitCode()
```

---

## 4. GUID 操作

### GUID 类型

```go
type GUID [16]byte
```

### NewGUID

从字符串生成 GUID。

```go
func NewGUID(source string) *GUID
```

### NameToGuid

从目录名生成 GUID（HCS API 要求的格式）。

```go
func NameToGuid(name string) (id GUID, err error)
```

### ToString

将 GUID 转换为字符串格式。

```go
func (g *GUID) ToString() string
```

返回格式：`xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx`

### 使用场景

```go
// HCS 要求 Layer ID 是 GUID 格式，不是目录名
dirName := filepath.Base(layerPath)
guid := hcsshim.NewGUID(dirName)
layer := hcsshim.Layer{
    ID:   guid.ToString(),  // 正确格式
    Path: layerPath,
}
```

---

## 5. 类型定义

### ContainerConfig

容器配置（CreateContainer 的参数）。

```go
type ContainerConfig struct {
    SystemType                  string              // 固定为 "Container"
    Name                        string              // 容器名称
    Owner                       string              // 所有者
    VolumePath                  string              // volume GUID path
    IgnoreFlushesDuringBoot     bool                // 启动优化
    LayerFolderPath             string              // scratch 层路径
    Layers                      []Layer             // 只读层列表
    Credentials                 string              // 凭据
    ProcessorCount              uint32              // CPU 核数
    ProcessorWeight             uint64              // CPU 权重 (1-10000)
    ProcessorMaximum            int64               // CPU 最大百分比
    StorageIOPSMaximum          uint64              // 存储 IOPS 限制
    StorageBandwidthMaximum     uint64              // 存储带宽限制
    StorageSandboxSize          uint64              // 系统盘大小
    MemoryMaximumInMB           int64               // 内存限制 (MB)
    HostName                    string              // 主机名
    MappedDirectories           []MappedDir         // 目录挂载
    MappedPipes                 []MappedPipe        // 命名管道挂载
    HvPartition                 bool                // Hyper-V 隔离
    NetworkSharedContainerName  string              // 共享网络的容器名
    EndpointList                []string            // 网络 endpoint
    HvRuntime                   *HvRuntime          // Hyper-V 配置
    Servicing                   bool                // 维护容器
    AllowUnqualifiedDNSQuery    bool                // 允许非限定 DNS
    DNSSearchList               string              // DNS 搜索列表
    ContainerType               string              // "Linux" = Linux 容器
    TerminateOnLastHandleClosed bool                // 关闭时终止
    MappedVirtualDisks          []MappedVirtualDisk // 虚拟磁盘
    AssignedDevices             []AssignedDevice    // 分配的设备
}
```

### Layer

镜像层。

```go
type Layer struct {
    ID   string // 层 ID（GUID 格式）
    Path string // 层路径
}
```

### MappedDir

目录挂载。

```go
type MappedDir struct {
    HostPath          string // 宿主机路径
    ContainerPath     string // 容器内路径
    ReadOnly          bool   // 只读
    BandwidthMaximum  uint64 // 带宽限制
    IOPSMaximum       uint64 // IOPS 限制
    CreateInUtilityVM bool   // 在 UVM 中创建
    LinuxMetadata     bool   // Linux 元数据
}
```

### MappedPipe

命名管道挂载。

```go
type MappedPipe struct {
    HostPath          string // 宿主机管道路径
    ContainerPipeName string // 容器内管道名
}
```

### HvRuntime

Hyper-V 运行时配置。

```go
type HvRuntime struct {
    ImagePath           string // Utility VM 镜像路径
    SkipTemplate        bool   // 跳过模板
    LinuxInitrdFile     string // Linux initrd 路径
    LinuxKernelFile     string // Linux kernel 路径
    LinuxBootParameters string // Linux 启动参数
    BootSource          string // "Vhd" = 从 VHD 启动
    WritableBootSource  bool   // 可写启动源
}
```

### MappedVirtualDisk

虚拟磁盘挂载。

```go
type MappedVirtualDisk struct {
    HostPath          string // VHD 路径
    ContainerPath     string // 容器内挂载点
    CreateInUtilityVM bool   // 在 UVM 中创建
    ReadOnly          bool   // 只读
    Cache             string // 缓存策略
    AttachOnly        bool   // 仅附加
}
```

### ComputeSystemQuery

查询条件。

```go
type ComputeSystemQuery struct {
    IDs    []string `json:"Ids,omitempty"`
    Types  []string `json:",omitempty"`
    Names  []string `json:",omitempty"`
    Owners []string `json:",omitempty"`
}
```

### Statistics

容器统计信息。

```go
type Statistics struct {
    Timestamp          time.Time
    ContainerStartTime time.Time
    Uptime100ns        uint64
    Memory             MemoryStats
    Processor          ProcessorStats
    Storage            StorageStats
    Network            []NetworkStats
}
```

### ResourceModificationRequestResponse

资源修改请求。

```go
type ResourceModificationRequestResponse struct {
    Resource ResourceType // "Network"
    Data     interface{}  // 配置数据
    Request  RequestType  // "Add" 或 "Remove"
}
```

---

## 6. 接口定义

### Container

容器接口（由 CreateContainer 返回）。

```go
type Container interface {
    Start() error
    Shutdown() error
    Terminate() error
    Wait() error
    WaitTimeout(time.Duration) error
    Pause() error
    Resume() error
    HasPendingUpdates() (bool, error)
    Statistics() (Statistics, error)
    ProcessList() ([]ProcessListItem, error)
    MappedVirtualDisks() (map[int]MappedVirtualDiskController, error)
    CreateProcess(c *ProcessConfig) (Process, error)
    OpenProcess(pid int) (Process, error)
    Close() error
    Modify(config *ResourceModificationRequestResponse) error
}
```

### Process

进程接口（由 CreateProcess 返回）。

```go
type Process interface {
    Pid() int
    Kill() error
    Wait() error
    WaitTimeout(time.Duration) error
    ExitCode() (int, error)
    ResizeConsole(width, height uint16) error
    Stdio() (io.WriteCloser, io.ReadCloser, io.ReadCloser, error)
    CloseStdin() error
    Close() error
}
```

---

## 7. 错误类型

### HcsError

HCS 操作错误。

```go
type HcsError struct {
    Op      string       // 操作名
    Err     error        // 原始错误
    Events  []ErrorEvent // HCS 事件详情
}
```

---

*基于 hcsshim v0.14.1 源码*
