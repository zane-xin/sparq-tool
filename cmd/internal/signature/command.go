package signaturecmd

import "github.com/spf13/cobra"

// New 创建 signature 命令。
func New() *cobra.Command {
	command := &cobra.Command{
		Use:   "signature",
		Short: "生成和校验插件产物签名",
	}
	command.AddCommand(newKeygenCommand())
	command.AddCommand(newSignCommand())
	command.AddCommand(newVerifyCommand())
	return command
}
