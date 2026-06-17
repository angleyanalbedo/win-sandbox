# win-sandbox

Windows 沙箱工具，基于 HCS (Host Compute Service) API 创建隔离容器环境。

## 功能

- 创建 Windows 进程隔离容器
- 在容器中执行命令并捕获输出
- 资源限制（CPU、内存）
- 网络隔离
- 共享目录挂载

## 前置条件

- Windows 11 Pro / Enterprise
- Docker Desktop（用于获取容器镜像层）
- 管理员权限

## 快速开始

```powershell
# 检查环境
powershell -ExecutionPolicy Bypass -File scripts\check_env.ps1

# 编译
go build -o win-sandbox.exe .

# 运行（需要管理员权限）
.\win-sandbox.exe
```

## 项目结构

```text
win-sandbox/
├── main.go                          # 入口
├── go.mod                           # Go 模块定义
├── pkg/
│   └── docker/
│       ├── layers.go                # Docker 镜像层查找函数
│       └── layers_test.go           # 测试
├── docs/
│   ├── vmcompute-api-reference.md   # vmcompute.dll API 参考
│   ├── hcsshim-api-layers.md        # hcsshim API 分层参考
│   ├── docker-image-storage.md      # Docker 镜像存储结构
│   └── troubleshooting.md           # 问题排查记录
├── scripts/
│   ├── README.md                    # 脚本说明
│   ├── check_env.ps1                # 环境诊断
│   ├── check_layers.ps1             # 系统层检查
│   ├── check_docker_layers.ps1      # Docker 层检查
│   ├── find_docker_layers.ps1       # 查找 Docker 层
│   └── export_layers.ps1            # 导出层
└── hcsshim/                         # hcsshim 源码（git submodule，参考用）
```

## 技术方案

### 层存储

Windows 上有两套路层存储（互不兼容）：

| 存储位置 | 管理者 | hcsshim 兼容 |
| --- | --- | --- |
| `C:\ProgramData\Microsoft\Windows\Containers\Layers\` | Windows Servicing Stack | 不兼容 |
| `C:\ProgramData\Docker\windowsfilter\` | WCIFS filter driver | 兼容 |

hcsshim 的层操作 API 只能操作 WCIFS filter driver 管理的层。
系统自带的层缺少 filter driver 元数据，无法直接使用。

### 容器创建流程

```text
1. CreateScratchLayer → 创建 scratch 目录和 sandbox.vhdx
2. ActivateLayer → 激活 scratch 层
3. PrepareLayer → 准备 scratch 层（传入基础层作为父层）
4. GetLayerMountPath → 获取 volume GUID path
5. CreateContainer → 创建容器（设置 VolumePath 和 LayerFolderPath）
6. Start → 启动容器
7. CreateProcess → 在容器中执行命令
```

### 依赖

- [hcsshim](https://github.com/microsoft/hcsshim) — Microsoft 的 HCS Go 封装
- [cobra](https://github.com/spf13/cobra) — CLI 框架
- [logrus](https://github.com/sirupsen/logrus) — 日志库

## 文档

- [vmcompute API 参考](docs/vmcompute-api-reference.md) — Windows HCS 底层 API 完整参考
- [hcsshim API 分层](docs/hcsshim-api-layers.md) — hcsshim 库的四层架构分析
- [hcsshim cmd 工具](docs/hcsshim-cmd-reference.md) — hcsshim 仓库中各工具的用途说明
- [Docker 镜像存储](docs/docker-image-storage.md) — Docker 在 Windows 上的镜像存储结构
- [问题排查记录](docs/troubleshooting.md) — 开发过程中遇到的问题和解决方案

## 许可证

MIT
