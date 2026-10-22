package restartcmd

import "github.com/spf13/cobra"

// New 创建 restart 命令。
func New() *cobra.Command {
	command := &cobra.Command{
		Use:   "restart",
		Short: "重启开发环境中的 app",
	}
	command.AddCommand(newAppCommand())
	return command
}
