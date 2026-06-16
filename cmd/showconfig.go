package cmd

import (
	"fmt"

	"github.com/angleyanalbedo/win-sandbox/pkg/sandbox"
	"github.com/spf13/cobra"
)

var showConfigCmd = &cobra.Command{
	Use:   "show-config",
	Short: "显示生成的 HCS JSON 配置",
	Long:  `根据当前参数生成 HCS JSON 配置并打印，用于调试和验证。`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmdShowConfig()
	},
}

func init() {
	showConfigCmd.Flags().IntVarP(&memory, "memory", "m", 2048, "分配的内存 (MB)")
	showConfigCmd.Flags().IntVarP(&cpus, "cpus", "c", 2, "分配的 CPU 数量")
	showConfigCmd.Flags().BoolVar(&network, "network", false, "启用网络访问")
	showConfigCmd.Flags().StringSliceVar(&domains, "allow-domain", nil, "允许访问的域名白名单")
	showConfigCmd.Flags().StringSliceVarP(&shares, "share", "s", nil, "共享目录 (host:guest[:ro])")
	showConfigCmd.Flags().StringVar(&diskType, "sandbox-type", "hyperv", "沙箱类型: hyperv, container, linux")

	rootCmd.AddCommand(showConfigCmd)
}

func cmdShowConfig() error {
	// 解析共享目录
	sharedDirs, err := parseSharedDirs(shares)
	if err != nil {
		return fmt.Errorf("解析共享目录失败: %w", err)
	}

	cfg := &sandbox.SandboxConfig{
		SandboxType:   sandbox.SandboxType(diskType),
		MemoryMB:      memory,
		CPUs:          cpus,
		EnableNetwork: network,
		AllowDomains:  domains,
		SharedDirs:    sharedDirs,
	}

	sb := sandbox.New(cfg)
	return sb.PrintConfig()
}
