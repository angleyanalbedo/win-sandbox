# Windows 容器层管理原理

## 概述

Windows 容器的文件系统隔离基于**分层存储**（Layered Storage）。每一层是只读的文件系统快照，多个层叠加形成容器看到的完整文件系统。

## 层的结构

一个 Windows 容器层在磁盘上包含：

```
<layer目录>/
├── Files/           # 文件系统内容（对应容器内的 C:\）
│   ├── Windows/
│   ├── Program Files/
│   └── ...
├── Hives/           # 注册表配置单元
│   ├── DEFAULT
│   ├── SOFTWARE
│   ├── SYSTEM
│   └── ...
├── UtilityVM/       # Hyper-V 隔离时的轻量 VM 镜像（可选）
│   └── Files/
└── layerchain.json  # 父层链（JSON 数组，基础层为 null）
```

### Files/ 目录

容器内进程看到的 `C:\` 文件系统。WCIFS（Windows Container Isolation File System）将多个层的 `Files/` 目录虚拟合并为一个统一视图。

### Hives/ 目录

Windows 注册表配置单元。容器内的注册表操作通过这些 hive 文件实现隔离。

### layerchain.json

记录该层的所有父层路径，从最底层（base）到当前层：

```json
[
  "C:\\ProgramData\\Docker\\windowsfilter\\abc123...",
  "C:\\ProgramData\\Docker\\windowsfilter\\def456..."
]
```

基础层的 `layerchain.json` 为 `null` 或空数组。

---

## 两种层格式

### 1. 文件夹格式（Folder Format）

层以目录形式存储在磁盘上，是 Windows 的原生格式。

```
C:\ProgramData\Docker\windowsfilter\<cacheID>\
├── Files\
├── Hives\
└── layerchain.json
```

**特点：**
- 直接操作目录，速度快
- 需要管理员权限（操作 Windows Filter Driver）
- Docker 本地存储使用此格式

### 2. Tar 流格式（OCI Layer Tar）

层以 OCI 标准的 tar 归档格式存储，用于网络传输和镜像分发。

```
layer.tar (OCI tar)
├── Files/Windows/System32/...    # 文件
├── Files/Windows/System32/...    # 文件
├── Hives/...                     # 注册表 hive
└── .wh.filename                  # 白障文件（标记删除）
```

**特点：**
- 标准格式，可用于网络传输
- OCI 镜像的每一层都是一个 tar
- 导入时需要解压并写入 Filter Driver

---

## hcsshim 层管理 API（三套）

hcsshim 提供了三套层管理 API，分别面向不同的使用场景。

### API 架构关系

```
hcsshim/pkg/ociwclayer          ← OCI 标准 tar 格式
    │   ImportLayerFromTar()
    │   ExportLayerToTar()
    │
    ├─ 内部调用 ──────────────────┐
    │                             ▼
hcsshim/computestorage         ← Windows 新版存储 API
    │   ImportLayer()              (直接调用 computestorage.dll)
    │   SetupBaseOSLayer()
    │   AttachLayerStorageFilter()
    │
    ├─ 内部调用 ──────────────────┐
    │                             ▼
hcsshim (根包)                  ← 旧版 API（兼容层）
    │   ImportLayer()              (内部转调 wclayer)
    │   ActivateLayer()
    │   PrepareLayer()
    │
    └─ 内部调用 ──────────────────┐
                                  ▼
内部 wclayer 包                 ← 实际调用 Windows Filter Driver
    wclayer.ImportLayer()          (调用 computestorage.dll 或 wclayer.dll)
    wclayer.NewLayerWriter()
```

### 第一套：`hcsshim`（根包）— 旧版 API

最常用的层操作 API，Docker 和 runhcs 都在使用。

```go
import "github.com/Microsoft/hcsshim"

// DriverInfo 指定层存储目录
info := hcsshim.DriverInfo{
    Flavour: 1,                              // 1 = windowsfilter
    HomeDir: "C:\\ProgramData\\Docker\\windowsfilter",
}
```

#### 文件夹格式导入导出

```go
// 从文件夹导入层
hcsshim.ImportLayer(info, layerID, importFolderPath, parentLayerPaths)

// 导出层到文件夹
hcsshim.ExportLayer(info, layerID, exportFolderPath, parentLayerPaths)
```

#### 层生命周期

```go
// 创建空读写层（基于父层）
hcsshim.CreateLayer(info, id, parent)

// 创建 scratch 层（容器的可写层）
hcsshim.CreateScratchLayer(info, layerId, "", parentLayerPaths)

// 销毁层
hcsshim.DestroyLayer(info, id)
```

#### 激活/挂载（WCIFS 操作）

```go
// 激活层（让 Filter Driver 管理该层）
hcsshim.ActivateLayer(info, id)

