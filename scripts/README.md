# Scripts

Windows 容器环境诊断和工具脚本。

## 脚本列表

### check_env.ps1
检查 Windows 容器环境的完整状态。

检查项：
- Windows 版本
- Windows 容器功能是否启用
- Hyper-V 是否启用
- vmcompute 服务状态
- wcifs 驱动状态
- Docker 安装状态和模式
- Docker windowsfilter 目录
- 已安装的容器镜像

用法：
```powershell
# 需要管理员权限
powershell -ExecutionPolicy Bypass -File scripts\check_env.ps1
```

### check_layers.ps1
检查系统上已安装的容器镜像层。

检查项：
- C:\ProgramData\Microsoft\Windows\Containers\Layers\ 下的所有层
- 每个层是否包含 Files\ 和 Hives\ 目录
- 是否有 layerchain.json 文件

用法：
```powershell
powershell -ExecutionPolicy Bypass -File scripts\check_layers.ps1
```

### check_docker_layers.ps1
检查 Docker 的层存储目录。

检查项：
- C:\ProgramData\Docker\windowsfilter\ 下的层
- 每个层是否包含 Files\ 和 layerchain.json

用法：
```powershell
# 需要管理员权限（Docker windowsfilter 目录权限受限）
powershell -ExecutionPolicy Bypass -File scripts\check_docker_layers.ps1
```

### export_layers.ps1
从 Docker 导出容器镜像的文件系统。

功能：
- 创建临时容器
- 导出容器文件系统为 tar 文件
- 清理临时容器

用法：
```powershell
powershell -ExecutionPolicy Bypass -File scripts\export_layers.ps1
```

## 背景知识

Windows 上有两套路层存储（互不兼容）：

| 存储位置 | 管理者 | hcsshim 兼容 |
|---------|--------|-------------|
| Containers\Layers\ | Windows Servicing Stack | 不兼容 |
| Docker\windowsfilter\ | WCIFS filter driver | 兼容 |

系统自带的层（Containers\Layers\）缺少 filter driver 的元数据，hcsshim 的层操作 API 无法使用。

Docker 的层（Docker\windowsfilter\）是通过 filter driver API 创建的，hcsshim 可以正常操作。

详见项目根目录的 `docs/` 文档。
