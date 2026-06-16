package sandbox

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	HostPath  string
	GuestPath string
	ReadOnly  bool
}

// SandboxConfig 沙箱配置
type SandboxConfig struct {
	Name          string
	SandboxType   SandboxType
	MemoryMB      int
	CPUs          int
	EnableNetwork bool
	AllowDomains  []string
	SharedDirs    []SharedDir
	DiffDisk      string
	Verbose       bool
}

// ToHCSJSON 生成 HCS v2 JSON 配置文档
func (c *SandboxConfig) ToHCSJSON() (string, error) {
	switch c.SandboxType {
	case SandboxHyperV:
		return c.hypervVMConfig()
	case SandboxContainer:
		return c.windowsContainerConfig()
	case SandboxLinux:
		return c.linuxContainerConfig()
	default:
		return "", fmt.Errorf("不支持的沙箱类型: %s", c.SandboxType)
	}
}

// hypervVMConfig 生成 Hyper-V 虚拟机 v2 JSON（与 Rust 版本一致）
func (c *SandboxConfig) hypervVMConfig() (string, error) {
	baseImage, err := DetectBaseImage()
	if err != nil {
		return "", fmt.Errorf("未找到 Hyper-V 基础镜像: %w", err)
	}

	// 构建 v2 schema 的 HCS JSON（与 Rust 版本的 config.rs 一致）
	config := map[string]interface{}{
		"SchemaVersion": map[string]interface{}{
			"Major": 2,
			"Minor": 1,
		},
		"Owner": "win-sandbox",
		"ShouldTerminateOnLastHandleClosed": true,
		"VirtualMachine": map[string]interface{}{
			"StopOnReset": true,
			"Chipset": map[string]interface{}{},
			"ComputeTopology": map[string]interface{}{
				"Memory": map[string]interface{}{
					"SizeInMB": c.MemoryMB,
					"AllowOvercommit": true,
				},
				"Processor": map[string]interface{}{
					"Count": c.CPUs,
				},
			},
			"Devices": map[string]interface{}{
				"SCSI": map[string]interface{}{
					"0": map[string]interface{}{
						"Attachments": c.buildSCSIAttachments(baseImage),
					},
				},
			},
		},
	}

	// 添加共享目录（MappedDirectories 在 Devices 下）
	if len(c.SharedDirs) > 0 {
		var mappedDirs []map[string]interface{}
		for _, dir := range c.SharedDirs {
			mappedDirs = append(mappedDirs, map[string]interface{}{
				"HostPath":      dir.HostPath,
				"ContainerPath": dir.GuestPath,
				"ReadOnly":      dir.ReadOnly,
			})
		}
		devices := config["VirtualMachine"].(map[string]interface{})["Devices"].(map[string]interface{})
		devices["MappedDirectories"] = mappedDirs
	}

	// 添加网络配置
	if c.EnableNetwork {
		networkConfig := map[string]interface{}{
			"MaxSocketPortCount":  65535,
			"MaxPartitionSocketConnectionCount": 1024,
		}
		if len(c.AllowDomains) > 0 {
			networkConfig["DNSSearchList"] = strings.Join(c.AllowDomains, ",")
		}
		config["GuestNetwork"] = networkConfig
	}

	jsonBytes, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return "", fmt.Errorf("JSON 序列化失败: %w", err)
	}
	return string(jsonBytes), nil
}

// buildSCSIAttachments 构建 SCSI 附件（支持差分磁盘）
func (c *SandboxConfig) buildSCSIAttachments(baseImage string) map[string]interface{} {
	attachments := map[string]interface{}{
		"0": map[string]interface{}{
			"Path": baseImage,
			"Type": "VirtualDisk",
		},
	}

	if c.DiffDisk != "" {
		attachments["1"] = map[string]interface{}{
			"Path": c.DiffDisk,
			"Type": "VirtualDisk",
		}
	}

	return attachments
}

