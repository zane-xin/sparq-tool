package artifactcmd

import (
	"github.com/spf13/cobra"
	"github.com/zane-xin/sparq-tool/cmd/internal/placeholder"
)

func newVerifyCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "verify <artifact>",
		Short: "校验产物 checksum 和 signature",
		Args:  cobra.ArbitraryArgs,
		RunE: func(command *cobra.Command, args []string) error {
			return placeholder.Print(command.OutOrStdout(), "artifact verify")
		},
	}
}
