package authcmd

import "github.com/spf13/cobra"

// New 创建 Sparq Cloud 认证命令。
func New() *cobra.Command {
	command := &cobra.Command{
		Use:   "auth",
		Short: "管理 Sparq Cloud 用户认证",
	}
	command.AddCommand(newLoginCommand())
	command.AddCommand(newLogoutCommand())
	command.AddCommand(newStatusCommand())
	command.AddCommand(newWhoAmICommand())
	return command
}
