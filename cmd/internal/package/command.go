package packagecmd

import "github.com/spf13/cobra"

// New 创建 package 命令。
func New() *cobra.Command {
	command := &cobra.Command{
		Use:   "package",
		Short: "打包 Sparq 项目",
	}
	command.AddCommand(newAppCommand())
	return command
}
