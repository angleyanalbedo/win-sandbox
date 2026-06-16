package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DetectBaseImage 检测 Hyper-V 基础镜像（sandbox.vhdx 或 Windows Server 镜像）
func DetectBaseImage() (string, error) {
	candidates := []string{
		// Windows Sandbox 安装路径
		filepath.Join(os.Getenv("ProgramData"), "Microsoft", "Windows", "Containers", "BaseImages"),
		// ContainerStorages 路径
		filepath.Join(os.Getenv("ProgramData"), "Microsoft", "Windows", "Containers", "LayerPackages"),
		// Hyper-V 默认目录
		filepath.Join(os.Getenv("ProgramData"), "Microsoft", "Windows", "Hyper-V"),
		// 用户目录下的 ContainerStorages
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "Windows", "Containers"),
		// Docker Desktop 路径
		filepath.Join(os.Getenv("ProgramData"), "DockerDesktop"),
		// 系统盘默认
		`C:\ProgramData\Microsoft\Windows\Containers\BaseImages`,
	}

	for _, dir := range candidates {
		if dir == "" {
			continue
		}
		vhdx, err := findVHDX(dir)
		if err == nil && vhdx != "" {
			return vhdx, nil
		}
	}

	// 从 ContainerStorages 搜索最大的 VHDX 文件
	containerStorages := filepath.Join(os.Getenv("ProgramData"), "Microsoft", "Windows", "Containers", "ContainerStorages")
	if containerStorages != "" {
		vhdx, err := findLargestVHDX(containerStorages)
		if err == nil && vhdx != "" {
			return vhdx, nil
		}
	}

	return "", fmt.Errorf("未找到基础 VHDX 镜像，请确保已安装 Windows Sandbox 或 Windows 容器功能")
}

// DetectContainerLayers 检测 Windows 容器镜像层
func DetectContainerLayers() ([]string, error) {
	// 检查 Docker Desktop 层
	dockerPath := filepath.Join(os.Getenv("ProgramData"), "DockerDesktop", "vm-data")
	if _, err := os.Stat(dockerPath); err == nil {
		return []string{dockerPath}, nil
	}

	// 检查 Windows 容器基础镜像层
	baseImagePath := filepath.Join(os.Getenv("ProgramData"), "Microsoft", "Windows", "Containers", "BaseImages")
	if entries, err := os.ReadDir(baseImagePath); err == nil {
		for _, entry := range entries {
			layerPath := filepath.Join(baseImagePath, entry.Name())
			if info, err := os.Stat(layerPath); err == nil && info.IsDir() {
				// 检查是否包含 layer 文件
				if _, err := os.Stat(filepath.Join(layerPath, "layerchain.json")); err == nil {
					return []string{layerPath}, nil
				}
			}
		}
	}

	return nil, fmt.Errorf("未找到容器镜像层，请确保已安装 Windows 容器功能或 Docker Desktop")
}

// DetectWSLKernel 检测 WSL2 内核路径
func DetectWSLKernel() (string, error) {
	candidates := []string{
		// WSL2 内置内核
		filepath.Join(os.Getenv("SystemRoot"), "System32", "lxss", "tools", "kernel"),
		// Microsoft Store 版本的 WSL
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Packages", "MicrosoftCorporationII.WindowsSubsystemForLinux_8wekyb3d8bbwe", "LocalState", "kernel"),
		// WSL2 内核安装路径
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "WindowsApps", "kernel"),
		// 常见的 WSL2 内核路径
		`C:\Windows\System32\lxss\tools\kernel`,
	}

	for _, path := range candidates {
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
		// 检查带扩展名的变体
		if _, err := os.Stat(path + ".efi"); err == nil {
			return path + ".efi", nil
		}
	}

	return "", fmt.Errorf("未找到 WSL2 内核，请确保已安装 WSL2")
}

// findVHDX 在目录中查找 VHDX 文件
func findVHDX(dir string) (string, error) {
	var found string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // 跳过无法访问的目录
		}
		if !info.IsDir() && strings.EqualFold(filepath.Ext(path), ".vhdx") {
			found = path
			return filepath.SkipAll // 找到第一个就停止
		}
		return nil
	})
	if err != nil && err != filepath.SkipAll {
		return "", err
	}
	return found, nil
}

// findLargestVHDX 在目录中查找最大的 VHDX 文件
func findLargestVHDX(dir string) (string, error) {
	var largestPath string
	var largestSize int64

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && strings.EqualFold(filepath.Ext(path), ".vhdx") {
			if info.Size() > largestSize {
				largestSize = info.Size()
				largestPath = path
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return largestPath, nil
}

// ComponentStatus 组件可用性状态
type ComponentStatus struct {
	HyperV       bool
	VmCompute    bool
	Admin        bool
	BaseImage    bool
	ContainerLayers bool
	WSLKernel    bool
}

// CheckComponents 检查各组件可用性
func CheckComponents() *ComponentStatus {
	status := &ComponentStatus{}

	// 检查 Hyper-V（通过检查 vmms 服务）
	status.HyperV = checkServiceRunning("vmms")

	// 检查 vmcompute 服务
	status.VmCompute = checkServiceRunning("vmcompute")

	// 检查管理员权限
	status.Admin = checkAdmin()

	// 检查基础镜像
	if _, err := DetectBaseImage(); err == nil {
		status.BaseImage = true
	}

	// 检查容器镜像层
	if _, err := DetectContainerLayers(); err == nil {
		status.ContainerLayers = true
	}

	// 检查 WSL 内核
	if _, err := DetectWSLKernel(); err == nil {
		status.WSLKernel = true
	}

	return status
}

// checkServiceRunning 检查 Windows 服务是否正在运行
func checkServiceRunning(name string) bool {
	// 使用 SC query 检查服务状态
	// 简化实现：尝试检查服务相关文件
	switch name {
	case "vmms":
		// Hyper-V 虚拟机管理服务
		return fileExists(filepath.Join(os.Getenv("SystemRoot"), "System32", "vmms.exe"))
	case "vmcompute":
		// 容器计算服务
		return fileExists(filepath.Join(os.Getenv("SystemRoot"), "System32", "vmcompute.dll"))
	}
	return false
}

// checkAdmin 检查是否有管理员权限
func checkAdmin() bool {
	// 尝试访问只有管理员才能访问的路径
	f, err := os.Open(filepath.Join(os.Getenv("SystemRoot"), "System32", "config", "SAM"))
	if err != nil {
		return false
	}
	f.Close()
	return true
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
