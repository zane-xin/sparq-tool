package localcmd

import "github.com/spf13/cobra"

// New 创建 local 命令。
func New() *cobra.Command {
	command := &cobra.Command{
		Use:   "local",
		Short: "接入和检查本地源码插件",
	}
	command.AddCommand(newCheckCommand())
	command.AddCommand(newListCommand())
	command.AddCommand(newSyncCommand())
	return command
}
