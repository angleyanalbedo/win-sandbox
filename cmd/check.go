package cmd

import (
	"fmt"

	"github.com/angleyanalbedo/win-sandbox/pkg/sandbox"
	"github.com/spf13/cobra"
)

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "检查系统环境和组件可用性",
	Long:  `检查 Windows 版本、Hyper-V 状态、vmcompute 服务、管理员权限以及各沙箱模式所需的基础镜像。`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmdCheck()
	},
}

func init() {
	rootCmd.AddCommand(checkCmd)
}

func cmdCheck() error {
	fmt.Println("=== win-sandbox 环境检查 ===")
	fmt.Println()

	status := sandbox.CheckComponents()

	// 检查管理员权限
	printCheck("管理员权限", status.Admin)

	// 检查 vmcompute 服务
	printCheck("vmcompute 服务", status.VmCompute)

	// 检查 Hyper-V
	printCheck("Hyper-V", status.HyperV)

	// 检查各模式可用性
	fmt.Println()
	fmt.Println("=== 沙箱模式可用性 ===")
	fmt.Println()

	printCheck("Hyper-V VM 模式 (hyperv)", status.BaseImage)
	printCheck("Windows 容器模式 (container)", status.ContainerLayers)
	printCheck("Linux 容器模式 (linux)", status.WSLKernel)

	// 给出建议
	fmt.Println()
	fmt.Println("=== 权限说明 ===")
	fmt.Println()
	if status.Admin {
		fmt.Println("  ✅ 当前以管理员权限运行")
	} else {
		fmt.Println("  ℹ  当前非管理员，run 命令会自动触发 UAC 提权")
	}
	if !status.VmCompute {
		fmt.Println("⚠ 请确保已启用 Windows 容器功能:")
		fmt.Println("  dism /online /enable-feature /featurename:Containers /All")
	}
	if !status.HyperV {
		fmt.Println("⚠ 请确保已启用 Hyper-V:")
		fmt.Println("  dism /online /enable-feature /featurename:Microsoft-Hyper-V /All")
	}

	// 诊断信息
	fmt.Println()
	fmt.Println("=== 诊断信息 ===")
	fmt.Println()

	// 检测到的路径
	baseImage, err := sandbox.DetectBaseImage()
	if err != nil {
		fmt.Printf("  基础镜像: 未找到 (%v)\n", err)
	} else {
		fmt.Printf("  基础镜像: %s\n", baseImage)
	}

	// dump 一个示例 hyperv 配置
	cfg := &sandbox.SandboxConfig{
		Name:        "diagnostic",
		SandboxType: sandbox.SandboxHyperV,
		MemoryMB:    1024,
		CPUs:        2,
	}
	if j, err := cfg.ToHCSJSON(); err == nil {
		fmt.Println()
		fmt.Println("  Hyper-V 模式示例配置:")
		fmt.Printf("  %s\n", j)
	}

	return nil
}

func printCheck(name string, ok bool) {
	status := "❌ 不可用"
	if ok {
		status = "✅ 可用"
	}
	fmt.Printf("  %-30s %s\n", name, status)
}
