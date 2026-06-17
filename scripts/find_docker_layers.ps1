# 从 Docker 存储中查找 nanoserver 镜像的层信息
# 需要管理员权限才能访问 Docker 的 windowsfilter 目录

# 定义要查找的目标镜像
$image = "mcr.microsoft.com/windows/nanoserver:ltsc2022"

# 输出脚本标题和目标镜像名称
Write-Host "=== Docker Image Layers ==="
Write-Host "Image: $image"
Write-Host ""

# 获取镜像的短 ID，如果镜像不存在则提示用户拉取
$imageId = docker images -q $image 2>$null
if (!$imageId) {
    Write-Host "Image not found. Run: docker pull $image"
    exit 1
}
# 输出镜像 ID
Write-Host "Image ID: $imageId"

# 使用 docker inspect 获取镜像的详细信息，包括镜像 ID 和层结构
Write-Host ""
Write-Host "=== Image Details ==="
# 获取镜像的完整 ID
docker inspect $image --format '{{.Id}}' 2>$null
# 获取镜像的根文件系统类型和层哈希列表
docker inspect $image --format '{{.RootFS.Type}} {{.RootFS.Layers}}' 2>$null

# 列出 Docker windowsfilter 目录中的所有层目录
Write-Host ""
Write-Host "=== Layer Directories ==="
$layersDir = "C:\ProgramData\Docker\windowsfilter"
$dirs = Get-ChildItem $layersDir -Directory -ErrorAction SilentlyContinue
if ($dirs) {
    # 输出找到的层目录总数
    Write-Host "Found $($dirs.Count) directories"

    # 遍历每个层目录，检查其结构和内容
    foreach ($dir in $dirs) {
        # 检查是否包含 Files 目录（层的文件系统内容）
        $hasFiles = Test-Path (Join-Path $dir.FullName "Files")
        # 检查是否包含 layerchain.json 文件（层的依赖链配置）
        $hasChain = Test-Path (Join-Path $dir.FullName "layerchain.json")

        # 只处理包含 Files 目录的有效层
        if ($hasFiles) {
            Write-Host "  $($dir.Name) Files:$hasFiles Chain:$hasChain"

            # 如果存在层链文件，读取并输出其内容（显示层的依赖关系）
            if ($hasChain) {
                $chain = Get-Content (Join-Path $dir.FullName "layerchain.json") -Raw
                Write-Host "    Chain: $chain"
            }
        }
    }
} else {
    # 无法访问目录，提示需要以管理员身份运行
    Write-Host "Cannot access $layersDir - run as admin"
}