// 停用层
hcsshim.DeactivateLayer(info, id)

// 准备层（WCIFS 合并多层为统一视图）
hcsshim.PrepareLayer(info, layerId, parentLayerPaths)

// 取消准备
hcsshim.UnprepareLayer(info, layerId)
```

#### 查询

```go
// 检查层是否存在
exists, err := hcsshim.LayerExists(info, id)

// 获取 volume 挂载路径（PrepareLayer 后可用）
volumePath, err := hcsshim.GetLayerMountPath(info, id)

// 获取共享基础镜像信息
imageData, err := hcsshim.GetSharedBaseImages()
```

#### 流式读写器

```go
// 创建层读取器（逐文件读取层内容）
reader, err := hcsshim.NewLayerReader(info, layerID, parentLayerPaths)
defer reader.Close()

for {
    name, size, fileInfo, err := reader.Next()
    if err == io.EOF { break }
    // 读取文件内容...
}

// 创建层写入器（逐文件写入层内容）
writer, err := hcsshim.NewLayerWriter(info, layerID, parentLayerPaths)
defer writer.Close()

writer.Add(name, fileInfo)     // 添加文件
writer.Write(data)             // 写入内容
writer.Remove(name)            // 标记删除
```

#### 工具函数

```go
// 处理基础层（初始化 Files/ 和 Hives/ 结构）
hcsshim.ProcessBaseLayer(path)

// 处理 UtilityVM 镜像
hcsshim.ProcessUtilityVMImage(path)

// 将目录转换为基础层格式
hcsshim.ConvertToBaseLayer(path)

// 扩展 scratch 层大小
hcsshim.ExpandScratchSize(info, id, sizeInBytes)
```

---

### 第二套：`hcsshim/computestorage` — 新版存储 API

Windows 新引入的存储 API，提供更多底层控制能力。

```go
import "github.com/Microsoft/hcsshim/computestorage"
```

#### 文件夹格式导入导出（新版）

```go
// 导入层（带 context 和结构化参数）
layerData := computestorage.LayerData{
    SchemaVersion: computestorage.Version{Major: 1},
    Layers:        []computestorage.Layer{{Path: parentPath}},
    FilterType:    "wcifs",
}
computestorage.ImportLayer(ctx, layerPath, sourceFolderPath, layerData)

// 导出层
options := computestorage.ExportLayerOptions{IsWritableLayer: false}
computestorage.ExportLayer(ctx, layerPath, exportFolderPath, layerData, options)

// 销毁层
computestorage.DestroyLayer(ctx, layerPath)
```

#### 基础 OS 层设置

```go
// 设置基础 OS 层（容器模式）
opts := computestorage.OsLayerOptions{
    Type: computestorage.OsLayerTypeContainer,
}
computestorage.SetupBaseOSLayer(ctx, layerPath, vhdHandle, opts)

// 设置基础 OS 卷
computestorage.SetupBaseOSVolume(ctx, layerPath, volumePath, opts)
```

#### 可写层管理

```go
// 初始化可写层
computestorage.InitializeWritableLayer(ctx, writableLayerPath, layerData, options)

// 格式化可写层 VHD
computestorage.FormatWritableLayerVhd(ctx, vhdHandle)
```

#### Filter 管理

```go
// 挂载存储过滤器（WCIFS）
computestorage.AttachLayerStorageFilter(ctx, layerPath, layerData)

// 卸载存储过滤器
computestorage.DetachLayerStorageFilter(ctx, layerPath)

// 挂载 overlay 过滤器
computestorage.AttachOverlayFilter(ctx, volumePath, layerData)

// 卸载 overlay 过滤器
computestorage.DetachOverlayFilter(ctx, volumePath)
```

#### VHD 挂载

```go
// 获取 VHD 挂载路径
mountPath, err := computestorage.GetLayerVhdMountPath(ctx, vhdHandle)
```

---

### 第三套：`hcsshim/pkg/ociwclayer` — OCI Tar 流 API

专门处理 OCI 标准 tar 格式的导入导出，Docker 在拉取/推送镜像时使用。

```go
import "github.com/Microsoft/hcsshim/pkg/ociwclayer"
```

#### 从 tar 流导入层

```go
// 打开 tar 文件
tarFile, _ := os.Open("nanoserver-base.tar")
defer tarFile.Close()

// 导入到指定路径
layerPath := "C:\\my-layers\\base-layer"
parentPaths := []string{}  // 基础层无父层

size, err := ociwclayer.ImportLayerFromTar(
    context.Background(),
    tarFile,           // io.Reader（tar 流）
    layerPath,         // 目标目录
    parentLayers,      // 父层路径列表
)
// 返回值: 导入的字节数
```

#### 导出层为 tar 流

```go
// 创建输出 tar 文件
tarFile, _ := os.Create("exported-layer.tar")
defer tarFile.Close()

