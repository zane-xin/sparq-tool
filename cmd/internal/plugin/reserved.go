package plugincmd

import (
	"github.com/spf13/cobra"
	"github.com/zane-xin/sparq-tool/cmd/internal/placeholder"
)

func newReservedCommand(use string, short string, name string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ArbitraryArgs,
		RunE: func(command *cobra.Command, args []string) error {
			return placeholder.Print(command.OutOrStdout(), name)
		},
	}
}
