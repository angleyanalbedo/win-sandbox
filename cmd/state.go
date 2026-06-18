package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/angleyanalbedo/win-sandbox/pkg/state"
	"github.com/spf13/cobra"
)

var stateCmd = &cobra.Command{
	Use:   "state <sandbox-id>",
	Short: "查看沙箱状态",
	Long:  "输出指定沙箱的详细状态信息。",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return showSandboxState(args[0])
	},
}

func init() {
	rootCmd.AddCommand(stateCmd)
}

func showSandboxState(id string) error {
	store, err := state.NewFileStore("")
	if err != nil {
		return fmt.Errorf("初始化状态存储失败: %w", err)
	}

	record, err := store.Load(id)
	if err != nil {
		return fmt.Errorf("加载沙箱状态失败: %w", err)
	}

	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}

	fmt.Println(string(data))
	return nil
}
