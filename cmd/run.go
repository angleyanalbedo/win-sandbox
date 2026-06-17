package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/angleyanalbedo/win-sandbox/pkg/sandbox"
	"github.com/spf13/cobra"
)

var (
	image   string
	memory  int
	cpus    int
	network string
	timeout time.Duration
	verbose bool
)

var runCmd = &cobra.Command{
	Use:   "run [command]",
	Short: "在沙箱中执行命令",
	Long:  "创建一个隔离的 Windows 容器沙箱，并在其中执行指定命令。",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runInSandbox(args)
	},
}

func init() {
	runCmd.Flags().StringVar(&image, "image", "mcr.microsoft.com/windows/nanoserver:ltsc2022", "容器镜像")
	runCmd.Flags().IntVar(&memory, "memory", 1024, "内存限制 (MB)")
	runCmd.Flags().IntVar(&cpus, "cpus", 2, "CPU 核数")
	runCmd.Flags().StringVar(&network, "network", "none", "网络模式: none, default")
	runCmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "命令超时")
	runCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "详细输出")
	rootCmd.AddCommand(runCmd)
}

func runInSandbox(args []string) error {
	// 构建沙箱选项
	opts := []sandbox.Option{
		sandbox.WithImage(image),
		sandbox.WithMemory(memory),
		sandbox.WithCPUs(cpus),
		sandbox.WithNetwork(network),
		sandbox.WithTimeout(timeout),
		sandbox.WithVerbose(verbose),
	}

	// 创建沙箱
	sb := sandbox.New(opts...)
	defer sb.Close()

	if verbose {
		fmt.Printf("沙箱 ID: %s\n", sb.ID())
		fmt.Printf("镜像: %s\n", image)
		fmt.Printf("内存: %d MB\n", memory)
		fmt.Printf("CPU: %d\n", cpus)
	}

	// 创建并启动
	fmt.Println("正在创建沙箱...")
	if err := sb.Create(); err != nil {
		return err
	}

	fmt.Println("正在启动沙箱...")
	if err := sb.Start(); err != nil {
		return err
	}

	// 执行命令
	command := args[0]
	if len(args) > 1 {
		for _, a := range args[1:] {
			command += " " + a
		}
	}

	if verbose {
		fmt.Printf("执行命令: %s\n", command)
	}

	result, err := sb.Execute(command)
	if err != nil {
		return err
	}

	// 输出结果
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

	if result.ExitCode != 0 {
		os.Exit(result.ExitCode)
	}

	return nil
}
