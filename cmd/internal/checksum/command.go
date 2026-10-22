package checksumcmd

import "github.com/spf13/cobra"

// New 创建 checksum 命令。
func New() *cobra.Command {
	command := &cobra.Command{
		Use:   "checksum",
		Short: "生成或校验文件 checksum",
	}
	command.AddCommand(newCreateCommand())
	command.AddCommand(newVerifyCommand())
	return command
}
