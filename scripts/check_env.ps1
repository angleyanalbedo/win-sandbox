# 诊断 Windows 容器环境
# 本脚本检查系统是否满足运行 Windows 容器的各项条件

# 检查 Windows 操作系统版本信息
Write-Host "=== Windows 版本 ==="
(Get-ComputerInfo).OsName
(Get-ComputerInfo).WindowsVersion

# 检查 Windows 容器功能是否已启用（需要管理员权限）
Write-Host ""
Write-Host "=== Windows 容器功能 ==="
$containers = Get-WindowsOptionalFeature -Online -FeatureName Containers -ErrorAction SilentlyContinue
if ($containers) {
    Write-Host "状态: $($containers.State)"
} else {
    Write-Host "无法查询（需要管理员权限）"
}

# 检查 Hyper-V 虚拟化功能是否已启用
Write-Host ""
Write-Host "=== Hyper-V ==="
$hyperv = Get-WindowsOptionalFeature -Online -FeatureName Microsoft-Hyper-V -ErrorAction SilentlyContinue
if ($hyperv) {
    Write-Host "状态: $($hyperv.State)"
} else {
    Write-Host "无法查询（需要管理员权限）"
}

# 检查 vmcompute 服务（容器主机计算服务）的运行状态
Write-Host ""
Write-Host "=== vmcompute 服务 ==="
Get-Service vmcompute -ErrorAction SilentlyContinue | Select-Object Name, Status, StartType

# 检查 wcifs 驱动（Windows Container Isolation Filter Service）的运行状态
# 该驱动负责管理容器层的文件系统隔离
Write-Host ""
Write-Host "=== wcifs 驱动 ==="
Get-Service wcifs -ErrorAction SilentlyContinue | Select-Object Name, Status, StartType

# 检查 Docker 守护进程版本信息
Write-Host ""
Write-Host "=== Docker ==="
docker version 2>&1 | Select-String "Version|OS"

# 检查 Docker 运行信息，包括存储驱动类型和已有容器/镜像数量
Write-Host ""
Write-Host "=== Docker 信息 ==="
docker info 2>&1 | Select-String "OSType|Storage|Containers|Images"

# 检查 Docker 的 windowsfilter 目录，该目录存储所有镜像层和容器层
Write-Host ""
Write-Host "=== Docker windowsfilter 目录 ==="
if (Test-Path "C:\ProgramData\Docker\windowsfilter" -ErrorAction SilentlyContinue) {
    $count = (Get-ChildItem "C:\ProgramData\Docker\windowsfilter" -Directory -ErrorAction SilentlyContinue).Count
    Write-Host "层数: $count"
} else {
    Write-Host "目录不存在或无权访问"
}

# 列出所有已拉取的 Docker 容器镜像
Write-Host ""
Write-Host "=== 已安装容器镜像 ==="
docker images 2>&1
