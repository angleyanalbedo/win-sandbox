package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/angleyanalbedo/win-sandbox/pkg/state"
	"github.com/spf13/cobra"
)

var listFormat string

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "列出所有沙箱",
	Long:  "列出所有已创建的沙箱及其状态。",
	RunE: func(cmd *cobra.Command, args []string) error {
		return listSandboxes()
	},
}

func init() {
	listCmd.Flags().StringVarP(&listFormat, "format", "f", "table", "输出格式: table, json")
	rootCmd.AddCommand(listCmd)
}

func listSandboxes() error {
	store, err := state.NewFileStore("")
	if err != nil {
		return fmt.Errorf("初始化状态存储失败: %w", err)
	}

	records, err := store.List()
	if err != nil {
		return fmt.Errorf("列出沙箱失败: %w", err)
	}

	if len(records) == 0 {
		fmt.Println("没有已创建的沙箱")
		return nil
	}

	switch listFormat {
	case "json":
		data, err := json.MarshalIndent(records, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))

	case "table":
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tSTATUS\tIMAGE\tMEMORY\tCPU\tCREATED")
		for _, r := range records {
			fmt.Fprintf(w, "%s\t%s\t%s\t%d MB\t%d\t%s\n",
				r.ID,
				r.Status,
				r.ImageRef,
				r.MemoryMB,
				r.CPUs,
				r.CreatedAt.Format(time.RFC3339),
			)
		}
		w.Flush()

	default:
		return fmt.Errorf("不支持的格式: %s", listFormat)
	}

	return nil
}
