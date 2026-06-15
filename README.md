## win-sandbox — Windows Sandbox 的开源 Rust 实现

使用 Windows HCS (Host Compute System) API 创建轻量 Hyper-V VM，
在 VM 内安全执行 .exe，完成后一次性销毁。

**这就是 Windows Sandbox 内部使用的同一套 API（vmcompute.dll / hcsshim）。**

### 前提条件

```
✓ Windows 10/11 Pro 或 Enterprise（Home 版不支持）
✓ Hyper-V 功能已启用
✓ 以管理员权限运行
```

启用 Hyper-V（管理员 PowerShell）：

```powershell
dism /online /enable-feature /featurename:Microsoft-Hyper-V /all
# 需要重启
```

### 快速开始

```powershell
# 1. 检查环境
wsandbox-vm check

# 2. 在轻量 VM 中执行 exe
wsandbox-vm run C:\path\to\program.exe arg1 arg2

# 3. 限制资源 + 启用网络
wsandbox-vm run --memory 256 --cpus 1 --network --timeout 30 program.exe

# 4. 查看生成的 HCS 配置（不执行，调试用）
wsandbox-vm show-config --sandbox-type hyperv
```

### 架构

```
┌─ Windows Host ──────────────────────────────────────┐
│                                                      │
│  wsandbox-vm (Rust)                                  │
│  │                                                   │
│  ├── HCS API (vmcompute.dll)                         │
│  │   ├── HcsCreateComputeSystem   → 创建轻量 VM      │
│  │   ├── HcsStartComputeSystem    → 启动 VM          │
│  │   ├── HcsExecuteProcess        → 在 VM 内跑 exe   │
│  │   ├── HcsWaitForProcess        → 等待结果         │
│  │   └── HcsTerminateComputeSystem → 销毁 VM         │
│  │                                                   │
│  └── 配置生成器 (config.rs)                          │
│      ├── Hyper-V VM 配置                             │
│      ├── Windows Container 配置                      │
│      └── Linux Container 配置                        │
│                                                      │
│  ┌─ Hyper-V 轻量 VM ─────────────────────────────┐  │
│  │  独立内核 + 独立文件系统视图                     │  │
│  │  ┌─ 你的 .exe ──────────────────────────────┐  │  │
│  │  │  完全隔离，碰不到 Host                     │  │  │
│  │  └──────────────────────────────────────────┘  │  │
│  │  关闭即销毁，不留痕迹                           │  │
│  └────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────┘
```

### 三种沙箱模式

```powershell
# 1. Hyper-V VM（最强隔离，最接近 Windows Sandbox）
wsandbox-vm run --sandbox-type hyperv program.exe

# 2. Windows Container（进程级隔离，更快启动）
wsandbox-vm run --sandbox-type container program.exe

# 3. Linux Container（通过 WSL2 Hyper-V 后端）
wsandbox-vm run --sandbox-type linux /bin/ls
```

### 项目结构

```
win-sandbox/
├── Cargo.toml
├── src/
│   ├── main.rs      # CLI 入口（clap 子命令）
│   ├── hcs.rs       # HCS API FFI 绑定（vmcompute.dll）
│   ├── config.rs    # HCS JSON 配置生成器
│   └── sandbox.rs   # VM 生命周期管理（创建→启动→执行→销毁）
```

### 工作原理

与 Windows Sandbox 完全相同的底层流程：

```
1. HcsCreateComputeSystem(config_json)
   → 创建一个轻量 Hyper-V VM
   → 使用差分磁盘（基础层共享 Host 系统文件，只存改动）
   → 内存可超分配（按需分配，不预占）

2. HcsStartComputeSystem()
   → 启动 VM（通常 2-5 秒）

3. HcsExecuteProcess(exe_path, args)
   → 在 VM 内启动进程
   → 通过管道连接 stdin/stdout/stderr

4. HcsWaitForProcessInComputeSystem(timeout)
   → 等待执行完成或超时
   → 收集退出码和执行统计

5. HcsTerminateComputeSystem()
   → 销毁 VM
   → 删除差分磁盘
   → 清理临时文件
   → 什么都不留
```

### 参考

- [hcsshim](https://github.com/microsoft/hcsshim) — 微软官方的 HCS Go 绑定（Apache-2.0）
- [HCS Schema](https://learn.microsoft.com/en-us/virtualization/api/hcs/resourceschemaversion2) — 配置 Schema 文档
- [Windows Sandbox 架构](https://learn.microsoft.com/en-us/windows/security/application-security/application-isolation/windows-sandbox/windows-sandbox-architecture)

### 局限性

- 需要 Windows Pro/Enterprise（Home 版不支持 Hyper-V）
- 需要管理员权限
- HCS API 不在公开 Win32 文档中，Schema 可能随 Windows 版本变化
- stdout/stderr 管道捕获需要额外实现（当前版本仅获取退出码）
