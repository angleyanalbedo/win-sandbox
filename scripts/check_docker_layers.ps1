Write-Host "=== Docker layer store ==="
$layers = Get-ChildItem "C:\ProgramData\Docker\windowsfilter" -Directory -ErrorAction SilentlyContinue
if ($layers) {
    Write-Host "Found $($layers.Count) layer directories"
    foreach ($layer in $layers) {
        $hasFiles = Test-Path (Join-Path $layer.FullName "Files")
        $hasChain = Test-Path (Join-Path $layer.FullName "layerchain.json")
        if ($hasFiles) {
            Write-Host "  $($layer.Name) Files:$hasFiles Chain:$hasChain"
        }
    }
} else {
    Write-Host "Cannot access - need admin"
}
