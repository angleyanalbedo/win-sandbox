package cmd

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/Microsoft/hcsshim"
	"github.com/angleyanalbedo/win-sandbox/pkg/docker"
	"github.com/spf13/cobra"
)

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "检查环境",
	Long:  "检查 Docker、HCS、层存储等环境状态。",
	RunE: func(cmd *cobra.Command, args []string) error {
		return checkEnvironment()
	},
}

func init() {
	rootCmd.AddCommand(checkCmd)
}

func checkEnvironment() error {
	fmt.Println("=== win-sandbox 环境检查 ===")
	fmt.Println()

	// 检查 Docker
	fmt.Println("--- Docker ---")
	if err := exec.Command("docker", "info").Run(); err != nil {
		fmt.Println("  状态: ❌ 不可用")
		fmt.Println("  请确保 Docker Desktop 已安装并运行")
	} else {
		fmt.Println("  状态: ✅ 可用")

		// 检查 Docker 模式
		out, err := exec.Command("docker", "info", "--format", "{{.OSType}}").Output()
		if err == nil {
			mode := strings.TrimSpace(string(out))
			fmt.Printf("  模式: %s\n", mode)
			if mode != "windows" {
				fmt.Println("  ⚠ 需要切换到 Windows 容器模式")
			}
		}
	}

	fmt.Println()

	// 检查镜像
	fmt.Println("--- 容器镜像 ---")
	imageRef := "mcr.microsoft.com/windows/nanoserver:ltsc2022"
	layers, err := docker.FindImageLayers(imageRef)
	if err != nil {
		fmt.Printf("  %s: ❌ 未找到\n", imageRef)
		fmt.Println("  请运行: docker pull", imageRef)
	} else {
		fmt.Printf("  %s: ✅ 可用 (%d 层)\n", imageRef, len(layers))
		base := layers[len(layers)-1]
		fmt.Printf("  基础层: %s\n", base.Path)
	}

	fmt.Println()

	// 检查 HCS
	fmt.Println("--- HCS 服务 ---")
	info := hcsshim.DriverInfo{Flavour: 1, HomeDir: docker.LayerStore}
	if _, err := hcsshim.LayerExists(info, "test"); err != nil {
		fmt.Println("  状态: ⚠ 可能需要管理员权限")
	} else {
		fmt.Println("  状态: ✅ 可用")
	}

	fmt.Println()
	fmt.Println("=== 检查完成 ===")
	return nil
}
