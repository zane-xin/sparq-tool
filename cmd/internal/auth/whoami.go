package authcmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newWhoAmICommand() *cobra.Command {
	options := credentialOptions{timeout: defaultRequestTimeout}
	command := &cobra.Command{
		Use:   "whoami",
		Short: "查看当前 Sparq Cloud 账号",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			_, account, profile, _, err := verifiedAccount(command.Context(), options)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(command.OutOrStdout(), "profile: %s\nid: %d\nnickname: %s\n", profile, account.ID, account.Nickname); err != nil {
				return err
			}
			for _, field := range []struct {
				name  string
				value string
			}{
				{name: "username", value: account.Username},
				{name: "email", value: account.Email},
				{name: "mobile", value: account.Mobile},
				{name: "status", value: account.Status},
			} {
				if field.value != "" {
					if _, err := fmt.Fprintf(command.OutOrStdout(), "%s: %s\n", field.name, field.value); err != nil {
						return err
					}
				}
			}
			return nil
		},
	}
	options.bindFlags(command.Flags())
	return command
}
