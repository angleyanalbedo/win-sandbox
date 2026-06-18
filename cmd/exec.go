package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/angleyanalbedo/win-sandbox/pkg/sandbox"
	"github.com/angleyanalbedo/win-sandbox/pkg/state"
	"github.com/spf13/cobra"
)

var (
	interactive bool
)

var execCmd = &cobra.Command{
	Use:   "exec <sandbox-id> <command> [args...]",
	Short: "在沙箱中执行命令",
	Long:  "在已创建的沙箱中执行命令。使用 -it 标志进入交互式 shell。",
	Args:  cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]
		command := strings.Join(args[1:], " ")
		return execInSandbox(id, command)
	},
}

func init() {
	execCmd.Flags().BoolVarP(&interactive, "interactive", "i", false, "交互式模式 (接管 stdin)")
	execCmd.Flags().BoolVarP(&tty, "tty", "t", false, "分配伪终端")
	rootCmd.AddCommand(execCmd)
}

var tty bool

func execInSandbox(id string, command string) error {
	// 初始化状态存储
	store, err := state.NewFileStore("")
	if err != nil {
		return fmt.Errorf("初始化状态存储失败: %w", err)
	}

	// 连接到已存在的沙箱
	sb, err := sandbox.Attach(id, store)
	if err != nil {
		return fmt.Errorf("连接沙箱失败: %w", err)
	}
	defer sb.Close()

	if interactive || tty {
		// 交互式模式
		exitCode, err := sb.ExecInteractive(command)
		if err != nil {
			return err
		}
		if exitCode != 0 {
			os.Exit(exitCode)
		}
	} else {
		// 一次性命令模式
		result, err := sb.Execute(command)
		if err != nil {
			return err
		}

		if result.Stdout != "" {
			fmt.Print(result.Stdout)
		}
		if result.Stderr != "" {
			fmt.Fprint(os.Stderr, result.Stderr)
		}

		if result.ExitCode != 0 {
			os.Exit(result.ExitCode)
		}
	}

	return nil
}
