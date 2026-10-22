package artifactcmd

import "github.com/spf13/cobra"

// New 创建 artifact 命令。
func New() *cobra.Command {
	command := &cobra.Command{
		Use:   "artifact",
		Short: "下载和处理插件产物",
	}
	command.AddCommand(newDownloadCommand())
	command.AddCommand(newVerifyCommand())
	return command
}
