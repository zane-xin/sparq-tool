package buildcmd

import "github.com/spf13/cobra"

// New 创建 build 命令。
func New() *cobra.Command {
	command := &cobra.Command{
		Use:   "build",
		Short: "构建 Sparq 项目",
	}
	command.AddCommand(newAppCommand())
	return command
}
