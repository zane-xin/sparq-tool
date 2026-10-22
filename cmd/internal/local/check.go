package localcmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zane-xin/sparq-tool/internal/workspace"
)

func newCheckCommand() *cobra.Command {
	var appDir string
	var pluginsDir string
	command := &cobra.Command{
		Use:   "check [workspace-root]",
		Short: "检查本地源码插件接入环境",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			root := "."
			if len(args) > 0 {
				root = strings.TrimSpace(args[0])
			}
			result, err := workspace.Check(workspace.CheckOptions{
				Root:       root,
				AppDir:     appDir,
				PluginsDir: pluginsDir,
			})
			if err != nil {
				return err
			}
			for _, item := range result.Items {
				if _, err := fmt.Fprintf(command.OutOrStdout(), "[%s] %s: %s\n", statusText(item.Status), item.Name, item.Message); err != nil {
					return err
				}
			}
			if result.HasFailure() {
				return fmt.Errorf("本地源码插件接入环境检查未通过")
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "本地源码插件接入环境检查通过: %s\n", result.Root)
			return err
		},
	}
	command.Flags().StringVar(&appDir, "app", "sparq-app", "标准 app 目录")
	command.Flags().StringVar(&pluginsDir, "plugins", "plugins", "插件目录")
	return command
}

func statusText(status workspace.Status) string {
	switch status {
	case workspace.StatusOK:
		return "通过"
	case workspace.StatusWarn:
		return "提示"
	case workspace.StatusFail:
		return "失败"
	default:
		return "未知"
	}
}
