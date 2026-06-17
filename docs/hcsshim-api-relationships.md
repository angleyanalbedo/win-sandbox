# hcsshim API 调用关系

公开 API 之间的依赖关系和典型调用链。

## 1. 核心调用链

### 创建并运行容器（完整流程）

```text
FindImageLayers(imageRef)          ← pkg/docker（自定义）
    │
    ▼
DriverInfo{Flavour:1, HomeDir:...} ← 配置 filter driver
    │
    ▼
CreateScratchLayer(info, ...)      ← 创建 scratch 层
    │
    ▼
ActivateLayer(info, scratchID)     ← 激活 scratch
    │
    ▼
PrepareLayer(info, scratchID, ...) ← 准备 scratch（传入父层）
    │
    ▼
GetLayerMountPath(info, scratchID) ← 获取 volume path
    │
    ▼
NewGUID(dirName)                   ← 生成 Layer ID
    │
    ▼
CreateContainer(id, config)        ← 创建容器
    │
    ▼
container.Start()                  ← 启动容器
    │
    ▼
container.CreateProcess(config)    ← 创建进程
    │
    ▼
process.Stdio()                    ← 获取 stdin/stdout/stderr
    │
    ▼
process.Wait()                     ← 等待退出
process.ExitCode()                 ← 获取退出码
    │
    ▼
container.Shutdown()               ← 关闭容器
container.Terminate()              ← 终止容器
container.Close()                  ← 释放句柄
```

## 2. API 依赖关系图

```text
CreateContainer(id, *ContainerConfig)
    │
    ├── 依赖 ContainerConfig
    │   ├── Layers []Layer
    │   │   ├── ID   ← NewGUID(dirName).ToString()
    │   │   └── Path ← 层的完整路径
    │   ├── VolumePath ← GetLayerMountPath() 返回值
    │   ├── HvPartition (bool)
    │   └── HvRuntime (Hyper-V 隔离时需要)
    │
    └── 返回 Container 接口
        ├── Start()
        ├── CreateProcess(*ProcessConfig) → Process 接口
        │   ├── Stdio() → (stdin, stdout, stderr)
        │   ├── Wait()
        │   ├── ExitCode()
        │   └── Kill()
        ├── Shutdown()
        ├── Terminate()
        └── Close()

ActivateLayer(info, id)
PrepareLayer(info, id, parentPaths)
GetLayerMountPath(info, id)
    │
    └── 都依赖 DriverInfo
        ├── Flavour (int)
        └── HomeDir (string)

NewGUID(source) → *GUID
NameToGuid(name) → GUID
    │
    └── GUID.ToString() → string (xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx)
```

## 3. 层操作的依赖顺序

```text
CreateScratchLayer
    │
    ├── 必须先于 ActivateLayer
    │
    ▼
ActivateLayer
    │
    ├── 必须先于 PrepareLayer
    ├── 必须先于 GetLayerMountPath
    │
    ▼
PrepareLayer
    │
    ├── 必须在 ActivateLayer 之后
    ├── 依赖 parentLayerPaths（基础层路径）
    │
    ▼
GetLayerMountPath
    │
    ├── 必须在 ActivateLayer 之后
    ├── 返回 volume GUID path
    │
    ▼
（使用 volume path 创建容器）
    │
    ▼
UnprepareLayer
    │
    ├── 必须在容器关闭之后
    ├── 必须在 DeactivateLayer 之前
    │
    ▼
DeactivateLayer
    │
    ├── 必须在 UnprepareLayer 之后
    └── 最后调用
```

### 正确的调用顺序

```go
// 1. 创建 scratch
hcsshim.CreateScratchLayer(info, scratchID, "", []string{baseLayerPath})

// 2. 激活
hcsshim.ActivateLayer(info, scratchID)
defer hcsshim.DeactivateLayer(info, scratchID)

// 3. 准备
hcsshim.PrepareLayer(info, scratchID, []string{baseLayerPath})
defer hcsshim.UnprepareLayer(info, scratchID)

// 4. 获取 volume path
volumePath, _ := hcsshim.GetLayerMountPath(info, scratchID)

// 5. 创建容器
container, _ := hcsshim.CreateContainer(id, config)

// 6. 使用容器...
container.Start()
proc, _ := container.CreateProcess(...)
proc.Wait()

// 7. 清理（defer 自动反向执行）
container.Shutdown()
container.Terminate()
container.Close()
```

