## win-sandbox — Windows Sandbox 的开源 Go 实现

使用 Microsoft HCS (Host Compute System) API 和 [hcsshim](https://github.com/microsoft/hcsshim) Go 库，
创建轻量 Hyper-V VM 或 Windows 容器，在隔离环境中安全执行程序，完成后一次性销毁。

**与 Windows Sandbox 使用同一套底层 API。**

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

### 安装

```bash
# 从源码编译
go build -o wsandbox-vm.exe -ldflags="-s -w" .

# 或下载 Release
# https://github.com/angleyanalbedo/win-sandbox/releases
```

### 快速开始

```powershell
# 1. 检查环境
wsandbox-vm check

# 2. 在轻量 VM 中执行程序
wsandbox-vm run -- cmd.exe /c echo Hello World

# 3. 限制资源 + 启用网络
wsandbox-vm run -m 256 -c 1 --network -t 30s -- program.exe

# 4. 共享目录
wsandbox-vm run -s C:\data:C:\data -- dir C:\data

# 5. 查看生成的配置（不执行，调试用）
wsandbox-vm show-config --sandbox-type hyperv
```

### 架构

```
┌─ Windows Host ──────────────────────────────────────────┐
│                                                          │
│  wsandbox-vm (Go + hcsshim)                             │
│  │                                                       │
│  ├── hcsshim.CreateContainer() → 创建 compute system     │
│  ├── container.Start()         → 启动 VM / 容器          │
│  ├── container.CreateProcess() → 在隔离环境内执行程序     │
│  ├── process.Stdio()           → 捕获 stdin/stdout/stderr│
│  ├── process.Wait()            → 等待退出                │
│  └── container.Terminate()     → 销毁一切                │
│                                                          │
│  ┌─ Hyper-V 轻量 VM / Container ──────────────────────┐ │
│  │  独立内核 + 独立文件系统视图                         │ │
│  │  ┌─ 你的程序 ────────────────────────────────────┐  │ │
│  │  │  完全隔离，碰不到 Host                         │  │ │
│  │  └────────────────────────────────────────────────┘  │ │
│  │  关闭即销毁，不留痕迹                                │ │
│  └──────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────┘
```

### 三种沙箱模式

```powershell
# 1. Hyper-V VM（最强隔离，最接近 Windows Sandbox）
wsandbox-vm run --sandbox-type hyperv -- program.exe

# 2. Windows Container（进程级隔离，更快启动）
wsandbox-vm run --sandbox-type container -- program.exe

# 3. Linux Container（通过 WSL2 Hyper-V 后端）
wsandbox-vm run --sandbox-type linux -- /bin/ls
```

### 项目结构

```
win-sandbox/
├── main.go                    # 程序入口
├── cmd/
│   ├── root.go               # 根命令定义
│   ├── run.go                # run 子命令
│   ├── check.go              # check 子命令
│   └── showconfig.go         # show-config 子命令
├── pkg/
│   └── sandbox/
│       ├── sandbox.go        # 沙箱生命周期管理
│       ├── config.go         # HCS 配置生成
│       └── detect.go         # 基础镜像/内核检测
├── go.mod
├── LICENSE
└── README.md
```

### 技术栈

- **[hcsshim](https://github.com/microsoft/hcsshim)** — 微软官方 HCS Go 库
- **[cobra](https://github.com/spf13/cobra)** — CLI 框架
- **[logrus](https://github.com/sirupsen/logrus)** — 日志库

### 参考

- [hcsshim](https://github.com/microsoft/hcsshim) — 微软官方的 HCS Go 绑定（Apache-2.0）
- [HCS Schema](https://learn.microsoft.com/en-us/virtualization/api/hcs/resourceschemaversion2) — 配置 Schema 文档
- [Windows Sandbox 架构](https://learn.microsoft.com/en-us/windows/security/application-security/application-isolation/windows-sandbox/windows-sandbox-architecture)

### License

MIT
