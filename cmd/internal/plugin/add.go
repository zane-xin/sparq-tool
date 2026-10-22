package plugincmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zane-xin/sparq-tool/internal/plugin/scaffold"
)

func newAddCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "add",
		Short: "向已有插件增加组件",
	}
	command.AddCommand(newAddFrontendCommand())
	return command
}

func newAddFrontendCommand() *cobra.Command {
	var pathValue string
	command := &cobra.Command{
		Use:   "frontend [--path <plugin-dir>]",
		Short: "向已有 Go 插件增加独立前端",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			target, err := scaffold.AddFrontend(scaffold.FrontendAddOptions{Dir: strings.TrimSpace(pathValue)})
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "插件前端已添加: %s\n后续命令（在前端目录执行）:\n  pnpm install\n  pnpm build\n", target)
			return err
		},
	}
	command.Flags().StringVar(&pathValue, "path", ".", "已有插件目录，默认使用当前目录")
	return command
}