err := ociwclayer.ExportLayerToTar(
    context.Background(),
    tarFile,           // io.Writer（tar 流）
    layerPath,         // 要导出的层路径
    parentLayers,      // 父层路径列表
)
```

#### 白障文件（Whiteout）

OCI tar 使用白障文件标记删除的文件：

```
Files/Windows/old-file.txt       ← 存在的文件
Files/Windows/.wh.deleted.txt    ← 白障：表示 deleted.txt 已被删除
```

`ImportLayerFromTar` 会自动解析 `.wh.` 前缀，调用 `writer.Remove()` 标记删除。

---

## 三套 API 完整对照表

| 能力 | hcsshim（根包） | computestorage | ociwclayer |
|------|---------------|----------------|------------|
| 文件夹导入导出 | ✅ | ✅ | ❌ |
| Tar 流导入导出 | ❌ | ❌ | ✅ |
| 创建空层 | ✅ `CreateLayer()` | ❌ | ❌ |
| 创建 Scratch | ✅ `CreateScratchLayer()` | ❌ | ❌ |
| 销毁层 | ✅ `DestroyLayer()` | ✅ `DestroyLayer()` | ❌ |
| 激活/停用层 | ✅ | ❌ | ❌ |
| Prepare/Unprepare | ✅ | ❌ | ❌ |
| 设置基础 OS 层 | ❌ | ✅ `SetupBaseOSLayer()` | ❌ |
| Filter 管理 | ❌ | ✅ `Attach/Detach*()` | ❌ |
| VHD 管理 | ❌ | ✅ `GetLayerVhdMountPath()` | ❌ |
| 流式读写器 | ✅ `NewLayerReader/Writer()` | ❌ | ❌ |
| 需要 DriverInfo | ✅ | ❌（用路径） | ❌（用路径） |
| 需要 context | ❌ | ✅ | ✅ |

---

## Docker Pull 的完整流程

### OCI 镜像格式

一个 OCI 镜像由以下部分组成：

```
镜像 (Image)
├── manifest.json          # 镜像清单，描述各层和配置
├── config.json            # 镜像配置（环境变量、CMD 等）
├── <layer1>.tar.gz        # 第 1 层（基础层）的 tar 压缩包
├── <layer2>.tar.gz        # 第 2 层
├── <layer3>.tar.gz        # 第 3 层
└── ...
```

### Registry API

Docker Registry 使用 HTTP API 传输镜像：

```
┌─────────────┐          ┌─────────────────┐
│ Docker CLI   │          │ Registry         │
│ (docker pull)│          │ (MCR/Docker Hub) │
└──────┬──────┘          └────────┬────────┘
       │                          │
       │  1. GET /v2/<name>/manifests/<tag>
       │ ─────────────────────────►
       │  ◄─────────────────────────
       │  返回 manifest.json（列出各层 digest）
       │
       │  2. GET /v2/<name>/blobs/<config-digest>
       │ ─────────────────────────►
       │  ◄─────────────────────────
       │  返回 config.json
       │
       │  3. GET /v2/<name>/blobs/<layer1-digest>
       │ ─────────────────────────►
       │  ◄─────────────────────────
       │  返回 layer1.tar.gz
       │
       │  4. GET /v2/<name>/blobs/<layer2-digest>
       │ ─────────────────────────►
       │  ◄─────────────────────────
       │  返回 layer2.tar.gz
       │  ...
```

### Docker 的层存储

下载完成后，Docker 将层解压到本地存储：

```
C:\ProgramData\Docker\windowsfilter\
├── <cacheID-1>/           # 第 1 层（基础层）
│   ├── Files\
│   ├── Hives\
│   └── layerchain.json    # null（无父层）
├── <cacheID-2>/           # 第 2 层
│   ├── Files\
│   ├── Hives\
│   └── layerchain.json    # ["<cacheID-1>"]
├── <cacheID-3>/           # 第 3 层
│   ├── Files\
│   ├── Hives\
│   └── layerchain.json    # ["<cacheID-1>", "<cacheID-2>"]
└── repositories.json      # 镜像名 → 层 ID 的映射
```

### Docker Pull 内部调用链

```
docker pull nanoserver:ltsc2022
    │
    ├─ 解析镜像名 → registry.microsoft.com/windows/nanoserver:ltsc2022
    │
    ├─ HTTP GET manifest → 获取各层 digest 列表
    │
    ├─ 对每一层:
    │   ├─ HTTP GET blobs/<digest> → 下载 layer.tar.gz
    │   ├─ 解压 gzip → layer.tar
    │   ├─ 解析 tar → 逐文件提取
    │   │
    │   └─ ociwclayer.ImportLayerFromTar(tar, layerPath, parentPaths)
    │       ├─ wclayer.NewLayerWriter()        ← 创建 Filter Driver 写入器
    │       ├─ 对 tar 中每个文件:
    │       │   ├─ writer.Add(name, fileInfo)  ← 添加文件元数据
    │       │   ├─ writer.Write(data)          ← 写入文件内容
    │       │   └─ 或 writer.Remove(name)      ← 白障文件 → 标记删除
    │       └─ writer.Close()                  ← 提交层到 Filter Driver
    │
    ├─ 更新 repositories.json（映像名 → 层 ID）
    │
    └─ 完成
