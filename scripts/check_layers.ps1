# 检查系统上已安装的容器镜像层
# 扫描 C:\ProgramData\Microsoft\Windows\Containers\Layers 目录

$layersHome = "C:\ProgramData\Microsoft\Windows\Containers\Layers"

Write-Host "=== System Container Layers ==="
Write-Host "Path: $layersHome"
Write-Host ""

Get-ChildItem $layersHome -Directory -ErrorAction SilentlyContinue | ForEach-Object {
    $layerPath = $_.FullName
    # Files: 容器文件系统（C:\ 盘内容）
    $hasFiles = Test-Path (Join-Path $layerPath "Files")
    # Hives: 注册表文件
    $hasHives = Test-Path (Join-Path $layerPath "Hives")
    # layerchain.json: 层依赖链
    $hasChain = Test-Path (Join-Path $layerPath "layerchain.json")

    Write-Host "=== $($_.Name) ==="
    Write-Host "  Files: $hasFiles"
    Write-Host "  Hives: $hasHives"
    Write-Host "  LayerChain: $hasChain"

    if ($hasChain) {
        Write-Host "  Chain:"
        Get-Content (Join-Path $layerPath "layerchain.json")
    }
    Write-Host ""
}
