# 诊断 Windows 容器环境
Write-Host "=== Windows 版本 ==="
(Get-ComputerInfo).OsName
(Get-ComputerInfo).WindowsVersion

Write-Host ""
Write-Host "=== Windows 容器功能 ==="
$containers = Get-WindowsOptionalFeature -Online -FeatureName Containers -ErrorAction SilentlyContinue
if ($containers) {
    Write-Host "状态: $($containers.State)"
} else {
    Write-Host "无法查询（需要管理员权限）"
}

Write-Host ""
Write-Host "=== Hyper-V ==="
$hyperv = Get-WindowsOptionalFeature -Online -FeatureName Microsoft-Hyper-V -ErrorAction SilentlyContinue
if ($hyperv) {
    Write-Host "状态: $($hyperv.State)"
} else {
    Write-Host "无法查询（需要管理员权限）"
}

Write-Host ""
Write-Host "=== vmcompute 服务 ==="
Get-Service vmcompute -ErrorAction SilentlyContinue | Select-Object Name, Status, StartType

Write-Host ""
Write-Host "=== wcifs 驱动 ==="
Get-Service wcifs -ErrorAction SilentlyContinue | Select-Object Name, Status, StartType

Write-Host ""
Write-Host "=== Docker ==="
docker version 2>&1 | Select-String "Version|OS"

Write-Host ""
Write-Host "=== Docker 信息 ==="
docker info 2>&1 | Select-String "OSType|Storage|Containers|Images"

Write-Host ""
Write-Host "=== Docker windowsfilter 目录 ==="
if (Test-Path "C:\ProgramData\Docker\windowsfilter" -ErrorAction SilentlyContinue) {
    $count = (Get-ChildItem "C:\ProgramData\Docker\windowsfilter" -Directory -ErrorAction SilentlyContinue).Count
    Write-Host "层数: $count"
} else {
    Write-Host "目录不存在或无权访问"
}

Write-Host ""
Write-Host "=== 已安装容器镜像 ==="
docker images 2>&1
