package authcmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

func newStatusCommand() *cobra.Command {
	options := credentialOptions{timeout: defaultRequestTimeout}
	command := &cobra.Command{
		Use:   "status",
		Short: "验证当前 Sparq Cloud 登录状态",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			session, account, profile, path, err := verifiedAccount(command.Context(), options)
			if err != nil {
				return err
			}
			expiresAt := "不限制"
			if session.ExpiresAt > 0 {
				expiresAt = time.Unix(session.ExpiresAt, 0).Local().Format(time.RFC3339)
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "Sparq Cloud 登录态有效\nprofile: %s\nendpoint: %s\naccount: %s\nexpiresAt: %s\nconfig: %s\n", profile, session.Endpoint, accountDisplay(account), expiresAt, path)
			return err
		},
	}
	options.bindFlags(command.Flags())
	return command
}
