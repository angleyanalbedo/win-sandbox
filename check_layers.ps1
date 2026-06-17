Get-ChildItem 'C:\ProgramData\Microsoft\Windows\Containers\Layers' -Directory | ForEach-Object {
    $chainFile = Join-Path $_.FullName 'layerchain.json'
    $hasChain = Test-Path $chainFile
    Write-Host "=== $($_.Name) ==="
    Write-Host "HasFiles: $(Test-Path (Join-Path $_.FullName 'Files'))"
    Write-Host "HasHives: $(Test-Path (Join-Path $_.FullName 'Hives'))"
    Write-Host "HasLayerChain: $hasChain"
    if ($hasChain) {
        Write-Host "LayerChain:"
        Get-Content $chainFile
    }
    Write-Host ''
}
