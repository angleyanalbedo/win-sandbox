package cmd

import (
	"fmt"

	"github.com/angleyanalbedo/win-sandbox/pkg/sandbox"
	"github.com/angleyanalbedo/win-sandbox/pkg/state"
	"github.com/spf13/cobra"
)

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "创建持久化沙箱",
	Long:  "创建一个持久化的 Windows 容器沙箱，可多次执行命令后手动销毁。",
	RunE: func(cmd *cobra.Command, args []string) error {
		return createSandbox()
	},
}

func init() {
	createCmd.Flags().StringVar(&image, "image", "mcr.microsoft.com/windows/nanoserver:ltsc2022", "容器镜像")
	createCmd.Flags().IntVar(&memory, "memory", 1024, "内存限制 (MB)")
	createCmd.Flags().IntVar(&cpus, "cpus", 2, "CPU 核数")
	createCmd.Flags().StringVar(&network, "network", "none", "网络模式: none, default")
	createCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "详细输出")
	rootCmd.AddCommand(createCmd)
}

func createSandbox() error {
	// 初始化状态存储
	store, err := state.NewFileStore("")
	if err != nil {
		return fmt.Errorf("初始化状态存储失败: %w", err)
	}

	// 构建沙箱选项
	opts := []sandbox.Option{
		sandbox.WithImage(image),
		sandbox.WithMemory(memory),
		sandbox.WithCPUs(cpus),
		sandbox.WithNetwork(network),
		sandbox.WithVerbose(verbose),
	}

	// 创建沙箱（带状态持久化）
	sb := sandbox.NewWithStore(store, opts...)

	if verbose {
		fmt.Printf("沙箱 ID: %s\n", sb.ID())
		fmt.Printf("镜像: %s\n", image)
		fmt.Printf("内存: %d MB\n", memory)
		fmt.Printf("CPU: %d\n", cpus)
	}

	// 创建容器
	fmt.Println("正在创建沙箱...")
	if err := sb.Create(); err != nil {
		return err
	}

	// 启动容器
	fmt.Println("正在启动沙箱...")
	if err := sb.Start(); err != nil {
		sb.Destroy()
		return err
	}

	// 释放句柄（TerminateOnLastHandleClosed=false，容器继续运行）
	defer sb.Close()

	fmt.Printf("沙箱已创建: %s\n", sb.ID())
	fmt.Printf("使用 'win-sandbox exec %s <command>' 执行命令\n", sb.ID())
	fmt.Printf("使用 'win-sandbox delete %s' 销毁沙箱\n", sb.ID())

	return nil
}
