package cmd

import (
	"fmt"

	"github.com/angleyanalbedo/win-sandbox/pkg/sandbox"
	"github.com/angleyanalbedo/win-sandbox/pkg/state"
	"github.com/spf13/cobra"
)

var forceDelete bool

var deleteCmd = &cobra.Command{
	Use:   "delete <sandbox-id>",
	Short: "销毁沙箱",
	Long:  "停止并销毁指定的沙箱，清理所有资源。",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return deleteSandbox(args[0])
	},
}

func init() {
	deleteCmd.Flags().BoolVarP(&forceDelete, "force", "f", false, "强制删除（即使沙箱仍在运行）")
	rootCmd.AddCommand(deleteCmd)
}

func deleteSandbox(id string) error {
	store, err := state.NewFileStore("")
	if err != nil {
		return fmt.Errorf("初始化状态存储失败: %w", err)
	}

	// 检查沙箱是否存在
	record, err := store.Load(id)
	if err != nil {
		return fmt.Errorf("沙箱 %s 不存在", id)
	}

	if !forceDelete && record.Status == "running" {
		return fmt.Errorf("沙箱 %s 仍在运行，使用 --force 强制删除", id)
	}

	fmt.Printf("正在销毁沙箱 %s...\n", id)

	// 尝试连接到容器并销毁
	sb, err := sandbox.Attach(id, store)
	if err != nil {
		// 容器可能已经不存在，只清理状态
		fmt.Printf("警告: 无法连接到容器: %v\n", err)
		fmt.Println("清理状态记录...")
		return store.Delete(id)
	}
	defer sb.Close()

	if err := sb.Destroy(); err != nil {
		return fmt.Errorf("销毁沙箱失败: %w", err)
	}

	fmt.Printf("沙箱 %s 已销毁\n", id)
	return nil
}
