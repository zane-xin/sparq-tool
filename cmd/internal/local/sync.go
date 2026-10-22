package localcmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zane-xin/sparq-tool/internal/plugin/localplugins"
)

func newSyncCommand() *cobra.Command {
	var appDir string
	var output string
	var dryRun bool
	command := &cobra.Command{
		Use:   "sync [workspace-root]",
		Short: "同步本地源码插件装配文件",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			root := "."
			if len(args) > 0 {
				root = strings.TrimSpace(args[0])
			}
			result, err := localplugins.Generate(localplugins.GenerateOptions{
				Root:   root,
				AppDir: appDir,
				Output: output,
			})
			if err != nil {
				return err
			}
			if dryRun {
				_, err := command.OutOrStdout().Write(result.Content)
				return err
			}
			if err := localplugins.Write(result); err != nil {
				return err
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "本地源码插件已同步: %s，插件数量: %d\n", result.Output, len(result.Entries))
			return err
		},
	}
	command.Flags().StringVar(&appDir, "app", "sparq-app", "标准 app 目录")
	command.Flags().StringVar(&output, "output", "", "输出文件路径，默认写入 app/cmd/sparq/plugins_gen.go")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "只输出生成内容，不写入文件")
	return command
}
