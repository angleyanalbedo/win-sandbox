package cmd

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "wsandbox-vm",
	Short: "开源的 Windows 沙箱实现",
	Long: `wsandbox-vm 是一个基于 Microsoft HCS (Host Compute System) 的开源 Windows 沙箱工具。
它使用 hcsshim Go 库创建轻量级 Hyper-V 虚拟机或 Windows 容器，在隔离环境中执行程序，
执行完成后销毁沙箱，不留任何痕迹。

支持三种沙箱模式:
  - hyperv:     Hyper-V 虚拟机（最强隔离，最接近 Windows Sandbox）
  - container:  Windows 容器（进程级隔离，更快启动）
  - linux:      Linux 容器（通过 WSL2 Hyper-V 后端）`,
}

// Execute 执行根命令
func Execute() error {
	return rootCmd.Execute()
}