// windowsContainerConfig 生成 Windows 容器配置（使用 hcsshim v1 API）
func (c *SandboxConfig) windowsContainerConfig() (string, error) {
	layers, err := DetectContainerLayers()
	if err != nil {
		return "", fmt.Errorf("未找到容器镜像层: %w", err)
	}

	type Layer struct {
		ID   string `json:"Id"`
		Path string
	}
	var hcsLayers []Layer
	for i, layer := range layers {
		hcsLayers = append(hcsLayers, Layer{
			ID:   fmt.Sprintf("layer-%d", i),
			Path: layer,
		})
	}

	config := map[string]interface{}{
		"SystemType":              "Container",
		"Name":                    c.Name,
		"HvPartition":             false,
		"IgnoreFlushesDuringBoot": true,
		"ProcessorCount":          c.CPUs,
		"MemoryMaximumInMB":       c.MemoryMB,
		"TerminateOnLastHandleClosed": true,
		"Layers":                  hcsLayers,
	}

	if len(c.SharedDirs) > 0 {
		var mappedDirs []map[string]interface{}
		for _, dir := range c.SharedDirs {
			mappedDirs = append(mappedDirs, map[string]interface{}{
				"HostPath":      dir.HostPath,
				"ContainerPath": dir.GuestPath,
				"ReadOnly":      dir.ReadOnly,
			})
		}
		config["MappedDirectories"] = mappedDirs
	}

	if c.EnableNetwork {
		config["EndpointList"] = []string{}
		if len(c.AllowDomains) > 0 {
			config["DNSSearchList"] = strings.Join(c.AllowDomains, ",")
			config["AllowUnqualifiedDNSQuery"] = true
		}
	}

	jsonBytes, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return "", fmt.Errorf("JSON 序列化失败: %w", err)
	}
	return string(jsonBytes), nil
}

// linuxContainerConfig 生成 Linux 容器配置
func (c *SandboxConfig) linuxContainerConfig() (string, error) {
	kernelPath, err := DetectWSLKernel()
	if err != nil {
		return "", fmt.Errorf("未找到 WSL 内核: %w", err)
	}

	initrdPath := ""
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

	hvRuntime := map[string]interface{}{
		"LinuxKernelFile": kernelPath,
	}
	if initrdPath != "" {
		hvRuntime["LinuxInitrdFile"] = initrdPath
	}

	config := map[string]interface{}{
		"SystemType":              "Container",
		"Name":                    c.Name,
		"HvPartition":             true,
		"IgnoreFlushesDuringBoot": true,
		"ProcessorCount":          c.CPUs,
		"MemoryMaximumInMB":       c.MemoryMB,
		"TerminateOnLastHandleClosed": true,
		"HvRuntime":               hvRuntime,
	}

	if len(c.SharedDirs) > 0 {
		var mappedDirs []map[string]interface{}
		for _, dir := range c.SharedDirs {
			mappedDirs = append(mappedDirs, map[string]interface{}{
				"HostPath":      dir.HostPath,
				"ContainerPath": dir.GuestPath,
				"ReadOnly":      dir.ReadOnly,
			})
		}
		config["MappedDirectories"] = mappedDirs
	}

	if c.EnableNetwork {
		config["EndpointList"] = []string{}
	}

	jsonBytes, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return "", fmt.Errorf("JSON 序列化失败: %w", err)
	}
	return string(jsonBytes), nil
}

// buildProcessConfigJSON 生成进程执行配置 JSON
func buildProcessConfigJSON(cmd string, args []string, workDir string) string {
	commandLine := cmd
	if len(args) > 0 {
		commandLine = cmd + " " + strings.Join(args, " ")
	}

	config := map[string]interface{}{
		"CommandLine":      commandLine,
		"WorkingDirectory": workDir,
		"CreateStdInPipe":  true,
		"CreateStdOutPipe": true,
		"CreateStdErrPipe": true,
		"ConsoleSize":      [2]uint{25, 80},
	}

	jsonBytes, _ := json.Marshal(config)
	return string(jsonBytes)
}
