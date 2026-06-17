# 检查 Docker 层存储目录
# 该目录存储 Docker 管理的所有镜像层和容器层，位于 windowsfilter 下
# 需要管理员权限才能访问

# 输出标题
Write-Host "=== Docker layer store ==="

# 获取 windowsfilter 目录下的所有子目录（每个子目录代表一个层）
$layers = Get-ChildItem "C:\ProgramData\Docker\windowsfilter" -Directory -ErrorAction SilentlyContinue
if ($layers) {
    # 输出找到的层目录总数
    Write-Host "Found $($layers.Count) layer directories"

    # 遍历每个层目录，检查其内容
    foreach ($layer in $layers) {
        # 检查是否包含 Files 目录（层的文件系统内容）
        $hasFiles = Test-Path (Join-Path $layer.FullName "Files")
        # 检查是否包含 layerchain.json 文件（层的依赖链配置）
        $hasChain = Test-Path (Join-Path $layer.FullName "layerchain.json")

        # 只输出包含 Files 目录的有效层
        if ($hasFiles) {
            Write-Host "  $($layer.Name) Files:$hasFiles Chain:$hasChain"
        }
    }
} else {
    # 无法访问目录，提示需要管理员权限
    Write-Host "Cannot access - need admin"
}
