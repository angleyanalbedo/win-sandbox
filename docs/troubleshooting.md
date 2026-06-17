# 问题排查记录

开发 win-sandbox 过程中遇到的问题、原因、解决方案。

## 1. CreateScratchLayer 报错 0x3 (The system cannot find the path specified)

### 现象

```text
hcsshim::CreateScratchLayer failed in Win32: The system cannot find the path specified. (0x3)
```

### 原因

Windows 上有两套路层存储，互不兼容：

| 存储位置 | 管理者 | hcsshim 兼容 |
| --- | --- | --- |
| `C:\ProgramData\Microsoft\Windows\Containers\Layers\` | Windows Servicing Stack | 不兼容 |
| `C:\ProgramData\Docker\windowsfilter\` | WCIFS filter driver | 兼容 |

系统自带的层（Windows 容器功能安装时创建）没有经过 WCIFS filter driver 注册，
缺少 filter driver 的元数据，hcsshim 的层操作 API 无法识别这些层。

### 解决方案

使用 Docker 的层存储。Docker pull 镜像后，层存储在 `C:\ProgramData\Docker\windowsfilter\` 目录下，
由 WCIFS filter driver 管理，hcsshim 可以正常操作。

查找层路径的流程：

```text
repositories.json → 镜像 ID
imagedb/content/sha256/<镜像ID> → 层 diff ID 列表
layerdb/sha256/<diffID>/cache-id → 实际层目录名
C:\ProgramData\Docker\windowsfilter\<cache-id>\ → 层数据
```

代码实现：`pkg/docker/layers.go` 的 `FindImageLayers` 函数。

## 2. CreateContainer 报错 The parameter is incorrect

### 现象

```text
hcs::CreateComputeSystem sandbox-xxx: The parameter is incorrect.
```

层查找、scratch 创建、激活、挂载都成功了，但 CreateContainer 失败。

### 原因

ContainerConfig 的 `Layers[].ID` 字段格式错误。

最初使用 `filepath.Base(layerPath)` 作为 ID，得到的是目录名（hash 值）：

```text
ID: "d643449f9e57da1a0c840dc257019281c4e01d02f814b3fe4eea12087b492291"
```

但 HCS 期望的是从目录名通过 `NameToGuid` 算法生成的 GUID：

```text
ID: "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
```

### 解决方案

使用 `hcsshim.NewGUID(dirName)` 生成正确的 GUID 格式 ID：

```go
dirName := filepath.Base(layerPath)
guid := hcsshim.NewGUID(dirName)
hcsLayers := []hcsshim.Layer{
    {ID: guid.ToString(), Path: layerPath},
}
```

`NameToGuid` 是 hcsshim 内部用来将目录名转换为 GUID 的算法，HCS API 要求 Layer ID 必须是这个格式。

## 3. 关键配置字段说明

### VolumePath

- 由 `GetLayerMountPath` 返回
- 格式：`\\?\Volume{xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx}`
- 进程隔离容器必须设置
- hcsoci 代码会去掉末尾反斜杠后再传给 HCS

### LayerFolderPath

- scratch 层在 windowsfilter 目录中的完整路径
- 格式：`C:\ProgramData\Docker\windowsfilter\sandbox-xxxxxxxx`
- hcsoci 不使用此字段（被注释掉），但 hcsshim 测试代码使用

### Layers

- 只读镜像层列表（不含 scratch）
- ID 必须是 `NewGUID(dirName).ToString()` 格式
- Path 是层的完整路径

### HvPartition

- `false` = 进程隔离容器（Argon）
- `true` = Hyper-V 隔离容器（Xenon）

## 4. 正确的容器创建流程

```text
1. FindImageLayers(imageRef)     → 获取层路径
2. DriverInfo{Flavour:1, HomeDir:docker.LayerStore}
3. MkdirAll(scratchPath)         → 创建 scratch 目录
4. CreateScratchLayer(...)       → 创建 sandbox.vhdx
5. ActivateLayer(scratchID)      → 激活 scratch（不是基础层！）
6. PrepareLayer(scratchID, ...)  → 准备 scratch（传入基础层作为父层）
7. GetLayerMountPath(scratchID)  → 获取 volume GUID path
8. NewGUID(dirName)              → 生成正确的 Layer ID
9. CreateContainer(config)       → 创建容器
10. container.Start()            → 启动
11. container.CreateProcess(...) → 执行命令
```

## 5. 环境要求

- Windows 11 Pro / Enterprise
- Docker Desktop（用于获取镜像层）
- 管理员权限（Docker 数据目录 ACL 限制）
- vmcompute 服务运行中
- wcifs 驱动运行中

诊断脚本：`scripts/check_env.ps1`

---

*基于 win-sandbox 开发过程中的实际问题整理*
