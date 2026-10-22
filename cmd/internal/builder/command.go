package buildercmd

import "github.com/spf13/cobra"

// New 创建 builder 命令。
func New() *cobra.Command {
	command := &cobra.Command{
		Use:   "builder",
		Short: "生成独立可信构建器的本地产物",
	}
	command.AddCommand(newCapsuleCommand())
	return command
}
