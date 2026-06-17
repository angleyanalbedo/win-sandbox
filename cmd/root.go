package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "win-sandbox",
	Short: "Windows 沙箱工具",
	Long:  "基于 HCS API 的 Windows 容器沙箱工具，用于在隔离环境中执行命令。",
}

// Execute 执行根命令
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
