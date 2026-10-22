package signaturecmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/zane-xin/sparq-tool/internal/artifact/signature"
)

func newSignCommand() *cobra.Command {
	var keyPath string
	var output string
	command := &cobra.Command{
		Use:   "sign <file>",
		Short: "为文件生成签名",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			signaturePEM, err := signature.Sign(args[0], keyPath)
			if err != nil {
				return err
			}
			if err := signature.WriteSignature(output, signaturePEM); err != nil {
				return err
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "签名已生成: %s\n", output)
			return err
		},
	}
	command.Flags().StringVar(&keyPath, "key", "private.pem", "ed25519 私钥路径")
	command.Flags().StringVar(&output, "output", "signature.pem", "签名输出路径")
	return command
}
