package checksumcmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/zane-xin/sparq-tool/internal/artifact/checksum"
)

func newVerifyCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "verify <checksum-file>",
		Short: "校验 sha256 checksum 清单",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if err := checksum.Verify(args[0]); err != nil {
				return err
			}
			_, err := fmt.Fprintf(command.OutOrStdout(), "checksum 校验通过: %s\n", args[0])
			return err
		},
	}
}