```

---

## 层的创建方式

### 方式一：从 Registry 拉取（Docker Pull）

```
Registry (tar.gz) → 下载 → 解压 → ImportLayerFromTar() → 磁盘层
```

### 方式二：从本地 tar 导入

```go
tarFile, _ := os.Open("nanoserver-base.tar")
defer tarFile.Close()

layerPath := "C:\\my-layers\\base-layer"
parentPaths := []string{}  // 基础层无父层

size, err := ociwclayer.ImportLayerFromTar(
    context.Background(),
    tarFile,
    layerPath,
    parentPaths,
)
```

### 方式三：从 Docker 目录直接使用

```
Docker 已下载的层 → 直接读取 C:\ProgramData\Docker\windowsfilter\<cacheID>\
```

### 方式四：创建空的读写层（Scratch Layer）

```go
// 创建一个新的空层（用于容器写入）
hcsshim.CreateScratchLayer(driverInfo, scratchID, "", parentLayerPaths)
```

---

## 容器创建中层的使用流程

```
1. 准备基础层（只读）
   │  方式 A: ImportLayerFromTar(tar)
   │  方式 B: 直接使用 Docker 目录
   │
2. 创建 Scratch 层（读写）
   │  CreateScratchLayer(driverInfo, scratchID, "", [basePath])
   │
3. 激活 Scratch 层
   │  ActivateLayer(driverInfo, scratchID)
   │
4. 准备层（WCIFS 合并基础层 + scratch）
   │  PrepareLayer(driverInfo, scratchID, [basePath])
   │
5. 获取挂载路径
   │  volumePath = GetLayerMountPath(driverInfo, scratchID)
   │
6. 创建容器
   │  CreateContainer(id, ContainerConfig{
   │      VolumePath:      volumePath,     // 合并后的路径
   │      LayerFolderPath: scratchPath,    // scratch 层路径
   │      Layers:          [{Path: basePath}],  // 基础层（只读）
   │  })
   │
7. 启动容器
      container.Start()
      // 容器内进程看到的是合并后的文件系统
```

---

## win-sandbox 当前方案与改进方向

### 当前方案（依赖 Docker）

```
win-sandbox
    │
    ├─ docker.FindImageLayers()     ← 读 Docker 的 windowsfilter 目录
    │   └─ 解析 repositories.json → 找到 cacheID → 拼接路径
    │
    ├─ MountLayers()
    │   └─ CreateScratchLayer() + PrepareLayer()  ← 基于 Docker 目录的层
    │
    └─ CreateContainer()            ← 使用 Docker 目录的层路径
```

**限制：** 必须先 `docker pull`，依赖 Docker Desktop 运行。

### 改进方案（支持 tar 导入）

```
win-sandbox
    │
    ├─ 方式 A: 本地 tar 文件
    │   └─ ociwclayer.ImportLayerFromTar(tar, myLayerPath, parents)
    │
    ├─ 方式 B: 从 Registry 下载
    │   ├─ HTTP GET manifest
    │   ├─ HTTP GET layer blobs
    │   └─ ociwclayer.ImportLayerFromTar(tar, myLayerPath, parents)
    │
    ├─ 方式 C: 从 Docker 目录读取（兼容现有）
    │   └─ docker.FindImageLayers()
    │
    ├─ 层存储: C:\Users\<user>\.win-sandbox\layers\
    │   ├── <layer-id-1>\
    │   ├── <layer-id-2>\
    │   └── layers.json      ← 层索引
    │
    └─ MountLayers() + CreateContainer()  ← 统一使用自己的层路径
```

**优势：** 不依赖 Docker，可独立运行。

---

## 安全注意事项

1. **管理员权限**：操作 Windows Filter Driver（PrepareLayer、ImportLayer 等）需要管理员权限
2. **Backup/Restore 特权**：`ImportLayerFromTar` 需要进程具有 `SeBackupPrivilege` 和 `SeRestorePrivilege`
3. **层路径安全**：tar 中的文件路径必须经过验证，防止路径穿越攻击（hcsshim 内部使用 `safefile` 包处理）
