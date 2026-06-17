# 检查 Windows 容器层存储目录
# 该目录由 HCS (Host Compute Service) 管理，存储 Windows 容器的基础层和差异层

# 遍历 Containers\Layers 目录下的所有层目录
Get-ChildItem 'C:\ProgramData\Microsoft\Windows\Containers\Layers' -Directory | ForEach-Object {
    # 检查每层目录下的 layerchain.json 文件，该文件记录层的依赖链
    $chainFile = Join-Path $_.FullName 'layerchain.json'
    $hasChain = Test-Path $chainFile

    # 输出当前层的名称
    Write-Host "=== $($_.Name) ==="

    # 检查是否包含 Files 目录（存放层的文件系统内容）
    Write-Host "HasFiles: $(Test-Path (Join-Path $_.FullName 'Files'))"

    # 检查是否包含 Hives 目录（存放注册表蜂巢文件）
    Write-Host "HasHives: $(Test-Path (Join-Path $_.FullName 'Hives'))"

    # 检查是否包含层链配置文件
    Write-Host "HasLayerChain: $hasChain"

    # 如果存在层链文件，输出其内容（显示层的依赖关系）
    if ($hasChain) {
        Write-Host "LayerChain:"
        Get-Content $chainFile
    }
    Write-Host ''
}