## 4. Container 接口方法的依赖关系

```text
CreateContainer
    │
    ▼
Start ──────────────────────────────┐
    │                               │
    ├── CreateProcess               │
    │   ├── Stdio()                 │
    │   ├── Wait()                  │
    │   ├── ExitCode()              │
    │   ├── Kill()                  │
    │   └── Close()                 │
    │                               │
    ├── OpenProcess(pid)            │
    │                               │
    ├── Pause ──► Resume            │
    │                               │
    ├── Statistics()                │
    ├── ProcessList()               │
    ├── Modify()                    │
    │                               │
    ▼                               │
Shutdown ─── Wait ──────────────────┘
    │
    ▼
Terminate ── Wait
    │
    ▼
Close
```

### 状态转换

```text
Created ──► Running ──► Stopped
              │
              ├──► Paused ──► Running
              │
              └──► Terminated
```

## 5. Process 接口方法的依赖关系

```text
CreateProcess
    │
    ├── Stdio() ──► (stdin, stdout, stderr)
    │   ├── stdin.Write()
    │   ├── stdout.Read()
    │   └── stderr.Read()
    │
    ├── Wait() ──► 阻塞直到进程退出
    │
    ├── ExitCode() ──► 必须在 Wait() 之后
    │
    ├── Kill() ──► 立即终止
    │
    ├── CloseStdin() ──► 通知进程没有更多输入
    │
    ├── ResizeConsole(w, h)
    │
    └── Close() ──► 释放句柄
```

## 6. DriverInfo 的作用范围

```text
DriverInfo
    │
    ├── ActivateLayer(info, ...)
    ├── DeactivateLayer(info, ...)
    ├── PrepareLayer(info, ...)
    ├── UnprepareLayer(info, ...)
    ├── GetLayerMountPath(info, ...)
    ├── CreateLayer(info, ...)
    ├── CreateScratchLayer(info, ...)
    ├── DestroyLayer(info, ...)
    ├── LayerExists(info, ...)
    ├── ExportLayer(info, ...)
    ├── ImportLayer(info, ...)
    ├── ExpandSandboxSize(info, ...)
    ├── ExpandScratchSize(info, ...)
    ├── NewLayerReader(info, ...)
    └── NewLayerWriter(info, ...)
```

所有层操作都依赖同一个 DriverInfo 实例。

## 7. 错误处理链

```text
HcsError
    ├── Op    ── 操作名（如 "hcs::CreateComputeSystem"）
    ├── Err   ── 原始 Windows 错误码
    └── Events ── HCS 事件详情

常见错误码：
    0x3   ── The system cannot find the path specified（层路径错误）
    0x5   ── Access denied（权限不足）
    0x8007000d ── The data is invalid（配置参数错误）
```

## 8. win-sandbox 的实际调用链

```text
main.go
    │
    ├── docker.FindImageLayers(imageRef)
    │   ├── getImageID(imageRef)
    │   ├── getImageDiffIDs(imageID)
    │   └── resolveLayer(diffID)
    │
    ├── hcsshim.DriverInfo{...}
    │
    ├── hcsshim.CreateScratchLayer(...)
    ├── hcsshim.ActivateLayer(...)
    ├── hcsshim.PrepareLayer(...)
    ├── hcsshim.GetLayerMountPath(...)
    │
    ├── hcsshim.NewGUID(dirName)
    │
    ├── hcsshim.CreateContainer(id, config)
    │   └── config.Layers[0].ID = guid.ToString()
    │
    ├── container.Start()
    ├── container.CreateProcess(...)
    │   └── process.Stdio()
    │
    ├── process.Wait()
    │
    ├── container.Shutdown()
    ├── container.Terminate()
    └── container.Close()
```

---

*基于 hcsshim v0.14.1 源码和 win-sandbox 实际开发经验*
