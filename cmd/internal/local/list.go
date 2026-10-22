package localcmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zane-xin/sparq-tool/internal/plugin/localplugins"
)

func newListCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "list [workspace-root]",
		Short: "列出 go.work 接入的本地源码插件",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			root := "."
			if len(args) > 0 {
				root = strings.TrimSpace(args[0])
			}
			entries, err := localplugins.Discover(localplugins.DiscoverOptions{Root: root})
			if err != nil {
				return err
			}
			if len(entries) == 0 {
				_, err := fmt.Fprintln(command.OutOrStdout(), "未发现 go.work 接入的本地源码插件")
				return err
			}
			for _, entry := range entries {
				if _, err := fmt.Fprintf(command.OutOrStdout(), "%s\t%s\t%s\t%s\n", entry.Code, entry.Version, entry.Module, entry.Path); err != nil {
					return err
				}
			}
			return nil
		},
	}
	return command
}
