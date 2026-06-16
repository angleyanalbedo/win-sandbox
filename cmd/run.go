package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/angleyanalbedo/win-sandbox/pkg/sandbox"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var (
	memory     int
	cpus       int
	timeout    time.Duration
	network    bool
	domains    []string
	shares     []string
	diskType   string
	verbose    bool
	jsonOutput bool
)

var runCmd = &cobra.Command{
	Use:   "run [flags] <command> [args...]",
	Short: "在沙箱中执行命令",
	Long:  `在隔离的 Windows 沙箱（Hyper-V VM 或容器）中执行指定命令，并捕获输出。`,
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmdRun(args[0], args[1:])
	},
}

func init() {
	runCmd.Flags().IntVarP(&memory, "memory", "m", 2048, "分配的内存 (MB)")
	runCmd.Flags().IntVarP(&cpus, "cpus", "c", 2, "分配的 CPU 数量")
	runCmd.Flags().DurationVarP(&timeout, "timeout", "t", 5*time.Minute, "命令超时时间")
	runCmd.Flags().BoolVar(&network, "network", false, "启用网络访问")
	runCmd.Flags().StringSliceVar(&domains, "allow-domain", nil, "允许访问的域名白名单")
	runCmd.Flags().StringSliceVarP(&shares, "share", "s", nil, "共享目录 (host:guest[:ro])")
	runCmd.Flags().StringVar(&diskType, "sandbox-type", "hyperv", "沙箱类型: hyperv, container, linux")
	runCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "详细输出")
	runCmd.Flags().BoolVar(&jsonOutput, "json", false, "以 JSON 格式输出结果")

	rootCmd.AddCommand(runCmd)
}

func cmdRun(command string, args []string) error {
	if verbose {
		logrus.SetLevel(logrus.DebugLevel)
	}

	// 自动 UAC 提权（如果不是管理员，会弹出 UAC 弹窗并重启进程）
	if err := sandbox.EnsureAdmin(); err != nil {
		return fmt.Errorf("提权失败: %w", err)
	}

	// 检查前提条件（此时已有管理员权限）
	if err := sandbox.CheckPrerequisites(); err != nil {
		return fmt.Errorf("前提条件检查失败: %w", err)
	}

	// 解析共享目录
	sharedDirs, err := parseSharedDirs(shares)
	if err != nil {
		return fmt.Errorf("解析共享目录失败: %w", err)
	}

	// 构建配置
	cfg := &sandbox.SandboxConfig{
		SandboxType:   sandbox.SandboxType(diskType),
		MemoryMB:      memory,
		CPUs:          cpus,
		EnableNetwork: network,
		AllowDomains:  domains,
		SharedDirs:    sharedDirs,
		Verbose:       verbose,
	}

	// 创建并启动沙箱
	sb := sandbox.New(cfg)
	if err := sb.Start(); err != nil {
		return fmt.Errorf("启动沙箱失败: %w", err)
	}
	defer sb.Terminate()

	// 执行命令
	result, err := sb.Execute(command, args, timeout)
	if err != nil {
		return fmt.Errorf("执行命令失败: %w", err)
	}

	// 输出结果
	if jsonOutput {
		fmt.Printf(`{"exitCode":%d,"stdout":%q,"stderr":%q,"elapsed":"%s"}`+"\n",
			result.ExitCode, result.Stdout, result.Stderr, result.Elapsed)
	} else {
		if result.Stdout != "" {
			fmt.Print(result.Stdout)
		}
		if result.Stderr != "" {
			fmt.Fprint(os.Stderr, result.Stderr)
		}
		if verbose {
			fmt.Fprintf(os.Stderr, "\n--- 执行完成 ---\n")
			fmt.Fprintf(os.Stderr, "退出码: %d\n", result.ExitCode)
			fmt.Fprintf(os.Stderr, "耗时: %s\n", result.Elapsed)
		}
	}

	// 传递退出码
	if result.ExitCode != 0 {
		os.Exit(result.ExitCode)
	}
	return nil
}

// parseSharedDirs 解析共享目录参数 "host:guest[:ro]"
func parseSharedDirs(shares []string) ([]sandbox.SharedDir, error) {
	var dirs []sandbox.SharedDir
	for _, s := range shares {
		parts := strings.Split(s, ":")
		if len(parts) < 2 {
			return nil, fmt.Errorf("共享目录格式错误: %s (应为 host:guest[:ro])", s)
		}
		dir := sandbox.SharedDir{
			HostPath:  parts[0],
			GuestPath: parts[1],
		}
		if len(parts) > 2 && strings.EqualFold(parts[2], "ro") {
			dir.ReadOnly = true
		}
		dirs = append(dirs, dir)
	}
	return dirs, nil
}
