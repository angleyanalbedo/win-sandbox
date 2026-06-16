package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Microsoft/hcsshim"
)

// SandboxType 沙箱类型
type SandboxType string

const (
	SandboxHyperV    SandboxType = "hyperv"
	SandboxContainer SandboxType = "container"
	SandboxLinux     SandboxType = "linux"
)

// SharedDir 共享目录配置
type SharedDir struct {
	HostPath    string
	GuestPath   string
	ReadOnly    bool
}

// SandboxConfig 沙箱配置
type SandboxConfig struct {
	Name         string
	SandboxType  SandboxType
	MemoryMB     int
	CPUs         int
	EnableNetwork bool
	AllowDomains []string
	SharedDirs   []SharedDir
	DiffDisk     string
	Verbose      bool
}

// ToContainerConfig 将沙箱配置转换为 hcsshim ContainerConfig
func (c *SandboxConfig) ToContainerConfig() (*hcsshim.ContainerConfig, error) {
	switch c.SandboxType {
	case SandboxHyperV:
		return c.hypervConfig()
	case SandboxContainer:
		return c.containerConfig()
	case SandboxLinux:
		return c.linuxConfig()
	default:
		return nil, fmt.Errorf("不支持的沙箱类型: %s", c.SandboxType)
	}
}

// hypervConfig 构建 Hyper-V 虚拟机配置（最强隔离，最接近 Windows Sandbox）
func (c *SandboxConfig) hypervConfig() (*hcsshim.ContainerConfig, error) {
	// 检测基础镜像
	baseImage, err := DetectBaseImage()
	if err != nil {
		return nil, fmt.Errorf("未找到 Hyper-V 基础镜像: %w", err)
	}

	cfg := &hcsshim.ContainerConfig{
		SystemType:              "Container",
		Name:                    c.Name,
		HvPartition:             true,
		IgnoreFlushesDuringBoot: true,
		ProcessorCount:          uint32(c.CPUs),
		MemoryMaximumInMB:       int64(c.MemoryMB),
		TerminateOnLastHandleClosed: true,
		HvRuntime: &hcsshim.HvRuntime{
			ImagePath:    baseImage,
			SkipTemplate: true,
		},
	}

	// 配置差分磁盘层
	if c.DiffDisk != "" {
		cfg.Layers = []hcsshim.Layer{
			{ID: c.Name + "-layer", Path: c.DiffDisk},
		}
	}

	// 配置共享目录
	for _, dir := range c.SharedDirs {
		cfg.MappedDirectories = append(cfg.MappedDirectories, hcsshim.MappedDir{
			HostPath:      dir.HostPath,
			ContainerPath: dir.GuestPath,
			ReadOnly:      dir.ReadOnly,
		})
	}

	// 配置网络
	if c.EnableNetwork {
		cfg.EndpointList = []string{} // HCS 会自动创建 NAT 网络
		if len(c.AllowDomains) > 0 {
			cfg.DNSSearchList = strings.Join(c.AllowDomains, ",")
			cfg.AllowUnqualifiedDNSQuery = true
		}
	}

	return cfg, nil
}

// containerConfig 构建 Windows 容器配置（进程级隔离，更快启动）
func (c *SandboxConfig) containerConfig() (*hcsshim.ContainerConfig, error) {
	layers, err := DetectContainerLayers()
	if err != nil {
		return nil, fmt.Errorf("未找到容器镜像层: %w", err)
	}

	var hcsLayers []hcsshim.Layer
	for i, layer := range layers {
		hcsLayers = append(hcsLayers, hcsshim.Layer{
			ID:   fmt.Sprintf("layer-%d", i),
			Path: layer,
		})
	}

	cfg := &hcsshim.ContainerConfig{
		SystemType:              "Container",
		Name:                    c.Name,
		HvPartition:             false,
		IgnoreFlushesDuringBoot: true,
		ProcessorCount:          uint32(c.CPUs),
		MemoryMaximumInMB:       int64(c.MemoryMB),
		TerminateOnLastHandleClosed: true,
		Layers:                  hcsLayers,
	}

	// 配置共享目录
	for _, dir := range c.SharedDirs {
		cfg.MappedDirectories = append(cfg.MappedDirectories, hcsshim.MappedDir{
			HostPath:      dir.HostPath,
			ContainerPath: dir.GuestPath,
			ReadOnly:      dir.ReadOnly,
		})
	}

	// 配置网络
	if c.EnableNetwork {
		cfg.EndpointList = []string{}
		if len(c.AllowDomains) > 0 {
			cfg.DNSSearchList = strings.Join(c.AllowDomains, ",")
			cfg.AllowUnqualifiedDNSQuery = true
		}
	}

	return cfg, nil
}

// linuxConfig 构建 Linux 容器配置（通过 WSL2 Hyper-V 后端）
func (c *SandboxConfig) linuxConfig() (*hcsshim.ContainerConfig, error) {
	kernelPath, err := DetectWSLKernel()
	if err != nil {
		return nil, fmt.Errorf("未找到 WSL 内核: %w", err)
	}

	initrdPath := ""
	// 尝试查找 initrd 文件
	initrdDir := filepath.Dir(kernelPath)
	candidates := []string{
		filepath.Join(initrdDir, "initrd.img"),
		filepath.Join(initrdDir, "initrd"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			initrdPath = p
			break
		}
	}

	cfg := &hcsshim.ContainerConfig{
		SystemType:              "Container",
		Name:                    c.Name,
		HvPartition:             true,
		IgnoreFlushesDuringBoot: true,
		ProcessorCount:          uint32(c.CPUs),
		MemoryMaximumInMB:       int64(c.MemoryMB),
		TerminateOnLastHandleClosed: true,
		HvRuntime: &hcsshim.HvRuntime{
			LinuxKernelFile: kernelPath,
		},
	}
	if initrdPath != "" {
		cfg.HvRuntime.LinuxInitrdFile = initrdPath
	}

	// 配置共享目录
	for _, dir := range c.SharedDirs {
		cfg.MappedDirectories = append(cfg.MappedDirectories, hcsshim.MappedDir{
			HostPath:      dir.HostPath,
			ContainerPath: dir.GuestPath,
			ReadOnly:      dir.ReadOnly,
		})
	}

	// 配置网络
	if c.EnableNetwork {
		cfg.EndpointList = []string{}
	}

	return cfg, nil
}

// buildProcessConfig 构建进程执行配置
func buildProcessConfig(cmd string, args []string, workDir string) *hcsshim.ProcessConfig {
	commandLine := cmd
	if len(args) > 0 {
		commandLine = cmd + " " + strings.Join(args, " ")
	}

	return &hcsshim.ProcessConfig{
		CommandLine:      commandLine,
		WorkingDirectory: workDir,
		CreateStdInPipe:  true,
		CreateStdOutPipe: true,
		CreateStdErrPipe: true,
		ConsoleSize:      [2]uint{25, 80},
	}
}
