package signaturecmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/zane-xin/sparq-tool/internal/artifact/signature"
)

func newKeygenCommand() *cobra.Command {
	var privatePath string
	var publicPath string
	command := &cobra.Command{
		Use:   "keygen",
		Short: "生成 ed25519 签名密钥",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			pair, err := signature.GenerateKeyPair()
			if err != nil {
				return err
			}
			if err := signature.WriteKeyPair(privatePath, publicPath, pair); err != nil {
				return err
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "签名密钥已生成: %s, %s\n", privatePath, publicPath)
			return err
		},
	}
	command.Flags().StringVar(&privatePath, "private", "private.pem", "私钥输出路径")
	command.Flags().StringVar(&publicPath, "public", "public.pem", "公钥输出路径")
	return command
}
