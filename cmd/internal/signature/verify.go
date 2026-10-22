package signaturecmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/zane-xin/sparq-tool/internal/artifact/signature"
)

func newVerifyCommand() *cobra.Command {
	var keyPath string
	var signaturePath string
	command := &cobra.Command{
		Use:   "verify <file>",
		Short: "校验文件签名",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if err := signature.Verify(args[0], keyPath, signaturePath); err != nil {
				return err
			}
			_, err := fmt.Fprintf(command.OutOrStdout(), "签名校验通过: %s\n", args[0])
			return err
		},
	}
	command.Flags().StringVar(&keyPath, "key", "public.pem", "ed25519 公钥路径")
	command.Flags().StringVar(&signaturePath, "signature", "signature.pem", "签名文件路径")
	return command
}
