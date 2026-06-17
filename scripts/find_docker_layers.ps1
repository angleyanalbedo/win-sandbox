# Find nanoserver image layers from Docker's storage
# Requires admin privileges
# 从 Docker 存储中查找 nanoserver 镜像的层信息（需要管理员权限）

# 目标镜像
$image = "mcr.microsoft.com/windows/nanoserver:ltsc2022"

Write-Host "=== Docker Image Layers ==="
Write-Host "Image: $image"
Write-Host ""

# 获取镜像 ID
$imageId = docker images -q $image 2>$null
if (!$imageId) {
    Write-Host "Image not found. Run: docker pull $image"
    exit 1
}
Write-Host "Image ID: $imageId"

# 获取镜像详情
Write-Host ""
Write-Host "=== Image Details ==="
docker inspect $image --format '{{.Id}}' 2>$null
docker inspect $image --format '{{.RootFS.Type}} {{.RootFS.Layers}}' 2>$null

# 列出层目录
Write-Host ""
Write-Host "=== Layer Directories ==="
$layersDir = "C:\ProgramData\Docker\windowsfilter"
$dirs = Get-ChildItem $layersDir -Directory -ErrorAction SilentlyContinue

if ($dirs) {
    Write-Host "Found $($dirs.Count) directories"
    foreach ($dir in $dirs) {
        # 检查 Files 目录（文件系统内容）
        $hasFiles = Test-Path (Join-Path $dir.FullName "Files")
        # 检查 layerchain.json（层依赖链）
        $hasChain = Test-Path (Join-Path $dir.FullName "layerchain.json")

        if ($hasFiles) {
            Write-Host "  $($dir.Name) Files:$hasFiles Chain:$hasChain"
            if ($hasChain) {
                $chain = Get-Content (Join-Path $dir.FullName "layerchain.json") -Raw
                Write-Host "    Chain: $chain"
            }
        }
    }
} else {
    # 无法访问，需要管理员权限
    Write-Host "Cannot access $layersDir - run as admin"
}
