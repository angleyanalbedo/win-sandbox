# 诊断 Windows 容器环境
# 检查系统是否满足运行 Windows 容器的各项条件

# 检查 Windows 版本
Write-Host "=== Windows Version ==="
(Get-ComputerInfo).OsName
(Get-ComputerInfo).WindowsVersion

# 检查 Windows 容器功能（需要管理员权限）
Write-Host ""
Write-Host "=== Windows Containers Feature ==="
$containers = Get-WindowsOptionalFeature -Online -FeatureName Containers -ErrorAction SilentlyContinue
if ($containers) {
    Write-Host "Status: $($containers.State)"
} else {
    Write-Host "Cannot query (need admin)"
}

# 检查 Hyper-V
Write-Host ""
Write-Host "=== Hyper-V ==="
$hyperv = Get-WindowsOptionalFeature -Online -FeatureName Microsoft-Hyper-V -ErrorAction SilentlyContinue
if ($hyperv) {
    Write-Host "Status: $($hyperv.State)"
} else {
    Write-Host "Cannot query (need admin)"
}

# 检查 vmcompute 服务（容器主机计算服务）
Write-Host ""
Write-Host "=== vmcompute Service ==="
Get-Service vmcompute -ErrorAction SilentlyContinue | Select-Object Name, Status, StartType

# 检查 wcifs 驱动（容器隔离过滤器服务）
Write-Host ""
Write-Host "=== wcifs Driver ==="
Get-Service wcifs -ErrorAction SilentlyContinue | Select-Object Name, Status, StartType

# 检查 Docker 版本
Write-Host ""
Write-Host "=== Docker ==="
docker version 2>&1 | Select-String "Version|OS"

# 检查 Docker 信息
Write-Host ""
Write-Host "=== Docker Info ==="
docker info 2>&1 | Select-String "OSType|Storage|Containers|Images"

# 检查 Docker windowsfilter 目录（存储所有镜像层）
Write-Host ""
Write-Host "=== Docker windowsfilter ==="
if (Test-Path "C:\ProgramData\Docker\windowsfilter" -ErrorAction SilentlyContinue) {
    $count = (Get-ChildItem "C:\ProgramData\Docker\windowsfilter" -Directory -ErrorAction SilentlyContinue).Count
    Write-Host "Layers: $count"
} else {
    Write-Host "Not found or no access"
}

# 列出已安装的容器镜像
Write-Host ""
Write-Host "=== Installed Images ==="
docker images 2>&1
