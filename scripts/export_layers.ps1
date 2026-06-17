# 将 nanoserver 镜像的文件系统导出到本地目录
# 通过创建临时容器并导出其文件系统来获取镜像的完整文件内容

# 定义导出目标目录
$exportDir = "C:\sandbox-layers"
# 如果导出目录不存在，则创建该目录
if (!(Test-Path $exportDir)) { New-Item -ItemType Directory -Path $exportDir -Force }

# 从 nanoserver 镜像创建一个临时容器（不启动，仅用于导出）
Write-Host "Creating temp container..."
docker create --name temp-export mcr.microsoft.com/windows/nanoserver:ltsc2022 cmd /c echo temp

# 导出容器的文件系统为 tar 归档文件
Write-Host "Exporting container filesystem..."
docker export temp-export -o "$exportDir\nanoserver.tar"

# 删除临时容器，清理资源
docker rm temp-export

# 输出导出结果路径
Write-Host "Exported to $exportDir\nanoserver.tar"

# 列出导出目录中的文件，确认导出成功
Get-ChildItem $exportDir
