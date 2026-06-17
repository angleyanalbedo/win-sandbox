# Export nanoserver image layers to a local directory
$exportDir = "C:\sandbox-layers"
if (!(Test-Path $exportDir)) { New-Item -ItemType Directory -Path $exportDir -Force }

# Create a temporary container from nanoserver (don't start it)
Write-Host "Creating temp container..."
docker create --name temp-export mcr.microsoft.com/windows/nanoserver:ltsc2022 cmd /c echo temp

# Export the container filesystem
Write-Host "Exporting container filesystem..."
docker export temp-export -o "$exportDir\nanoserver.tar"

# Clean up temp container
docker rm temp-export

Write-Host "Exported to $exportDir\nanoserver.tar"

# List the export
Get-ChildItem $exportDir
